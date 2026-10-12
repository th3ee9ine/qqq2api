//go:build integration

package repository

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// Pause an actual UPDATE after its timestamp read, so a second connection can
// commit a newer snapshot before the first writer reaches PostgreSQL's CAS.
type delayedCodexSnapshotSQL struct {
	*sql.DB
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (d *delayedCodexSnapshotSQL) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	d.once.Do(func() {
		close(d.entered)
		select {
		case <-d.release:
		case <-ctx.Done():
		}
	})
	return d.DB.ExecContext(ctx, query, args...)
}

func TestIntegrationCodexSnapshotCAS(t *testing.T) {
	ctx := context.Background()
	var version string
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SHOW server_version").Scan(&version))
	t.Logf("actual PostgreSQL server: %s", version)
	created, err := testEntClient(t).Account.Create().SetName("codex-cas-real-db").
		SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).
		SetCredentials(map[string]any{"api_key": "codex-cas-test"}).
		SetExtra(map[string]any{"unrelated": "keep", "codex_usage_updated_at": "legacy-invalid"}).
		SetConcurrency(1).SetPriority(50).SetStatus(service.StatusActive).SetSchedulable(true).Save(ctx)
	require.NoError(t, err)
	accountID := created.ID
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", accountID)
		require.NoError(t, err)
	})
	repo := &accountRepository{sql: integrationDB}
	first := time.Date(2026, 10, 13, 10, 0, 0, 123456789, time.UTC)
	updates := func(ts time.Time, used float64) map[string]any {
		return map[string]any{"codex_usage_updated_at": ts.Format(time.RFC3339Nano), "codex_5h_used_percent": used}
	}
	applied, err := repo.UpdateExtraIfCodexSnapshotNewer(ctx, accountID, updates(first, 10), first)
	require.NoError(t, err)
	require.True(t, applied, "an invalid legacy timestamp must not block a fresh snapshot")
	for _, stale := range []time.Time{first.Add(-time.Nanosecond), first} {
		applied, err = repo.UpdateExtraIfCodexSnapshotNewer(ctx, accountID, updates(stale, 99), stale)
		require.NoError(t, err)
		require.False(t, applied, "older and equal snapshots must be rejected")
	}

	delayed := &delayedCodexSnapshotSQL{DB: integrationDB, entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(delayed.release) }) }
	t.Cleanup(unblock)
	slowRepo := &accountRepository{sql: delayed}
	type result struct {
		applied bool
		err     error
	}
	finished := make(chan result, 1)
	go func() {
		ts := first.Add(time.Nanosecond)
		ok, err := slowRepo.UpdateExtraIfCodexSnapshotNewer(ctx, accountID, updates(ts, 20), ts)
		finished <- result{ok, err}
	}()
	select {
	case <-delayed.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("delayed writer never reached SQL CAS")
	}
	newest := first.Add(2 * time.Nanosecond)
	applied, err = repo.UpdateExtraIfCodexSnapshotNewer(ctx, accountID, updates(newest, 30), newest)
	require.NoError(t, err)
	require.True(t, applied)
	unblock()
	select {
	case slow := <-finished:
		require.NoError(t, slow.err)
		require.False(t, slow.applied, "the delayed writer must lose to the committed newer timestamp")
	case <-time.After(5 * time.Second):
		t.Fatal("delayed writer did not finish")
	}
	var timestamp, used, unrelated string
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT extra->>'codex_usage_updated_at', extra->>'codex_5h_used_percent', extra->>'unrelated' FROM accounts WHERE id = $1", accountID).
		Scan(&timestamp, &used, &unrelated))
	require.Equal(t, newest.Format(time.RFC3339Nano), timestamp)
	require.Equal(t, "30", used)
	require.Equal(t, "keep", unrelated)
}

func TestIntegrationSubscriptionGenerationRejectsLateFill(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewBillingCache(rdb).(*billingCache)
	data := &service.SubscriptionCacheData{Status: "active", ExpiresAt: time.Now().Add(time.Hour), DailyUsage: 7}
	const userID, groupID int64 = 819, 820
	generation, err := cache.GetSubscriptionCacheGeneration(ctx, userID, groupID)
	require.NoError(t, err)
	require.Zero(t, generation)
	require.NoError(t, cache.InvalidateSubscriptionCacheAndBumpGeneration(ctx, userID, groupID))
	filled, err := cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, generation, data)
	require.NoError(t, err)
	require.False(t, filled)
	_, err = cache.GetSubscriptionCache(ctx, userID, groupID)
	require.ErrorIs(t, err, redis.Nil)
	generation, err = cache.GetSubscriptionCacheGeneration(ctx, userID, groupID)
	require.NoError(t, err)
	require.Equal(t, int64(1), generation)
	filled, err = cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, generation, data)
	require.NoError(t, err)
	require.True(t, filled)
	data.DailyUsage = 99
	filled, err = cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, generation, data)
	require.NoError(t, err)
	require.False(t, filled, "same-generation fill must not overwrite an existing snapshot")
	got, err := cache.GetSubscriptionCache(ctx, userID, groupID)
	require.NoError(t, err)
	require.Equal(t, 7.0, got.DailyUsage)
	for _, key := range []string{billingSubKey(userID, groupID), billingSubGenerationKey(userID, groupID)} {
		ttl, err := rdb.TTL(ctx, key).Result()
		require.NoError(t, err)
		assertTTLWithin(t, ttl, time.Second, billingCacheTTL)
	}
}

func TestIntegrationStickyClaimAndMigrationAreAtomic(t *testing.T) {
	ctx := context.Background()
	cache := NewGatewayCache(testRedis(t)).(*gatewayCache)
	const groupID int64 = 821
	const key = "openai:atomic-claim"
	type outcome struct {
		won bool
		err error
	}
	results := make(chan outcome, 16)
	start := make(chan struct{})
	for id := int64(1); id <= 16; id++ {
		go func() {
			<-start
			won, err := cache.ClaimSessionAccountID(ctx, groupID, key, id, time.Minute)
			results <- outcome{won, err}
		}()
	}
	close(start)
	winners := 0
	for range 16 {
		result := <-results
		require.NoError(t, result.err)
		if result.won {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	owner, err := cache.GetSessionAccountID(ctx, groupID, key)
	require.NoError(t, err)
	won, err := cache.CompareAndSetSessionAccountID(ctx, groupID, key, owner, 99, time.Minute)
	require.NoError(t, err)
	require.True(t, won)
	won, err = cache.CompareAndSetSessionAccountID(ctx, groupID, key, owner, 100, time.Minute)
	require.NoError(t, err)
	require.False(t, won)
	deleted, err := cache.CompareAndDeleteSessionAccountID(ctx, groupID, key, owner)
	require.NoError(t, err)
	require.False(t, deleted)
	got, err := cache.GetSessionAccountID(ctx, groupID, key)
	require.NoError(t, err)
	require.Equal(t, int64(99), got)
}

func TestIntegrationRegularSlotHeartbeatRetainsOwnership(t *testing.T) {
	ctx := context.Background()
	rdb := testRedis(t)
	cache := NewConcurrencyCache(rdb, 15, 900).(*concurrencyCache)
	for _, tc := range []struct {
		name    string
		key     func(int64) string
		acquire func(context.Context, int64, int, string) (bool, error)
		refresh func(context.Context, int64, string) (bool, error)
		release func(context.Context, int64, string) error
	}{
		{"account", accountSlotKey, cache.AcquireAccountSlot, cache.RefreshAccountSlot, cache.ReleaseAccountSlot},
		{"user", userSlotKey, cache.AcquireUserSlot, cache.RefreshUserSlot, cache.ReleaseUserSlot},
		{"api_key", apiKeySlotKey, cache.AcquireAPIKeySlot, cache.RefreshAPIKeySlot, cache.ReleaseAPIKeySlot},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const id int64 = 822
			owned, err := tc.acquire(ctx, id, 1, "owner")
			require.NoError(t, err)
			require.True(t, owned)
			now, err := rdb.Time(ctx).Result()
			require.NoError(t, err)
			// Move only the test member near expiry; no long wall-clock wait is needed.
			require.NoError(t, rdb.ZAdd(ctx, tc.key(id), redis.Z{Score: float64(now.Unix() - 899), Member: "owner"}).Err())
			require.NoError(t, rdb.Expire(ctx, tc.key(id), 2*time.Second).Err())
			owned, err = tc.refresh(ctx, id, "owner")
			require.NoError(t, err)
			require.True(t, owned)
			score, err := rdb.ZScore(ctx, tc.key(id), "owner").Result()
			require.NoError(t, err)
			require.GreaterOrEqual(t, score, float64(now.Unix()))
			ttl, err := rdb.TTL(ctx, tc.key(id)).Result()
			require.NoError(t, err)
			assertTTLWithin(t, ttl, 898*time.Second, 900*time.Second)
			owned, err = tc.acquire(ctx, id, 1, "competitor")
			require.NoError(t, err)
			require.False(t, owned)
			require.NoError(t, tc.release(ctx, id, "owner"))
			owned, err = tc.refresh(ctx, id, "owner")
			require.NoError(t, err)
			require.False(t, owned, "late heartbeat must not recreate a released member")
		})
	}
}
