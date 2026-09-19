package repository

import (
	"context"
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
		service.CodexTurnStateAutoExtraKey:                 "opaque-atomic-test-state",
		service.CodexTurnStateAutoSetAtExtraKey:            int64(1_000),
		service.CodexTurnStateAutoVerifiedAtExtraKey:       int64(2_000),
		service.CodexTurnStateAutoVerifiedModelExtraKey:    "gpt-6-astra",
		service.CodexTurnStateAutoProbeAtExtraKey:          int64(3_000),
		service.CodexTurnStateAutoProbeCompletedAtExtraKey: int64(3_500),
		service.CodexTurnStateAutoLastErrorExtraKey:        "",
		service.CodexTurnStateAutoProbeModelExtraKey:       "gpt-6-astra",
		service.CodexTurnStateAutoProbeNotBeforeExtraKey:   int64(4_000),
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
	mock.ExpectQuery(`(?s)WITH locked AS.*UPDATE accounts AS account.*RETURNING merged\.fully_won`).
		WithArgs(slot, string(payload), int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"fully_won"}).AddRow(true))

	updated, err := repo.UpdateCodexTurnState(context.Background(), 3, slot, value)
	require.NoError(t, err)
	require.True(t, updated)
	require.NoError(t, mock.ExpectationsWereMet())

	sqlText := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(*query, " "))
	require.Contains(t, sqlText, "WHERE id = $3 AND deleted_at IS NULL FOR UPDATE")
	require.Contains(t, sqlText, "ARRAY[$1]::text[]")
	require.Contains(t, sqlText, "$2::jsonb AS incoming_slot")
	require.NotContains(t, sqlText, value[service.CodexTurnStateAutoExtraKey])
	require.Contains(t, sqlText, "incoming_epoch > old_epoch AS generation_wins")
	require.Contains(t, sqlText, "ROW(old_probe_at, old_probe_completed_at) <= ROW(incoming_probe_at, incoming_probe_completed_at) AS outcome_wins")
	require.Contains(t, sqlText, "ROW(old_verified_at, old_set_at) <= ROW(incoming_verified_at, incoming_set_at) AS state_wins")
	require.Contains(t, sqlText, "generation_wins OR (incoming_epoch = old_epoch AND outcome_wins AND state_wins) AS fully_won")

	for _, key := range []string{
		service.CodexTurnStateAutoProbeAtExtraKey,
		service.CodexTurnStateAutoProbeCompletedAtExtraKey,
		service.CodexTurnStateAutoLastErrorExtraKey,
		service.CodexTurnStateAutoProbeModelExtraKey,
	} {
		require.Contains(t, sqlText, "'"+key+"', incoming_slot -> '"+key+"'", "outcome field must advance as one versioned group")
	}
	for _, key := range []string{
		service.CodexTurnStateAutoExtraKey,
		service.CodexTurnStateAutoSetAtExtraKey,
		service.CodexTurnStateAutoVerifiedAtExtraKey,
		service.CodexTurnStateAutoVerifiedModelExtraKey,
	} {
		require.Contains(t, sqlText, "'"+key+"', incoming_slot -> '"+key+"'", "verified state field must advance as one versioned group")
	}
	require.Contains(t, sqlText, "CASE WHEN state_wins AND successful_state THEN jsonb_build_object( '"+service.CodexTurnStateAutoRecoveryExtraKey+"', jsonb_set(")
	require.Contains(t, sqlText, "ARRAY['pending']::text[], 'false'::jsonb", "only a successful state winner clears recovery pending")

	boundary := service.CodexTurnStateAutoProbeNotBeforeExtraKey
	require.Contains(t, sqlText, "GREATEST( COALESCE((old_extra ->> '"+boundary+"')::bigint, 0), COALESCE((old_slot ->> '"+boundary+"')::bigint, 0), COALESCE((incoming_slot ->> '"+boundary+"')::bigint, 0) ) AS probe_not_before")
	require.Contains(t, sqlText, "'"+boundary+"', probe_not_before")
	require.Contains(t, sqlText, "ARRAY['"+boundary+"']::text[]")
	require.Contains(t, sqlText, "SELECT COALESCE((SELECT fully_won FROM updated), false)")
}

func TestUpdateCodexTurnStateAtomicWinnerResultAndDatabaseErrors(t *testing.T) {
	databaseErr := errors.New("atomic update database failure")
	returnedRowErr := errors.New("atomic update returned-row failure")
	for _, tc := range []struct {
		name       string
		queryErr   error
		rowErr     error
		wantErr    error
		updated    bool
		revocation bool
	}{
		{name: "complete snapshot wins", updated: true},
		{name: "partial merge requires reconciliation"},
		{name: "new recovery generation wins completely", updated: true, revocation: true},
		{name: "database failure", queryErr: databaseErr, wantErr: databaseErr},
		{name: "returned row failure", rowErr: returnedRowErr, wantErr: returnedRowErr},
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
			expected := mock.ExpectQuery(`(?s)WITH locked AS.*UPDATE accounts AS account.*RETURNING merged\.fully_won`).WithArgs(slot, string(payload), int64(3))
			if tc.queryErr != nil {
				expected.WillReturnError(tc.queryErr)
			} else {
				rows := sqlmock.NewRows([]string{"fully_won"}).AddRow(tc.updated)
				if tc.rowErr != nil {
					rows.RowError(0, tc.rowErr)
				}
				expected.WillReturnRows(rows)
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
