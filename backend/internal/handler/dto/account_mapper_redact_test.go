package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestAccountFromServiceShallow_RedactsSensitiveCredentials(t *testing.T) {
	src := &service.Account{
		ID:       42,
		Name:     "demo",
		Platform: "anthropic",
		Type:     "oauth",
		Credentials: map[string]any{
			"access_token":  "at-secret",
			"refresh_token": "rt-secret",
			"id_token":      "id-secret",
			"api_key":       "sk-secret",
			"base_url":      "https://api.example.com",
			"model_mapping": map[string]any{"foo": "bar"},
		},
	}

	got := AccountFromServiceShallow(src)
	require.NotNil(t, got)

	// 敏感键不在 Credentials 里
	require.NotContains(t, got.Credentials, "access_token")
	require.NotContains(t, got.Credentials, "refresh_token")
	require.NotContains(t, got.Credentials, "id_token")
	require.NotContains(t, got.Credentials, "api_key")
	// 非敏感键保留
	require.Equal(t, "https://api.example.com", got.Credentials["base_url"])
	require.Equal(t, map[string]any{"foo": "bar"}, got.Credentials["model_mapping"])

	// 状态 map 标记敏感键存在
	require.True(t, got.CredentialsStatus["has_access_token"])
	require.True(t, got.CredentialsStatus["has_refresh_token"])
	require.True(t, got.CredentialsStatus["has_id_token"])
	require.True(t, got.CredentialsStatus["has_api_key"])

	// JSON 序列化校验：响应体里不会出现敏感子串
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "rt-secret")
	require.NotContains(t, string(raw), "at-secret")
	require.NotContains(t, string(raw), "sk-secret")
	require.NotContains(t, string(raw), "id-secret")
	// 状态标识应序列化进 JSON
	require.Contains(t, string(raw), "credentials_status")
	require.Contains(t, string(raw), "has_refresh_token")

	// 原始 service.Account 不应被改动
	require.Equal(t, "rt-secret", src.Credentials["refresh_token"])
}

func TestAccountFromServiceShallow_ExposesSubscriptionExpiry(t *testing.T) {
	src := &service.Account{
		ID:       42,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Credentials: map[string]any{
			"subscription_expires_at": "2027-01-02T03:04:05Z",
		},
	}

	got := AccountFromServiceShallow(src)
	require.Equal(t, "2027-01-02T03:04:05Z", got.SubscriptionExpiresAt)
	require.Equal(t, "2027-01-02T03:04:05Z", got.Credentials["subscription_expires_at"])
}

// Accounts can arrive from an old scheduler cache without repository hydration.
// Retiring the integrations must not expose their persisted session or snapshots.
func TestAccountFromServiceShallow_RedactsRetiredUsageExtra(t *testing.T) {
	legacyKeys := []string{
		"ollama_cloud_usage_session",
		"ollama_cloud_usage_auto_refresh",
		"ollama_cloud_usage_snapshot",
		"opencode_go_usage_auto_refresh",
		"opencode_go_usage_snapshot",
	}
	extra := map[string]any{"ordinary": "kept"}
	for _, key := range legacyKeys {
		extra[key] = "retired-sensitive-state"
	}
	src := &service.Account{
		ID: 9, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "secret-key"},
		Extra:       extra,
	}
	got := AccountFromServiceShallow(src)
	for _, key := range legacyKeys {
		require.NotContains(t, got.Extra, key)
		require.Contains(t, src.Extra, key, "mapping must not mutate the cached account")
	}
	require.Equal(t, "kept", got.Extra["ordinary"])
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "retired-sensitive-state")
	require.NotContains(t, string(raw), "secret-key")
}

func TestAccountListItemFromAccount_RedactsRetiredUsageExtra(t *testing.T) {
	src := &Account{Extra: map[string]any{
		"ollama_cloud_usage_session": "retired-sensitive-state",
		"opencode_go_usage_snapshot": "retired-sensitive-state",
		"ordinary":                   "kept",
	}}

	got := AccountListItemFromAccount(src)
	require.NotContains(t, got.Extra, "ollama_cloud_usage_session")
	require.NotContains(t, got.Extra, "opencode_go_usage_snapshot")
	require.Equal(t, "kept", got.Extra["ordinary"])
	require.Contains(t, src.Extra, "ollama_cloud_usage_session")
}

func TestAccountFromServiceShallow_RedactsOpenAISessionCleanupState(t *testing.T) {
	legacyState := map[string]any{
		"status":          "failed",
		"last_success_at": "session_id=SECRET",
		"last_run_at":     "2026-09-02T12:00:00+08:00",
		"error_code":      "CUSTOM access_token=SECRET",
		"message":         "https://chatgpt.example/sessions/SECRET",
		"revoked_count":   -3,
		"failed_count":    -1,
		"private_field":   "bearer SECRET",
	}
	src := &service.Account{
		ID:       12,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Extra: map[string]any{
			service.OpenAISessionCleanupStateExtraKey: legacyState,
			"ordinary": "kept",
		},
	}

	got := AccountFromServiceShallow(src)
	require.NotNil(t, got)
	state, ok := got.Extra[service.OpenAISessionCleanupStateExtraKey].(map[string]any)
	require.True(t, ok)
	require.Equal(t, service.OpenAISessionCleanupStatusFailed, state["status"])
	require.Empty(t, state["last_success_at"])
	require.Equal(t, "2026-09-02T04:00:00Z", state["last_run_at"])
	require.Equal(t, "OPENAI_SESSION_CLEANUP_FAILED", state["error_code"])
	require.Equal(t, "the OpenAI session cleanup request failed", state["message"])
	require.Equal(t, 0, state["revoked_count"])
	require.Equal(t, 0, state["failed_count"])
	require.NotContains(t, state, "private_field")
	require.Equal(t, "kept", got.Extra["ordinary"])

	raw, err := json.Marshal(got)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "SECRET")
	// Mapping must not mutate the service account's nested state map.
	require.Equal(t, "session_id=SECRET", legacyState["last_success_at"])
}

func TestAccountFromServiceShallow_NilCredentialsOmitsStatus(t *testing.T) {
	src := &service.Account{ID: 1, Name: "n", Platform: "anthropic", Type: "oauth"}
	got := AccountFromServiceShallow(src)
	require.NotNil(t, got)
	require.Nil(t, got.Credentials)
	require.Nil(t, got.CredentialsStatus)
}
