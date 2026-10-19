//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"github.com/th3ee9ine/qqq2api/migrations"
)

func TestGroupUsageLifetimeSurvivesHistoricalCleanup(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	ctx := context.Background()
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	for _, partitioned := range []bool{false, true} {
		name := "ordinary"
		if partitioned {
			name = "partitioned"
		}
		t.Run(name, func(t *testing.T) {
			tx := newGroupUsageLifetimeTestTx(t, ctx, partitioned)
			seedGroupUsageLifetimeHistory(t, ctx, tx)
			applyGroupUsageLifetimeMigration(t, ctx, tx)
			aggregation := newDashboardAggregationRepositoryWithSQL(tx)
			require.NoError(t, aggregation.SyncGroupUsageRollups(ctx, todayStart))
			assertGroupUsageLifetimeSummary(t, ctx, tx, todayStart, 10, 10000, 1000, 9000)

			cleanup := newUsageCleanupRepositoryWithSQL(nil, tx)
			deleted, err := cleanup.DeleteUsageLogsBatch(ctx, service.UsageCleanupFilters{
				StartTime: todayStart.AddDate(0, 0, -2),
				EndTime:   todayStart,
			}, 100)
			require.NoError(t, err)
			require.Equal(t, int64(1), deleted)
			require.NoError(t, aggregation.SyncGroupUsageRollups(ctx, todayStart))

			// Retention changes the daily history, but the original 9000 remains
			// part of lifetime usage alongside today's 1000.
			assertGroupUsageLifetimeSummary(t, ctx, tx, todayStart, 10, 10000, 1000, 0)
		})
	}
}

func TestGroupUsageLifetimeSurvivesLogDeletion(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	ctx := context.Background()
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name      string
		statement string
		targeted  bool
		todayCost float64
	}{
		{name: "all_logs", statement: "DELETE FROM usage_logs"},
		{name: "repository_delete", targeted: true, todayCost: 1000},
		{name: "cascaded_user_delete", statement: "DELETE FROM users WHERE id = 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := newGroupUsageLifetimeTestTx(t, ctx, false)
			seedGroupUsageLifetimeHistory(t, ctx, tx)
			applyGroupUsageLifetimeMigration(t, ctx, tx)
			aggregation := newDashboardAggregationRepositoryWithSQL(tx)
			require.NoError(t, aggregation.SyncGroupUsageRollups(ctx, todayStart))

			if tc.targeted {
				require.NoError(t, newUsageLogRepositoryWithSQL(nil, tx).Delete(ctx, 1))
			} else {
				_, err := tx.ExecContext(ctx, tc.statement)
				require.NoError(t, err)
			}
			require.NoError(t, aggregation.SyncGroupUsageRollups(ctx, todayStart))
			assertGroupUsageLifetimeSummary(t, ctx, tx, todayStart, 10, 10000, tc.todayCost, 0)
		})
	}
}

func TestGroupUsageLifetimeBackfillUsesHistoricalBucketsWithoutDoubleCounting(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	ctx := context.Background()
	tx := newGroupUsageLifetimeTestTx(t, ctx, false)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10), (20);
		INSERT INTO users (id) VALUES (1);
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
			(1, 1, 10, 9000, TIMESTAMPTZ '2026-08-13 12:00:00+08'),
			(2, 1, 10, 1000, TIMESTAMPTZ '2026-08-14 12:00:00+08'),
			(3, 1, 20, 1000, TIMESTAMPTZ '2026-08-13 12:00:00+08'),
			(4, 1, 20, 1000, TIMESTAMPTZ '2026-08-14 12:00:00+08');
		INSERT INTO usage_group_daily_rollups (bucket_date, group_id, actual_cost) VALUES
			(DATE '2026-08-13', 10, 9000),
			(DATE '2026-08-12', 20, 8000),
			(DATE '2026-08-13', 20, 1000),
			(DATE '2026-08-14', 20, 999999);
		UPDATE usage_group_rollup_state
		SET closed_before = DATE '2026-08-14',
			retained_from = TIMESTAMPTZ '2026-08-12 00:00:00+08',
			timezone_name = 'Asia/Shanghai'
		WHERE id = 1;
	`)
	require.NoError(t, err)
	applyGroupUsageLifetimeMigration(t, ctx, tx)

	// Group 10 still has all source rows: its published bucket is not extra
	// usage. Group 20 has lost an older day's source rows, but its valid bucket
	// still recovers that amount. The unpublished poisoned bucket is ignored.
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 10000)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 20, 10000)
}

func TestGroupUsageLifetimeMigrationReplayPreservesAccumulatedUsage(t *testing.T) {
	ctx := context.Background()
	tx := newGroupUsageLifetimeTestTx(t, ctx, false)
	seedGroupUsageLifetimeHistory(t, ctx, tx)
	applyGroupUsageLifetimeMigration(t, ctx, tx)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
		VALUES (3, 1, 10, 250, TIMESTAMPTZ '2026-08-14 13:00:00+08');
	`)
	require.NoError(t, err)
	applyGroupUsageLifetimeMigration(t, ctx, tx)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 10250)

	_, err = tx.ExecContext(ctx, "DELETE FROM usage_logs")
	require.NoError(t, err)
	applyGroupUsageLifetimeMigration(t, ctx, tx)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 10250)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
		VALUES (4, 1, 10, 50, TIMESTAMPTZ '2026-08-14 14:00:00+08');
	`)
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 10300)
}

func TestGroupUsageLifetimeTracksInsertUpdateAndRollback(t *testing.T) {
	ctx := context.Background()
	tx := newGroupUsageLifetimeTestTx(t, ctx, false)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10), (20);
		INSERT INTO users (id) VALUES (1);
	`)
	require.NoError(t, err)
	applyGroupUsageLifetimeMigration(t, ctx, tx)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
			(1, 1, 10, 1000, TIMESTAMPTZ '2026-08-14 12:00:00+08'),
			(2, 1, NULL, 50, TIMESTAMPTZ '2026-08-14 12:00:00+08');
	`)
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 1000)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 20, 0)

	_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET actual_cost = 1100 WHERE id = 1")
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 1100)
	_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET group_id = 20, actual_cost = 1200 WHERE id = 1")
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 0)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 20, 1200)

	_, err = tx.ExecContext(ctx, `
		UPDATE usage_logs SET group_id = 10 WHERE id = 2;
		UPDATE usage_logs SET group_id = NULL WHERE id = 1;
	`)
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 50)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 20, 0)
	_, err = tx.ExecContext(ctx, "UPDATE usage_logs SET group_id = 10 WHERE id = 1")
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 1250)

	_, err = tx.ExecContext(ctx, `
		SAVEPOINT usage_mutation;
		UPDATE usage_logs SET actual_cost = 1400 WHERE id = 1;
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
		VALUES (3, 1, 10, 250, TIMESTAMPTZ '2026-08-14 13:00:00+08');
	`)
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 1700)
	_, err = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT usage_mutation")
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 1250)
}

func TestGroupUsageLifetimePartitionWritesAndDrop(t *testing.T) {
	useGroupUsageRepositoryTestTimezone(t, "Asia/Shanghai")
	ctx := context.Background()
	tx := newGroupUsageLifetimeTestTx(t, ctx, true)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		CREATE TABLE usage_logs_202608 PARTITION OF usage_logs
		FOR VALUES FROM ('2026-08-01 00:00:00+08') TO ('2026-09-01 00:00:00+08');
	`)
	require.NoError(t, err)
	applyGroupUsageLifetimeMigration(t, ctx, tx)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at)
		VALUES (1, 1, 10, 1000, TIMESTAMPTZ '2026-08-14 12:00:00+08');
	`)
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 1000)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_logs_202608 (id, user_id, group_id, actual_cost, created_at)
		VALUES (2, 1, 10, 2000, TIMESTAMPTZ '2026-08-14 13:00:00+08');
		INSERT INTO usage_logs_default (id, user_id, group_id, actual_cost, created_at)
		VALUES (3, 1, 10, 3000, TIMESTAMPTZ '2026-01-14 12:00:00+08');
	`)
	require.NoError(t, err)
	assertGroupUsageLifetimeTotal(t, ctx, tx, 10, 6000)

	_, err = tx.ExecContext(ctx, "DROP TABLE usage_logs_202608")
	require.NoError(t, err)
	todayStart := time.Date(2026, 8, 13, 16, 0, 0, 0, time.UTC)
	require.NoError(t, newDashboardAggregationRepositoryWithSQL(tx).SyncGroupUsageRollups(ctx, todayStart))
	assertGroupUsageLifetimeSummary(t, ctx, tx, todayStart, 10, 6000, 0, 0)
}

func newGroupUsageLifetimeTestTx(t *testing.T, ctx context.Context, partitioned bool) *sql.Tx {
	t.Helper()
	schema := createGroupUsageRollupTriggerTestSchema(t, ctx, partitioned, true)
	tx := beginGroupUsageRollupTriggerTestTx(t, ctx, schema)
	t.Cleanup(func() { _ = tx.Rollback() })
	require.NoError(t, setGroupUsageRollupTriggerTimeZone(ctx, tx, "Asia/Shanghai"))
	return tx
}

func seedGroupUsageLifetimeHistory(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO groups (id) VALUES (10);
		INSERT INTO users (id) VALUES (1);
		INSERT INTO usage_logs (id, user_id, group_id, actual_cost, created_at) VALUES
			(1, 1, 10, 9000, TIMESTAMPTZ '2026-08-13 12:00:00+08'),
			(2, 1, 10, 1000, TIMESTAMPTZ '2026-08-14 12:00:00+08');
	`)
	require.NoError(t, err)
}

func applyGroupUsageLifetimeMigration(t *testing.T, ctx context.Context, tx *sql.Tx) {
	t.Helper()
	migrationSQL, err := migrations.FS.ReadFile("246_usage_group_lifetime_totals.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
}

func assertGroupUsageLifetimeTotal(t *testing.T, ctx context.Context, tx *sql.Tx, groupID int64, want float64) {
	t.Helper()
	var actual float64
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT COALESCE((SELECT actual_cost FROM usage_group_lifetime_totals WHERE group_id = $1), 0)
	`, groupID).Scan(&actual))
	require.InDelta(t, want, actual, 0.0000001, "group %d lifetime total", groupID)
}

func assertGroupUsageLifetimeSummary(t *testing.T, ctx context.Context, tx *sql.Tx, todayStart time.Time, groupID int64, total, today, yesterday float64) {
	t.Helper()
	result, err := newUsageLogRepositoryWithSQL(nil, tx).GetAllGroupUsageSummary(ctx, todayStart)
	require.NoError(t, err)
	for _, row := range result {
		if row.GroupID == groupID {
			require.InDelta(t, total, row.TotalCost, 0.0000001)
			require.InDelta(t, today, row.TodayCost, 0.0000001)
			require.InDelta(t, yesterday, row.YesterdayCost, 0.0000001)
			return
		}
	}
	t.Fatalf("group %d missing from usage summary", groupID)
}
