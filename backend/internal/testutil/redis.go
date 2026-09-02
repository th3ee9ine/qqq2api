package testutil

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/th3ee9ine/qqq2api/internal/repository"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// NewRedisGatewayCache returns a real Redis-backed gateway cache for tests.
func NewRedisGatewayCache(t *testing.T) service.GatewayCache {
	t.Helper()

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	return repository.NewGatewayCache(redisClient)
}
