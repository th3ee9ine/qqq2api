package service

import (
	"context"
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
}

func (d *openAIWSTurnStateSequenceDialer) Dial(
	_ context.Context,
	_ string,
	headers http.Header,
	_ string,
) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.headers = append(d.headers, cloneHeader(headers))
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
		proxyErr := svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, nil)
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
