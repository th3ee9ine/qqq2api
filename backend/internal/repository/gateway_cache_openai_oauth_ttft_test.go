package repository

import (
	"context"
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
