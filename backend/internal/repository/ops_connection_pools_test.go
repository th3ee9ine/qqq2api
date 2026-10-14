package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestOpsDatabaseConnectionPoolStatsReportsActualWaitWithoutQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = db.Conn(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// The pool is still saturated. The snapshot must read client memory only.
	stats := (&opsRepository{db: db}).DatabaseConnectionPoolStats()
	require.Equal(t, 1, stats.MaxOpenConnections)
	require.Equal(t, 1, stats.InUse)
	require.EqualValues(t, 1, stats.WaitCountTotal)
	require.Positive(t, stats.WaitDurationMsTotal)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpsRedisConnectionPoolStatsReportsActualWaitAndTimeout(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{
		Addr:           server.Addr(),
		PoolSize:       1,
		MaxActiveConns: 1,
		PoolTimeout:    100 * time.Millisecond,
		MaxRetries:     -1,
		Protocol:       2,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := &concurrencyCache{rdb: rdb}
	conn := rdb.Conn()
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.Ping(context.Background()).Err())

	done := make(chan error, 1)
	go func() { done <- rdb.Ping(context.Background()).Err() }()
	require.Eventually(t, func() bool {
		return cache.RedisConnectionPoolStats().PendingRequests > 0
	}, time.Second, time.Millisecond)
	require.NoError(t, conn.Close())
	require.NoError(t, <-done)
	stats := cache.RedisConnectionPoolStats()
	require.Equal(t, 1, stats.PoolSize)
	require.Equal(t, 1, stats.MaxActiveConns)
	require.Positive(t, stats.WaitCountTotal)
	require.Positive(t, stats.WaitDurationMsTotal)
	require.Zero(t, stats.PendingRequests)

	conn = rdb.Conn()
	require.NoError(t, conn.Ping(context.Background()).Err())
	before := cache.RedisConnectionPoolStats().TimeoutsTotal
	require.ErrorContains(t, rdb.Ping(context.Background()).Err(), "connection pool timeout")
	commandCount := server.CommandCount()
	require.Greater(t, cache.RedisConnectionPoolStats().TimeoutsTotal, before)
	// Reading counters did not send an extra command to the Redis server.
	require.Equal(t, commandCount, server.CommandCount())
}

func TestOpsConnectionPoolStatsWithoutClientsIsUnavailable(t *testing.T) {
	require.Nil(t, (&opsRepository{}).DatabaseConnectionPoolStats())
	require.Nil(t, (&concurrencyCache{}).RedisConnectionPoolStats())
}
