package service

import (
	"time"

	"github.com/google/uuid"
)

// A process identity lets pollers distinguish replicas and counter resets even
// when they reach this endpoint through a load balancer.
var opsConnectionPoolInstanceID = uuid.NewString()

// OpsConnectionPools is a live, local snapshot. Cumulative counters belong to
// the underlying client lifetime; they are not cluster totals or queue gauges.
type OpsConnectionPools struct {
	Scope        string                          `json:"scope"`
	InstanceID   string                          `json:"instance_id"`
	CollectedAt  time.Time                       `json:"collected_at"`
	CounterScope string                          `json:"counter_scope"`
	Database     *OpsDatabaseConnectionPoolStats `json:"database"`
	Redis        *OpsRedisConnectionPoolStats    `json:"redis"`
}

type OpsDatabaseConnectionPoolStats struct {
	MaxOpenConnections  int     `json:"max_open_connections"`
	OpenConnections     int     `json:"open_connections"`
	InUse               int     `json:"in_use"`
	Idle                int     `json:"idle"`
	WaitCountTotal      int64   `json:"wait_count_total"`
	WaitDurationMsTotal float64 `json:"wait_duration_ms_total"`
}

type OpsRedisConnectionPoolStats struct {
	PoolSize        int    `json:"pool_size"`
	MaxActiveConns  int    `json:"max_active_conns"`
	TotalConns      uint32 `json:"total_conns"`
	IdleConns       uint32 `json:"idle_conns"`
	PendingRequests uint32 `json:"pending_requests"`
	// go-redis counts successful pool waits here; timeouts are separate.
	WaitCountTotal      uint32  `json:"wait_count_total"`
	WaitDurationMsTotal float64 `json:"wait_duration_ms_total"`
	TimeoutsTotal       uint32  `json:"timeouts_total"`
}

// Optional capabilities preserve existing repository/cache implementations.
// Both methods read client memory only, without acquiring a pool connection.
type opsDatabaseConnectionPoolStatsProvider interface {
	DatabaseConnectionPoolStats() *OpsDatabaseConnectionPoolStats
}

type opsRedisConnectionPoolStatsProvider interface {
	RedisConnectionPoolStats() *OpsRedisConnectionPoolStats
}

func (s *OpsService) GetConnectionPoolStats() *OpsConnectionPools {
	result := &OpsConnectionPools{
		Scope:        "instance",
		InstanceID:   opsConnectionPoolInstanceID,
		CollectedAt:  time.Now().UTC(),
		CounterScope: "client_lifetime",
	}
	if s == nil {
		return result
	}
	if provider, ok := s.opsRepo.(opsDatabaseConnectionPoolStatsProvider); ok {
		result.Database = provider.DatabaseConnectionPoolStats()
	}
	if s.concurrencyService != nil {
		if provider, ok := s.concurrencyService.cache.(opsRedisConnectionPoolStatsProvider); ok {
			result.Redis = provider.RedisConnectionPoolStats()
		}
	}
	return result
}
