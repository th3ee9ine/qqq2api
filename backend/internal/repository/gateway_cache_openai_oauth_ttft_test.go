package repository

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestGatewayCacheOpenAIOAuthTTFTStreakIsAtomicAndResettable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache, ok := NewGatewayCache(client).(service.OpenAIOAuthTTFTStreakCache)
	require.True(t, ok)
	ctx := context.Background()

	count, err := cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	count, err = cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	require.NoError(t, cache.ResetOpenAIOAuthTTFTStreak(ctx, 17))
	count, err = cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestGatewayCacheOpenAIOAuthTTFTStreakSharesAtomicCountsAcrossClients(t *testing.T) {
	server := miniredis.RunT(t)
	first := redis.NewClient(&redis.Options{Addr: server.Addr()})
	second := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	caches := []service.OpenAIOAuthTTFTStreakCache{
		NewGatewayCache(first).(service.OpenAIOAuthTTFTStreakCache),
		NewGatewayCache(second).(service.OpenAIOAuthTTFTStreakCache),
	}
	const calls = 40
	counts := make([]int, calls)
	errs := make([]error, calls)
	var wg sync.WaitGroup
	for i := range counts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			count, err := caches[i%len(caches)].IncrementOpenAIOAuthTTFTStreak(context.Background(), 17, time.Minute)
			counts[i], errs[i] = int(count), err
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	sort.Ints(counts)
	for i, count := range counts {
		require.Equal(t, i+1, count)
	}
	require.NoError(t, caches[0].ResetOpenAIOAuthTTFTStreak(context.Background(), 17))
	count, err := caches[1].IncrementOpenAIOAuthTTFTStreak(context.Background(), 17, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "a reset is visible to every instance")
}

func TestGatewayCacheOpenAIOAuthTTFTStreakExpiryAndAccountIsolation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTStreakCache)
	ctx := context.Background()
	_, err := cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, time.Minute)
	require.NoError(t, err)
	_, err = cache.IncrementOpenAIOAuthTTFTStreak(ctx, 18, 2*time.Minute)
	require.NoError(t, err)
	server.FastForward(time.Minute)
	count, err := cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "expired history must not contribute to a new streak")
	count, err = cache.IncrementOpenAIOAuthTTFTStreak(ctx, 18, 2*time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 2, count, "other accounts retain their own streak")
}

func TestGatewayCacheOpenAIOAuthTTFTStreakPreservesLowFrequencyCalls(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTStreakCache)
	ctx := context.Background()
	// Retain already recorded history when an old expiring counter is reused.
	count, err := cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, 30*time.Minute)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	for want := 2; want <= 5; want++ {
		count, err = cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, 0)
		require.NoError(t, err)
		require.EqualValues(t, want, count)
		server.FastForward(24 * time.Hour)
	}
	require.NoError(t, cache.ResetOpenAIOAuthTTFTStreak(ctx, 17))
	count, err = cache.IncrementOpenAIOAuthTTFTStreak(ctx, 17, 0)
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "a compliant actual call still resets the streak")
}
