package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func codexTurnStateRenewalTestConfig() OpenAICodexTurnStateConfig {
	return OpenAICodexTurnStateConfig{
		Models:          "gpt-5*",
		ModelScopeValid: true,
		AutoEnabled:     true,
	}
}

func codexTurnStateRenewalTestAccount(slot map[string]any) *Account {
	return &Account{
		ID:          10,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"access_token": "access-secret", "chatgpt_account_id": "account-test"},
		Extra:       map[string]any{codexTurnStateModelExtraKey("gpt-5"): slot},
	}
}

func TestCodexTurnStateExpiredRenewalPlansIncludeHardExpiredSlot(t *testing.T) {
	now := time.Now()
	collectedAt := now.Add(-codexTurnStateTTL - time.Minute)
	account := codexTurnStateRenewalTestAccount(verifiedTurnStateSlot(
		"gpt-5",
		testGlobalTurnStateToken(collectedAt, 10),
		collectedAt.UnixMilli(),
	))

	require.Equal(t, []codexTurnStateRenewalPlan{{
		requestModel: "gpt-5",
		owner:        "gpt-5",
	}}, codexTurnStateExpiredRenewalPlans(account, codexTurnStateRenewalTestConfig(), now))
}

func TestCodexTurnStateExpiredRenewalPlansIncludeFailedSlots(t *testing.T) {
	now := time.Now()
	failedAt := now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli()

	for _, tc := range []struct {
		name string
		slot map[string]any
	}{
		{
			name: "without token",
			slot: map[string]any{
				CodexTurnStateAutoProbeAtExtraKey:   failedAt,
				CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
			},
		},
		{
			name: "with still-valid previous token",
			slot: func() map[string]any {
				collectedAt := now.Add(-time.Minute)
				slot := verifiedTurnStateSlot("gpt-5", testGlobalTurnStateToken(collectedAt, 10), collectedAt.UnixMilli())
				slot[CodexTurnStateAutoProbeAtExtraKey] = failedAt
				slot[CodexTurnStateAutoLastErrorExtraKey] = "transport_failed"
				return slot
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := codexTurnStateRenewalTestAccount(tc.slot)
			require.Equal(t, []codexTurnStateRenewalPlan{{
				requestModel: "gpt-5",
				owner:        "gpt-5",
			}}, codexTurnStateExpiredRenewalPlans(account, codexTurnStateRenewalTestConfig(), now))
		})
	}
}

func TestCodexTurnStateFailedRenewalRestoresExactRequestModel(t *testing.T) {
	now := time.Now()
	const (
		owner = "gpt-6-astra"
		alias = "gpt-6-astra-2026-09-19"
	)
	cfg := OpenAICodexTurnStateConfig{Models: alias, ModelScopeValid: true, AutoEnabled: true}
	for _, tc := range []struct {
		name       string
		probeModel string
	}{
		{name: "persisted probe model", probeModel: alias},
		{name: "legacy exact scope fallback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			slot := map[string]any{
				CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
				CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
			}
			if tc.probeModel != "" {
				slot[CodexTurnStateAutoProbeModelExtraKey] = tc.probeModel
			}
			account := codexTurnStateRenewalTestAccount(slot)
			account.Extra = map[string]any{codexTurnStateModelExtraKey(owner): slot}

			require.Equal(t, []codexTurnStateRenewalPlan{{requestModel: alias, owner: owner}},
				codexTurnStateExpiredRenewalPlans(account, cfg, now))
		})
	}
}

func TestCodexTurnStateFailedRenewalRestoresLegacyWildcardModelFromAccountMapping(t *testing.T) {
	now := time.Now()
	const (
		owner = "gpt-6-astra"
		model = "gpt-6-astra-2026-09-19"
	)
	account := codexTurnStateRenewalTestAccount(map[string]any{})
	account.Extra = map[string]any{codexTurnStateModelExtraKey(owner): map[string]any{
		CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
		CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
	}}
	account.Credentials["model_mapping"] = map[string]any{
		"public-astra": model,
		"wrong-owner":  "gpt-5.6-sol",
	}
	cfg := OpenAICodexTurnStateConfig{Models: "gpt-6-astra-*", ModelScopeValid: true, AutoEnabled: true}

	require.Equal(t, []codexTurnStateRenewalPlan{{requestModel: model, owner: owner}},
		codexTurnStateExpiredRenewalPlans(account, cfg, now))
}

func TestCodexTurnStateFailedRenewalResolvesLegacyWildcardModelFromCatalog(t *testing.T) {
	now := time.Now()
	const catalogModel = "gpt-6-astra-2026-09-19"
	account := newCodexModelsAPIKeyTestAccount("https://models.example/v1")
	account.Extra = map[string]any{
		codexTurnStateModelExtraKey("gpt-5"): map[string]any{
			CodexTurnStateAutoProbeAtExtraKey:    now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
			CodexTurnStateAutoLastErrorExtraKey:  "transport_failed",
			CodexTurnStateAutoProbeModelExtraKey: "gpt-5",
		},
		codexTurnStateModelExtraKey("gpt-6-astra"): map[string]any{
			CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
			CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
		},
	}
	cfg := OpenAICodexTurnStateConfig{Models: "gpt-5,gpt-6-astra-*", ModelScopeValid: true, AutoEnabled: true}
	var calls atomic.Int32
	s := newCodexModelsAPIKeyTestService(&turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return ordinaryModelsUpstreamResponse(`{"object":"list","data":[{"id":"gpt-5.6-sol"},{"id":"gpt-6-astra"},{"id":"gpt-6-astra-2026-09-19"}]}`), nil
	}})

	plans, catalogFailed := s.resolveCodexTurnStateExpiredRenewalPlans(context.Background(), account, cfg, now)
	require.False(t, catalogFailed)
	require.EqualValues(t, 1, calls.Load())
	require.ElementsMatch(t, []codexTurnStateRenewalPlan{
		{requestModel: "gpt-5", owner: "gpt-5"},
		{requestModel: catalogModel, owner: "gpt-6-astra"},
	}, plans)
}

func TestCodexTurnStateFailedRenewalKeepsResolvedPlansWhenCatalogFails(t *testing.T) {
	now := time.Now()
	account := newCodexModelsAPIKeyTestAccount("https://models.example/v1")
	account.Extra = map[string]any{
		codexTurnStateModelExtraKey("gpt-5"): map[string]any{
			CodexTurnStateAutoProbeAtExtraKey:    now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
			CodexTurnStateAutoLastErrorExtraKey:  "transport_failed",
			CodexTurnStateAutoProbeModelExtraKey: "gpt-5",
		},
		codexTurnStateModelExtraKey("gpt-6-astra"): map[string]any{
			CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
			CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
		},
	}
	cfg := OpenAICodexTurnStateConfig{Models: "gpt-5,gpt-6-astra-*", ModelScopeValid: true, AutoEnabled: true}
	var calls atomic.Int32
	s := newCodexModelsAPIKeyTestService(&turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		response := ordinaryModelsUpstreamResponse(`{"error":"unavailable"}`)
		response.StatusCode = http.StatusServiceUnavailable
		return response, nil
	}})

	plans, catalogFailed := s.resolveCodexTurnStateExpiredRenewalPlans(context.Background(), account, cfg, now)
	require.True(t, catalogFailed)
	require.EqualValues(t, 1, calls.Load())
	require.Equal(t, []codexTurnStateRenewalPlan{{requestModel: "gpt-5", owner: "gpt-5"}}, plans)
}

func TestCodexTurnStateRenewalSkipsCatalogWhenEveryDueOwnerIsResolved(t *testing.T) {
	now := time.Now()
	account := newCodexModelsAPIKeyTestAccount("https://models.example/v1")
	account.Extra = map[string]any{codexTurnStateModelExtraKey("gpt-5"): map[string]any{
		CodexTurnStateAutoProbeAtExtraKey:    now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
		CodexTurnStateAutoLastErrorExtraKey:  "transport_failed",
		CodexTurnStateAutoProbeModelExtraKey: "gpt-5",
	}}
	cfg := OpenAICodexTurnStateConfig{Models: "gpt-5,gpt-6-astra-*", ModelScopeValid: true, AutoEnabled: true}
	var calls atomic.Int32
	s := newCodexModelsAPIKeyTestService(&turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return ordinaryModelsUpstreamResponse(`{"object":"list","data":[]}`), nil
	}})

	plans, catalogFailed := s.resolveCodexTurnStateExpiredRenewalPlans(context.Background(), account, cfg, now)
	require.False(t, catalogFailed)
	require.Zero(t, calls.Load())
	require.Equal(t, []codexTurnStateRenewalPlan{{requestModel: "gpt-5", owner: "gpt-5"}}, plans)
}

func TestCodexTurnStateRenewalRetriesFailedSlotsAfterBackoff(t *testing.T) {
	now := time.Now()
	failedAt := now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli()

	for _, tc := range []struct {
		name string
		slot map[string]any
	}{
		{
			name: "without token",
			slot: map[string]any{
				CodexTurnStateAutoProbeAtExtraKey:   failedAt,
				CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
			},
		},
		{
			name: "with still-valid previous token",
			slot: func() map[string]any {
				collectedAt := now.Add(-time.Minute)
				slot := verifiedTurnStateSlot("gpt-5", testGlobalTurnStateToken(collectedAt, 10), collectedAt.UnixMilli())
				slot[CodexTurnStateAutoProbeAtExtraKey] = failedAt
				slot[CodexTurnStateAutoLastErrorExtraKey] = "transport_failed"
				return slot
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, _ := newTurnStateAutoService(t)
			account := codexTurnStateRenewalTestAccount(tc.slot)
			repo.mu.Lock()
			repo.accounts[account.ID] = account
			repo.mu.Unlock()

			var calls atomic.Int32
			renewed := testGlobalTurnStateToken(time.Now(), 10)
			s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls.Add(1)
				return turnStateResponse(renewed), nil
			}}

			plans := codexTurnStateExpiredRenewalPlans(account, codexTurnStateRenewalTestConfig(), now)
			require.Len(t, plans, 1)
			scheduled, saturated := s.scheduleExpiredCodexTurnStateRenewal(account, codexTurnStateRenewalTestConfig(), plans[0], now)
			require.True(t, scheduled)
			require.False(t, saturated)
			waitTurnStateAutoIdle(t, s)

			require.EqualValues(t, 1, calls.Load())
			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, renewed, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
			require.Empty(t, codexTurnStateModelAccount(stored, "gpt-5").GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
		})
	}
}

func TestCodexTurnStateRenewalDefersFailedSlotsDuringBackoffOrProbeBoundary(t *testing.T) {
	now := time.Now()

	for _, tc := range []struct {
		name string
		slot map[string]any
	}{
		{
			name: "five minute failure backoff active",
			slot: map[string]any{
				CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval + time.Second).UnixMilli(),
				CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
			},
		},
		{
			name: "persisted probe not before in future",
			slot: map[string]any{
				CodexTurnStateAutoProbeAtExtraKey:        now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
				CodexTurnStateAutoProbeNotBeforeExtraKey: now.Add(time.Minute).UnixMilli(),
				CodexTurnStateAutoLastErrorExtraKey:      "http_429_retry_after",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, _ := newTurnStateAutoService(t)
			account := codexTurnStateRenewalTestAccount(tc.slot)
			repo.mu.Lock()
			repo.accounts[account.ID] = account
			repo.mu.Unlock()

			var calls atomic.Int32
			s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls.Add(1)
				return turnStateResponse(testGlobalTurnStateToken(time.Now(), 10)), nil
			}}

			scheduled, saturated := s.scheduleExpiredCodexTurnStateRenewal(account, codexTurnStateRenewalTestConfig(), codexTurnStateRenewalPlan{
				requestModel: "gpt-5",
				owner:        "gpt-5",
			}, now)
			require.False(t, scheduled)
			require.False(t, saturated)
			waitTurnStateAutoIdle(t, s)
			require.Zero(t, calls.Load())
		})
	}
}

func TestCodexTurnStateExpiredRenewalPlansSkipHealthyUnexpiredSlot(t *testing.T) {
	now := time.Now()
	collectedAt := now.Add(-time.Minute)
	account := codexTurnStateRenewalTestAccount(verifiedTurnStateSlot(
		"gpt-5",
		testGlobalTurnStateToken(collectedAt, 10),
		collectedAt.UnixMilli(),
	))

	require.Empty(t, codexTurnStateExpiredRenewalPlans(account, codexTurnStateRenewalTestConfig(), now))
}

func TestCodexTurnStateExpiredRenewalPlansSkipOutOfScopeSlot(t *testing.T) {
	now := time.Now()
	failedAt := now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli()
	account := codexTurnStateRenewalTestAccount(map[string]any{
		CodexTurnStateAutoProbeAtExtraKey:   failedAt,
		CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
	})
	account.Extra[codexTurnStateModelExtraKey("gpt-4")] = map[string]any{
		CodexTurnStateAutoProbeAtExtraKey:   failedAt,
		CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
	}

	require.Equal(t, []codexTurnStateRenewalPlan{{requestModel: "gpt-5", owner: "gpt-5"}},
		codexTurnStateExpiredRenewalPlans(account, codexTurnStateRenewalTestConfig(), now))
}

func TestCodexTurnStateFailedRenewalUsesDurableBurstBudgetWithValidOldState(t *testing.T) {
	now := time.Now()
	entry := &codexTurnStateAutoEntry{
		model:         "gpt-5",
		token:         testGlobalTurnStateToken(now.Add(-time.Minute), 10),
		setAt:         now.Add(-time.Minute).UnixMilli(),
		verifiedAt:    now.Add(-time.Minute).UnixMilli(),
		verifiedModel: "gpt-5",
		probeAt:       now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
		lastError:     "transport_failed",
	}

	require.True(t, codexTurnStateProbeNeedsBurst(entry, now))
}

func TestOpenAICodexTurnStateRenewalStartStopIsIdempotent(t *testing.T) {
	s, _, _ := newTurnStateAutoService(t)
	s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		return turnStateResponse("unused"), nil
	}}

	s.StartOpenAICodexTurnStateRenewal()
	firstDone := s.openaiTurnStateRenewalDone
	require.NotNil(t, firstDone)
	s.StartOpenAICodexTurnStateRenewal()
	require.Equal(t, firstDone, s.openaiTurnStateRenewalDone)

	s.StopOpenAICodexTurnStateRenewal()
	require.Nil(t, s.openaiTurnStateRenewalStop)
	require.Nil(t, s.openaiTurnStateRenewalDone)
	s.StopOpenAICodexTurnStateRenewal()
}

func TestCodexTurnStateRenewalSchedulerUsesIndependentFairCapacity(t *testing.T) {
	s, repo, _ := newTurnStateAutoService(t)
	now := time.Now()
	cfg := codexTurnStateRenewalTestConfig()
	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		<-release
		return turnStateResponse(testGlobalTurnStateToken(time.Now(), 10)), nil
	}}

	accounts := make([]*Account, 0, codexTurnStateRenewalMaxWorkers+1)
	repo.mu.Lock()
	for index := 0; index < codexTurnStateRenewalMaxWorkers+1; index++ {
		slot := map[string]any{
			CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
			CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
		}
		account := codexTurnStateRenewalTestAccount(slot)
		account.ID = int64(index + 10)
		repo.accounts[account.ID] = account
		accounts = append(accounts, account)
	}
	repo.mu.Unlock()

	for index := 0; index < codexTurnStateRenewalMaxWorkers; index++ {
		plan := codexTurnStateExpiredRenewalPlans(accounts[index], cfg, now)
		require.Len(t, plan, 1)
		scheduled, saturated := s.scheduleExpiredCodexTurnStateRenewal(accounts[index], cfg, plan[0], now)
		require.True(t, scheduled)
		require.False(t, saturated)
	}
	lastPlan := codexTurnStateExpiredRenewalPlans(accounts[len(accounts)-1], cfg, now)
	scheduled, saturated := s.scheduleExpiredCodexTurnStateRenewal(accounts[len(accounts)-1], cfg, lastPlan[0], now)
	require.False(t, scheduled)
	require.True(t, saturated)

	close(release)
	released = true
	waitTurnStateAutoIdle(t, s)
	s.openaiTurnStateMu.Lock()
	require.Zero(t, s.openaiTurnStateRenewalWorkers)
	require.Empty(t, s.openaiTurnStateRenewalAccounts)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateRenewalSchedulesAtMostOneModelPerAccount(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	now := time.Now()
	failed := func() map[string]any {
		return map[string]any{
			CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
			CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
		}
	}
	account.Extra = map[string]any{
		codexTurnStateModelExtraKey("gpt-5"):   failed(),
		codexTurnStateModelExtraKey("gpt-5.5"): failed(),
	}
	repo.mu.Lock()
	repo.accounts[account.ID] = account
	repo.mu.Unlock()

	release := make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		<-release
		return turnStateResponse(testGlobalTurnStateToken(time.Now(), 10)), nil
	}}
	cfg := OpenAICodexTurnStateConfig{Models: "gpt-5,gpt-5.5", ModelScopeValid: true, AutoEnabled: true}
	plans := codexTurnStateExpiredRenewalPlans(account, cfg, now)
	require.Len(t, plans, 2)
	scheduled, saturated := s.scheduleExpiredCodexTurnStateRenewal(account, cfg, plans[0], now)
	require.True(t, scheduled)
	require.False(t, saturated)
	scheduled, saturated = s.scheduleExpiredCodexTurnStateRenewal(account, cfg, plans[1], now)
	require.False(t, scheduled)
	require.False(t, saturated)

	close(release)
	released = true
	waitTurnStateAutoIdle(t, s)
}

func TestCodexTurnStateRenewalWorkerHonorsScannerCancellation(t *testing.T) {
	s, repo, _ := newTurnStateAutoService(t)
	now := time.Now()
	account := codexTurnStateRenewalTestAccount(map[string]any{
		CodexTurnStateAutoProbeAtExtraKey:   now.Add(-codexTurnStateAutoProbeInterval - time.Second).UnixMilli(),
		CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
	})
	repo.mu.Lock()
	repo.accounts[account.ID] = account
	repo.mu.Unlock()

	started := make(chan struct{}, 1)
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	ctx, cancel := context.WithCancel(context.Background())
	plan := codexTurnStateExpiredRenewalPlans(account, codexTurnStateRenewalTestConfig(), now)
	require.Len(t, plan, 1)
	scheduled, saturated := s.scheduleExpiredCodexTurnStateRenewalWithContext(ctx, account, codexTurnStateRenewalTestConfig(), plan[0], now)
	require.True(t, scheduled)
	require.False(t, saturated)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("renewal probe did not start")
	}
	cancel()
	waitTurnStateAutoIdle(t, s)
	s.openaiTurnStateMu.Lock()
	require.Zero(t, s.openaiTurnStateRenewalWorkers)
	s.openaiTurnStateMu.Unlock()
}
