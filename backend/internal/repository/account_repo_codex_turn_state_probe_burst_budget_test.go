package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestCompareAndSwapCodexTurnStateProbeBurstBudgetUsesVersionedModelSlot(t *testing.T) {
	repo, mock, query := newCodexTurnStateAtomicRepository(t)
	model := "gpt-6-astra"
	slot := service.CodexTurnStateProbeBurstBudgetExtraKey(model)
	budget := service.CodexTurnStateProbeBurstBudget{
		Version: 2, Generation: 1700, Model: model, StartedAtMS: 2000, Attempts: 2, CandidatePendingUntilMS: 3000,
	}
	payload, err := json.Marshal(budget)
	require.NoError(t, err)
	mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).
		WithArgs(slot, string(payload), int64(3), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	updated, err := repo.CompareAndSwapCodexTurnStateProbeBurstBudget(context.Background(), 3, slot, 1, budget)
	require.NoError(t, err)
	require.True(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())

	sqlText := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(*query, " "))
	require.Contains(t, sqlText, "ARRAY[$1]::text[]")
	require.Contains(t, sqlText, "jsonb_typeof(COALESCE(extra, '{}'::jsonb) -> $1 -> 'version') = 'number'")
	require.Contains(t, sqlText, "END = $4")
	require.Contains(t, sqlText, "AND ($4 <> 0 OR NOT (COALESCE(extra, '{}'::jsonb) ? $1))")
	require.NotContains(t, sqlText, model, "model metadata must be a bound parameter")
	require.NotContains(t, string(payload), "proxy")
	require.NotContains(t, string(payload), "password")
	require.NotContains(t, string(payload), "sid")
	require.NotContains(t, string(payload), "state")
	require.NotEqual(t, slot, service.CodexTurnStateProbeBurstBudgetExtraKey("codex-auto-review"))
}

func TestCompareAndSwapCodexTurnStateProbeBurstBudgetReportsCASLoss(t *testing.T) {
	repo, mock, _ := newCodexTurnStateAtomicRepository(t)
	model := "gpt-6-astra"
	slot := service.CodexTurnStateProbeBurstBudgetExtraKey(model)
	budget := service.CodexTurnStateProbeBurstBudget{Version: 4, Generation: 10, Model: model, StartedAtMS: 1000, Attempts: 3}
	mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).
		WithArgs(slot, sqlmock.AnyArg(), int64(3), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	updated, err := repo.CompareAndSwapCodexTurnStateProbeBurstBudget(context.Background(), 3, slot, 3, budget)
	require.NoError(t, err)
	require.False(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompareAndSwapCodexTurnStateProbeBurstBudgetAcceptsShortRenewableLease(t *testing.T) {
	repo, mock, _ := newCodexTurnStateAtomicRepository(t)
	model := "gpt-6-astra"
	slot := service.CodexTurnStateProbeBurstBudgetExtraKey(model)
	budget := service.CodexTurnStateProbeBurstBudget{
		Version: 1, Model: model, StartedAtMS: 2_000, Attempts: 1,
		InFlightUntilMS: 62_000,
	}
	mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).
		WithArgs(slot, sqlmock.AnyArg(), int64(3), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	updated, err := repo.CompareAndSwapCodexTurnStateProbeBurstBudget(context.Background(), 3, slot, 0, budget)
	require.NoError(t, err)
	require.True(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompareAndSwapCodexTurnStateProbeBurstBudgetPropagatesStorageFailures(t *testing.T) {
	tests := []struct {
		name   string
		result sql.Result
		dbErr  error
	}{
		{name: "exec", dbErr: errors.New("write unavailable")},
		{name: "rows affected", result: sqlmock.NewErrorResult(errors.New("result unavailable"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock, _ := newCodexTurnStateAtomicRepository(t)
			model := "gpt-6-astra"
			slot := service.CodexTurnStateProbeBurstBudgetExtraKey(model)
			budget := service.CodexTurnStateProbeBurstBudget{Version: 1, Model: model, StartedAtMS: 1000, Attempts: 1}
			expectation := mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).
				WithArgs(slot, sqlmock.AnyArg(), int64(3), int64(0))
			if tc.dbErr != nil {
				expectation.WillReturnError(tc.dbErr)
			} else {
				expectation.WillReturnResult(tc.result)
			}

			updated, err := repo.CompareAndSwapCodexTurnStateProbeBurstBudget(context.Background(), 3, slot, 0, budget)
			require.Error(t, err)
			require.False(t, updated)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCompareAndSwapCodexTurnStateProbeBurstBudgetRejectsInvalidMetadataBeforeSQL(t *testing.T) {
	model := "gpt-6-astra"
	validSlot := service.CodexTurnStateProbeBurstBudgetExtraKey(model)
	valid := service.CodexTurnStateProbeBurstBudget{Version: 1, Model: model, StartedAtMS: 1000, Attempts: 1}
	tests := []struct {
		name     string
		account  int64
		slot     string
		expected int64
		budget   service.CodexTurnStateProbeBurstBudget
	}{
		{name: "unrelated slot", account: 3, slot: "credentials", budget: valid},
		{name: "wrong model slot", account: 3, slot: service.CodexTurnStateProbeBurstBudgetExtraKey("codex-auto-review"), budget: valid},
		{name: "nonincrementing version", account: 3, slot: validSlot, expected: 1, budget: valid},
		{name: "attempt zero", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget { b := valid; b.Attempts = 0; return b }()},
		{name: "attempt four", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget { b := valid; b.Attempts = 4; return b }()},
		{name: "negative in flight", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget { b := valid; b.InFlightUntilMS = -1; return b }()},
		{name: "in flight before start", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget { b := valid; b.InFlightUntilMS = 999; return b }()},
		{name: "in flight beyond maximum lease", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget {
			b := valid
			b.InFlightUntilMS = b.StartedAtMS + service.CodexTurnStateProbeBurstMaxLeaseMS + 1
			return b
		}()},
		{name: "in flight with pending", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget {
			b := valid
			b.InFlightUntilMS = 2_000
			b.CandidatePendingUntilMS = 3_000
			return b
		}()},
		{name: "negative pending", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget { b := valid; b.CandidatePendingUntilMS = -1; return b }()},
		{name: "pending before start", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget { b := valid; b.CandidatePendingUntilMS = 999; return b }()},
		{name: "trimmed model mismatch", account: 3, slot: validSlot, budget: func() service.CodexTurnStateProbeBurstBudget { b := valid; b.Model = " " + model; return b }()},
		{name: "invalid account", account: 0, slot: validSlot, budget: valid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock, _ := newCodexTurnStateAtomicRepository(t)
			updated, err := repo.CompareAndSwapCodexTurnStateProbeBurstBudget(context.Background(), tc.account, tc.slot, tc.expected, tc.budget)
			require.ErrorIs(t, err, errInvalidCodexTurnStateProbeBurstBudgetSlot)
			require.False(t, updated)
			require.NoError(t, mock.ExpectationsWereMet(), "invalid metadata must not dispatch SQL")
		})
	}
}
