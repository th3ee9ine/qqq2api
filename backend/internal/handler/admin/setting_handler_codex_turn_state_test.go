package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestOpenAICodexTurnStateSettingsOmissionAndSecretResponse(t *testing.T) {
	for _, body := range []map[string]any{
		{"risk_control_enabled": true},
		{"openai_codex_turn_state": nil, "openai_codex_turn_state_enabled": nil, "openai_codex_turn_state_models": nil},
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyOpenAICodexTurnState:            "private-turn-state",
			service.SettingKeyOpenAICodexTurnStateEnabled:     "true",
			service.SettingKeyOpenAICodexTurnStateAutoEnabled: "true",
			service.SettingKeyOpenAICodexTurnStateModels:      "gpt-5.*",
			service.SettingKeyOpenAICodexTurnStateSetAtMS:     "12345",
		})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "private-turn-state", repo.values[service.SettingKeyOpenAICodexTurnState])
		require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexTurnStateEnabled])
		require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexTurnStateAutoEnabled])
		require.Equal(t, "gpt-5.*", repo.values[service.SettingKeyOpenAICodexTurnStateModels])
		require.Equal(t, "12345", repo.values[service.SettingKeyOpenAICodexTurnStateSetAtMS])
		assertCodexTurnStateSecretResponse(t, rec, true)

		getRec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(getRec)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
		h.GetSettings(c)
		require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())
		assertCodexTurnStateSecretResponse(t, getRec, true)
	}
}

func assertCodexTurnStateSecretResponse(t *testing.T, rec *httptest.ResponseRecorder, configured bool) {
	t.Helper()
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.NotContains(t, payload.Data, "openai_codex_turn_state")
	require.NotContains(t, rec.Body.String(), "private-turn-state")
	require.Equal(t, configured, payload.Data["openai_codex_turn_state_configured"])
	require.Contains(t, payload.Data, "openai_codex_turn_state_status")
}

func TestOpenAICodexTurnStateSettingsReplaceDisableAndClear(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	before := time.Now().UnixMilli()
	rec := doUpdateSettings(t, h, map[string]any{
		"openai_codex_turn_state":           "private-turn-state",
		"openai_codex_turn_state_enabled":   true,
		"openai_codex_turn_state_models":    " GPT-5.*,gpt-5.*, GPT-4.1 ",
		"openai_codex_turn_state_set_at_ms": 1, // Read-only: must not set the stored clock.
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "private-turn-state", repo.values[service.SettingKeyOpenAICodexTurnState])
	require.Equal(t, "gpt-5.*,gpt-4.1", repo.values[service.SettingKeyOpenAICodexTurnStateModels])
	setAt, err := strconv.ParseInt(repo.values[service.SettingKeyOpenAICodexTurnStateSetAtMS], 10, 64)
	require.NoError(t, err)
	require.GreaterOrEqual(t, setAt, before)
	require.LessOrEqual(t, setAt, time.Now().UnixMilli())
	assertCodexTurnStateSecretResponse(t, rec, true)

	rec = doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_enabled": false}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "private-turn-state", repo.values[service.SettingKeyOpenAICodexTurnState])
	require.Equal(t, strconv.FormatInt(setAt, 10), repo.values[service.SettingKeyOpenAICodexTurnStateSetAtMS], "switch changes must not reset token age")

	rec = doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state": ""}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Empty(t, repo.values[service.SettingKeyOpenAICodexTurnState])
	require.Equal(t, "0", repo.values[service.SettingKeyOpenAICodexTurnStateSetAtMS])
	assertCodexTurnStateSecretResponse(t, rec, false)
}

func TestOpenAICodexTurnStateSettingsValidation(t *testing.T) {
	for _, tc := range []struct{ field, value string }{
		{"openai_codex_turn_state", "token\r\nX-Injected: yes"},
		{"openai_codex_turn_state", "token\x00suffix"},
		{"openai_codex_turn_state", "令牌"},
		{"openai_codex_turn_state", strings.Repeat("a", 4097)},
		{"openai_codex_turn_state_models", "gpt-*bad"},
		{"openai_codex_turn_state_models", "gpt-5 with spaces"},
	} {
		t.Run(tc.field+"/"+strconv.Itoa(len(tc.value)), func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyOpenAICodexTurnState: "private-turn-state"})
			rec := doUpdateSettings(t, h, map[string]any{tc.field: tc.value}, nil)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Equal(t, "private-turn-state", repo.values[service.SettingKeyOpenAICodexTurnState])
		})
	}
}

func TestOpenAICodexTurnStateSettingsSameTokenPreservesAge(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexTurnState:        "private-turn-state",
		service.SettingKeyOpenAICodexTurnStateSetAtMS: "12345",
	})
	rec := doUpdateSettings(t, h, map[string]any{
		"openai_codex_turn_state":         " private-turn-state ",
		"openai_codex_turn_state_enabled": true,
		"openai_codex_turn_state_models":  "gpt-*",
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "12345", repo.values[service.SettingKeyOpenAICodexTurnStateSetAtMS])
	assertCodexTurnStateSecretResponse(t, rec, true)
}

func TestOpenAICodexTurnStateSettingsAuditRedactsToken(t *testing.T) {
	token := "new-private-turn-state"
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{
		OpenAICodexTurnState: token, OpenAICodexTurnStateEnabled: true, OpenAICodexTurnStateModels: "gpt-5.*",
	}, nil, nil, UpdateSettingsRequest{OpenAICodexTurnState: &token})
	require.Contains(t, changed, "openai_codex_turn_state")
	require.Contains(t, changed, "openai_codex_turn_state_enabled")
	require.Contains(t, changed, "openai_codex_turn_state_models")
	require.NotContains(t, strings.Join(changed, ","), token)
}

func TestOpenAICodexTurnStateAutomaticSwitchIndependent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, enabled := range []bool{true, false} {
		rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_auto_enabled": enabled}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, strconv.FormatBool(enabled), repo.values[service.SettingKeyOpenAICodexTurnStateAutoEnabled])
		require.NotEqual(t, "true", repo.values[service.SettingKeyOpenAICodexTurnStateEnabled])
		require.Contains(t, rec.Body.String(), `"openai_codex_turn_state_enabled":false`)
		require.Contains(t, rec.Body.String(), `"openai_codex_turn_state_auto_enabled":`+strconv.FormatBool(enabled))
	}
}
