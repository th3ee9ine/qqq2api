//go:build unit

package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountHandlerBulkUpdateForwardsExpectedProxyID(t *testing.T) {
	adminSvc := &stubAdminService{}
	router := setupAccountMixedChannelRouter(adminSvc)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/bulk-update", bytes.NewBufferString(`{"account_ids":[11,12],"proxy_id":0,"expected_proxy_id":7}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NotNil(t, adminSvc.lastBulkUpdateAccountInput)
	require.Equal(t, int64(7), *adminSvc.lastBulkUpdateAccountInput.ExpectedProxyID)
	require.Equal(t, int64(0), *adminSvc.lastBulkUpdateAccountInput.ProxyID)
}
