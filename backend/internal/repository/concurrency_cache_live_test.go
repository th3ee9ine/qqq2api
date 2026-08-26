package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestLiveLeaseReplacesRegularSlotsAndCountsTowardLimits(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	live, ok := regular.(service.LiveConcurrencyCache)
	require.True(t, ok)
	apiKeys, ok := regular.(service.APIKeyConcurrencyLimiterCache)
	require.True(t, ok)
	ctx := context.Background()

	accountAcquired, err := regular.AcquireAccountSlot(ctx, 10, 1, "regular-account")
	require.NoError(t, err)
	require.True(t, accountAcquired)
	userAcquired, err := regular.AcquireUserSlot(ctx, 20, 1, "regular-user")
	require.NoError(t, err)
	require.True(t, userAcquired)
	apiKeyAcquired, err := apiKeys.AcquireAPIKeySlot(ctx, 30, 1, "regular-api-key")
	require.NoError(t, err)
	require.True(t, apiKeyAcquired)

	acquired, err := live.AcquireLiveLease(ctx, 10, 1, 20, 0, 30, 1, "live-lease", true)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NoError(t, regular.ReleaseAccountSlot(ctx, 10, "regular-account"))
	require.NoError(t, regular.ReleaseUserSlot(ctx, 20, "regular-user"))
	require.NoError(t, apiKeys.ReleaseAPIKeySlot(ctx, 30, "regular-api-key"))

	accountCount, err := regular.GetAccountConcurrency(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, accountCount)
	userCount, err := regular.GetUserConcurrency(ctx, 20)
	require.NoError(t, err)
	require.Equal(t, 1, userCount)
	apiKeyCounts, err := regular.(service.APIKeyConcurrencyCache).GetAPIKeyConcurrencyBatch(ctx, []int64{30})
	require.NoError(t, err)
	require.Equal(t, 1, apiKeyCounts[30])
	accountAcquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-blocked")
	require.NoError(t, err)
	require.False(t, accountAcquired)
	apiKeyAcquired, err = apiKeys.AcquireAPIKeySlot(ctx, 30, 1, "api-key-blocked-by-live")
	require.NoError(t, err)
	require.False(t, apiKeyAcquired)

	refreshed, err := live.RefreshLiveLease(ctx, 10, 20, 30, "live-lease")
	require.NoError(t, err)
	require.True(t, refreshed)
	require.NoError(t, live.ReleaseLiveLease(ctx, 10, 20, 30, "live-lease"))
	accountAcquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-allowed")
	require.NoError(t, err)
	require.True(t, accountAcquired)
}

func TestAPIKeyConcurrencyLimitsAreIsolatedByKeyID(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache := NewConcurrencyCache(client, 15, 900)
	apiKeys, ok := cache.(service.APIKeyConcurrencyLimiterCache)
	require.True(t, ok)
	ctx := context.Background()

	acquired, err := apiKeys.AcquireAPIKeySlot(ctx, 101, 1, "key-a-first")
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = apiKeys.AcquireAPIKeySlot(ctx, 101, 1, "key-a-second")
	require.NoError(t, err)
	require.False(t, acquired)

	acquired, err = apiKeys.AcquireAPIKeySlot(ctx, 202, 1, "key-b-first")
	require.NoError(t, err)
	require.True(t, acquired, "another API key must have an independent concurrency bucket")

	require.NoError(t, apiKeys.ReleaseAPIKeySlot(ctx, 101, "key-a-first"))
	acquired, err = apiKeys.AcquireAPIKeySlot(ctx, 101, 1, "key-a-after-release")
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestLiveLeaseExpiresWithoutRefresh(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	regular := NewConcurrencyCache(client, 15, 900)
	live, ok := regular.(service.LiveConcurrencyCache)
	require.True(t, ok)
	ctx := context.Background()

	acquired, err := live.AcquireLiveLease(ctx, 10, 1, 20, 0, 30, 1, "expired-live", false)
	require.NoError(t, err)
	require.True(t, acquired)

	redisServer.FastForward(61 * time.Second)
	acquired, err = regular.AcquireAccountSlot(ctx, 10, 1, "ordinary-after-expiry")
	require.NoError(t, err)
	require.True(t, acquired)
	refreshed, err := live.RefreshLiveLease(ctx, 10, 20, 30, "expired-live")
	require.NoError(t, err)
	require.False(t, refreshed)
}
