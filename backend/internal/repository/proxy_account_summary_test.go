//go:build unit

package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
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
