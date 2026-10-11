package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type ttftHistoryCacheStub struct {
	GatewayCache
	mu                 sync.Mutex
	history            map[int64][]OpenAIOAuthTTFTObservation
	appendErr, readErr error
	reads              int
	readIDs            []int64
}

func (c *ttftHistoryCacheStub) AppendOpenAIOAuthTTFTObservation(_ context.Context, id int64, observation OpenAIOAuthTTFTObservation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.appendErr != nil {
		return c.appendErr
	}
	if c.history == nil {
		c.history = make(map[int64][]OpenAIOAuthTTFTObservation)
	}
	c.history[id] = append(cloneOpenAIOAuthTTFTObservations([]OpenAIOAuthTTFTObservation{observation}), c.history[id]...)
	if len(c.history[id]) > OpenAIOAuthTTFTHistoryLimit {
		c.history[id] = c.history[id][:OpenAIOAuthTTFTHistoryLimit]
	}
	return nil
}

func (c *ttftHistoryCacheStub) GetOpenAIOAuthTTFTObservations(_ context.Context, ids []int64) (map[int64][]OpenAIOAuthTTFTObservation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	c.readIDs = append([]int64(nil), ids...)
	if c.readErr != nil {
		return nil, c.readErr
	}
	result := make(map[int64][]OpenAIOAuthTTFTObservation)
	for _, id := range ids {
		result[id] = cloneOpenAIOAuthTTFTObservations(c.history[id])
	}
	return result, nil
}

func newTTFTHistoryGateway(t *testing.T, cache GatewayCache) *OpenAIGatewayService {
	t.Helper()
	svc := &OpenAIGatewayService{cache: cache, settingService: newEnabledTTFTGuardSettingService(t, 20)}
	t.Cleanup(svc.stopOpenAIOAuthTTFTGuardWorker)
	return svc
}

func TestOpenAIOAuthTTFTHistoryRetainsLatestActualMeasurements(t *testing.T) {
	svc := newTTFTHistoryGateway(t, nil)
	account := newTTFTProbeAccount()
	fast, slow, zero, negative := 20000, 20001, 0, -1
	for _, measurement := range []*int{&slow, &slow, &fast, nil, &negative, &zero, &slow} {
		svc.ObserveOpenAIOAuthTTFT(account, " gpt-5.4 ", measurement)
	}
	// The caller retains ownership of the input pointer.
	slow = 99999
	histories, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	history := histories[account.ID]
	require.Equal(t, 20000, history.ThresholdMs)
	require.Len(t, history.RecentRequests, 5)
	want := []*int{new(int), &zero, nil, nil, &fast}
	*want[0] = 20001
	for i, observation := range history.RecentRequests {
		require.Equal(t, want[i], observation.FirstTokenMs)
		require.Equal(t, "gpt-5.4", observation.Model)
		require.WithinDuration(t, time.Now(), observation.ObservedAt, time.Second)
		if i > 0 {
			require.False(t, observation.ObservedAt.After(history.RecentRequests[i-1].ObservedAt))
		}
	}
	*history.RecentRequests[0].FirstTokenMs = 123
	history.RecentRequests[1].Model = "modified"
	again, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Equal(t, 20001, *again[account.ID].RecentRequests[0].FirstTokenMs)
	require.Equal(t, "gpt-5.4", again[account.ID].RecentRequests[1].Model)
}

func TestOpenAIOAuthTTFTHistorySurvivesPauseAndStreakResets(t *testing.T) {
	svc := newTTFTHistoryGateway(t, nil)
	repo := &ttftGuardAccountRepoStub{changed: true}
	svc.accountRepo = repo
	account, slow := newTTFTProbeAccount(), 20001
	for i := 0; i < 5; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
	}
	require.Equal(t, 1, repo.pauses)
	history, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Len(t, history[account.ID].RecentRequests, 5, "trigger consumption must retain its five actual requests")
	svc.ObserveOpenAIOAuthTTFT(account, "", nil)
	fast := 50
	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &fast)
	history, err = svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Len(t, history[account.ID].RecentRequests, 5)
	require.Equal(t, fast, *history[account.ID].RecentRequests[0].FirstTokenMs)
	require.Nil(t, history[account.ID].RecentRequests[1].FirstTokenMs)
	require.Empty(t, history[account.ID].RecentRequests[1].Model)
	require.Equal(t, slow, *history[account.ID].RecentRequests[2].FirstTokenMs)
}

func TestOpenAIOAuthTTFTHistoryEligibilityAndEmptyArrays(t *testing.T) {
	cache := &ttftHistoryCacheStub{}
	svc := newTTFTHistoryGateway(t, cache)
	parent := int64(1)
	accounts := []Account{
		{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeSetupToken},
		{ID: 3, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 4, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent},
		{ID: 0, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
	}
	for i := 2; i < len(accounts); i++ {
		svc.ObserveOpenAIOAuthTTFT(&accounts[i], "gpt-5.4", nil)
	}
	svc.ObserveOpenAIOAuthTTFT(&accounts[0], "gpt-image-2", nil)
	accounts = append(accounts, accounts[0])
	history, err := svc.GetOpenAIOAuthTTFTHistory(nil, accounts)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, []int64{1, 2}, cache.readIDs)
	require.Equal(t, 1, cache.reads, "eligible accounts must be fetched in one batch")
	for _, item := range history {
		require.NotNil(t, item.RecentRequests)
		require.Empty(t, item.RecentRequests)
		encoded, err := json.Marshal(item)
		require.NoError(t, err)
		require.JSONEq(t, `{"threshold_ms":20000,"recent_requests":[]}`, string(encoded))
	}
	require.Empty(t, cache.history)
}

func TestOpenAIOAuthTTFTHistoryDisabledAndSettingsErrors(t *testing.T) {
	cache := &ttftHistoryCacheStub{}
	svc := newTTFTHistoryGateway(t, cache)
	account := newTTFTProbeAccount()
	value := 100
	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &value)
	enabled := svc.settingService
	svc.settingService = nil
	svc.ObserveOpenAIOAuthTTFT(account, "disabled-call", &value)
	history, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Empty(t, history)
	require.Zero(t, cache.reads)
	svc.settingService = NewSettingService(&ttftGuardSettingRepo{err: errors.New("settings unavailable")}, nil)
	history, err = svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.ErrorContains(t, err, "settings unavailable")
	require.Empty(t, history)
	svc.settingService = enabled
	history, err = svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Len(t, history[account.ID].RecentRequests, 1)
	require.Equal(t, "gpt-5.4", history[account.ID].RecentRequests[0].Model)
}

func TestOpenAIOAuthTTFTHistorySharesAcrossInstancesAndFallsBackLocally(t *testing.T) {
	cache := &ttftHistoryCacheStub{}
	first, second := newTTFTHistoryGateway(t, cache), newTTFTHistoryGateway(t, cache)
	account, latency := newTTFTProbeAccount(), 100
	first.ObserveOpenAIOAuthTTFT(account, "first", &latency)
	second.ObserveOpenAIOAuthTTFT(account, "second", &latency)
	history, err := first.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Len(t, history[account.ID].RecentRequests, 2)
	require.Equal(t, "second", history[account.ID].RecentRequests[0].Model)
	cache.appendErr = errors.New("write unavailable")
	first.ObserveOpenAIOAuthTTFT(account, "local-only", &latency)
	// A successful read after a failed write must not replace newer local data
	// with the older distributed list or duplicate already persisted requests.
	history, err = first.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Len(t, history[account.ID].RecentRequests, 3)
	require.Equal(t, "local-only", history[account.ID].RecentRequests[0].Model)
	cache.readErr = errors.New("read unavailable")
	again, err := first.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Equal(t, history, again)
	*again[account.ID].RecentRequests[0].FirstTokenMs = 0
	require.Equal(t, 100, *history[account.ID].RecentRequests[0].FirstTokenMs)
}

func TestOpenAIOAuthTTFTHistoryConcurrentReadsAndObservations(t *testing.T) {
	cache := &ttftHistoryCacheStub{}
	svc := newTTFTHistoryGateway(t, cache)
	account := newTTFTProbeAccount()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			value := i
			svc.ObserveOpenAIOAuthTTFT(account, fmt.Sprintf("call-%d", i), &value)
			history, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
			if !assertTTFTHistoryBounded(t, history, account.ID, err) {
				return
			}
			*history[account.ID].RecentRequests[0].FirstTokenMs = -1
		}(i)
	}
	wg.Wait()
	value := 999
	svc.ObserveOpenAIOAuthTTFT(account, "last", &value)
	history, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Len(t, history[account.ID].RecentRequests, 5)
	require.Equal(t, "last", history[account.ID].RecentRequests[0].Model)
	require.Equal(t, 999, *history[account.ID].RecentRequests[0].FirstTokenMs)
	seen := map[string]bool{}
	for _, observation := range history[account.ID].RecentRequests {
		require.False(t, seen[observation.Model])
		seen[observation.Model] = true
		require.NotNil(t, observation.FirstTokenMs)
		require.GreaterOrEqual(t, *observation.FirstTokenMs, 0)
	}
}

func assertTTFTHistoryBounded(t *testing.T, history map[int64]*OpenAIOAuthTTFTHistory, id int64, err error) bool {
	t.Helper()
	if err != nil || history[id] == nil || len(history[id].RecentRequests) == 0 || len(history[id].RecentRequests) > 5 {
		t.Errorf("invalid concurrent history: history=%v err=%v", history, err)
		return false
	}
	return true
}

func TestOpenAIOAuthTTFTHistoryRecoveryProbeDoesNotAddObservation(t *testing.T) {
	svc := newTTFTHistoryGateway(t, nil)
	account := ttftGuardPausedAccount()
	svc.cfg = &config.Config{}
	finished := false
	svc.accountRepo = &ttftGuardAccountRepoStub{
		claimFunc: func(context.Context, time.Time, string, time.Time) (*Account, error) { return account, nil },
		finishFunc: func(_ context.Context, _ int64, _ string, recovered bool, _ time.Time) (bool, error) {
			finished = true
			require.True(t, recovered)
			return true, nil
		},
	}
	svc.httpUpstream = ttftProbeUpstreamFunc(func(*http.Request, string, int64, int) (*http.Response, error) {
		return ttftProbeStreamResponse(ttftProbeSuccessfulEvents), nil
	})
	latency := 100
	svc.ObserveOpenAIOAuthTTFT(account, "actual", &latency)
	before, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.NoError(t, svc.RunOpenAIOAuthTTFTRecovery(context.Background()))
	require.True(t, finished)
	after, err := svc.GetOpenAIOAuthTTFTHistory(context.Background(), []Account{*account})
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestOpenAIOAuthTTFTHistoryMergePreservesIdenticalRequests(t *testing.T) {
	value := 100
	observation := OpenAIOAuthTTFTObservation{FirstTokenMs: &value, Model: "gpt-5.4", ObservedAt: time.Now()}
	merged := mergeOpenAIOAuthTTFTObservations([]OpenAIOAuthTTFTObservation{observation}, []OpenAIOAuthTTFTObservation{observation, observation})
	require.Len(t, merged, 2)
	*merged[0].FirstTokenMs = 200
	require.Equal(t, 100, *merged[1].FirstTokenMs)
	require.Equal(t, 100, value)
}

func TestOpenAIOAuthTTFTHistoryMergeDistinguishesUnknownZeroAndModels(t *testing.T) {
	zero, hundred := 0, 100
	now := time.Now()
	remote := []OpenAIOAuthTTFTObservation{
		{FirstTokenMs: nil, Model: "gpt-5.4", ObservedAt: now},
		{FirstTokenMs: &hundred, Model: "gpt-5.4", ObservedAt: now},
	}
	local := []OpenAIOAuthTTFTObservation{
		{FirstTokenMs: &zero, Model: "gpt-5.4", ObservedAt: now},
		{FirstTokenMs: &hundred, Model: "gpt-6-astra", ObservedAt: now},
	}
	merged := mergeOpenAIOAuthTTFTObservations(remote, local)
	require.Len(t, merged, 4)
	require.Equal(t, append(remote, local...), merged)
}
