//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountAdminGroupOptionsExposeOnlyAccountFormFields(t *testing.T) {
	options := accountAdminGroupOptions([]service.Group{{
		ID:                        7,
		Name:                      "OpenAI",
		Description:               "primary",
		Platform:                  service.PlatformOpenAI,
		RateMultiplier:            1.2,
		Status:                    service.StatusActive,
		SubscriptionType:          service.SubscriptionTypeStandard,
		LongContextPricingEnabled: true,
		AccountCount:              3,
		ProfitControlEnabled:      true,
		ProfitMinMargin:           0.4,
		ModelRouting:              map[string][]int64{"secret-route": {99}},
	}})

	require.Len(t, options, 1)
	raw, err := json.Marshal(options)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"long_context_pricing_enabled":true`)
	require.NotContains(t, string(raw), "profit")
	require.NotContains(t, string(raw), "model_routing")
	require.NotContains(t, string(raw), "secret-route")
}

func TestGroupHandlerGetAllRedactsInternalFieldsForAccountAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	svc.groups = []service.Group{{
		ID:                   7,
		Name:                 "OpenAI",
		Platform:             service.PlatformOpenAI,
		Status:               service.StatusActive,
		ProfitControlEnabled: true,
		ProfitMinMargin:      0.4,
		ModelRouting:         map[string][]int64{"secret-route": {99}},
	}}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAccountAdmin)
		c.Next()
	})
	router.GET("/api/v1/admin/groups/all", NewGroupHandler(svc, nil, nil).GetAll)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/all?include_inactive=true", nil))

	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), `"name":"OpenAI"`)
	require.NotContains(t, response.Body.String(), "profit")
	require.NotContains(t, response.Body.String(), "model_routing")
	require.NotContains(t, response.Body.String(), "secret-route")
}

func TestAccountAdminTLSProfileOptionsOmitFingerprintMaterial(t *testing.T) {
	options := accountAdminTLSProfileOptions([]*model.TLSFingerprintProfile{{
		ID:           5,
		Name:         "Chrome",
		CipherSuites: []uint16{4865, 4866},
		Extensions:   []uint16{0, 10, 13},
	}})

	raw, err := json.Marshal(options)
	require.NoError(t, err)
	require.JSONEq(t, `[{"id":5,"name":"Chrome"}]`, string(raw))
}

func TestAccountAdminWebSearchConfigOmitProviderSecretsAndPolicy(t *testing.T) {
	quota := int64(1000)
	proxyID := int64(12)
	view := accountAdminWebSearchConfig(&service.WebSearchEmulationConfig{
		Enabled: true,
		Providers: []service.WebSearchProviderConfig{{
			Type:       "brave",
			APIKey:     "secret-key",
			QuotaLimit: &quota,
			ProxyID:    &proxyID,
		}},
	})

	raw, err := json.Marshal(view)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":true,"providers":[{"type":"brave"}]}`, string(raw))
	require.NotContains(t, string(raw), "secret-key")
	require.NotContains(t, string(raw), "quota")
	require.NotContains(t, string(raw), "proxy_id")
}
