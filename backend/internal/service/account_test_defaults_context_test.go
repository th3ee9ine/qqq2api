package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAITestDefaultsForAccountResolvesShadowHeaders(t *testing.T) {
	parent := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token":               "secret-parent-token",
		"chatgpt_account_id":         "parent-chatgpt-account",
		"chatgpt_account_is_fedramp": true,
	}}
	proxyID := int64(7)
	shadow := &Account{ID: 200, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent.ID,
		Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "my-shadow-model"}},
		ProxyID:     &proxyID, Proxy: &Proxy{ID: proxyID, Name: "shadow-proxy", Protocol: "http", Host: "proxy.example", Port: 8080},
	}
	svc := &AccountTestService{accountRepo: newStubCredRepo(parent)}
	got, err := svc.BuildOpenAITestDefaultsForAccount(context.Background(), shadow, "responses", "")
	require.NoError(t, err)
	parentHeaders := svc.BuildOpenAITestDefaults(parent, "responses", "").Headers
	parentHeaders["X-Codex-Routing-Hint"] = "model=my-shadow-model"
	require.Equal(t, parentHeaders, got.Headers)
	require.Equal(t, "my-shadow-model", got.Body["model"])
	require.Equal(t, &proxyID, got.ProxyID)
	for _, value := range got.Headers {
		require.NotContains(t, value, "secret-parent-token")
	}
	require.Contains(t, got.Notes, "影子账号的上游请求头来自母账号；模型映射与代理配置保留所选账号设置。")
}

func TestOpenAITestDefaultsForAccountRejectsMissingShadowParent(t *testing.T) {
	parentID := int64(100)
	shadow := &Account{ID: 200, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
	svc := &AccountTestService{accountRepo: newStubCredRepo(nil)}
	_, err := svc.BuildOpenAITestDefaultsForAccount(context.Background(), shadow, "responses", "")
	require.Error(t, err)
	var nilService *AccountTestService
	_, err = nilService.BuildOpenAITestDefaultsForAccount(context.Background(), shadow, "responses", "")
	require.Error(t, err)
	_, err = nilService.BuildOpenAITestDefaultsForAccount(context.Background(), nil, "responses", "")
	require.NoError(t, err)
}
