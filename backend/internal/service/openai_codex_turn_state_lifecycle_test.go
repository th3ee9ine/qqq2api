package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type turnStateRestoreFailureRepo struct {
	*turnStateAutoRepo
	service       *OpenAIGatewayService
	model         string
	injected      atomic.Bool
	restoreFailed atomic.Bool
}

type turnStateManualOutcomeRetryRepo struct {
	*codexTurnStateCASWinnerRepo
	attempts atomic.Int32
}

func (r *turnStateManualOutcomeRetryRepo) UpdateCodexTurnState(context.Context, int64, string, map[string]any) (bool, error) {
	switch r.attempts.Add(1) {
	case 1, 2, 3:
		return false, errCodexTurnStateProbeBurstPersistence
	case 4:
		return false, nil
	default:
		return true, nil
	}
}

func (r *turnStateRestoreFailureRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	account, err := r.turnStateAutoRepo.GetByID(ctx, id)
	if err != nil || account == nil {
		return account, err
	}
	budget, parseErr := codexTurnStateProbeBurstBudgetFromAccount(account, codexTurnStateProbeBurstBudgetExtraKey(r.model))
	if parseErr == nil && budget.CandidatePendingUntilMS > time.Now().UnixMilli() && r.injected.CompareAndSwap(false, true) {
		r.service.openaiTurnStateMu.Lock()
		entry := r.service.openaiTurnStates[codexTurnStateKey{account.ID, r.model}]
		entry.candidate = codexTurnStateUsageCandidate{
			state:                 testGlobalTurnStateToken(time.Now(), 10),
			verifiedModel:         r.model,
			collectedAt:           time.Now(),
			recoveryGeneration:    entry.recovery.InvalidatedAtMS,
			scopeModels:           []string{r.model},
			expectedResponseModel: r.model,
		}
		r.service.openaiTurnStateMu.Unlock()
	}
	return account, nil
}

func (r *turnStateRestoreFailureRepo) GetCodexTurnStateSource(ctx context.Context, id int64) (*Account, error) {
	if r.injected.Load() && r.restoreFailed.CompareAndSwap(false, true) {
		return nil, errCodexTurnStateProbeBurstPersistence
	}
	return r.turnStateAutoRepo.GetByID(ctx, id)
}

func TestPersistCodexTurnStateManualOutcomeRetainsProvenanceAfterRetryExhaustion(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	winner := *account
	winner.Extra = map[string]any{}
	sourceStart := make(chan struct{})
	sourceResume := make(chan struct{})
	repo := &turnStateManualOutcomeRetryRepo{codexTurnStateCASWinnerRepo: &codexTurnStateCASWinnerRepo{
		turnStateAutoRepo: baseRepo,
		winner:            &winner,
		sourceStart:       sourceStart,
		sourceResume:      sourceResume,
	}}
	s.accountRepo = repo
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	var upstreamCalls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		upstreamCalls.Add(1)
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(testGlobalTurnStateToken(time.Now(), 10), "gpt-5"), nil
		}
		return turnStateModelResponse("", "gpt-5"), nil
	}}
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, time.Now(), "gpt-5")
	entry.lastError = "request_failed"
	s.openaiTurnStateMu.Unlock()

	require.False(t, s.persistCodexTurnStateProbeOutcome(account.ID, entry, true))
	s.openaiTurnStateMu.Lock()
	require.True(t, entry.dirty)
	require.True(t, entry.manualOutcomePending)
	require.False(t, entry.retryAfter.IsZero())
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
	entry.running = true
	s.openaiTurnStateWorkers = 1
	s.openaiTurnStateMu.Unlock()

	done := make(chan struct{})
	go func() {
		s.runCodexTurnStateWorker(account.ID, entry)
		close(done)
	}()
	select {
	case <-sourceStart:
	case <-time.After(time.Second):
		t.Fatal("dirty retry did not reach CAS winner reconciliation")
	}
	s.openaiTurnStateMu.Lock()
	require.True(t, entry.reconciling)
	require.True(t, entry.probe)
	require.True(t, entry.forceProbe)
	require.True(t, entry.manualProbe, "dirty retry must pass the retained manual provenance into CAS reconciliation")
	s.openaiTurnStateMu.Unlock()
	close(sourceResume)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("manual retry worker did not finish")
	}
	require.EqualValues(t, 2, upstreamCalls.Load(), "manual work must still run while automatic collection is disabled")
	require.GreaterOrEqual(t, repo.attempts.Load(), int32(6))
}

func TestCodexTurnStateWorkerDropsDeletedAccountWithoutRetry(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	repo := &codexTurnStateCASWinnerRepo{turnStateAutoRepo: baseRepo}
	s.accountRepo = repo
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, time.Now(), "gpt-5")
	entry.dirty = true
	entry.probe = true
	entry.forceProbe = true
	entry.manualProbe = true
	entry.manualOutcomePending = true
	entry.retryWakeAt = time.Now().Add(time.Hour)
	entry.probeWakeAt = time.Now().Add(time.Hour)
	entry.running = true
	s.openaiTurnStateWorkers = 1
	s.openaiTurnStateMu.Unlock()

	s.runCodexTurnStateWorker(account.ID, entry)

	s.openaiTurnStateMu.Lock()
	require.False(t, entry.running)
	require.False(t, entry.dirty)
	require.False(t, entry.probe)
	require.False(t, entry.forceProbe)
	require.False(t, entry.manualProbe)
	require.False(t, entry.manualOutcomePending)
	require.True(t, entry.retryAfter.IsZero())
	require.True(t, entry.retryWakeAt.IsZero())
	require.True(t, entry.probeRetryAfter.IsZero())
	require.True(t, entry.probeWakeAt.IsZero())
	require.Zero(t, s.openaiTurnStateWorkers)
	s.openaiTurnStateMu.Unlock()
	require.EqualValues(t, 1, repo.updateCalls.Load(), "the account disappears after the zero-row CAS")
	require.EqualValues(t, 1, repo.sourceCalls.Load())
}

func TestCodexTurnStateRestoreFailureClearsOwnedPendingMarker(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	const model = "gpt-5"
	repo := &turnStateRestoreFailureRepo{turnStateAutoRepo: baseRepo, service: s, model: model}
	s.accountRepo = repo
	candidate := testGlobalTurnStateToken(time.Now().Add(-time.Minute), 10)
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(candidate, model), nil
		}
		return turnStateModelResponse("", model), nil
	}}

	require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, model))
	waitTurnStateAutoIdle(t, s)
	require.True(t, repo.injected.Load(), "the competing candidate must reject the staged result after pending ownership is acquired")
	require.True(t, repo.restoreFailed.Load(), "the pending-to-live restore must reach the injected failure")
	require.Eventually(t, func() bool {
		stored, err := baseRepo.GetByID(context.Background(), account.ID)
		if err != nil {
			return false
		}
		budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
		return err == nil && budget.CandidatePendingUntilMS == 0
	}, time.Second, time.Millisecond, "restore failure must retain ownership until deferred exact cleanup")
}

func TestManualCodexTurnStateCollectionExpandsWildcardAndRunsEveryOwnerAcrossPool(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-5*"
	settings.values[SettingKeyOpenAICodexTurnStateProxyURLs] = `[
		"socks5://first:secret@first.example:1080",
		"socks5://second:secret@second.example:1080"
	]`
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	var (
		mu        sync.Mutex
		attempts  = make(map[string]map[string]int)
		active    atomic.Int32
		maxActive atomic.Int32
	)
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, route string, _ int64) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/codex/models") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{"models":[
					{"slug":"gpt-5.5"},
					{"slug":"gpt-5.6-sol"},
					{"slug":"gpt-image-2"},
					{"slug":"gpt-6-astra"}
				]}`)),
			}, nil
		}
		currentActive := active.Add(1)
		defer active.Add(-1)
		for observed := maxActive.Load(); currentActive > observed && !maxActive.CompareAndSwap(observed, currentActive); observed = maxActive.Load() {
		}
		time.Sleep(5 * time.Millisecond)
		body, readErr := io.ReadAll(req.Body)
		require.NoError(t, readErr)
		var payload struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		mu.Lock()
		if attempts[payload.Model] == nil {
			attempts[payload.Model] = make(map[string]int)
		}
		attempts[payload.Model][route]++
		mu.Unlock()
		if strings.Contains(route, "first.example") {
			response := turnStateModelResponse("", payload.Model)
			response.StatusCode = http.StatusBadGateway
			return response, nil
		}
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse("state-"+payload.Model, payload.Model), nil
		}
		return turnStateModelResponse("", payload.Model), nil
	}}

	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, "ignored-model")
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	require.Equal(t, []string{"gpt-5.5", "gpt-5.6-sol"}, result.TargetModels)
	require.Equal(t, []CodexTurnStateManualModelTarget{
		{Model: "gpt-5.5", Owner: "gpt-5.5"},
		{Model: "gpt-5.6-sol", Owner: "gpt-5.6-sol"},
	}, result.ModelTargets)
	require.Equal(t, []string{"gpt-5.5", "gpt-5.6-sol"}, result.QueuedModels)
	waitTurnStateAutoIdle(t, s)

	mu.Lock()
	for _, model := range result.QueuedModels {
		require.Equal(t, 1, attempts[model]["socks5://first:secret@first.example:1080"], model)
		require.Equal(t, 2, attempts[model]["socks5://second:secret@second.example:1080"], model)
	}
	mu.Unlock()
	require.EqualValues(t, 1, maxActive.Load(), "manual owners for one account must access the upstream serially")
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	info := CodexTurnStateAutoInfoForAccount(stored, time.Now())
	require.ElementsMatch(t, []string{"gpt-5.5", "gpt-5.6-sol"}, info.SuccessfulModels)
}

func TestManualCodexTurnStateCollectionRejectsWholeBatchWhenAnyOwnerIsBusy(t *testing.T) {
	for _, busyState := range []string{"running", "probe", "reconciling"} {
		t.Run(busyState, func(t *testing.T) {
			s, _, account := newTurnStateAutoService(t)
			settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
			settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-5.5,gpt-5.6-sol"
			s.settingService.InvalidateOpenAICodexTurnStateCache()
			var calls atomic.Int32
			s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls.Add(1)
				return turnStateModelResponse("must-not-run", "gpt-5.5"), nil
			}}

			s.openaiTurnStateMu.Lock()
			first := s.codexTurnStateEntryLocked(account, time.Now(), "gpt-5.5")
			busy := s.codexTurnStateEntryLocked(account, time.Now(), "gpt-5.6-sol")
			switch busyState {
			case "running":
				busy.running = true
			case "probe":
				busy.probe = true
			case "reconciling":
				busy.reconciling = true
			}
			s.openaiTurnStateMu.Unlock()

			result, err := s.RequestCodexTurnStateCollection(context.Background(), account, "ignored")
			require.NoError(t, err)
			require.Equal(t, CodexTurnStateManualStatusRejected, result.Status)
			require.Equal(t, "collection_already_in_flight", result.Reason)
			require.Empty(t, result.QueuedModels)
			require.Zero(t, calls.Load())

			s.openaiTurnStateMu.Lock()
			require.False(t, first.probe, "an earlier idle owner must not be partially queued")
			require.False(t, first.manualProbe, "an earlier idle owner must not inherit manual intent")
			busy.running, busy.probe, busy.reconciling = false, false, false
			s.openaiTurnStateMu.Unlock()
		})
	}
}

func TestManualCodexTurnStateCollectionWildcardCatalogFailureIsFailClosed(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-5*"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":{}}`))}, nil
	}}
	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, "gpt-5.5")
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusRejected, result.Status)
	require.Equal(t, "model_catalog_unavailable", result.Reason)
	require.Empty(t, result.TargetModels)
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 1, calls.Load())
}

func TestManualCodexTurnStateTargetsExpandSetupTokenCatalog(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	account.Type = AccountTypeSetupToken
	repo.mu.Lock()
	repo.accounts[account.ID].Type = AccountTypeSetupToken
	repo.mu.Unlock()
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		require.Contains(t, req.URL.Path, "/codex/models")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"models":[{"slug":"gpt-5.6-sol"},{"slug":"gpt-6-astra"}]}`)),
		}, nil
	}}
	targets, err := s.resolveCodexTurnStateManualTargets(context.Background(), account, OpenAICodexTurnStateConfig{
		Models:          "gpt-5*",
		ModelScopeValid: true,
	})
	require.NoError(t, err)
	require.Equal(t, []CodexTurnStateManualModelTarget{{Model: "gpt-5.6-sol", Owner: "gpt-5.6-sol"}}, targets)
}

func TestCodexTurnStateCollectionRequiresSchedulableAccount(t *testing.T) {
	future := time.Now().Add(time.Hour)
	past := time.Now().Add(-time.Hour)
	tests := []struct {
		name   string
		mutate func(*Account)
	}{
		{name: "disabled", mutate: func(account *Account) { account.Status = StatusDisabled }},
		{name: "error", mutate: func(account *Account) { account.Status = StatusError }},
		{name: "manual scheduling disabled", mutate: func(account *Account) { account.Schedulable = false }},
		{name: "rate limited", mutate: func(account *Account) { account.RateLimitResetAt = &future }},
		{name: "overloaded", mutate: func(account *Account) { account.OverloadUntil = &future }},
		{name: "temporarily paused", mutate: func(account *Account) {
			account.TempUnschedulableUntil = &future
			account.TempUnschedulableReason = "transport_error"
		}},
		{name: "automatically paused after expiry", mutate: func(account *Account) {
			account.AutoPauseOnExpired = true
			account.ExpiresAt = &past
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, requestAccount := newTurnStateAutoService(t)
			repo.mu.Lock()
			tc.mutate(repo.accounts[requestAccount.ID])
			repo.mu.Unlock()
			var calls atomic.Int32
			s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls.Add(1)
				return turnStateModelResponse("must-not-run", "gpt-5"), nil
			}}

			result, err := s.RequestCodexTurnStateCollection(context.Background(), requestAccount, "gpt-5")
			require.NoError(t, err)
			require.Equal(t, CodexTurnStateManualStatusRejected, result.Status)
			require.Equal(t, "account_not_schedulable", result.Reason)
			encodedResult, marshalErr := json.Marshal(result)
			require.NoError(t, marshalErr)
			require.Contains(t, string(encodedResult), `"codex_turn_state_auto":null`)

			// The caller snapshot is intentionally stale. The worker must enforce
			// the repository's current lifecycle state independently.
			require.Empty(t, s.autoTurnStateForAccount(context.Background(), requestAccount, "gpt-5"))
			waitTurnStateAutoIdle(t, s)
			require.Zero(t, calls.Load())

			current, loadErr := repo.GetByID(context.Background(), requestAccount.ID)
			require.NoError(t, loadErr)
			require.Nil(t, CodexTurnStateAutoInfoForAccount(current, time.Now()), "list diagnostics are exposed only for currently eligible accounts")
		})
	}
}

func TestManualCodexTurnStateCollectionRetriesValidStateWithLastError(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-5"
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = model
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	old := testGlobalTurnStateToken(time.Now().Add(-time.Minute), 10)
	seedAutomaticTurnState(account, old, model)
	slot := codexTurnStateModelAccount(account, model)
	slot.Extra[CodexTurnStateAutoLastErrorExtraKey] = "transport_failed"
	repo.mu.Lock()
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	repo.mu.Unlock()

	const candidate = "manual-retry-candidate"
	var calls atomic.Int32
	firstRequestStarted := make(chan struct{})
	releaseFirstRequest := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirstRequest) }) }
	t.Cleanup(release)
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(firstRequestStarted)
			<-releaseFirstRequest
		}
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(candidate, model), nil
		}
		require.Equal(t, candidate, req.Header.Get(openAICodexTurnStateHeader))
		return turnStateModelResponse("", model), nil
	}}

	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, model)
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	select {
	case <-firstRequestStarted:
	case <-time.After(time.Second):
		t.Fatal("manual retry did not reach its first route")
	}
	inProgress, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	inProgressScope := codexTurnStateModelAccount(inProgress, model)
	require.Empty(t, inProgressScope.GetExtraString(CodexTurnStateAutoLastErrorExtraKey), "a new manual round must clear the previous terminal error before its first IP")
	require.Positive(t, codexTurnStateAutoInt64(inProgressScope, CodexTurnStateAutoProbeAtExtraKey))
	release()
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 2, calls.Load())
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, candidate, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, model)))
}

func TestManualCodexTurnStateSameTokenRefreshesEvidenceWithoutResettingAge(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-5"
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = model
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	setAt := time.Now().Add(-20 * time.Minute).UnixMilli()
	state := testGlobalTurnStateToken(time.UnixMilli(setAt), 10)
	account.Extra = map[string]any{codexTurnStateModelExtraKey(model): map[string]any{
		CodexTurnStateAutoExtraKey:              state,
		CodexTurnStateAutoSetAtExtraKey:         setAt,
		CodexTurnStateAutoProbeAtExtraKey:       setAt,
		CodexTurnStateAutoVerifiedAtExtraKey:    setAt,
		CodexTurnStateAutoVerifiedModelExtraKey: model,
		CodexTurnStateAutoLastErrorExtraKey:     "transport_failed",
	}}
	repo.mu.Lock()
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	repo.mu.Unlock()

	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(state, model), nil
		}
		require.Equal(t, state, req.Header.Get(openAICodexTurnStateHeader))
		return turnStateModelResponse("", model), nil
	}}
	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, "ignored")
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	waitTurnStateAutoIdle(t, s)

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	slot := codexTurnStateModelAccount(stored, model)
	require.Equal(t, state, codexTurnStateAutoToken(slot))
	require.Equal(t, setAt, codexTurnStateAutoInt64(slot, CodexTurnStateAutoSetAtExtraKey))
	require.Greater(t, codexTurnStateAutoInt64(slot, CodexTurnStateAutoVerifiedAtExtraKey), setAt)
	require.Greater(t, codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey), setAt)
	require.Empty(t, slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	require.Contains(t, CodexTurnStateAutoInfoForAccount(stored, time.Now()).SuccessfulModels, model)
}

func TestManualCodexTurnStateScopeChangePersistsTerminalFailure(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-5.5"
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = model
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse("out-of-scope-candidate", model), nil
		}
		return turnStateModelResponse("", model), nil
	}}

	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, "ignored")
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual collection did not reach the upstream")
	}
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6-astra"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	releaseOnce.Do(func() { close(release) })
	waitTurnStateAutoIdle(t, s)

	require.EqualValues(t, 2, calls.Load(), "the in-flight collection and its same-route replay finish before publication is rejected")
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	slot := codexTurnStateModelAccount(stored, model)
	require.Greater(t, codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey), int64(0))
	require.Equal(t, "model_scope_changed", slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	require.Empty(t, codexTurnStateAutoToken(slot))
}

func TestManualCodexTurnStateWorkerPersistsScopeChangeBeforeFirstProbe(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-5.5"
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = model
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return turnStateModelResponse("must-not-run", model), nil
	}}

	// Build the exact state left by an accepted manual request, but do not let the
	// worker run until the configured scope no longer contains its model.
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, time.Now(), model)
	require.True(t, replaceCodexTurnStateProbeModelsLocked(entry, model, model))
	entry.probe = true
	entry.forceProbe = true
	entry.manualProbe = true
	entry.running = true
	s.openaiTurnStateWorkers = 1
	s.openaiTurnStateMu.Unlock()

	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6-astra"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	s.runCodexTurnStateWorker(account.ID, entry)

	require.Zero(t, calls.Load(), "a task rejected before its first iteration must not reach the upstream")
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	slot := codexTurnStateModelAccount(stored, model)
	require.Greater(t, codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey), int64(0))
	require.Equal(t, "model_scope_changed", slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))

	s.openaiTurnStateMu.Lock()
	require.False(t, entry.running)
	require.False(t, entry.probe)
	require.False(t, entry.forceProbe)
	require.False(t, entry.manualProbe)
	require.False(t, entry.manualOutcomePending)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateDiagnosticsAndManualResponseUseEffectiveInterval(t *testing.T) {
	for _, tc := range []struct {
		interval int
		due      bool
	}{{interval: 5, due: true}, {interval: 20, due: false}} {
		t.Run(fmt.Sprintf("interval_%d", tc.interval), func(t *testing.T) {
			s, repo, account := newTurnStateAutoService(t)
			settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
			settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-5"
			settings.values[SettingKeyOpenAICodexTurnStateAutoIntervalMinutes] = fmt.Sprintf("%d", tc.interval)
			s.settingService.InvalidateOpenAICodexTurnStateCache()
			setAt := time.Now().Add(-10 * time.Minute).UnixMilli()
			account.Extra = map[string]any{codexTurnStateModelExtraKey("gpt-5"): verifiedTurnStateSlot("gpt-5", "still-valid-state", setAt)}
			repo.mu.Lock()
			repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
			repo.mu.Unlock()
			s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				t.Fatal("valid state must not trigger manual upstream collection")
				return nil, nil
			}}

			info := s.CodexTurnStateAutoInfoForAccount(context.Background(), account, time.Now())
			require.NotNil(t, info)
			require.Equal(t, tc.due, info.Due)
			result, err := s.RequestCodexTurnStateCollection(context.Background(), account, "ignored")
			require.NoError(t, err)
			require.Equal(t, CodexTurnStateManualStatusAlreadyValid, result.Status)
			require.NotNil(t, result.CodexTurnStateAuto)
			require.Equal(t, tc.due, result.CodexTurnStateAuto.Due)
		})
	}
}

func TestCodexTurnStateManualWorkerIgnoresAutomaticRetryBoundary(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, "gpt-5")
	entry.probe = true
	entry.forceProbe = true
	entry.manualProbe = true
	entry.probeRetryAfter = now.Add(time.Hour)
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	require.True(t, entry.running, "manual work must start even when an earlier automatic round installed a retry timer")
	s.openaiTurnStateMu.Unlock()
	waitTurnStateAutoIdle(t, s)
}

func TestManualCodexTurnStateCollectionBypassesExistingCooldown(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const (
		model     = "gpt-5"
		candidate = "manual-cooldown-candidate"
	)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = model
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	notBefore := time.Now().Add(time.Hour).UnixMilli()
	account.Extra = map[string]any{
		CodexTurnStateAutoProbeNotBeforeExtraKey: notBefore,
		codexTurnStateModelExtraKey(model): map[string]any{
			CodexTurnStateAutoProbeNotBeforeExtraKey: notBefore,
			CodexTurnStateAutoLastErrorExtraKey:      codexTurnStateProbe429RetryAfterCode,
		},
	}
	repo.mu.Lock()
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	repo.mu.Unlock()

	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(candidate, model), nil
		}
		return turnStateModelResponse("", model), nil
	}}
	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, model)
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 2, calls.Load(), "manual collection must bypass the existing boundary")

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, codexTurnStateAutoInt64(stored, CodexTurnStateAutoProbeNotBeforeExtraKey), notBefore)

	restarted := &OpenAIGatewayService{settingService: s.settingService, accountRepo: repo, proxyRepo: s.proxyRepo, httpUpstream: s.httpUpstream}
	restarted.openaiTurnStateMu.Lock()
	entry := restarted.codexTurnStateEntryLocked(stored, time.Now(), model)
	entry.forceProbe = true
	restarted.openaiTurnStateMu.Unlock()
	restarted.runCodexTurnStateProbe(account.ID, entry)
	require.EqualValues(t, 2, calls.Load(), "the retained boundary must still block a later automatic round")
}

func TestCodexTurnStateRoundContinuesAfter429AndStopsOnSuccess(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const (
		model       = "gpt-6-astra"
		actualModel = "openai/gpt-6-astra-2026-09-19"
		candidate   = "candidate-after-rate-limit"
	)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = model
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	s.proxyRepo = &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
	var (
		mu                   sync.Mutex
		routes               []string
		calls                int
		secondAttemptStarted = make(chan struct{})
		releaseSecondAttempt = make(chan struct{})
		releaseSecondOnce    sync.Once
	)
	releaseSecond := func() { releaseSecondOnce.Do(func() { close(releaseSecondAttempt) }) }
	t.Cleanup(releaseSecond)
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, route string, _ int64) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		routes = append(routes, route)
		if calls == 1 {
			resp := turnStateModelResponse("", actualModel)
			resp.StatusCode = http.StatusTooManyRequests
			resp.Header.Set("Retry-After", "3600")
			return resp, nil
		}
		if calls == 2 {
			close(secondAttemptStarted)
			<-releaseSecondAttempt
		}
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(candidate, actualModel), nil
		}
		return turnStateModelResponse("", actualModel), nil
	}}

	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, model)
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	select {
	case <-secondAttemptStarted:
	case <-time.After(time.Second):
		t.Fatal("the round did not continue to the second IP after the 429")
	}
	intermediate, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	intermediateScope := codexTurnStateModelAccount(intermediate, model)
	require.Greater(t, codexTurnStateAutoInt64(intermediate, CodexTurnStateAutoProbeNotBeforeExtraKey), time.Now().UnixMilli(),
		"the account boundary must be durable before the next IP is attempted")
	require.Empty(t, intermediateScope.GetExtraString(CodexTurnStateAutoLastErrorExtraKey),
		"a mid-round 429 must not be exposed as the model's terminal outcome")
	require.Empty(t, codexTurnStateAutoToken(intermediateScope))
	intermediateInfo := CodexTurnStateAutoInfoForAccount(intermediate, time.Now())
	require.NotNil(t, intermediateInfo)
	require.False(t, intermediateInfo.CollectionSucceeded)
	require.Empty(t, intermediateInfo.SuccessfulModels)
	releaseSecond()
	waitTurnStateAutoIdle(t, s)
	mu.Lock()
	require.Equal(t, 3, calls)
	require.NotEqual(t, routes[0], routes[1])
	require.Equal(t, routes[1], routes[2], "collection and replay must use the same successful IP")
	mu.Unlock()

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	storedScope := codexTurnStateModelAccount(stored, model)
	require.Equal(t, candidate, codexTurnStateAutoToken(storedScope))
	require.Equal(t, actualModel, storedScope.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
	require.Empty(t, storedScope.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	require.Greater(t, codexTurnStateAutoInt64(stored, CodexTurnStateAutoProbeNotBeforeExtraKey), time.Now().UnixMilli())
	info := CodexTurnStateAutoInfoForAccount(stored, time.Now())
	require.NotNil(t, info)
	require.True(t, info.CollectionSucceeded)
	require.Contains(t, info.SuccessfulModels, actualModel)
}

func TestManualCodexTurnStateRejectsOlderStateAndContinuesSamePoolRound(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-5.5"
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = model
	settings.values[SettingKeyOpenAICodexTurnStateProxyURLs] = `[
		"socks5://first:secret@first.example:1080",
		"socks5://second:secret@second.example:1080"
	]`
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	now := time.Now().Truncate(time.Second)
	current := testGlobalTurnStateToken(now.Add(-time.Minute), 10)
	older := testGlobalTurnStateToken(now.Add(-2*time.Minute), 10)
	fresh := testGlobalTurnStateToken(now, 10)
	slot := verifiedTurnStateSlot(model, current, now.Add(-time.Minute).UnixMilli())
	slot[CodexTurnStateAutoLastErrorExtraKey] = "transport_failed"
	account.Extra = map[string]any{codexTurnStateModelExtraKey(model): slot}
	repo.mu.Lock()
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	repo.mu.Unlock()

	var (
		mu       sync.Mutex
		sequence []string
	)
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, route string, _ int64) (*http.Response, error) {
		phase := "collection"
		if req.Header.Get(openAICodexTurnStateHeader) != "" {
			phase = "replay"
		}
		mu.Lock()
		sequence = append(sequence, route+"|"+phase)
		mu.Unlock()

		if strings.Contains(route, "first.example") {
			if phase == "collection" {
				return turnStateModelResponse(older, model), nil
			}
			require.Equal(t, older, req.Header.Get(openAICodexTurnStateHeader))
			return turnStateModelResponse("", model), nil
		}
		if phase == "collection" {
			return turnStateModelResponse(fresh, model), nil
		}
		require.Equal(t, fresh, req.Header.Get(openAICodexTurnStateHeader))
		return turnStateModelResponse("", model), nil
	}}

	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, "ignored")
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	waitTurnStateAutoIdle(t, s)

	mu.Lock()
	require.Equal(t, []string{
		"socks5://first:secret@first.example:1080|collection",
		"socks5://first:secret@first.example:1080|replay",
		"socks5://second:secret@second.example:1080|collection",
		"socks5://second:secret@second.example:1080|replay",
	}, sequence, "a rejected replayable state must advance within the same pool round")
	mu.Unlock()

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	storedSlot := codexTurnStateModelAccount(stored, model)
	require.Equal(t, fresh, codexTurnStateAutoToken(storedSlot))
	require.Empty(t, storedSlot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	s.openaiTurnStateMu.Lock()
	require.False(t, s.openaiTurnStates[codexTurnStateKey{account.ID, model}].probe, "success must not queue a new pool round")
	require.False(t, s.openaiTurnStates[codexTurnStateKey{account.ID, model}].manualProbe)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateAutomaticRoundCountsOnceAndVisitsEveryFailedRoute(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-5"
	const routeCount = codexTurnStateProxyURLsMaxSize + 44
	proxies := make([]Proxy, 0, routeCount)
	for index := range routeCount {
		proxies = append(proxies, Proxy{Protocol: "http", Host: fmt.Sprintf("turn-state-pool-%03d.example", index), Port: 8080})
	}
	s.proxyRepo = &turnStateProxyRepo{proxies: proxies}
	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		resp := turnStateModelResponse("", model)
		resp.StatusCode = http.StatusUnauthorized
		return resp, nil
	}}

	require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, model))
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, routeCount, calls.Load(), "the complete global pool must be visited without a three- or 256-route cap")
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, 1, budget.Attempts, "the complete IP pool is one durable collection attempt")
	require.Equal(t, "http_401", codexTurnStateModelAccount(stored, model).GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
}

func TestCodexTurnStateAutomaticStopsWhenAccountChangesBeforeReplay(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-5"
	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		repo.mu.Lock()
		repo.accounts[account.ID].Status = StatusError
		repo.mu.Unlock()
		return turnStateModelResponse("must-not-be-staged", model), nil
	}}

	require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, model))
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 1, calls.Load(), "the replay must re-read and reject the changed account")
	s.openaiTurnStateMu.Lock()
	entry := s.openaiTurnStates[codexTurnStateKey{account.ID, model}]
	require.NotNil(t, entry)
	require.Empty(t, entry.candidate.state)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateVerificationFailureDoesNotScheduleUnschedulableAccount(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	account.Schedulable = false
	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return turnStateModelResponse("must-not-run", "gpt-5"), nil
	}}

	s.noteCodexTurnStateVerificationError(context.Background(), account, "gpt-5", errCodexTurnStateResponseModelMismatch)
	waitTurnStateAutoIdle(t, s)
	require.Zero(t, calls.Load())
	s.openaiTurnStateMu.Lock()
	require.Empty(t, s.openaiTurnStates)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateDiagnosticsReportSuccessfulModelsWithoutSecrets(t *testing.T) {
	now := time.Now()
	account := &Account{
		ID: 77, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Extra: map[string]any{
			codexTurnStateModelExtraKey("gpt-6-astra"): verifiedTurnStateSlot("openai/gpt-6-astra-2026-09-19", testGlobalTurnStateToken(now, 10), now.UnixMilli()),
			codexTurnStateModelExtraKey("codex-auto-review"): map[string]any{
				CodexTurnStateAutoLastErrorExtraKey: "transport_failed",
			},
		},
	}

	info := CodexTurnStateAutoInfoForAccount(account, now)
	require.NotNil(t, info)
	require.True(t, info.CollectionSucceeded)
	require.Equal(t, []string{"openai/gpt-6-astra-2026-09-19"}, info.SuccessfulModels)
	require.True(t, info.Models["gpt-6-astra"].CollectionSucceeded)
	require.False(t, info.Models["codex-auto-review"].CollectionSucceeded)

	encoded, err := json.Marshal(info)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"successful_models":["openai/gpt-6-astra-2026-09-19"]`)
	require.Contains(t, string(encoded), `"collection_succeeded":true`)
	require.NotContains(t, string(encoded), codexTurnStateAutoToken(codexTurnStateModelAccount(account, "gpt-6-astra")))

	for _, tc := range []struct {
		name string
		slot map[string]any
	}{
		{
			name: "wrong owner",
			slot: verifiedTurnStateSlot("codex-auto-review", testGlobalTurnStateToken(now, 10), now.UnixMilli()),
		},
		{
			name: "expired",
			slot: verifiedTurnStateSlot("gpt-6-astra", testGlobalTurnStateToken(now.Add(-codexTurnStateTTL-time.Minute), 10), now.Add(-codexTurnStateTTL-time.Minute).UnixMilli()),
		},
		{
			name: "latest collection failed",
			slot: func() map[string]any {
				slot := verifiedTurnStateSlot("gpt-6-astra", testGlobalTurnStateToken(now, 10), now.UnixMilli())
				slot[CodexTurnStateAutoLastErrorExtraKey] = "transport_failed"
				return slot
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := &Account{
				ID: 78, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
				Extra: map[string]any{codexTurnStateModelExtraKey("gpt-6-astra"): tc.slot},
			}
			invalidInfo := CodexTurnStateAutoInfoForAccount(invalid, now)
			require.NotNil(t, invalidInfo)
			require.False(t, invalidInfo.CollectionSucceeded)
			require.Empty(t, invalidInfo.SuccessfulModels)
			require.False(t, invalidInfo.Models["gpt-6-astra"].CollectionSucceeded)
		})
	}
}
