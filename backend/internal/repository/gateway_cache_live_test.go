package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestGatewayCacheLiveCallIdentityAndController(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache, ok := NewGatewayCache(client).(service.LiveCallStore)
	require.True(t, ok)
	otherInstance, ok := NewGatewayCache(client).(service.LiveCallStore)
	require.True(t, ok)
	record := &service.LiveCallRecord{
		CallID:                "call_secret",
		CallHash:              HashLiveCallID("call_secret"),
		AccountID:             11,
		APIKeyID:              22,
		UserID:                33,
		GroupID:               44,
		LeaseID:               "lease",
		Model:                 "gpt-live-test",
		AttestationCiphertext: "encrypted-attestation",
		CreatedAt:             time.Now(),
		ExpiresAt:             time.Now().Add(time.Hour),
		Controller:            service.LiveControllerPending,
	}
	require.NoError(t, cache.SaveLiveCall(context.Background(), record, time.Hour))

	loaded, err := otherInstance.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, record.CallID, loaded.CallID)
	require.Equal(t, record.AccountID, loaded.AccountID)
	require.Equal(t, record.AttestationCiphertext, loaded.AttestationCiphertext)
	require.Nil(t, loaded.UpstreamOriginator)
	require.Nil(t, loaded.UpstreamUserAgent)
	require.Nil(t, loaded.UpstreamVersion)

	claimed, err := cache.ClaimLiveController(context.Background(), record.CallHash, service.LiveControllerObserver, "observer-1")
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = cache.ClaimLiveController(context.Background(), record.CallHash, service.LiveControllerProxy, "proxy-1")
	require.NoError(t, err)
	require.True(t, claimed)
	controller, err := cache.GetLiveController(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.Equal(t, service.LiveControllerProxy, controller)

	released, err := cache.ReleaseLiveController(context.Background(), record.CallHash, "proxy-1")
	require.NoError(t, err)
	require.True(t, released)
	closed, err := cache.MarkLiveCallClosed(context.Background(), record.CallHash, time.Hour)
	require.NoError(t, err)
	require.True(t, closed)
	closed, err = cache.MarkLiveCallClosed(context.Background(), record.CallHash, time.Hour)
	require.NoError(t, err)
	require.False(t, closed)
}

func TestGatewayCacheLiveCallUpstreamIdentityPreservesFieldPresence(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	cache, ok := NewGatewayCache(client).(service.LiveCallStore)
	require.True(t, ok)

	turnState := "secret-turn-state-must-not-load"
	emptyOriginator := ""
	userAgent := "live-agent/1.2.3"
	version := "1.2.3"
	record := &service.LiveCallRecord{
		CallID:             "call_identity_presence",
		CallHash:           HashLiveCallID("call_identity_presence"),
		LeaseID:            "lease",
		CreatedAt:          time.Now(),
		ExpiresAt:          time.Now().Add(time.Hour),
		Controller:         service.LiveControllerPending,
		UpstreamOriginator: &emptyOriginator,
		UpstreamUserAgent:  &userAgent,
		UpstreamVersion:    &version,
	}
	require.NoError(t, cache.SaveLiveCall(context.Background(), record, time.Hour))

	stored, err := client.HGetAll(context.Background(), liveCallKey(record.CallHash)).Result()
	require.NoError(t, err)
	require.NotContains(t, stored, "upstream_turn_state")
	require.Contains(t, stored, "upstream_originator")
	require.Empty(t, stored["upstream_originator"])
	require.Equal(t, userAgent, stored["upstream_user_agent"])
	require.Equal(t, version, stored["upstream_version"])

	// Historical cache hashes may still contain the retired field. Loading one
	// must ignore it instead of reintroducing the opaque value into runtime state.
	require.NoError(t, client.HSet(context.Background(), liveCallKey(record.CallHash), "upstream_turn_state", turnState).Err())

	loaded, err := cache.GetLiveCall(context.Background(), record.CallHash)
	require.NoError(t, err)
	require.NotContains(t, fmt.Sprintf("%+v", loaded), turnState)
	require.NotNil(t, loaded.UpstreamOriginator)
	require.Empty(t, *loaded.UpstreamOriginator)
	require.NotNil(t, loaded.UpstreamUserAgent)
	require.Equal(t, userAgent, *loaded.UpstreamUserAgent)
	require.NotNil(t, loaded.UpstreamVersion)
	require.Equal(t, version, *loaded.UpstreamVersion)
}
