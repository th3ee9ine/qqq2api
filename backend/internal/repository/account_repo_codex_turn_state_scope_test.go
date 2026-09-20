package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestDeleteCodexTurnStateScopeExtrasUsesAtomicJSONBKeyDeletion(t *testing.T) {
	repo, mock, query := newCodexTurnStateAtomicRepository(t)
	modelKey := service.CodexTurnStateModelExtraPrefix + "Z3B0LTQ"
	budgetKey := service.CodexTurnStateProbeBurstBudgetExtraPrefix + "Z3B0LTQ"
	mock.ExpectExec(`(?s)UPDATE accounts SET extra = COALESCE\(extra, '\{\}'::jsonb\) - \$1::text\[\].*WHERE id = \$2`).
		WithArgs("{\""+modelKey+"\",\""+budgetKey+"\"}", int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.DeleteCodexTurnStateScopeExtras(context.Background(), 3, []string{modelKey, budgetKey, modelKey, "unrelated"}))
	require.NoError(t, mock.ExpectationsWereMet())
	require.Contains(t, regexp.MustCompile(`\s+`).ReplaceAllString(*query, " "), "- $1::text[]")
}
