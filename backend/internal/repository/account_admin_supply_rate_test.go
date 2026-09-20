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

const accountAdminSupplyRateUpdatePattern = `(?s)UPDATE accounts.*SET rate_multiplier = \$1.*upstream_billing_rate_sync_enabled.*WHERE account_admin_id = \$2.*deleted_at IS NULL.*RETURNING id`
const accountAdminOwnershipReleasePattern = `(?s)UPDATE accounts.*account_admin_id = NULL.*rate_multiplier = 1\.0.*upstream_billing_probe_enabled.*upstream_billing_rate_sync_enabled.*WHERE account_admin_id = \$1.*RETURNING id`

func TestUpdateSupplyRateMultiplierByAccountAdminOwnsTransactionAndOutbox(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(accountAdminSupplyRateUpdatePattern).
		WithArgs(0.375, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(29)).AddRow(int64(11)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, accountIDsPayloadMatcher{want: []int64{11, 29}}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	accountIDs, err := repo.UpdateSupplyRateMultiplierByAccountAdmin(context.Background(), 42, 0.375)

	require.NoError(t, err)
	require.Equal(t, []int64{11, 29}, accountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateSupplyRateMultiplierByAccountAdminUsesExternalTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	txCtx := dbent.NewTxContext(context.Background(), tx)
	mock.ExpectQuery(accountAdminSupplyRateUpdatePattern).
		WithArgs(0.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, accountIDsPayloadMatcher{want: []int64{11}}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	accountIDs, err := repo.UpdateSupplyRateMultiplierByAccountAdmin(txCtx, 42, 0)
	require.NoError(t, err)
	require.Equal(t, []int64{11}, accountIDs)
	require.NoError(t, tx.Commit(), "repository must leave the external transaction open")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateSupplyRateMultiplierByAccountAdminSkipsOutboxWithoutAccounts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(accountAdminSupplyRateUpdatePattern).
		WithArgs(0.5, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	accountIDs, err := repo.UpdateSupplyRateMultiplierByAccountAdmin(context.Background(), 42, 0.5)

	require.NoError(t, err)
	require.Empty(t, accountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateSupplyRateMultiplierByAccountAdminRollsBackWhenOutboxFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	failure := errors.New("outbox unavailable")

	mock.ExpectBegin()
	mock.ExpectQuery(accountAdminSupplyRateUpdatePattern).
		WithArgs(0.5, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, accountIDsPayloadMatcher{want: []int64{11}}).
		WillReturnError(failure)
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	accountIDs, err := repo.UpdateSupplyRateMultiplierByAccountAdmin(context.Background(), 42, 0.5)

	require.ErrorIs(t, err, failure)
	require.Nil(t, accountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReleaseAccountAdminOwnershipResetsRateAndQueuesSchedulerRefresh(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(accountAdminOwnershipReleasePattern).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(29)).AddRow(int64(11)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, accountIDsPayloadMatcher{want: []int64{11, 29}}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	accountIDs, err := repo.ReleaseAccountAdminOwnership(context.Background(), 42)

	require.NoError(t, err)
	require.Equal(t, []int64{11, 29}, accountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReleaseAccountAdminOwnershipSkipsOutboxWhenNoAccountsMatch(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(accountAdminOwnershipReleasePattern).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	accountIDs, err := repo.ReleaseAccountAdminOwnership(context.Background(), 42)

	require.NoError(t, err)
	require.Empty(t, accountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}
