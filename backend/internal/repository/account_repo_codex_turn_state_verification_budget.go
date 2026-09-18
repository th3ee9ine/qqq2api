package repository

import (
	"context"
	"encoding/json"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

var _ service.CodexTurnStateVerificationBudgetRepository = (*accountRepository)(nil)

// CompareAndSwapCodexTurnStateVerificationBudget reserves baseline/SID budget
// state without a read-modify-write race. Concurrent instances that observed the
// same version cannot both consume the final (third) session slot.
func (r *accountRepository) CompareAndSwapCodexTurnStateVerificationBudget(
	ctx context.Context,
	accountID, expectedVersion int64,
	budget service.CodexTurnStateVerificationBudget,
) (bool, error) {
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
	`, service.CodexTurnStateVerificationBudgetExtraKey, string(payload), accountID, expectedVersion)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
