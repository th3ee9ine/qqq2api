package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreGroupModelsListConfigCompatibilityMigration(t *testing.T) {
	content, err := FS.ReadFile("238_restore_group_models_list_config_compat.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	// The migration must be safe to replay and preserve allowlist data when
	// restoring the legacy column used by the generated Ent schema.
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS models_list_config JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.Contains(t, sql, "SET models_list_config = model_allowlist")
	require.Contains(t, sql, "COALESCE(models_list_config, '{}'::jsonb) = '{}'::jsonb")
	require.Contains(t, sql, "COALESCE(model_allowlist, '{}'::jsonb) <> '{}'::jsonb")
	require.Contains(t, sql, "ALTER COLUMN models_list_config SET NOT NULL")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.models_list_config")
	require.Contains(t, sql, "attrelid = 'groups'::regclass")
	// Avoid hard-coding the public schema; ALTER TABLE follows search_path.
	require.NotContains(t, sql, "table_schema = 'public'")
}
