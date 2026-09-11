//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type proxyBindingRepoStub struct {
	proxyRepoStub
	proxy *Proxy
	err   error
}

type proxyBindingAtomicAccountRepoStub struct {
	accountRepoStubForBulkUpdate
	shadowReads int
}

func (s *proxyBindingAtomicAccountRepoStub) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	s.shadowReads++
	return nil, ErrAccountNotFound
}

func TestBulkProxyBindingDoesNotPropagateShadowsAfterCommit(t *testing.T) {
	source, target := int64(7), int64(0)
	repo := &proxyBindingAtomicAccountRepoStub{accountRepoStubForBulkUpdate: accountRepoStubForBulkUpdate{
		getByIDsAccounts: []*Account{{ID: 11, Platform: PlatformOpenAI, ProxyID: &source}},
	}}
	svc := &adminServiceImpl{accountRepo: repo}
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{11}, ProxyID: &target, ExpectedProxyID: &source,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{11}, result.SuccessIDs)
	require.Zero(t, repo.shadowReads, "the repository already committed parent and shadow bindings together")
}

func (s *proxyBindingRepoStub) GetByID(context.Context, int64) (*Proxy, error) {
	return s.proxy, s.err
}

func TestBulkProxyBindingMovesAndUnbindsOnlyExplicitSourceAccounts(t *testing.T) {
	for _, target := range []int64{0, 9} {
		t.Run(map[bool]string{true: "unbind", false: "move"}[target == 0], func(t *testing.T) {
			source := int64(7)
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
				{ID: 11, Platform: PlatformOpenAI, ProxyID: &source},
				{ID: 12, Platform: PlatformAnthropic, ProxyID: &source},
			}}
			svc := &adminServiceImpl{accountRepo: repo, proxyRepo: &proxyBindingRepoStub{proxy: &Proxy{ID: 9, Status: StatusActive, MaxAccounts: 1}}}
			result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
				AccountIDs: []int64{11, 11, 12}, ProxyID: &target, ExpectedProxyID: &source,
			})
			require.NoError(t, err)
			require.Equal(t, []int64{11, 12}, result.SuccessIDs)
			require.Equal(t, 2, result.Success)
			require.Equal(t, []int64{11, 12}, repo.bulkUpdateIDs)
			require.Equal(t, &source, repo.lastBulkUpdate.ExpectedProxyID)
			require.Equal(t, &target, repo.lastBulkUpdate.ProxyID)
		})
	}
}

func TestBulkProxyBindingRejectsMissingStaleAndShadowAccounts(t *testing.T) {
	source, other, target, parent := int64(7), int64(8), int64(0), int64(99)
	for _, tc := range []struct {
		name     string
		accounts []*Account
		reason   string
	}{
		{name: "missing", reason: "ACCOUNT_NOT_FOUND"},
		{name: "different source", accounts: []*Account{{ID: 11, Platform: PlatformOpenAI, ProxyID: &other}}, reason: "PROXY_BINDING_CHANGED"},
		{name: "already unbound", accounts: []*Account{{ID: 11, Platform: PlatformOpenAI}}, reason: "PROXY_BINDING_CHANGED"},
		{name: "shadow", accounts: []*Account{{ID: 11, Platform: PlatformOpenAI, ProxyID: &source, ParentAccountID: &parent}}, reason: "SPARK_SHADOW_PROXY_INHERITED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: tc.accounts}
			svc := &adminServiceImpl{accountRepo: repo}
			result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{11}, ProxyID: &target, ExpectedProxyID: &source})
			require.Nil(t, result)
			requireApplicationErrorReason(t, err, tc.reason)
			require.Zero(t, repo.bulkUpdateCalls)
		})
	}
}

func TestBulkProxyBindingRejectsUnavailableTargets(t *testing.T) {
	source, target := int64(7), int64(9)
	expired := time.Now().Add(-time.Minute)
	for _, tc := range []struct {
		name  string
		proxy *Proxy
		err   error
	}{
		{name: "missing", err: ErrProxyNotFound},
		{name: "inactive", proxy: &Proxy{ID: 9, Status: "inactive"}},
		{name: "expired", proxy: &Proxy{ID: 9, Status: StatusActive, ExpiresAt: &expired}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 11, Platform: PlatformOpenAI, ProxyID: &source}}}
			svc := &adminServiceImpl{accountRepo: repo, proxyRepo: &proxyBindingRepoStub{proxy: tc.proxy, err: tc.err}}
			result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{11}, ProxyID: &target, ExpectedProxyID: &source})
			require.Nil(t, result)
			require.ErrorIs(t, err, ErrProxyBindingTargetUnavailable)
			require.Zero(t, repo.bulkUpdateCalls)
		})
	}
}

func TestBulkProxyBindingRejectsMixedOrInvalidInputsBeforeReading(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BulkUpdateAccountsInput)
		reason string
	}{
		{name: "filters", mutate: func(in *BulkUpdateAccountsInput) { in.Filters = &BulkUpdateAccountFilters{} }},
		{name: "no ids", mutate: func(in *BulkUpdateAccountsInput) { in.AccountIDs = nil }},
		{name: "name change", mutate: func(in *BulkUpdateAccountsInput) { in.Name = "changed" }},
		{name: "credentials", mutate: func(in *BulkUpdateAccountsInput) { in.Credentials = map[string]any{"api_key": "secret"} }},
		{name: "managed extra", mutate: func(in *BulkUpdateAccountsInput) { in.Extra = map[string]any{UpstreamBillingProbeExtraKey: nil} }},
		{name: "no target", mutate: func(in *BulkUpdateAccountsInput) { in.ProxyID = nil }},
		{name: "negative target", mutate: func(in *BulkUpdateAccountsInput) { *in.ProxyID = -1 }},
		{name: "same proxy", mutate: func(in *BulkUpdateAccountsInput) { *in.ProxyID = *in.ExpectedProxyID }},
		{name: "zero source", mutate: func(in *BulkUpdateAccountsInput) { *in.ExpectedProxyID = 0 }},
		{name: "invalid account", mutate: func(in *BulkUpdateAccountsInput) { in.AccountIDs = []int64{0} }, reason: "INVALID_ACCOUNT_ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, target := int64(7), int64(0)
			input := &BulkUpdateAccountsInput{AccountIDs: []int64{11}, ProxyID: &target, ExpectedProxyID: &source}
			tc.mutate(input)
			repo := &accountRepoStubForBulkUpdate{}
			result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(context.Background(), input)
			require.Nil(t, result)
			reason := tc.reason
			if reason == "" {
				reason = "PROXY_BINDING_INPUT_INVALID"
			}
			requireApplicationErrorReason(t, err, reason)
			require.False(t, repo.getByIDsCalled)
			require.Zero(t, repo.bulkUpdateCalls)
		})
	}
}
