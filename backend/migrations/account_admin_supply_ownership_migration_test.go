package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountAdminSupplyOwnershipMigration(t *testing.T) {
	content, err := FS.ReadFile("240_account_admin_supply_ownership.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS supply_rate_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0")
	require.Contains(t, sql, "CHECK (supply_rate_multiplier >= 0)")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS account_admin_id BIGINT")
	require.Contains(t, sql, "FOREIGN KEY (account_admin_id) REFERENCES users(id) ON DELETE SET NULL")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_accounts_account_admin_id ON accounts(account_admin_id)")
}
