package repository

import (
	"context"
	"strings"

	"github.com/lib/pq"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// DeleteCodexTurnStateScopeExtras removes only the generated, model-scoped
// Turn State records selected by the service. Keeping the operation atomic
// avoids a read/replace race with another model's collection update.
func (r *accountRepository) DeleteCodexTurnStateScopeExtras(ctx context.Context, id int64, keys []string) error {
	if r == nil || r.sql == nil || id <= 0 || len(keys) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || !strings.HasPrefix(key, service.CodexTurnStateModelExtraPrefix) && !strings.HasPrefix(key, service.CodexTurnStateProbeBurstBudgetExtraPrefix) {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		filtered = append(filtered, key)
	}
	if len(filtered) == 0 {
		return nil
	}
	query := "UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) - $1::text[], updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL"
	args := []any{pqStringArray(filtered), id}
	if ownerID, ok := ctxkey.AccountAdminIDFromContext(ctx); ok {
		query += " AND account_admin_id = $3"
		args = append(args, ownerID)
	}
	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAccountNotFound
	}
	if r.schedulerCache != nil {
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return nil
}

// pqStringArray is kept local to avoid exposing database-driver details in the
// service package. The repository already uses pq for array parameters.
func pqStringArray(values []string) any {
	return pq.Array(values)
}

var _ service.CodexTurnStateScopeCleanupRepository = (*accountRepository)(nil)
