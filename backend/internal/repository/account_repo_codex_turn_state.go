package repository

import (
	"context"
	"encoding/json"
	"time"

	dbaccount "github.com/th3ee9ine/qqq2api/ent/account"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

var _ service.CodexTurnStateSourceRepository = (*accountRepository)(nil)
var _ service.CodexTurnStateProvenanceRepository = (*accountRepository)(nil)
var _ service.CodexTurnStateAtomicRepository = (*accountRepository)(nil)
var _ service.CodexTurnStateAccountProbeBoundaryRepository = (*accountRepository)(nil)

// AdvanceCodexTurnStateProbeNotBefore persists the account-wide 429 boundary
// independently of a model-slot CAS. A concurrent state publication may win the
// slot while the quota cooldown still has to block every model on the account.
func (r *accountRepository) AdvanceCodexTurnStateProbeNotBefore(ctx context.Context, accountID, notBefore int64) error {
	if notBefore <= 0 {
		return nil
	}
	_, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET extra = jsonb_set(
			COALESCE(extra, '{}'::jsonb),
			ARRAY['codex_turn_state_auto_probe_not_before_ms']::text[],
			to_jsonb($1::bigint),
			true
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		  AND COALESCE((extra ->> 'codex_turn_state_auto_probe_not_before_ms')::bigint, 0) < $1
	`, notBefore, accountID)
	return err
}

// Each account/model slot is replaced in one UPDATE. Older collection or
// verification snapshots and pre-revocation workers cannot overwrite a newer
// verified value, including across application instances.
func (r *accountRepository) UpdateCodexTurnState(ctx context.Context, accountID int64, slot string, value map[string]any) (bool, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
			UPDATE accounts SET extra = jsonb_set(
				jsonb_set(COALESCE(extra, '{}'::jsonb), ARRAY[$1]::text[],
					$2::jsonb || jsonb_build_object(
						'codex_turn_state_auto_probe_not_before_ms', GREATEST(COALESCE((extra -> $1 ->> 'codex_turn_state_auto_probe_not_before_ms')::bigint, 0), COALESCE(($2::jsonb ->> 'codex_turn_state_auto_probe_not_before_ms')::bigint, 0)),
						'codex_turn_state_auto_probe_at_ms', GREATEST(COALESCE((extra -> $1 ->> 'codex_turn_state_auto_probe_at_ms')::bigint, 0), COALESCE(($2::jsonb ->> 'codex_turn_state_auto_probe_at_ms')::bigint, 0))
					), true),
				ARRAY['codex_turn_state_auto_probe_not_before_ms']::text[],
				to_jsonb(GREATEST(
					COALESCE((extra ->> 'codex_turn_state_auto_probe_not_before_ms')::bigint, 0),
					COALESCE(($2::jsonb ->> 'codex_turn_state_auto_probe_not_before_ms')::bigint, 0)
				)), true
			), updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL
		AND COALESCE((extra -> $1 -> 'codex_turn_state_auto_recovery' ->> 'invalidated_at_ms')::bigint, 0) <= COALESCE(($2::jsonb -> 'codex_turn_state_auto_recovery' ->> 'invalidated_at_ms')::bigint, 0)
		AND (
			COALESCE((extra -> $1 -> 'codex_turn_state_auto_recovery' ->> 'invalidated_at_ms')::bigint, 0) < COALESCE(($2::jsonb -> 'codex_turn_state_auto_recovery' ->> 'invalidated_at_ms')::bigint, 0)
			OR (
				COALESCE((extra -> $1 ->> 'codex_turn_state_auto_set_at_ms')::bigint, 0) <= COALESCE(($2::jsonb ->> 'codex_turn_state_auto_set_at_ms')::bigint, 0)
				AND COALESCE((extra -> $1 ->> 'codex_turn_state_auto_verified_at_ms')::bigint, 0) <= COALESCE(($2::jsonb ->> 'codex_turn_state_auto_verified_at_ms')::bigint, 0)
			)
		)
	`, slot, string(payload), accountID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

// Refresh just the selected account's managed states, without credentials,
// proxies or group joins. The normal soft-delete filter still applies.
func (r *accountRepository) GetCodexTurnStateSource(ctx context.Context, id int64) (*service.Account, error) {
	row, err := r.client.Account.Query().Where(dbaccount.IDEQ(id)).Select(dbaccount.FieldID, dbaccount.FieldPlatform, dbaccount.FieldType, dbaccount.FieldExtra).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	return &service.Account{ID: row.ID, Platform: row.Platform, Type: row.Type, Extra: row.Extra}, nil
}

// RecordCodexTurnStateProvenance atomically adds one digest ownership record
// while pruning expired entries and retaining at most the newest bounded
// history. The opaque state is never provided to this repository method.
func (r *accountRepository) RecordCodexTurnStateProvenance(ctx context.Context, accountID int64, model, digest string, expiresAtMS int64) error {
	payload, err := json.Marshal(map[string]any{
		digest: map[string]any{
			"model":         model,
			"expires_at_ms": expiresAtMS,
		},
	})
	if err != nil {
		return err
	}
	result, err := r.sql.ExecContext(ctx, `
		UPDATE accounts
		SET extra = jsonb_set(
			COALESCE(extra, '{}'::jsonb),
			ARRAY[$1]::text[],
			COALESCE((
				SELECT jsonb_object_agg(pruned.key, pruned.value)
				FROM (
					SELECT item.key, item.value
					FROM jsonb_each(
						CASE
							WHEN jsonb_typeof(COALESCE(extra, '{}'::jsonb) -> $1) = 'object'
							THEN COALESCE(extra, '{}'::jsonb) -> $1
							ELSE '{}'::jsonb
						END
					) AS item
					WHERE CASE
						WHEN jsonb_typeof(item.value -> 'expires_at_ms') = 'number'
						THEN (item.value ->> 'expires_at_ms')::bigint
						ELSE 0
					END > $2
					ORDER BY CASE
						WHEN jsonb_typeof(item.value -> 'expires_at_ms') = 'number'
						THEN (item.value ->> 'expires_at_ms')::bigint
						ELSE 0
					END DESC
					LIMIT $3
				) AS pruned
			), '{}'::jsonb) || $4::jsonb,
			true
		), updated_at = NOW()
		WHERE id = $5 AND deleted_at IS NULL
	`, service.CodexTurnStateNativeProvenanceExtraKey, time.Now().UnixMilli(), service.CodexTurnStateNativeProvenanceLimit-1, string(payload), accountID)
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
	return nil
}

// FindCodexTurnStateProvenance performs a digest-only reverse lookup across
// accounts so a different process can reject a known cross-account/model echo.
func (r *accountRepository) FindCodexTurnStateProvenance(ctx context.Context, digest string) ([]service.CodexTurnStateNativeProvenance, error) {
	needle, err := json.Marshal(map[string]any{
		service.CodexTurnStateNativeProvenanceExtraKey: map[string]any{digest: map[string]any{}},
	})
	if err != nil {
		return nil, err
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id, extra -> $1 -> $2
		FROM accounts
		WHERE deleted_at IS NULL
		  AND extra @> $3::jsonb
	`, service.CodexTurnStateNativeProvenanceExtraKey, digest, string(needle))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	origins := make([]service.CodexTurnStateNativeProvenance, 0, 1)
	for rows.Next() {
		var (
			accountID int64
			payload   []byte
			origin    service.CodexTurnStateNativeProvenance
		)
		if err := rows.Scan(&accountID, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &origin); err != nil {
			continue
		}
		if origin.Model == "" || origin.ExpiresAtMS <= 0 {
			continue
		}
		origin.AccountID = accountID
		origins = append(origins, origin)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return origins, nil
}
