package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	dbent "github.com/th3ee9ine/qqq2api/ent"
	_ "github.com/th3ee9ine/qqq2api/ent/runtime"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestCodexTurnStateSourceProjection(t *testing.T) {
	var query string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &query}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)
	mock.ExpectQuery("state projection").WithArgs(int64(12)).WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "extra"}).AddRow(int64(12), service.PlatformOpenAI, service.AccountTypeOAuth, []byte(`{"codex_turn_state_auto_model:Z3B0LTUuNQ":{"codex_turn_state_auto":"state"}}`)))
	row, err := repo.GetCodexTurnStateSource(context.Background(), 12)
	require.NoError(t, err)
	require.Equal(t, int64(12), row.ID)
	require.Equal(t, service.PlatformOpenAI, row.Platform)
	require.Contains(t, row.Extra, "codex_turn_state_auto_model:Z3B0LTUuNQ")
	require.Empty(t, row.Credentials)
	require.NotContains(t, query, "credentials")
	require.NotContains(t, query, "account_groups")
	require.Contains(t, query, "deleted_at")
	require.NoError(t, mock.ExpectationsWereMet())
}
