//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func accountAdminTestContext() context.Context {
	return context.WithValue(context.Background(), ctxkey.AccountAdminID, int64(41))
}

func TestUsageLogAccountAdminScopeHelpersPreserveUnscopedQueries(t *testing.T) {
	ctx := accountAdminTestContext()
	conditions, args := appendUsageLogAccountAdminScope(ctx, []string{"account_id = $1"}, []any{int64(7)}, "account_id")
	require.Equal(t, []string{
		"account_id = $1",
		"account_id IN (SELECT id FROM accounts WHERE account_admin_id = $2 AND deleted_at IS NULL)",
	}, conditions)
	require.Equal(t, []any{int64(7), int64(41)}, args)

	query := "SELECT account_id FROM usage_logs WHERE account_id = ANY($1)\n\t\tGROUP BY account_id"
	scopedQuery, scopedArgs := appendUsageLogAccountAdminQueryScopeBefore(ctx, query, []any{"ids"}, "account_id", "\n\t\tGROUP BY account_id")
	require.Equal(t,
		"SELECT account_id FROM usage_logs WHERE account_id = ANY($1) AND account_id IN (SELECT id FROM accounts WHERE account_admin_id = $2 AND deleted_at IS NULL)\n\t\tGROUP BY account_id",
		scopedQuery,
	)
	require.Equal(t, []any{"ids", int64(41)}, scopedArgs)

	unscopedQuery, unscopedArgs := appendUsageLogAccountAdminQueryScope(context.Background(), query, []any{"ids"}, "account_id")
	require.Equal(t, query, unscopedQuery)
	require.Equal(t, []any{"ids"}, unscopedArgs)
}

func TestGetAccountWindowStatsAppliesAccountAdminScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("AND account_id IN (SELECT id FROM accounts WHERE account_admin_id = $3 AND deleted_at IS NULL)")).
		WithArgs(int64(7), start, int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"requests", "tokens", "cost", "standard_cost", "user_cost"}).AddRow(2, 30, 1.5, 2.0, 1.0))

	repo := &usageLogRepository{sql: db}
	stats, err := repo.GetAccountWindowStats(accountAdminTestContext(), 7, start)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.Requests)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetAccountWindowStatsBatchPlacesScopeBeforeGroupBy(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("AND account_id IN (SELECT id FROM accounts WHERE account_admin_id = $3 AND deleted_at IS NULL)\n\t\tGROUP BY account_id")).
		WithArgs(sqlmock.AnyArg(), start, int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "requests", "tokens", "cost", "standard_cost", "user_cost"}))

	repo := &usageLogRepository{sql: db}
	stats, err := repo.GetAccountWindowStatsBatch(accountAdminTestContext(), []int64{7, 8}, start)
	require.NoError(t, err)
	require.Len(t, stats, 2)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLoadAccountDailyUsageStatsScopesRollupAndRawCTEs(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	startDate, endDate, startBound, endBound := usageRollupDateRangeBounds(start, end)
	ownerPredicate := "account_id IN (SELECT id FROM accounts WHERE account_admin_id = $8 AND deleted_at IS NULL)"
	mock.ExpectQuery(regexp.QuoteMeta(ownerPredicate)).
		WithArgs(int64(7), startDate, endDate, start, end, startBound, endBound, int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{
			"bucket_date", "has_durable", "durable_requests", "durable_input_tokens", "durable_output_tokens",
			"durable_cache_creation_tokens", "durable_cache_read_tokens", "durable_standard_cost", "durable_account_cost",
			"durable_user_cost", "durable_duration_ms", "durable_duration_count", "all_requests", "range_requests",
			"all_input_tokens", "range_input_tokens", "all_output_tokens", "range_output_tokens", "all_cache_creation_tokens",
			"range_cache_creation_tokens", "all_cache_read_tokens", "range_cache_read_tokens", "all_standard_cost",
			"range_standard_cost", "all_account_cost", "range_account_cost", "all_user_cost", "range_user_cost",
			"all_duration_ms", "range_duration_ms", "all_duration_count", "range_duration_count",
		}))

	repo := &usageLogRepository{sql: db}
	stats, err := repo.loadAccountDailyUsageStats(accountAdminTestContext(), 7, start, end)
	require.NoError(t, err)
	require.Empty(t, stats)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountAdminScopeFallbackPreservesTrailingClauses(t *testing.T) {
	query := "SELECT account_id FROM usage_logs WHERE account_id = $1 GROUP BY account_id ORDER BY account_id;"
	scopedQuery, args := appendUsageLogAccountAdminQueryScopeBefore(
		accountAdminTestContext(), query, []any{int64(7)}, "account_id", "missing marker",
	)
	require.Equal(t,
		"SELECT account_id FROM usage_logs WHERE account_id = $1 AND account_id IN (SELECT id FROM accounts WHERE account_admin_id = $2 AND deleted_at IS NULL) GROUP BY account_id ORDER BY account_id;",
		scopedQuery,
	)
	require.Equal(t, []any{int64(7), int64(41)}, args)
}

func TestAggregatedUsageQueriesApplyAccountAdminScope(t *testing.T) {
	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	aggregateRows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens", "total_cache_tokens",
			"total_cache_creation_tokens", "total_cache_read_tokens", "total_cost", "total_actual_cost", "avg_duration_ms",
		}).AddRow(1, 2, 3, 4, 5, 6, 0.7, 0.8, 9.0)
	}

	t.Run("user", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $4 AND deleted_at IS NULL)")).
			WithArgs(int64(9), start, end, int64(41)).WillReturnRows(aggregateRows())
		_, err := repo.GetUserStatsAggregated(accountAdminTestContext(), 9, start, end)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("api key", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $4 AND deleted_at IS NULL)")).
			WithArgs(int64(3), start, end, int64(41)).WillReturnRows(aggregateRows())
		_, err := repo.GetAPIKeyStatsAggregated(accountAdminTestContext(), 3, start, end)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("model", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $4 AND deleted_at IS NULL)")).
			WithArgs("gpt-5", start, end, int64(41)).WillReturnRows(aggregateRows())
		_, err := repo.GetModelStatsAggregated(accountAdminTestContext(), "gpt-5", start, end)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("global", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $3 AND deleted_at IS NULL)")).
			WithArgs(start, end, int64(41)).WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens", "total_cache_tokens",
			"total_cost", "total_actual_cost", "avg_duration_ms",
		}).AddRow(1, 2, 3, 4, 0.7, 0.8, 9.0))
		_, err := repo.GetGlobalStats(accountAdminTestContext(), start, end)
		require.NoError(t, err)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDailyAndBatchUsageQueriesApplyAccountAdminScope(t *testing.T) {
	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ctx := accountAdminTestContext()

	t.Run("daily", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $5 AND deleted_at IS NULL)")).
			WithArgs(int64(9), start, end, sqlmock.AnyArg(), int64(41)).
			WillReturnRows(sqlmock.NewRows([]string{
				"date", "total_requests", "total_input_tokens", "total_output_tokens", "total_cache_tokens",
				"total_cost", "total_actual_cost", "avg_duration_ms",
			}))
		got, err := repo.GetDailyStatsAggregated(ctx, 9, start, end)
		require.NoError(t, err)
		require.Empty(t, got)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("batch users", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("ul.account_id IN (SELECT id FROM accounts WHERE account_admin_id = $5 AND deleted_at IS NULL)")).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(41)).
			WillReturnRows(sqlmock.NewRows([]string{"user_id", "platform", "total_cost", "today_cost"}))
		got, err := repo.GetBatchUserUsageStats(ctx, []int64{9}, time.Time{}, time.Time{})
		require.NoError(t, err)
		require.Contains(t, got, int64(9))
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("batch api keys", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $5 AND deleted_at IS NULL)")).
			WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), int64(41)).
			WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "total_cost", "today_cost"}))
		got, err := repo.GetBatchAPIKeyUsageStats(ctx, []int64{3}, time.Time{}, time.Time{})
		require.NoError(t, err)
		require.Contains(t, got, int64(3))
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestDashboardStatsRejectAccountAdminScope(t *testing.T) {
	repo := &usageLogRepository{}
	_, err := repo.GetDashboardStats(accountAdminTestContext())
	require.ErrorIs(t, err, service.ErrInsufficientPerms)

	_, err = repo.GetDashboardStatsWithRange(accountAdminTestContext(), time.Time{}, time.Time{})
	require.ErrorIs(t, err, service.ErrInsufficientPerms)
}
