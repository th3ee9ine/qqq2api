package service

import (
	"bytes"
	"context"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

const (
	configuredCodexOriginator = "system-gpt-client"
	configuredCodexUserAgent  = "codex-tui/0.125.0 (Linux 6.8; x86_64) xterm"
	configuredCodexVersion    = "0.200.1"
	wantConfiguredCodexUA     = "system-gpt-client/0.200.1 (Linux 6.8; x86_64) xterm"
)

// installConfiguredCodexIdentityForOutboundTest mirrors the production
// ProvideSettingService resolver wiring. Keeping the values in a repository
// (instead of returning literals directly from the resolvers) verifies the
// complete setting -> cached getter -> canonical identity -> outbound request
// chain used after an administrator saves the three system settings.
func installConfiguredCodexIdentityForOutboundTest(t *testing.T) {
	t.Helper()

	codexCanonicalUAMu.RLock()
	previousUAResolver := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	codexCanonicalOriginatorMu.RLock()
	previousOriginatorResolver := codexCanonicalOriginator
	codexCanonicalOriginatorMu.RUnlock()
	codexCanonicalResponsesVersionMu.RLock()
	previousVersionResolver := codexCanonicalResponsesVersion
	codexCanonicalResponsesVersionMu.RUnlock()
	previousEnforcement := codexIdentityEnforcement.Load()
	previousLocalIdentityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	SetCodexIdentityEnforcementEnabled(true)
	SetCodexAccountLocalDeviceIdentityEnabled(true)

	repo := &codexHeaderSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexOriginator:    configuredCodexOriginator,
		SettingKeyOpenAICodexUserAgent:     configuredCodexUserAgent,
		SettingKeyOpenAICodexClientVersion: configuredCodexVersion,
	}}
	settings := NewSettingService(repo, &config.Config{})
	SetCodexCanonicalUserAgentResolver(func() string {
		return settings.GetOpenAICodexCanonicalUserAgent(context.Background())
	})
	SetCodexCanonicalOriginatorResolver(func() string {
		return settings.GetOpenAICodexOriginator(context.Background())
	})
	SetCodexCanonicalResponsesVersionResolver(func() string {
		return settings.GetOpenAICodexResponsesVersion(context.Background())
	})
	t.Cleanup(func() {
		SetCodexCanonicalUserAgentResolver(previousUAResolver)
		SetCodexCanonicalOriginatorResolver(previousOriginatorResolver)
		SetCodexCanonicalResponsesVersionResolver(previousVersionResolver)
		SetCodexIdentityEnforcementEnabled(previousEnforcement)
		SetCodexAccountLocalDeviceIdentityEnabled(previousLocalIdentityEnabled)
	})
}

type expectedCodexOutboundIdentity struct {
	originator string
	userAgent  string
	version    string
}

func configuredCodexOutboundIdentity() expectedCodexOutboundIdentity {
	return expectedCodexOutboundIdentity{configuredCodexOriginator, wantConfiguredCodexUA, configuredCodexVersion}
}

func requireCodexOutboundIdentity(t *testing.T, headers http.Header, want expectedCodexOutboundIdentity, inference bool) {
	t.Helper()
	require.Equal(t, want.originator, headers.Get("Originator"))
	require.Equal(t, want.userAgent, headers.Get("User-Agent"))
	if inference {
		require.Equal(t, want.version, headers.Get("Version"))
	} else {
		require.Empty(t, headers.Get("Version"), "credential/control-plane requests must omit the Responses Version header")
	}
}

func TestConfiguredCodexIdentityReachesHTTPResponsesUpstream(t *testing.T) {
	installConfiguredCodexIdentityForOutboundTest(t)
	for _, passthrough := range []bool{false, true} {
		name := "normal"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			testCodexIdentityHTTPResponsesUpstream(t, nil, configuredCodexOutboundIdentity(), passthrough)
		})
	}
}

func testCodexIdentityHTTPResponsesUpstream(t *testing.T, accountExtra map[string]any, want expectedCodexOutboundIdentity, passthrough bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	extra := maps.Clone(accountExtra)
	if extra == nil {
		extra = make(map[string]any)
	}
	extra["openai_passthrough"] = passthrough
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
	c.Request.Header.Set("Originator", "stale-inbound-client")
	c.Request.Header.Set("User-Agent", "stale-inbound-client/0.1.0")
	c.Request.Header.Set("Version", "0.1.0")

	body := []byte(`{"model":"gpt-5.2","stream":false,"store":true,"input":[{"type":"text","text":"hi"}]}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "x-request-id": []string{"rid"}},
		Body: io.NopCloser(strings.NewReader("data: " +
			`{"type":"response.completed","response":{"id":"resp_identity_http","model":"gpt-5.2","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":0}}}` +
			"\n\ndata: [DONE]\n\n")),
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Gateway: config.GatewayConfig{ForceCodexCLI: false}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:             7101,
		Name:           "configured-identity-http",
		Platform:       PlatformOpenAI,
		Type:           AccountTypeOAuth,
		Concurrency:    1,
		Credentials:    map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-acc"},
		Extra:          extra,
		Status:         StatusActive,
		Schedulable:    true,
		RateMultiplier: f64p(1),
	}

	_, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, upstream.lastReq)
	requireCodexOutboundIdentity(t, upstream.lastReq.Header, want, true)
}

func TestConfiguredCodexIdentityReachesWSResponsesHandshake(t *testing.T) {
	installConfiguredCodexIdentityForOutboundTest(t)
	testCodexIdentityWSResponsesHandshake(t, nil, configuredCodexOutboundIdentity())
}

func testCodexIdentityWSResponsesHandshake(t *testing.T, accountExtra map[string]any, want expectedCodexOutboundIdentity) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	extra := maps.Clone(accountExtra)
	if extra == nil {
		extra = make(map[string]any)
	}
	extra["responses_websockets_v2_enabled"] = true

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	c.Request.Header.Set("Originator", "stale-inbound-client")
	c.Request.Header.Set("User-Agent", "stale-inbound-client/0.1.0")
	c.Request.Header.Set("Version", "0.1.0")

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.AllowStoreRecovery = false
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1

	captureConn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_configured_identity","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	captureDialer := &openAIWSCaptureDialer{conn: captureConn}
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(captureDialer)

	svc := &OpenAIGatewayService{
		cfg:              cfg,
		httpUpstream:     &httpUpstreamRecorder{},
		cache:            &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
		toolCorrector:    NewCodexToolCorrector(),
		openaiWSPool:     pool,
	}
	account := &Account{
		ID:          7102,
		Name:        "configured-identity-ws",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
		Extra:       extra,
	}

	body := []byte(`{"model":"gpt-5.1","stream":false,"input":[{"type":"input_text","text":"hello"}]}`)
	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	requireCodexOutboundIdentity(t, captureDialer.lastHeaders, want, true)
}

func TestConfiguredCodexIdentityReachesAllWhamRequestsWithoutVersion(t *testing.T) {
	installConfiguredCodexIdentityForOutboundTest(t)
	testCodexIdentityAllWhamRequestsWithoutVersion(t, nil, configuredCodexOutboundIdentity())
}

func testCodexIdentityAllWhamRequestsWithoutVersion(t *testing.T, accountExtra map[string]any, want expectedCodexOutboundIdentity) {
	t.Helper()

	account := &Account{
		ID:       7103,
		Extra:    maps.Clone(accountExtra),
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"chatgpt_account_id": "configured-identity-account",
		},
	}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	tokenCache := &stubQuotaTokenCache{tokens: map[string]string{
		OpenAITokenCacheKey(account): "fake-token",
	}}
	tokenProvider := NewOpenAITokenProvider(repo, tokenCache, nil)

	seen := make(map[string]int)
	capturedHeaders := make(map[string][]http.Header)
	var captureMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captureMu.Lock()
		seen[r.URL.Path]++
		capturedHeaders[r.URL.Path] = append(capturedHeaders[r.URL.Path], r.Header.Clone())
		captureMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/backend-api/wham/usage":
			_, _ = w.Write([]byte(`{"rate_limit_reset_credits":{"available_count":0}}`))
		case "/backend-api/wham/rate-limit-reset-credits":
			_, _ = w.Write([]byte(`{"available_count":0,"credits":[]}`))
		case "/backend-api/wham/rate-limit-reset-credits/consume":
			_, _ = w.Write([]byte(`{"code":"ok","windows_reset":1}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	svc := NewOpenAIQuotaService(repo, nil, tokenProvider, newQuotaRedirectingFactory(srv))
	usage, err := svc.QueryUsage(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotNil(t, usage)
	reset, err := svc.ResetCreditTargeted(context.Background(), account.ID, "credit-123", "redeem-456")
	require.NoError(t, err)
	require.Equal(t, "ok", reset.Code)

	captureMu.Lock()
	defer captureMu.Unlock()
	require.Equal(t, map[string]int{
		"/backend-api/wham/usage":                            1,
		"/backend-api/wham/rate-limit-reset-credits":         1,
		"/backend-api/wham/rate-limit-reset-credits/consume": 1,
	}, seen)
	for path, headers := range capturedHeaders {
		require.Len(t, headers, 1, "unexpected request count for %s", path)
		requireCodexOutboundIdentity(t, headers[0], want, false)
	}
}

func TestConfiguredCodexIdentityReachesInputTokensUpstreamWithoutVersion(t *testing.T) {
	installConfiguredCodexIdentityForOutboundTest(t)
	testCodexIdentityInputTokensUpstreamWithoutVersion(t, nil, configuredCodexOutboundIdentity())
}

func testCodexIdentityInputTokensUpstreamWithoutVersion(t *testing.T, accountExtra map[string]any, want expectedCodexOutboundIdentity) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.4","input":"hello"}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/input_tokens", bytes.NewReader(body))
	c.Request.Header.Set("Originator", "stale-inbound-client")
	c.Request.Header.Set("User-Agent", "stale-inbound-client/0.1.0")
	c.Request.Header.Set("Version", "0.1.0")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"object":"response.input_tokens","input_tokens":42}`)),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{
		ID:          7104,
		Extra:       maps.Clone(accountExtra),
		Name:        "configured-identity-input-tokens",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Credentials: map[string]any{"access_token": "oauth-token"},
	}

	err := svc.ForwardResponsesInputTokens(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"object":"response.input_tokens","input_tokens":42}`, rec.Body.String())
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, openaiPlatformAPIInputTokensURL, upstream.lastReq.URL.String())
	requireCodexOutboundIdentity(t, upstream.lastReq.Header, want, false)
}

// This exercises the actual HTTP/WS/control-plane request constructors with a
// higher global version, so a local identity accidentally normalized to the
// global version is visible at the outbound boundary.
func TestAccountLocalCodexIdentityVersionReachesAllOutboundPaths(t *testing.T) {
	installConfiguredCodexIdentityForOutboundTest(t)
	const localUA = "codex-tui/0.125.0 (Mac OS X 14.0; arm64) iTerm"
	for _, tc := range []struct {
		name     string
		extra    map[string]any
		disabled bool
		want     expectedCodexOutboundIdentity
	}{
		{
			name: "explicit_local_version_rebuilds_ua_without_global_upgrade",
			extra: map[string]any{
				"openai_local_device_originator": "codex-tui",
				"openai_local_device_user_agent": localUA,
				"openai_local_device_version":    "0.120.3",
			},
			want: expectedCodexOutboundIdentity{"codex-tui", "codex-tui/0.120.3 (Mac OS X 14.0; arm64) iTerm", "0.120.3"},
		},
		{
			name: "missing_local_version_uses_ua_version",
			extra: map[string]any{
				"openai_local_device_originator": "codex-tui",
				"openai_local_device_user_agent": localUA,
			},
			want: expectedCodexOutboundIdentity{"codex-tui", localUA, "0.125.0"},
		},
		{
			name: "invalid_explicit_version_falls_back_as_whole_identity",
			extra: map[string]any{
				"openai_local_device_originator": "codex-tui",
				"openai_local_device_user_agent": localUA,
				"openai_local_device_version":    "broken-version",
			},
			want: configuredCodexOutboundIdentity(),
		},
		{
			name: "invalid_pair_falls_back_as_whole_identity",
			extra: map[string]any{
				"openai_local_device_originator": "Codex Desktop",
				"openai_local_device_user_agent": localUA,
				"openai_local_device_version":    "0.120.3",
			},
			want: configuredCodexOutboundIdentity(),
		},
		{
			name: "disabled_local_identity_uses_global_triple",
			extra: map[string]any{
				"openai_local_device_originator": "codex-tui",
				"openai_local_device_user_agent": localUA,
				"openai_local_device_version":    "0.120.3",
			},
			disabled: true,
			want:     configuredCodexOutboundIdentity(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
			SetCodexAccountLocalDeviceIdentityEnabled(!tc.disabled)
			t.Cleanup(func() { SetCodexAccountLocalDeviceIdentityEnabled(previousEnabled) })
			t.Run("http_responses", func(t *testing.T) {
				testCodexIdentityHTTPResponsesUpstream(t, tc.extra, tc.want, false)
			})
			t.Run("http_responses_passthrough", func(t *testing.T) {
				testCodexIdentityHTTPResponsesUpstream(t, tc.extra, tc.want, true)
			})
			t.Run("ws_responses", func(t *testing.T) {
				testCodexIdentityWSResponsesHandshake(t, tc.extra, tc.want)
			})
			t.Run("input_tokens", func(t *testing.T) {
				testCodexIdentityInputTokensUpstreamWithoutVersion(t, tc.extra, tc.want)
			})
			t.Run("wham", func(t *testing.T) {
				testCodexIdentityAllWhamRequestsWithoutVersion(t, tc.extra, tc.want)
			})
		})
	}
}
