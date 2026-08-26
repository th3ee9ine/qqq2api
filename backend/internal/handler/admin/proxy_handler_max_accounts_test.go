//go:build unit

package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProxyHandlerBatchCreateRejectsNegativeMaxAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adminSvc := newStubAdminService()
	router := gin.New()
	router.POST("/api/v1/admin/proxies/batch", NewProxyHandler(adminSvc).BatchCreate)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/batch", bytes.NewBufferString(`{
		"proxies":[{
			"protocol":"http",
			"host":"127.0.0.1",
			"port":8080,
			"max_accounts":-1
		}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	adminSvc.mu.Lock()
	defer adminSvc.mu.Unlock()
	require.Empty(t, adminSvc.createdProxies)
}
