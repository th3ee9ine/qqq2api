package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai"
)

// Explicit opt-in only: this test spends real account quota. Account credentials
// and issued states stay in memory; failures never print their values. The
// service request builders/workers are real; persistence is an in-memory repo
// and the network adapter is net/http, not the production repository pool.
func TestCodexTurnStateLiveIntegration(t *testing.T) {
	path := os.Getenv("CODEX_TURN_STATE_LIVE_ACCOUNT_FILE")
	if path == "" {
		t.Skip("set CODEX_TURN_STATE_LIVE_ACCOUNT_FILE to explicitly enable real upstream requests")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("cannot read live account file")
	}
	var exported struct {
		Accounts []struct {
			Platform    string         `json:"platform"`
			Type        string         `json:"type"`
			Credentials map[string]any `json:"credentials"`
			Extra       map[string]any `json:"extra"`
			ProxyID     *int64         `json:"proxy_id"`
			ProxyRef    string         `json:"proxy_ref"`
		} `json:"accounts"`
	}
	if json.Unmarshal(data, &exported) != nil || len(exported.Accounts) != 1 {
		t.Fatal("live test requires exactly one exported account")
	}
	source := exported.Accounts[0]
	if source.Platform != PlatformOpenAI || source.Type != AccountTypeOAuth || source.ProxyID != nil || source.ProxyRef != "" {
		t.Fatal("live test requires one OpenAI OAuth account without an assigned proxy")
	}
	a := &Account{ID: 1, Platform: source.Platform, Type: source.Type, Credentials: source.Credentials,
		Extra: StripCodexTurnStateAutoExtra(source.Extra), Concurrency: 1, Status: StatusActive, Schedulable: true}
	if a.GetOpenAIAccessToken() == "" {
		t.Fatal("live account has no access token")
	}
	model := strings.TrimSpace(os.Getenv("CODEX_TURN_STATE_LIVE_MODEL"))
	if model == "" {
		model = openai.DefaultTestModel
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	defer transport.CloseIdleConnections()
	network := &codexTurnStateLiveHTTP{t: t, credentials: a.Credentials, client: &http.Client{Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	repo := &turnStateAutoRepo{accounts: map[int64]*Account{a.ID: a}}
	settings, sr := turnStateTestSettings("", "")
	sr.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "true"
	sr.values[SettingKeyOpenAICodexTurnStateDefaultModel] = model
	s := &OpenAIGatewayService{settingService: settings, accountRepo: repo, httpUpstream: network, cfg: &config.Config{}}
	defer codexTurnStateLiveWait(t, s)
	gin.SetMode(gin.TestMode)

	snapshot := func() *Account {
		copy, err := repo.GetByID(context.Background(), a.ID)
		if err != nil {
			t.Fatal("in-memory snapshot failed")
		}
		return copy
	}
	usable := func(phase string) string {
		codexTurnStateLiveWait(t, s)
		stored := snapshot()
		info := CodexTurnStateAutoInfoForAccount(stored, time.Now())
		t.Logf("phase=%s configured=%t recovery_pending=%t last_error=%s", phase, info.Configured, info.RecoveryPending, info.LastError)
		state := codexTurnStateAutoToken(stored)
		_, blocks, ok := parseCodexTurnState(state)
		if !ok || blocks != 10 || info.RecoveryPending {
			t.Fatalf("phase=%s did not produce a usable upstream 292", phase)
		}
		return state
	}
	ordinary := func(phase string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		account := snapshot()
		payload := createOpenAITestPayload(model, true)
		payload["instructions"] = "Reply with OK only."
		body, _ := json.Marshal(payload)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		req, err := s.buildUpstreamRequest(ctx, c, account, body, account.GetOpenAIAccessToken(), true, "", true, model)
		if err != nil {
			t.Fatal("live gateway request construction failed")
		}
		network.setPhase(phase)
		resp, err := s.doOpenAIUpstream(req, "", account)
		if err != nil {
			t.Fatal("live gateway transport failed (details suppressed)")
		}
		defer resp.Body.Close()
		sentSnapshot := upstreamTurnStateFromResponse(resp)
		if sentSnapshot == nil || *sentSnapshot != req.Header.Get(openAICodexTurnStateHeader) || *sentSnapshot == "" {
			t.Fatal("actual outbound Turn State snapshot was not preserved")
		}
		t.Logf("phase=%s outbound_state_snapshot_matches=true", phase)
		body, err = io.ReadAll(io.LimitReader(resp.Body, codexTurnStateAutoMaxBody))
		if err != nil || resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"type":"response.completed"`)) {
			t.Fatalf("phase=%s response did not complete successfully; http=%d", phase, resp.StatusCode)
		}
		s.relayOpenAICodexTurnState(c, account, resp.Header, resp.Request)
	}

	network.setPhase("initial_automatic_probe")
	started := time.Now()
	if s.autoTurnStateForAccount(context.Background(), snapshot(), model) != "" {
		t.Fatal("unexpected initial cached state")
	}
	t.Logf("initial_request_return_ms=%d", time.Since(started).Milliseconds())
	initial := usable("initial_automatic_probe")
	for _, phase := range []string{"real_request_1", "real_request_2"} {
		ordinary(phase)
		usable(phase)
	}

	// Exercise the renewal branch immediately by aging only local metadata.
	// The real token is never edited, re-signed or fabricated for the network.
	beforeRenew := codexTurnStateAutoToken(snapshot())
	age := time.Now().Add(-51 * time.Minute).UnixMilli()
	repo.mu.Lock()
	repo.accounts[a.ID].Extra[CodexTurnStateAutoSetAtExtraKey] = age
	repo.accounts[a.ID].Extra[CodexTurnStateAutoProbeAtExtraKey] = int64(0)
	repo.mu.Unlock()
	s.openaiTurnStateMu.Lock()
	s.openaiTurnStates[a.ID].setAt = age
	s.openaiTurnStates[a.ID].probeAt = 0
	s.openaiTurnStateMu.Unlock()
	network.setPhase("simulated_renewal_clock_real_probe")
	s.autoTurnStateForAccount(context.Background(), snapshot(), model)
	renewed := usable("simulated_renewal_clock_real_probe")
	t.Logf("renewal_changed=%t initial_changed=%t", codexTurnStateCanonicalDigest(beforeRenew) != codexTurnStateCanonicalDigest(renewed), codexTurnStateCanonicalDigest(initial) != codexTurnStateCanonicalDigest(renewed))

	if network.natural312Count() == 0 {
		// This local event is clearly labeled and never sent to OpenAI. The
		// ensuing replacement is obtained from the real upstream probe.
		network.setPhase("simulated_312_real_recovery_probe")
		s.collectOpenAICodexTurnState(context.Background(), snapshot(), testGlobalTurnStateToken(time.Now(), 11), renewed)
		if s.codexTurnStateAllowed(context.Background(), snapshot(), renewed) {
			t.Fatal("revoked real state remained eligible")
		}
		recovered := usable("simulated_312_real_recovery_probe")
		if codexTurnStateCanonicalDigest(recovered) == codexTurnStateCanonicalDigest(renewed) {
			t.Fatal("recovery reused revoked state")
		}
		t.Log("simulated_312_recovery_new_real_292=true")
	}
	ordinary("real_request_after_recovery")
	usable("final")
	t.Logf("natural_312_count=%d (zero means natural 312 recovery was NOT verified)", network.natural312Count())
}

func codexTurnStateLiveWait(t *testing.T, s *OpenAIGatewayService) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		s.openaiTurnStateMu.Lock()
		idle := s.openaiTurnStateWorkers == 0
		s.openaiTurnStateMu.Unlock()
		if idle {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("live turn-state worker did not finish within 30 seconds")
}

type codexTurnStateLiveHTTP struct {
	HTTPUpstream
	t           *testing.T
	client      *http.Client
	mu          sync.Mutex
	phase       string
	count       int
	signals     int
	credentials map[string]any
}

func (u *codexTurnStateLiveHTTP) setPhase(phase string) { u.mu.Lock(); u.phase = phase; u.mu.Unlock() }
func (u *codexTurnStateLiveHTTP) natural312Count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.signals
}
func (u *codexTurnStateLiveHTTP) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.count++
	n, phase := u.count, u.phase
	u.mu.Unlock()
	if n > 8 || proxy != "" || req.URL.Scheme != "https" || req.URL.Host != "chatgpt.com" || req.URL.Path != "/backend-api/codex/responses" {
		return nil, errors.New("live test request budget or destination guard")
	}
	started := time.Now()
	_, sentBlocks, _ := parseCodexTurnState(req.Header.Get(openAICodexTurnStateHeader))
	resp, err := u.client.Do(req)
	if err != nil {
		u.t.Logf("request=%d phase=%s transport_failed=true elapsed_ms=%d", n, phase, time.Since(started).Milliseconds())
		return nil, err
	}
	state := extractOpenAICodexTurnState(resp.Header)
	issued, blocks, parsed := parseCodexTurnState(state)
	if codexTurnStateIs312(state) {
		u.mu.Lock()
		u.signals++
		u.mu.Unlock()
	}
	u.t.Logf("request=%d phase=%s http=%d header_ms=%d sent_blocks=%d received_chars=%d received_blocks=%d parsed=%t issued_at=%s", n, phase, resp.StatusCode, time.Since(started).Milliseconds(), sentBlocks, len(state), blocks, parsed, issued.UTC().Format(time.RFC3339))
	resp.Body = &codexTurnStateLiveBody{ReadCloser: resp.Body, t: u.t, request: n, status: resp.StatusCode, credentials: u.credentials}
	return resp, nil
}

type codexTurnStateLiveBody struct {
	io.ReadCloser
	t           *testing.T
	request     int
	buf         bytes.Buffer
	closed      bool
	status      int
	credentials map[string]any
}

func (b *codexTurnStateLiveBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if remaining := codexTurnStateAutoMaxBody - b.buf.Len(); remaining > 0 {
		b.buf.Write(p[:min(n, remaining)])
	}
	return n, err
}
func (b *codexTurnStateLiveBody) Close() error {
	if !b.closed {
		b.closed = true
		if b.buf.Len() == 0 {
			_, _ = io.CopyN(io.Discard, b, codexTurnStateAutoMaxBody)
		}
		body := b.buf.Bytes()
		b.t.Logf("request=%d body_bytes=%d completed=%t failed_event=%t output_ok=%t", b.request, len(body), bytes.Contains(body, []byte(`"type":"response.completed"`)), bytes.Contains(body, []byte(`"type":"response.failed"`)), bytes.Contains(body, []byte(`"text":"OK"`)))
		if b.status >= 400 {
			var detail struct {
				Detail  string `json:"detail"`
				Message string `json:"message"`
				Error   struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal(body, &detail) == nil {
				message := detail.Detail + " " + detail.Message + " " + detail.Error.Message
				for _, raw := range b.credentials {
					if secret, ok := raw.(string); ok && secret != "" {
						message = strings.ReplaceAll(message, secret, "[redacted]")
					}
				}
				message = regexp.MustCompile(`[^\s]+@[^\s]+|[A-Za-z0-9_./+=:-]{32,}`).ReplaceAllString(message, "[redacted]")
				if len(message) > 300 {
					message = message[:300]
				}
				b.t.Logf("request=%d upstream_error_redacted=%q", b.request, strings.TrimSpace(message))
			}
		}
	}
	return b.ReadCloser.Close()
}
