package repository

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func newCodexTurnStateAtomicRepository(t *testing.T) (*accountRepository, sqlmock.Sqlmock, *string) {
	t.Helper()
	var query string
	matcher := sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		query = actual
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(matcher))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &accountRepository{sql: db}, mock, &query
}

func codexTurnStateAtomicTestValue() map[string]any {
	return map[string]any{
		service.CodexTurnStateAutoExtraKey:               "opaque-atomic-test-state",
		service.CodexTurnStateAutoSetAtExtraKey:          int64(1_000),
		service.CodexTurnStateAutoVerifiedAtExtraKey:     int64(2_000),
		service.CodexTurnStateAutoVerifiedModelExtraKey:  "gpt-6-astra",
		service.CodexTurnStateAutoProbeAtExtraKey:        int64(3_000),
		service.CodexTurnStateAutoProbeNotBeforeExtraKey: int64(4_000),
		service.CodexTurnStateAutoRecoveryExtraKey: map[string]any{
			"invalidated_at_ms": int64(500),
			"pending":           false,
		},
	}
}

func TestAdvanceCodexTurnStateProbeNotBeforeUsesAccountWideMonotonicBoundary(t *testing.T) {
	repo, mock, query := newCodexTurnStateAtomicRepository(t)
	mock.ExpectExec(`(?s)UPDATE accounts.*codex_turn_state_auto_probe_not_before_ms.*WHERE id = \$2`).
		WithArgs(int64(4_000), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.AdvanceCodexTurnStateProbeNotBefore(context.Background(), 3, 4_000))
	require.NoError(t, mock.ExpectationsWereMet())
	sqlText := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(*query, " "))
	require.Contains(t, sqlText, "to_jsonb($1::bigint)")
	require.Contains(t, sqlText, "COALESCE((extra ->> '"+service.CodexTurnStateAutoProbeNotBeforeExtraKey+"')::bigint, 0) < $1")

	queryBefore := *query
	require.NoError(t, repo.AdvanceCodexTurnStateProbeNotBefore(context.Background(), 3, 0))
	require.Equal(t, queryBefore, *query, "a zero boundary must not dispatch SQL")
}

func TestUpdateCodexTurnStateAtomicVersionConditionsAndProbeBoundaries(t *testing.T) {
	repo, mock, query := newCodexTurnStateAtomicRepository(t)
	slot := service.CodexTurnStateModelExtraPrefix + base64.RawURLEncoding.EncodeToString([]byte("gpt-6-astra"))
	value := codexTurnStateAtomicTestValue()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).
		WithArgs(slot, string(payload), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	updated, err := repo.UpdateCodexTurnState(context.Background(), 3, slot, value)
	require.NoError(t, err)
	require.True(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())

	sqlText := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(*query, " "))
	require.Contains(t, sqlText, "ARRAY[$1]::text[]")
	require.Contains(t, sqlText, "$2::jsonb || jsonb_build_object(")
	require.NotContains(t, sqlText, value[service.CodexTurnStateAutoExtraKey])
	oldTime := func(key string) string { return "COALESCE((extra -> $1 ->> '" + key + "')::bigint, 0)" }
	newTime := func(key string) string { return "COALESCE(($2::jsonb ->> '" + key + "')::bigint, 0)" }
	for _, key := range []string{service.CodexTurnStateAutoProbeAtExtraKey, service.CodexTurnStateAutoProbeNotBeforeExtraKey} {
		require.Contains(t, sqlText, "'"+key+"', GREATEST("+oldTime(key)+", "+newTime(key)+")")
	}
	accountBoundary := service.CodexTurnStateAutoProbeNotBeforeExtraKey
	require.Contains(t, sqlText, "ARRAY['"+accountBoundary+"']::text[]")
	require.Contains(t, sqlText, "to_jsonb(GREATEST( COALESCE((extra ->> '"+accountBoundary+"')::bigint, 0), "+newTime(accountBoundary)+" ))")
	oldEpoch := "COALESCE((extra -> $1 -> '" + service.CodexTurnStateAutoRecoveryExtraKey + "' ->> 'invalidated_at_ms')::bigint, 0)"
	newEpoch := "COALESCE(($2::jsonb -> '" + service.CodexTurnStateAutoRecoveryExtraKey + "' ->> 'invalidated_at_ms')::bigint, 0)"
	require.Contains(t, sqlText, "AND "+oldEpoch+" <= "+newEpoch)
	versionBranch := "AND ( " + oldEpoch + " < " + newEpoch + " OR ( " +
		oldTime(service.CodexTurnStateAutoSetAtExtraKey) + " <= " + newTime(service.CodexTurnStateAutoSetAtExtraKey) + " AND " +
		oldTime(service.CodexTurnStateAutoVerifiedAtExtraKey) + " <= " + newTime(service.CodexTurnStateAutoVerifiedAtExtraKey) + " ) )"
	require.Contains(t, sqlText, versionBranch)
}

func TestUpdateCodexTurnStateAtomicRowsAffectedAndDatabaseErrors(t *testing.T) {
	databaseErr := errors.New("atomic update database failure")
	rowsErr := errors.New("atomic update affected-row failure")
	for _, tc := range []struct {
		name       string
		result     sql.Result
		execErr    error
		wantErr    error
		updated    bool
		revocation bool
	}{
		{name: "successful replacement", result: sqlmock.NewResult(0, 1), updated: true},
		{name: "stale snapshot rejected", result: sqlmock.NewResult(0, 0)},
		{name: "new revocation clears state", result: sqlmock.NewResult(0, 1), updated: true, revocation: true},
		{name: "database failure", execErr: databaseErr, wantErr: databaseErr},
		{name: "affected-row failure", result: sqlmock.NewErrorResult(rowsErr), wantErr: rowsErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock, _ := newCodexTurnStateAtomicRepository(t)
			slot := service.CodexTurnStateModelExtraPrefix + base64.RawURLEncoding.EncodeToString([]byte("gpt-6-astra"))
			value := codexTurnStateAtomicTestValue()
			if tc.revocation {
				value[service.CodexTurnStateAutoExtraKey] = ""
				value[service.CodexTurnStateAutoSetAtExtraKey] = int64(0)
				value[service.CodexTurnStateAutoVerifiedAtExtraKey] = int64(0)
				value[service.CodexTurnStateAutoRecoveryExtraKey] = map[string]any{"invalidated_at_ms": int64(5_000), "pending": true}
			}
			payload, err := json.Marshal(value)
			require.NoError(t, err)
			expected := mock.ExpectExec(`(?s)UPDATE accounts.*WHERE id = \$3 AND deleted_at IS NULL`).WithArgs(slot, string(payload), int64(3))
			if tc.execErr != nil {
				expected.WillReturnError(tc.execErr)
			} else {
				expected.WillReturnResult(tc.result)
			}

			updated, err := repo.UpdateCodexTurnState(context.Background(), 3, slot, value)
			require.Equal(t, tc.updated, updated)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpdateCodexTurnStateAtomicMarshalFailureDoesNotDispatchSQL(t *testing.T) {
	repo, mock, query := newCodexTurnStateAtomicRepository(t)
	updated, err := repo.UpdateCodexTurnState(context.Background(), 3, "model-slot", map[string]any{"unsupported": make(chan int)})
	require.False(t, updated)
	require.ErrorContains(t, err, "json: unsupported type")
	require.Empty(t, *query)
	require.NoError(t, mock.ExpectationsWereMet())
}
