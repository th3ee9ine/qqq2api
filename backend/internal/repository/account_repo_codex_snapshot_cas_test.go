//go:build unit

package repository

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func codexSnapshotReadQuery() string {
	return regexp.QuoteMeta("SELECT extra->>'codex_usage_updated_at' FROM accounts WHERE id = $1 AND deleted_at IS NULL")
}

func codexSnapshotUpdateQuery() string {
	return `UPDATE accounts(?s).*IS NOT DISTINCT FROM \$3`
}

func codexSnapshotTestUpdate(t *testing.T, repo *accountRepository, candidate time.Time) (bool, error) {
	t.Helper()
	return repo.UpdateExtraIfCodexSnapshotNewer(context.Background(), 7, map[string]any{
		"codex_usage_updated_at": candidate.Format(time.RFC3339Nano),
		"codex_5h_used_percent":  42.0,
	}, candidate)
}

func TestUpdateExtraIfCodexSnapshotNewerUsesTimestampCAS(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := &accountRepository{sql: db}
	candidate := time.Date(2026, 10, 13, 10, 0, 0, 123000000, time.UTC)
	mock.ExpectQuery(codexSnapshotReadQuery()).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"codex_usage_updated_at"}).AddRow(nil))
	mock.ExpectExec(codexSnapshotUpdateQuery()).
		WithArgs(sqlmock.AnyArg(), int64(7), nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	applied, err := codexSnapshotTestUpdate(t, repo, candidate)
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateExtraIfCodexSnapshotNewerReportsStaleUpdateWithoutExec(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := &accountRepository{sql: db}
	candidate := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	existing := candidate.Add(time.Minute).Format(time.RFC3339Nano)
	mock.ExpectQuery(codexSnapshotReadQuery()).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"codex_usage_updated_at"}).AddRow(existing))

	applied, err := codexSnapshotTestUpdate(t, repo, candidate)
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateExtraIfCodexSnapshotNewerLosesCASConflictToNewerSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := &accountRepository{sql: db}
	candidate := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	initial := candidate.Add(-time.Minute).Format(time.RFC3339Nano)
	newer := candidate.Add(time.Minute).Format(time.RFC3339Nano)
	mock.ExpectQuery(codexSnapshotReadQuery()).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"codex_usage_updated_at"}).AddRow(initial))
	mock.ExpectExec(codexSnapshotUpdateQuery()).
		WithArgs(sqlmock.AnyArg(), int64(7), initial).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(codexSnapshotReadQuery()).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"codex_usage_updated_at"}).AddRow(newer))

	applied, err := codexSnapshotTestUpdate(t, repo, candidate)
	require.NoError(t, err)
	require.False(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateExtraIfCodexSnapshotNewerRetriesAfterUnrelatedCASConflict(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := &accountRepository{sql: db}
	candidate := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	initial := candidate.Add(-time.Minute).Format(time.RFC3339Nano)
	mock.ExpectQuery(codexSnapshotReadQuery()).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"codex_usage_updated_at"}).AddRow(initial))
	mock.ExpectExec(codexSnapshotUpdateQuery()).
		WithArgs(sqlmock.AnyArg(), int64(7), initial).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(codexSnapshotReadQuery()).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"codex_usage_updated_at"}).AddRow(initial))
	mock.ExpectExec(codexSnapshotUpdateQuery()).
		WithArgs(sqlmock.AnyArg(), int64(7), initial).
		WillReturnResult(sqlmock.NewResult(0, 1))

	applied, err := codexSnapshotTestUpdate(t, repo, candidate)
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateExtraIfCodexSnapshotNewerUsesPostgres15CompatibleCAS(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(_ string, actual string) error {
		if strings.HasPrefix(strings.TrimSpace(actual), "SELECT") {
			return nil
		}
		if !strings.Contains(actual, "UPDATE accounts") {
			return fmt.Errorf("query is not an account update: %s", actual)
		}
		if !strings.Contains(actual, "IS NOT DISTINCT FROM") {
			return fmt.Errorf("query does not use NULL-safe CAS: %s", actual)
		}
		if strings.Contains(actual, "pg_input_is_valid") || strings.Contains(actual, "::timestamptz") {
			return fmt.Errorf("query uses PostgreSQL 16-only timestamp validation: %s", actual)
		}
		return nil
	})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := &accountRepository{sql: db}
	candidate := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(codexSnapshotReadQuery()).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"codex_usage_updated_at"}).AddRow(nil))
	mock.ExpectExec(codexSnapshotUpdateQuery()).
		WithArgs(sqlmock.AnyArg(), int64(7), nil).
		WillReturnResult(sqlmock.NewResult(0, 1))

	applied, err := codexSnapshotTestUpdate(t, repo, candidate)
	require.NoError(t, err)
	require.True(t, applied)
	require.NoError(t, mock.ExpectationsWereMet())
}
