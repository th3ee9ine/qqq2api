package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	dbent "github.com/th3ee9ine/qqq2api/ent"
	_ "github.com/th3ee9ine/qqq2api/ent/runtime"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func newCodexTurnStateProvenanceRepository(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), mock
}

func TestRecordCodexTurnStateProvenanceUsesBoundedAtomicDigestMap(t *testing.T) {
	repo, mock := newCodexTurnStateProvenanceRepository(t)
	digest := "50af1f5f7c631a76422f3b9d8a12a2eb6bc55f827e461f63f4f7b0b3a14c2f15"
	expiresAt := time.Now().Add(time.Hour).UnixMilli()
	mock.ExpectExec(`(?s)UPDATE accounts.*jsonb_set.*jsonb_each.*LIMIT \$3.*WHERE id = \$5 AND deleted_at IS NULL`).
		WithArgs(
			service.CodexTurnStateNativeProvenanceExtraKey,
			sqlmock.AnyArg(),
			service.CodexTurnStateNativeProvenanceLimit-1,
			sqlmock.AnyArg(),
			int64(12),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.RecordCodexTurnStateProvenance(context.Background(), 12, "gpt-6-astra", digest, expiresAt)
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFindCodexTurnStateProvenanceReturnsOnlyDigestMetadata(t *testing.T) {
	repo, mock := newCodexTurnStateProvenanceRepository(t)
	digest := "50af1f5f7c631a76422f3b9d8a12a2eb6bc55f827e461f63f4f7b0b3a14c2f15"
	expiresAt := time.Now().Add(time.Hour).UnixMilli()
	mock.ExpectQuery(`(?s)SELECT id, extra -> \$1 -> \$2.*AND extra @> \$3::jsonb`).
		WithArgs(service.CodexTurnStateNativeProvenanceExtraKey, digest, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "origin"}).
			AddRow(int64(12), []byte(`{"model":"gpt-6-astra","expires_at_ms":`+strconv.FormatInt(expiresAt, 10)+`}`)).
			AddRow(int64(13), []byte(`{"model":`)))

	origins, err := repo.FindCodexTurnStateProvenance(context.Background(), digest)
	require.NoError(t, err)
	require.Equal(t, []service.CodexTurnStateNativeProvenance{{
		AccountID: 12, Model: "gpt-6-astra", ExpiresAtMS: expiresAt,
	}}, origins)
	require.NoError(t, mock.ExpectationsWereMet())
}
