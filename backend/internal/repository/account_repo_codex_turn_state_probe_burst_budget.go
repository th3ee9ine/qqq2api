package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

var _ service.CodexTurnStateProbeBurstBudgetRepository = (*accountRepository)(nil)

var errInvalidCodexTurnStateProbeBurstBudgetSlot = errors.New("invalid codex turn state probe burst budget slot")

// CompareAndSwapCodexTurnStateProbeBurstBudget reserves one model-scoped
// collection attempt without a read-modify-write race. The slot and payload
// contain only model/generation/count metadata, never a proxy URL or credential.
func (r *accountRepository) CompareAndSwapCodexTurnStateProbeBurstBudget(
	ctx context.Context,
	accountID int64,
	slot string,
	expectedVersion int64,
	budget service.CodexTurnStateProbeBurstBudget,
) (bool, error) {
	model := strings.TrimSpace(budget.Model)
	normalizedModel, modelErr := service.NormalizeOpenAICodexTurnStateDefaultModel(model)
	if accountID <= 0 || expectedVersion < 0 || model == "" || budget.Model != model || modelErr != nil || normalizedModel != model ||
		service.CodexTurnStateOwnerModel(model) != model ||
		slot != service.CodexTurnStateProbeBurstBudgetExtraKey(budget.Model) ||
		!strings.HasPrefix(slot, service.CodexTurnStateProbeBurstBudgetExtraPrefix) ||
		budget.Version != expectedVersion+1 || budget.Generation < 0 || budget.StartedAtMS <= 0 ||
		budget.Attempts < 1 || budget.Attempts > service.CodexTurnStateProbeBurstMaxAttempts ||
		budget.InFlightUntilMS < 0 || budget.InFlightUntilMS > 0 &&
		(budget.InFlightUntilMS <= budget.StartedAtMS || budget.InFlightUntilMS-budget.StartedAtMS > service.CodexTurnStateProbeBurstMaxLeaseMS) ||
		budget.CandidatePendingUntilMS < 0 || budget.CandidatePendingUntilMS > 0 && budget.CandidatePendingUntilMS <= budget.StartedAtMS ||
		budget.InFlightUntilMS > 0 && budget.CandidatePendingUntilMS > 0 {
		return false, errInvalidCodexTurnStateProbeBurstBudgetSlot
	}
	payload, err := json.Marshal(budget)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET extra = jsonb_set(
			COALESCE(extra, '{}'::jsonb),
			ARRAY[$1]::text[],
			$2::jsonb,
			true
		), updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL
		  AND CASE
			WHEN jsonb_typeof(COALESCE(extra, '{}'::jsonb) -> $1) = 'object'
			 AND jsonb_typeof(COALESCE(extra, '{}'::jsonb) -> $1 -> 'version') = 'number'
			THEN (COALESCE(extra, '{}'::jsonb) -> $1 ->> 'version')::bigint
			ELSE 0
		  END = $4
		  AND ($4 <> 0 OR NOT (COALESCE(extra, '{}'::jsonb) ? $1))
	`, slot, string(payload), accountID, expectedVersion)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
