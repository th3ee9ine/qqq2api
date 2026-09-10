package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountLocalDeviceVersionImportFormats(t *testing.T) {
	tests := []struct {
		name        string
		extra       map[string]any
		credentials map[string]any
	}{
		{
			name:  "canonical Extra",
			extra: map[string]any{OpenAILocalDeviceVersionExtraKey: "0.125.0"},
		},
		{
			name:        "canonical Credentials",
			credentials: map[string]any{OpenAILocalDeviceVersionExtraKey: "0.125.0"},
		},
		{
			name:  "device alias",
			extra: map[string]any{"openai_device_version": "0.125.0"},
		},
		{
			name:        "session alias",
			credentials: map[string]any{"openai_session_client_version": "0.125.0"},
		},
		{
			name:  "nested map any",
			extra: map[string]any{"openai_local_device_session": map[string]any{"version": "0.125.0"}},
		},
		{
			name:        "nested map string",
			credentials: map[string]any{"openai_device_session": map[string]string{"Version": "0.125.0"}},
		},
		{
			name:  "nested raw JSON",
			extra: map[string]any{"local_device_session": json.RawMessage(`{"client_version":"0.125.0"}`)},
		},
		{
			name:        "nested JSON string",
			credentials: map[string]any{"openai_current_device_session": `{"clientVersion":"0.125.0"}`},
		},
		{
			name:  "local device container",
			extra: map[string]any{"openai_local_device": map[string]any{"version": "0.125.0"}},
		},
		{
			name:        "device container",
			credentials: map[string]any{"openai_device": map[string]string{"version": "0.125.0"}},
		},
		{
			name:  "current session container",
			extra: map[string]any{"current_device_session": map[string]any{"version": "0.125.0"}},
		},
		{
			name: "legacy credential triple",
			credentials: map[string]any{
				"user_agent": "codex-tui/0.125.0 (Linux; x86_64)",
				"originator": "codex-tui",
				"version":    "0.125.0",
			},
		},
		{
			name: "legacy credential client version",
			credentials: map[string]any{
				"user_agent":     "codex-tui/0.125.0 (Linux; x86_64)",
				"originator":     "codex-tui",
				"client_version": "0.125.0",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Extra: tt.extra, Credentials: tt.credentials}
			require.Equal(t, "0.125.0", account.GetOpenAILocalDeviceVersion())
		})
	}
}

func TestAccountLocalDeviceVersionPreservesExplicitInvalidValues(t *testing.T) {
	for _, raw := range []string{"invalid", "0.125.0-beta.1", " 0.125.0 ", "\r\n", "0.125.0\nInjected: true"} {
		t.Run(raw, func(t *testing.T) {
			account := &Account{
				Platform: PlatformOpenAI,
				Extra: map[string]any{
					OpenAILocalDeviceVersionExtraKey: raw,
					"openai_device_version":          "0.125.0",
				},
				Credentials: map[string]any{OpenAILocalDeviceVersionExtraKey: "0.200.1"},
			}
			require.Equal(t, raw, account.GetOpenAILocalDeviceVersion())
			account.Extra = map[string]any{"openai_local_device_session": map[string]any{"version": raw, "client_version": "0.125.0"}}
			account.Credentials = nil
			require.Equal(t, raw, account.GetOpenAILocalDeviceVersion())
		})
	}
	for _, raw := range []any{nil, 125, 0.125, true, []string{"0.125.0"}, map[string]any{"value": "0.125.0"}} {
		encoded, err := json.Marshal(raw)
		require.NoError(t, err)
		t.Run(string(encoded), func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Extra: map[string]any{OpenAILocalDeviceVersionExtraKey: raw}}
			require.Equal(t, "<invalid-version>", account.GetOpenAILocalDeviceVersion())
			account.Extra = nil
			account.Credentials = map[string]any{"local_device_session": map[string]any{"version": raw, "client_version": "0.125.0"}}
			require.Equal(t, "<invalid-version>", account.GetOpenAILocalDeviceVersion())
		})
	}
}

func TestAccountLocalDeviceVersionIgnoresUnrelatedGenericVersions(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Extra:    map[string]any{"version": "1.0.0"},
		Credentials: map[string]any{
			"version":        "2.0.0",
			"client_version": "3.0.0",
		},
	}
	require.Empty(t, account.GetOpenAILocalDeviceVersion())
	account.Credentials["user_agent"] = "codex-tui/0.125.0 (Linux; x86_64)"
	require.Empty(t, account.GetOpenAILocalDeviceVersion(), "a generic version without a legacy originator is not local identity")
	account.Credentials["originator"] = "codex-tui"
	account.Extra[OpenAILocalDeviceUserAgentExtraKey] = "codex_vscode/0.125.0 (Linux; x86_64)"
	account.Extra[OpenAILocalDeviceOriginatorExtraKey] = "codex_vscode"
	require.Empty(t, account.GetOpenAILocalDeviceVersion(), "an explicit local UA does not inherit the legacy schema version")
}

func TestAccountLocalDeviceVersionEmptyAndPrecedence(t *testing.T) {
	var account *Account
	require.Empty(t, account.GetOpenAILocalDeviceVersion())
	account = &Account{Platform: PlatformAnthropic, Extra: map[string]any{OpenAILocalDeviceVersionExtraKey: "0.125.0"}}
	require.Empty(t, account.GetOpenAILocalDeviceVersion())
	account.Platform = PlatformOpenAI
	account.Extra[OpenAILocalDeviceVersionExtraKey] = ""
	require.Empty(t, account.GetOpenAILocalDeviceVersion())
	account.Extra["openai_device_version"] = "0.100.0"
	account.Credentials = map[string]any{OpenAILocalDeviceVersionExtraKey: "0.125.0"}
	require.Equal(t, "0.125.0", account.GetOpenAILocalDeviceVersion(), "canonical Credentials wins over Extra alias")
	account.Extra[OpenAILocalDeviceVersionExtraKey] = "0.200.1"
	require.Equal(t, "0.200.1", account.GetOpenAILocalDeviceVersion(), "canonical Extra wins over canonical Credentials")
}
