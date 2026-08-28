//go:build unit

package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountHandlerCreateForwardsAutoAssignProxy(t *testing.T) {
	adminSvc := &stubAdminService{}
	router := setupAccountMixedChannelRouter(adminSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewBufferString(`{
		"name":"created",
		"platform":"openai",
		"type":"apikey",
		"credentials":{"api_key":"test"},
		"auto_assign_proxy":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.Len(t, adminSvc.createdAccounts, 1)
	require.True(t, adminSvc.createdAccounts[0].AutoAssignProxy)
	require.Nil(t, adminSvc.createdAccounts[0].ProxyID)
}

func TestAccountHandlerUpdateForwardsAutoAssignProxy(t *testing.T) {
	adminSvc := &stubAdminService{}
	router := setupAccountMixedChannelRouter(adminSvc)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/11", bytes.NewBufferString(`{
		"name":"updated",
		"auto_assign_proxy":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NotNil(t, adminSvc.lastUpdateAccountInput)
	require.True(t, adminSvc.lastUpdateAccountInput.AutoAssignProxy)
	require.Nil(t, adminSvc.lastUpdateAccountInput.ProxyID)
}

func TestAccountHandlerCreateAndUpdateRejectConflictingProxyModes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "create",
			method: http.MethodPost,
			path:   "/api/v1/admin/accounts",
			body:   `{"name":"created","platform":"openai","type":"apikey","credentials":{"api_key":"test"},"proxy_id":7,"auto_assign_proxy":true}`,
		},
		{
			name:   "update",
			method: http.MethodPut,
			path:   "/api/v1/admin/accounts/11",
			body:   `{"proxy_id":7,"auto_assign_proxy":true}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adminSvc := &stubAdminService{}
			router := setupAccountMixedChannelRouter(adminSvc)
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			require.Equal(t, "PROXY_ASSIGNMENT_MODE_CONFLICT", body["reason"])
		})
	}
}

func TestAccountHandlerBulkUpdateForwardsAutoAssignProxy(t *testing.T) {
	adminSvc := &stubAdminService{}
	router := setupAccountMixedChannelRouter(adminSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/bulk-update", bytes.NewBufferString(`{
		"account_ids":[11,12],
		"auto_assign_proxy":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NotNil(t, adminSvc.lastBulkUpdateAccountInput)
	require.True(t, adminSvc.lastBulkUpdateAccountInput.AutoAssignProxy)
	require.Nil(t, adminSvc.lastBulkUpdateAccountInput.ProxyID)
	require.Equal(t, []int64{11, 12}, adminSvc.lastBulkUpdateAccountInput.AccountIDs)
}

func TestAccountHandlerBulkUpdateRejectsConflictingProxyModes(t *testing.T) {
	adminSvc := &stubAdminService{}
	router := setupAccountMixedChannelRouter(adminSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/bulk-update", bytes.NewBufferString(`{
		"account_ids":[11],
		"proxy_id":7,
		"auto_assign_proxy":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	require.Nil(t, adminSvc.lastBulkUpdateAccountInput)
	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Equal(t, "PROXY_ASSIGNMENT_MODE_CONFLICT", body["reason"])
	require.Contains(t, body["message"], "mutually exclusive")
}

func TestAccountHandlerBulkUpdateRejectsNonPositiveAccountID(t *testing.T) {
	adminSvc := &stubAdminService{}
	router := setupAccountMixedChannelRouter(adminSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/bulk-update", bytes.NewBufferString(`{
		"account_ids":[11,0],
		"auto_assign_proxy":true
	}`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	require.Nil(t, adminSvc.lastBulkUpdateAccountInput)
}
