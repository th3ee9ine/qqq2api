//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration232AddsProxyMaxAccounts(t *testing.T) {
	content, err := FS.ReadFile("232_add_proxy_max_accounts.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS max_accounts INT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "CHECK (max_accounts >= 0)")
	require.Contains(t, sql, "Zero means unlimited")
}
