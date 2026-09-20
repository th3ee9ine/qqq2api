package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type codexTurnStateScopeCleanupRepo struct {
	*turnStateAutoRepo
	deleted []string
}

func (r *codexTurnStateScopeCleanupRepo) DeleteCodexTurnStateScopeExtras(_ context.Context, id int64, keys []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[id]
	if account == nil {
		return ErrAccountNotFound
	}
	for _, key := range keys {
		delete(account.Extra, key)
		r.deleted = append(r.deleted, key)
	}
	return nil
}

func seedScopedTurnState(repo *turnStateAutoRepo, a *Account, model, token string) {
	seedAutomaticTurnState(a, token, model)
	repo.mu.Lock()
	repo.accounts[a.ID].Extra = mergeMap(nil, a.Extra)
	repo.mu.Unlock()
}

func verifiedTurnStateSlot(model, token string, timestamp int64) map[string]any {
	return map[string]any{
		CodexTurnStateAutoExtraKey:              token,
		CodexTurnStateAutoSetAtExtraKey:         timestamp,
		CodexTurnStateAutoVerifiedAtExtraKey:    timestamp,
		CodexTurnStateAutoVerifiedModelExtraKey: model,
	}
}

func TestCodexTurnStateModelFamilyCanonicalSlotsAndLegacyFallback(t *testing.T) {
	now := time.Now().UnixMilli()
	astraKey := codexTurnStateModelExtraKey("gpt-6-astra")
	for _, variant := range []string{"gpt-6", "gpt-6-astra-2026-09-19", "openai/gpt-6-astra-build-42"} {
		require.Equal(t, astraKey, codexTurnStateModelExtraKey(variant))
	}
	reviewKey := codexTurnStateModelExtraKey("codex-auto-review")
	require.Equal(t, reviewKey, codexTurnStateModelExtraKey("provider/codex-auto-review-2026-09-19"))
	require.NotEqual(t, astraKey, reviewKey)

	account := &Account{Extra: map[string]any{
		astraKey:  verifiedTurnStateSlot("openai/gpt-6-astra-2026-09-19", "astra-family-state", now),
		reviewKey: verifiedTurnStateSlot("codex-auto-review-build-42", "review-family-state", now),
	}}
	for _, variant := range []string{"gpt-6", "gpt-6-astra-2026-09-20", "provider/gpt-6-astra-build-43"} {
		require.Equal(t, "astra-family-state", codexTurnStateAutoToken(codexTurnStateModelAccount(account, variant)))
	}
	for _, variant := range []string{"codex-auto-review", "openai/codex-auto-review-2026-09-20"} {
		require.Equal(t, "review-family-state", codexTurnStateAutoToken(codexTurnStateModelAccount(account, variant)))
	}
	require.NotEqual(t,
		codexTurnStateAutoToken(codexTurnStateModelAccount(account, "gpt-6-astra")),
		codexTurnStateAutoToken(codexTurnStateModelAccount(account, "codex-auto-review")),
	)

	legacyOne := &Account{Extra: map[string]any{
		codexTurnStateRawModelExtraKey("gpt-6-astra-2026-09-18"): verifiedTurnStateSlot("gpt-6-astra-2026-09-18", "legacy-one", now),
	}}
	require.Equal(t, "legacy-one", codexTurnStateAutoToken(codexTurnStateModelAccount(legacyOne, "gpt-6")), "one verified same-family legacy slot remains readable")

	legacyAmbiguous := &Account{Extra: map[string]any{
		codexTurnStateRawModelExtraKey("gpt-6-astra-2026-09-18"): verifiedTurnStateSlot("gpt-6-astra-2026-09-18", "legacy-one", now),
		codexTurnStateRawModelExtraKey("gpt-6-astra-2026-09-19"): verifiedTurnStateSlot("gpt-6-astra-2026-09-19", "legacy-two", now),
	}}
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(legacyAmbiguous, "gpt-6-astra")), "multiple same-family legacy slots fail closed")

	canonicalWins := &Account{Extra: map[string]any{
		astraKey: map[string]any{},
		codexTurnStateRawModelExtraKey("gpt-6-astra-2026-09-18"): verifiedTurnStateSlot("gpt-6-astra-2026-09-18", "must-not-override-canonical", now),
	}}
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(canonicalWins, "gpt-6")), "an existing canonical slot always wins over legacy fallback")
}

func TestCodexTurnStateScopeCleanupKeysRetainsConfiguredAliasesAndFamilies(t *testing.T) {
	account := &Account{Extra: map[string]any{
		codexTurnStateModelExtraKey("gpt-5"): map[string]any{},
		codexTurnStateModelExtraKey("gpt-4"): map[string]any{},
		codexTurnStateModelExtraKey("gpt-6-astra"): map[string]any{
			CodexTurnStateAutoProbeModelExtraKey: "gpt-6-astra-2026-09-19",
		},
		codexTurnStateModelExtraKey("gpt-6-sol"):        map[string]any{},
		codexTurnStateProbeBurstBudgetExtraKey("gpt-5"): map[string]any{},
		codexTurnStateProbeBurstBudgetExtraKey("gpt-4"): map[string]any{},
		CodexTurnStateModelExtraPrefix + "malformed":    map[string]any{},
		CodexTurnStateNativeProvenanceExtraKey:          map[string]any{"digest": "kept"},
		"unrelated":                                     "kept",
	}}
	cfg := OpenAICodexTurnStateConfig{Models: "gpt-5,gpt-6-astra-*", ModelScopeValid: true}
	keys := codexTurnStateScopeCleanupKeys(account, cfg)
	require.Equal(t, []string{
		codexTurnStateModelExtraKey("gpt-4"),
		codexTurnStateModelExtraKey("gpt-6-sol"),
		CodexTurnStateModelExtraPrefix + "malformed",
		codexTurnStateProbeBurstBudgetExtraKey("gpt-4"),
	}, keys)
	filtered, filteredKeys := codexTurnStateAccountWithoutScopeRecords(account, cfg)
	require.Equal(t, keys, filteredKeys)
	require.NotContains(t, filtered.Extra, codexTurnStateModelExtraKey("gpt-4"))
	require.Contains(t, filtered.Extra, codexTurnStateModelExtraKey("gpt-5"))
	require.Contains(t, filtered.Extra, codexTurnStateModelExtraKey("gpt-6-astra"))
	require.Contains(t, filtered.Extra, CodexTurnStateNativeProvenanceExtraKey)
	require.Equal(t, "kept", filtered.Extra["unrelated"])
}

func TestCodexTurnStateScopeCleanupFailsClosedForInvalidOrEmptyScope(t *testing.T) {
	account := &Account{Extra: map[string]any{
		codexTurnStateModelExtraKey("gpt-4"):            map[string]any{},
		codexTurnStateProbeBurstBudgetExtraKey("gpt-4"): map[string]any{},
	}}
	for _, cfg := range []OpenAICodexTurnStateConfig{
		{Models: "gpt-5", ModelScopeValid: false},
		{Models: "", ModelScopeValid: true},
	} {
		filtered, keys := codexTurnStateAccountWithoutScopeRecords(account, cfg)
		require.Empty(t, keys)
		require.Same(t, account, filtered)
	}
}

func TestCodexTurnStateScopeCleanupDeletesPersistedOutOfScopeRecords(t *testing.T) {
	s, base, account := newTurnStateAutoService(t)
	account.Extra = map[string]any{
		codexTurnStateModelExtraKey("gpt-5"):            map[string]any{},
		codexTurnStateModelExtraKey("gpt-4"):            map[string]any{},
		codexTurnStateProbeBurstBudgetExtraKey("gpt-4"): map[string]any{},
	}
	base.mu.Lock()
	base.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	base.mu.Unlock()
	repo := &codexTurnStateScopeCleanupRepo{turnStateAutoRepo: base}
	s.accountRepo = repo
	cfg := OpenAICodexTurnStateConfig{Models: "gpt-5", ModelScopeValid: true}
	filtered, err := s.cleanupCodexTurnStateScope(context.Background(), account, cfg)
	require.NoError(t, err)
	require.NotContains(t, filtered.Extra, codexTurnStateModelExtraKey("gpt-4"))
	require.Contains(t, filtered.Extra, codexTurnStateModelExtraKey("gpt-5"))
	require.ElementsMatch(t, []string{
		codexTurnStateModelExtraKey("gpt-4"),
		codexTurnStateProbeBurstBudgetExtraKey("gpt-4"),
	}, repo.deleted)
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotContains(t, stored.Extra, codexTurnStateModelExtraKey("gpt-4"))
	require.NotContains(t, stored.Extra, codexTurnStateProbeBurstBudgetExtraKey("gpt-4"))
}

func TestCodexTurnStateScopeCleanupSkipsActiveOwnerUntilWorkerFinishes(t *testing.T) {
	s, base, account := newTurnStateAutoService(t)
	account.Extra = map[string]any{codexTurnStateModelExtraKey("gpt-4"): map[string]any{}}
	base.mu.Lock()
	base.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	base.mu.Unlock()
	repo := &codexTurnStateScopeCleanupRepo{turnStateAutoRepo: base}
	s.accountRepo = repo
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, time.Now(), "gpt-4")
	entry.running = true
	s.openaiTurnStateMu.Unlock()

	filtered, err := s.cleanupCodexTurnStateScope(context.Background(), account, OpenAICodexTurnStateConfig{Models: "gpt-5", ModelScopeValid: true})
	require.NoError(t, err)
	require.NotContains(t, filtered.Extra, codexTurnStateModelExtraKey("gpt-4"))
	require.Empty(t, repo.deleted, "an active owner must not be deleted while its worker can still persist")
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Contains(t, stored.Extra, codexTurnStateModelExtraKey("gpt-4"))

	s.openaiTurnStateMu.Lock()
	entry.running = false
	s.openaiTurnStateMu.Unlock()
	_, err = s.cleanupCodexTurnStateScope(context.Background(), account, OpenAICodexTurnStateConfig{Models: "gpt-5", ModelScopeValid: true})
	require.NoError(t, err)
	require.Contains(t, repo.deleted, codexTurnStateModelExtraKey("gpt-4"))
}

func TestCodexTurnStateScopedAccountModelAndIP(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	stateA, stateB := recoveryTestToken(now, 10, 1), recoveryTestToken(now, 10, 2)
	seedScopedTurnState(repo, a, "gpt-5.5", stateA)
	seedScopedTurnState(repo, a, "gpt-5.4", stateB)
	other := *a
	other.ID = 20
	other.Extra = nil
	repo.accounts[20] = &other
	for _, tc := range []struct {
		account             *Account
		model, native, want string
	}{
		{a, "gpt-5.5", "", stateA}, {a, "gpt-5.4", "", stateB},
		{a, "gpt-5.3", "", ""}, {&other, "gpt-5.5", "", ""},
		{a, "gpt-5.4", "official-native", "official-native"},
	} {
		h := http.Header{}
		h.Set(openAICodexTurnStateHeader, tc.native)
		require.NoError(t, s.applyOpenAICodexTurnState(context.Background(), tc.account, h, tc.model))
		require.Equal(t, tc.want, h.Get(openAICodexTurnStateHeader))
	}
	waitTurnStateAutoIdle(t, s)
	// Changing the transport IP must not change ownership or the selected state.
	for _, route := range []string{"http://first.test:8080", "socks5://second.test:1080"} {
		s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, proxy string, _ int64) (*http.Response, error) {
			require.Equal(t, route, proxy)
			require.Equal(t, stateA, req.Header.Get(openAICodexTurnStateHeader))
			return turnStateResponse(""), nil
		}}
		req, err := s.prepareCodexTurnStateRequest(context.Background(), httptest.NewRequest(http.MethodPost, "https://example.com/responses", nil), a, "client-alias", "gpt-5.5")
		require.NoError(t, err)
		resp, err := s.doOpenAIUpstream(req, route, a)
		require.NoError(t, err)
		require.Equal(t, stateA, *upstreamTurnStateFromResponse(resp))
	}
}

func TestCodexTurnStateScoped312HeaderOnlyDoesNotRevokeAnyModel(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	stateA, stateB := recoveryTestToken(now.Add(-time.Minute), 10, 1), recoveryTestToken(now.Add(-time.Minute), 10, 2)
	seedScopedTurnState(repo, a, "gpt-5.5", stateA)
	seedScopedTurnState(repo, a, "gpt-5.4", stateB)
	ctx := withCodexTurnStateModel(context.Background(), "gpt-5.5")
	s.collectOpenAICodexTurnState(ctx, a, recoveryTestToken(now, 11, 3), stateA)
	waitTurnStateAutoIdle(t, s)
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, stateA)
	require.NoError(t, s.applyOpenAICodexTurnState(ctx, a, h, "gpt-5.5"))
	require.Equal(t, stateA, h.Get(openAICodexTurnStateHeader))
	h = http.Header{}
	require.NoError(t, s.applyOpenAICodexTurnState(ctx, a, h, "gpt-5.4"))
	require.Equal(t, stateB, h.Get(openAICodexTurnStateHeader))
	next := recoveryTestToken(now, 10, 4)
	publishVerifiedTurnStateForTest(s, a, "gpt-5.5", next)
	waitTurnStateAutoIdle(t, s)
	stored, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, next, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.5")))
	require.Equal(t, stateB, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.4")))
}

func TestCodexTurnStateScopedTTLAndLegacy(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	state := recoveryTestToken(now, 10, 1)
	expires := codexTurnStateAutoExpiry(state, now.UnixMilli(), now)
	require.Equal(t, now.Add(time.Hour).UnixMilli(), expires)
	require.True(t, codexTurnStateFreshNormal(state, now.Add(time.Hour-time.Millisecond)))
	require.False(t, codexTurnStateFreshNormal(state, now.Add(time.Hour)))
	teamState := recoveryTestToken(now, 12, 2)
	require.Len(t, teamState, 332)
	require.True(t, codexTurnStateFreshNormal(teamState, now.Add(time.Hour-time.Millisecond)))
	require.False(t, codexTurnStateFreshNormal(teamState, now.Add(time.Hour)))
	s, repo, a := newTurnStateAutoService(t)
	a.Extra = map[string]any{CodexTurnStateAutoExtraKey: state, CodexTurnStateAutoSetAtExtraKey: now.UnixMilli()}
	repo.accounts[a.ID].Extra = mergeMap(nil, a.Extra)
	h := http.Header{}
	require.NoError(t, s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5.5"))
	require.Empty(t, h.Get(openAICodexTurnStateHeader), "unscoped historical state must not be assigned an invented model")
	seedScopedTurnState(repo, a, "gpt-5.5", recoveryTestToken(now.Add(-time.Hour), 10, 3))
	require.NoError(t, s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5.5"))
	require.Empty(t, h.Get(openAICodexTurnStateHeader), "the local one-hour bound prevents expired automatic injection")
	h.Set(openAICodexTurnStateHeader, state)
	require.NoError(t, s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5.5"))
	require.Equal(t, state, h.Get(openAICodexTurnStateHeader), "an official native continuation has priority and is not re-aged as cache data")
}

func TestCodexTurnStateScopedWorkersPersistIndependentModels(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-5.5,gpt-5.4"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	var mu sync.Mutex
	seen := map[string]int{}
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		mu.Lock()
		seen[body.Model]++
		mu.Unlock()
		marker := byte(1)
		if body.Model == "gpt-5.4" {
			marker = 2
		}
		return turnStateResponse(recoveryTestToken(time.Now(), 10, marker)), nil
	}}
	result, err := s.RequestCodexTurnStateCollection(context.Background(), a, "")
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	require.ElementsMatch(t, []string{"gpt-5.5", "gpt-5.4"}, result.QueuedModels)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, map[string]int{"gpt-5.5": 1, "gpt-5.4": 1}, seen)
	stored, err := repo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	first := codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.5"))
	second := codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.4"))
	require.NotEmpty(t, first)
	require.NotEmpty(t, second)
	require.NotEqual(t, first, second)
	s.openaiTurnStateMu.Lock()
	firstCandidate := s.openaiTurnStates[codexTurnStateKey{a.ID, "gpt-5.5"}].candidate
	secondCandidate := s.openaiTurnStates[codexTurnStateKey{a.ID, "gpt-5.4"}].candidate
	s.openaiTurnStateMu.Unlock()
	require.Empty(t, firstCandidate)
	require.Empty(t, secondCandidate)
	info := CodexTurnStateAutoInfoForAccount(stored, time.Now())
	require.Len(t, info.Models, 2)
	require.True(t, info.Models["gpt-5.5"].Configured)
	require.Equal(t, "gpt-5.5", info.Models["gpt-5.5"].VerifiedModel)
	require.NotZero(t, info.Models["gpt-5.5"].VerifiedAtMS)
	require.Equal(t, len(first), info.Models["gpt-5.5"].StateLength)
	require.True(t, info.Models["gpt-5.4"].Configured)
	require.Equal(t, "gpt-5.4", info.Models["gpt-5.4"].VerifiedModel)
	require.NotZero(t, info.Models["gpt-5.4"].VerifiedAtMS)
	require.Equal(t, len(second), info.Models["gpt-5.4"].StateLength)
	encoded, err := json.Marshal(info)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), first)
	require.NotContains(t, string(encoded), second)
}

func TestCodexTurnStateScopedDispatchReloadAndUnverifiedResponseAttribution(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	req, err := s.prepareCodexTurnStateRequest(context.Background(), httptest.NewRequest(http.MethodPost, "https://example.com/responses", nil), a, "gpt-5.5")
	require.NoError(t, err)
	require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
	waitTurnStateAutoIdle(t, s)
	value := recoveryTestToken(time.Now(), 10, 1)
	seedScopedTurnState(repo, a, "gpt-5.5", value)
	s.httpUpstream = &turnStateRawUpstream{call: func(sent *http.Request, _ string, _ int64) (*http.Response, error) {
		require.Equal(t, value, sent.Header.Get(openAICodexTurnStateHeader))
		return turnStateResponse(""), nil
	}}
	response, err := s.doOpenAIUpstream(req, "", a)
	require.NoError(t, err)
	require.Equal(t, value, *upstreamTurnStateFromResponse(response))
	waitTurnStateAutoIdle(t, s)
	s.settingService.settingRepo.(*codexHeaderSettingRepoStub).values[SettingKeyOpenAICodexTurnStateDefaultModel] = "gpt-5.4"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	returned := recoveryTestToken(time.Now(), 10, 2)
	s.collectCodexTurnStateHTTP(context.Background(), a, returned, response.Request)
	waitTurnStateAutoIdle(t, s)
	stored, err := repo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	require.NotEqual(t, returned, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.5")))
	require.Equal(t, value, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.5")), "a single response must not replace the verified model slot without both replay stages")
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.4")))
}

func TestCodexTurnStateScopedNativeProvenanceRejectsCrossAccountAndModel(t *testing.T) {
	s, _, a := newTurnStateAutoService(t)
	c, _ := newTurnStateTestContext(t, 7, "scoped-native")
	originRequest := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	originRequest = originRequest.WithContext(withCodexTurnStateModel(originRequest.Context(), "gpt-5.5"))
	stateHeader := func() http.Header {
		header := http.Header{}
		header.Set(openAICodexTurnStateHeader, "native-state")
		return header
	}
	upstream := stateHeader()
	s.relayOpenAICodexTurnState(c, a, upstream, originRequest)
	observer := beginUpstreamResponseModelObservation(c)
	observeCodexTurnStateLifecycle(observer, "gpt-5.5", "gpt-5.5", false)
	s.commitPendingCodexTurnStateObservation(c, true)

	same := stateHeader()
	s.guardOpenAICodexTurnStateEcho(c, a, same, "gpt-5.5")
	require.Equal(t, "native-state", same.Get(openAICodexTurnStateHeader))

	crossModel := stateHeader()
	s.guardOpenAICodexTurnStateEcho(c, a, crossModel, "gpt-5.4")
	require.Empty(t, crossModel.Get(openAICodexTurnStateHeader))

	other := *a
	other.ID++
	crossAccount := stateHeader()
	s.guardOpenAICodexTurnStateEcho(c, &other, crossAccount, "gpt-5.5")
	require.Empty(t, crossAccount.Get(openAICodexTurnStateHeader))
}

type failingTurnStateSource struct{ *turnStateAutoRepo }

func (r *failingTurnStateSource) GetCodexTurnStateSource(context.Context, int64) (*Account, error) {
	return nil, errors.New("private database failure")
}

func TestCodexTurnStateScopedLookupFailureDoesNotSendHeaderless(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	req, err := s.prepareCodexTurnStateRequest(context.Background(), httptest.NewRequest(http.MethodPost, "https://example.com/responses", nil), a, "gpt-5.5")
	require.NoError(t, err)
	waitTurnStateAutoIdle(t, s)
	s.accountRepo = &failingTurnStateSource{repo}
	_, err = s.doOpenAIUpstream(req, "", a)
	require.ErrorIs(t, err, errCodexTurnStateLookup)
	require.NotContains(t, err.Error(), "private")
}

func TestCodexTurnStateScopedWSModelChangeReconnects(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	first, second := recoveryTestToken(time.Now(), 10, 1), recoveryTestToken(time.Now(), 10, 2)
	seedScopedTurnState(repo, a, "gpt-5.5", first)
	seedScopedTurnState(repo, a, "gpt-5.4", second)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	dialer := newOpenAIWSFirstDialBlockingCaptureDialer()
	close(dialer.releaseFirst)
	pool.setClientDialerForTest(dialer)
	req := openAIWSAcquireRequest{Account: a, WSURL: "wss://example.com/responses", Headers: http.Header{}, TurnState: openAIWSTurnStatePolicy{Settings: s.settingService, Gateway: s, Models: []string{"gpt-5.5"}}}
	one, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	id := one.ConnID()
	require.Equal(t, first, *one.SentTurnState())
	one.Release()
	req.TurnState.Models = []string{"gpt-5.4"}
	two, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, id, two.ConnID())
	require.Equal(t, second, *two.SentTurnState())
	two.Release()
	require.NoError(t, s.checkCodexTurnStatePassthrough(context.Background(), a, first, "gpt-5.5", "gpt-5.5"))
	err = s.checkCodexTurnStatePassthrough(context.Background(), a, first, "gpt-5.5", "gpt-5.4")
	var closeErr *OpenAIWSClientCloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusTryAgainLater, closeErr.StatusCode())
}
