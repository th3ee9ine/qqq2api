package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// Legacy keys deliberately remain in fixtures to guard upgrades from 3.0.7.
func turnStateTestSettings(legacyToken, models string) (*SettingService, *codexHeaderSettingRepoStub) {
	repo := &codexHeaderSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTurnStateEnabled: "true", SettingKeyOpenAICodexTurnState: legacyToken,
		SettingKeyOpenAICodexTurnStateModels: models, SettingKeyOpenAICodexTurnStateAutoEnabled: "true",
	}}
	return NewSettingService(repo, &config.Config{}), repo
}
func seedAutomaticTurnState(a *Account, token string) {
	a.Extra = mergeMap(a.Extra, map[string]any{CodexTurnStateAutoExtraKey: token, CodexTurnStateAutoSetAtExtraKey: time.Now().UnixMilli()})
}
func TestCodexTurnStateModelScope(t *testing.T) {
	for _, tc := range []struct {
		scope  string
		models []string
		want   bool
	}{
		{"", []string{"gpt-5"}, true}, {"GPT-5", []string{"gpt-5"}, true}, {"gpt-5*", []string{"gpt-5.5"}, true},
		{"gpt-5", []string{"alias", "gpt-5"}, true}, {"alias", []string{"alias", "gpt-5"}, true},
		{"gpt-5", []string{"gpt-5.5"}, false}, {"gpt-5", []string{"", ""}, true}, {"gpt-5", []string{"", "gpt-4"}, false},
	} {
		require.Equal(t, tc.want, codexTurnStateModelMatches(tc.scope, tc.models...), tc)
	}
}
func TestCodexTurnStateLegacyManualValueNeverInjected(t *testing.T) {
	for _, legacy := range []string{"legacy-private", "invalid\nlegacy", testGlobalTurnStateToken(time.Now(), 10)} {
		settings, repo := turnStateTestSettings(legacy, "")
		svc := &OpenAIGatewayService{settingService: settings}
		a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		h := http.Header{}
		svc.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5.5")
		require.Empty(t, h.Get(openAICodexTurnStateHeader))
		seedAutomaticTurnState(a, "account-state")
		svc.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5.5")
		require.Equal(t, "account-state", h.Get(openAICodexTurnStateHeader))
		repo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
		settings.InvalidateOpenAICodexTurnStateCache()
		h = http.Header{}
		svc.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5.5")
		require.Empty(t, h.Get(openAICodexTurnStateHeader), "legacy enabled must not enable automatic mode")
	}
}
func TestCodexTurnStateAutomaticHTTPAndCompactBuilders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings, _ := turnStateTestSettings("legacy-private", "alias")
	svc := &OpenAIGatewayService{settingService: settings, cfg: &config.Config{}}
	a := &Account{ID: 73, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "access", "chatgpt_account_id": "id"}}
	seedAutomaticTurnState(a, "account-state")
	for _, path := range []string{"/v1/responses", "/v1/responses/compact"} {
		for _, passthrough := range []bool{false, true} {
			for _, native := range []string{"", "native"} {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest("POST", path, nil)
				if native != "" {
					c.Request.Header.Set(openAICodexTurnStateHeader, native)
				}
				body := []byte(`{"model":"gpt-5","input":"hello"}`)
				var req *http.Request
				var err error
				if passthrough {
					req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, a, body, "access", "alias")
				} else {
					req, err = svc.buildUpstreamRequest(context.Background(), c, a, body, "access", false, "", false, "alias")
				}
				require.NoError(t, err)
				want := native
				if want == "" {
					want = "account-state"
				}
				require.Equal(t, want, req.Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, native, c.Request.Header.Get(openAICodexTurnStateHeader))
			}
		}
	}
	for _, other := range []*Account{nil, {ID: 74, Platform: PlatformAnthropic, Type: AccountTypeOAuth}, {ID: 75, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}} {
		h := http.Header{}
		svc.applyOpenAICodexTurnState(context.Background(), other, h, "alias")
		require.Empty(t, h.Get(openAICodexTurnStateHeader))
	}
}
func TestCodexTurnStateAutomaticCacheIsolationAndReload(t *testing.T) {
	first, repo := turnStateTestSettings("legacy-one", "gpt-5*")
	second, _ := turnStateTestSettings("legacy-two", "other")
	ctx := context.Background()
	require.Equal(t, "gpt-5*", first.GetOpenAICodexTurnState(ctx).Models)
	require.Equal(t, "other", second.GetOpenAICodexTurnState(ctx).Models)
	repo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	first.InvalidateOpenAICodexTurnStateCache()
	require.False(t, first.GetOpenAICodexTurnState(ctx).AutoEnabled)
	require.True(t, second.GetOpenAICodexTurnState(ctx).AutoEnabled)
	repo.values[SettingKeyOpenAICodexTurnStateModels] = "invalid*scope"
	repo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "true"
	first.openAICodexTurnStateCache.Store(&cachedOpenAICodexTurnState{expiresAt: time.Now().Add(-time.Second)})
	require.False(t, first.GetOpenAICodexTurnState(ctx).AutoEnabled)
}
func TestCodexTurnStateAutomaticPoolDialAndRotation(t *testing.T) {
	settings, repo := turnStateTestSettings("legacy", "")
	a := &Account{ID: 5000, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	seedAutomaticTurnState(a, "first")
	svc := &OpenAIGatewayService{settingService: settings}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	t.Cleanup(pool.Close)
	dialer := newOpenAIWSFirstDialBlockingCaptureDialer()
	close(dialer.releaseFirst)
	pool.setClientDialerForTest(dialer)
	req := openAIWSAcquireRequest{Account: a, WSURL: "wss://example.com/responses", Headers: http.Header{}, TurnState: openAIWSTurnStatePolicy{Settings: settings, Gateway: svc}}
	first, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "first", *first.SentTurnState())
	firstID := first.ConnID()
	first.Release()
	reused, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, firstID, reused.ConnID())
	require.Equal(t, "first", *reused.SentTurnState())
	reused.Release()
	svc.collectOpenAICodexTurnState(context.Background(), a, "second")
	second, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, firstID, second.ConnID())
	require.Equal(t, "second", *second.SentTurnState())
	secondID := second.ConnID()
	second.Release()
	repo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	settings.InvalidateOpenAICodexTurnStateCache()
	third, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, secondID, third.ConnID())
	require.Equal(t, "", *third.SentTurnState())
	third.Release()
	require.Equal(t, 3, dialer.DialCount())
	dialer.mu.Lock()
	defer dialer.mu.Unlock()
	for i, want := range []string{"first", "second", ""} {
		require.Equal(t, want, dialer.headers[i].Get(openAICodexTurnStateHeader))
	}
	native := http.Header{}
	native.Set(openAICodexTurnStateHeader, "native")
	req.Headers = native
	req.TurnState.NativeState = "native"
	require.Equal(t, "native", req.withCurrentTurnState(context.Background()).Headers.Get(openAICodexTurnStateHeader))
}
func TestCodexTurnStatePreviewDoesNotInjectLegacyOrProbe(t *testing.T) {
	settings, _ := turnStateTestSettings("legacy-private", "")
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	headers := (&AccountTestService{settingService: settings}).BuildOpenAITestDefaults(a, "responses", "").Headers
	require.Empty(t, headers[http.CanonicalHeaderKey(openAICodexTurnStateHeader)])
	data, err := json.Marshal(headers)
	require.NoError(t, err)
	require.NotContains(t, string(data), "legacy-private")
}
func TestCodexTurnStateConcurrentReads(t *testing.T) {
	settings, _ := turnStateTestSettings("ignored", "*")
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if !settings.GetOpenAICodexTurnState(context.Background()).AutoEnabled {
					t.Error("disabled")
				}
			}
		}()
	}
	wg.Wait()
}
