//go:build integration

package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/timezone"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// TestUsageAccountDailyRollupPreservesTodayStatsAfterUsageLogCleanup guards the
// contract exposed by the Accounts page: deleting raw usage_logs rows must not
// reset today's request/token/account-cost/system-cost counters.  The
// usage_account_daily_rollups trigger is expected to have populated the bucket
// before the rows are removed.
func (s *UsageLogRepoSuite) TestUsageAccountDailyRollupPreservesTodayStatsAfterUsageLogCleanup() {
	t := s.T()
	ctx := context.Background()

	user := mustCreateUser(t, s.client, &service.User{Email: "acct-daily-rollup-today@example.com"})
	apiKey := mustCreateApiKey(t, s.client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-acct-daily-rollup-today-" + uuid.NewString(),
		Name:   "daily-rollup",
	})
	// Keep the account creation timestamp before the synthetic requests even
	// when the test runs close to a day boundary.
	now := timezone.Now()
	today := timezone.StartOfDay(now)
	account := mustCreateAccount(t, s.client, &service.Account{
		Name:      "acc-daily-rollup-today-" + uuid.NewString(),
		CreatedAt: today.Add(-24 * time.Hour),
	})

	accountMultiplier := 1.5
	accountStatsCost := 0.7
	// Use timestamps that are in today's bucket and not in the future.  At the
	// first instant of a day there may not be two distinct past minutes, so use
	// two points between midnight and `now` in that narrow edge case.
	createdAt := []time.Time{now.Add(-2 * time.Minute), now.Add(-time.Minute)}
	if now.Sub(today) < 2*time.Minute {
		createdAt = []time.Time{
			today.Add(now.Sub(today) / 3),
			today.Add(now.Sub(today) * 2 / 3),
		}
	}
	for i, values := range []struct {
		input, output int
		standard      float64
		userCost      float64
	}{
		{input: 10, output: 20, standard: 1.0, userCost: 0.8},
		{input: 5, output: 5, standard: 0.5, userCost: 0.4},
	} {
		log := &service.UsageLog{
			UserID:                user.ID,
			APIKeyID:              apiKey.ID,
			AccountID:             account.ID,
			RequestID:             uuid.NewString(),
			Model:                 "rollup-test-model",
			InputTokens:           values.input,
			OutputTokens:          values.output,
			TotalCost:             values.standard,
			ActualCost:            values.userCost,
			AccountRateMultiplier: &accountMultiplier,
			AccountStatsCost:      &accountStatsCost,
			CreatedAt:             createdAt[i],
		}
		_, err := s.repo.Create(ctx, log)
		require.NoError(t, err)
	}

	before, err := s.repo.GetAccountTodayStats(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), before.Requests)
	require.Equal(t, int64(40), before.Tokens)
	// account_cost = (0.7 + 0.7) * 1.5; standard/user costs are retained
	// separately for the system and API-key billing columns.
	require.InEpsilon(t, 2.1, before.Cost, 1e-9)
	require.InEpsilon(t, 1.5, before.StandardCost, 1e-9)
	require.InEpsilon(t, 1.2, before.UserCost, 1e-9)

	_, err = s.repo.sql.ExecContext(ctx, "DELETE FROM usage_logs WHERE account_id = $1", account.ID)
	require.NoError(t, err)
	var remaining int64
	// Keep the count query in the same transaction as the delete and consume it
	// explicitly so the test does not leak a cursor.
	rows, queryErr := s.repo.sql.QueryContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE account_id = $1", account.ID)
	require.NoError(t, queryErr)
	require.True(t, rows.Next())
	require.NoError(t, rows.Scan(&remaining))
	require.NoError(t, rows.Close())
	require.Equal(t, int64(0), remaining)

	after, err := s.repo.GetAccountTodayStats(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, before.Requests, after.Requests)
	require.Equal(t, before.Tokens, after.Tokens)
	require.InEpsilon(t, before.Cost, after.Cost, 1e-9)
	require.InEpsilon(t, before.StandardCost, after.StandardCost, 1e-9)
	require.InEpsilon(t, before.UserCost, after.UserCost, 1e-9)

	batch, err := s.repo.GetAccountTodayStatsBatch(ctx, []int64{account.ID})
	require.NoError(t, err)
	require.Equal(t, after.Requests, batch[account.ID].Requests)
	require.Equal(t, after.Tokens, batch[account.ID].Tokens)
	require.InEpsilon(t, after.Cost, batch[account.ID].Cost, 1e-9)
}

// TestUsageAccountDailyRollupPreservesLifetimeStatsAfterUsageLogCleanup
// verifies the account-statistics column (lifetime batch endpoint) and the
// modal's summary source survive physical deletion of every source row.
func (s *UsageLogRepoSuite) TestUsageAccountDailyRollupPreservesLifetimeStatsAfterUsageLogCleanup() {
	t := s.T()
	ctx := context.Background()
	today := timezone.Today()
	now := timezone.Now()

	user := mustCreateUser(t, s.client, &service.User{Email: "acct-daily-rollup-life@example.com"})
	apiKey := mustCreateApiKey(t, s.client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-acct-daily-rollup-life-" + uuid.NewString(),
		Name:   "daily-rollup",
	})
	account := mustCreateAccount(t, s.client, &service.Account{
		Name:      "acc-daily-rollup-life-" + uuid.NewString(),
		CreatedAt: today.Add(-72 * time.Hour),
	})

	for i, values := range []struct {
		at            time.Time
		input, output int
		standard      float64
		userCost      float64
	}{
		{at: today.Add(-48 * time.Hour).Add(time.Hour), input: 11, output: 19, standard: 1.1, userCost: 0.9},
		{at: today.Add(-24 * time.Hour).Add(time.Hour), input: 13, output: 17, standard: 1.3, userCost: 1.0},
		// Keep the newest fixture strictly in the past so the assertion also
		// covers the legacy lifetime query's created_at <= NOW() boundary.
		{at: now.Add(-time.Hour), input: 7, output: 9, standard: 0.7, userCost: 0.5},
	} {
		log := &service.UsageLog{
			UserID:       user.ID,
			APIKeyID:     apiKey.ID,
			AccountID:    account.ID,
			RequestID:    uuid.NewString(),
			Model:        "rollup-lifetime-model",
			InputTokens:  values.input,
			OutputTokens: values.output,
			TotalCost:    values.standard,
			ActualCost:   values.userCost,
			CreatedAt:    values.at.Add(time.Duration(i) * time.Minute),
		}
		_, err := s.repo.Create(ctx, log)
		require.NoError(t, err)
	}

	beforeMap, err := s.repo.GetAccountLifetimeStatsBatch(ctx, []int64{account.ID})
	require.NoError(t, err)
	before := beforeMap[account.ID]
	require.NotNil(t, before)
	require.Equal(t, int64(3), before.Requests)
	require.Equal(t, int64(76), before.Tokens)
	require.InEpsilon(t, 3.1, before.Cost, 1e-9)
	require.InEpsilon(t, 3.1, before.StandardCost, 1e-9)
	require.InEpsilon(t, 2.4, before.UserCost, 1e-9)

	_, err = s.repo.sql.ExecContext(ctx, "DELETE FROM usage_logs WHERE account_id = $1", account.ID)
	require.NoError(t, err)

	afterMap, err := s.repo.GetAccountLifetimeStatsBatch(ctx, []int64{account.ID})
	require.NoError(t, err)
	after := afterMap[account.ID]
	require.NotNil(t, after)
	require.Equal(t, before.Requests, after.Requests)
	require.Equal(t, before.Tokens, after.Tokens)
	require.InEpsilon(t, before.Cost, after.Cost, 1e-9)
	require.InEpsilon(t, before.StandardCost, after.StandardCost, 1e-9)
	require.InEpsilon(t, before.UserCost, after.UserCost, 1e-9)
}

// TestUsageAccountDailyRollupPreservesAggregatedStatsAfterUsageLogCleanup
// covers the account usage modal's aggregate endpoint in addition to the
// lightweight table endpoints above.  The modal requests a day-aligned range
// and expects the history/summary counters to remain available after the raw
// rows are purged.
func (s *UsageLogRepoSuite) TestUsageAccountDailyRollupPreservesAggregatedStatsAfterUsageLogCleanup() {
	t := s.T()
	ctx := context.Background()
	now := timezone.Now()
	today := timezone.StartOfDay(now)
	// Keep both rows in today's bucket and strictly before NOW so this fixture
	// remains valid if the repository restores the legacy future-row boundary.
	// The branch handles a test process that starts within the first two minutes
	// of a local day, where subtracting two minutes would cross yesterday.
	createdAt := []time.Time{now.Add(-2 * time.Minute), now.Add(-time.Minute)}
	if now.Sub(today) < 2*time.Minute {
		createdAt = []time.Time{
			today.Add(now.Sub(today) / 3),
			today.Add(now.Sub(today) * 2 / 3),
		}
	}

	user := mustCreateUser(t, s.client, &service.User{Email: "acct-daily-rollup-aggregate@example.com"})
	apiKey := mustCreateApiKey(t, s.client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-acct-daily-rollup-aggregate-" + uuid.NewString(),
		Name:   "daily-rollup",
	})
	account := mustCreateAccount(t, s.client, &service.Account{
		Name:      "acc-daily-rollup-aggregate-" + uuid.NewString(),
		CreatedAt: today.Add(-24 * time.Hour),
	})

	duration1, duration2 := 120, 300
	for i, values := range []struct {
		input, output int
		standard      float64
		userCost      float64
		duration      *int
	}{
		{input: 12, output: 8, standard: 0.8, userCost: 0.6, duration: &duration1},
		{input: 4, output: 6, standard: 0.4, userCost: 0.3, duration: &duration2},
	} {
		log := &service.UsageLog{
			UserID:       user.ID,
			APIKeyID:     apiKey.ID,
			AccountID:    account.ID,
			RequestID:    uuid.NewString(),
			Model:        "rollup-aggregate-model",
			InputTokens:  values.input,
			OutputTokens: values.output,
			TotalCost:    values.standard,
			ActualCost:   values.userCost,
			DurationMs:   values.duration,
			CreatedAt:    createdAt[i],
		}
		_, err := s.repo.Create(ctx, log)
		require.NoError(t, err)
	}

	startTime := today.Add(-24 * time.Hour)
	endTime := today.Add(24 * time.Hour)
	aggregatedBefore, err := s.repo.GetAccountStatsAggregated(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.Equal(t, int64(2), aggregatedBefore.TotalRequests)
	require.Equal(t, int64(30), aggregatedBefore.TotalTokens)
	require.InEpsilon(t, 1.2, aggregatedBefore.TotalCost, 1e-9)
	require.InEpsilon(t, 0.9, aggregatedBefore.TotalActualCost, 1e-9)
	require.InEpsilon(t, 210, aggregatedBefore.AverageDurationMs, 1e-9)

	modalBefore, err := s.repo.GetAccountUsageStats(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.Len(t, modalBefore.History, 1)
	require.Equal(t, int64(2), modalBefore.Summary.TotalRequests)
	require.Equal(t, int64(30), modalBefore.Summary.TotalTokens)
	require.InEpsilon(t, 1.2, modalBefore.Summary.TotalCost, 1e-9)
	require.InEpsilon(t, 0.9, modalBefore.Summary.TotalUserCost, 1e-9)
	require.InEpsilon(t, 1.2, modalBefore.Summary.TotalStandardCost, 1e-9)
	require.NotNil(t, modalBefore.Summary.Today)

	_, err = s.repo.sql.ExecContext(ctx, "DELETE FROM usage_logs WHERE account_id = $1", account.ID)
	require.NoError(t, err)

	aggregatedAfter, err := s.repo.GetAccountStatsAggregated(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.Equal(t, aggregatedBefore.TotalRequests, aggregatedAfter.TotalRequests)
	require.Equal(t, aggregatedBefore.TotalTokens, aggregatedAfter.TotalTokens)
	require.InEpsilon(t, aggregatedBefore.TotalCost, aggregatedAfter.TotalCost, 1e-9)
	require.InEpsilon(t, aggregatedBefore.TotalActualCost, aggregatedAfter.TotalActualCost, 1e-9)
	require.InEpsilon(t, aggregatedBefore.AverageDurationMs, aggregatedAfter.AverageDurationMs, 1e-9)

	modalAfter, err := s.repo.GetAccountUsageStats(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.Len(t, modalAfter.History, len(modalBefore.History))
	require.Equal(t, modalBefore.Summary.TotalRequests, modalAfter.Summary.TotalRequests)
	require.Equal(t, modalBefore.Summary.TotalTokens, modalAfter.Summary.TotalTokens)
	require.InEpsilon(t, modalBefore.Summary.TotalCost, modalAfter.Summary.TotalCost, 1e-9)
	require.InEpsilon(t, modalBefore.Summary.TotalUserCost, modalAfter.Summary.TotalUserCost, 1e-9)
	require.InEpsilon(t, modalBefore.Summary.TotalStandardCost, modalAfter.Summary.TotalStandardCost, 1e-9)
	require.NotNil(t, modalAfter.Summary.Today)
}

// TestUsageAccountDailyRollupPreservesModalDurationNullSemantics verifies that
// the durable modal average continues to ignore rows whose duration_ms was
// NULL (the pre-rollup AVG(duration_ms) behavior), while the generic
// aggregated endpoint keeps its historical COALESCE(duration_ms, 0) behavior.
func (s *UsageLogRepoSuite) TestUsageAccountDailyRollupPreservesModalDurationNullSemantics() {
	t := s.T()
	ctx := context.Background()
	today := timezone.StartOfDay(timezone.Now())

	user := mustCreateUser(t, s.client, &service.User{Email: "acct-daily-rollup-duration-null@example.com"})
	apiKey := mustCreateApiKey(t, s.client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-acct-daily-rollup-duration-null-" + uuid.NewString(),
		Name:   "daily-rollup",
	})
	account := mustCreateAccount(t, s.client, &service.Account{
		Name:      "acc-daily-rollup-duration-null-" + uuid.NewString(),
		CreatedAt: today.Add(-24 * time.Hour),
	})
	duration := 100
	for i, value := range []struct {
		input, output int
		duration      *int
	}{
		{input: 1, output: 2, duration: &duration},
		{input: 3, output: 4, duration: nil},
	} {
		_, err := s.repo.Create(ctx, &service.UsageLog{
			UserID:       user.ID,
			APIKeyID:     apiKey.ID,
			AccountID:    account.ID,
			RequestID:    uuid.NewString(),
			Model:        "rollup-duration-null-model",
			InputTokens:  value.input,
			OutputTokens: value.output,
			CreatedAt:    today.Add(time.Duration(i+1) * time.Hour),
			DurationMs:   value.duration,
		})
		require.NoError(t, err)
	}

	startTime := today
	endTime := today.Add(24 * time.Hour)
	aggregatedBefore, err := s.repo.GetAccountStatsAggregated(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.InEpsilon(t, 50, aggregatedBefore.AverageDurationMs, 1e-9)
	modalBefore, err := s.repo.GetAccountUsageStats(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.InEpsilon(t, 100, modalBefore.Summary.AvgDurationMs, 1e-9)

	_, err = s.repo.sql.ExecContext(ctx, "DELETE FROM usage_logs WHERE account_id = $1", account.ID)
	require.NoError(t, err)

	aggregatedAfter, err := s.repo.GetAccountStatsAggregated(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.InEpsilon(t, aggregatedBefore.AverageDurationMs, aggregatedAfter.AverageDurationMs, 1e-9)
	modalAfter, err := s.repo.GetAccountUsageStats(ctx, account.ID, startTime, endTime)
	require.NoError(t, err)
	require.InEpsilon(t, modalBefore.Summary.AvgDurationMs, modalAfter.Summary.AvgDurationMs, 1e-9)
}
