//go:build unit

package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration235CreatesDurableAccountDailyRollup(t *testing.T) {
	content, err := FS.ReadFile("235_usage_account_daily_rollups.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS usage_account_daily_rollups")
	require.Contains(t, sql, "PRIMARY KEY (account_id, bucket_date)")
	for _, column := range []string{
		"total_requests",
		"input_tokens",
		"output_tokens",
		"cache_creation_tokens",
		"cache_read_tokens",
		"standard_cost",
		"account_cost",
		"user_cost",
		"total_duration_ms",
		"duration_count",
		"lifetime_requests",
		"lifetime_account_cost",
	} {
		require.Contains(t, sql, column)
	}
}

func TestMigration235BackfillsAndMaintainsAccountDailyRollup(t *testing.T) {
	content, err := FS.ReadFile("235_usage_account_daily_rollups.sql")
	require.NoError(t, err)

	sql := string(content)
	// Existing rows are backfilled idempotently, while new rows are aggregated
	// through a statement-level transition-table trigger.  These checks catch
	// accidental regressions that would make counters disappear after cleanup.
	require.Contains(t, sql, "INSERT INTO usage_account_daily_rollups")
	require.Contains(t, sql, "FROM usage_logs ul")
	require.Contains(t, sql, "ON CONFLICT (account_id, bucket_date)")
	require.Contains(t, sql, "DO NOTHING")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION usage_account_daily_rollup_after_insert")
	require.Contains(t, sql, "REFERENCING NEW TABLE AS inserted_usage_logs")
	require.Contains(t, sql, "CREATE TRIGGER usage_logs_account_daily_rollup_insert")
	require.Contains(t, sql, "AFTER INSERT ON usage_logs")
	// The migration serializes the backfill with concurrent writers and also
	// installs equivalent triggers on existing partitions that can be written
	// to directly by maintenance jobs.
	require.Contains(t, sql, "LOCK TABLE usage_logs IN SHARE MODE")
	require.Contains(t, sql, "FROM pg_inherits inh")
	require.Contains(t, sql, "DROP TRIGGER IF EXISTS usage_logs_account_daily_rollup_insert ON %I.%I")
	require.Contains(t, sql, "CREATE TRIGGER usage_logs_account_daily_rollup_insert\n             AFTER INSERT ON %I.%I")
}
