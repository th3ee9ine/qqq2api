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

func TestOpenAIOAuthTTFTGuardSettingsHandlerValidationAndPersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name      string
		payload   string
		status    int
		threshold int
	}{
		{"minimum", `{"enabled":true,"recovery_interval_minutes":1,"first_token_threshold_seconds":1}`, http.StatusOK, 1},
		{"maximum", `{"enabled":true,"recovery_interval_minutes":10080,"first_token_threshold_seconds":300}`, http.StatusOK, 300},
		{"legacy client", `{"enabled":true,"recovery_interval_minutes":30}`, http.StatusOK, 15},
		{"negative threshold", `{"enabled":true,"recovery_interval_minutes":30,"first_token_threshold_seconds":-1}`, http.StatusBadRequest, 0},
		{"threshold too large", `{"enabled":true,"recovery_interval_minutes":30,"first_token_threshold_seconds":301}`, http.StatusBadRequest, 0},
		{"fractional seconds", `{"enabled":true,"recovery_interval_minutes":30,"first_token_threshold_seconds":1.5}`, http.StatusBadRequest, 0},
		{"non numeric seconds", `{"enabled":true,"recovery_interval_minutes":30,"first_token_threshold_seconds":"fast"}`, http.StatusBadRequest, 0},
		{"invalid bool", `{"enabled":"yes","recovery_interval_minutes":30,"first_token_threshold_seconds":15}`, http.StatusBadRequest, 0},
		{"missing recovery", `{"enabled":true,"first_token_threshold_seconds":15}`, http.StatusBadRequest, 0},
		{"negative recovery", `{"enabled":true,"recovery_interval_minutes":-1,"first_token_threshold_seconds":15}`, http.StatusBadRequest, 0},
		{"recovery too large", `{"enabled":true,"recovery_interval_minutes":10081,"first_token_threshold_seconds":15}`, http.StatusBadRequest, 0},
		{"malformed JSON", `{broken`, http.StatusBadRequest, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &openAIOAuthTTFTGuardHandlerRepo{}
			h := NewSettingHandler(service.NewSettingService(repo, nil), nil, nil)
			router := gin.New()
			router.PUT("/settings/openai-oauth-ttft-guard", h.UpdateOpenAIOAuthTTFTGuardSettings)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/settings/openai-oauth-ttft-guard", strings.NewReader(tc.payload)))
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			if tc.status != http.StatusOK {
				require.Empty(t, repo.value, "invalid input must not overwrite persisted policy")
				return
			}
			fresh := service.NewSettingService(repo, nil)
			got, err := fresh.GetOpenAIOAuthTTFTGuardSettings(context.Background())
			require.NoError(t, err)
			require.True(t, got.Enabled)
			require.Equal(t, tc.threshold, got.FirstTokenThresholdSeconds)
		})
	}
}
