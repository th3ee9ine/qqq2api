//go:build unit

package handler

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

type settingHandlerPublicRepoStub struct {
	values map[string]string
}

func (s *settingHandlerPublicRepoStub) Get(ctx context.Context, key string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *settingHandlerPublicRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *settingHandlerPublicRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingHandlerPublicRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := s.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (s *settingHandlerPublicRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingHandlerPublicRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingHandlerPublicRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func TestSettingHandler_GetPublicSettings_OmitsRetiredFeatures(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &settingHandlerPublicRepoStub{
		values: map[string]string{
			"force_email_on_third_party_signup": "true",
			"balance_low_notify_enabled":        "true",
			"balance_low_notify_threshold":      "20",
			"balance_low_notify_recharge_url":   "https://example.com/recharge",
		},
	}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	for _, field := range []string{
		"force_email_on_third_party_signup", "balance_low_notify_enabled",
		"balance_low_notify_threshold", "balance_low_notify_recharge_url",
	} {
		require.NotContains(t, resp.Data, field)
	}
}

func TestSettingHandler_GetPublicSettings_ExposesTencentCaptchaConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &settingHandlerPublicRepoStub{
		values: map[string]string{
			service.SettingKeyTencentCaptchaEnabled: "true",
			service.SettingKeyTencentCaptchaAppID:   "123456789",
			service.SettingKeyTencentCaptchaRegion:  service.TencentCaptchaRegionINTL,
		},
	}
	h := NewSettingHandler(service.NewSettingService(repo, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			TencentCaptchaEnabled bool   `json:"tencent_captcha_enabled"`
			TencentCaptchaAppID   string `json:"tencent_captcha_app_id"`
			TencentCaptchaRegion  string `json:"tencent_captcha_region"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.True(t, resp.Data.TencentCaptchaEnabled)
	require.Equal(t, "123456789", resp.Data.TencentCaptchaAppID)
	require.Equal(t, service.TencentCaptchaRegionINTL, resp.Data.TencentCaptchaRegion)
}

func TestSettingHandler_GetPublicSettings_OmitsRetiredWeChatOAuthFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewSettingHandler(service.NewSettingService(&settingHandlerPublicRepoStub{
		values: map[string]string{
			"wechat_connect_enabled":               "true",
			"wechat_connect_app_id":                "wx-mp-app",
			"wechat_connect_app_secret":            "wx-mp-secret",
			"wechat_connect_mode":                  "mp",
			"wechat_connect_scopes":                "snsapi_base",
			"wechat_connect_open_enabled":          "true",
			"wechat_connect_mp_enabled":            "true",
			"wechat_connect_redirect_url":          "https://api.example.com/api/v1/auth/oauth/wechat/callback",
			"wechat_connect_frontend_redirect_url": "/auth/wechat/callback",
		},
	}, &config.Config{}), "test-version")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)

	h.GetPublicSettings(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotContains(t, resp.Data, "wechat_oauth_enabled")
	require.NotContains(t, resp.Data, "wechat_oauth_open_enabled")
	require.NotContains(t, resp.Data, "wechat_oauth_mp_enabled")
}
