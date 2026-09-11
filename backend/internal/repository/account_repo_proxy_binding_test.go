//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	dbent "github.com/th3ee9ine/qqq2api/ent"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

const proxyBindingShadowUpdatePattern = `(?s)UPDATE accounts.*SET proxy_id = \$2,.*WHERE parent_account_id = ANY\(\$1\).*quota_dimension = \$3 AND deleted_at IS NULL.*RETURNING id`

func TestBulkProxyBindingCommitsOnlyCompleteSourceMatches(t *testing.T) {
	for _, tc := range []struct {
		name             string
		target, affected int64
	}{
		{name: "unbind", target: 0, affected: 2},
		{name: "move", target: 9, affected: 2},
		{name: "partially stale selection rolls back", target: 9, affected: 1},
		{name: "deleted selection rolls back", target: 0, affected: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			mock.ExpectBegin()
			if tc.target > 0 {
				mock.ExpectQuery(`(?s)SELECT id FROM proxies.*deleted_at IS NULL AND status = \$2.*expires_at > NOW\(\).*FOR SHARE`).
					WithArgs(tc.target, service.StatusActive).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(tc.target))
				mock.ExpectExec(`(?s)UPDATE accounts SET proxy_id = \$1,.*WHERE id = ANY\(\$2\) AND deleted_at IS NULL AND proxy_id = \$3 AND parent_account_id IS NULL`).
					WithArgs(tc.target, "{11,12}", int64(7)).WillReturnResult(sqlmock.NewResult(0, tc.affected))
			} else {
				mock.ExpectExec(`(?s)UPDATE accounts SET proxy_id = NULL,.*WHERE id = ANY\(\$1\) AND deleted_at IS NULL AND proxy_id = \$2 AND parent_account_id IS NULL`).
					WithArgs("{11,12}", int64(7)).WillReturnResult(sqlmock.NewResult(0, tc.affected))
			}
			if tc.affected == 2 {
				var target any
				if tc.target > 0 {
					target = tc.target
				}
				mock.ExpectQuery(proxyBindingShadowUpdatePattern).
					WithArgs("{11,12}", target, service.QuotaDimensionSpark).
					WillReturnRows(sqlmock.NewRows([]string{"id"}))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
					WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, sqlmock.AnyArg()).
					WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			source := int64(7)
			repo := newAccountRepositoryWithSQL(client, db, nil)
			rows, err := repo.BulkUpdate(context.Background(), []int64{11, 11, 12}, service.AccountBulkUpdate{ProxyID: &tc.target, ExpectedProxyID: &source})
			if tc.affected == 2 {
				require.NoError(t, err)
				require.Equal(t, int64(2), rows)
			} else {
				require.ErrorIs(t, err, service.ErrProxyBindingChanged)
				require.Zero(t, rows)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type proxyBindingSchedulerRecorder struct {
	service.SchedulerCache
	accounts []*service.Account
}

func (s *proxyBindingSchedulerRecorder) SetAccount(_ context.Context, account *service.Account) error {
	s.accounts = append(s.accounts, account)
	return nil
}

func TestBulkProxyBindingUpdatesShadowsAndSchedulerInOneOperation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts SET proxy_id = NULL,.*AND proxy_id = \$2 AND parent_account_id IS NULL`).
		WithArgs("{11,12}", int64(7)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectQuery(proxyBindingShadowUpdatePattern).
		WithArgs("{11,12}", nil, service.QuotaDimensionSpark).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(21)).AddRow(int64(22)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, []byte(`{"account_ids":[11,12,21,22]}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	// Snapshot reads happen after commit and include every inherited account.
	mock.ExpectQuery(`(?s)SELECT .* FROM "accounts" WHERE "accounts"\."id" IN \(\$1, \$2, \$3, \$4\)`).
		WithArgs(int64(11), int64(12), int64(21), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "proxy_id", "parent_account_id"}).
			AddRow(int64(11), nil, nil).AddRow(int64(12), nil, nil).
			AddRow(int64(21), nil, int64(11)).AddRow(int64(22), nil, int64(12)))
	mock.ExpectQuery(`(?s)SELECT .* FROM "account_groups".*`).
		WithArgs(int64(11), int64(12), int64(21), int64(22)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id"}))
	source, target := int64(7), int64(0)
	cache := &proxyBindingSchedulerRecorder{}
	repo := newAccountRepositoryWithSQL(client, db, cache)
	rows, err := repo.BulkUpdate(context.Background(), []int64{11, 12}, service.AccountBulkUpdate{ProxyID: &target, ExpectedProxyID: &source})
	require.NoError(t, err)
	require.Equal(t, int64(2), rows, "response counts selected parents, not inherited shadows")
	require.Len(t, cache.accounts, 4)
	var cachedIDs []int64
	for _, account := range cache.accounts {
		cachedIDs = append(cachedIDs, account.ID)
		require.Nil(t, account.ProxyID)
	}
	require.Equal(t, []int64{11, 12, 21, 22}, cachedIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkProxyBindingRollsBackParentsWhenShadowPropagationFails(t *testing.T) {
	for _, stage := range []string{"shadow update", "shadow result", "outbox"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			t.Cleanup(func() { _ = client.Close() })
			failure := errors.New("injected " + stage + " failure")
			mock.ExpectBegin()
			mock.ExpectQuery(`(?s)SELECT id FROM proxies.*FOR SHARE`).WithArgs(int64(9), service.StatusActive).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
			mock.ExpectExec(`(?s)UPDATE accounts SET proxy_id = \$1,.*AND proxy_id = \$3 AND parent_account_id IS NULL`).
				WithArgs(int64(9), "{11}", int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
			shadowUpdate := mock.ExpectQuery(proxyBindingShadowUpdatePattern).
				WithArgs("{11}", int64(9), service.QuotaDimensionSpark)
			if stage == "shadow update" {
				shadowUpdate.WillReturnError(failure)
			} else if stage == "shadow result" {
				shadowUpdate.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(21)).RowError(0, failure))
			} else {
				shadowUpdate.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(21)))
				mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).WillReturnError(failure)
			}
			mock.ExpectRollback()
			source, target := int64(7), int64(9)
			cache := &proxyBindingSchedulerRecorder{}
			repo := newAccountRepositoryWithSQL(client, db, cache)
			rows, err := repo.BulkUpdate(context.Background(), []int64{11}, service.AccountBulkUpdate{ProxyID: &target, ExpectedProxyID: &source})
			require.Zero(t, rows)
			require.ErrorIs(t, err, failure)
			require.Empty(t, cache.accounts, "rolled-back bindings must not reach the scheduler")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestBulkProxyBindingRechecksTargetInsideTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id FROM proxies.*FOR SHARE`).WithArgs(int64(9), service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	source, target := int64(7), int64(9)
	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{11}, service.AccountBulkUpdate{ProxyID: &target, ExpectedProxyID: &source})
	require.Zero(t, rows)
	require.ErrorIs(t, err, service.ErrProxyBindingTargetUnavailable)
	require.NoError(t, mock.ExpectationsWereMet(), "an unavailable target must prevent every account write")
}
