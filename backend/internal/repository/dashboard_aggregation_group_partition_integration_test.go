//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/migrations"
)

func TestCreateUsageLogsPartitionMaintainsGroupLifetimeTotals(t *testing.T) {
	ctx := context.Background()
	tx, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	schema := pq.QuoteIdentifier(fmt.Sprintf("group_lifetime_partition_%d", time.Now().UnixNano()))
	_, err = tx.ExecContext(ctx, "CREATE SCHEMA "+schema+"; SET LOCAL search_path TO "+schema+`;
		CREATE TABLE usage_logs (
			group_id BIGINT,
			actual_cost NUMERIC(20, 10) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		) PARTITION BY RANGE (created_at)
	`)
	require.NoError(t, err)
	repo := newDashboardAggregationRepositoryWithSQL(tx)
	october := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	// Partition maintenance remains usable before the durable rollup migration.
	require.NoError(t, repo.createUsageLogsPartition(ctx, october))

	// Apply the real migration to verify its function/transition-table contract
	// agrees with partition maintenance, without unrelated account-rollup setup.
	_, err = tx.ExecContext(ctx, `
		CREATE TABLE usage_group_rollup_state (
			id INT PRIMARY KEY,
			closed_before DATE NOT NULL,
			timezone_name TEXT NOT NULL
		);
		CREATE TABLE usage_group_daily_rollups (
			group_id BIGINT NOT NULL,
			bucket_date DATE NOT NULL,
			actual_cost NUMERIC(20, 10) NOT NULL
		)
	`)
	require.NoError(t, err)
	migrationSQL, err := migrations.FS.ReadFile("246_usage_group_lifetime_totals.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migrationSQL))
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `
		DROP TRIGGER usage_logs_group_usage_lifetime_insert ON usage_logs_202610;
		DROP TRIGGER usage_logs_group_usage_lifetime_update ON usage_logs_202610
	`)
	require.NoError(t, err)

	// Existing partitions can be repaired, while later partitions receive the
	// trigger on creation. Repeating housekeeping must not duplicate it.
	for _, month := range []time.Time{october, october.AddDate(0, 1, 0)} {
		for range 2 {
			require.NoError(t, repo.createUsageLogsPartition(ctx, month))
		}
		for _, trigger := range []string{"usage_logs_group_usage_lifetime_insert", "usage_logs_group_usage_lifetime_update"} {
			var triggers int
			err = tx.QueryRowContext(ctx, `
				SELECT COUNT(*) FROM pg_trigger
				WHERE tgrelid = $1::regclass AND tgname = $2
			`, "usage_logs_"+month.Format("200601"), trigger).Scan(&triggers)
			require.NoError(t, err)
			require.Equal(t, 1, triggers)
		}
	}

	// Parent routing and direct child writes must each contribute once. Cleanup
	// then removes all source rows, without changing the durable total.
	_, err = tx.ExecContext(ctx, `
		INSERT INTO usage_logs VALUES (10, 1.25, TIMESTAMPTZ '2026-11-02 00:00:00+00');
		INSERT INTO usage_logs_202611 VALUES (10, 2.5, TIMESTAMPTZ '2026-11-03 00:00:00+00');
		INSERT INTO usage_logs_202610 VALUES (10, 5, TIMESTAMPTZ '2026-10-04 00:00:00+00');
		UPDATE usage_logs_202611 SET actual_cost = 4 WHERE actual_cost = 2.5;
		UPDATE usage_logs SET actual_cost = 2, created_at = TIMESTAMPTZ '2026-10-06 00:00:00+00'
		WHERE actual_cost = 1.25;
		DROP TABLE usage_logs_202610;
		DROP TABLE usage_logs_202611
	`)
	require.NoError(t, err)
	var total float64
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT actual_cost FROM usage_group_lifetime_totals WHERE group_id = 10
	`).Scan(&total))
	require.InDelta(t, 11, total, 1e-9)
}
