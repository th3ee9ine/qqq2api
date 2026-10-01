package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// Existing installations may still contain enabled settings and credentials
// for removed features. Neither reading nor saving the retained settings may
// expose, re-enable, or overwrite those historical values.
func TestSettingsRetiredFeatureContract(t *testing.T) {
	retired := map[string]string{
		"registration_enabled": "true", "email_verify_enabled": "true",
		"password_reset_enabled": "true", "passkey_enabled": "true",
		"github_oauth_enabled": "true", "google_oauth_enabled": "true",
		"linuxdo_connect_enabled": "true", "wechat_connect_enabled": "true",
		"dingtalk_connect_enabled": "true", "oidc_connect_enabled": "true",
		"payment_enabled": "true", "purchase_subscription_enabled": "true",
		"balance_low_notify_enabled": "true", "balance_low_notify_threshold": "20",
		"balance_low_notify_recharge_url": "https://example.com/recharge",
		"subscription_enabled":            "true", "promo_code_enabled": "true",
		"invitation_code_enabled": "true", "affiliate_enabled": "true",
		"default_balance": "100", "default_concurrency": "10",
		"auth_source_default_email_balance": "100", "custom_menu_items": "[]",
		"smtp_host": "mail.example.com", "smtp_password": "historical-secret",
		"antigravity_user_agent_version": "1.0.0", "fallback_model_gemini": "old-model",
	}
	h, repo := newStepUpSwitchTestHandler(t, retired)
	c, rec := func() (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
		return ctx, w
	}()
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
	for field := range retired {
		require.NotContains(t, payload.Data, field)
	}

	for field := range retired {
		t.Run(field, func(t *testing.T) {
			result := doUpdateSettings(t, h, map[string]any{field: true, "site_name": "must not persist"}, nil)
			require.Equal(t, http.StatusBadRequest, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), field)
			require.NotContains(t, repo.values, service.SettingKeySiteName)
		})
	}
	result := doUpdateSettings(t, h, map[string]any{"site_name": "Admin gateway"}, nil)
	require.Equal(t, http.StatusOK, result.Code, result.Body.String())
	require.Equal(t, "Admin gateway", repo.values[service.SettingKeySiteName])
	for field, value := range retired {
		require.Equal(t, value, repo.values[field])
	}
}
