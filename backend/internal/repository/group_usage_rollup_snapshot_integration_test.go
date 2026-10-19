//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Interleave a historical mutation immediately before the aggregation query.
// The former two-query implementation already read the old watermark here and
// consequently reused a stale daily bucket. A single SQL statement must observe
// the mutation and the trigger's invalidated watermark in the same snapshot.
func TestGroupUsageSummaryObservesHistoricalMutationBeforeAggregation(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	ctx := context.Background()
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name          string
		mutation      string
		totalCost     float64
		yesterdayCost float64
	}{
		{
			name: "insert",
			mutation: `INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
				VALUES (4, 1, 10, 5, TIMESTAMPTZ '2026-08-13 13:00:00+08')`,
			totalCost: 14, yesterdayCost: 8,
		},
		{
			name:          "update",
			mutation:      `UPDATE usage_logs SET actual_cost = 7 WHERE id = 2`,
			totalCost:     13,
			yesterdayCost: 7,
		},
		{
			name:          "delete",
			mutation:      `DELETE FROM usage_logs WHERE id = 2`,
			totalCost:     9, // Log deletion retains lifetime spend; yesterday reflects retained logs.
			yesterdayCost: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := seedGroupUsageSummarySnapshotTest(t, ctx)
			defer func() { _ = tx.Rollback() }()
			executor := &groupUsageSummaryInterleavingExecutor{
				sqlExecutor: tx,
				beforeAggregation: func(ctx context.Context) error {
					_, err := tx.ExecContext(ctx, tc.mutation)
					return err
				},
			}
			repo := newUsageLogRepositoryWithSQL(nil, executor)

			result, err := repo.GetAllGroupUsageSummary(ctx, todayStart)
			require.NoError(t, err)
			require.True(t, executor.interleaved, "historical mutation must run before the aggregation query")
			require.Len(t, result, 1)
			require.Equal(t, int64(10), result[0].GroupID)
			require.InDelta(t, tc.totalCost, result[0].TotalCost, 0.0000001)
			require.InDelta(t, 4, result[0].TodayCost, 0.0000001)
			require.InDelta(t, tc.yesterdayCost, result[0].YesterdayCost, 0.0000001)
		})
	}
}

func TestGroupUsageSummaryIgnoresHistoricalBucketsWhenStateInvalid(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	ctx := context.Background()
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		invalidate string
	}{
		{name: "missing_state", invalidate: `DELETE FROM usage_group_rollup_state WHERE id = 1`},
		{name: "timezone_mismatch", invalidate: `UPDATE usage_group_rollup_state SET timezone_name = 'UTC' WHERE id = 1`},
		{name: "future_watermark", invalidate: `UPDATE usage_group_rollup_state SET closed_before = DATE '2026-08-15' WHERE id = 1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := seedGroupUsageSummarySnapshotTest(t, ctx)
			defer func() { _ = tx.Rollback() }()
			// Deliberately poison the cached buckets: a fallback that reuses them
			// or adds them to the raw rows must fail the numeric assertions below.
			_, err := tx.ExecContext(ctx, `UPDATE usage_group_daily_rollups SET actual_cost = 1000`)
			require.NoError(t, err)
			_, err = tx.ExecContext(ctx, tc.invalidate)
			require.NoError(t, err)

			repo := newUsageLogRepositoryWithSQL(nil, tx)
			result, err := repo.GetAllGroupUsageSummary(ctx, todayStart)
			require.NoError(t, err)
			require.Len(t, result, 1)
			require.Equal(t, int64(10), result[0].GroupID)
			require.InDelta(t, 9, result[0].TotalCost, 0.0000001)
			require.InDelta(t, 4, result[0].TodayCost, 0.0000001)
			require.InDelta(t, 3, result[0].YesterdayCost, 0.0000001)
		})
	}
}

func seedGroupUsageSummarySnapshotTest(t *testing.T, ctx context.Context) *sql.Tx {
	t.Helper()
	schema := createGroupUsageRollupTriggerTestSchema(t, ctx, false)
	tx := beginGroupUsageRollupTriggerTestTx(t, ctx, schema)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.NoError(t, setGroupUsageRollupTriggerTimeZone(ctx, tx, "Asia/Shanghai"))
	_, err := tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
			(1, 1, 10, 2, TIMESTAMPTZ '2026-08-12 12:00:00+08'),
			(2, 1, 10, 3, TIMESTAMPTZ '2026-08-13 12:00:00+08'),
			(3, 1, 10, 4, TIMESTAMPTZ '2026-08-14 12:00:00+08');
		INSERT INTO usage_group_daily_rollups (bucket_date, group_id, actual_cost) VALUES
			(DATE '2026-08-12', 10, 2),
			(DATE '2026-08-13', 10, 3);
		UPDATE usage_group_rollup_state
		SET closed_before = DATE '2026-08-14',
			retained_from = TIMESTAMPTZ '2026-08-12 00:00:00+08',
			timezone_name = 'Asia/Shanghai'
		WHERE id = 1;
	`)
	require.NoError(t, err)
	return tx
}

type groupUsageSummaryInterleavingExecutor struct {
	sqlExecutor
	beforeAggregation func(context.Context) error
	interleaved       bool
}

func (e *groupUsageSummaryInterleavingExecutor) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if !e.interleaved && strings.Contains(query, "FROM usage_group_daily_rollups") {
		e.interleaved = true
		if err := e.beforeAggregation(ctx); err != nil {
			return nil, err
		}
	}
	return e.sqlExecutor.QueryContext(ctx, query, args...)
}
