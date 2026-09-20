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

func TestGatewayCacheCodexTurnStateCollectionLeaseCompetesAcrossInstances(t *testing.T) {
	mr := miniredis.RunT(t)
	firstClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	secondClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = firstClient.Close()
		_ = secondClient.Close()
	})
	first := NewGatewayCache(firstClient).(service.CodexTurnStateCollectionLeaseStore)
	second := NewGatewayCache(secondClient).(service.CodexTurnStateCollectionLeaseStore)
	ctx := context.Background()

	acquired, err := first.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "instance-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = second.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "instance-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "a second instance must not acquire the same account")

	acquired, err = second.TryAcquireCodexTurnStateCollectionLease(ctx, 43, "instance-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired, "different accounts keep independent leases")
}

func TestGatewayCacheCodexTurnStateCollectionLeaseRefreshAndReleaseRequireOwner(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.CodexTurnStateCollectionLeaseStore)
	ctx := context.Background()

	acquired, err := store.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "owner-a", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	refreshed, err := store.RefreshCodexTurnStateCollectionLease(ctx, 42, "owner-b", 2*time.Minute)
	require.NoError(t, err)
	require.False(t, refreshed)
	released, err := store.ReleaseCodexTurnStateCollectionLease(ctx, 42, "owner-b")
	require.NoError(t, err)
	require.False(t, released)

	mr.FastForward(30 * time.Second)
	refreshed, err = store.RefreshCodexTurnStateCollectionLease(ctx, 42, "owner-a", 2*time.Minute)
	require.NoError(t, err)
	require.True(t, refreshed)
	mr.FastForward(90 * time.Second)

	acquired, err = store.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "owner-b", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "owner refresh must extend the lease TTL")
	released, err = store.ReleaseCodexTurnStateCollectionLease(ctx, 42, "owner-a")
	require.NoError(t, err)
	require.True(t, released)

	acquired, err = store.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "owner-b", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
}

func TestGatewayCacheCodexTurnStateCollectionLeaseExpiresAfterCrash(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewGatewayCache(client).(service.CodexTurnStateCollectionLeaseStore)
	ctx := context.Background()

	acquired, err := store.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "crashed-owner", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
	mr.FastForward(time.Minute + time.Millisecond)

	acquired, err = store.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "replacement", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	refreshed, err := store.RefreshCodexTurnStateCollectionLease(ctx, 42, "crashed-owner", time.Minute)
	require.NoError(t, err)
	require.False(t, refreshed, "an expired owner must not extend its replacement's lease")
	released, err := store.ReleaseCodexTurnStateCollectionLease(ctx, 42, "crashed-owner")
	require.NoError(t, err)
	require.False(t, released, "an expired owner must not delete its replacement's lease")

	acquired, err = store.TryAcquireCodexTurnStateCollectionLease(ctx, 42, "third-owner", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired, "the replacement lease must remain owned")
}
