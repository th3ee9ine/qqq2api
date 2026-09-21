package admin

import (
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

func TestGetReliabilityStatusReturnsFlatNoStoreProjection(t *testing.T) {
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
	require.NotContains(t, envelope.Data, "summary")
	require.Contains(t, envelope.Data, "cooldowns")
	require.Contains(t, envelope.Data, "connection")
	require.Contains(t, envelope.Data, "turn_state")
	require.Contains(t, envelope.Data, "diagnostics")
	require.NotContains(t, envelope.Data, "fallback")
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
