package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func assertCodexTurnStateAutomaticResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	for _, key := range []string{"openai_codex_turn_state", "openai_codex_turn_state_enabled", "openai_codex_turn_state_configured", "openai_codex_turn_state_set_at_ms", "openai_codex_turn_state_status"} {
		require.NotContains(t, payload.Data, key)
	}
	require.NotContains(t, rec.Body.String(), "private-turn-state")
	require.Contains(t, payload.Data, "openai_codex_turn_state_auto_enabled")
	require.Contains(t, payload.Data, "openai_codex_turn_state_models")
	require.Contains(t, payload.Data, "openai_codex_turn_state_default_model")
}
func TestOpenAICodexTurnStateAutomaticSettingsOmission(t *testing.T) {
	for _, body := range []map[string]any{{"risk_control_enabled": true}, {"openai_codex_turn_state_auto_enabled": nil, "openai_codex_turn_state_models": nil}} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyOpenAICodexTurnState: "private-turn-state", service.SettingKeyOpenAICodexTurnStateEnabled: "true",
			service.SettingKeyOpenAICodexTurnStateAutoEnabled: "true", service.SettingKeyOpenAICodexTurnStateModels: "gpt-5.*",
		})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexTurnStateAutoEnabled])
		require.Equal(t, "gpt-5.*", repo.values[service.SettingKeyOpenAICodexTurnStateModels])
		assertCodexTurnStateAutomaticResponse(t, rec)
		getRec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(getRec)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
		h.GetSettings(c)
		require.Equal(t, http.StatusOK, getRec.Code)
		assertCodexTurnStateAutomaticResponse(t, getRec)
	}
}
func TestOpenAICodexTurnStateLegacyInputsIgnored(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyOpenAICodexTurnState: "private-turn-state", service.SettingKeyOpenAICodexTurnStateEnabled: "true"})
	rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state": "replacement\r\nvalue", "openai_codex_turn_state_enabled": true, "openai_codex_turn_state_set_at_ms": 123}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "private-turn-state", repo.values[service.SettingKeyOpenAICodexTurnState], "retired fields must not be written")
	require.NotEqual(t, "true", repo.values[service.SettingKeyOpenAICodexTurnStateAutoEnabled], "legacy switch must not enable quota-spending probes")
	assertCodexTurnStateAutomaticResponse(t, rec)
}
func TestOpenAICodexTurnStateAutomaticSwitchAndScope(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	for _, enabled := range []bool{true, false} {
		rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_auto_enabled": enabled, "openai_codex_turn_state_models": " GPT-5.*,gpt-5.*, GPT-5.5 "}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, strconv.FormatBool(enabled), repo.values[service.SettingKeyOpenAICodexTurnStateAutoEnabled])
		require.Equal(t, "gpt-5.*,gpt-5.5", repo.values[service.SettingKeyOpenAICodexTurnStateModels])
		assertCodexTurnStateAutomaticResponse(t, rec)
	}
	for _, scope := range []string{"gpt-*bad", "gpt-5 with spaces", strings.Repeat("a", 1025)} {
		rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_models": scope}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "gpt-5.*,gpt-5.5", repo.values[service.SettingKeyOpenAICodexTurnStateModels])
	}
}
func TestOpenAICodexTurnStateAutomaticSettingsAudit(t *testing.T) {
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{OpenAICodexTurnStateAutoEnabled: true, OpenAICodexTurnStateModels: "gpt-5.*", OpenAICodexTurnStateDefaultModel: "custom/probe"}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "openai_codex_turn_state_auto_enabled")
	require.Contains(t, changed, "openai_codex_turn_state_models")
	require.Contains(t, changed, "openai_codex_turn_state_default_model")
	require.NotContains(t, changed, "openai_codex_turn_state")
	require.NotContains(t, changed, "openai_codex_turn_state_enabled")
}

func TestOpenAICodexTurnStateEditableDefaultModel(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	key := service.SettingKeyOpenAICodexTurnStateDefaultModel
	for _, tc := range []struct{ input, expected string }{
		{" custom/probe-1 ", "custom/probe-1"}, {"", "gpt-5.5"}, {"custom/probe-2", "custom/probe-2"},
	} {
		// Warm the runtime cache before editing; saving must invalidate it.
		h.settingService.GetOpenAICodexTurnState(context.Background())
		rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_default_model": tc.input}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, tc.expected, repo.values[key])
		require.Equal(t, tc.expected, h.settingService.GetOpenAICodexTurnState(context.Background()).DefaultModel)
		var payload struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		require.Equal(t, tc.expected, payload.Data["openai_codex_turn_state_default_model"])
	}
	for _, body := range []map[string]any{{"risk_control_enabled": true}, {"openai_codex_turn_state_default_model": nil}} {
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "custom/probe-2", repo.values[key])
	}
	for _, invalid := range []string{"gpt-5*", "two models", "gpt-5.5,gpt-5.6-sol", "gpt\nmodel", strings.Repeat("a", 129)} {
		rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_default_model": invalid}, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, "custom/probe-2", repo.values[key])
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Contains(t, rec.Body.String(), `"openai_codex_turn_state_default_model":"custom/probe-2"`)
}
