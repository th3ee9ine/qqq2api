package admin

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestUpdateSettingsPartialPayloadKeepsOpenAICodexOriginator(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexOriginator: "codex-tui",
	})

	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "codex-tui", repo.values[service.SettingKeyOpenAICodexOriginator])
}

func TestOmittedSettingKeysIncludesAbsentOpenAICodexIdentityPointers(t *testing.T) {
	omitted := omittedSettingKeys(map[string]json.RawMessage{
		"risk_control_enabled": json.RawMessage("true"),
	})

	for _, key := range []string{
		service.SettingKeyOpenAICodexOriginator,
		service.SettingKeyOpenAICodexUserAgent,
		service.SettingKeyOpenAICodexClientVersion,
		service.SettingKeyOpenAICodexVersionAutoSyncEnabled,
		service.SettingKeyEnableOpenAIAccountLocalDeviceIdentity,
	} {
		require.Contains(t, omitted, key)
	}

	sent := omittedSettingKeys(map[string]json.RawMessage{
		"openai_codex_originator": json.RawMessage(`"codex-tui"`),
	})
	require.NotContains(t, sent, service.SettingKeyOpenAICodexOriginator)
	require.Contains(t, sent, service.SettingKeyOpenAICodexUserAgent)
}

func TestUpdateSettingsWritesNormalizedOpenAICodexOriginatorAndReturnsDefaults(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"openai_codex_originator": "  codex-tui  ",
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "codex-tui", repo.values[service.SettingKeyOpenAICodexOriginator])

	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, "codex-tui", payload.Data["openai_codex_originator"])
	require.Equal(t, "Codex Desktop", payload.Data["openai_codex_originator_default"])
	require.Equal(t, "0.200.1", payload.Data["openai_codex_client_version_default"])
	require.Equal(t,
		"Codex Desktop/0.200.1 (Mac OS 26.2.0; arm64) unknown (Codex Desktop; 26.820.60940)",
		payload.Data["openai_codex_user_agent_default"],
	)
}

func TestUpdateSettingsRejectsUnsafeOpenAICodexIdentityHeaders(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "originator slash", field: "openai_codex_originator", value: "bad/originator"},
		{name: "originator newline", field: "openai_codex_originator", value: "bad\noriginator"},
		{name: "originator non ascii", field: "openai_codex_originator", value: "Codex 桌面"},
		{name: "originator trailing newline", field: "openai_codex_originator", value: "Codex Desktop\n"},
		{name: "user agent CRLF", field: "openai_codex_user_agent", value: "Codex Desktop/0.150.1\r\nX-Injected: true"},
		{name: "user agent control byte", field: "openai_codex_user_agent", value: "Codex Desktop/0.150.1\x00suffix"},
		{name: "user agent non ascii", field: "openai_codex_user_agent", value: "Codex 桌面/0.150.1"},
		{name: "user agent leading tab", field: "openai_codex_user_agent", value: "\tCodex Desktop/0.150.1"},
		{name: "user agent trailing newline", field: "openai_codex_user_agent", value: "Codex Desktop/0.150.1\n"},
		{name: "user agent over length whitespace", field: "openai_codex_user_agent", value: strings.Repeat(" ", 513)},
		{name: "user agent missing client version shape", field: "openai_codex_user_agent", value: "my-gateway"},
		{name: "user agent invalid version", field: "openai_codex_user_agent", value: "my-gateway/latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
			rec := doUpdateSettings(t, h, map[string]any{tt.field: tt.value}, nil)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), tt.field)
			require.NotContains(t, repo.values, tt.field)
		})
	}
}
