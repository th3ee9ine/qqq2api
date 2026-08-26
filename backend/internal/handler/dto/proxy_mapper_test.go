//go:build unit

package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

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
