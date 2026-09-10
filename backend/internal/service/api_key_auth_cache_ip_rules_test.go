package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	ippkg "github.com/th3ee9ine/qqq2api/internal/pkg/ip"
)

type l2IPRulesCacheStub struct {
	APIKeyCache
	entry *APIKeyAuthCacheEntry
	calls atomic.Int32
}

func (c *l2IPRulesCacheStub) GetAuthCache(context.Context, string) (*APIKeyAuthCacheEntry, error) {
	c.calls.Add(1)
	return c.entry, nil
}

func TestAPIKeyServiceL2AuthCacheBackfillReusesCompiledIPRules(t *testing.T) {
	groupID := int64(9)
	entry := &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
		Version:  apiKeyAuthSnapshotVersion,
		APIKeyID: 1, UserID: 2, GroupID: &groupID, Status: StatusActive,
		IPWhitelist: []string{"198.51.100.0/24"}, IPBlacklist: []string{"198.51.100.42"},
		User: APIKeyAuthUserSnapshot{ID: 2, Status: StatusActive, Role: RoleUser},
	}}
	cache := &l2IPRulesCacheStub{entry: entry}
	cfg := &config.Config{APIKeyAuth: config.APIKeyAuthCacheConfig{
		L1Size: 1000, L1TTLSeconds: 60, L2TTLSeconds: 300,
	}}
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, cfg)
	require.NotNil(t, svc.authCacheL1)
	t.Cleanup(func() { svc.authCacheL1.Close() })

	first, err := svc.GetByKey(context.Background(), "sk-l2-ip")
	require.NoError(t, err)
	require.NotNil(t, first.CompiledIPWhitelist)
	require.NotNil(t, first.CompiledIPBlacklist)
	// The value returned by the L2 provider is immutable and must not be patched.
	require.Nil(t, entry.Snapshot.CompiledIPWhitelist)
	require.Nil(t, entry.Snapshot.CompiledIPBlacklist)
	svc.authCacheL1.Wait()
	require.Eventually(t, func() bool {
		_, ok := svc.authCacheL1.Get(svc.authCacheKey("sk-l2-ip"))
		return ok
	}, time.Second, time.Millisecond)

	second, err := svc.GetByKey(context.Background(), "sk-l2-ip")
	require.NoError(t, err)
	// The second lookup must use the compiled rules promoted into L1.
	l1Value, ok := svc.authCacheL1.Get(svc.authCacheKey("sk-l2-ip"))
	require.True(t, ok)
	l1Entry := l1Value.(*APIKeyAuthCacheEntry)
	require.Same(t, l1Entry.Snapshot.CompiledIPWhitelist, second.CompiledIPWhitelist)
	require.Same(t, l1Entry.Snapshot.CompiledIPBlacklist, second.CompiledIPBlacklist)
	require.Equal(t, int32(1), cache.calls.Load(), "L1 should serve the backfilled L2 snapshot")
}

func TestAPIKeyAuthSnapshotReusesCompiledIPRulesForL1(t *testing.T) {
	key := &APIKey{
		ID: 1, UserID: 2, Key: "sk-ip-l1", Status: StatusActive,
		IPWhitelist: []string{"203.0.113.0/24"},
		IPBlacklist: []string{"203.0.113.42"},
		User:        &User{ID: 2, Status: StatusActive},
	}
	key.CompiledIPWhitelist = ippkg.CompileIPRules(key.IPWhitelist)
	key.CompiledIPBlacklist = ippkg.CompileIPRules(key.IPBlacklist)

	snapshot := (&APIKeyService{}).snapshotFromAPIKey(context.Background(), key)
	require.Same(t, key.CompiledIPWhitelist, snapshot.CompiledIPWhitelist)
	require.Same(t, key.CompiledIPBlacklist, snapshot.CompiledIPBlacklist)

	materialized := (&APIKeyService{}).snapshotToAPIKey(key.Key, snapshot)
	require.Same(t, key.CompiledIPWhitelist, materialized.CompiledIPWhitelist)
	require.Same(t, key.CompiledIPBlacklist, materialized.CompiledIPBlacklist)
}

func TestAPIKeyAuthSnapshotCompilesIPRulesAfterL2RoundTrip(t *testing.T) {
	key := &APIKey{
		ID: 1, UserID: 2, Key: "sk-ip-l2", Status: StatusActive,
		IPWhitelist: []string{"198.51.100.0/24"},
		IPBlacklist: []string{"198.51.100.42"},
		User:        &User{ID: 2, Status: StatusActive},
	}

	svc := &APIKeyService{}
	payload, err := json.Marshal(&APIKeyAuthCacheEntry{Snapshot: svc.snapshotFromAPIKey(context.Background(), key)})
	require.NoError(t, err)
	var cached APIKeyAuthCacheEntry
	require.NoError(t, json.Unmarshal(payload, &cached))

	materialized, used, err := svc.applyAuthCacheEntry(key.Key, &cached)
	require.NoError(t, err)
	require.True(t, used)
	require.NotNil(t, materialized.CompiledIPWhitelist)
	require.NotNil(t, materialized.CompiledIPBlacklist)

	ok, reason := ippkg.CheckIPRestrictionWithCompiledRules("198.51.100.10", materialized.CompiledIPWhitelist, materialized.CompiledIPBlacklist)
	require.True(t, ok, reason)
	ok, reason = ippkg.CheckIPRestrictionWithCompiledRules("198.51.100.42", materialized.CompiledIPWhitelist, materialized.CompiledIPBlacklist)
	require.False(t, ok, reason)
}
