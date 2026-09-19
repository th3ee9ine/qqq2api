package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type turnStateAutoRepo struct {
	AccountRepository
	mu       sync.Mutex
	accounts map[int64]*Account
	writes   int
	fail     bool
}

func (r *turnStateAutoRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.accounts[id]
	if a == nil {
		return nil, ErrAccountNotFound
	}
	copy := *a
	copy.Extra = mergeMap(nil, a.Extra)
	copy.Credentials = mergeMap(nil, a.Credentials)
	return &copy, nil
}
func (r *turnStateAutoRepo) GetCodexTurnStateSource(ctx context.Context, id int64) (*Account, error) {
	return r.GetByID(ctx, id)
}
func (r *turnStateAutoRepo) CompareAndSwapCodexTurnStateProbeBurstBudget(
	_ context.Context,
	id int64,
	slot string,
	expectedVersion int64,
	budget CodexTurnStateProbeBurstBudget,
) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[id]
	if account == nil {
		return false, ErrAccountNotFound
	}
	current, err := codexTurnStateProbeBurstBudgetFromAccount(account, slot)
	if err != nil {
		return false, err
	}
	if current.Version != expectedVersion {
		return false, nil
	}
	if budget.Version != expectedVersion+1 || slot != codexTurnStateProbeBurstBudgetExtraKey(budget.Model) {
		return false, errCodexTurnStateProbeBurstBudgetCorrupt
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[slot] = budget
	return true, nil
}
func (r *turnStateAutoRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, bounded := ctx.Deadline(); !bounded {
		panic("unbounded persistence")
	}
	r.writes++
	if r.fail {
		return errors.New("private-provider-error")
	}
	a := r.accounts[id]
	if a == nil {
		return ErrAccountNotFound
	}
	a.Extra = mergeMap(a.Extra, updates)
	return nil
}

type turnStateAutoUpstream struct {
	HTTPUpstream
	call func(*http.Request, string, int64) (*http.Response, error)
}

type turnStateRawUpstream struct {
	HTTPUpstream
	call func(*http.Request, string, int64) (*http.Response, error)
}

func (u *turnStateRawUpstream) Do(req *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
	return u.call(req, proxy, id)
}

func (u *turnStateAutoUpstream) Do(req *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
	var payload []byte
	if req.Body != nil {
		payload, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(payload))
	}
	model := strings.TrimSpace(gjson.GetBytes(payload, "model").String())
	if req.Header.Get(openAICodexTurnStateHeader) != "" {
		return turnStateModelResponse("", model), nil
	}
	resp, err := u.call(req, proxy, id)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if _, synthetic := resp.Body.(*turnStateSyntheticBody); synthetic {
		_ = resp.Body.Close()
		resp.Body = turnStateModelResponse("", model).Body
	}
	return resp, nil
}
func newTurnStateAutoService(t *testing.T) (*OpenAIGatewayService, *turnStateAutoRepo, *Account) {
	t.Helper()
	account := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "access-secret", "chatgpt_account_id": "account-test"}}
	repo := &turnStateAutoRepo{accounts: map[int64]*Account{10: account}}
	settings, sr := turnStateTestSettings("", "gpt-5*")
	sr.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "true"
	sr.values[SettingKeyOpenAICodexTurnStateDefaultModel] = "gpt-5"
	svc := &OpenAIGatewayService{
		settingService: settings,
		accountRepo:    repo,
		proxyRepo:      &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedSessionProxy("DEFAULT01")}},
	}
	t.Cleanup(func() { waitTurnStateAutoIdle(t, svc) })
	copy, err := repo.GetByID(context.Background(), 10)
	require.NoError(t, err)
	return svc, repo, copy
}
func waitTurnStateAutoIdle(t *testing.T, s *OpenAIGatewayService) {
	t.Helper()
	require.Eventually(t, func() bool {
		s.openaiTurnStateMu.Lock()
		defer s.openaiTurnStateMu.Unlock()
		return s.openaiTurnStateWorkers == 0
	}, 3*time.Second, time.Millisecond)
}
func turnStateResponse(state string) *http.Response {
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, state)
	return &http.Response{StatusCode: 200, Header: h, Body: &turnStateSyntheticBody{Reader: strings.NewReader("data: [DONE]\n\n")}}
}

type turnStateSyntheticBody struct{ *strings.Reader }

func (b *turnStateSyntheticBody) Close() error { return nil }

func turnStateModelResponse(state, model string) *http.Response {
	resp := turnStateResponse(state)
	body := "data: {\"type\":\"response.created\",\"response\":{\"model\":" + strconv.Quote(model) + "}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"model\":" + strconv.Quote(model) + ",\"status\":\"completed\",\"output\":[]}}\n\n" +
		"data: [DONE]\n\n"
	resp.Body = io.NopCloser(strings.NewReader(body))
	return resp
}

func publishVerifiedTurnStateForTest(s *OpenAIGatewayService, account *Account, model, state string) {
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	s.setCodexTurnStateLocked(entry, state, now)
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	s.openaiTurnStateMu.Unlock()
}
func TestCodexTurnStateAutoExpiryAndSafeDiagnostics(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name, token string
		setAt       int64
		due         bool
	}{
		{"missing", "", 0, true},
		{"fresh", testGlobalTurnStateToken(now.Add(-time.Minute), 10), now.UnixMilli(), false},
		{"renew", testGlobalTurnStateToken(now.Add(-51*time.Minute), 10), now.UnixMilli(), true},
		{"opaque", "opaque-private-state", now.UnixMilli(), false},
		{"opaque-no-clock", "opaque-private-state", 0, true},
		{"opaque-expired", "opaque-private-state", now.Add(-time.Hour).UnixMilli(), true},
		{"future-does-not-extend", testGlobalTurnStateToken(now.Add(time.Hour), 10), now.Add(-time.Hour).UnixMilli(), true},
		{"unsafe", "state\r\nsecret", now.UnixMilli(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := map[string]any{CodexTurnStateAutoExtraKey: tc.token, CodexTurnStateAutoSetAtExtraKey: tc.setAt, CodexTurnStateAutoLastErrorExtraKey: "bearer-private-error"}
			if tc.token != "" {
				extra[CodexTurnStateAutoVerifiedAtExtraKey] = tc.setAt
				extra[CodexTurnStateAutoVerifiedModelExtraKey] = "gpt-5"
			}
			a := &Account{Extra: extra}
			info := codexTurnStateAutoInfo(a, now)
			require.Equal(t, tc.due, info.Due)
			data, err := json.Marshal(info)
			require.NoError(t, err)
			require.NotContains(t, string(data), "private")
			if tc.token != "" {
				require.NotContains(t, string(data), tc.token)
			}
		})
	}
}
func TestCodexTurnStateAutoCollectionRequiresReplayVerification(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	h := turnStateResponse("collected-secret").Header
	var staged http.Header
	stageOpenAICodexTurnState(&staged, h)
	require.Equal(t, 0, repo.writes, "staging alone must not collect")
	observer := beginUpstreamResponseModelObservation(c)
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-5"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5"}}`), "response.completed")
	s.noteStagedOpenAICodexTurnStateCommitted(c, a, staged)
	waitTurnStateAutoIdle(t, s)
	s.relayOpenAICodexTurnState(c, a, h)
	s.collectOpenAICodexTurnState(context.Background(), a, "bad\nstate")
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, 0, repo.writes, "an ordinary response must not publish a candidate before both replay stages")
	require.Empty(t, a.Extra, "never mutate the caller's snapshot")
	stored, _ := repo.GetByID(context.Background(), 10)
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-4")))
}
func TestCodexTurnStateAutoProbeDedupNonBlockingAndNativeWins(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, proxy string, id int64) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return turnStateResponse("probe-secret"), nil
	}}
	var wg sync.WaitGroup
	for n := 0; n < 100; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.autoTurnStateForAccount(context.Background(), a, "alias", "gpt-5") }()
	}
	wg.Wait() // All request paths return even though the network probe is blocked.
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	publishVerifiedTurnStateForTest(s, a, "gpt-5", "native-newer-secret")
	close(release)
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 1, calls.Load())
	stored, _ := repo.GetByID(context.Background(), 10)
	require.Equal(t, "native-newer-secret", codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
	require.Empty(t, a.Extra)
}
func TestCodexTurnStateAutoProbeRenewalAndRetryThrottle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{true: "failure", false: "success"}[fail], func(t *testing.T) {
			s, repo, a := newTurnStateAutoService(t)
			renewed := testGlobalTurnStateToken(time.Now(), 10)
			old := testGlobalTurnStateToken(time.Now().Add(-51*time.Minute), 10)
			seedAutomaticTurnState(a, old, "gpt-5")
			codexTurnStateModelAccount(a, "gpt-5").Extra[CodexTurnStateAutoSetAtExtraKey] = time.Now().Add(-51 * time.Minute).UnixMilli()
			repo.accounts[10].Extra = mergeMap(nil, a.Extra)
			var calls atomic.Int32
			s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
				calls.Add(1)
				if fail {
					return nil, errors.New("token=private-transport-error")
				}
				return turnStateResponse(renewed), nil
			}}
			require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"), "keep usable token while renewing")
			waitTurnStateAutoIdle(t, s)
			stored, _ := repo.GetByID(context.Background(), 10)
			if fail {
				require.Equal(t, old, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
				require.Equal(t, "transport_failed", codexTurnStateModelAccount(stored, "gpt-5").Extra[CodexTurnStateAutoLastErrorExtraKey])
			} else {
				require.Equal(t, old, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")), "maintenance success only stages a candidate; it cannot replace the verified token before usage evidence")
				s.openaiTurnStateMu.Lock()
				require.Equal(t, renewed, s.openaiTurnStates[codexTurnStateKey{a.ID, "gpt-5"}].candidate.state)
				require.Empty(t, s.openaiTurnStates[codexTurnStateKey{a.ID, "gpt-5"}].candidate.requestID)
				s.openaiTurnStateMu.Unlock()
			}
			for n := 0; n < 10; n++ {
				s.autoTurnStateForAccount(context.Background(), a, "gpt-5")
			}
			waitTurnStateAutoIdle(t, s)
			require.EqualValues(t, 1, calls.Load())
			require.Equal(t, StatusActive, stored.Status)
			require.True(t, stored.Schedulable)
			// A second gateway reads the persisted cooldown and does not re-probe.
			second := &OpenAIGatewayService{settingService: s.settingService, accountRepo: repo, httpUpstream: s.httpUpstream}
			second.autoTurnStateForAccount(context.Background(), stored, "gpt-5")
			waitTurnStateAutoIdle(t, second)
			require.EqualValues(t, 1, calls.Load())
		})
	}
}
func TestCodexTurnStateAutoProbeAuthenticationAndResponseValidation(t *testing.T) {
	s, _, a := newTurnStateAutoService(t)
	for _, authMode := range []string{"oauth", "setup", "agent"} {
		t.Run(authMode, func(t *testing.T) {
			ac := *a
			ac.Credentials = mergeMap(nil, a.Credentials)
			expected := "Bearer access-secret"
			s.openAITokenProvider = nil
			if authMode == "oauth" {
				s.openAITokenProvider = &OpenAITokenProvider{tokenCache: &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(&ac): "cached-access"}}}
				expected = "Bearer cached-access"
			}
			if authMode == "setup" {
				ac.Type = AccountTypeSetupToken
			}
			if authMode == "agent" {
				key, private := newTestAgentIdentityKey(t)
				ac.Credentials = map[string]any{"auth_mode": OpenAIAuthModeAgentIdentity, "agent_runtime_id": key.runtimeID, "agent_private_key": private, "task_id": key.taskID}
				expected = "AgentAssertion "
			}
			ac.Credentials["header_overrides"] = map[string]any{openAICodexTurnStateHeader: "old-state"}
			s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, id int64) (*http.Response, error) {
				require.Equal(t, a.ID, id)
				require.True(t, strings.HasPrefix(req.Header.Get("Authorization"), expected))
				require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, chatgptCodexAPIURL, req.URL.String())
				require.Equal(t, "chatgpt.com", req.Host)
				body, _ := io.ReadAll(req.Body)
				var payload map[string]any
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, false, payload["store"])
				require.Equal(t, true, payload["stream"])
				return turnStateResponse("new-state"), nil
			}}
			token, err := s.probeOpenAICodexTurnState(context.Background(), &ac, "gpt-5")
			require.NoError(t, err)
			require.Equal(t, "new-state", token)
		})
	}
	s.openAITokenProvider = nil
	for _, tc := range []struct {
		status      int
		state, code string
	}{{401, "sensitive", "http_401"}, {429, "sensitive", codexTurnStateProbe429NoRetryAfterCode}, {200, "", "missing_state"}, {200, "bad\nsecret", "invalid_state"}} {
		s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
			r := turnStateResponse(tc.state)
			r.StatusCode = tc.status
			return r, nil
		}}
		token, err := s.probeOpenAICodexTurnState(context.Background(), a, "gpt-5")
		require.Empty(t, token)
		require.EqualError(t, err, tc.code)
	}
}
func TestCodexTurnStateAutoLegacyIgnoredDisableAndWSRefresh(t *testing.T) {
	s, _, a := newTurnStateAutoService(t)
	publishVerifiedTurnStateForTest(s, a, "gpt-5", "auto-state")
	waitTurnStateAutoIdle(t, s)
	h := http.Header{}
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, "auto-state", h.Get(openAICodexTurnStateHeader))
	h.Set(openAICodexTurnStateHeader, "native-state")
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, "native-state", h.Get(openAICodexTurnStateHeader), "official native continuation has priority over automatic injection")
	req := openAIWSAcquireRequest{Account: a, Headers: http.Header{}, TurnState: openAIWSTurnStatePolicy{Settings: s.settingService, Gateway: s, Models: []string{"gpt-5"}}}
	applied := req.withCurrentTurnState(context.Background())
	require.Equal(t, "auto-state", applied.Headers.Get(openAICodexTurnStateHeader))
	require.NotEmpty(t, applied.turnStateFingerprint)
	set := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	set.values[SettingKeyOpenAICodexTurnState] = "global-state"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, "native-state", h.Get(openAICodexTurnStateHeader), "legacy settings do not displace native continuation")
	set.values[SettingKeyOpenAICodexTurnStateEnabled] = "false"
	set.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	applied = applied.withCurrentTurnState(context.Background())
	require.Empty(t, applied.Headers.Get(openAICodexTurnStateHeader))
	require.Empty(t, applied.turnStateFingerprint)
	s.collectOpenAICodexTurnState(context.Background(), a, "ignored-state")
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
}
func TestCodexTurnStateAutoManagedExtraPreservedAndNotImported(t *testing.T) {
	burstKey := codexTurnStateProbeBurstBudgetExtraKey("gpt-5.5")
	source := map[string]any{
		codexTurnStateModelExtraKey("gpt-5.5"): map[string]any{CodexTurnStateAutoExtraKey: "private-scoped"},
		burstKey:                               CodexTurnStateProbeBurstBudget{Version: 1, Model: "gpt-5.5", StartedAtMS: 123, Attempts: 1},
		CodexTurnStateAutoExtraKey:             "private-state",
		CodexTurnStateAutoSetAtExtraKey:        int64(123),
		"note":                                 "keep",
	}
	stripped := StripCodexTurnStateAutoExtra(source)
	require.Equal(t, map[string]any{"note": "keep"}, stripped)
	require.Contains(t, source, CodexTurnStateAutoExtraKey)
	require.Contains(t, source, burstKey)
	repo := &upstreamBillingProbeAdminRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{10: {ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: source}}}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 10, &UpdateAccountInput{Extra: map[string]any{CodexTurnStateAutoExtraKey: "injected-state", "note": "edited"}})
	require.NoError(t, err)
	require.Equal(t, "private-state", updated.Extra[CodexTurnStateAutoExtraKey])
	require.Equal(t, source[codexTurnStateModelExtraKey("gpt-5.5")], updated.Extra[codexTurnStateModelExtraKey("gpt-5.5")])
	require.Equal(t, source[burstKey], updated.Extra[burstKey], "ordinary account edits must not reset the durable burst budget")
	created, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, source)
	require.NoError(t, err)
	require.NotContains(t, created.Extra, CodexTurnStateAutoExtraKey)
	require.NotContains(t, created.Extra, codexTurnStateModelExtraKey("gpt-5.5"))
	require.NotContains(t, created.Extra, burstKey)
}

func TestCodexTurnStateAutoProbeWorkersAreNotGloballyCappedAndDisableInFlight(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	accounts := make([]*Account, 12)
	for i := range accounts {
		copy := *a
		copy.ID = int64(i + 100)
		accounts[i] = &copy
		repo.accounts[copy.ID] = &copy
	}
	started, release := make(chan struct{}, 12), make(chan struct{})
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return turnStateResponse("discard-after-disable"), nil
	}}
	for _, account := range accounts {
		s.autoTurnStateForAccount(context.Background(), account, "gpt-5")
	}
	for range accounts {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	require.EqualValues(t, len(accounts), calls.Load())
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	close(release)
	waitTurnStateAutoIdle(t, s)
	for _, account := range accounts {
		stored, _ := repo.GetByID(context.Background(), account.ID)
		require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
	}
}

type turnStateCountBody struct {
	read   int
	closed bool
}

func (b *turnStateCountBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	b.read += len(p)
	return len(p), nil
}
func (b *turnStateCountBody) Close() error { b.closed = true; return nil }
func TestCodexTurnStateAutoProbeBodyBoundAndUnavailableAccount(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	body := &turnStateCountBody{}
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		resp := turnStateResponse("bounded-state")
		resp.Body = body
		return resp, nil
	}}
	token, err := s.probeOpenAICodexTurnState(context.Background(), a, "gpt-5")
	require.ErrorIs(t, err, errCodexTurnStateResponseNotCompleted)
	require.Empty(t, token)
	require.Equal(t, codexTurnStateAutoMaxBody+1, body.read)
	require.True(t, body.closed)
	repo.accounts[a.ID].Schedulable = false
	s.autoTurnStateForAccount(context.Background(), a, "gpt-5")
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 2, calls.Load(), "Turn State collection must ignore account schedulability")
}

func TestManualCodexTurnStateCollectionPublishesWithAutoDisabledAndUnschedulableAccount(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	account.Schedulable = false
	repo.accounts[account.ID].Schedulable = false

	const (
		model     = "gpt-5"
		candidate = "manual-self-contained-candidate"
	)
	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(candidate, model), nil
		}
		require.Equal(t, candidate, req.Header.Get(openAICodexTurnStateHeader))
		return turnStateModelResponse("", model), nil
	}}

	result, err := s.RequestCodexTurnStateCollection(context.Background(), account, model)
	require.NoError(t, err)
	require.Equal(t, CodexTurnStateManualStatusQueued, result.Status)
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 2, calls.Load(), "manual collection needs only collection and same-route replay")

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	slot := codexTurnStateModelAccount(stored, model)
	require.Equal(t, candidate, codexTurnStateAutoToken(slot))
	require.Equal(t, model, slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
}

func TestCodexTurnStateModelScopeRejectsAutomaticAndManualCollection(t *testing.T) {
	for _, tc := range []struct {
		name, requestedModel, expectedModel string
	}{
		{name: "explicit model", requestedModel: "gpt-5", expectedModel: "gpt-5"},
		{name: "default model", requestedModel: "", expectedModel: "gpt-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, account := newTurnStateAutoService(t)
			settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
			settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6*"
			settings.values[SettingKeyOpenAICodexTurnStateDefaultModel] = "gpt-5"
			s.settingService.InvalidateOpenAICodexTurnStateCache()

			var calls atomic.Int32
			s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls.Add(1)
				return turnStateModelResponse("must-not-be-collected", "gpt-5"), nil
			}}

			require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, "gpt-5"))
			result, err := s.RequestCodexTurnStateCollection(context.Background(), account, tc.requestedModel)
			require.NoError(t, err)
			require.Equal(t, CodexTurnStateManualStatusRejected, result.Status)
			require.Equal(t, "model_out_of_scope", result.Reason)
			require.Equal(t, tc.expectedModel, result.Model)
			waitTurnStateAutoIdle(t, s)
			require.Zero(t, calls.Load(), "out-of-scope models must not start maintenance requests")
		})
	}
}

func TestCodexTurnStateProbeScopeReplacesHistoricalAliases(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	now := time.Now()

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, "alias-a", "gpt-5")
	replacedFirst := replaceCodexTurnStateProbeModelsLocked(entry, "gpt-5", "alias-a", "gpt-5")
	sameEntry := s.codexTurnStateEntryLocked(account, now.Add(time.Millisecond), "alias-b", "gpt-5")
	afterRefresh := append([]string(nil), entry.scopeModels...)
	replacedSecond := replaceCodexTurnStateProbeModelsLocked(entry, "gpt-5", "alias-b", "gpt-5")
	afterReplacement := append([]string(nil), entry.scopeModels...)
	s.openaiTurnStateMu.Unlock()

	require.True(t, replacedFirst)
	require.Same(t, entry, sameEntry)
	require.Equal(t, []string{"alias-a", "gpt-5"}, afterRefresh, "cache refreshes must not append unrelated aliases")
	require.True(t, replacedSecond)
	require.Equal(t, []string{"alias-b", "gpt-5"}, afterReplacement, "a new task must replace, not union, its scope snapshot")
}

func TestCodexTurnStateActiveProbeTaskKeepsOwnScopeSnapshot(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	now := time.Now()

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, "alias-a", "gpt-5")
	require.True(t, replaceCodexTurnStateProbeModelsLocked(entry, "gpt-5", "alias-a", "gpt-5"))
	entry.forceProbe = true
	task := codexTurnStateProbeTaskLocked(entry)
	require.True(t, replaceCodexTurnStateProbeModelsLocked(entry, "gpt-5", "alias-b", "gpt-5"))
	entry.forceProbe = false
	s.openaiTurnStateMu.Unlock()

	require.Equal(t, "gpt-5", task.requestModel)
	require.Equal(t, []string{"alias-a", "gpt-5"}, task.scopeModels)
	require.True(t, task.force)
	require.True(t, codexTurnStateProbeTaskAllows(OpenAICodexTurnStateConfig{Models: "alias-a", ModelScopeValid: true}, task))
	require.False(t, codexTurnStateProbeTaskAllows(OpenAICodexTurnStateConfig{Models: "alias-b", ModelScopeValid: true}, task))
	require.True(t, codexTurnStateEntryScopeAllowed(OpenAICodexTurnStateConfig{Models: "alias-b", ModelScopeValid: true}, entry))

	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "alias-b"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return turnStateModelResponse("must-not-be-staged", "gpt-5"), nil
	}}
	s.runCodexTurnStateProbeTaskWithMode(account.ID, entry, false, task)
	require.Zero(t, calls.Load(), "an active task cannot borrow a later queued alias")
	s.openaiTurnStateMu.Lock()
	require.Empty(t, entry.candidate.state)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateOutOfScopeDirtyStateStillPersists(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "allowed-alias"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	now := time.Now()
	state := recoveryTestToken(now, 10, 91)
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, "blocked-alias", "gpt-5")
	replaced := replaceCodexTurnStateProbeModelsLocked(entry, "gpt-5", "blocked-alias", "gpt-5")
	s.setCodexTurnStateLocked(entry, state, now)
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	s.openaiTurnStateMu.Unlock()

	require.True(t, replaced)
	waitTurnStateAutoIdle(t, s)
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, state, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
}

func TestCodexTurnStateOutOfScopeDirtyRetryTimerPersistsWithoutTraffic(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "allowed-alias"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	now := time.Now()
	state := recoveryTestToken(now, 10, 92)
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, "blocked-alias", "gpt-5")
	require.True(t, replaceCodexTurnStateProbeModelsLocked(entry, "gpt-5", "blocked-alias", "gpt-5"))
	s.setCodexTurnStateLocked(entry, state, now)
	entry.retryAfter = now.Add(20 * time.Millisecond)
	s.scheduleCodexTurnStatePersistenceRetryLocked(account.ID, entry, entry.retryAfter)
	s.openaiTurnStateMu.Unlock()

	require.Eventually(t, func() bool {
		stored, err := repo.GetByID(context.Background(), account.ID)
		return err == nil && codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")) == state
	}, time.Second, time.Millisecond)
	waitTurnStateAutoIdle(t, s)
	s.openaiTurnStateMu.Lock()
	require.False(t, entry.dirty)
	require.True(t, entry.retryWakeAt.IsZero())
	require.False(t, entry.probe)
	require.False(t, entry.manualProbe)
	s.openaiTurnStateMu.Unlock()
	repo.mu.Lock()
	require.Equal(t, 1, repo.writes)
	repo.mu.Unlock()
}

func TestCodexTurnStateAutoCollectionRetriesFailedPersistence(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	repo.fail = true
	publishVerifiedTurnStateForTest(s, a, "gpt-5", "retry-state")
	waitTurnStateAutoIdle(t, s)
	repo.mu.Lock()
	repo.fail = false
	repo.mu.Unlock()
	s.openaiTurnStateMu.Lock()
	s.openaiTurnStates[codexTurnStateKey{a.ID, "gpt-5"}].retryAfter = time.Time{}
	s.openaiTurnStateMu.Unlock()
	// A duplicate response retries a failed write without resetting collection age.
	publishVerifiedTurnStateForTest(s, a, "gpt-5", "retry-state")
	waitTurnStateAutoIdle(t, s)
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Equal(t, "retry-state", codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
	require.Equal(t, 2, repo.writes)
}

type turnStateAutoWSDialer struct{ fail bool }

func (d *turnStateAutoWSDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	h := turnStateResponse("ws-collected-state").Header
	if d.fail {
		return nil, 403, h, errors.New("handshake rejected")
	}
	return &openAIWSFakeConn{}, 101, h, nil
}
func TestCodexTurnStateAutoWSHandshakeDoesNotPromoteHeaderOnlyState(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "rejected", false: "accepted"}[fail], func(t *testing.T) {
			s, repo, a := newTurnStateAutoService(t)
			seedAutomaticTurnState(a, "previous-verified-state", "gpt-5")
			repo.accounts[a.ID].Extra = mergeMap(nil, a.Extra)
			pool := newOpenAIWSConnPool(nil)
			t.Cleanup(pool.Close)
			pool.setClientDialerForTest(&turnStateAutoWSDialer{fail: fail})
			conn, err := pool.dialConn(context.Background(), openAIWSAcquireRequest{Account: a, WSURL: "wss://example.com/responses", TurnState: openAIWSTurnStatePolicy{Settings: s.settingService, Gateway: s}})
			if fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				closeOpenAIWSConns([]*openAIWSConn{conn})
			}
			waitTurnStateAutoIdle(t, s)
			stored, _ := repo.GetByID(context.Background(), a.ID)
			require.Equal(t, "previous-verified-state", codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")),
				"a WS handshake header has no created/completed evidence and must not replace the automatic cache")
		})
	}
}

func TestCodexTurnStateAutoRepeatedSuccessClearsErrorWithoutRenewingAge(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	at := time.Now().Add(-20 * time.Minute).UnixMilli()
	a.Extra = map[string]any{codexTurnStateModelExtraKey("gpt-5"): map[string]any{
		CodexTurnStateAutoExtraKey: "same-state", CodexTurnStateAutoSetAtExtraKey: at, CodexTurnStateAutoLastErrorExtraKey: "http_429",
		CodexTurnStateAutoVerifiedAtExtraKey: at, CodexTurnStateAutoVerifiedModelExtraKey: "gpt-5",
	}}
	repo.accounts[a.ID].Extra = mergeMap(nil, a.Extra)
	publishVerifiedTurnStateForTest(s, a, "gpt-5", "same-state")
	waitTurnStateAutoIdle(t, s)
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Empty(t, codexTurnStateModelAccount(stored, "gpt-5").GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	require.Equal(t, at, codexTurnStateAutoInt64(codexTurnStateModelAccount(stored, "gpt-5"), CodexTurnStateAutoSetAtExtraKey))
	require.Equal(t, at, codexTurnStateAutoInt64(codexTurnStateModelAccount(stored, "gpt-5"), CodexTurnStateAutoVerifiedAtExtraKey),
		"same-token verification must not mint a new burst generation")
}
