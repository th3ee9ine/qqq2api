package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func testGlobalTurnStateToken(issued time.Time, blocks int) string {
	raw := make([]byte, 57+blocks*16)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issued.Unix()))
	return base64.RawURLEncoding.EncodeToString(raw)
}
func turnStateTestSettings(token, models string) (*SettingService, *codexHeaderSettingRepoStub) {
	repo := &codexHeaderSettingRepoStub{values: map[string]string{SettingKeyOpenAICodexTurnStateEnabled: "true", SettingKeyOpenAICodexTurnState: token, SettingKeyOpenAICodexTurnStateModels: models}}
	return NewSettingService(repo, &config.Config{}), repo
}
func TestGlobalCodexTurnStateModelScope(t *testing.T) {
	for _, tc := range []struct {
		scope  string
		models []string
		want   bool
	}{
		{"", []string{"gpt-5"}, true}, {"GPT-5", []string{"gpt-5"}, true}, {"gpt-5*", []string{"gpt-5.4"}, true},
		{"gpt-5", []string{"alias", "gpt-5"}, true}, {"alias", []string{"alias", "gpt-5"}, true},
		{"gpt-5", []string{"gpt-5.4"}, false}, {"gpt-5", []string{"", ""}, true}, {"gpt-5", []string{"", "gpt-4"}, false},
	} {
		require.Equal(t, tc.want, codexTurnStateModelMatches(tc.scope, tc.models...), tc)
	}
}
func TestGlobalCodexTurnStateDiagnosticsNeverGateOpaqueExpiredOrSuspect(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		token, reason string
		blocks        int
	}{
		{"opaque-state", "unknown", 0}, {testGlobalTurnStateToken(now.Add(-2*time.Hour), 10), "expired", 10},
		{testGlobalTurnStateToken(now.Add(2*time.Hour), 10), "future", 10}, {testGlobalTurnStateToken(now, 11), "suspect", 11},
		{testGlobalTurnStateToken(now, 10), "ready", 10},
	} {
		status := InspectCodexTurnState(tc.token, true, now)
		require.True(t, status.Active)
		require.Equal(t, tc.reason, status.Reason)
		require.Equal(t, tc.blocks, status.Blocks)
		settings, _ := turnStateTestSettings(tc.token, "")
		svc := &OpenAIGatewayService{settingService: settings}
		h := http.Header{}
		svc.applyOpenAICodexTurnState(context.Background(), &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, h, "gpt-5")
		require.Equal(t, tc.token, h.Get(openAICodexTurnStateHeader))
		encoded, err := json.Marshal(status)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), tc.token)
	}
	require.False(t, InspectCodexTurnState("bad\nheader", true, now).Active)
	require.False(t, InspectCodexTurnState("", true, now).Active)
	require.False(t, InspectCodexTurnState("state", false, now).Active)
}
func TestGlobalCodexTurnStateHTTPAndCompactBuilders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings, repo := turnStateTestSettings("system-private-state", "alias")
	svc := &OpenAIGatewayService{settingService: settings, cfg: &config.Config{}}
	account := &Account{ID: 73, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "access", "chatgpt_account_id": "id"}}
	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		for _, passthrough := range []bool{false, true} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", path, nil)
			c.Request.Header.Set(openAICodexTurnStateHeader, "native")
			body := []byte(`{"model":"gpt-5","input":"hello"}`)
			var req *http.Request
			var err error
			if passthrough {
				req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "access", "alias")
			} else {
				req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "access", false, "", false, "alias")
			}
			require.NoError(t, err)
			require.Equal(t, "system-private-state", req.Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, "native", c.Request.Header.Get(openAICodexTurnStateHeader))
		}
	}
	// Every account uses the same global setting, never credentials.
	for _, a := range []*Account{{ID: 74, Platform: PlatformOpenAI, Type: AccountTypeSetupToken}, {ID: 75, Platform: PlatformOpenAI, Type: AccountTypeOAuth}} {
		h := http.Header{}
		svc.applyOpenAICodexTurnState(context.Background(), a, h, "alias")
		require.Equal(t, "system-private-state", h.Get(openAICodexTurnStateHeader))
	}
	for _, a := range []*Account{nil, {Platform: PlatformAnthropic, Type: AccountTypeOAuth}, {Platform: PlatformOpenAI, Type: AccountTypeAPIKey}} {
		h := http.Header{}
		svc.applyOpenAICodexTurnState(context.Background(), a, h, "alias")
		require.Empty(t, h.Get(openAICodexTurnStateHeader))
	}
	repo.values[SettingKeyOpenAICodexTurnStateEnabled] = "false"
	settings.InvalidateOpenAICodexTurnStateCache()
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, "native")
	svc.applyOpenAICodexTurnState(context.Background(), account, h, "alias")
	require.Equal(t, "native", h.Get(openAICodexTurnStateHeader))
}
func TestGlobalCodexTurnStateCacheIsolationReloadAndDirtyValues(t *testing.T) {
	identityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	forwarding, _ := gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings)
	t.Cleanup(func() {
		SetCodexAccountLocalDeviceIdentityEnabled(identityEnabled)
		gatewayForwardingCache.Store(forwarding)
	})
	first, repo := turnStateTestSettings("first", "")
	second, _ := turnStateTestSettings("second", "")
	ctx := context.Background()
	require.Equal(t, "first", first.GetOpenAICodexTurnState(ctx).Token)
	require.Equal(t, "second", second.GetOpenAICodexTurnState(ctx).Token)
	require.NoError(t, first.UpdateSettings(ctx, &SystemSettings{OpenAICodexTurnStateEnabled: true, OpenAICodexTurnState: "replacement"}))
	require.Equal(t, "replacement", first.GetOpenAICodexTurnState(ctx).Token)
	first.openAICodexTurnStateCache.Store(&cachedOpenAICodexTurnState{expiresAt: time.Now().Add(-time.Second)})
	require.Equal(t, "replacement", first.GetOpenAICodexTurnState(ctx).Token)
	repo.values[SettingKeyOpenAICodexTurnStateModels] = "invalid*scope"
	first.InvalidateOpenAICodexTurnStateCache()
	require.False(t, first.GetOpenAICodexTurnState(ctx).Enabled)
}
func TestGlobalCodexTurnStateWSRefreshAndNativeCompatibility(t *testing.T) {
	settings, repo := turnStateTestSettings("first", "gpt-5*")
	ctx := context.Background()
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	native := http.Header{}
	native.Set(openAICodexTurnStateHeader, "native")
	req := openAIWSAcquireRequest{Account: account, Headers: native, TurnState: openAIWSTurnStatePolicy{Settings: settings, Models: []string{"gpt-5"}, NativeState: "native"}}
	injected := req.withCurrentTurnState(ctx)
	require.Equal(t, "first", injected.Headers.Get(openAICodexTurnStateHeader))
	require.Equal(t, "native", native.Get(openAICodexTurnStateHeader))
	first := normalizeOpenAIWSHandshakeCompatibility(account, injected.Headers, injected.turnStateFingerprint)
	repo.values[SettingKeyOpenAICodexTurnState] = "second"
	settings.InvalidateOpenAICodexTurnStateCache()
	next := injected.withCurrentTurnState(ctx)
	require.Equal(t, "second", next.Headers.Get(openAICodexTurnStateHeader))
	require.NotEqual(t, first, normalizeOpenAIWSHandshakeCompatibility(account, next.Headers, next.turnStateFingerprint))
	repo.values[SettingKeyOpenAICodexTurnStateEnabled] = "false"
	settings.InvalidateOpenAICodexTurnStateCache()
	off := next.withCurrentTurnState(ctx)
	require.Equal(t, "native", off.Headers.Get(openAICodexTurnStateHeader))
	require.Empty(t, off.turnStateFingerprint)
	nativeNext := native.Clone()
	nativeNext.Set(openAICodexTurnStateHeader, "native-next")
	require.Equal(t, normalizeOpenAIWSHandshakeCompatibility(account, native), normalizeOpenAIWSHandshakeCompatibility(account, nativeNext))
	conn := newOpenAIWSConn("pinned", 1, &openAIWSFakeConn{}, nil)
	conn.handshakeCompatibility = first
	require.True(t, conn.matchesContinuationHandshakeCompatibility(normalizeOpenAIWSHandshakeCompatibility(account, off.Headers)))
}
func TestGlobalCodexTurnStatePoolDialAndRotation(t *testing.T) {
	settings, repo := turnStateTestSettings("first", "")
	account := &Account{ID: 5000, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	dialer := newOpenAIWSFirstDialBlockingCaptureDialer()
	close(dialer.releaseFirst)
	pool.setClientDialerForTest(dialer)
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses", Headers: http.Header{}, TurnState: openAIWSTurnStatePolicy{Settings: settings, NativeState: "native"}}
	a, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	id := a.ConnID()
	a.Release()
	repo.values[SettingKeyOpenAICodexTurnState] = "second"
	settings.InvalidateOpenAICodexTurnStateCache()
	b, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, id, b.ConnID())
	bID := b.ConnID()
	b.Release()
	require.Equal(t, 2, dialer.DialCount())
	repo.values[SettingKeyOpenAICodexTurnStateEnabled] = "false"
	settings.InvalidateOpenAICodexTurnStateCache()
	c, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, bID, c.ConnID())
	c.Release()
	require.Equal(t, 3, dialer.DialCount())
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	for i, want := range []string{"first", "second", "native"} {
		require.Equal(t, want, dialer.headers[i].Get(openAICodexTurnStateHeader), "actual dial %d", i)
	}
}
func TestGlobalCodexTurnStateProbeDefaultsRedacted(t *testing.T) {
	settings, _ := turnStateTestSettings("private-test-token", "")
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	headers := buildOpenAIAccountTestHeaderDefaults(a, "responses", "https://chatgpt.com/backend-api/codex/responses", map[string]any{"model": "gpt-5"}, settings)
	require.Equal(t, openAITestRedactedHeader, headers[http.CanonicalHeaderKey(openAICodexTurnStateHeader)])
	encoded, err := json.Marshal(headers)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-test-token")
}

// Concurrent cold reads coalesce behind the write mutex; warm reads are atomic.
func TestGlobalCodexTurnStateConcurrentReads(t *testing.T) {
	settings, _ := turnStateTestSettings(strings.Repeat("a", 4096), "*")
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if !settings.GetOpenAICodexTurnState(context.Background()).Enabled {
					t.Error("disabled")
				}
			}
		}()
	}
	wg.Wait()
}
