package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestGetReliabilityStatusReturnsTurnStateOnlyNoStoreProjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Ops: config.OpsConfig{Enabled: false}}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc := service.NewOpsService(nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := NewOpsHandler(svc)
	router := gin.New()
	router.GET("/status", handler.GetReliabilityStatus)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
	var envelope struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Zero(t, envelope.Code)
	require.Len(t, envelope.Data, 1)
	require.Contains(t, envelope.Data, "turn_state")
	for _, removed := range []string{
		"summary",
		"enabled",
		"timestamp",
		"cooldowns",
		"account_availability",
		"traffic",
		"connection",
		"limits",
		"runtime",
		"diagnostics",
		"notes",
		"fallback",
	} {
		require.NotContains(t, envelope.Data, removed)
	}
	require.NotContains(t, string(recorder.Body.Bytes()), "x-codex-turn-state")
}

func TestGetReliabilityStatusWithoutServiceReturnsNoStore503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/status", NewOpsHandler(nil).GetReliabilityStatus)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func TestWriteReliabilityStatusErrorReturnsGatewayTimeoutForInternalDeadline(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/status", nil)

	writeReliabilityStatusError(c, context.DeadlineExceeded)

	require.Equal(t, http.StatusGatewayTimeout, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Reliability status collection timed out")
}

func TestCodexTurnStateRuntimeSettingsHandlersDefaultAndPersistCompleteSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := newTestSettingRepo()
	svc := service.NewOpsService(nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := NewOpsHandler(svc)
	router := gin.New()
	router.GET("/turn-state-settings", handler.GetCodexTurnStateRuntimeSettings)
	router.PUT("/turn-state-settings", handler.UpdateCodexTurnStateRuntimeSettings)

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/turn-state-settings", nil))
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Equal(t, "no-store", getRecorder.Header().Get("Cache-Control"))
	var getEnvelope struct {
		Data service.CodexTurnStateRuntimeSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(getRecorder.Body.Bytes(), &getEnvelope))
	require.True(t, getEnvelope.Data.ProbeEnabled)
	require.True(t, getEnvelope.Data.InjectionEnabled)

	putRecorder := httptest.NewRecorder()
	putRequest := httptest.NewRequest(http.MethodPut, "/turn-state-settings", bytes.NewBufferString(`{"probe_enabled":false,"injection_enabled":true}`))
	putRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(putRecorder, putRequest)
	require.Equal(t, http.StatusOK, putRecorder.Code)
	var putEnvelope struct {
		Data service.CodexTurnStateRuntimeSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(putRecorder.Body.Bytes(), &putEnvelope))
	require.False(t, putEnvelope.Data.ProbeEnabled)
	require.True(t, putEnvelope.Data.InjectionEnabled)
	require.Equal(t, "false", repo.values[service.SettingKeyCodexTurnStateProbeEnabled])
	require.Equal(t, "true", repo.values[service.SettingKeyCodexTurnStateCacheInjectionEnabled])
}

func TestUpdateCodexTurnStateRuntimeSettingsRequiresBothBooleanFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewOpsHandler(service.NewOpsService(nil, newTestSettingRepo(), nil, nil, nil, nil, nil, nil, nil, nil, nil))
	router := gin.New()
	router.PUT("/turn-state-settings", handler.UpdateCodexTurnStateRuntimeSettings)

	for _, body := range []string{
		`{"probe_enabled":false}`,
		`{"probe_enabled":"false","injection_enabled":true}`,
		`{}`,
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/turn-state-settings", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
}
