//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

type updatingProxyRepoStub struct {
	*proxyRepoStub
	proxy       *Proxy
	updateCalls int
}

func (s *updatingProxyRepoStub) GetByID(context.Context, int64) (*Proxy, error) {
	copy := *s.proxy
	return &copy, nil
}

func (s *updatingProxyRepoStub) Update(_ context.Context, proxy *Proxy) error {
	s.updateCalls++
	copy := *proxy
	s.proxy = &copy
	return nil
}

func TestBothProxyUpdateServicesUseRepositoryUpdateBoundary(t *testing.T) {
	t.Run("ProxyService", func(t *testing.T) {
		repo := &updatingProxyRepoStub{
			proxyRepoStub: &proxyRepoStub{},
			proxy:         &Proxy{ID: 9, Protocol: "http", Host: "old.example", Port: 8080, Status: StatusActive},
		}
		svc := NewProxyService(repo)
		host := "new.example"
		maxAccounts := 25

		_, err := svc.Update(context.Background(), 9, UpdateProxyRequest{Host: &host, MaxAccounts: &maxAccounts})

		require.NoError(t, err)
		require.Equal(t, 1, repo.updateCalls)
		require.Equal(t, host, repo.proxy.Host)
		require.Equal(t, maxAccounts, repo.proxy.MaxAccounts)
	})

	t.Run("adminService", func(t *testing.T) {
		repo := &updatingProxyRepoStub{
			proxyRepoStub: &proxyRepoStub{},
			proxy: &Proxy{
				ID:             9,
				Protocol:       "http",
				Host:           "old.example",
				Port:           8080,
				Status:         StatusActive,
				FallbackMode:   FallbackModeNone,
				ExpiryWarnDays: 7,
			},
		}
		svc := &adminServiceImpl{proxyRepo: repo}
		maxAccounts := 30

		_, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{
			Host:           "new.example",
			FallbackMode:   FallbackModeNone,
			ExpiryWarnDays: 7,
			MaxAccounts:    &maxAccounts,
		})

		require.NoError(t, err)
		require.Equal(t, 1, repo.updateCalls)
		require.Equal(t, "new.example", repo.proxy.Host)
		require.Equal(t, maxAccounts, repo.proxy.MaxAccounts)
	})
}

func TestProxyServicesRejectNegativeMaxAccountsBeforeRepositoryWrite(t *testing.T) {
	negative := -1

	_, err := NewProxyService(nil).Create(context.Background(), CreateProxyRequest{MaxAccounts: negative})
	require.Error(t, err)
	require.Equal(t, "PROXY_MAX_ACCOUNTS_INVALID", infraerrors.Reason(err))

	_, err = (&adminServiceImpl{}).CreateProxy(context.Background(), &CreateProxyInput{MaxAccounts: negative})
	require.Error(t, err)
	require.Equal(t, "PROXY_MAX_ACCOUNTS_INVALID", infraerrors.Reason(err))

	repo := &updatingProxyRepoStub{
		proxyRepoStub: &proxyRepoStub{},
		proxy:         &Proxy{ID: 9, Protocol: "http", Host: "old.example", Port: 8080, Status: StatusActive},
	}
	_, err = NewProxyService(repo).Update(context.Background(), 9, UpdateProxyRequest{MaxAccounts: &negative})
	require.Error(t, err)
	require.Equal(t, "PROXY_MAX_ACCOUNTS_INVALID", infraerrors.Reason(err))
	require.Zero(t, repo.updateCalls)

	_, err = (&adminServiceImpl{proxyRepo: repo}).UpdateProxy(context.Background(), 9, &UpdateProxyInput{
		FallbackMode:   FallbackModeNone,
		ExpiryWarnDays: 7,
		MaxAccounts:    &negative,
	})
	require.Error(t, err)
	require.Equal(t, "PROXY_MAX_ACCOUNTS_INVALID", infraerrors.Reason(err))
	require.Zero(t, repo.updateCalls)
}
