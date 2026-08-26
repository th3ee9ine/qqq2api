//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration231AddsUnlimitedAPIKeyConcurrency(t *testing.T) {
	content, err := FS.ReadFile("231_api_key_concurrency.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS concurrency INT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "CHECK (concurrency >= 0)")
	require.Contains(t, sql, "Zero means unlimited")
}
