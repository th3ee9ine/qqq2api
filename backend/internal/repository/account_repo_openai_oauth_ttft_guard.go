package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/ent/predicate"
	"github.com/th3ee9ine/qqq2api/internal/pkg/logger"
	"github.com/th3ee9ine/qqq2api/internal/service"

	entsql "entgo.io/ent/dialect/sql"
)

// PostgreSQL's timestamptz cast is guarded by a CASE below. This keeps
// imported/malformed JSON metadata from making an otherwise unrelated
// scheduler query fail during expression reordering.
const openAIOAuthTTFTRFC3339Pattern = `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$`

// openAIOAuthTTFTTimestampValidSQL validates both the wire shape and the
// actual PostgreSQL timestamp value. A regular expression alone accepts
// values such as 2026-02-31T00:00:00Z; a direct ::timestamptz cast of those
// imported values would abort the entire scheduler query. JSONPath datetime
// conversion with silent errors works on PostgreSQL 15; normalizing Z to an
// explicit offset also supports that version's narrower ISO-8601 parser.
func openAIOAuthTTFTTimestampValidSQL(expr string) string {
	expr = "regexp_replace(" + expr + ", '^[[:space:]]+|[[:space:]]+$', '', 'g')"
	// JSONPath on PostgreSQL 15 accepts at most microsecond precision, while
	// RFC3339 and timestamptz accept nanoseconds. Fractional seconds cannot
	// affect calendar validity, so omit them only for this validation step.
	calendarExpr := "regexp_replace(" + expr + ", '\\.[0-9]+', '')"
	return "(" + expr + " ~ '" + openAIOAuthTTFTRFC3339Pattern + "'" +
		" AND COALESCE(jsonb_path_exists(to_jsonb(regexp_replace(" + calendarExpr +
		", 'Z$', '+00:00')), 'strict $.datetime()', '{}'::jsonb, true), FALSE))"
}

func openAIOAuthTTFTGuardStateValidSQL(extraExpr string) string {
	state := "(" + extraExpr + " -> '" + service.OpenAIOAuthTTFTGuardStateExtraKey + "')"
	pausedAt := state + " ->> 'paused_at'"
	nextProbeAt := state + " ->> 'next_probe_at'"
	lease := state + " -> 'probe_lease_until'"
	// JSON extraction returns SQL NULL for absent keys. Without COALESCE,
	// NOT(valid_state) is also NULL and excludes every normal account from
	// both scheduling and its first pause. Keep this predicate two-valued and
	// match ParseOpenAIOAuthTTFTGuardState's required model and optional lease.
	return "COALESCE((jsonb_typeof(" + state + ") = 'object'" +
		" AND " + openAIOAuthTTFTTimestampValidSQL(pausedAt) +
		" AND " + openAIOAuthTTFTTimestampValidSQL(nextProbeAt) +
		" AND jsonb_typeof(" + state + " -> 'model') = 'string'" +
		" AND (" + state + " ->> 'model') ~ '[^[:space:]]'" +
		" AND (" + lease + " IS NULL OR " + lease + " = 'null'::jsonb OR " +
		openAIOAuthTTFTTimestampValidSQL(state+" ->> 'probe_lease_until'") + ")), FALSE)"
}

// openAIOAuthTTFTGuardSQL is shared by Ent predicates and raw scheduler
// projections. A valid persisted guard state always removes a non-shadow
// OpenAI OAuth account from scheduling, even after its probe deadline elapses.
func openAIOAuthTTFTGuardSQL(extraExpr, platformExpr, typeExpr, parentExpr string) string {
	return "NOT (" + platformExpr + " = 'openai' AND " + typeExpr +
		" IN ('oauth', 'setup-token') AND " + parentExpr + " IS NULL AND " +
		openAIOAuthTTFTGuardStateValidSQL(extraExpr) + ")"
}

func openAIOAuthTTFTGuardPredicate() predicate.Account {
	return predicate.Account(func(s *entsql.Selector) {
		s.Where(entsql.P(func(b *entsql.Builder) {
			b.WriteString(openAIOAuthTTFTGuardSQL(s.C("extra"), s.C("platform"), s.C("type"), s.C("parent_account_id")))
		}))
	})
}

func ttftGuardTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func (r *accountRepository) PauseOpenAIOAuthTTFT(
	ctx context.Context,
	id int64,
	model string,
	now, next time.Time,
) (bool, error) {
	if id <= 0 {
		return false, service.ErrAccountNotFound
	}
	if now.IsZero() {
		now = time.Now()
	}
	if next.IsZero() {
		next = now.Add(30 * time.Minute)
	}
	state, err := json.Marshal(map[string]any{
		"paused_at":     ttftGuardTimestamp(now),
		"next_probe_at": ttftGuardTimestamp(next),
		"model":         model,
	})
	if err != nil {
		return false, err
	}
	query := `
		UPDATE accounts AS a
		SET extra = jsonb_set(COALESCE(a.extra, '{}'::jsonb),
			'{openai_oauth_ttft_guard_state}', $2::jsonb, true),
			updated_at = NOW()
		WHERE a.id = $1
			AND a.deleted_at IS NULL
			AND a.status = $3
			AND a.schedulable IS TRUE
			AND a.platform = $4
			AND a.type IN ($5, $6)
			AND a.parent_account_id IS NULL
			AND NOT (` + openAIOAuthTTFTGuardStateValidSQL("COALESCE(a.extra, '{}'::jsonb)") + `)`
	args := []any{id, string(state), service.StatusActive, service.PlatformOpenAI, service.AccountTypeOAuth, service.AccountTypeSetupToken}
	if ownerClause, ownerArg, scoped := accountAdminSQLScope(ctx, "a.account_admin_id", len(args)+1); scoped {
		query += ownerClause
		args = append(args, ownerArg)
	}
	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if err := r.notifyOpenAIOAuthTTFTGuardChanged(ctx, id, "pause"); err != nil {
		return true, err
	}
	return true, nil
}

func (r *accountRepository) ClaimOpenAIOAuthTTFTProbe(
	ctx context.Context,
	now time.Time,
	token string,
	leaseUntil time.Time,
) (*service.Account, error) {
	if now.IsZero() {
		now = time.Now()
	}
	if token == "" {
		return nil, fmt.Errorf("openai oauth ttft probe token is empty")
	}
	if leaseUntil.IsZero() || !leaseUntil.After(now) {
		return nil, fmt.Errorf("openai oauth ttft probe lease must be after now")
	}
	nowString := ttftGuardTimestamp(now)
	query := `
		WITH candidate AS MATERIALIZED (
			SELECT a.id
			FROM accounts AS a
			WHERE a.deleted_at IS NULL
				AND a.status = $1
				AND a.schedulable IS TRUE
				AND a.platform = $2
				AND a.type IN ($3, $4)
				AND a.parent_account_id IS NULL
				AND ` + openAIOAuthTTFTGuardStateValidSQL("COALESCE(a.extra, '{}'::jsonb)") + `
				AND CASE
					WHEN ` + openAIOAuthTTFTTimestampValidSQL("(COALESCE(a.extra, '{}'::jsonb) -> 'openai_oauth_ttft_guard_state' ->> 'next_probe_at')") + `
					THEN ((COALESCE(a.extra, '{}'::jsonb) -> 'openai_oauth_ttft_guard_state' ->> 'next_probe_at')::timestamptz <= $5::timestamptz)
					ELSE FALSE
				END
				AND (
					NULLIF(COALESCE(a.extra, '{}'::jsonb) -> 'openai_oauth_ttft_guard_state' ->> 'probe_lease_until', '') IS NULL
					OR CASE
						WHEN ` + openAIOAuthTTFTTimestampValidSQL("(COALESCE(a.extra, '{}'::jsonb) -> 'openai_oauth_ttft_guard_state' ->> 'probe_lease_until')") + `
						THEN ((COALESCE(a.extra, '{}'::jsonb) -> 'openai_oauth_ttft_guard_state' ->> 'probe_lease_until')::timestamptz <= $5::timestamptz)
						ELSE FALSE
					END
				)
			ORDER BY a.id ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE accounts AS a
			SET extra = jsonb_set(
				COALESCE(a.extra, '{}'::jsonb),
				'{openai_oauth_ttft_guard_state}',
				(COALESCE(a.extra -> 'openai_oauth_ttft_guard_state', '{}'::jsonb)
					|| jsonb_build_object('probe_token', $6::text, 'probe_lease_until', $7::text)),
				true),
				updated_at = NOW()
			FROM candidate
			WHERE a.id = candidate.id
			RETURNING a.id
		)
		SELECT id FROM claimed`
	args := []any{service.StatusActive, service.PlatformOpenAI, service.AccountTypeOAuth, service.AccountTypeSetupToken, nowString, token, ttftGuardTimestamp(leaseUntil)}
	if ownerClause, ownerArg, scoped := accountAdminSQLScope(ctx, "a.account_admin_id", len(args)+1); scoped {
		// The candidate alias is the account selected by the CTE; keep the
		// owner boundary inside the CTE before the row lock is taken.
		query = replaceFirstAccountAliasOwnerClause(query, ownerClause)
		args = append(args, ownerArg)
	}
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	var id int64
	claimed := false
	if rows.Next() {
		claimed = true
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	if !claimed {
		return nil, nil
	}
	if err := r.notifyOpenAIOAuthTTFTGuardChanged(ctx, id, "claim"); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id)
}

func (r *accountRepository) FinishOpenAIOAuthTTFTProbe(
	ctx context.Context,
	id int64,
	token string,
	recovered bool,
	next time.Time,
) (bool, error) {
	if id <= 0 || token == "" {
		return false, nil
	}
	if next.IsZero() {
		next = time.Now().Add(30 * time.Minute)
	}
	query := `
		UPDATE accounts AS a
		SET extra = CASE WHEN $4::boolean
			THEN COALESCE(a.extra, '{}'::jsonb) - 'openai_oauth_ttft_guard_state'
			ELSE jsonb_set(
				COALESCE(a.extra, '{}'::jsonb),
				'{openai_oauth_ttft_guard_state}',
				((COALESCE(a.extra -> 'openai_oauth_ttft_guard_state', '{}'::jsonb)
					- 'probe_token' - 'probe_lease_until')
					|| jsonb_build_object('next_probe_at', $3::text)),
				true)
			END,
			updated_at = NOW()
		WHERE a.id = $1
			AND a.deleted_at IS NULL
			AND (a.extra -> 'openai_oauth_ttft_guard_state' ->> 'probe_token') = $2`
	args := []any{id, token, ttftGuardTimestamp(next), recovered}
	if ownerClause, ownerArg, scoped := accountAdminSQLScope(ctx, "a.account_admin_id", len(args)+1); scoped {
		query += ownerClause
		args = append(args, ownerArg)
	}
	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if err := r.notifyOpenAIOAuthTTFTGuardChanged(ctx, id, "finish"); err != nil {
		return true, err
	}
	return true, nil
}

func (r *accountRepository) notifyOpenAIOAuthTTFTGuardChanged(ctx context.Context, id int64, operation string) error {
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] openai oauth ttft guard %s failed: account=%d err=%v", operation, id, err)
		return err
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

// replaceFirstAccountAliasOwnerClause injects a restricted owner predicate in
// the candidate CTE. The helper is intentionally local to this query so the
// outer UPDATE (which only joins the selected id) cannot accidentally broaden
// the account-admin scope.
func replaceFirstAccountAliasOwnerClause(query, ownerClause string) string {
	needle := "\n\t\t\t\tAND a.parent_account_id IS NULL"
	return strings.Replace(query, needle, needle+ownerClause, 1)
}
