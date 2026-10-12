//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestBillingCacheSubscriptionGeneration(t *testing.T) {
	cache, _ := newMiniRedisCache(t)
	t.Cleanup(func() { _ = cache.rdb.Close() })
	ctx := context.Background()
	userID, groupID := int64(7), int64(9)
	data := &service.SubscriptionCacheData{
		Status:       "active",
		ExpiresAt:    time.Now().Add(time.Hour),
		DailyUsage:   1,
		WeeklyUsage:  2,
		MonthlyUsage: 3,
		Version:      11,
	}

	generation, err := cache.GetSubscriptionCacheGeneration(ctx, userID, groupID)
	require.NoError(t, err)
	require.Zero(t, generation, "a missing generation key represents generation zero")

	filled, err := cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, 0, data)
	require.NoError(t, err)
	require.True(t, filled)
	ttl, err := cache.rdb.TTL(ctx, billingSubKey(userID, groupID)).Result()
	require.NoError(t, err)
	require.Positive(t, ttl)
	require.LessOrEqual(t, ttl, billingCacheTTL)

	// A conditional fill never overwrites a cache that another request has
	// already populated, even if the generation is unchanged.
	replacement := *data
	replacement.DailyUsage = 99
	filled, err = cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, 0, &replacement)
	require.NoError(t, err)
	require.False(t, filled)
	got, err := cache.GetSubscriptionCache(ctx, userID, groupID)
	require.NoError(t, err)
	require.Equal(t, data.DailyUsage, got.DailyUsage)

	require.NoError(t, cache.InvalidateSubscriptionCacheAndBumpGeneration(ctx, userID, groupID))
	generation, err = cache.GetSubscriptionCacheGeneration(ctx, userID, groupID)
	require.NoError(t, err)
	require.Equal(t, int64(1), generation)
	ttl, err = cache.rdb.TTL(ctx, billingSubGenerationKey(userID, groupID)).Result()
	require.NoError(t, err)
	require.Equal(t, billingCacheTTL, ttl)
	_, err = cache.GetSubscriptionCache(ctx, userID, groupID)
	require.ErrorIs(t, err, redis.Nil)

	// The tombstone rejects the old snapshot (generation zero), while a new
	// snapshot can fill against the bumped generation.
	filled, err = cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, 0, data)
	require.NoError(t, err)
	require.False(t, filled)
	filled, err = cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, 1, data)
	require.NoError(t, err)
	require.True(t, filled)

	// Invalidation also creates a tombstone when no subscription hash exists.
	missingUserID, missingGroupID := int64(17), int64(19)
	require.NoError(t, cache.InvalidateSubscriptionCacheAndBumpGeneration(ctx, missingUserID, missingGroupID))
	generation, err = cache.GetSubscriptionCacheGeneration(ctx, missingUserID, missingGroupID)
	require.NoError(t, err)
	require.Equal(t, int64(1), generation)
	filled, err = cache.SetSubscriptionCacheIfGeneration(ctx, missingUserID, missingGroupID, 0, data)
	require.NoError(t, err)
	require.False(t, filled)
}

func TestBillingCacheSubscriptionInvalidateDirectlyBumpsGeneration(t *testing.T) {
	cache, _ := newMiniRedisCache(t)
	t.Cleanup(func() { _ = cache.rdb.Close() })
	ctx := context.Background()

	require.NoError(t, cache.InvalidateSubscriptionCache(ctx, 7, 9))
	generation, err := cache.GetSubscriptionCacheGeneration(ctx, 7, 9)
	require.NoError(t, err)
	require.Equal(t, int64(1), generation)
	require.NoError(t, cache.InvalidateSubscriptionCache(ctx, 7, 9))
	generation, err = cache.GetSubscriptionCacheGeneration(ctx, 7, 9)
	require.NoError(t, err)
	require.Equal(t, int64(2), generation)
}

func TestBillingCacheSubscriptionGenerationReadErrorIsNotZero(t *testing.T) {
	cache, _ := newMiniRedisCache(t)
	t.Cleanup(func() { _ = cache.rdb.Close() })
	ctx := context.Background()

	require.NoError(t, cache.rdb.HSet(ctx, billingSubGenerationKey(7, 9), "unexpected", "hash").Err())
	_, err := cache.GetSubscriptionCacheGeneration(ctx, 7, 9)
	require.Error(t, err, "Redis errors must not be interpreted as generation zero")
}
