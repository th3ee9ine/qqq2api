package repository

import (
	"context"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	dbent "github.com/th3ee9ine/qqq2api/ent"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

const scopedSupplyLockPattern = `(?s)SELECT supply_rate_multiplier FROM users.*id = \$1 AND role = \$2 AND status = \$3 AND deleted_at IS NULL.*FOR SHARE`

func scopedSupplyTestContext() context.Context {
	return context.WithValue(context.Background(), ctxkey.AccountAdminID, int64(42))
}

func scopedSupplyTestAccount() *service.Account {
	return &service.Account{
		Name: "supplied-account", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"}, Extra: map[string]any{},
		Concurrency: 1, Priority: 50, Status: service.StatusActive, Schedulable: true,
	}
}

func newScopedSupplyTestRepo(t *testing.T) (*accountRepository, sqlmock.Sqlmock, *dbent.Client) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), mock, client
}

func expectScopedSupplyLock(mock sqlmock.Sqlmock, multiplier float64) {
	mock.ExpectQuery(scopedSupplyLockPattern).
		WithArgs(int64(42), service.RoleAccountAdmin, service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"supply_rate_multiplier"}).AddRow(multiplier))
}

func expectScopedSupplyAccountCreate(mock sqlmock.Sqlmock, id int64) {
	mock.ExpectQuery(`(?s)INSERT INTO .*accounts.*RETURNING.*`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(id))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload, dedup_key)")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, id, nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestScopedAccountCreateLocksOwnerUntilCommit(t *testing.T) {
	repo, mock, _ := newScopedSupplyTestRepo(t)
	mock.ExpectBegin()
	expectScopedSupplyLock(mock, 0.375)
	expectScopedSupplyAccountCreate(mock, 29)
	mock.ExpectCommit()

	account := scopedSupplyTestAccount()
	err := repo.Create(scopedSupplyTestContext(), account)
	require.NoError(t, err)
	require.Equal(t, int64(29), account.ID)
	require.Equal(t, int64(42), *account.AccountAdminID)
	require.Equal(t, 0.375, *account.RateMultiplier)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScopedAccountCreateRollsBackWhenOwnerIsInactive(t *testing.T) {
	repo, mock, _ := newScopedSupplyTestRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(scopedSupplyLockPattern).
		WithArgs(int64(42), service.RoleAccountAdmin, service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"supply_rate_multiplier"}))
	mock.ExpectRollback()

	err := repo.Create(scopedSupplyTestContext(), scopedSupplyTestAccount())
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScopedAutomaticProxyCreateLocksOwnerBeforeProxy(t *testing.T) {
	repo, mock, _ := newScopedSupplyTestRepo(t)
	mock.ExpectBegin()
	expectScopedSupplyLock(mock, 0.375)
	mock.ExpectQuery(`(?s)SELECT id, max_accounts.*FOR NO KEY UPDATE`).
		WithArgs(service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"id", "max_accounts"}).AddRow(int64(7), 2))
	expectScopedSupplyLock(mock, 0.375)
	mock.ExpectQuery(`(?s)INSERT INTO .*accounts.*RETURNING.*`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(31)))
	mock.ExpectQuery(`(?s)SELECT proxy_id, COUNT\(\*\).*GROUP BY proxy_id`).
		WithArgs(pq.Array([]int64{7}), pq.Array([]int64{31})).
		WillReturnRows(sqlmock.NewRows([]string{"proxy_id", "count"}))
	mock.ExpectExec(`(?s)UPDATE accounts AS a.*assignment\.proxy_id.*a\.account_admin_id = \$3`).
		WithArgs(pq.Array([]int64{31}), pq.Array([]int64{7}), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload, dedup_key)")).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(31), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	account := scopedSupplyTestAccount()
	err := repo.CreateWithAutomaticProxy(scopedSupplyTestContext(), account)
	require.NoError(t, err)
	require.Equal(t, int64(7), *account.ProxyID)
	require.Equal(t, 0.375, *account.RateMultiplier)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScopedAccountDuplicateCreationReusesExternalTransaction(t *testing.T) {
	repo, mock, client := newScopedSupplyTestRepo(t)
	mock.ExpectBegin()
	tx, err := client.Tx(context.Background())
	require.NoError(t, err)
	expectScopedSupplyLock(mock, 0.5)
	expectScopedSupplyAccountCreate(mock, 30)
	mock.ExpectCommit()

	account := scopedSupplyTestAccount()
	err = repo.CreateWithAccountGroups(dbent.NewTxContext(scopedSupplyTestContext(), tx), account, nil)
	require.NoError(t, err)
	require.Equal(t, 0.5, *account.RateMultiplier)
	require.NoError(t, tx.Commit(), "repository must not commit the caller's transaction")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestScopedBulkUpdateLocksOwnerBeforeUsingCurrentRate(t *testing.T) {
	repo, mock, _ := newScopedSupplyTestRepo(t)
	mock.ExpectBegin()
	expectScopedSupplyLock(mock, 0.375)
	mock.ExpectExec(`(?s)UPDATE accounts SET.*rate_multiplier = \(SELECT supply_rate_multiplier FROM users WHERE id = \$2.*WHERE id = ANY\(\$4\).*account_admin_id = \$5`).
		WithArgs("scoped", int64(42), sqlmock.AnyArg(), pq.Array([]int64{27}), int64(42), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, accountIDsPayloadMatcher{want: []int64{27}}).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	name := "scoped"
	forgedRate := 99.0
	rows, err := repo.BulkUpdate(scopedSupplyTestContext(), []int64{27}, service.AccountBulkUpdate{
		Name: &name, RateMultiplier: &forgedRate,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	require.NoError(t, mock.ExpectationsWereMet())
}
