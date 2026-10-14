package repository

import (
	"time"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

// DatabaseConnectionPoolStats never queries PostgreSQL or waits for a connection.
func (r *opsRepository) DatabaseConnectionPoolStats() *service.OpsDatabaseConnectionPoolStats {
	if r == nil || r.db == nil {
		return nil
	}
	stats := r.db.Stats()
	return &service.OpsDatabaseConnectionPoolStats{
		MaxOpenConnections:  stats.MaxOpenConnections,
		OpenConnections:     stats.OpenConnections,
		InUse:               stats.InUse,
		Idle:                stats.Idle,
		WaitCountTotal:      stats.WaitCount,
		WaitDurationMsTotal: float64(stats.WaitDuration) / float64(time.Millisecond),
	}
}

// RedisConnectionPoolStats reads the shared Redis client's local pool counters.
// PoolSize is the base pool size; MaxActiveConns is the optional hard cap (0 is
// unlimited). Neither value is a cluster-wide request-concurrency limit.
func (c *concurrencyCache) RedisConnectionPoolStats() *service.OpsRedisConnectionPoolStats {
	if c == nil || c.rdb == nil {
		return nil
	}
	stats := c.rdb.PoolStats()
	if stats == nil {
		return nil
	}
	opts := c.rdb.Options()
	return &service.OpsRedisConnectionPoolStats{
		PoolSize:            opts.PoolSize,
		MaxActiveConns:      opts.MaxActiveConns,
		TotalConns:          stats.TotalConns,
		IdleConns:           stats.IdleConns,
		PendingRequests:     stats.PendingRequests,
		WaitCountTotal:      stats.WaitCount,
		WaitDurationMsTotal: float64(stats.WaitDurationNs) / float64(time.Millisecond),
		TimeoutsTotal:       stats.Timeouts,
	}
}
