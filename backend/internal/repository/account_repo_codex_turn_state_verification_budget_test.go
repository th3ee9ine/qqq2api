package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestCompareAndSwapCodexTurnStateVerificationBudgetUsesVersionedTopLevelSlot(t *testing.T) {
	repo, mock, query := newCodexTurnStateAtomicRepository(t)
	budget := service.CodexTurnStateVerificationBudget{
		Version: 2, StartedAtMS: 1000, ExpiresAtMS: 2000, APIKeyID: 9, Country: "US",
		SessionDigests: []string{strings.Repeat("a", 64)}, Baselines: []string{"gpt-6-astra"},
	}
	payload, err := json.Marshal(budget)
	require.NoError(t, err)
	mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).
		WithArgs(service.CodexTurnStateVerificationBudgetExtraKey, string(payload), int64(3), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	updated, err := repo.CompareAndSwapCodexTurnStateVerificationBudget(context.Background(), 3, 1, budget)
	require.NoError(t, err)
	require.True(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())

	sqlText := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(*query, " "))
	require.Contains(t, sqlText, "ARRAY[$1]::text[]")
	require.Contains(t, sqlText, "jsonb_typeof(COALESCE(extra, '{}'::jsonb) -> $1 -> 'version') = 'number'")
	require.Contains(t, sqlText, "END = $4")
	require.NotContains(t, sqlText, strings.Repeat("a", 64), "budget contents must be bound parameters")
}

func TestCompareAndSwapCodexTurnStateVerificationBudgetReportsCASLoss(t *testing.T) {
	repo, mock, _ := newCodexTurnStateAtomicRepository(t)
	budget := service.CodexTurnStateVerificationBudget{Version: 1, StartedAtMS: 1000, ExpiresAtMS: 2000, APIKeyID: 9}
	mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).
		WithArgs(service.CodexTurnStateVerificationBudgetExtraKey, sqlmock.AnyArg(), int64(3), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	updated, err := repo.CompareAndSwapCodexTurnStateVerificationBudget(context.Background(), 3, 0, budget)
	require.NoError(t, err)
	require.False(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}
