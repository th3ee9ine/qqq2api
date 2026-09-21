package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type openAIAccountRuntimeStateInvalidatorSpy struct {
	mu  sync.Mutex
	ids []int64
}

func (*openAIAccountRuntimeStateInvalidatorSpy) BlockAccountScheduling(*Account, time.Time, string) {}
func (*openAIAccountRuntimeStateInvalidatorSpy) ClearAccountSchedulingBlock(int64)                  {}

func (s *openAIAccountRuntimeStateInvalidatorSpy) InvalidateOpenAIAccountRuntimeState(accountID int64) {
	s.mu.Lock()
	s.ids = append(s.ids, accountID)
	s.mu.Unlock()
}

func (s *openAIAccountRuntimeStateInvalidatorSpy) snapshot() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.ids...)
}

type openAIAccountRuntimeStateRepo struct {
	AccountRepository
	accounts  []*Account
	shadows   map[int64][]*Account
	deleted   []int64
	deleteErr map[int64]error
	updates   []AccountBulkUpdate
}

type openAIProxyRuntimeRepo struct {
	ProxyRepository
	proxy             *Proxy
	accountsByProxyID map[int64][]ProxyAccountSummary
	allProxies        []Proxy
	listAccountsErr   error
	updateErr         error
	deleteErr         error
	countErr          error
	accountCount      int64
	sweepErr          error
	sweepChanged      int64
	updateCalls       int
	deleteCalls       int
	sweepCalls        int
	listAccountCalls  []int64
}

func (r *openAIProxyRuntimeRepo) GetByID(context.Context, int64) (*Proxy, error) {
	copy := *r.proxy
	return &copy, nil
}

func (r *openAIProxyRuntimeRepo) Update(_ context.Context, proxy *Proxy) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	copy := *proxy
	r.proxy = &copy
	return nil
}

func (r *openAIProxyRuntimeRepo) Delete(context.Context, int64) error {
	r.deleteCalls++
	return r.deleteErr
}

func (r *openAIProxyRuntimeRepo) CountAccountsByProxyID(context.Context, int64) (int64, error) {
	return r.accountCount, r.countErr
}

func (r *openAIProxyRuntimeRepo) ListAccountSummariesByProxyID(_ context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	r.listAccountCalls = append(r.listAccountCalls, proxyID)
	if r.listAccountsErr != nil {
		return nil, r.listAccountsErr
	}
	return append([]ProxyAccountSummary(nil), r.accountsByProxyID[proxyID]...), nil
}

func (r *openAIProxyRuntimeRepo) ListAllForFallback(context.Context) ([]Proxy, error) {
	return append([]Proxy(nil), r.allProxies...), nil
}

func (r *openAIProxyRuntimeRepo) SweepExpiredProxies(context.Context, time.Time) (int64, error) {
	r.sweepCalls++
	return r.sweepChanged, r.sweepErr
}

func (r *openAIAccountRuntimeStateRepo) GetByIDs(context.Context, []int64) ([]*Account, error) {
	return r.accounts, nil
}

func (r *openAIAccountRuntimeStateRepo) ListShadowsByParent(_ context.Context, parentID int64) ([]*Account, error) {
	return r.shadows[parentID], nil
}

func (r *openAIAccountRuntimeStateRepo) BulkUpdate(_ context.Context, _ []int64, update AccountBulkUpdate) (int64, error) {
	r.updates = append(r.updates, update)
	return int64(len(r.accounts)), nil
}

func (r *openAIAccountRuntimeStateRepo) Delete(_ context.Context, accountID int64) error {
	if err := r.deleteErr[accountID]; err != nil {
		return err
	}
	r.deleted = append(r.deleted, accountID)
	return nil
}

func TestInvalidateOpenAIAccountRuntimeStateWithShadows(t *testing.T) {
	parent := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	repo := &openAIAccountRuntimeStateRepo{shadows: map[int64][]*Account{
		parent.ID: {{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
	}}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}

	invalidateOpenAIAccountRuntimeStateWithShadows(context.Background(), invalidator, repo, parent)
	require.Equal(t, []int64{10, 11}, invalidator.snapshot())

	// The shadow itself is independently keyed, but must not recursively query
	// for descendants (credential-shadow nesting is invalid by construction).
	parentID := parent.ID
	shadow := &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
	invalidateOpenAIAccountRuntimeStateWithShadows(context.Background(), invalidator, repo, shadow)
	require.Equal(t, []int64{10, 11, 11}, invalidator.snapshot())
}

func TestAdminDeleteAccountInvalidatesEachCommittedDelete(t *testing.T) {
	deleteFailure := errors.New("delete failed")
	repo := &openAIAccountRuntimeStateRepo{
		shadows:   map[int64][]*Account{10: {{ID: 11}, {ID: 12}}},
		deleteErr: map[int64]error{12: deleteFailure},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}

	err := svc.DeleteAccount(context.Background(), 10)
	require.ErrorIs(t, err, deleteFailure)
	require.Equal(t, []int64{11}, repo.deleted)
	require.Equal(t, []int64{11}, invalidator.snapshot(),
		"a later cascade failure must not retain runtime state for an already deleted shadow")

	delete(repo.deleteErr, 12)
	invalidator.ids = nil
	repo.deleted = nil
	require.NoError(t, svc.DeleteAccount(context.Background(), 10))
	require.Equal(t, []int64{11, 12, 10}, repo.deleted)
	require.Equal(t, []int64{11, 12, 10}, invalidator.snapshot())
}

func TestAdminBulkProxyMoveInvalidatesParentAndShadows(t *testing.T) {
	sourceProxyID := int64(7)
	targetProxyID := int64(0)
	parent := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &sourceProxyID}
	repo := &openAIAccountRuntimeStateRepo{
		accounts: []*Account{parent},
		shadows:  map[int64][]*Account{10: {{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:      []int64{parent.ID},
		ProxyID:         &targetProxyID,
		ExpectedProxyID: &sourceProxyID,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{parent.ID}, result.SuccessIDs)
	require.Len(t, repo.updates, 1)
	require.Equal(t, []int64{10, 11}, invalidator.snapshot())
}

func TestTokenRefreshStateSyncInvalidatesParentAndShadows(t *testing.T) {
	parent := &Account{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	repo := &openAIAccountRuntimeStateRepo{shadows: map[int64][]*Account{
		parent.ID: {{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
	}}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &TokenRefreshService{accountRepo: repo, runtimeBlocker: invalidator}

	svc.postRefreshStateSync(context.Background(), parent, true)
	require.Equal(t, []int64{20, 21}, invalidator.snapshot())

	invalidator.ids = nil
	svc.postRefreshStateSync(context.Background(), parent, false)
	require.Empty(t, invalidator.snapshot(), "a refresh result without persisted credentials must not rotate the runtime lease")
}

func TestOpenAIGatewayRuntimeStateInvalidationIsAccountScoped(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	accountKey := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 30, Scope: "scope-a", Model: "gpt-5"})
	siblingKey := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 31, Scope: "scope-b", Model: "gpt-5"})
	require.True(t, collector.OfferValueMust(accountKey, collectorTestToken(t, now, 2, 1), "route-a", now))
	require.True(t, collector.OfferValueMust(siblingKey, collectorTestToken(t, now, 2, 2), "route-b", now))

	stateStore := NewOpenAIWSStateStore(nil)
	stateStore.BindSessionTurnState(1, 30, "scope-a", "state-a", time.Hour, "gpt-5")
	stateStore.BindSessionTurnState(1, 31, "scope-b", "state-b", time.Hour, "gpt-5")
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSFakeDialer{})
	defer pool.Close()
	for _, accountID := range []int64{30, 31} {
		lease, err := pool.Acquire(context.Background(), openAIWSAcquireRequest{
			Account: &Account{ID: accountID, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			WSURL:   "wss://example.com/v1/responses",
			Headers: http.Header{},
		})
		require.NoError(t, err)
		lease.Release()
	}
	svc := &OpenAIGatewayService{
		codexTurnStateCollector: collector,
		openaiWSStateStore:      stateStore,
		openaiWSPool:            pool,
	}
	svc.openaiCodexTurnStateOrigins.Store("origin-a", openAICodexTurnStateOrigin{accountID: 30})
	svc.openaiCodexTurnStateOrigins.Store("origin-b", openAICodexTurnStateOrigin{accountID: 31})

	svc.InvalidateOpenAIAccountRuntimeState(30)

	_, accountUsable := collector.Acquire(accountKey, now)
	_, siblingUsable := collector.Acquire(siblingKey, now)
	require.False(t, accountUsable)
	require.True(t, siblingUsable)
	_, accountOriginExists := svc.openaiCodexTurnStateOrigins.Load("origin-a")
	_, siblingOriginExists := svc.openaiCodexTurnStateOrigins.Load("origin-b")
	require.False(t, accountOriginExists)
	require.True(t, siblingOriginExists)
	_, accountStateExists := stateStore.GetSessionTurnState(1, 30, "scope-a", "gpt-5")
	siblingState, siblingStateExists := stateStore.GetSessionTurnState(1, 31, "scope-b", "gpt-5")
	require.False(t, accountStateExists)
	require.True(t, siblingStateExists)
	require.Equal(t, "state-b", siblingState)
	_, _, accountConns := pool.AccountPoolLoad(30)
	_, _, siblingConns := pool.AccountPoolLoad(31)
	require.Zero(t, accountConns)
	require.Equal(t, 1, siblingConns)
}

func TestAdminProxyConnectionIdentityUpdateInvalidatesOnlyAffectedCodexAccounts(t *testing.T) {
	parentID := int64(10)
	repo := &openAIProxyRuntimeRepo{
		proxy: &Proxy{ID: 9, Protocol: "http", Host: "old.example", Port: 8080, Status: StatusActive, FallbackMode: FallbackModeNone},
		accountsByProxyID: map[int64][]ProxyAccountSummary{9: {
			{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeSetupToken},
			{ID: 12, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
			{ID: 13, Platform: PlatformAnthropic, Type: AccountTypeOAuth},
			{ID: 14, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID},
			{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		}},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{proxyRepo: repo, runtimeBlocker: invalidator}

	_, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{Host: "new.example"})
	require.NoError(t, err)
	require.Equal(t, []int64{9}, repo.listAccountCalls)
	require.Equal(t, []int64{10, 11, 14}, invalidator.snapshot(), "credential shadows carry their own inherited proxy_id and runtime state")

	invalidator.ids = nil
	repo.listAccountCalls = nil
	maxAccounts := 20
	_, err = svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{Name: "renamed", MaxAccounts: &maxAccounts})
	require.NoError(t, err)
	require.Empty(t, repo.listAccountCalls)
	require.Empty(t, invalidator.snapshot(), "metadata and capacity changes do not change upstream connection identity")
}

func TestAdminProxyRuntimeInvalidationRequiresCommittedUpdate(t *testing.T) {
	repo := &openAIProxyRuntimeRepo{
		proxy: &Proxy{ID: 9, Protocol: "http", Host: "old.example", Port: 8080, Status: StatusActive},
		accountsByProxyID: map[int64][]ProxyAccountSummary{9: {
			{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		}},
		updateErr: errors.New("update failed"),
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{proxyRepo: repo, runtimeBlocker: invalidator}

	_, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{Status: StatusDisabled})
	require.Error(t, err)
	require.Empty(t, invalidator.snapshot())

	repo.updateErr = nil
	repo.listAccountsErr = errors.New("list failed")
	_, err = svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{Status: StatusDisabled})
	require.Error(t, err)
	require.Equal(t, 1, repo.updateCalls, "the proxy must not change when its affected accounts cannot be snapshotted")
	require.Empty(t, invalidator.snapshot())
}

func TestProxyServiceUpdateAndDeleteInvalidateAffectedCodexAccounts(t *testing.T) {
	repo := &openAIProxyRuntimeRepo{
		proxy: &Proxy{ID: 9, Protocol: "http", Host: "old.example", Port: 8080, Status: StatusActive},
		accountsByProxyID: map[int64][]ProxyAccountSummary{9: {
			{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		}},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := NewProxyService(repo, invalidator)

	newPassword := "rotated-secret"
	_, err := svc.Update(context.Background(), 9, UpdateProxyRequest{Password: &newPassword})
	require.NoError(t, err)
	require.Equal(t, []int64{10}, invalidator.snapshot())

	invalidator.ids = nil
	repo.deleteErr = errors.New("delete failed")
	require.Error(t, svc.Delete(context.Background(), 9))
	require.Empty(t, invalidator.snapshot(), "a failed delete must retain runtime state")
	repo.deleteErr = nil
	require.NoError(t, svc.Delete(context.Background(), 9))
	require.Equal(t, 2, repo.deleteCalls)
	require.Equal(t, []int64{10}, invalidator.snapshot())
}

func TestAdminProxyDeleteInvalidatesOnlyAfterDeleteCommits(t *testing.T) {
	repo := &openAIProxyRuntimeRepo{
		proxy: &Proxy{ID: 9},
		accountsByProxyID: map[int64][]ProxyAccountSummary{9: {
			{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		}},
		deleteErr: errors.New("delete failed"),
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{proxyRepo: repo, runtimeBlocker: invalidator}

	require.Error(t, svc.DeleteProxy(context.Background(), 9))
	require.Empty(t, invalidator.snapshot())

	repo.deleteErr = nil
	require.NoError(t, svc.DeleteProxy(context.Background(), 9))
	require.Equal(t, []int64{10}, invalidator.snapshot())
}

func TestProxyExpiryInvalidatesAccountsBoundToExpiredProxy(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	repo := &openAIProxyRuntimeRepo{
		allProxies: []Proxy{
			{ID: 9, Status: StatusActive, ExpiresAt: &past},
			{ID: 10, Status: StatusActive, ExpiresAt: &future},
			{ID: 11, Status: StatusDisabled, ExpiresAt: &past},
		},
		accountsByProxyID: map[int64][]ProxyAccountSummary{9: {
			{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		}},
		sweepChanged: 2,
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := NewProxyExpiryService(repo, time.Minute, invalidator)

	svc.runOnce()

	require.Equal(t, 1, repo.sweepCalls)
	require.Equal(t, []int64{9}, repo.listAccountCalls)
	require.Equal(t, []int64{20}, invalidator.snapshot())
}
