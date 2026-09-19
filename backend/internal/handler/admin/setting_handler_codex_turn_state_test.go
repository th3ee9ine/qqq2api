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
	require.Contains(t, payload.Data, "openai_codex_turn_state_auto_interval_minutes")
	require.Contains(t, payload.Data, "openai_codex_turn_state_models")
	require.Contains(t, payload.Data, "openai_codex_turn_state_default_model")
	require.Contains(t, payload.Data, "openai_codex_turn_state_proxy_urls")
	require.Contains(t, payload.Data, "openai_codex_turn_state_proxy_urls_valid")
	require.Contains(t, payload.Data, "openai_codex_turn_state_proxy_pool_configured")
	require.Contains(t, payload.Data, "openai_codex_turn_state_proxy_pool_count")
	require.Contains(t, payload.Data, "openai_codex_turn_state_proxy_ids")
	require.Contains(t, payload.Data, "openai_codex_turn_state_proxy_id")
	require.Contains(t, payload.Data, "openai_codex_turn_state_proxy_ids_valid")
}

func TestOpenAICodexTurnStateAutoIntervalRoundTripAndValidation(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_auto_interval_minutes": 7}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "7", repo.values[service.SettingKeyOpenAICodexTurnStateAutoIntervalMinutes])
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.EqualValues(t, 7, payload.Data["openai_codex_turn_state_auto_interval_minutes"])

	for _, invalid := range []int{0, 61} {
		invalidRec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_auto_interval_minutes": invalid}, nil)
		require.Equal(t, http.StatusBadRequest, invalidRec.Code, invalidRec.Body.String())
		require.Equal(t, "7", repo.values[service.SettingKeyOpenAICodexTurnStateAutoIntervalMinutes])
	}
}

func TestOpenAICodexTurnStateProxyURLsRootSettingsRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	body := map[string]any{"openai_codex_turn_state_proxy_urls": []string{
		"socks5://user:p%40ss@PROXY.example:01080",
		"socks5://user:p%40ss@proxy.example:1080",
		"socks5://second:secret@second.example:443",
	}}
	rec := doUpdateSettings(t, h, body, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, `["socks5://user:p%40ss@proxy.example:1080","socks5://second:secret@second.example:443"]`, repo.values[service.SettingKeyOpenAICodexTurnStateProxyURLs])

	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, []any{"socks5://user:p%40ss@proxy.example:1080", "socks5://second:secret@second.example:443"}, payload.Data["openai_codex_turn_state_proxy_urls"])
	require.Equal(t, true, payload.Data["openai_codex_turn_state_proxy_urls_valid"])
	require.Equal(t, true, payload.Data["openai_codex_turn_state_proxy_pool_configured"])
	require.EqualValues(t, 2, payload.Data["openai_codex_turn_state_proxy_pool_count"])
}

func TestOpenAICodexTurnStateProxyURLsOmittedNullAndClear(t *testing.T) {
	const stored = `["socks5://user:private-secret@proxy.example:1080"]`
	for _, body := range []map[string]any{
		{"risk_control_enabled": true},
		{"openai_codex_turn_state_proxy_urls": nil},
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyOpenAICodexTurnStateProxyURLs: stored})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, stored, repo.values[service.SettingKeyOpenAICodexTurnStateProxyURLs])
	}

	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyOpenAICodexTurnStateProxyURLs: stored})
	rec := doUpdateSettings(t, h, map[string]any{"openai_codex_turn_state_proxy_urls": []string{}}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "[]", repo.values[service.SettingKeyOpenAICodexTurnStateProxyURLs])
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, []any{}, payload.Data["openai_codex_turn_state_proxy_urls"])
	require.Equal(t, false, payload.Data["openai_codex_turn_state_proxy_pool_configured"])
	require.EqualValues(t, 0, payload.Data["openai_codex_turn_state_proxy_pool_count"])
}

func TestOpenAICodexTurnStateMalformedProxyURLsRedactedAndInvalid(t *testing.T) {
	const malformed = "private-malformed-proxy-secret"
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyOpenAICodexTurnStateProxyURLs: malformed})
	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, malformed, repo.values[service.SettingKeyOpenAICodexTurnStateProxyURLs])
	require.NotContains(t, rec.Body.String(), malformed)
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, []any{}, payload.Data["openai_codex_turn_state_proxy_urls"])
	require.Equal(t, false, payload.Data["openai_codex_turn_state_proxy_urls_valid"])
	require.Equal(t, false, payload.Data["openai_codex_turn_state_proxy_pool_configured"])
	require.EqualValues(t, 0, payload.Data["openai_codex_turn_state_proxy_pool_count"])
}

func TestOpenAICodexTurnStateProxyURLsRejectWithoutCredentialLeakOrOverwrite(t *testing.T) {
	const stored = `["socks5://old:old-secret@old.example:1080"]`
	const submittedSecret = "submitted-secret-must-not-leak"
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyOpenAICodexTurnStateProxyURLs: stored})
	rec := doUpdateSettings(t, h, map[string]any{
		"openai_codex_turn_state_proxy_urls": []string{"http://user:" + submittedSecret + "@new.example:80"},
	}, nil)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), submittedSecret)
	require.Equal(t, stored, repo.values[service.SettingKeyOpenAICodexTurnStateProxyURLs])
}

func TestOpenAICodexTurnStateMalformedStoredProxyPoolIsRedactedAndMarkedInvalid(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexTurnStateProxyIDs: "not-json-private-value",
		service.SettingKeyOpenAICodexTurnStateProxyID:  "17",
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, []any{}, payload.Data["openai_codex_turn_state_proxy_ids"])
	require.EqualValues(t, 0, payload.Data["openai_codex_turn_state_proxy_id"])
	require.Equal(t, false, payload.Data["openai_codex_turn_state_proxy_ids_valid"])
	require.NotContains(t, rec.Body.String(), "not-json-private-value")
}

func TestOpenAICodexTurnStateMalformedStoredLegacyProxyIsRedactedAndMarkedInvalid(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexTurnStateProxyIDs: "[]",
		service.SettingKeyOpenAICodexTurnStateProxyID:  "malformed-private-value",
	})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, []any{}, payload.Data["openai_codex_turn_state_proxy_ids"])
	require.EqualValues(t, 0, payload.Data["openai_codex_turn_state_proxy_id"])
	require.Equal(t, false, payload.Data["openai_codex_turn_state_proxy_ids_valid"])
	require.NotContains(t, rec.Body.String(), "malformed-private-value")
}

func TestOpenAICodexTurnStateAutomaticSettingsOmission(t *testing.T) {
	for _, body := range []map[string]any{{"risk_control_enabled": true}, {"openai_codex_turn_state_auto_enabled": nil, "openai_codex_turn_state_auto_interval_minutes": nil, "openai_codex_turn_state_models": nil}} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyOpenAICodexTurnState: "private-turn-state", service.SettingKeyOpenAICodexTurnStateEnabled: "true",
			service.SettingKeyOpenAICodexTurnStateAutoEnabled: "true", service.SettingKeyOpenAICodexTurnStateAutoIntervalMinutes: "9", service.SettingKeyOpenAICodexTurnStateModels: "gpt-5.*",
		})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexTurnStateAutoEnabled])
		require.Equal(t, "9", repo.values[service.SettingKeyOpenAICodexTurnStateAutoIntervalMinutes])
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
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{OpenAICodexTurnStateAutoEnabled: true, OpenAICodexTurnStateAutoIntervalMinutes: 7, OpenAICodexTurnStateModels: "gpt-5.*", OpenAICodexTurnStateDefaultModel: "custom/probe", OpenAICodexTurnStateProxyURLs: []string{"socks5://audit-secret:must-not-be-logged@proxy.example:1080"}, OpenAICodexTurnStateProxyIDs: []int64{7, 9}, OpenAICodexTurnStateProxyID: 7}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "openai_codex_turn_state_auto_enabled")
	require.Contains(t, changed, "openai_codex_turn_state_auto_interval_minutes")
	require.Contains(t, changed, "openai_codex_turn_state_models")
	require.Contains(t, changed, "openai_codex_turn_state_default_model")
	require.Contains(t, changed, "openai_codex_turn_state_proxy_urls")
	require.NotContains(t, strings.Join(changed, ","), "must-not-be-logged")
	require.Contains(t, changed, "openai_codex_turn_state_proxy_ids")
	require.Contains(t, changed, "openai_codex_turn_state_proxy_id")
	require.NotContains(t, changed, "openai_codex_turn_state")
	require.NotContains(t, changed, "openai_codex_turn_state_enabled")
}

func TestOpenAICodexTurnStateProxyPoolSettingsCompatibility(t *testing.T) {
	type responsePayload struct {
		Data struct {
			ProxyIDs      []int64 `json:"openai_codex_turn_state_proxy_ids"`
			ProxyID       int64   `json:"openai_codex_turn_state_proxy_id"`
			ProxyIDsValid bool    `json:"openai_codex_turn_state_proxy_ids_valid"`
		} `json:"data"`
	}
	decode := func(t *testing.T, rec *httptest.ResponseRecorder) responsePayload {
		t.Helper()
		var payload responsePayload
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
		return payload
	}

	for _, tc := range []struct {
		name       string
		body       map[string]any
		wantJSON   string
		wantLegacy string
		wantIDs    []int64
	}{
		{
			name: "new pool wins and synchronizes legacy",
			body: map[string]any{
				"openai_codex_turn_state_proxy_ids": []int64{9, 3, 9},
				"openai_codex_turn_state_proxy_id":  int64(77),
			},
			wantJSON: "[9,3]", wantLegacy: "9", wantIDs: []int64{9, 3},
		},
		{
			name:       "legacy-only request synchronizes pool",
			body:       map[string]any{"openai_codex_turn_state_proxy_id": int64(17)},
			wantJSON:   "[17]",
			wantLegacy: "17",
			wantIDs:    []int64{17},
		},
		{
			name:       "empty new pool clears legacy",
			body:       map[string]any{"openai_codex_turn_state_proxy_ids": []int64{}},
			wantJSON:   "[]",
			wantLegacy: "0",
			wantIDs:    []int64{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := newStepUpSwitchTestHandler(t, map[string]string{
				service.SettingKeyOpenAICodexTurnStateProxyIDs: "[5,4]",
				service.SettingKeyOpenAICodexTurnStateProxyID:  "5",
			})
			rec := doUpdateSettings(t, h, tc.body, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, tc.wantJSON, repo.values[service.SettingKeyOpenAICodexTurnStateProxyIDs])
			require.Equal(t, tc.wantLegacy, repo.values[service.SettingKeyOpenAICodexTurnStateProxyID])
			payload := decode(t, rec)
			require.Equal(t, tc.wantIDs, payload.Data.ProxyIDs)
			require.Equal(t, tc.wantLegacy, strconv.FormatInt(payload.Data.ProxyID, 10))
			require.True(t, payload.Data.ProxyIDsValid)
		})
	}
}

func TestOpenAICodexTurnStateProxyPoolRepairReturnsValid(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexTurnStateProxyIDs: "malformed-private-value",
		service.SettingKeyOpenAICodexTurnStateProxyID:  "17",
	})
	rec := doUpdateSettings(t, h, map[string]any{
		"openai_codex_turn_state_proxy_ids": []int64{},
	}, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "[]", repo.values[service.SettingKeyOpenAICodexTurnStateProxyIDs])
	require.Equal(t, "0", repo.values[service.SettingKeyOpenAICodexTurnStateProxyID])
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, true, payload.Data["openai_codex_turn_state_proxy_ids_valid"])
	require.NotContains(t, rec.Body.String(), "malformed-private-value")
}

func TestOpenAICodexTurnStateMalformedProxyPoolRemainsInvalidWhenOmitted(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyOpenAICodexTurnStateProxyIDs: "malformed-private-value",
		service.SettingKeyOpenAICodexTurnStateProxyID:  "17",
	})
	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "malformed-private-value", repo.values[service.SettingKeyOpenAICodexTurnStateProxyIDs])
	require.Equal(t, "17", repo.values[service.SettingKeyOpenAICodexTurnStateProxyID])
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	require.Equal(t, false, payload.Data["openai_codex_turn_state_proxy_ids_valid"])
	require.Equal(t, []any{}, payload.Data["openai_codex_turn_state_proxy_ids"])
	require.EqualValues(t, 0, payload.Data["openai_codex_turn_state_proxy_id"])
	require.NotContains(t, rec.Body.String(), "malformed-private-value")
}

func TestOpenAICodexTurnStateProxyPoolOmittedOrNullPreservesPair(t *testing.T) {
	for _, body := range []map[string]any{
		{"risk_control_enabled": true},
		{"openai_codex_turn_state_proxy_ids": nil, "openai_codex_turn_state_proxy_id": nil},
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyOpenAICodexTurnStateProxyIDs: "[8,6]",
			service.SettingKeyOpenAICodexTurnStateProxyID:  "8",
		})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, "[8,6]", repo.values[service.SettingKeyOpenAICodexTurnStateProxyIDs])
		require.Equal(t, "8", repo.values[service.SettingKeyOpenAICodexTurnStateProxyID])
	}
}

func TestOpenAICodexTurnStateProxyPoolRejectsInvalidWithoutOverwrite(t *testing.T) {
	for _, body := range []map[string]any{
		{"openai_codex_turn_state_proxy_ids": []int64{1, 0}},
		{"openai_codex_turn_state_proxy_id": int64(-1)},
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{
			service.SettingKeyOpenAICodexTurnStateProxyIDs: "[8,6]",
			service.SettingKeyOpenAICodexTurnStateProxyID:  "8",
		})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, "[8,6]", repo.values[service.SettingKeyOpenAICodexTurnStateProxyIDs])
		require.Equal(t, "8", repo.values[service.SettingKeyOpenAICodexTurnStateProxyID])
	}
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
