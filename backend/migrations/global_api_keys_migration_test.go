//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration229RetiresSubscriptionGroupBilling(t *testing.T) {
	content, err := FS.ReadFile("229_global_api_keys.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "UPDATE groups SET subscription_type = 'standard'")
	require.Contains(t, sql, "daily_limit_usd = NULL")
	require.Contains(t, sql, "weekly_limit_usd = NULL")
	require.Contains(t, sql, "monthly_limit_usd = NULL")
	require.Contains(t, sql, "peak_rate_enabled = FALSE")
	require.Contains(t, sql, "peak_start = ''")
	require.Contains(t, sql, "peak_end = ''")
	require.Contains(t, sql, "peak_rate_multiplier = 1.0")
}
