package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/tidwall/gjson"
)

const (
	openAIWSTurnStateLifecycleGroupID  int64 = 9
	openAIWSTurnStateLifecycleAPIKeyID int64 = 21
)

func newOpenAIWSTurnStateLifecycleConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	return cfg
}

func enableOpenAIWSTurnStateLifecycleCollector(cfg *config.Config) {
	cfg.Gateway.CodexTurnState = config.GatewayCodexTurnStateConfig{
		ProbeTimeoutSeconds:   2,
		RefreshBeforeSeconds:  120,
		CooldownSeconds:       3,
		TTLSeconds:            600,
		ExpectedBlocks:        2,
		MaxEntries:            16,
		MaxTokenBytes:         2048,
		MaxProbeResponseBytes: 64 * 1024,
	}
}

func newOpenAIWSTurnStateLifecycleAccount(id int64) *Account {
	return &Account{
		ID:          id,
		Name:        "openai-ws-turn-state-lifecycle",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}
}

func openAIWSTurnStateLifecycleHandshake(value string) http.Header {
	header := make(http.Header)
	header.Set(openAIWSTurnStateHeader, value)
	return header
}

func newOpenAIWSTurnStateLifecycleService(
	t *testing.T,
	cfg *config.Config,
	dialer openAIWSClientDialer,
) (*OpenAIGatewayService, OpenAIWSStateStore) {
	t.Helper()
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	t.Cleanup(pool.Close)
	store := NewOpenAIWSStateStore(nil)
	fallback := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_http_fallback","model":"gpt-5.1","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}`,
		)),
	}}
	return &OpenAIGatewayService{
		cfg:                cfg,
		httpUpstream:       fallback,
		cache:              &stubGatewayCache{},
		openaiWSResolver:   NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:      NewCodexToolCorrector(),
		openaiWSPool:       pool,
		openaiWSStateStore: store,
	}, store
}

func newOpenAIWSTurnStateLifecycleContext(
	t *testing.T,
	writer http.ResponseWriter,
	requestContext context.Context,
	sessionID string,
) *gin.Context {
	t.Helper()
	if writer == nil {
		writer = httptest.NewRecorder()
	}
	c, _ := gin.CreateTestContext(writer)
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	if requestContext != nil {
		req = req.WithContext(requestContext)
	}
	req.Header.Set("session_id", sessionID)
	c.Request = req
	groupID := openAIWSTurnStateLifecycleGroupID
	c.Set("api_key", &APIKey{ID: openAIWSTurnStateLifecycleAPIKeyID, GroupID: &groupID})
	return c
}

type openAIWSTurnStateSequenceDialer struct {
	mu         sync.Mutex
	conns      []openAIWSClientConn
	handshakes []http.Header
	headers    []http.Header
	proxies    []string
}

func (d *openAIWSTurnStateSequenceDialer) Dial(
	_ context.Context,
	_ string,
	headers http.Header,
	proxyURL string,
) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.headers = append(d.headers, cloneHeader(headers))
	d.proxies = append(d.proxies, proxyURL)
	if len(d.conns) == 0 {
		return nil, http.StatusServiceUnavailable, nil, errors.New("no test websocket connection")
	}
	conn := d.conns[0]
	d.conns = d.conns[1:]
	var handshake http.Header
	if len(d.handshakes) > 0 {
		handshake = cloneHeader(d.handshakes[0])
		d.handshakes = d.handshakes[1:]
	}
	return conn, 0, handshake, nil
}

func (d *openAIWSTurnStateSequenceDialer) Headers() []http.Header {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]http.Header, len(d.headers))
	for i := range d.headers {
		result[i] = cloneHeader(d.headers[i])
	}
	return result
}

func (d *openAIWSTurnStateSequenceDialer) Proxies() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.proxies...)
}

func requireOpenAICodexWSTicketBodyIsolated(t *testing.T, body map[string]any) {
	t.Helper()
	require.NotContains(t, body, "prompt_cache_key")
	require.NotContains(t, body, "device_id")
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	// The writer may add its own timing marker under client_metadata.
	require.False(t, gjson.GetBytes(raw, "client_metadata.old_marker").Exists())
	require.False(t, gjson.GetBytes(raw, "client_metadata.session_id").Exists())
}

type openAIWSTurnStateWriteFailConn struct{}

func (*openAIWSTurnStateWriteFailConn) WriteJSON(context.Context, any) error {
	return errors.New("test websocket write failed")
}

func (*openAIWSTurnStateWriteFailConn) ReadMessage(context.Context) ([]byte, error) {
	return nil, io.EOF
}

func (*openAIWSTurnStateWriteFailConn) Ping(context.Context) error { return nil }

func (*openAIWSTurnStateWriteFailConn) Close() error { return nil }

type openAIWSTurnStateWriteErrorResponseWriter struct {
	header http.Header
}

func (w *openAIWSTurnStateWriteErrorResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (*openAIWSTurnStateWriteErrorResponseWriter) WriteHeader(int) {}

func (*openAIWSTurnStateWriteErrorResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("test downstream write failed")
}

func TestOpenAIGatewayService_Forward_WSv2_HandshakeTurnStateCommitGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		eventType  string
		wantStored bool
	}{
		{name: "completed", eventType: "response.completed", wantStored: true},
		{name: "done", eventType: "response.done", wantStored: true},
		{name: "failed", eventType: "response.failed", wantStored: false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			handshakeState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, byte(80+index))
			model := "gpt-5.5"
			event := []byte(fmt.Sprintf(
				`{"type":%q,"response":{"id":%q,"model":%q,"status":%q,"usage":{"input_tokens":1,"output_tokens":1}}}`,
				test.eventType,
				fmt.Sprintf("resp_commit_gate_%d", index),
				model,
				strings.TrimPrefix(test.eventType, "response."),
			))
			conn := &openAIWSCaptureConn{events: [][]byte{event}}
			handshake := openAIWSTurnStateLifecycleHandshake(handshakeState)
			dialer := &openAIWSTurnStateSequenceDialer{
				conns:      []openAIWSClientConn{conn},
				handshakes: []http.Header{handshake},
			}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8800 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"input_text","text":"hello"}]}`)
			c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "commit-gate-"+test.name)

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)

			scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
			state, stored := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, model)
			require.Equal(t, test.wantStored, stored)
			if test.wantStored {
				require.Equal(t, handshakeState, state)
			}
		})
	}
}

func TestOpenAIGatewayService_Forward_WSv2_CollectorTicketRotation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for index, test := range []struct {
		name           string
		stream         bool
		eventType      string
		stale          bool
		ticketIdentity bool
	}{
		{name: "completed_non_stream", eventType: "response.completed"},
		{name: "completed_stream", stream: true, eventType: "response.completed"},
		{name: "completed_ticket_identity", eventType: "response.completed", ticketIdentity: true},
		{name: "failed", eventType: "response.failed"},
		{name: "stale_completed", eventType: "response.completed", stale: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			now := time.Now().UTC()
			active := collectorTestToken(t, now.Add(-2*time.Minute), 2, byte(150+index*3))
			handshake := collectorTestToken(t, now.Add(-time.Minute), 2, byte(151+index*3))
			replacement := collectorTestToken(t, now.Add(-30*time.Second), 2, byte(152+index*3))
			event := fmt.Sprintf(`{"type":%q,"response":{"id":"resp_ticket_rotation","model":"gpt-5.5","status":%q,"usage":{"input_tokens":1,"output_tokens":1}}}`,
				test.eventType, strings.TrimPrefix(test.eventType, "response."))
			var conn openAIWSClientConn = &openAIWSCaptureConn{events: [][]byte{[]byte(event)}}
			var gated *openAIWSGatedConn
			if test.stale {
				gated = newOpenAIWSGatedConn(event)
				conn = gated
			}
			dialer := &openAIWSTurnStateSequenceDialer{
				conns: []openAIWSClientConn{conn}, handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(handshake)},
			}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8920 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			body := []byte(fmt.Sprintf(`{"model":"gpt-5.5","stream":%t,"input":"hello"}`, test.stream))
			if test.ticketIdentity {
				body = []byte(`{"model":"gpt-5.5","stream":false,"input":"hello","prompt_cache_key":"old-cache","client_metadata":{"old_marker":"client","session_id":"foreign-session"},"device_id":"old-device"}`)
			}
			c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "rotation-v2-"+test.name)
			scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
			require.NotEmpty(t, scope)
			bindOpenAICodexTurnStateExecutionScopeValue(c, scope)
			key := svc.codexTurnStateKey(c, account, "gpt-5.5")
			if test.ticketIdentity {
				token, err := ValidateOpenAICodexTurnState(active, svc.codexTurnStatePolicy(), now)
				require.NoError(t, err)
				require.True(t, svc.codexTurnStateCollector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
					Token: token, Route: "seed", HarvestSessionID: "harvest-session-a",
				}, now))
			} else {
				require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, active, "seed", now))
			}

			forward := func() error {
				result, err := svc.Forward(context.Background(), c, account, body)
				if err == nil && result == nil {
					return errors.New("nil forwarding result")
				}
				return err
			}
			if test.stale {
				outcomeCh := make(chan error, 1)
				go func() { outcomeCh <- forward() }()
				select {
				case <-gated.sent:
				case <-time.After(3 * time.Second):
					close(gated.gate)
					t.Fatal("timed out waiting for the in-flight request")
				}
				issued, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
				require.True(t, ok)
				require.Equal(t, active, issued.Token.Value)
				require.True(t, svc.codexTurnStateCollector.ObserveQualified(key, replacement, issued, nil, time.Now()))
				close(gated.gate)
				select {
				case err := <-outcomeCh:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("timed out waiting for the stale response")
				}
			} else {
				require.NoError(t, forward())
			}
			headers := dialer.Headers()
			require.Len(t, headers, 1)
			require.Equal(t, active, headers[0].Get(openAIWSTurnStateHeader))
			if test.ticketIdentity {
				capture := conn.(*openAIWSCaptureConn)
				requireOpenAICodexWSTicketBodyIsolated(t, capture.lastWrite)
				require.Equal(t, "harvest-session-a", headers[0].Get("session_id"))
			}
			current, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
			require.True(t, ok)
			wantState := active
			if test.stale {
				wantState = replacement
			} else if test.eventType == "response.completed" {
				wantState = handshake
			}
			require.Equal(t, wantState, current.Token.Value)
			stored, ok := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
			if test.eventType == "response.completed" && !test.stale {
				require.True(t, ok)
				require.Equal(t, handshake, stored)
			} else {
				require.False(t, ok, "a failed or stale response must not commit its handshake state")
			}
		})
	}
}

func TestOpenAIGatewayService_Forward_WSv2_ConsumesHandshakeEvidenceOncePerConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for index, test := range []struct {
		name             string
		firstEvent       string
		handshakeRotates bool
	}{
		{name: "completed_then_completed"},
		{name: "failed_then_completed", firstEvent: "response.failed", handshakeRotates: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			now := time.Now().UTC()
			active := collectorTestToken(t, now.Add(-2*time.Minute), 2, byte(205+index*3))
			handshakeState := active
			if test.handshakeRotates {
				handshakeState = collectorTestToken(t, now.Add(-time.Minute), 2, byte(206+index*3))
			}
			firstEvent := test.firstEvent
			if firstEvent == "" {
				firstEvent = "response.completed"
			}
			conn := &openAIWSCaptureConn{events: [][]byte{
				[]byte(fmt.Sprintf(`{"type":%q,"response":{"id":"resp_handshake_evidence_1","model":"gpt-5.5","status":%q,"usage":{"input_tokens":1,"output_tokens":1}}}`,
					firstEvent, strings.TrimPrefix(firstEvent, "response."))),
				[]byte(`{"type":"response.completed","response":{"id":"resp_handshake_evidence_2","model":"gpt-5.5","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`),
			}}
			handshake := openAIWSTurnStateLifecycleHandshake(handshakeState)
			handshake.Add("Set-Cookie", "session=stable-cookie; Path=/; HttpOnly")
			dialer := &openAIWSTurnStateSequenceDialer{
				conns: []openAIWSClientConn{conn}, handshakes: []http.Header{handshake},
			}
			svc, _ := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8980 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			body := []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`)
			sessionID := "handshake-evidence-" + test.name
			seedContext := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, sessionID)
			scope, _ := resolveOpenAIWSExecutionScope(seedContext, body, openAIWSTurnStateLifecycleAPIKeyID)
			bindOpenAICodexTurnStateExecutionScopeValue(seedContext, scope)
			key := svc.codexTurnStateKey(seedContext, account, "gpt-5.5")
			token, err := ValidateOpenAICodexTurnState(active, svc.codexTurnStatePolicy(), now)
			require.NoError(t, err)
			initialCookieAt := now.Add(-time.Minute)
			require.True(t, svc.codexTurnStateCollector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
				Token: token, Route: "seed", HarvestSessionID: "harvest-session",
				HarvestCookies: []OpenAICodexTurnStateCookie{{Name: "session", Value: "stable-cookie"}}, HarvestCookiesAt: initialCookieAt,
			}, now))
			initial, ok := svc.codexTurnStateCollector.Acquire(key, now)
			require.True(t, ok)

			forward := func() {
				c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, sessionID)
				result, forwardErr := svc.Forward(context.Background(), c, account, body)
				require.NoError(t, forwardErr)
				require.NotNil(t, result)
			}
			forward()
			afterFirst, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
			require.True(t, ok)
			if firstEvent == "response.failed" {
				require.Equal(t, initial.Version, afterFirst.Version, "a failed turn must not consume or apply handshake evidence")
				require.Equal(t, initialCookieAt, afterFirst.HarvestCookiesAt)
			} else {
				require.Greater(t, afterFirst.Version, initial.Version)
				require.True(t, afterFirst.HarvestCookiesAt.After(initialCookieAt))
			}

			forward()
			afterSecond, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
			require.True(t, ok)
			require.Len(t, dialer.Headers(), 1, "both downstream requests must reuse the same upstream connection")
			if firstEvent == "response.failed" {
				require.Equal(t, handshakeState, afterSecond.Token.Value)
				require.Greater(t, afterSecond.Version, afterFirst.Version)
				require.True(t, afterSecond.HarvestCookiesAt.After(initialCookieAt))
			} else {
				require.Equal(t, afterFirst.Version, afterSecond.Version, "immutable handshake evidence must not advance the collector twice")
				require.Equal(t, afterFirst.HarvestCookiesAt, afterSecond.HarvestCookiesAt, "reused Set-Cookie must not refresh cookie age")
			}
		})
	}
}

func TestResolveOpenAIWSCodexTurnStatePreservesMatchingHarvestedTicket(t *testing.T) {
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	svc, _ := newOpenAIWSTurnStateLifecycleService(t, cfg, &openAIWSTurnStateSequenceDialer{})
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8990)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`)
	c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "matching-harvested-ticket")
	scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
	bindOpenAICodexTurnStateExecutionScopeValue(c, scope)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 219)
	token, err := ValidateOpenAICodexTurnState(state, svc.codexTurnStatePolicy(), now)
	require.NoError(t, err)
	require.True(t, svc.codexTurnStateCollector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
		Token: token, Route: "probe", HarvestSessionID: "harvested-session",
		HarvestCookies: []OpenAICodexTurnStateCookie{{Name: "session", Value: "harvested-cookie"}}, HarvestCookiesAt: now,
		EgressPinned: true, EgressProxyURL: "http://127.0.0.1:8990",
	}, now))

	resolved := svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, "gpt-5.5", state, false)
	require.Equal(t, state, resolved)
	binding, bound := svc.codexTurnStateBinding(c, account, "gpt-5.5")
	require.True(t, bound)
	require.True(t, binding.used)
	require.Equal(t, "harvested-session", binding.snapshot.HarvestSessionID)

	headers := http.Header{"Session-Id": []string{"foreign-session"}, "Cookie": []string{"foreign=cookie"}}
	proxyURL := svc.pinOpenAICodexTurnStateWSIdentity(c, account, "gpt-5.5", headers, "http://127.0.0.1:8000")
	require.Equal(t, "http://127.0.0.1:8990", proxyURL)
	require.Equal(t, "harvested-session", headers.Get("session_id"))
	require.Equal(t, "session=harvested-cookie", headers.Get("Cookie"))
	require.Empty(t, headers.Get("session-id"))
}

func TestOpenAIGatewayService_Forward_WSv2_AdminDisabledLateBindDoesNotCommitHandshakeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	handshakeState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 89)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.completed","response":{"id":"resp_admin_disabled_v2","model":"gpt-5.5","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)}}
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{conn},
		handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(handshakeState)},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8809)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	current := *account
	current.Schedulable = false
	svc.accountRepo = &openAIWSTurnStateAuthoritativeRepo{current: &current}
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"input_text","text":"hello"}]}`)
	recorder := httptest.NewRecorder()
	c := newOpenAIWSTurnStateLifecycleContext(t, recorder, nil, "late-bind-disabled-v2")

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	scope, _ := boundOpenAICodexTurnStateExecutionScope(c)
	_, stored := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
	require.False(t, stored)
	require.Empty(t, recorder.Header().Get(openAIWSTurnStateHeader), "an old identity's handshake state must not be exposed after admin disable")
}

func TestOpenAIGatewayService_Forward_WSv2_TurnStateIsolatedByFinalModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		model     string
		upstream  string
		wantState bool
	}{
		{name: "same_final_model", model: "client-a", upstream: "upstream-a", wantState: true},
		{name: "different_final_model", model: "client-b", upstream: "upstream-b", wantState: false},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			storedState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, byte(90+index))
			conn := &openAIWSCaptureConn{events: [][]byte{[]byte(fmt.Sprintf(
				`{"type":"response.completed","response":{"id":%q,"model":%q,"usage":{"input_tokens":1,"output_tokens":1}}}`,
				fmt.Sprintf("resp_model_isolation_%d", index),
				test.upstream,
			))}}
			dialer := &openAIWSTurnStateSequenceDialer{conns: []openAIWSClientConn{conn}}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8810 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			account.Credentials["model_mapping"] = map[string]any{
				"client-a": "upstream-a",
				"client-b": "upstream-b",
			}
			body := []byte(fmt.Sprintf(
				`{"model":%q,"stream":false,"input":[{"type":"input_text","text":"hello"}]}`,
				test.model,
			))
			c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "final-model-isolation")
			scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
			require.True(t, svc.bindOpenAIWSSessionTurnStateIfRefreshNeeded(
				c,
				store,
				openAIWSTurnStateLifecycleGroupID,
				account,
				scope,
				"upstream-a",
				storedState,
			))

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			headers := dialer.Headers()
			require.Len(t, headers, 1)
			if test.wantState {
				require.Equal(t, storedState, headers[0].Get(openAIWSTurnStateHeader))
			} else {
				require.Empty(t, headers[0].Get(openAIWSTurnStateHeader))
			}
		})
	}
}

func TestOpenAIGatewayService_Forward_WSv2_InjectionDisabledIgnoresStoreAndStripsUnprovenNativeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	tests := []struct {
		name        string
		nativeState string
		wantState   string
	}{
		{name: "stored_state_is_not_injected"},
		{
			name:        "unproven_native_state_is_stripped",
			nativeState: collectorTestToken(t, now.Add(-time.Minute), 2, 22),
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			conn := &openAIWSCaptureConn{events: [][]byte{[]byte(fmt.Sprintf(
				`{"type":"response.completed","response":{"id":%q,"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`,
				fmt.Sprintf("resp_disabled_v2_%d", index),
			))}}
			dialer := &openAIWSTurnStateSequenceDialer{conns: []openAIWSClientConn{conn}}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.SetCodexTurnStateRuntimeSettings(false, false)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8870 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			body := []byte(`{"model":"gpt-5.5","stream":false,"input":"hello"}`)
			c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "disabled-v2-"+test.name)
			scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
			storedState := collectorTestToken(t, now.Add(-2*time.Minute), 2, byte(30+index))
			store.BindSessionTurnState(
				openAIWSTurnStateLifecycleGroupID,
				account.ID,
				scope,
				storedState,
				time.Minute,
				"gpt-5.5",
			)
			if test.nativeState != "" {
				c.Request.Header.Set(openAIWSTurnStateHeader, test.nativeState)
			}

			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			headers := dialer.Headers()
			require.Len(t, headers, 1)
			require.Equal(t, test.wantState, headers[0].Get(openAIWSTurnStateHeader))
		})
	}
}

func TestOpenAIGatewayService_Forward_WSv2_NoReliableScopeDoesNotUseFallbackTurnStateStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	now := time.Now().UTC()
	storedState := collectorTestToken(t, now.Add(-2*time.Minute), 2, 40)
	handshakeState := collectorTestToken(t, now.Add(-time.Minute), 2, 41)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.completed","response":{"id":"resp_no_scope_v2","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)}}
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{conn},
		handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(handshakeState)},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8880)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "")
	const fallbackPromptCacheKey = "fingerprint-fallback-only"
	fallbackHash, _ := openAIWSSessionHashesFromID(fallbackPromptCacheKey)
	require.NotEmpty(t, fallbackHash)
	store.BindSessionTurnState(
		openAIWSTurnStateLifecycleGroupID,
		account.ID,
		fallbackHash,
		storedState,
		time.Minute,
		"gpt-5.5",
	)

	result, err := svc.forwardOpenAIWSV2(
		context.Background(),
		c,
		account,
		map[string]any{"model": "gpt-5.5", "stream": false, "input": "hello"},
		fallbackPromptCacheKey,
		"",
		"test-access-token",
		svc.getOpenAIWSProtocolResolver().Resolve(account),
		false,
		false,
		"gpt-5.5",
		"gpt-5.5",
		time.Now(),
		1,
		"",
		new(bool),
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	headers := dialer.Headers()
	require.Len(t, headers, 1)
	require.Empty(t, headers[0].Get(openAIWSTurnStateHeader), "fallback session hash must not be read as turn state")
	remaining, ok := store.GetSessionTurnState(
		openAIWSTurnStateLifecycleGroupID,
		account.ID,
		fallbackHash,
		"gpt-5.5",
	)
	require.True(t, ok)
	require.Equal(t, storedState, remaining, "handshake state must not overwrite a fallback session hash")
}

func TestOpenAIGatewayService_Forward_WSv2_InvalidStoredStateFallsBackToCollector(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.completed","response":{"id":"resp_invalid_store_fallback","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)}}
	dialer := &openAIWSTurnStateSequenceDialer{conns: []openAIWSClientConn{conn}}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8819)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"input_text","text":"hello"}]}`)
	c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "invalid-store-fallback")
	scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
	bindOpenAICodexTurnStateExecutionScopeValue(c, scope)
	store.BindSessionTurnState(
		openAIWSTurnStateLifecycleGroupID,
		account.ID,
		scope,
		"invalid-stored-state",
		time.Minute,
		"gpt-5.5",
	)
	cached := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 8)
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(svc.codexTurnStateKey(c, account, "gpt-5.5"), cached, "cached", time.Now()))

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	headers := dialer.Headers()
	require.Len(t, headers, 1)
	require.Equal(t, cached, headers[0].Get(openAIWSTurnStateHeader), "an invalid store entry must not suppress a usable collector state")
}

func TestOpenAIGatewayService_Forward_WSv2_InvalidationDuringFlightDoesNotRepublishState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 9)
	gated := newOpenAIWSGatedConn(`{"type":"response.completed","response":{"id":"resp_stale_generation","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`)
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{gated},
		handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(state)},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8829)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"input_text","text":"hello"}]}`)
	recorder := httptest.NewRecorder()
	c := newOpenAIWSTurnStateLifecycleContext(t, recorder, nil, "stale-generation")

	type forwardOutcome struct {
		result *OpenAIForwardResult
		err    error
	}
	outcomeCh := make(chan forwardOutcome, 1)
	go func() {
		result, err := svc.Forward(context.Background(), c, account, body)
		outcomeCh <- forwardOutcome{result: result, err: err}
	}()

	select {
	case <-gated.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for the in-flight websocket write")
	}
	svc.InvalidateOpenAIAccountRuntimeState(account.ID)
	close(gated.gate)

	select {
	case outcome := <-outcomeCh:
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the stale-generation websocket response")
	}
	scope, _ := boundOpenAICodexTurnStateExecutionScope(c)
	require.NotEmpty(t, scope)
	_, stored := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
	require.False(t, stored)
	require.Empty(t, recorder.Header().Get(openAIWSTurnStateHeader), "a stale generation must not expose its handshake state downstream")
}

func TestOpenAIGatewayService_Forward_WSv2_DoesNotCommitHandshakeStateOnFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		configure func(*config.Config)
		conn      func() openAIWSClientConn
	}{
		{
			name: "write_request",
			conn: func() openAIWSClientConn {
				return &openAIWSTurnStateWriteFailConn{}
			},
		},
		{
			name: "prewarm",
			configure: func(cfg *config.Config) {
				cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
			},
			conn: func() openAIWSClientConn {
				return &openAIWSCaptureConn{events: [][]byte{[]byte(
					`{"type":"error","error":{"type":"server_error","code":"server_error","message":"prewarm failed"}}`,
				)}}
			},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			cfg.Gateway.OpenAIWS.RetryBackoffInitialMS = 1
			cfg.Gateway.OpenAIWS.RetryBackoffMaxMS = 1
			cfg.Gateway.OpenAIWS.RetryJitterRatio = 0
			cfg.Gateway.OpenAIWS.RetryTotalBudgetMS = 1
			if test.configure != nil {
				test.configure(cfg)
			}
			handshakeState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, byte(110+index))
			handshake := openAIWSTurnStateLifecycleHandshake(handshakeState)
			dialer := &openAIWSTurnStateSequenceDialer{
				conns:      []openAIWSClientConn{test.conn()},
				handshakes: []http.Header{handshake},
			}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8820 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"input_text","text":"hello"}]}`)
			c := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "failure-"+test.name)

			_, _ = svc.Forward(context.Background(), c, account, body)

			scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
			_, stored := store.GetSessionTurnState(
				openAIWSTurnStateLifecycleGroupID,
				account.ID,
				scope,
				"gpt-5.5",
			)
			require.False(t, stored)
			require.NotEmpty(t, dialer.Headers(), "the websocket failure path must be exercised")
		})
	}
}

func TestOpenAIGatewayService_Forward_WSv2_FinalWriteFailureDoesNotCommitHandshakeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name      string
		newWriter func(context.CancelFunc) http.ResponseWriter
	}{
		{
			name: "context_canceled_during_write",
			newWriter: func(cancel context.CancelFunc) http.ResponseWriter {
				return &cancelOnFirstWriteResponseWriter{cancel: cancel}
			},
		},
		{
			name: "writer_returns_error_without_cancel",
			newWriter: func(context.CancelFunc) http.ResponseWriter {
				return &openAIWSTurnStateWriteErrorResponseWriter{}
			},
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			handshakeState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, byte(120+index))
			conn := &openAIWSCaptureConn{events: [][]byte{[]byte(fmt.Sprintf(
				`{"type":"response.completed","response":{"id":%q,"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`,
				fmt.Sprintf("resp_final_write_%d", index),
			))}}
			dialer := &openAIWSTurnStateSequenceDialer{
				conns:      []openAIWSClientConn{conn},
				handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(handshakeState)},
			}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8830 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			body := []byte(`{"model":"gpt-5.5","stream":false,"input":[{"type":"input_text","text":"hello"}]}`)
			c, _ := gin.CreateTestContext(test.newWriter(cancel))
			c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil).WithContext(ctx)
			c.Request.Header.Set("session_id", "final-write-"+test.name)
			groupID := openAIWSTurnStateLifecycleGroupID
			c.Set("api_key", &APIKey{ID: openAIWSTurnStateLifecycleAPIKeyID, GroupID: &groupID})

			result, err := svc.Forward(ctx, c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.ClientDisconnect)

			scope, _ := resolveOpenAIWSExecutionScope(c, body, openAIWSTurnStateLifecycleAPIKeyID)
			_, stored := store.GetSessionTurnState(groupID, account.ID, scope, "gpt-5.5")
			require.False(t, stored)
		})
	}
}

func startOpenAIWSTurnStateIngressServer(
	t *testing.T,
	svc *OpenAIGatewayService,
	account *Account,
	hooks ...*OpenAIWSIngressHooks,
) (*httptest.Server, <-chan error, <-chan string) {
	t.Helper()
	serverErrCh := make(chan error, 1)
	scopeCh := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErrCh <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		recorder := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(recorder)
		req := r.Clone(r.Context())
		req.Header = req.Header.Clone()
		req.Header.Set("User-Agent", "unit-test-agent/1.0")
		ginCtx.Request = req
		groupID := openAIWSTurnStateLifecycleGroupID
		ginCtx.Set("api_key", &APIKey{ID: openAIWSTurnStateLifecycleAPIKeyID, GroupID: &groupID})
		readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		_, firstMessage, readErr := conn.Read(readCtx)
		cancel()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		var ingressHooks *OpenAIWSIngressHooks
		if len(hooks) > 0 {
			ingressHooks = hooks[0]
		}
		proxyErr := svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, ingressHooks)
		scope, _ := boundOpenAICodexTurnStateExecutionScope(ginCtx)
		scopeCh <- scope
		serverErrCh <- proxyErr
	}))
	t.Cleanup(server.Close)
	return server, serverErrCh, scopeCh
}

func dialOpenAIWSTurnStateIngressClient(t *testing.T, serverURL, sessionID string) *coderws.Conn {
	return dialOpenAIWSTurnStateIngressClientWithState(t, serverURL, sessionID, "")
}

func dialOpenAIWSTurnStateIngressClientWithState(t *testing.T, serverURL, sessionID, turnState string) *coderws.Conn {
	t.Helper()
	header := make(http.Header)
	header.Set("session-id", sessionID)
	if turnState != "" {
		header.Set(openAIWSTurnStateHeader, turnState)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(serverURL, "http"), &coderws.DialOptions{HTTPHeader: header})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func TestOpenAIGatewayService_Ingress_InjectionDisabledIgnoresStoreAndStripsUnprovenNativeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	tests := []struct {
		name        string
		nativeState string
		wantState   string
	}{
		{name: "stored_state_is_not_injected"},
		{
			name:        "unproven_native_state_is_stripped",
			nativeState: collectorTestToken(t, now.Add(-time.Minute), 2, 51),
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			conn := &openAIWSCaptureConn{events: [][]byte{[]byte(fmt.Sprintf(
				`{"type":"response.completed","response":{"id":%q,"model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`,
				fmt.Sprintf("resp_disabled_ingress_%d", index),
			))}}
			dialer := &openAIWSTurnStateSequenceDialer{conns: []openAIWSClientConn{conn}}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.SetCodexTurnStateRuntimeSettings(false, false)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8890 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			body := `{"type":"response.create","model":"gpt-5.5","stream":false,"input":"hello"}`
			sessionID := "disabled-ingress-" + test.name
			scopeContext := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, sessionID)
			scope, _ := resolveOpenAIWSExecutionScope(scopeContext, []byte(body), openAIWSTurnStateLifecycleAPIKeyID)
			store.BindSessionTurnState(
				openAIWSTurnStateLifecycleGroupID,
				account.ID,
				scope,
				collectorTestToken(t, now.Add(-2*time.Minute), 2, byte(60+index)),
				time.Minute,
				"gpt-5.5",
			)
			server, serverErrCh, _ := startOpenAIWSTurnStateIngressServer(t, svc, account)
			client := dialOpenAIWSTurnStateIngressClientWithState(t, server.URL, sessionID, test.nativeState)
			writeOpenAIWSTurnStateIngressMessage(t, client, body)
			completed := readOpenAIWSTurnStateIngressMessage(t, client)
			require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			select {
			case err := <-serverErrCh:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for ingress websocket to finish")
			}
			headers := dialer.Headers()
			require.Len(t, headers, 1)
			require.Equal(t, test.wantState, headers[0].Get(openAIWSTurnStateHeader))
		})
	}
}

func TestOpenAIGatewayService_Ingress_AdminDisabledLateBindDoesNotCommitHandshakeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	handshakeState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 69)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.completed","response":{"id":"resp_admin_disabled_ingress","model":"gpt-5.5","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)}}
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{conn},
		handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(handshakeState)},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8899)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	current := *account
	current.Schedulable = false
	svc.accountRepo = &openAIWSTurnStateAuthoritativeRepo{current: &current}
	body := `{"type":"response.create","model":"gpt-5.5","stream":false,"input":"hello"}`
	sessionID := "late-bind-disabled-ingress"
	server, serverErrCh, scopeCh := startOpenAIWSTurnStateIngressServer(t, svc, account)
	client := dialOpenAIWSTurnStateIngressClient(t, server.URL, sessionID)

	writeOpenAIWSTurnStateIngressMessage(t, client, body)
	completed := readOpenAIWSTurnStateIngressMessage(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-serverErrCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress websocket to finish")
	}
	scope := <-scopeCh
	_, stored := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
	require.False(t, stored)
}

func TestOpenAIGatewayService_Ingress_NoReliableScopeDoesNotUseFallbackTurnStateStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	now := time.Now().UTC()
	storedState := collectorTestToken(t, now.Add(-2*time.Minute), 2, 70)
	handshakeState := collectorTestToken(t, now.Add(-time.Minute), 2, 71)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.completed","response":{"id":"resp_no_scope_ingress","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)}}
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{conn},
		handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(handshakeState)},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8900)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	body := `{"type":"response.create","model":"gpt-5.5","stream":false,"input":"fallback content"}`
	scopeContext := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, "")
	fallbackHash := svc.GenerateSessionHash(scopeContext, []byte(body))
	require.NotEmpty(t, fallbackHash)
	scope, _ := resolveOpenAIWSExecutionScope(scopeContext, []byte(body), openAIWSTurnStateLifecycleAPIKeyID)
	require.Empty(t, scope)
	store.BindSessionTurnState(
		openAIWSTurnStateLifecycleGroupID,
		account.ID,
		fallbackHash,
		storedState,
		time.Minute,
		"gpt-5.5",
	)
	server, serverErrCh, scopeCh := startOpenAIWSTurnStateIngressServer(t, svc, account)
	client := dialOpenAIWSTurnStateIngressClient(t, server.URL, "")
	writeOpenAIWSTurnStateIngressMessage(t, client, body)
	completed := readOpenAIWSTurnStateIngressMessage(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-serverErrCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress websocket to finish")
	}
	require.Empty(t, <-scopeCh)
	headers := dialer.Headers()
	require.Len(t, headers, 1)
	require.Empty(t, headers[0].Get(openAIWSTurnStateHeader), "content-derived session hash must not be read as turn state")
	remaining, ok := store.GetSessionTurnState(
		openAIWSTurnStateLifecycleGroupID,
		account.ID,
		fallbackHash,
		"gpt-5.5",
	)
	require.True(t, ok)
	require.Equal(t, storedState, remaining, "handshake state must not overwrite a content-derived session hash")
}

func writeOpenAIWSTurnStateIngressMessage(t *testing.T, conn *coderws.Conn, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(payload)))
}

func readOpenAIWSTurnStateIngressMessage(t *testing.T, conn *coderws.Conn) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, payload, err := conn.Read(ctx)
	require.NoError(t, err)
	return payload
}

func TestOpenAIGatewayService_IngressTurnStateWriteRetryDoesNotCommitStaleHandshake(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	secondConn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.completed","response":{"id":"resp_ingress_retry","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)}}
	firstHandshake := openAIWSTurnStateLifecycleHandshake(
		collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 130),
	)
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{&openAIWSTurnStateWriteFailConn{}, secondConn},
		handshakes: []http.Header{firstHandshake, nil},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8840)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	server, serverErrCh, scopeCh := startOpenAIWSTurnStateIngressServer(t, svc, account)
	client := dialOpenAIWSTurnStateIngressClient(t, server.URL, "ingress-write-retry")
	writeOpenAIWSTurnStateIngressMessage(t, client, `{"type":"response.create","model":"gpt-5.5","stream":false,"input":"hello"}`)
	completed := readOpenAIWSTurnStateIngressMessage(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))

	select {
	case err := <-serverErrCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress websocket to finish")
	}
	scope := <-scopeCh
	require.NotEmpty(t, scope)
	_, stored := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
	require.False(t, stored)
	headers := dialer.Headers()
	require.Len(t, headers, 2)
	require.Empty(t, headers[1].Get(openAIWSTurnStateHeader), "an unconfirmed state must not cross into the retry lease")
}

func TestOpenAIGatewayService_IngressResponseFailedDoesNotCommitHandshakeState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.failed","response":{"id":"resp_ingress_failed","model":"gpt-5.5","status":"failed","error":{"code":"server_error","message":"failed"}}}`,
	)}}
	handshake := openAIWSTurnStateLifecycleHandshake(
		collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 131),
	)
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{conn},
		handshakes: []http.Header{handshake},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8850)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	server, serverErrCh, scopeCh := startOpenAIWSTurnStateIngressServer(t, svc, account)
	client := dialOpenAIWSTurnStateIngressClient(t, server.URL, "ingress-response-failed")
	writeOpenAIWSTurnStateIngressMessage(t, client, `{"type":"response.create","model":"gpt-5.5","stream":false,"input":"hello"}`)
	failed := readOpenAIWSTurnStateIngressMessage(t, client)
	require.Equal(t, "response.failed", gjson.GetBytes(failed, "type").String())
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))

	select {
	case err := <-serverErrCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress websocket to finish")
	}
	scope := <-scopeCh
	require.NotEmpty(t, scope)
	_, stored := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
	require.False(t, stored)
}

func TestOpenAIGatewayService_IngressFailedTurnPreservesHandshakeEvidenceForLaterSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := newOpenAIWSTurnStateLifecycleConfig()
	enableOpenAIWSTurnStateLifecycleCollector(cfg)
	now := time.Now().UTC()
	active := collectorTestToken(t, now.Add(-2*time.Minute), 2, 225)
	handshakeState := collectorTestToken(t, now.Add(-time.Minute), 2, 226)
	conn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.failed","response":{"id":"resp_ingress_evidence_failed","model":"gpt-5.5","status":"failed","error":{"code":"server_error","message":"failed"}}}`),
		[]byte(`{"type":"response.completed","response":{"id":"resp_ingress_evidence_completed","model":"gpt-5.5","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	handshake := openAIWSTurnStateLifecycleHandshake(handshakeState)
	handshake.Add("Set-Cookie", "session=stable-cookie; Path=/; HttpOnly")
	dialer := &openAIWSTurnStateSequenceDialer{
		conns: []openAIWSClientConn{conn}, handshakes: []http.Header{handshake},
	}
	svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
	svc.initCodexTurnStateCollector()
	account := codexTurnStateGatewayTestAccount(8991)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
	const sessionID = "ingress-failed-then-success-evidence"
	body := []byte(`{"type":"response.create","model":"gpt-5.5","stream":false,"input":"hello"}`)
	seedContext := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, sessionID)
	scope, _ := resolveOpenAIWSExecutionScope(seedContext, body, openAIWSTurnStateLifecycleAPIKeyID)
	bindOpenAICodexTurnStateExecutionScopeValue(seedContext, scope)
	key := svc.codexTurnStateKey(seedContext, account, "gpt-5.5")
	token, err := ValidateOpenAICodexTurnState(active, svc.codexTurnStatePolicy(), now)
	require.NoError(t, err)
	initialCookieAt := now.Add(-time.Minute)
	require.True(t, svc.codexTurnStateCollector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
		Token: token, Route: "seed", HarvestSessionID: "harvest-session",
		HarvestCookies: []OpenAICodexTurnStateCookie{{Name: "session", Value: "stable-cookie"}}, HarvestCookiesAt: initialCookieAt,
	}, now))

	server, serverErrCh, scopeCh := startOpenAIWSTurnStateIngressServer(t, svc, account)
	client := dialOpenAIWSTurnStateIngressClient(t, server.URL, sessionID)
	writeOpenAIWSTurnStateIngressMessage(t, client, string(body))
	failed := readOpenAIWSTurnStateIngressMessage(t, client)
	require.Equal(t, "response.failed", gjson.GetBytes(failed, "type").String())
	afterFailure, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.True(t, ok)
	require.Equal(t, active, afterFailure.Token.Value)
	require.Equal(t, initialCookieAt, afterFailure.HarvestCookiesAt)

	writeOpenAIWSTurnStateIngressMessage(t, client, string(body))
	completed := readOpenAIWSTurnStateIngressMessage(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
	select {
	case proxyErr := <-serverErrCh:
		require.NoError(t, proxyErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ingress websocket to finish")
	}
	require.Equal(t, scope, <-scopeCh)
	require.Len(t, dialer.Headers(), 1)
	afterSuccess, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.True(t, ok)
	require.Equal(t, handshakeState, afterSuccess.Token.Value)
	require.True(t, afterSuccess.HarvestCookiesAt.After(initialCookieAt))
	stored, ok := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
	require.True(t, ok)
	require.Equal(t, handshakeState, stored)
}

func TestOpenAIGatewayService_IngressCollectorTicketRotation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for index, test := range []struct {
		name           string
		eventType      string
		stale          bool
		ticketIdentity bool
	}{
		{name: "completed", eventType: "response.completed"},
		{name: "completed_ticket_identity", eventType: "response.completed", ticketIdentity: true},
		{name: "failed", eventType: "response.failed"},
		{name: "stale_completed", eventType: "response.completed", stale: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			enableOpenAIWSTurnStateLifecycleCollector(cfg)
			now := time.Now().UTC()
			active := collectorTestToken(t, now.Add(-2*time.Minute), 2, byte(175+index*3))
			handshake := collectorTestToken(t, now.Add(-time.Minute), 2, byte(176+index*3))
			replacement := collectorTestToken(t, now.Add(-30*time.Second), 2, byte(177+index*3))
			event := fmt.Sprintf(`{"type":%q,"response":{"id":"resp_ingress_ticket_rotation","model":"gpt-5.5","status":%q,"usage":{"input_tokens":1,"output_tokens":1}}}`,
				test.eventType, strings.TrimPrefix(test.eventType, "response."))
			var conn openAIWSClientConn = &openAIWSCaptureConn{events: [][]byte{[]byte(event)}}
			var gated *openAIWSGatedConn
			var secondCapture *openAIWSCaptureConn
			conns := []openAIWSClientConn{conn}
			if test.stale {
				gated = newOpenAIWSGatedConn(event)
				conn = gated
				secondCapture = &openAIWSCaptureConn{events: [][]byte{[]byte(
					`{"type":"response.completed","response":{"id":"resp_after_stale_reconnect","model":"gpt-5.5","status":"completed"}}`,
				)}}
				conns = []openAIWSClientConn{gated, secondCapture}
			}
			dialer := &openAIWSTurnStateSequenceDialer{
				conns: conns, handshakes: []http.Header{openAIWSTurnStateLifecycleHandshake(handshake)},
			}
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, dialer)
			svc.initCodexTurnStateCollector()
			account := codexTurnStateGatewayTestAccount(8950 + int64(index))
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			sessionID := "rotation-ingress-" + test.name
			body := `{"type":"response.create","model":"gpt-5.5","stream":false,"input":"hello"}`
			if test.ticketIdentity {
				body = `{"type":"response.create","model":"gpt-5.5","stream":false,"input":"hello","prompt_cache_key":"old-cache","client_metadata":{"old_marker":"client","session_id":"foreign-session"},"device_id":"old-device"}`
			}
			seedContext := newOpenAIWSTurnStateLifecycleContext(t, nil, nil, sessionID)
			scope, _ := resolveOpenAIWSExecutionScope(seedContext, []byte(body), openAIWSTurnStateLifecycleAPIKeyID)
			require.NotEmpty(t, scope)
			bindOpenAICodexTurnStateExecutionScopeValue(seedContext, scope)
			key := svc.codexTurnStateKey(seedContext, account, "gpt-5.5")
			if test.stale || test.ticketIdentity {
				token, err := ValidateOpenAICodexTurnState(active, svc.codexTurnStatePolicy(), now)
				require.NoError(t, err)
				require.True(t, svc.codexTurnStateCollector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
					Token: token, Route: "seed", HarvestSessionID: "harvest-session-a",
					HarvestCookies: []OpenAICodexTurnStateCookie{{Name: "session", Value: "cookie-a"}}, HarvestCookiesAt: now,
					EgressPinned: true, EgressProxyURL: "http://127.0.0.1:8123",
				}, now))
			} else {
				require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, active, "seed", now))
			}

			firstTurnDone := make(chan struct{})
			server, serverErrCh, scopeCh := startOpenAIWSTurnStateIngressServer(t, svc, account, &OpenAIWSIngressHooks{
				AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
					if turn == 1 {
						close(firstTurnDone)
					}
				},
			})
			client := dialOpenAIWSTurnStateIngressClient(t, server.URL, sessionID)
			writeOpenAIWSTurnStateIngressMessage(t, client, body)
			if test.stale {
				select {
				case <-gated.sent:
				case <-time.After(3 * time.Second):
					close(gated.gate)
					t.Fatal("timed out waiting for the in-flight ingress turn")
				}
				issued, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
				require.True(t, ok)
				require.Equal(t, active, issued.Token.Value)
				svc.codexTurnStateCollector.DeleteAndForceRefresh(key)
				replacementToken, err := ValidateOpenAICodexTurnState(replacement, svc.codexTurnStatePolicy(), time.Now())
				require.NoError(t, err)
				require.True(t, svc.codexTurnStateCollector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
					Token: replacementToken, Route: "seed", HarvestSessionID: "harvest-session-c",
					HarvestCookies: []OpenAICodexTurnStateCookie{{Name: "session", Value: "cookie-c"}}, HarvestCookiesAt: time.Now(),
					EgressPinned: true, EgressProxyURL: "http://127.0.0.1:8124",
				}, time.Now()))
				close(gated.gate)
			}
			terminal := readOpenAIWSTurnStateIngressMessage(t, client)
			require.Equal(t, test.eventType, gjson.GetBytes(terminal, "type").String())
			if test.stale {
				select {
				case <-firstTurnDone:
				case <-time.After(3 * time.Second):
					t.Fatal("timed out waiting for the stale turn to complete")
				}
				require.NoError(t, gated.Close())
				writeOpenAIWSTurnStateIngressMessage(t, client,
					`{"type":"response.create","model":"gpt-5.5","stream":false,"prompt_cache_key":"next-rotation","client_metadata":{"old_marker":"next","session_id":"foreign-next"},"device_id":"old-device","input":"next"}`)
				second := readOpenAIWSTurnStateIngressMessage(t, client)
				require.Equal(t, "response.completed", gjson.GetBytes(second, "type").String())
			}
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			select {
			case err := <-serverErrCh:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for ingress to finish")
			}
			require.Equal(t, scope, <-scopeCh)
			headers := dialer.Headers()
			if test.stale {
				require.Len(t, headers, 2)
				require.Equal(t, replacement, headers[1].Get(openAIWSTurnStateHeader), "a reconnect must use the current ticket, not A or rejected B")
				require.Equal(t, "harvest-session-a", headers[0].Get("session_id"))
				require.Equal(t, "session=cookie-a", headers[0].Get("Cookie"))
				require.Equal(t, "harvest-session-c", headers[1].Get("session_id"))
				require.Equal(t, "session=cookie-c", headers[1].Get("Cookie"))
				require.Equal(t, []string{"http://127.0.0.1:8123", "http://127.0.0.1:8124"}, dialer.Proxies())
				requireOpenAICodexWSTicketBodyIsolated(t, secondCapture.lastWrite)
			} else {
				require.Len(t, headers, 1)
				if test.ticketIdentity {
					require.Equal(t, "harvest-session-a", headers[0].Get("session_id"))
					requireOpenAICodexWSTicketBodyIsolated(t, conn.(*openAIWSCaptureConn).lastWrite)
				}
			}
			require.Equal(t, active, headers[0].Get(openAIWSTurnStateHeader))
			current, ok := svc.codexTurnStateCollector.Acquire(key, time.Now())
			require.True(t, ok)
			wantState := active
			if test.stale {
				wantState = replacement
			} else if test.eventType == "response.completed" {
				wantState = handshake
			}
			require.Equal(t, wantState, current.Token.Value)
			stored, ok := store.GetSessionTurnState(openAIWSTurnStateLifecycleGroupID, account.ID, scope, "gpt-5.5")
			if test.eventType == "response.completed" && !test.stale {
				require.True(t, ok)
				require.Equal(t, handshake, stored)
			} else {
				require.False(t, ok, "a failed or stale response must not commit its handshake state")
			}
		})
	}
}

func TestOpenAIGatewayService_IngressTurnStateModelSwitchFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(
		`{"type":"response.completed","response":{"id":"resp_before_model_switch","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`,
	)}}
	handshake := openAIWSTurnStateLifecycleHandshake("model-bound-state")
	dialer := &openAIWSTurnStateSequenceDialer{
		conns:      []openAIWSClientConn{conn},
		handshakes: []http.Header{handshake},
	}
	svc, _ := newOpenAIWSTurnStateLifecycleService(t, newOpenAIWSTurnStateLifecycleConfig(), dialer)
	account := newOpenAIWSTurnStateLifecycleAccount(8860)
	server, serverErrCh, _ := startOpenAIWSTurnStateIngressServer(t, svc, account)
	client := dialOpenAIWSTurnStateIngressClient(t, server.URL, "ingress-model-switch")
	writeOpenAIWSTurnStateIngressMessage(t, client, `{"type":"response.create","model":"gpt-5.1","stream":false,"input":"first"}`)
	completed := readOpenAIWSTurnStateIngressMessage(t, client)
	require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
	accountPool, ok := svc.getOpenAIWSConnPool().getAccountPool(account.ID)
	require.True(t, ok)
	accountPool.mu.Lock()
	var pooledConn *openAIWSConn
	for _, conn := range accountPool.conns {
		pooledConn = conn
		break
	}
	accountPool.mu.Unlock()
	require.NotNil(t, pooledConn)
	require.True(t, pooledConn.matchesHandshakeTurnStateModel("gpt-5.1"), "ingress must bind the state-bearing connection to the final upstream model")
	writeOpenAIWSTurnStateIngressMessage(t, client, `{"type":"response.create","model":"gpt-5.2","stream":false,"input":"second"}`)

	select {
	case err := <-serverErrCh:
		require.Error(t, err)
		var closeErr *OpenAIWSClientCloseError
		require.ErrorAs(t, err, &closeErr)
		require.Equal(t, coderws.StatusPolicyViolation, closeErr.StatusCode())
		require.Contains(t, closeErr.Reason(), "different model")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for model-switch rejection")
	}
	conn.mu.Lock()
	writeCount := len(conn.writes)
	conn.mu.Unlock()
	require.Equal(t, 1, writeCount, "the switched-model turn must not reach upstream")
}
