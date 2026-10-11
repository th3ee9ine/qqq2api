//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type openAIOAuthTTFTGuardHandlerRepo struct{ value string }

func (r *openAIOAuthTTFTGuardHandlerRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}
func (r *openAIOAuthTTFTGuardHandlerRepo) GetValue(context.Context, string) (string, error) {
	if r.value == "" {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}
func (r *openAIOAuthTTFTGuardHandlerRepo) Set(_ context.Context, _ string, value string) error {
	r.value = value
	return nil
}
func (r *openAIOAuthTTFTGuardHandlerRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *openAIOAuthTTFTGuardHandlerRepo) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (r *openAIOAuthTTFTGuardHandlerRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}
func (r *openAIOAuthTTFTGuardHandlerRepo) Delete(context.Context, string) error { return nil }

func TestOpenAIOAuthTTFTGuardSettingsHandlerRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &openAIOAuthTTFTGuardHandlerRepo{}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), nil, nil)
	router := gin.New()
	router.GET("/settings/openai-oauth-ttft-guard", h.GetOpenAIOAuthTTFTGuardSettings)
	router.PUT("/settings/openai-oauth-ttft-guard", h.UpdateOpenAIOAuthTTFTGuardSettings)

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/settings/openai-oauth-ttft-guard", nil))
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Contains(t, getRecorder.Body.String(), `"recovery_interval_minutes":30`)
	require.Contains(t, getRecorder.Body.String(), `"first_token_threshold_seconds":15`)

	putRecorder := httptest.NewRecorder()
	router.ServeHTTP(putRecorder, httptest.NewRequest(http.MethodPut, "/settings/openai-oauth-ttft-guard", strings.NewReader(`{"enabled":false,"recovery_interval_minutes":45,"first_token_threshold_seconds":20}`)))
	require.Equal(t, http.StatusOK, putRecorder.Code)
	require.Contains(t, putRecorder.Body.String(), `"recovery_interval_minutes":45`)
	require.Contains(t, putRecorder.Body.String(), `"first_token_threshold_seconds":20`)

	badRecorder := httptest.NewRecorder()
	router.ServeHTTP(badRecorder, httptest.NewRequest(http.MethodPut, "/settings/openai-oauth-ttft-guard", strings.NewReader(`{"enabled":true,"recovery_interval_minutes":0,"first_token_threshold_seconds":15}`)))
	require.Equal(t, http.StatusBadRequest, badRecorder.Code)

	badThresholdRecorder := httptest.NewRecorder()
	router.ServeHTTP(badThresholdRecorder, httptest.NewRequest(http.MethodPut, "/settings/openai-oauth-ttft-guard", strings.NewReader(`{"enabled":true,"recovery_interval_minutes":30,"first_token_threshold_seconds":301}`)))
	require.Equal(t, http.StatusBadRequest, badThresholdRecorder.Code)
}
