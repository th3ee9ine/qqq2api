package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCache_ClaimSessionAccountFirstWriterWins(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheValue := NewGatewayCache(client)
	cache, ok := cacheValue.(*gatewayCache)
	require.True(t, ok)

	const groupID int64 = 77
	const sessionHash = "openai:concurrent-session"
	results := make(chan bool, 2)
	var wg sync.WaitGroup
	for _, accountID := range []int64{101, 202} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			claimed, err := cache.ClaimSessionAccountID(context.Background(), groupID, sessionHash, id, time.Minute)
			require.NoError(t, err)
			results <- claimed
		}(accountID)
	}
	wg.Wait()
	close(results)

	claimedCount := 0
	for claimed := range results {
		if claimed {
			claimedCount++
		}
	}
	require.Equal(t, 1, claimedCount)
	owner, err := cache.GetSessionAccountID(context.Background(), groupID, sessionHash)
	require.NoError(t, err)
	require.Contains(t, []int64{101, 202}, owner)

	deleted, err := cache.CompareAndDeleteSessionAccountID(context.Background(), groupID, sessionHash, owner+1)
	require.NoError(t, err)
	require.False(t, deleted, "旧账号不得删除新绑定")
	deleted, err = cache.CompareAndDeleteSessionAccountID(context.Background(), groupID, sessionHash, owner)
	require.NoError(t, err)
	require.True(t, deleted)
}

func TestGatewayCache_CompareAndSetSessionRejectsLateMigration(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cacheValue := NewGatewayCache(client)
	cache, ok := cacheValue.(*gatewayCache)
	require.True(t, ok)
	ctx := context.Background()
	require.NoError(t, cache.SetSessionAccountID(ctx, 1, "weighted", 101, time.Minute))
	results := make(chan bool, 2)
	var wg sync.WaitGroup
	for _, target := range []int64{202, 303} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			ok, err := cache.CompareAndSetSessionAccountID(ctx, 1, "weighted", 101, id, time.Minute)
			require.NoError(t, err)
			results <- ok
		}(target)
	}
	wg.Wait()
	close(results)
	winners := 0
	for ok := range results {
		if ok {
			winners++
		}
	}
	require.Equal(t, 1, winners)
	owner, err := cache.GetSessionAccountID(ctx, 1, "weighted")
	require.NoError(t, err)
	claimed, err := cache.ClaimSessionAccountID(ctx, 1, "weighted", 101, time.Minute)
	require.NoError(t, err)
	require.False(t, claimed, "old request's terminal bind must not overwrite the migration")
	actual, err := cache.GetSessionAccountID(ctx, 1, "weighted")
	require.NoError(t, err)
	require.Equal(t, owner, actual)
}
