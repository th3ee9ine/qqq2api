package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

// Expose the production atomic capability to scheduler tests; the historical
// test cache alone exercises only the legacy adapter path.
type atomicSchedulerGatewayCache struct {
	schedulerTestGatewayCache
	mu sync.Mutex
}

func (c *atomicSchedulerGatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, hash string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.schedulerTestGatewayCache.GetSessionAccountID(ctx, groupID, hash)
}

func (c *atomicSchedulerGatewayCache) ClaimSessionAccountID(ctx context.Context, groupID int64, hash string, id int64, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old := c.sessionBindings[hash]; old > 0 && old != id {
		return false, nil
	}
	return true, c.SetSessionAccountID(ctx, groupID, hash, id, ttl)
}

func (c *atomicSchedulerGatewayCache) CompareAndSetSessionAccountID(ctx context.Context, groupID int64, hash string, expected, id int64, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if old := c.sessionBindings[hash]; old > 0 && old != expected && old != id {
		return false, nil
	}
	return true, c.SetSessionAccountID(ctx, groupID, hash, id, ttl)
}

func (c *atomicSchedulerGatewayCache) CompareAndDeleteSessionAccountID(_ context.Context, _ int64, hash string, expected int64) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionBindings[hash] != expected {
		return false, nil
	}
	delete(c.sessionBindings, hash)
	return true, nil
}

func TestOpenAIStickyAtomicPreviousResponseKeepsResponseOwner(t *testing.T) {
	ctx := context.Background()
	groupID := int64(9)
	account := Account{
		ID: 1001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 2,
		Extra: map[string]any{"openai_apikey_responses_websockets_v2_enabled": true},
	}
	cache := &atomicSchedulerGatewayCache{schedulerTestGatewayCache: schedulerTestGatewayCache{
		sessionBindings: map[string]int64{"openai:session": 2002},
	}}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{account}},
		cache:       cache, cfg: cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_atomic", account.ID, time.Hour))
	scheduler := newDefaultOpenAIAccountScheduler(svc, nil)
	selection, decision, err := scheduler.Select(ctx, OpenAIAccountScheduleRequest{
		GroupID: &groupID, Platform: PlatformOpenAI, PreviousResponseID: "resp_atomic",
		SessionHash: "session", RequestedModel: "gpt-5.1", RequiredTransport: OpenAIUpstreamTransportAny,
	})
	require.NoError(t, err)
	require.NotNil(t, selection)
	t.Cleanup(selection.ReleaseFunc)
	require.Equal(t, account.ID, selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
	require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, &groupID, "session", account.ID))
	owner, err := cache.GetSessionAccountID(ctx, groupID, "openai:session")
	require.NoError(t, err)
	require.Equal(t, int64(2002), owner)
}

func TestOpenAIStickyAtomicWeightedMigrationAndTerminalBind(t *testing.T) {
	for _, mode := range []string{"weighted", "failover"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			cache := &atomicSchedulerGatewayCache{schedulerTestGatewayCache: schedulerTestGatewayCache{
				sessionBindings: map[string]int64{"openai:session": 101},
			}}
			svc := &OpenAIGatewayService{cache: cache}
			req := OpenAIAccountScheduleRequest{SessionHash: "session", StickyAccountID: 101, StickyWeighted: mode == "weighted"}
			if mode == "failover" {
				req.ExcludedIDs = map[int64]struct{}{101: {}}
			}
			require.NoError(t, svc.bindOpenAISelectionSticky(ctx, req, 202))
			require.ErrorIs(t, svc.bindOpenAISelectionSticky(ctx, req, 303), ErrStickySessionClaimConflict)
			require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, nil, "session", 101))
			owner, err := cache.GetSessionAccountID(ctx, 0, "openai:session")
			require.NoError(t, err)
			require.Equal(t, int64(202), owner)
		})
	}
}

func TestOpenAIStickyAtomicProfitGateDefersWeightedBind(t *testing.T) {
	cache := &atomicSchedulerGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	ctx := context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{})
	req := OpenAIAccountScheduleRequest{SessionHash: "profit-session", StickyWeighted: true}
	require.NoError(t, svc.bindOpenAISelectionSticky(ctx, req, 101))
	require.Empty(t, cache.sessionBindings)
	require.NoError(t, svc.BindStickySessionAfterProfitAdmission(ctx, nil, req.SessionHash, 202))
	require.Equal(t, int64(202), cache.sessionBindings["openai:profit-session"])
}
