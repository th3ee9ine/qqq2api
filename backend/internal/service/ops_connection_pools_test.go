package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type opsPoolStatsRepo struct {
	OpsRepository
	stats OpsDatabaseConnectionPoolStats
}

func (r *opsPoolStatsRepo) DatabaseConnectionPoolStats() *OpsDatabaseConnectionPoolStats {
	stats := r.stats
	return &stats
}

type opsPoolStatsCache struct {
	ConcurrencyCache
	stats OpsRedisConnectionPoolStats
}

func (c *opsPoolStatsCache) RedisConnectionPoolStats() *OpsRedisConnectionPoolStats {
	stats := c.stats
	return &stats
}

func TestOpsConnectionPoolStatsReadsLiveProvidersAndIdentifiesInstance(t *testing.T) {
	repo := &opsPoolStatsRepo{stats: OpsDatabaseConnectionPoolStats{WaitCountTotal: 3, WaitDurationMsTotal: 1.25}}
	cache := &opsPoolStatsCache{stats: OpsRedisConnectionPoolStats{WaitCountTotal: 7, TimeoutsTotal: 2}}
	svc := &OpsService{opsRepo: repo, concurrencyService: NewConcurrencyService(cache)}
	first := svc.GetConnectionPoolStats()
	require.Equal(t, "instance", first.Scope)
	require.Equal(t, "client_lifetime", first.CounterScope)
	require.NotEmpty(t, first.InstanceID)
	require.False(t, first.CollectedAt.IsZero())
	require.EqualValues(t, 3, first.Database.WaitCountTotal)
	require.Equal(t, 1.25, first.Database.WaitDurationMsTotal)
	require.EqualValues(t, 2, first.Redis.TimeoutsTotal)

	repo.stats.WaitCountTotal = 5
	cache.stats.TimeoutsTotal = 4
	second := svc.GetConnectionPoolStats()
	require.Equal(t, first.InstanceID, second.InstanceID)
	require.EqualValues(t, 5, second.Database.WaitCountTotal)
	require.EqualValues(t, 4, second.Redis.TimeoutsTotal)
	require.EqualValues(t, 3, first.Database.WaitCountTotal)
}

func TestOpsConnectionPoolStatsMissingProvidersAreNull(t *testing.T) {
	for _, svc := range []*OpsService{nil, {}, {concurrencyService: NewConcurrencyService(nil)}} {
		stats := svc.GetConnectionPoolStats()
		require.Nil(t, stats.Database)
		require.Nil(t, stats.Redis)
	}
}
