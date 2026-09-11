//go:build unit

package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestProxyAccountSummaryMapperIncludesParentAccountID(t *testing.T) {
	parentID := int64(11)
	summary := ProxyAccountSummaryFromService(&service.ProxyAccountSummary{
		ID: 12, Name: "spark", Platform: "openai", Type: "oauth", ParentAccountID: &parentID,
	})
	require.NotNil(t, summary)
	require.Equal(t, &parentID, summary.ParentAccountID)
	data, err := json.Marshal(summary)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":12,"name":"spark","platform":"openai","type":"oauth","parent_account_id":11}`, string(data))
}

func TestProxyAccountSummaryMapperOmitsParentAccountIDForIndependentAccount(t *testing.T) {
	summary := ProxyAccountSummaryFromService(&service.ProxyAccountSummary{
		ID: 11, Name: "parent", Platform: "openai", Type: "oauth",
	})
	require.NotNil(t, summary)
	require.Nil(t, summary.ParentAccountID)
	data, err := json.Marshal(summary)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":11,"name":"parent","platform":"openai","type":"oauth"}`, string(data))
	require.Nil(t, ProxyAccountSummaryFromService(nil))
}

func TestProxyMappersIncludeAutomaticAssignmentLimit(t *testing.T) {
	proxy := &service.Proxy{ID: 7, MaxAccounts: 19, Password: "secret"}

	regular := ProxyFromService(proxy)
	require.NotNil(t, regular)
	require.Equal(t, 19, regular.MaxAccounts)

	admin := ProxyFromServiceAdmin(proxy)
	require.NotNil(t, admin)
	require.Equal(t, 19, admin.MaxAccounts)
	require.Equal(t, "secret", admin.Password)

	withCount := ProxyWithAccountCountFromServiceAdmin(&service.ProxyWithAccountCount{
		Proxy:        *proxy,
		AccountCount: 4,
	})
	require.NotNil(t, withCount)
	require.Equal(t, 19, withCount.MaxAccounts)
	require.Equal(t, int64(4), withCount.AccountCount)
}
