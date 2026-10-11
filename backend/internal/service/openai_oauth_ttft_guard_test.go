package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type ttftGuardSettingRepo struct {
	SettingRepository
	value string
	err   error
}

func (r *ttftGuardSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyOpenAIOAuthTTFTGuardSettings {
		return "", ErrSettingNotFound
	}
	return r.value, r.err
}

func (r *ttftGuardSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func newEnabledTTFTGuardSettingService(t *testing.T, thresholdSeconds int) *SettingService {
	t.Helper()
	value, err := json.Marshal(OpenAIOAuthTTFTGuardSettings{
		Enabled:                    true,
		RecoveryIntervalMinutes:    30,
		FirstTokenThresholdSeconds: thresholdSeconds,
	})
	require.NoError(t, err)
	return NewSettingService(&ttftGuardSettingRepo{value: string(value)}, nil)
}

type ttftGuardAccountRepoStub struct {
	AccountRepository
	mu         sync.Mutex
	pauses     int
	model      string
	next       time.Time
	changed    bool
	pauseErr   error
	claimFunc  func(context.Context, time.Time, string, time.Time) (*Account, error)
	finishFunc func(context.Context, int64, string, bool, time.Time) (bool, error)
}

func (r *ttftGuardAccountRepoStub) PauseOpenAIOAuthTTFT(_ context.Context, _ int64, model string, _ time.Time, next time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pauses++
	r.model = model
	r.next = next
	return r.changed, r.pauseErr
}

func (r *ttftGuardAccountRepoStub) ClaimOpenAIOAuthTTFTProbe(ctx context.Context, now time.Time, token string, lease time.Time) (*Account, error) {
	if r.claimFunc != nil {
		return r.claimFunc(ctx, now, token, lease)
	}
	return nil, nil
}

func (r *ttftGuardAccountRepoStub) FinishOpenAIOAuthTTFTProbe(ctx context.Context, id int64, token string, recovered bool, next time.Time) (bool, error) {
	if r.finishFunc != nil {
		return r.finishFunc(ctx, id, token, recovered, next)
	}
	return false, nil
}

func TestObserveOpenAIOAuthTTFTPausesAfterFiveStrictlySlowCalls(t *testing.T) {
	repo := &ttftGuardAccountRepoStub{changed: true}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	latency := 15_001
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold-1; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &latency)
	}
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()

	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &latency)
	repo.mu.Lock()
	require.Equal(t, 1, repo.pauses)
	require.Equal(t, "gpt-5.3-codex", repo.model)
	require.True(t, repo.next.After(time.Now()))
	repo.mu.Unlock()
}

func TestObserveOpenAIOAuthTTFTIsDisabledByDefault(t *testing.T) {
	repo := &ttftGuardAccountRepoStub{changed: true}
	svc := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 47, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	latency := 15_001
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &latency)
	}
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()
}

func TestObserveOpenAIOAuthTTFTFastCallBreaksStreakAndSkipsUnsupportedAccounts(t *testing.T) {
	repo := &ttftGuardAccountRepoStub{changed: true}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
	account := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeSetupToken}
	slow := 15_001
	fast := 15_000
	for i := 0; i < 4; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &slow)
	}
	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &fast)
	svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &slow)
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()

	apiKey := &Account{ID: 44, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold; i++ {
		svc.ObserveOpenAIOAuthTTFT(apiKey, "gpt-5.3-codex", &slow)
	}
	shadowID := int64(45)
	shadow := &Account{ID: 45, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &shadowID}
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold; i++ {
		svc.ObserveOpenAIOAuthTTFT(shadow, "gpt-5.3-codex", &slow)
	}
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()
}

func TestObserveOpenAIOAuthTTFTUsesConfiguredThreshold(t *testing.T) {
	repo := &ttftGuardAccountRepoStub{changed: true}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 20)}
	account := &Account{ID: 46, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	nearDefault := 15_001
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &nearDefault)
	}
	repo.mu.Lock()
	require.Zero(t, repo.pauses)
	repo.mu.Unlock()

	configuredSlow := 20_001
	for i := 0; i < openAIOAuthTTFTSlowStreakThreshold; i++ {
		svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.3-codex", &configuredSlow)
	}
	repo.mu.Lock()
	require.Equal(t, 1, repo.pauses)
	repo.mu.Unlock()
}

func TestObserveOpenAIOAuthTTFTUnmeasuredCallsBreakStreak(t *testing.T) {
	negative, boundary, slow := -1, 15_000, 15_001
	for _, tt := range []struct {
		name, model string
		latency     *int
	}{
		{"nil", "gpt-5.4", nil}, {"negative", "gpt-5.4", &negative},
		{"boundary", "gpt-5.4", &boundary}, {"missing model", "  ", &slow},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ttftGuardAccountRepoStub{changed: true}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
			t.Cleanup(svc.stopOpenAIOAuthTTFTGuardWorker)
			account := newTTFTProbeAccount()
			for i := 0; i < 4; i++ {
				svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			}
			svc.ObserveOpenAIOAuthTTFT(account, tt.model, tt.latency)
			for i := 0; i < 4; i++ {
				svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			}
			require.Zero(t, repo.pauses)
			svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			require.Equal(t, 1, repo.pauses)
		})
	}
}

func TestObserveOpenAIOAuthTTFTEligibility(t *testing.T) {
	parent := int64(99)
	for _, tt := range []struct {
		name, model string
		account     *Account
	}{
		{"nil", "gpt-5.4", nil},
		{"invalid id", "gpt-5.4", &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
		{"API key", "gpt-5.4", &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}},
		{"other platform", "gpt-5.4", &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth}},
		{"shadow", "gpt-5.4", &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent}},
		{"image", "gpt-image-1", newTTFTProbeAccount()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ttftGuardAccountRepoStub{changed: true}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
			t.Cleanup(svc.stopOpenAIOAuthTTFTGuardWorker)
			slow := 16_000
			for i := 0; i < 5; i++ {
				svc.ObserveOpenAIOAuthTTFT(tt.account, tt.model, &slow)
			}
			require.Zero(t, repo.pauses)
		})
	}
}

func TestObserveOpenAIOAuthTTFTDisabledAndSettingFailuresResetStreak(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "setting error"}[failure], func(t *testing.T) {
			repo := &ttftGuardAccountRepoStub{changed: true}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
			t.Cleanup(svc.stopOpenAIOAuthTTFTGuardWorker)
			account, slow := newTTFTProbeAccount(), 16_000
			for i := 0; i < 4; i++ {
				svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			}
			svc.settingService = nil
			if failure {
				svc.settingService = NewSettingService(&ttftGuardSettingRepo{err: errors.New("settings unavailable")}, nil)
			}
			svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			svc.settingService = newEnabledTTFTGuardSettingService(t, 15)
			for i := 0; i < 4; i++ {
				svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			}
			require.Zero(t, repo.pauses)
			svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			require.Equal(t, 1, repo.pauses)
		})
	}
}

func TestObserveOpenAIOAuthTTFTPauseCASPreservesOtherState(t *testing.T) {
	for _, tt := range []struct {
		name     string
		changed  bool
		pauseErr error
	}{
		{"another pause won", false, nil}, {"database failed", false, errors.New("write failed")},
		{"committed but cache refresh failed", true, errors.New("cache refresh failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &ttftGuardAccountRepoStub{changed: tt.changed, pauseErr: tt.pauseErr}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
			t.Cleanup(svc.stopOpenAIOAuthTTFTGuardWorker)
			account, slow := newTTFTProbeAccount(), 16_000
			until := time.Now().Add(time.Hour)
			account.TempUnschedulableUntil = &until
			account.TempUnschedulableReason = "manual-reason"
			account.Extra["other_state"] = "preserved"
			for i := 0; i < 5; i++ {
				svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			}
			require.Equal(t, 1, repo.pauses)
			require.Equal(t, tt.changed, account.IsOpenAIOAuthTTFTPaused())
			require.Equal(t, "preserved", account.Extra["other_state"])
			require.Equal(t, &until, account.TempUnschedulableUntil)
			require.Equal(t, "manual-reason", account.TempUnschedulableReason)
			require.True(t, account.Schedulable)
		})
	}
}

type ttftGuardStreakCache struct {
	GatewayCache
	mu     sync.Mutex
	count  int64
	resets int
	err    error
}

func (c *ttftGuardStreakCache) IncrementOpenAIOAuthTTFTStreak(context.Context, int64, time.Duration) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return 0, c.err
	}
	c.count++
	return c.count, nil
}
func (c *ttftGuardStreakCache) ResetOpenAIOAuthTTFTStreak(context.Context, int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.resets++
	if c.err != nil {
		return c.err
	}
	c.count = 0
	return nil
}

func TestObserveOpenAIOAuthTTFTDistributedStreakAndCacheFailureFallback(t *testing.T) {
	for _, cacheFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "shared across instances", true: "local fallback"}[cacheFails], func(t *testing.T) {
			cache := &ttftGuardStreakCache{}
			if cacheFails {
				cache.err = errors.New("redis unavailable")
			}
			repo := &ttftGuardAccountRepoStub{changed: true}
			first := &OpenAIGatewayService{accountRepo: repo, cache: cache, settingService: newEnabledTTFTGuardSettingService(t, 15)}
			second := &OpenAIGatewayService{accountRepo: repo, cache: cache, settingService: newEnabledTTFTGuardSettingService(t, 15)}
			t.Cleanup(first.stopOpenAIOAuthTTFTGuardWorker)
			t.Cleanup(second.stopOpenAIOAuthTTFTGuardWorker)
			account, slow := newTTFTProbeAccount(), 16_000
			for i := 0; i < 5; i++ {
				svc := first
				if !cacheFails && i%2 == 1 {
					svc = second
				}
				svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow)
			}
			require.Equal(t, 1, repo.pauses)
			require.Greater(t, cache.resets, 0)
		})
	}
}

func ttftGuardPausedAccount() *Account {
	account := newTTFTProbeAccount()
	account.Extra[OpenAIOAuthTTFTGuardStateExtraKey] = map[string]any{
		"paused_at":     time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"next_probe_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), "model": "gpt-5.4",
	}
	return account
}

func TestRunOpenAIOAuthTTFTRecoveryClaimProbeFinish(t *testing.T) {
	for _, tt := range []struct {
		name      string
		enabled   bool
		events    string
		recovered bool
	}{
		{"success", true, ttftProbeSuccessfulEvents, true},
		{"disabled still recovers prior pause", false, ttftProbeSuccessfulEvents, true},
		{"incomplete stream stays paused", true, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := ttftGuardPausedAccount()
			var claimedToken string
			var probeDeadline, leaseDeadline time.Time
			finishes, upstreamCalls := 0, 0
			repo := &ttftGuardAccountRepoStub{
				claimFunc: func(ctx context.Context, now time.Time, token string, lease time.Time) (*Account, error) {
					claimedToken, leaseDeadline = token, lease
					require.Len(t, token, 32)
					require.InDelta(t, 340, lease.Sub(now).Seconds(), .01)
					deadline, ok := ctx.Deadline()
					require.True(t, ok)
					require.Greater(t, time.Until(deadline), 314*time.Second)
					return account, nil
				},
				finishFunc: func(ctx context.Context, id int64, token string, recovered bool, next time.Time) (bool, error) {
					finishes++
					require.NoError(t, ctx.Err())
					require.Equal(t, account.ID, id)
					require.Equal(t, claimedToken, token)
					require.Equal(t, tt.recovered, recovered)
					require.InDelta(t, 30*60, time.Until(next).Seconds(), 1)
					return true, nil
				},
			}
			value, _ := json.Marshal(OpenAIOAuthTTFTGuardSettings{Enabled: tt.enabled, FirstTokenThresholdSeconds: 300, RecoveryIntervalMinutes: 30})
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: NewSettingService(&ttftGuardSettingRepo{value: string(value)}, nil), cfg: &config.Config{}, httpUpstream: ttftProbeUpstreamFunc(func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				upstreamCalls++
				var ok bool
				probeDeadline, ok = req.Context().Deadline()
				require.True(t, ok)
				return ttftProbeStreamResponse(tt.events), nil
			})}
			require.NoError(t, svc.RunOpenAIOAuthTTFTRecovery(context.Background()))
			require.Equal(t, 1, finishes)
			require.Equal(t, 1, upstreamCalls)
			require.True(t, leaseDeadline.After(probeDeadline.Add(openAIOAuthTTFTRecoveryOuterGrace)))
		})
	}
}

func TestObserveOpenAIOAuthTTFTConcurrentTriggerConsumption(t *testing.T) {
	repo := &ttftGuardAccountRepoStub{}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
	t.Cleanup(svc.stopOpenAIOAuthTTFTGuardWorker)
	account, slow := newTTFTProbeAccount(), 16_000
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); svc.ObserveOpenAIOAuthTTFT(account, "gpt-5.4", &slow) }()
	}
	wg.Wait()
	// The repository may reject a pause because another subsystem already
	// paused the account. Each complete group of five still consumes exactly
	// one trigger without erasing another goroutine's newer observations.
	require.Equal(t, 10, repo.pauses)
}

func TestRunOpenAIOAuthTTFTRecoveryNoCandidateAndRepositoryErrors(t *testing.T) {
	claimErr, finishErr := errors.New("claim failed"), errors.New("finish failed")
	for _, tt := range []struct {
		name                         string
		candidate                    *Account
		claimErr, finishErr, wantErr error
		wantFinishes                 int
	}{
		{name: "no candidate"},
		{name: "claim failed", claimErr: claimErr, wantErr: claimErr},
		{name: "invalid persisted state", candidate: newTTFTProbeAccount(), wantFinishes: 1},
		{name: "finish failed", candidate: newTTFTProbeAccount(), finishErr: finishErr, wantErr: finishErr, wantFinishes: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			finishes := 0
			repo := &ttftGuardAccountRepoStub{
				claimFunc: func(context.Context, time.Time, string, time.Time) (*Account, error) {
					return tt.candidate, tt.claimErr
				},
				finishFunc: func(_ context.Context, _ int64, _ string, recovered bool, _ time.Time) (bool, error) {
					finishes++
					require.False(t, recovered)
					return false, tt.finishErr
				},
			}
			svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 15)}
			err := svc.RunOpenAIOAuthTTFTRecovery(context.Background())
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantFinishes, finishes)
		})
	}
}

func TestOpenAIOAuthTTFTGuardShutdownCancelsProbeAndReleasesLease(t *testing.T) {
	started, finished := make(chan struct{}), make(chan struct{})
	account := ttftGuardPausedAccount()
	repo := &ttftGuardAccountRepoStub{
		claimFunc: func(context.Context, time.Time, string, time.Time) (*Account, error) { return account, nil },
		finishFunc: func(ctx context.Context, _ int64, _ string, recovered bool, _ time.Time) (bool, error) {
			require.NoError(t, ctx.Err(), "cleanup must remain writable after worker cancellation")
			require.False(t, recovered)
			close(finished)
			return true, nil
		},
	}
	svc := &OpenAIGatewayService{accountRepo: repo, settingService: newEnabledTTFTGuardSettingService(t, 300), cfg: &config.Config{}, openAIOAuthTTFTGuardStop: make(chan struct{}), httpUpstream: ttftProbeUpstreamFunc(func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	svc.openAIOAuthTTFTGuardWG.Add(1)
	go func() { defer svc.openAIOAuthTTFTGuardWG.Done(); svc.runOpenAIOAuthTTFTRecoveryOnce() }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start")
	}
	stopped := make(chan struct{})
	go func() { svc.stopOpenAIOAuthTTFTGuardWorker(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown waited for the full 310-second probe timeout")
	}
	select {
	case <-finished:
	default:
		t.Fatal("canceled probe lease was not released")
	}
}
