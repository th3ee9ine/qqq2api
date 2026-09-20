//go:build unit

package repository

import (
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUsageLogTrendQueriesApplyAccountAdminScope(t *testing.T) {
	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	ctx := accountAdminTestContext()

	t.Run("api key trend scopes both ranking and output", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $6 AND deleted_at IS NULL)")).
			WithArgs(start, end, 5, start, end, int64(41)).
			WillReturnRows(rowsForAPIKeyUsageTrend())

		got, err := repo.GetAPIKeyUsageTrend(ctx, start, end, "day", 5)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("user trend scopes both ranking and output", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $6 AND deleted_at IS NULL)")).
			WithArgs(start, end, 5, start, end, int64(41)).
			WillReturnRows(rowsForUserUsageTrend())

		got, err := repo.GetUserUsageTrend(ctx, start, end, "day", 5)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("spending ranking scopes source rows", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("u.account_id IN (SELECT id FROM accounts WHERE account_admin_id = $4 AND deleted_at IS NULL)")).
			WithArgs(start, end, 5, int64(41)).
			WillReturnRows(rowsForUserSpendingRanking())

		got, err := repo.GetUserSpendingRanking(ctx, start, end, 5)
		require.NoError(t, err)
		require.Len(t, got.Ranking, 1)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("user detail trend scopes source rows", func(t *testing.T) {
		db, mock := newSQLMock(t)
		repo := &usageLogRepository{sql: db}
		mock.ExpectQuery(regexp.QuoteMeta("account_id IN (SELECT id FROM accounts WHERE account_admin_id = $4 AND deleted_at IS NULL)")).
			WithArgs(int64(9), start, end, int64(41)).
			WillReturnRows(rowsForUserDetailTrend())

		got, err := repo.GetUserUsageTrendByUserID(ctx, 9, start, end, "day")
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func rowsForAPIKeyUsageTrend() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"date", "api_key_id", "key_name", "requests", "tokens"}).
		AddRow("2026-09-19", int64(3), "key", int64(1), int64(20))
}

func rowsForUserUsageTrend() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"date", "user_id", "email", "username", "requests", "tokens", "cost", "actual_cost"}).
		AddRow("2026-09-19", int64(9), "user@example.com", "user", int64(1), int64(20), 0.4, 0.3)
}

func rowsForUserSpendingRanking() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"user_id", "email", "username", "actual_cost", "requests", "tokens", "total_actual_cost", "total_requests", "total_tokens"}).
		AddRow(int64(9), "user@example.com", "user", 0.3, int64(1), int64(20), 0.3, int64(1), int64(20))
}

func rowsForUserDetailTrend() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost"}).
		AddRow("2026-09-19", int64(1), int64(10), int64(10), int64(0), int64(0), int64(20), 0.4, 0.3)
}
