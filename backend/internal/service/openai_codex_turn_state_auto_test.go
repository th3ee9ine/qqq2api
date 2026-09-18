package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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

func (u *turnStateAutoUpstream) Do(req *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
	return u.call(req, proxy, id)
}
func newTurnStateAutoService(t *testing.T) (*OpenAIGatewayService, *turnStateAutoRepo, *Account) {
	t.Helper()
	account := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"access_token": "access-secret", "chatgpt_account_id": "account-test"}}
	repo := &turnStateAutoRepo{accounts: map[int64]*Account{10: account}}
	settings, sr := turnStateTestSettings("", "gpt-5*")
	sr.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "true"
	svc := &OpenAIGatewayService{settingService: settings, accountRepo: repo}
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
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}
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
			a := &Account{Extra: map[string]any{CodexTurnStateAutoExtraKey: tc.token, CodexTurnStateAutoSetAtExtraKey: tc.setAt, CodexTurnStateAutoLastErrorExtraKey: "bearer-private-error"}}
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
func TestCodexTurnStateAutoCollectionCommitDedupAndIsolation(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	h := turnStateResponse("collected-secret").Header
	var staged http.Header
	stageOpenAICodexTurnState(&staged, h)
	require.Equal(t, 0, repo.writes, "staging alone must not collect")
	s.noteStagedOpenAICodexTurnStateCommitted(c, a, staged)
	waitTurnStateAutoIdle(t, s)
	s.relayOpenAICodexTurnState(c, a, h)
	s.collectOpenAICodexTurnState(context.Background(), a, "bad\nstate")
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, 1, repo.writes)
	require.Empty(t, a.Extra, "never mutate the caller's snapshot")
	require.Equal(t, "collected-secret", s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	other := &Account{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), other, "gpt-5"))
	stored, _ := repo.GetByID(context.Background(), 10)
	require.Equal(t, "collected-secret", stored.Extra[CodexTurnStateAutoExtraKey])
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-4"))
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
	s.collectOpenAICodexTurnState(context.Background(), a, "native-newer-secret")
	close(release)
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 1, calls.Load())
	stored, _ := repo.GetByID(context.Background(), 10)
	require.Equal(t, "native-newer-secret", codexTurnStateAutoToken(stored))
	require.Empty(t, a.Extra)
}
func TestCodexTurnStateAutoProbeRenewalAndRetryThrottle(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{true: "failure", false: "success"}[fail], func(t *testing.T) {
			s, repo, a := newTurnStateAutoService(t)
			old := testGlobalTurnStateToken(time.Now().Add(-51*time.Minute), 10)
			a.Extra = map[string]any{CodexTurnStateAutoExtraKey: old, CodexTurnStateAutoSetAtExtraKey: time.Now().Add(-51 * time.Minute).UnixMilli()}
			repo.accounts[10].Extra = mergeMap(nil, a.Extra)
			var calls atomic.Int32
			s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
				calls.Add(1)
				if fail {
					return nil, errors.New("token=private-transport-error")
				}
				return turnStateResponse("renewed-state"), nil
			}}
			require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"), "keep usable token while renewing")
			waitTurnStateAutoIdle(t, s)
			stored, _ := repo.GetByID(context.Background(), 10)
			if fail {
				require.Equal(t, old, codexTurnStateAutoToken(stored))
				require.Equal(t, "transport_failed", stored.Extra[CodexTurnStateAutoLastErrorExtraKey])
			} else {
				require.Equal(t, "renewed-state", codexTurnStateAutoToken(stored))
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
	}{{401, "sensitive", "http_401"}, {429, "sensitive", "http_429"}, {200, "", "missing_state"}, {200, "bad\nsecret", "invalid_state"}} {
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
func TestCodexTurnStateAutoOverrideDisableAndWSRefresh(t *testing.T) {
	s, _, a := newTurnStateAutoService(t)
	s.collectOpenAICodexTurnState(context.Background(), a, "auto-state")
	waitTurnStateAutoIdle(t, s)
	h := http.Header{}
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, "auto-state", h.Get(openAICodexTurnStateHeader))
	h.Set(openAICodexTurnStateHeader, "native-state")
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, "native-state", h.Get(openAICodexTurnStateHeader))
	req := openAIWSAcquireRequest{Account: a, Headers: http.Header{}, TurnState: openAIWSTurnStatePolicy{Settings: s.settingService, Gateway: s, Models: []string{"gpt-5"}}}
	applied := req.withCurrentTurnState(context.Background())
	require.Equal(t, "auto-state", applied.Headers.Get(openAICodexTurnStateHeader))
	require.NotEmpty(t, applied.turnStateFingerprint)
	set := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	set.values[SettingKeyOpenAICodexTurnState] = "global-state"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, "global-state", h.Get(openAICodexTurnStateHeader))
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
	source := map[string]any{CodexTurnStateAutoExtraKey: "private-state", CodexTurnStateAutoSetAtExtraKey: int64(123), "note": "keep"}
	stripped := StripCodexTurnStateAutoExtra(source)
	require.Equal(t, map[string]any{"note": "keep"}, stripped)
	require.Contains(t, source, CodexTurnStateAutoExtraKey)
	repo := &upstreamBillingProbeAdminRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{10: {ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Extra: source}}}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 10, &UpdateAccountInput{Extra: map[string]any{CodexTurnStateAutoExtraKey: "injected-state", "note": "edited"}})
	require.NoError(t, err)
	require.Equal(t, "private-state", updated.Extra[CodexTurnStateAutoExtraKey])
	created, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, source)
	require.NoError(t, err)
	require.NotContains(t, created.Extra, CodexTurnStateAutoExtraKey)
}

func TestCodexTurnStateAutoProbeWorkerBoundAndDisableInFlight(t *testing.T) {
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
	for n := 0; n < codexTurnStateAutoMaxWorkers; n++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	require.EqualValues(t, codexTurnStateAutoMaxWorkers, calls.Load())
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	close(release)
	waitTurnStateAutoIdle(t, s)
	for _, account := range accounts {
		stored, _ := repo.GetByID(context.Background(), account.ID)
		require.Empty(t, codexTurnStateAutoToken(stored))
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
	require.NoError(t, err)
	require.Equal(t, "bounded-state", token)
	require.Equal(t, codexTurnStateAutoMaxBody, body.read)
	require.True(t, body.closed)
	repo.accounts[a.ID].Schedulable = false
	s.autoTurnStateForAccount(context.Background(), a, "gpt-5")
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 1, calls.Load(), "a stale request snapshot must not probe a now-paused account")
}

func TestCodexTurnStateAutoCollectionRetriesFailedPersistence(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	repo.fail = true
	s.collectOpenAICodexTurnState(context.Background(), a, "retry-state")
	waitTurnStateAutoIdle(t, s)
	repo.mu.Lock()
	repo.fail = false
	repo.mu.Unlock()
	s.openaiTurnStateMu.Lock()
	s.openaiTurnStates[a.ID].retryAfter = time.Time{}
	s.openaiTurnStateMu.Unlock()
	// A duplicate response retries a failed write without resetting collection age.
	s.collectOpenAICodexTurnState(context.Background(), a, "retry-state")
	waitTurnStateAutoIdle(t, s)
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Equal(t, "retry-state", codexTurnStateAutoToken(stored))
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
func TestCodexTurnStateAutoWSHandshakeCollection(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "rejected", false: "accepted"}[fail], func(t *testing.T) {
			s, repo, a := newTurnStateAutoService(t)
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
			if fail {
				require.Empty(t, codexTurnStateAutoToken(stored))
			} else {
				require.Equal(t, "ws-collected-state", codexTurnStateAutoToken(stored))
			}
		})
	}
}

func TestCodexTurnStateAutoRepeatedSuccessClearsErrorWithoutRenewingAge(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	at := time.Now().Add(-20 * time.Minute).UnixMilli()
	a.Extra = map[string]any{CodexTurnStateAutoExtraKey: "same-state", CodexTurnStateAutoSetAtExtraKey: at, CodexTurnStateAutoLastErrorExtraKey: "http_429"}
	repo.accounts[a.ID].Extra = mergeMap(nil, a.Extra)
	s.collectOpenAICodexTurnState(context.Background(), a, "same-state")
	waitTurnStateAutoIdle(t, s)
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Empty(t, stored.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	require.Equal(t, at, codexTurnStateAutoInt64(stored, CodexTurnStateAutoSetAtExtraKey))
}
