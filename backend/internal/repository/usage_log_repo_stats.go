package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/th3ee9ine/qqq2api/internal/pkg/logger"
	"github.com/th3ee9ine/qqq2api/internal/pkg/timezone"
	"github.com/th3ee9ine/qqq2api/internal/pkg/usagestats"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

const usageRollupDateLayout = "2006-01-02"

// usageRollupDate converts a timestamp to the application's natural date
// before it is sent to PostgreSQL.  The rollup trigger stores bucket_date in
// the session/application timezone; sending a DATE-shaped value here avoids
// relying on a connection's implicit timestamptz->date cast (which can differ
// for externally-created repository connections).
func usageRollupDate(value time.Time) string {
	return value.In(timezone.Location()).Format(usageRollupDateLayout)
}

// usageRollupDateRangeBounds is the date-range variant used by the SQL
// reconciliation query.  In addition to DATE-shaped bounds it returns the
// corresponding half-open timestamptz interval, allowing PostgreSQL to use the
// existing (account_id, created_at) index when scanning retained rows.
func usageRollupDateRangeBounds(startTime, endTime time.Time) (startDate, endDate string, startBound, endBound time.Time) {
	loc := timezone.Location()
	startLocal := startTime.In(loc)
	endLocal := endTime.In(loc)
	startDay := time.Date(startLocal.Year(), startLocal.Month(), startLocal.Day(), 0, 0, 0, 0, loc)
	endDay := time.Date(endLocal.Year(), endLocal.Month(), endLocal.Day(), 0, 0, 0, 0, loc)

	if !endTime.After(startTime) {
		// Keep an empty date interval when callers provide a reversed/equal
		// timestamp range; `bucket_date >= D AND bucket_date < D` is empty.
		endDay = startDay
	} else if endLocal.After(endDay) {
		// A non-midnight end intersects its local end day, so include that day
		// by making the date bound exclusive at the following midnight.
		endDay = endDay.AddDate(0, 0, 1)
	}

	return startDay.Format(usageRollupDateLayout), endDay.Format(usageRollupDateLayout), startDay, endDay
}

// accountDailyUsageStats is the subset of usage counters needed by the
// account-facing statistics endpoints.  The durable rollup contains the
// counters for every row ever written; the raw aggregate is used below to
// preserve exact timestamp-range semantics for rows that are still retained.
type accountDailyUsageStats struct {
	Requests            int64
	InputTokens         int64
	OutputTokens        int64
	CacheCreationTokens int64
	CacheReadTokens     int64
	StandardCost        float64
	AccountCost         float64
	UserCost            float64
	TotalDurationMs     int64
	DurationCount       int64
}

type accountDailyRawUsageStats struct {
	All     accountDailyUsageStats
	InRange accountDailyUsageStats
}

func hasAccountDailyUsage(stats accountDailyUsageStats) bool {
	return stats.Requests != 0 ||
		stats.InputTokens != 0 ||
		stats.OutputTokens != 0 ||
		stats.CacheCreationTokens != 0 ||
		stats.CacheReadTokens != 0 ||
		stats.StandardCost != 0 ||
		stats.AccountCost != 0 ||
		stats.UserCost != 0 ||
		stats.TotalDurationMs != 0 ||
		stats.DurationCount != 0
}

func combineAccountDailyUsageStats(durable accountDailyUsageStats, raw accountDailyRawUsageStats) accountDailyUsageStats {
	// The rollup is populated by the INSERT trigger for every normal write, so
	// subtracting the retained raw rows removes their contribution before the
	// exact in-range subset is added back.  A direct INSERT into a partition
	// that predates the child-trigger installation can make raw.All temporarily
	// larger than the durable bucket.  Treat raw.All as the lower bound before
	// subtraction so those retained rows are not lost from the result.  Clamp
	// counters at zero to keep malformed/direct-import data from producing
	// negative UI values.
	durable.Requests = maxInt64(0, maxInt64(durable.Requests, raw.All.Requests)-raw.All.Requests+raw.InRange.Requests)
	durable.InputTokens = maxInt64(0, maxInt64(durable.InputTokens, raw.All.InputTokens)-raw.All.InputTokens+raw.InRange.InputTokens)
	durable.OutputTokens = maxInt64(0, maxInt64(durable.OutputTokens, raw.All.OutputTokens)-raw.All.OutputTokens+raw.InRange.OutputTokens)
	durable.CacheCreationTokens = maxInt64(0, maxInt64(durable.CacheCreationTokens, raw.All.CacheCreationTokens)-raw.All.CacheCreationTokens+raw.InRange.CacheCreationTokens)
	durable.CacheReadTokens = maxInt64(0, maxInt64(durable.CacheReadTokens, raw.All.CacheReadTokens)-raw.All.CacheReadTokens+raw.InRange.CacheReadTokens)
	durable.TotalDurationMs = maxInt64(0, maxInt64(durable.TotalDurationMs, raw.All.TotalDurationMs)-raw.All.TotalDurationMs+raw.InRange.TotalDurationMs)
	durable.DurationCount = maxInt64(0, maxInt64(durable.DurationCount, raw.All.DurationCount)-raw.All.DurationCount+raw.InRange.DurationCount)

	durable.StandardCost = maxFloat64(0, maxFloat64(durable.StandardCost, raw.All.StandardCost)-raw.All.StandardCost+raw.InRange.StandardCost)
	durable.AccountCost = maxFloat64(0, maxFloat64(durable.AccountCost, raw.All.AccountCost)-raw.All.AccountCost+raw.InRange.AccountCost)
	durable.UserCost = maxFloat64(0, maxFloat64(durable.UserCost, raw.All.UserCost)-raw.All.UserCost+raw.InRange.UserCost)
	return durable
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func maxFloat64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// loadAccountDailyUsageStats combines durable daily counters with retained raw
// rows in one PostgreSQL statement.  Keeping both aggregates in the same
// statement gives them one MVCC snapshot: a concurrent INSERT or cleanup
// DELETE therefore cannot be observed on only one side of the reconciliation.
// For each bucket, the result is:
//
//	durable_total - retained_rows_in_day + retained_rows_in_requested_range
//
// Daily rollups intentionally provide whole-day values after raw retention has
// removed the source rows.  While rows are still retained, the raw subset keeps
// arbitrary half-open timestamp ranges exact.  If a legacy/import path has no
// durable bucket, the retained in-range rows are used as a compatibility
// fallback.
func (r *usageLogRepository) loadAccountDailyUsageStats(ctx context.Context, accountID int64, startTime, endTime time.Time) (result map[string]accountDailyUsageStats, err error) {
	result = make(map[string]accountDailyUsageStats)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	startDate, endDate, startBound, endBound := usageRollupDateRangeBounds(startTime, endTime)
	if !endTime.After(startTime) {
		return result, nil
	}

	// The raw CTE is restricted to the complete natural-day interval so the
	// date expression can be used for grouping without broadening the scan to
	// every retained row for the account.  The absolute timestamptz bounds are
	// sargable against idx_usage_logs_account_created_at (and its equivalent
	// partition-local indexes).
	query := `
		WITH durable AS (
			SELECT
				TO_CHAR(bucket_date, 'YYYY-MM-DD') AS bucket_date,
				total_requests,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				standard_cost,
				account_cost,
				user_cost,
				total_duration_ms,
				duration_count
			FROM usage_account_daily_rollups
			WHERE account_id = $1 AND bucket_date >= $2::date AND bucket_date < $3::date
		), raw AS (
			SELECT
				TO_CHAR((created_at AT TIME ZONE current_setting('TimeZone'))::date, 'YYYY-MM-DD') AS bucket_date,
				COUNT(*) AS all_requests,
				COUNT(*) FILTER (WHERE created_at >= $4 AND created_at < $5) AS range_requests,
				COALESCE(SUM(input_tokens), 0) AS all_input_tokens,
				COALESCE(SUM(input_tokens) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_input_tokens,
				COALESCE(SUM(output_tokens), 0) AS all_output_tokens,
				COALESCE(SUM(output_tokens) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_output_tokens,
				COALESCE(SUM(cache_creation_tokens), 0) AS all_cache_creation_tokens,
				COALESCE(SUM(cache_creation_tokens) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_cache_creation_tokens,
				COALESCE(SUM(cache_read_tokens), 0) AS all_cache_read_tokens,
				COALESCE(SUM(cache_read_tokens) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_cache_read_tokens,
				COALESCE(SUM(total_cost), 0) AS all_standard_cost,
				COALESCE(SUM(total_cost) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_standard_cost,
				COALESCE(SUM(COALESCE(account_stats_cost, total_cost, 0) * COALESCE(account_rate_multiplier, 1)), 0) AS all_account_cost,
				COALESCE(SUM(COALESCE(account_stats_cost, total_cost, 0) * COALESCE(account_rate_multiplier, 1)) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_account_cost,
				COALESCE(SUM(actual_cost), 0) AS all_user_cost,
				COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_user_cost,
				COALESCE(SUM(COALESCE(duration_ms, 0)), 0) AS all_duration_ms,
				COALESCE(SUM(COALESCE(duration_ms, 0)) FILTER (WHERE created_at >= $4 AND created_at < $5), 0) AS range_duration_ms,
				COUNT(duration_ms) AS all_duration_count,
				COUNT(duration_ms) FILTER (WHERE created_at >= $4 AND created_at < $5) AS range_duration_count
			FROM usage_logs
			WHERE account_id = $1 AND created_at >= $6 AND created_at < $7
			GROUP BY 1
		)
		SELECT
			COALESCE(d.bucket_date, raw.bucket_date) AS bucket_date,
			(d.bucket_date IS NOT NULL) AS has_durable,
			COALESCE(d.total_requests, 0) AS durable_requests,
			COALESCE(d.input_tokens, 0) AS durable_input_tokens,
			COALESCE(d.output_tokens, 0) AS durable_output_tokens,
			COALESCE(d.cache_creation_tokens, 0) AS durable_cache_creation_tokens,
			COALESCE(d.cache_read_tokens, 0) AS durable_cache_read_tokens,
			COALESCE(d.standard_cost, 0) AS durable_standard_cost,
			COALESCE(d.account_cost, 0) AS durable_account_cost,
			COALESCE(d.user_cost, 0) AS durable_user_cost,
			COALESCE(d.total_duration_ms, 0) AS durable_duration_ms,
			COALESCE(d.duration_count, 0) AS durable_duration_count,
			COALESCE(raw.all_requests, 0) AS all_requests,
			COALESCE(raw.range_requests, 0) AS range_requests,
			COALESCE(raw.all_input_tokens, 0) AS all_input_tokens,
			COALESCE(raw.range_input_tokens, 0) AS range_input_tokens,
			COALESCE(raw.all_output_tokens, 0) AS all_output_tokens,
			COALESCE(raw.range_output_tokens, 0) AS range_output_tokens,
			COALESCE(raw.all_cache_creation_tokens, 0) AS all_cache_creation_tokens,
			COALESCE(raw.range_cache_creation_tokens, 0) AS range_cache_creation_tokens,
			COALESCE(raw.all_cache_read_tokens, 0) AS all_cache_read_tokens,
			COALESCE(raw.range_cache_read_tokens, 0) AS range_cache_read_tokens,
			COALESCE(raw.all_standard_cost, 0) AS all_standard_cost,
			COALESCE(raw.range_standard_cost, 0) AS range_standard_cost,
			COALESCE(raw.all_account_cost, 0) AS all_account_cost,
			COALESCE(raw.range_account_cost, 0) AS range_account_cost,
			COALESCE(raw.all_user_cost, 0) AS all_user_cost,
			COALESCE(raw.range_user_cost, 0) AS range_user_cost,
			COALESCE(raw.all_duration_ms, 0) AS all_duration_ms,
			COALESCE(raw.range_duration_ms, 0) AS range_duration_ms,
			COALESCE(raw.all_duration_count, 0) AS all_duration_count,
			COALESCE(raw.range_duration_count, 0) AS range_duration_count
		FROM durable d
		FULL OUTER JOIN raw ON raw.bucket_date = d.bucket_date
		ORDER BY 1
	`

	rows, err := r.sql.QueryContext(ctx, query, accountID, startDate, endDate, startTime, endTime, startBound, endBound)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			result = nil
		}
	}()

	for rows.Next() {
		var (
			date       string
			hasDurable bool
			durable    accountDailyUsageStats
			rawAll     accountDailyUsageStats
			rawInRange accountDailyUsageStats
		)
		if err := rows.Scan(
			&date,
			&hasDurable,
			&durable.Requests,
			&durable.InputTokens,
			&durable.OutputTokens,
			&durable.CacheCreationTokens,
			&durable.CacheReadTokens,
			&durable.StandardCost,
			&durable.AccountCost,
			&durable.UserCost,
			&durable.TotalDurationMs,
			&durable.DurationCount,
			&rawAll.Requests,
			&rawInRange.Requests,
			&rawAll.InputTokens,
			&rawInRange.InputTokens,
			&rawAll.OutputTokens,
			&rawInRange.OutputTokens,
			&rawAll.CacheCreationTokens,
			&rawInRange.CacheCreationTokens,
			&rawAll.CacheReadTokens,
			&rawInRange.CacheReadTokens,
			&rawAll.StandardCost,
			&rawInRange.StandardCost,
			&rawAll.AccountCost,
			&rawInRange.AccountCost,
			&rawAll.UserCost,
			&rawInRange.UserCost,
			&rawAll.TotalDurationMs,
			&rawInRange.TotalDurationMs,
			&rawAll.DurationCount,
			&rawInRange.DurationCount,
		); err != nil {
			return nil, err
		}

		if hasDurable {
			stats := combineAccountDailyUsageStats(durable, accountDailyRawUsageStats{All: rawAll, InRange: rawInRange})
			if hasAccountDailyUsage(stats) {
				result[date] = stats
			}
			continue
		}
		// No durable bucket: only retained rows can be represented exactly.
		if hasAccountDailyUsage(rawInRange) {
			result[date] = rawInRange
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// GetUserStatsAggregated returns aggregated usage statistics for a user using database-level aggregation
func (r *usageLogRepository) GetUserStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	query := `
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(AVG(COALESCE(duration_ms, 0)), 0) as avg_duration_ms
		FROM usage_logs
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
	`

	var stats usagestats.UsageStats
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{userID, startTime, endTime},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	return &stats, nil
}

// GetAPIKeyStatsAggregated returns aggregated usage statistics for an API key using database-level aggregation
func (r *usageLogRepository) GetAPIKeyStatsAggregated(ctx context.Context, apiKeyID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	query := `
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(AVG(COALESCE(duration_ms, 0)), 0) as avg_duration_ms
		FROM usage_logs
		WHERE api_key_id = $1 AND created_at >= $2 AND created_at < $3
	`

	var stats usagestats.UsageStats
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{apiKeyID, startTime, endTime},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	return &stats, nil
}

// GetAccountStatsAggregated 使用持久化日汇总和保留日志聚合统计账号使用数据。
//
// loadAccountDailyUsageStats 在一个 SQL 快照中合并 durable rollup 与仍保留的
// usage_logs 行：已清理的日期直接从汇总表读取，未清理的日期仍保留原有的
// 时间范围精度。这样账号历史统计不会随着 usage_logs 清理而归零。
func (r *usageLogRepository) GetAccountStatsAggregated(ctx context.Context, accountID int64, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	buckets, err := r.loadAccountDailyUsageStats(ctx, accountID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	var stats usagestats.UsageStats
	for _, bucket := range buckets {
		stats.TotalRequests += bucket.Requests
		stats.TotalInputTokens += bucket.InputTokens
		stats.TotalOutputTokens += bucket.OutputTokens
		stats.TotalCacheCreationTokens += bucket.CacheCreationTokens
		stats.TotalCacheReadTokens += bucket.CacheReadTokens
		stats.TotalCacheTokens += bucket.CacheCreationTokens + bucket.CacheReadTokens
		stats.TotalCost += bucket.StandardCost
		stats.TotalActualCost += bucket.UserCost
		stats.AverageDurationMs += float64(bucket.TotalDurationMs)
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	// The previous query used AVG(COALESCE(duration_ms, 0)); its denominator is
	// therefore every request, including rows with a NULL duration.
	if stats.TotalRequests > 0 {
		stats.AverageDurationMs /= float64(stats.TotalRequests)
	} else {
		stats.AverageDurationMs = 0
	}
	return &stats, nil
}

// GetModelStatsAggregated 使用 SQL 聚合统计模型使用数据
// 性能优化：数据库层聚合计算，避免应用层循环统计
func (r *usageLogRepository) GetModelStatsAggregated(ctx context.Context, modelName string, startTime, endTime time.Time) (*usagestats.UsageStats, error) {
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(AVG(COALESCE(duration_ms, 0)), 0) as avg_duration_ms
		FROM usage_logs
		WHERE %s = $1 AND created_at >= $2 AND created_at < $3
	`, rawUsageLogModelColumn)

	var stats usagestats.UsageStats
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{modelName, startTime, endTime},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	return &stats, nil
}

// GetDailyStatsAggregated 使用 SQL 聚合统计用户的每日使用数据
// 性能优化：使用 GROUP BY 在数据库层按日期分组聚合，避免应用层循环分组统计
func (r *usageLogRepository) GetDailyStatsAggregated(ctx context.Context, userID int64, startTime, endTime time.Time) (result []map[string]any, err error) {
	tzName := resolveUsageStatsTimezone()
	query := `
		SELECT
			-- 使用应用时区分组，避免数据库会话时区导致日边界偏移。
			TO_CHAR(created_at AT TIME ZONE $4, 'YYYY-MM-DD') as date,
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(AVG(COALESCE(duration_ms, 0)), 0) as avg_duration_ms
		FROM usage_logs
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
		GROUP BY 1
		ORDER BY 1
	`

	rows, err := r.sql.QueryContext(ctx, query, userID, startTime, endTime, tzName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			result = nil
		}
	}()

	result = make([]map[string]any, 0)
	for rows.Next() {
		var (
			date              string
			totalRequests     int64
			totalInputTokens  int64
			totalOutputTokens int64
			totalCacheTokens  int64
			totalCost         float64
			totalActualCost   float64
			avgDurationMs     float64
		)
		if err = rows.Scan(
			&date,
			&totalRequests,
			&totalInputTokens,
			&totalOutputTokens,
			&totalCacheTokens,
			&totalCost,
			&totalActualCost,
			&avgDurationMs,
		); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"date":                date,
			"total_requests":      totalRequests,
			"total_input_tokens":  totalInputTokens,
			"total_output_tokens": totalOutputTokens,
			"total_cache_tokens":  totalCacheTokens,
			"total_tokens":        totalInputTokens + totalOutputTokens + totalCacheTokens,
			"total_cost":          totalCost,
			"total_actual_cost":   totalActualCost,
			"average_duration_ms": avgDurationMs,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// resolveUsageStatsTimezone 获取用于 SQL 分组的时区名称。
// 优先使用应用初始化的时区，其次尝试读取 TZ 环境变量，最后回落为 UTC。
func resolveUsageStatsTimezone() string {
	tzName := timezone.Name()
	if tzName != "" && tzName != "Local" {
		return tzName
	}
	if envTZ := strings.TrimSpace(os.Getenv("TZ")); envTZ != "" {
		return envTZ
	}
	return "UTC"
}

// GetAccountTodayStats 获取账号今日统计
func (r *usageLogRepository) GetAccountTodayStats(ctx context.Context, accountID int64) (*usagestats.AccountStats, error) {
	today := timezone.Today()

	query := `
		SELECT
			COALESCE(SUM(total_requests), 0) as requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as tokens,
			COALESCE(SUM(account_cost), 0) as cost,
			COALESCE(SUM(standard_cost), 0) as standard_cost,
			COALESCE(SUM(user_cost), 0) as user_cost
		FROM usage_account_daily_rollups
		WHERE account_id = $1 AND bucket_date = $2::date
	`

	stats := &usagestats.AccountStats{}
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{accountID, usageRollupDate(today)},
		&stats.Requests,
		&stats.Tokens,
		&stats.Cost,
		&stats.StandardCost,
		&stats.UserCost,
	); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetAccountTodayStatsBatch 批量读取账号今日统计。
//
// This method intentionally lives outside UsageLogRepository's required
// interface so older in-memory/test implementations remain source compatible;
// AccountUsageService discovers it through a narrow optional interface.  The
// data comes from the durable daily rollup, therefore deleting usage_logs rows
// cannot reset the values shown in the admin account table.
func (r *usageLogRepository) GetAccountTodayStatsBatch(ctx context.Context, accountIDs []int64) (map[int64]*usagestats.AccountStats, error) {
	result := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT
			account_id,
			COALESCE(total_requests, 0) AS requests,
			COALESCE(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens, 0) AS tokens,
			COALESCE(account_cost, 0) AS cost,
			COALESCE(standard_cost, 0) AS standard_cost,
			COALESCE(user_cost, 0) AS user_cost
		FROM usage_account_daily_rollups
		WHERE account_id = ANY($1) AND bucket_date = $2::date
	`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(accountIDs), usageRollupDate(timezone.Today()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		stats := &usagestats.AccountStats{}
		if err := rows.Scan(
			&accountID,
			&stats.Requests,
			&stats.Tokens,
			&stats.Cost,
			&stats.StandardCost,
			&stats.UserCost,
		); err != nil {
			return nil, err
		}
		result[accountID] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, accountID := range accountIDs {
		if _, ok := result[accountID]; !ok {
			result[accountID] = &usagestats.AccountStats{}
		}
	}
	return result, nil
}

// GetAccountWindowStats 获取账号时间窗口内的统计
func (r *usageLogRepository) GetAccountWindowStats(ctx context.Context, accountID int64, startTime time.Time) (*usagestats.AccountStats, error) {
	query := `
		SELECT
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as tokens,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as cost,
			COALESCE(SUM(total_cost), 0) as standard_cost,
			COALESCE(SUM(actual_cost), 0) as user_cost
		FROM usage_logs
		WHERE account_id = $1 AND created_at >= $2
	`

	stats := &usagestats.AccountStats{}
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{accountID, startTime},
		&stats.Requests,
		&stats.Tokens,
		&stats.Cost,
		&stats.StandardCost,
		&stats.UserCost,
	); err != nil {
		return nil, err
	}
	return stats, nil
}

// GetAccountWindowStatsBatch 批量获取同一窗口起点下多个账号的统计数据。
// 返回 map[accountID]*AccountStats，未命中的账号会返回零值统计，便于上层直接复用。
func (r *usageLogRepository) GetAccountWindowStatsBatch(ctx context.Context, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error) {
	result := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT
			account_id,
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as tokens,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as cost,
			COALESCE(SUM(total_cost), 0) as standard_cost,
			COALESCE(SUM(actual_cost), 0) as user_cost
		FROM usage_logs
		WHERE account_id = ANY($1) AND created_at >= $2
		GROUP BY account_id
	`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(accountIDs), startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		stats := &usagestats.AccountStats{}
		if err := rows.Scan(
			&accountID,
			&stats.Requests,
			&stats.Tokens,
			&stats.Cost,
			&stats.StandardCost,
			&stats.UserCost,
		); err != nil {
			return nil, err
		}
		result[accountID] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, accountID := range accountIDs {
		if _, ok := result[accountID]; !ok {
			result[accountID] = &usagestats.AccountStats{}
		}
	}
	return result, nil
}

// GetAccountLifetimeStatsBatch 批量获取账号从创建时间到当前时刻的累计统计。
//
// 通过 accounts.created_at 约束每个账号各自的统计起点，而不是使用一个公共窗口，
// 这样同一批次中创建时间不同的账号也能得到准确结果。
func (r *usageLogRepository) GetAccountLifetimeStatsBatch(ctx context.Context, accountIDs []int64) (map[int64]*usagestats.AccountStats, error) {
	result := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT
			a.id AS account_id,
			COALESCE(SUM(r.lifetime_requests), 0) AS requests,
			COALESCE(SUM(r.lifetime_input_tokens + r.lifetime_output_tokens + r.lifetime_cache_creation_tokens + r.lifetime_cache_read_tokens), 0) AS tokens,
			COALESCE(SUM(r.lifetime_account_cost), 0) AS cost,
			COALESCE(SUM(r.lifetime_standard_cost), 0) AS standard_cost,
			COALESCE(SUM(r.lifetime_user_cost), 0) AS user_cost
		FROM accounts a
		LEFT JOIN usage_account_daily_rollups r
			ON r.account_id = a.id
			-- Lifetime counters are filtered against the cutoff captured by the
			-- insert trigger/backfill.  The date guard also keeps a future
			-- natural-day bucket out of the result, matching the legacy
			-- created_at <= NOW() query.
			AND r.bucket_date <= $2::date
		WHERE a.id = ANY($1)
		GROUP BY a.id
	`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(accountIDs), usageRollupDate(timezone.Today()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		stats := &usagestats.AccountStats{}
		if err := rows.Scan(
			&accountID,
			&stats.Requests,
			&stats.Tokens,
			&stats.Cost,
			&stats.StandardCost,
			&stats.UserCost,
		); err != nil {
			return nil, err
		}
		result[accountID] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, accountID := range accountIDs {
		if _, ok := result[accountID]; !ok {
			result[accountID] = &usagestats.AccountStats{}
		}
	}
	return result, nil
}

// GetGeminiUsageTotalsBatch 批量聚合 Gemini 账号在窗口内的 Pro/Flash 请求与用量。
// 模型分类规则与 service.geminiModelClassFromName 一致：model 包含 flash/lite 视为 flash，其余视为 pro。
func (r *usageLogRepository) GetGeminiUsageTotalsBatch(ctx context.Context, accountIDs []int64, startTime, endTime time.Time) (map[int64]service.GeminiUsageTotals, error) {
	result := make(map[int64]service.GeminiUsageTotals, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT
			account_id,
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(model, '')) LIKE '%flash%' OR LOWER(COALESCE(model, '')) LIKE '%lite%' THEN 1 ELSE 0 END), 0) AS flash_requests,
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(model, '')) LIKE '%flash%' OR LOWER(COALESCE(model, '')) LIKE '%lite%' THEN 0 ELSE 1 END), 0) AS pro_requests,
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(model, '')) LIKE '%flash%' OR LOWER(COALESCE(model, '')) LIKE '%lite%' THEN (input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) ELSE 0 END), 0) AS flash_tokens,
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(model, '')) LIKE '%flash%' OR LOWER(COALESCE(model, '')) LIKE '%lite%' THEN 0 ELSE (input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens) END), 0) AS pro_tokens,
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(model, '')) LIKE '%flash%' OR LOWER(COALESCE(model, '')) LIKE '%lite%' THEN actual_cost ELSE 0 END), 0) AS flash_cost,
			COALESCE(SUM(CASE WHEN LOWER(COALESCE(model, '')) LIKE '%flash%' OR LOWER(COALESCE(model, '')) LIKE '%lite%' THEN 0 ELSE actual_cost END), 0) AS pro_cost
		FROM usage_logs
		WHERE account_id = ANY($1) AND created_at >= $2 AND created_at < $3
		GROUP BY account_id
	`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(accountIDs), startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var accountID int64
		var totals service.GeminiUsageTotals
		if err := rows.Scan(
			&accountID,
			&totals.FlashRequests,
			&totals.ProRequests,
			&totals.FlashTokens,
			&totals.ProTokens,
			&totals.FlashCost,
			&totals.ProCost,
		); err != nil {
			return nil, err
		}
		result[accountID] = totals
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, accountID := range accountIDs {
		if _, ok := result[accountID]; !ok {
			result[accountID] = service.GeminiUsageTotals{}
		}
	}
	return result, nil
}

// UsageStats represents usage statistics
type UsageStats = usagestats.UsageStats

// BatchUserUsageStats represents usage stats for a single user
type BatchUserUsageStats = usagestats.BatchUserUsageStats

// PlatformUsage represents per-platform usage breakdown
type PlatformUsage = usagestats.PlatformUsage

func normalizePositiveInt64IDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// GetBatchUserUsageStats gets today and total actual_cost for multiple users within a time range.
// If startTime is zero, defaults to 30 days ago.
func (r *usageLogRepository) GetBatchUserUsageStats(ctx context.Context, userIDs []int64, startTime, endTime time.Time) (map[int64]*BatchUserUsageStats, error) {
	result := make(map[int64]*BatchUserUsageStats)
	normalizedUserIDs := normalizePositiveInt64IDs(userIDs)
	if len(normalizedUserIDs) == 0 {
		return result, nil
	}

	// 默认最近 30 天
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	for _, id := range normalizedUserIDs {
		result[id] = &BatchUserUsageStats{UserID: id}
	}

	// GROUP BY (user_id, effective_platform) 一次查询同时得到总值与按平台拆分。
	// 应用层把同一 user_id 的多行累加为总值，并把非空 platform 行收集到 ByPlatform。
	query := `
		SELECT
			ul.user_id,
			` + usageLogEffectivePlatformExpr + ` as platform,
			COALESCE(SUM(ul.actual_cost) FILTER (WHERE ul.created_at >= $2 AND ul.created_at < $3), 0) as total_cost,
			COALESCE(SUM(ul.actual_cost) FILTER (WHERE ul.created_at >= $4), 0) as today_cost
		FROM usage_logs ul
		LEFT JOIN groups g ON g.id = ul.group_id
		LEFT JOIN accounts a ON a.id = ul.account_id
		WHERE ul.user_id = ANY($1)
		  AND ul.created_at >= LEAST($2, $4)
		  AND ` + usageLogSuccessFilterUL + `
		GROUP BY ul.user_id, ` + usageLogEffectivePlatformExpr + `
	`
	today := timezone.Today()
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(normalizedUserIDs), startTime, endTime, today)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var userID int64
		var platform sql.NullString
		var total float64
		var todayTotal float64
		if err := rows.Scan(&userID, &platform, &total, &todayTotal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		stats, ok := result[userID]
		if !ok {
			continue
		}
		stats.TotalActualCost += total
		stats.TodayActualCost += todayTotal
		if platform.Valid && platform.String != "" {
			stats.ByPlatform = append(stats.ByPlatform, PlatformUsage{
				Platform:        platform.String,
				TotalActualCost: total,
				TodayActualCost: todayTotal,
			})
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// BatchAPIKeyUsageStats represents usage stats for a single API key
type BatchAPIKeyUsageStats = usagestats.BatchAPIKeyUsageStats

// GetBatchAPIKeyUsageStats gets today and total actual_cost for multiple API keys within a time range.
// If startTime is zero, defaults to 30 days ago.
func (r *usageLogRepository) GetBatchAPIKeyUsageStats(ctx context.Context, apiKeyIDs []int64, startTime, endTime time.Time) (map[int64]*BatchAPIKeyUsageStats, error) {
	result := make(map[int64]*BatchAPIKeyUsageStats)
	normalizedAPIKeyIDs := normalizePositiveInt64IDs(apiKeyIDs)
	if len(normalizedAPIKeyIDs) == 0 {
		return result, nil
	}

	// 默认最近 30 天
	if startTime.IsZero() {
		startTime = time.Now().AddDate(0, 0, -30)
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	for _, id := range normalizedAPIKeyIDs {
		result[id] = &BatchAPIKeyUsageStats{APIKeyID: id}
	}

	query := `
		SELECT
			api_key_id,
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $2 AND created_at < $3), 0) as total_cost,
			COALESCE(SUM(actual_cost) FILTER (WHERE created_at >= $4), 0) as today_cost
		FROM usage_logs
		WHERE api_key_id = ANY($1)
		  AND created_at >= LEAST($2, $4)
		GROUP BY api_key_id
	`
	today := timezone.Today()
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(normalizedAPIKeyIDs), startTime, endTime, today)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var apiKeyID int64
		var total float64
		var todayTotal float64
		if err := rows.Scan(&apiKeyID, &total, &todayTotal); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if stats, ok := result[apiKeyID]; ok {
			stats.TotalActualCost = total
			stats.TodayActualCost = todayTotal
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// resolveEndpointColumn maps endpoint type to the corresponding DB column name.
func resolveEndpointColumn(endpointType string) string {
	switch endpointType {
	case "upstream":
		return "ul.upstream_endpoint"
	case "path":
		return "ul.inbound_endpoint || ' -> ' || ul.upstream_endpoint"
	default:
		return "ul.inbound_endpoint"
	}
}

// GetGlobalStats gets usage statistics for all users within a time range
func (r *usageLogRepository) GetGlobalStats(ctx context.Context, startTime, endTime time.Time) (*UsageStats, error) {
	query := `
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens + cache_read_tokens), 0) as total_cache_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
	`

	stats := &UsageStats{}
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{startTime, endTime},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens
	return stats, nil
}

// GetStatsWithFilters gets usage statistics with optional filters
func (r *usageLogRepository) GetStatsWithFilters(ctx context.Context, filters UsageLogFilters) (*UsageStats, error) {
	conditions := make([]string, 0, 9)
	args := make([]any, 0, 9)

	if filters.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("user_id = $%d", len(args)+1))
		args = append(args, filters.UserID)
	}
	if filters.APIKeyID > 0 {
		conditions = append(conditions, fmt.Sprintf("api_key_id = $%d", len(args)+1))
		args = append(args, filters.APIKeyID)
	}
	if filters.AccountID > 0 {
		conditions = append(conditions, fmt.Sprintf("account_id = $%d", len(args)+1))
		args = append(args, filters.AccountID)
	}
	if filters.GroupID > 0 {
		conditions = append(conditions, fmt.Sprintf("group_id = $%d", len(args)+1))
		args = append(args, filters.GroupID)
	}
	conditions, args = appendUsageLogModelWhereCondition(conditions, args, filters.Model, filters.ModelFilterSource)
	conditions, args = appendRequestTypeOrStreamWhereCondition(conditions, args, filters.RequestType, filters.Stream)
	if filters.BillingType != nil {
		conditions = append(conditions, fmt.Sprintf("billing_type = $%d", len(args)+1))
		args = append(args, int16(*filters.BillingType))
	}
	conditions, args = appendUsageLogBillingModeWhereCondition(conditions, args, filters.BillingMode)
	if filters.UpstreamModelMismatch != nil {
		conditions = append(conditions, upstreamModelMismatchCondition("upstream_model_mismatch", *filters.UpstreamModelMismatch))
	}
	if filters.StartTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at >= $%d", len(args)+1))
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		conditions = append(conditions, fmt.Sprintf("created_at < $%d", len(args)+1))
		args = append(args, *filters.EndTime)
	}

	query := fmt.Sprintf(`
		WITH scoped AS (
			SELECT
				COALESCE(NULLIF(TRIM(inbound_endpoint), ''), 'unknown') AS inbound_endpoint,
				COALESCE(NULLIF(TRIM(upstream_endpoint), ''), 'unknown') AS upstream_endpoint,
				input_tokens,
				output_tokens,
				cache_creation_tokens,
				cache_read_tokens,
				total_cost,
				actual_cost,
				COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1) AS account_cost,
				duration_ms
			FROM usage_logs
			%s
		)
		SELECT
			GROUPING(inbound_endpoint) AS inbound_grouped,
			GROUPING(upstream_endpoint) AS upstream_grouped,
			inbound_endpoint,
			upstream_endpoint,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens), 0) AS input_tokens,
			COALESCE(SUM(output_tokens), 0) AS output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS cost,
			COALESCE(SUM(actual_cost), 0) AS actual_cost,
			COALESCE(SUM(account_cost), 0) AS account_cost,
			COALESCE(AVG(duration_ms), 0) AS avg_duration_ms
		FROM scoped
		GROUP BY GROUPING SETS (
			(),
			(inbound_endpoint),
			(upstream_endpoint),
			(inbound_endpoint, upstream_endpoint)
		)
	`, buildWhere(conditions))

	stats := &UsageStats{}
	var totalAccountCost float64
	useAccountCostForEndpoint := filters.AccountID > 0 && filters.UserID == 0 && filters.APIKeyID == 0
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			inboundGrouped, upstreamGrouped                                      int
			inboundEndpoint, upstreamEndpoint                                    sql.NullString
			requests, inputTokens, outputTokens, cacheCreationTokens, cacheReads int64
			cost, actualCost, accountCost, averageDurationMs                     float64
		)
		if err := rows.Scan(
			&inboundGrouped,
			&upstreamGrouped,
			&inboundEndpoint,
			&upstreamEndpoint,
			&requests,
			&inputTokens,
			&outputTokens,
			&cacheCreationTokens,
			&cacheReads,
			&cost,
			&actualCost,
			&accountCost,
			&averageDurationMs,
		); err != nil {
			return nil, err
		}

		totalTokens := inputTokens + outputTokens + cacheCreationTokens + cacheReads
		endpointActualCost := actualCost
		if useAccountCostForEndpoint {
			endpointActualCost = accountCost
		}

		switch {
		case inboundGrouped == 1 && upstreamGrouped == 1:
			stats.TotalRequests = requests
			stats.TotalInputTokens = inputTokens
			stats.TotalOutputTokens = outputTokens
			stats.TotalCacheCreationTokens = cacheCreationTokens
			stats.TotalCacheReadTokens = cacheReads
			stats.TotalCacheTokens = cacheCreationTokens + cacheReads
			stats.TotalCost = cost
			stats.TotalActualCost = actualCost
			totalAccountCost = accountCost
			stats.AverageDurationMs = averageDurationMs
		case inboundGrouped == 0 && upstreamGrouped == 1:
			stats.Endpoints = append(stats.Endpoints, EndpointStat{
				Endpoint: inboundEndpoint.String, Requests: requests, TotalTokens: totalTokens,
				Cost: cost, ActualCost: endpointActualCost,
			})
		case inboundGrouped == 1 && upstreamGrouped == 0:
			stats.UpstreamEndpoints = append(stats.UpstreamEndpoints, EndpointStat{
				Endpoint: upstreamEndpoint.String, Requests: requests, TotalTokens: totalTokens,
				Cost: cost, ActualCost: endpointActualCost,
			})
		case inboundGrouped == 0 && upstreamGrouped == 0:
			stats.EndpointPaths = append(stats.EndpointPaths, EndpointStat{
				Endpoint: inboundEndpoint.String + " -> " + upstreamEndpoint.String,
				Requests: requests, TotalTokens: totalTokens, Cost: cost, ActualCost: endpointActualCost,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sortEndpointStats := func(values []EndpointStat) {
		sort.Slice(values, func(i, j int) bool {
			if values[i].Requests != values[j].Requests {
				return values[i].Requests > values[j].Requests
			}
			return values[i].Endpoint < values[j].Endpoint
		})
	}
	sortEndpointStats(stats.Endpoints)
	sortEndpointStats(stats.UpstreamEndpoints)
	sortEndpointStats(stats.EndpointPaths)

	stats.TotalAccountCost = &totalAccountCost
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheTokens

	return stats, nil
}

// AccountUsageHistory represents daily usage history for an account
type AccountUsageHistory = usagestats.AccountUsageHistory

// AccountUsageSummary represents summary statistics for an account
type AccountUsageSummary = usagestats.AccountUsageSummary

// AccountUsageStatsResponse represents the full usage statistics response for an account
type AccountUsageStatsResponse = usagestats.AccountUsageStatsResponse

// EndpointStat represents endpoint usage statistics row.
type EndpointStat = usagestats.EndpointStat

func (r *usageLogRepository) getEndpointStatsByColumnWithFilters(ctx context.Context, endpointColumn string, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, modelSource string, requestType *int16, stream *bool, billingType *int8, billingMode string) (results []EndpointStat, err error) {
	actualCostExpr := "COALESCE(SUM(actual_cost), 0) as actual_cost"
	if accountID > 0 && userID == 0 && apiKeyID == 0 {
		actualCostExpr = "COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as actual_cost"
	}

	query := fmt.Sprintf(`
		SELECT
			COALESCE(NULLIF(TRIM(%s), ''), 'unknown') AS endpoint,
			COUNT(*) AS requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) AS total_tokens,
			COALESCE(SUM(total_cost), 0) as cost,
			%s
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
	`, endpointColumn, actualCostExpr)

	args := []any{startTime, endTime}
	if userID > 0 {
		query += fmt.Sprintf(" AND user_id = $%d", len(args)+1)
		args = append(args, userID)
	}
	if apiKeyID > 0 {
		query += fmt.Sprintf(" AND api_key_id = $%d", len(args)+1)
		args = append(args, apiKeyID)
	}
	if accountID > 0 {
		query += fmt.Sprintf(" AND account_id = $%d", len(args)+1)
		args = append(args, accountID)
	}
	if groupID > 0 {
		query += fmt.Sprintf(" AND group_id = $%d", len(args)+1)
		args = append(args, groupID)
	}
	query, args = appendUsageLogModelQueryFilter(query, args, model, modelSource)
	query, args = appendRequestTypeOrStreamQueryFilter(query, args, requestType, stream)
	if billingType != nil {
		query += fmt.Sprintf(" AND billing_type = $%d", len(args)+1)
		args = append(args, int16(*billingType))
	}
	query, args = appendUsageLogBillingModeQueryFilter(query, args, billingMode, "")
	query += " GROUP BY endpoint ORDER BY requests DESC"

	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
			results = nil
		}
	}()

	results = make([]EndpointStat, 0)
	for rows.Next() {
		var row EndpointStat
		if err := rows.Scan(&row.Endpoint, &row.Requests, &row.TotalTokens, &row.Cost, &row.ActualCost); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// GetEndpointStatsWithFilters returns inbound endpoint statistics with optional filters.
func (r *usageLogRepository) GetEndpointStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]EndpointStat, error) {
	return r.getEndpointStatsByColumnWithFilters(ctx, "inbound_endpoint", startTime, endTime, userID, apiKeyID, accountID, groupID, model, "", requestType, stream, billingType, "")
}

// GetUpstreamEndpointStatsWithFilters returns upstream endpoint statistics with optional filters.
func (r *usageLogRepository) GetUpstreamEndpointStatsWithFilters(ctx context.Context, startTime, endTime time.Time, userID, apiKeyID, accountID, groupID int64, model string, requestType *int16, stream *bool, billingType *int8) ([]EndpointStat, error) {
	return r.getEndpointStatsByColumnWithFilters(ctx, "upstream_endpoint", startTime, endTime, userID, apiKeyID, accountID, groupID, model, "", requestType, stream, billingType, "")
}

// GetAccountUsageStats returns comprehensive usage statistics for an account over a time range
func (r *usageLogRepository) GetAccountUsageStats(ctx context.Context, accountID int64, startTime, endTime time.Time) (resp *AccountUsageStatsResponse, err error) {
	daysCount := int(endTime.Sub(startTime).Hours()/24) + 1
	if daysCount <= 0 {
		daysCount = 30
	}
	buckets, err := r.loadAccountDailyUsageStats(ctx, accountID, startTime, endTime)
	if err != nil {
		return nil, err
	}

	dates := make([]string, 0, len(buckets))
	for date := range buckets {
		dates = append(dates, date)
	}
	sort.Strings(dates)

	history := make([]AccountUsageHistory, 0, len(dates))
	var totalDurationMs, durationCount int64
	for _, date := range dates {
		bucket := buckets[date]
		t, _ := time.Parse("2006-01-02", date)
		history = append(history, AccountUsageHistory{
			Date:       date,
			Label:      t.Format("01/02"),
			Requests:   bucket.Requests,
			Tokens:     bucket.InputTokens + bucket.OutputTokens + bucket.CacheCreationTokens + bucket.CacheReadTokens,
			Cost:       bucket.StandardCost,
			ActualCost: bucket.AccountCost,
			UserCost:   bucket.UserCost,
		})
		totalDurationMs += bucket.TotalDurationMs
		durationCount += bucket.DurationCount
	}

	var totalAccountCost, totalUserCost, totalStandardCost float64
	var totalRequests, totalTokens int64
	var highestCostDay, highestRequestDay *AccountUsageHistory

	for i := range history {
		h := &history[i]
		totalAccountCost += h.ActualCost
		totalUserCost += h.UserCost
		totalStandardCost += h.Cost
		totalRequests += h.Requests
		totalTokens += h.Tokens

		if highestCostDay == nil || h.ActualCost > highestCostDay.ActualCost {
			highestCostDay = h
		}
		if highestRequestDay == nil || h.Requests > highestRequestDay.Requests {
			highestRequestDay = h
		}
	}

	actualDaysUsed := len(history)
	if actualDaysUsed == 0 {
		actualDaysUsed = 1
	}

	var avgDuration float64
	if durationCount > 0 {
		avgDuration = float64(totalDurationMs) / float64(durationCount)
	}

	summary := AccountUsageSummary{
		Days:              daysCount,
		ActualDaysUsed:    actualDaysUsed,
		TotalCost:         totalAccountCost,
		TotalUserCost:     totalUserCost,
		TotalStandardCost: totalStandardCost,
		TotalRequests:     totalRequests,
		TotalTokens:       totalTokens,
		AvgDailyCost:      totalAccountCost / float64(actualDaysUsed),
		AvgDailyUserCost:  totalUserCost / float64(actualDaysUsed),
		AvgDailyRequests:  float64(totalRequests) / float64(actualDaysUsed),
		AvgDailyTokens:    float64(totalTokens) / float64(actualDaysUsed),
		AvgDurationMs:     avgDuration,
	}

	todayStr := usageRollupDate(timezone.Now())
	for i := range history {
		if history[i].Date == todayStr {
			summary.Today = &struct {
				Date     string  `json:"date"`
				Cost     float64 `json:"cost"`
				UserCost float64 `json:"user_cost"`
				Requests int64   `json:"requests"`
				Tokens   int64   `json:"tokens"`
			}{
				Date:     history[i].Date,
				Cost:     history[i].ActualCost,
				UserCost: history[i].UserCost,
				Requests: history[i].Requests,
				Tokens:   history[i].Tokens,
			}
			break
		}
	}

	if highestCostDay != nil {
		summary.HighestCostDay = &struct {
			Date     string  `json:"date"`
			Label    string  `json:"label"`
			Cost     float64 `json:"cost"`
			UserCost float64 `json:"user_cost"`
			Requests int64   `json:"requests"`
		}{
			Date:     highestCostDay.Date,
			Label:    highestCostDay.Label,
			Cost:     highestCostDay.ActualCost,
			UserCost: highestCostDay.UserCost,
			Requests: highestCostDay.Requests,
		}
	}

	if highestRequestDay != nil {
		summary.HighestRequestDay = &struct {
			Date     string  `json:"date"`
			Label    string  `json:"label"`
			Requests int64   `json:"requests"`
			Cost     float64 `json:"cost"`
			UserCost float64 `json:"user_cost"`
		}{
			Date:     highestRequestDay.Date,
			Label:    highestRequestDay.Label,
			Requests: highestRequestDay.Requests,
			Cost:     highestRequestDay.ActualCost,
			UserCost: highestRequestDay.UserCost,
		}
	}

	models, err := r.GetModelStatsWithFilters(ctx, startTime, endTime, 0, 0, accountID, 0, nil, nil, nil)
	if err != nil {
		models = []ModelStat{}
	}
	endpoints, endpointErr := r.GetEndpointStatsWithFilters(ctx, startTime, endTime, 0, 0, accountID, 0, "", nil, nil, nil)
	if endpointErr != nil {
		logger.LegacyPrintf("repository.usage_log", "GetEndpointStatsWithFilters failed in GetAccountUsageStats: %v", endpointErr)
		endpoints = []EndpointStat{}
	}
	upstreamEndpoints, upstreamEndpointErr := r.GetUpstreamEndpointStatsWithFilters(ctx, startTime, endTime, 0, 0, accountID, 0, "", nil, nil, nil)
	if upstreamEndpointErr != nil {
		logger.LegacyPrintf("repository.usage_log", "GetUpstreamEndpointStatsWithFilters failed in GetAccountUsageStats: %v", upstreamEndpointErr)
		upstreamEndpoints = []EndpointStat{}
	}

	resp = &AccountUsageStatsResponse{
		History:           history,
		Summary:           summary,
		Models:            models,
		Endpoints:         endpoints,
		UpstreamEndpoints: upstreamEndpoints,
	}
	return resp, nil
}
