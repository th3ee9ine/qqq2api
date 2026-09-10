package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestUpdateSettingsOpenAICodexVersionMode(t *testing.T) {
	for _, tc := range []struct {
		name        string
		stored      map[string]string
		body        map[string]any
		wantCode    int
		wantMode    string
		wantVersion string
	}{
		{name: "legacy default", body: map[string]any{"risk_control_enabled": true}, wantCode: http.StatusOK, wantMode: "auto"},
		{name: "save historical pin", body: map[string]any{"openai_codex_client_version_mode": " pinned ", "openai_codex_client_version": "0.100.0"}, wantCode: http.StatusOK, wantMode: "pinned", wantVersion: "0.100.0"},
		{name: "invalid mode", body: map[string]any{"openai_codex_client_version_mode": "latest"}, wantCode: http.StatusBadRequest},
		{name: "pin requires nonempty version", body: map[string]any{"openai_codex_client_version_mode": "pinned"}, wantCode: http.StatusBadRequest},
		{name: "pin rejects prerelease", body: map[string]any{"openai_codex_client_version_mode": "pinned", "openai_codex_client_version": "0.100.0-alpha.1"}, wantCode: http.StatusBadRequest},
		{name: "preserve pin when omitted", stored: map[string]string{service.SettingKeyOpenAICodexClientVersionMode: "pinned", service.SettingKeyOpenAICodexClientVersion: "0.100.0"}, body: map[string]any{"risk_control_enabled": true}, wantCode: http.StatusOK, wantMode: "pinned", wantVersion: "0.100.0"},
		{name: "null fields preserve pin", stored: map[string]string{service.SettingKeyOpenAICodexClientVersionMode: "pinned", service.SettingKeyOpenAICodexClientVersion: "0.100.0"}, body: map[string]any{"openai_codex_client_version_mode": nil, "openai_codex_client_version": nil}, wantCode: http.StatusOK, wantMode: "pinned", wantVersion: "0.100.0"},
		{name: "pin an existing version", stored: map[string]string{service.SettingKeyOpenAICodexClientVersion: "0.100.0"}, body: map[string]any{"openai_codex_client_version_mode": "pinned"}, wantCode: http.StatusOK, wantMode: "pinned", wantVersion: "0.100.0"},
		{name: "clear pinned version rejects entire write", stored: map[string]string{service.SettingKeyOpenAICodexClientVersionMode: "pinned", service.SettingKeyOpenAICodexClientVersion: "0.100.0"}, body: map[string]any{"openai_codex_client_version": "", "risk_control_enabled": true}, wantCode: http.StatusBadRequest},
		{name: "unpin and clear", stored: map[string]string{service.SettingKeyOpenAICodexClientVersionMode: "pinned", service.SettingKeyOpenAICodexClientVersion: "0.100.0"}, body: map[string]any{"openai_codex_client_version_mode": "auto", "openai_codex_client_version": ""}, wantCode: http.StatusOK, wantMode: "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := make(map[string]string, len(tc.stored))
			for key, value := range tc.stored {
				stored[key] = value
			}
			h, repo := newStepUpSwitchTestHandler(t, stored)
			rec := doUpdateSettings(t, h, tc.body, nil)
			require.Equal(t, tc.wantCode, rec.Code, rec.Body.String())
			if tc.wantCode != http.StatusOK {
				require.Contains(t, rec.Body.String(), "openai_codex_client_version")
				require.Equal(t, len(tc.stored), len(repo.values), "rejected updates must not partially persist")
				for key, value := range tc.stored {
					require.Equal(t, value, repo.values[key])
				}
				return
			}
			var payload struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			require.Equal(t, tc.wantMode, payload.Data["openai_codex_client_version_mode"])
			require.Equal(t, tc.wantVersion, payload.Data["openai_codex_client_version"])
		})
	}
}

func TestOmittedSettingKeysIncludesCodexVersionMode(t *testing.T) {
	require.Contains(t, omittedSettingKeys(map[string]json.RawMessage{}), service.SettingKeyOpenAICodexClientVersionMode)
	require.NotContains(t, omittedSettingKeys(map[string]json.RawMessage{
		"openai_codex_client_version_mode": json.RawMessage(`"pinned"`),
	}), service.SettingKeyOpenAICodexClientVersionMode)
	nullOmitted := omittedSettingKeys(map[string]json.RawMessage{
		"openai_codex_client_version_mode": json.RawMessage(`null`),
		"openai_codex_client_version":      json.RawMessage(` null `),
	})
	require.Contains(t, nullOmitted, service.SettingKeyOpenAICodexClientVersionMode)
	require.Contains(t, nullOmitted, service.SettingKeyOpenAICodexClientVersion)
}

func TestSettingsAuditIncludesCodexVersionMode(t *testing.T) {
	changed := diffSettings(&service.SystemSettings{OpenAICodexClientVersionMode: "auto"}, &service.SystemSettings{OpenAICodexClientVersionMode: "pinned"}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "openai_codex_client_version_mode")
}
