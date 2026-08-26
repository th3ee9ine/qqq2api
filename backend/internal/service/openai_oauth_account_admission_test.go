//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func testOpenAIOAuthAdmissionPolicy(interval, maxWait time.Duration) openAIOAuthAdmissionPolicy {
	return openAIOAuthAdmissionPolicy{
		baseInterval:          interval,
		highQuotaInterval:     2 * interval,
		criticalQuotaInterval: 3 * interval,
		severeQuotaInterval:   4 * interval,
		maxQueueWait:          maxWait,
		stateTTL:              time.Minute,
		maxAccounts:           128,
	}
}

func testOpenAIOAuthAdmissionAccount(id int64) *Account {
	return &Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra:    map[string]any{},
	}
}

func TestOpenAIOAuthAdmissionControllerSpacesConcurrentStarts(t *testing.T) {
	const workers = 4
	interval := 15 * time.Millisecond
	controller := newOpenAIOAuthAdmissionController(testOpenAIOAuthAdmissionPolicy(interval, 250*time.Millisecond))

	start := make(chan struct{})
	times := make(chan time.Time, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := controller.wait(context.Background(), 101, interval, time.Time{})
			errs <- err
			if err == nil {
				times <- time.Now()
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	close(times)

	for err := range errs {
		require.NoError(t, err)
	}
	started := make([]time.Time, 0, workers)
	for at := range times {
		started = append(started, at)
	}
	require.Len(t, started, workers)
	sort.Slice(started, func(i, j int) bool { return started[i].Before(started[j]) })
	require.GreaterOrEqual(t, started[len(started)-1].Sub(started[0]), 3*interval-8*time.Millisecond)
}

func TestOpenAIOAuthAdmissionControllerHasBoundedQueue(t *testing.T) {
	interval := 30 * time.Millisecond
	controller := newOpenAIOAuthAdmissionController(testOpenAIOAuthAdmissionPolicy(interval, 40*time.Millisecond))
	require.NoError(t, controller.wait(context.Background(), 102, interval, time.Time{}))

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- controller.wait(context.Background(), 102, interval, time.Time{})
	}()
	require.Eventually(t, func() bool {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		state := controller.accounts[102]
		return state != nil && time.Until(state.nextStart) > interval
	}, 100*time.Millisecond, time.Millisecond)

	err := controller.wait(context.Background(), 102, interval, time.Time{})
	var admissionErr *OpenAIOAuthAdmissionError
	require.ErrorAs(t, err, &admissionErr)
	require.Equal(t, "bounded_queue_full", admissionErr.Reason)
	require.Positive(t, admissionErr.RetryAfter)
	require.NoError(t, <-secondDone)
}

func TestOpenAIOAuthAdmissionControllerWaitIsContextCancelable(t *testing.T) {
	interval := 100 * time.Millisecond
	controller := newOpenAIOAuthAdmissionController(testOpenAIOAuthAdmissionPolicy(interval, 250*time.Millisecond))
	require.NoError(t, controller.wait(context.Background(), 103, interval, time.Time{}))

	ctx, cancel := context.WithCancel(context.Background())
	startedAt := time.Now()
	done := make(chan error, 1)
	go func() { done <- controller.wait(ctx, 103, interval, time.Time{}) }()
	time.Sleep(10 * time.Millisecond)
	cancel()

	require.ErrorIs(t, <-done, context.Canceled)
	require.Less(t, time.Since(startedAt), 80*time.Millisecond)
}

func TestOpenAIOAuthAdmissionIntervalUsesRPMAndResetAwareQuotaPressure(t *testing.T) {
	now := time.Now()
	policy := defaultOpenAIOAuthAdmissionPolicy()
	account := testOpenAIOAuthAdmissionAccount(104)
	account.Extra = map[string]any{
		"codex_5h_used_percent":  92.0,
		"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339Nano),
		"codex_usage_updated_at": now.Format(time.RFC3339Nano),
	}
	require.Equal(t, policy.criticalQuotaInterval, openAIOAuthAdmissionInterval(account, now, policy))

	// Once the provider window resets, the cached percentage no longer slows
	// new requests.
	account.Extra["codex_5h_reset_at"] = now.Add(-time.Second).Format(time.RFC3339Nano)
	require.Equal(t, policy.baseInterval, openAIOAuthAdmissionInterval(account, now, policy))

	// base_rpm and explicit per-account spacing are lower bounds, never ways to
	// make the default guard faster.
	account.Extra["base_rpm"] = 30
	require.Equal(t, 2*time.Second, openAIOAuthAdmissionInterval(account, now, policy))
	account.Extra["openai_min_request_interval_ms"] = 3000
	require.Equal(t, 3*time.Second, openAIOAuthAdmissionInterval(account, now, policy))
}

func TestWaitForOpenAIOAuthAccountAdmissionHonorsRuntimeAndQuotaReset(t *testing.T) {
	policy := testOpenAIOAuthAdmissionPolicy(5*time.Millisecond, 20*time.Millisecond)
	svc := &OpenAIGatewayService{openAIOAuthAdmission: newOpenAIOAuthAdmissionController(policy)}
	account := testOpenAIOAuthAdmissionAccount(105)

	blockedUntil := time.Now().Add(150 * time.Millisecond)
	svc.openaiAccountRuntimeBlockUntil.Store(account.ID, blockedUntil)
	err := svc.WaitForOpenAIOAuthAccountAdmission(context.Background(), account)
	var admissionErr *OpenAIOAuthAdmissionError
	require.ErrorAs(t, err, &admissionErr)
	require.Equal(t, "account_cooldown", admissionErr.Reason)
	require.Greater(t, admissionErr.RetryAfter, 100*time.Millisecond)

	svc.ClearAccountSchedulingBlock(account.ID)
	require.NoError(t, svc.WaitForOpenAIOAuthAccountAdmission(context.Background(), account), "explicit recovery must clear a long admission defer")
	now := time.Now()
	account.Extra = map[string]any{
		"codex_5h_used_percent":   95.0,
		"codex_5h_reset_at":       now.Add(2 * time.Minute).Format(time.RFC3339Nano),
		"codex_usage_updated_at":  now.Format(time.RFC3339Nano),
		"auto_pause_5h_threshold": 0.90,
		"auto_pause_7d_threshold": 0.99,
	}
	err = svc.WaitForOpenAIOAuthAccountAdmission(context.Background(), account)
	require.ErrorAs(t, err, &admissionErr)
	require.Equal(t, "quota_5h", admissionErr.Reason)
	require.Greater(t, admissionErr.RetryAfter, time.Minute)
}

func TestOpenAIOAuth429DefersAlreadyQueuedAdmission(t *testing.T) {
	interval := 40 * time.Millisecond
	policy := testOpenAIOAuthAdmissionPolicy(interval, 180*time.Millisecond)
	svc := &OpenAIGatewayService{openAIOAuthAdmission: newOpenAIOAuthAdmissionController(policy)}
	account := testOpenAIOAuthAdmissionAccount(106)
	require.NoError(t, svc.WaitForOpenAIOAuthAccountAdmission(context.Background(), account))

	done := make(chan error, 1)
	go func() {
		done <- svc.WaitForOpenAIOAuthAccountAdmission(context.Background(), account)
	}()
	require.Eventually(t, func() bool {
		svc.openAIOAuthAdmission.mu.Lock()
		defer svc.openAIOAuthAdmission.mu.Unlock()
		state := svc.openAIOAuthAdmission.accounts[account.ID]
		return state != nil && time.Until(state.nextStart) > interval
	}, 100*time.Millisecond, time.Millisecond)

	// A transient 429 grants only the existing bounded same-account retry and
	// advances all other queued admissions to the shared retry-not-before.
	svc.markOpenAIOAuth429RateLimited(context.Background(), account, http.Header{}, []byte(`{"error":{"message":"rate limited"}}`))
	err := <-done
	var admissionErr *OpenAIOAuthAdmissionError
	require.ErrorAs(t, err, &admissionErr)
	require.Equal(t, "bounded_queue_full", admissionErr.Reason)
}

func TestOpenAIOAuthAdmissionStateIsBounded(t *testing.T) {
	policy := testOpenAIOAuthAdmissionPolicy(time.Millisecond, 10*time.Millisecond)
	policy.maxAccounts = 2
	controller := newOpenAIOAuthAdmissionController(policy)
	require.NoError(t, controller.wait(context.Background(), 201, time.Millisecond, time.Time{}))
	require.NoError(t, controller.wait(context.Background(), 202, time.Millisecond, time.Time{}))

	err := controller.wait(context.Background(), 203, time.Millisecond, time.Time{})
	var admissionErr *OpenAIOAuthAdmissionError
	require.True(t, errors.As(err, &admissionErr))
	require.Equal(t, "local_state_capacity", admissionErr.Reason)
	require.Len(t, controller.accounts, 2)
}

type tokenCountStickyWriteProbe struct {
	*schedulerTestGatewayCache
	setCalls     int
	refreshCalls int
	deleteCalls  int
}

func (c *tokenCountStickyWriteProbe) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	c.setCalls++
	return c.schedulerTestGatewayCache.SetSessionAccountID(ctx, groupID, sessionHash, accountID, ttl)
}

func (c *tokenCountStickyWriteProbe) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	c.refreshCalls++
	return c.schedulerTestGatewayCache.RefreshSessionTTL(ctx, groupID, sessionHash, ttl)
}

func (c *tokenCountStickyWriteProbe) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	c.deleteCalls++
	return c.schedulerTestGatewayCache.DeleteSessionAccountID(ctx, groupID, sessionHash)
}

func TestSelectAccountForTokenCountReadsButNeverWritesStickyState(t *testing.T) {
	groupID := int64(301)
	accounts := []Account{
		{
			ID: 30101, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"openai_capabilities": []any{"chat_completions"}},
		},
		{
			ID: 30102, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5,
			GroupIDs:    []int64{groupID},
			Credentials: map[string]any{"openai_capabilities": []any{"chat_completions"}},
		},
	}
	cache := &tokenCountStickyWriteProbe{schedulerTestGatewayCache: &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}}
	svc := &OpenAIGatewayService{
		accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:       cache,
		cfg:         &config.Config{},
	}

	selected, err := svc.SelectAccountForTokenCount(
		context.Background(), &groupID, "count-miss", "gpt-5.1",
		OpenAIEndpointCapabilityChatCompletions, PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(30101), selected.ID)
	require.Zero(t, cache.setCalls, "a token-count miss must not create affinity")
	require.Zero(t, cache.refreshCalls)
	require.Zero(t, cache.deleteCalls)

	cache.sessionBindings["openai:count-hit"] = 30102
	selected, err = svc.SelectAccountForTokenCount(
		context.Background(), &groupID, "count-hit", "gpt-5.1",
		OpenAIEndpointCapabilityChatCompletions, PlatformOpenAI,
	)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, int64(30102), selected.ID, "an existing sticky binding remains readable")
	require.Zero(t, cache.setCalls)
	require.Zero(t, cache.refreshCalls, "token counting must not extend sticky TTL")
	require.Zero(t, cache.deleteCalls)
}

func TestOpenAISameAccountRetryTargetRejectsSilentCredentialSwitch(t *testing.T) {
	ctx := WithOpenAISameAccountRetryTarget(context.Background(), 901)
	other := &Account{
		ID:          902,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
	}

	require.Equal(t,
		"same_account_retry_mismatch",
		openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, other, PlatformOpenAI, "", false, ""),
	)
	scheduler := &defaultOpenAIAccountScheduler{service: &OpenAIGatewayService{}}
	compatible, reason := scheduler.isAccountRequestCompatibleReason(ctx, other, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI})
	require.False(t, compatible)
	require.Equal(t, "same_account_retry_mismatch", reason)

	cleared := WithOpenAISameAccountRetryTarget(ctx, 0)
	require.Zero(t, openAISameAccountRetryTarget(cleared))
}

func TestOpenAISameAccountRetryTargetPinsSchedulerPass(t *testing.T) {
	groupID := int64(903)
	target := Account{
		ID: 90301, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 10,
	}
	preferredOther := Account{
		ID: 90302, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 0,
	}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{preferredOther, target}},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	ctx := WithOpenAISameAccountRetryTarget(context.Background(), target.ID)

	selection, _, err := svc.SelectAccountWithSchedulerForCapability(
		ctx, &groupID, "", "", "", nil,
		OpenAIUpstreamTransportHTTPSSE, "", false, false, true,
	)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, target.ID, selection.Account.ID)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}
