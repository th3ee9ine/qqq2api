package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func accountAdminRoleMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAccountAdmin)
		c.Next()
	}
}

func TestProxyHandlerAccountAdminListOmitsPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	svc.proxyCounts = []service.ProxyWithAccountCount{{
		Proxy: service.Proxy{
			ID:       7,
			Name:     "shared",
			Protocol: "http",
			Host:     "proxy.internal",
			Port:     8080,
			Password: "do-not-return",
		},
		AccountCount: 2,
	}}
	router := gin.New()
	router.Use(accountAdminRoleMiddleware())
	router.GET("/proxies", NewProxyHandler(svc).List)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxies", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Items, 1)
	_, hasPassword := envelope.Data.Items[0]["password"]
	require.False(t, hasPassword, "account-admin proxy list must not expose credentials")
}

func TestProxyHandlerAccountAdminGetByIDOmitsPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	svc.proxies = []service.Proxy{{
		ID:       7,
		Name:     "shared",
		Protocol: "http",
		Host:     "proxy.internal",
		Port:     8080,
		Password: "do-not-return",
	}}
	router := gin.New()
	router.Use(accountAdminRoleMiddleware())
	router.GET("/proxies/:id", NewProxyHandler(svc).GetByID)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxies/7", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	_, hasPassword := envelope.Data["password"]
	require.False(t, hasPassword, "account-admin proxy detail must not expose credentials")
}

func TestProxyHandlerAccountAdminCannotExportProxyCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newStubAdminService()
	router := gin.New()
	router.Use(accountAdminRoleMiddleware())
	router.GET("/proxies/data", NewProxyHandler(svc).ExportData)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxies/data", nil)
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
}
