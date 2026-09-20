//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
)

func TestProxyAccountSummariesPreserveParentAccountRelationship(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT id, name, platform, type, notes, parent_account_id.*FROM accounts.*WHERE proxy_id = \$1 AND deleted_at IS NULL.*ORDER BY id DESC`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "type", "notes", "parent_account_id"}).
			AddRow(int64(12), "spark", "openai", "oauth", "linked quota", int64(11)).
			AddRow(int64(11), "parent", "openai", "oauth", nil, nil))

	summaries, err := newProxyRepositoryWithSQL(nil, db).ListAccountSummariesByProxyID(context.Background(), 7)

	require.NoError(t, err)
	require.Len(t, summaries, 2)
	require.Equal(t, int64(12), summaries[0].ID)
	require.NotNil(t, summaries[0].ParentAccountID)
	require.Equal(t, int64(11), *summaries[0].ParentAccountID)
	require.NotNil(t, summaries[0].Notes)
	require.Equal(t, "linked quota", *summaries[0].Notes)
	require.Equal(t, int64(11), summaries[1].ID)
	require.Nil(t, summaries[1].ParentAccountID)
	require.Nil(t, summaries[1].Notes)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyAccountQueriesRespectAccountAdminScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.WithValue(context.Background(), ctxkey.AccountAdminID, int64(41))
	repo := newProxyRepositoryWithSQL(nil, db)

	mock.ExpectQuery(`(?s)SELECT COUNT\(\*\) FROM accounts WHERE proxy_id = \$1 AND deleted_at IS NULL AND account_admin_id = \$2`).
		WithArgs(int64(7), int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(2)))
	count, err := repo.CountAccountsByProxyID(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)

	mock.ExpectQuery(`(?s)SELECT id, name, platform, type, notes, parent_account_id.*FROM accounts.*WHERE proxy_id = \$1 AND deleted_at IS NULL AND account_admin_id = \$2.*ORDER BY id DESC`).
		WithArgs(int64(7), int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "type", "notes", "parent_account_id"}).
			AddRow(int64(12), "owned", "openai", "oauth", nil, nil))
	summaries, err := repo.ListAccountSummariesByProxyID(ctx, 7)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, int64(12), summaries[0].ID)

	mock.ExpectQuery(`(?s)SELECT proxy_id, COUNT\(\*\) AS count FROM accounts WHERE proxy_id IS NOT NULL AND deleted_at IS NULL AND account_admin_id = \$1 GROUP BY proxy_id`).
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"proxy_id", "count"}).AddRow(int64(7), int64(2)))
	counts, err := repo.GetAccountCountsForProxies(ctx)
	require.NoError(t, err)
	require.Equal(t, map[int64]int64{7: 2}, counts)
	require.NoError(t, mock.ExpectationsWereMet())
}
