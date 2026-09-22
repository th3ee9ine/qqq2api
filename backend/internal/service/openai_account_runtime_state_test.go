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
	mu                  sync.Mutex
	ids                 []int64
	globalInvalidations int
}

func (*openAIAccountRuntimeStateInvalidatorSpy) BlockAccountScheduling(*Account, time.Time, string) {}
func (*openAIAccountRuntimeStateInvalidatorSpy) ClearAccountSchedulingBlock(int64)                  {}

func (s *openAIAccountRuntimeStateInvalidatorSpy) InvalidateOpenAIAccountRuntimeState(accountID int64) {
	s.mu.Lock()
	s.ids = append(s.ids, accountID)
	s.mu.Unlock()
}

func (s *openAIAccountRuntimeStateInvalidatorSpy) InvalidateAllOpenAIAccountRuntimeState() {
	s.mu.Lock()
	s.globalInvalidations++
	s.mu.Unlock()
}

func (s *openAIAccountRuntimeStateInvalidatorSpy) snapshot() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int64(nil), s.ids...)
}

func (s *openAIAccountRuntimeStateInvalidatorSpy) globalInvalidationCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.globalInvalidations
}

type openAIAccountRuntimeStateRepo struct {
	AccountRepository
	accounts          []*Account
	shadows           map[int64][]*Account
	deleted           []int64
	deleteErr         map[int64]error
	updates           []AccountBulkUpdate
	updateErr         error
	bulkUpdateErr     error
	setSchedulableErr error
	bindGroupErr      map[int64]error
	shadowLookupErr   error
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

func (r *openAIAccountRuntimeStateRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for _, account := range r.accounts {
		if account != nil && account.ID == id {
			return account, nil
		}
	}
	return nil, ErrAccountNotFound
}

func (r *openAIAccountRuntimeStateRepo) Update(_ context.Context, account *Account) error {
	return r.updateErr
}

func (r *openAIAccountRuntimeStateRepo) SetSchedulable(_ context.Context, id int64, schedulable bool) error {
	if r.setSchedulableErr != nil {
		return r.setSchedulableErr
	}
	account, err := r.GetByID(context.Background(), id)
	if err != nil {
		return err
	}
	account.Schedulable = schedulable
	return nil
}

func (r *openAIAccountRuntimeStateRepo) BindGroups(_ context.Context, accountID int64, groupIDs []int64) error {
	if err := r.bindGroupErr[accountID]; err != nil {
		return err
	}
	account, err := r.GetByID(context.Background(), accountID)
	if err != nil {
		return err
	}
	account.GroupIDs = append([]int64(nil), groupIDs...)
	return nil
}

func (r *openAIAccountRuntimeStateRepo) ListShadowsByParent(_ context.Context, parentID int64) ([]*Account, error) {
	if r.shadowLookupErr != nil {
		return nil, r.shadowLookupErr
	}
	return r.shadows[parentID], nil
}

func (r *openAIAccountRuntimeStateRepo) BulkUpdate(_ context.Context, _ []int64, update AccountBulkUpdate) (int64, error) {
	r.updates = append(r.updates, update)
	if r.bulkUpdateErr != nil {
		return 0, r.bulkUpdateErr
	}
	for _, account := range r.accounts {
		if account == nil {
			continue
		}
		if update.ProxyID != nil {
			if *update.ProxyID == 0 {
				account.ProxyID = nil
			} else {
				proxyID := *update.ProxyID
				account.ProxyID = &proxyID
			}
		}
		if update.Status != nil {
			account.Status = *update.Status
		}
		if update.Schedulable != nil {
			account.Schedulable = *update.Schedulable
		}
		if len(update.Credentials) > 0 {
			if account.Credentials == nil {
				account.Credentials = make(map[string]any)
			}
			for key, value := range update.Credentials {
				account.Credentials[key] = value
			}
		}
		if len(update.Extra) > 0 {
			if account.Extra == nil {
				account.Extra = make(map[string]any)
			}
			for key, value := range update.Extra {
				account.Extra[key] = value
			}
		}
	}
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

func TestInvalidateOpenAIAccountRuntimeStateWithShadowsFailsClosedWhenLookupFails(t *testing.T) {
	lookupErr := errors.New("shadow lookup failed")
	parent := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	repo := &openAIAccountRuntimeStateRepo{shadowLookupErr: lookupErr}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}

	invalidateOpenAIAccountRuntimeStateWithShadows(context.Background(), invalidator, repo, parent)

	require.Equal(t, []int64{parent.ID}, invalidator.snapshot())
	require.Equal(t, 1, invalidator.globalInvalidationCount(),
		"an unknown shadow set must trigger the process-wide fail-closed fallback")
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

func TestAdminUpdateAccountAvailabilityChangesInvalidateRuntimeState(t *testing.T) {
	parent := &Account{
		ID:          40,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       map[string]any{codexFingerprintSeedExtraKey: testCodexFingerprintSeed},
		GroupIDs:    []int64{9},
	}
	repo := &openAIAccountRuntimeStateRepo{
		accounts: []*Account{parent},
		shadows: map[int64][]*Account{
			parent.ID: {{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
		},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}

	_, err := svc.UpdateAccount(context.Background(), parent.ID, &UpdateAccountInput{Status: StatusDisabled})
	require.NoError(t, err)
	require.Equal(t, []int64{40, 41}, invalidator.snapshot())

	invalidator.ids = nil
	_, err = svc.UpdateAccount(context.Background(), parent.ID, &UpdateAccountInput{Status: StatusDisabled})
	require.NoError(t, err)
	require.Empty(t, invalidator.snapshot(), "an unchanged status must not rotate a healthy runtime generation")

	expiresAt := time.Now().Add(time.Hour).Unix()
	autoPause := true
	emptyGroups := []int64{}
	for _, test := range []struct {
		name  string
		input *UpdateAccountInput
	}{
		{name: "expires_at", input: &UpdateAccountInput{ExpiresAt: &expiresAt}},
		{name: "auto_pause", input: &UpdateAccountInput{AutoPauseOnExpired: &autoPause}},
		{name: "groups", input: &UpdateAccountInput{GroupIDs: &emptyGroups, SkipMixedChannelCheck: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalidator.ids = nil
			_, err = svc.UpdateAccount(context.Background(), parent.ID, test.input)
			require.NoError(t, err)
			require.Equal(t, []int64{40, 41}, invalidator.snapshot())

			invalidator.ids = nil
			_, err = svc.UpdateAccount(context.Background(), parent.ID, test.input)
			require.NoError(t, err)
			require.Empty(t, invalidator.snapshot(), "an unchanged availability value must not rotate the runtime generation")
		})
	}
}

func TestAdminBulkAvailabilityChangesInvalidateOnlyAffectedAccounts(t *testing.T) {
	changed := &Account{ID: 50, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	unchanged := &Account{ID: 60, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusDisabled, Schedulable: false}
	repo := &openAIAccountRuntimeStateRepo{
		accounts: []*Account{changed, unchanged},
		shadows: map[int64][]*Account{
			changed.ID:   {{ID: 51, Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
			unchanged.ID: {{ID: 61, Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
		},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}
	schedulable := false

	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:  []int64{changed.ID, unchanged.ID},
		Status:      StatusDisabled,
		Schedulable: &schedulable,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{changed.ID, unchanged.ID}, result.SuccessIDs)
	require.Equal(t, []int64{50, 51}, invalidator.snapshot())

	changed.GroupIDs = []int64{9}
	unchanged.GroupIDs = []int64{9}
	repo.bindGroupErr = map[int64]error{unchanged.ID: errors.New("bind failed")}
	invalidator.ids = nil
	emptyGroups := []int64{}
	result, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:            []int64{changed.ID, unchanged.ID},
		GroupIDs:              &emptyGroups,
		SkipMixedChannelCheck: true,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{changed.ID}, result.SuccessIDs)
	require.Equal(t, []int64{unchanged.ID}, result.FailedIDs)
	require.Equal(t, []int64{50, 51}, invalidator.snapshot(),
		"a failed group binding must not rotate that account's runtime generation")

	delete(repo.bindGroupErr, unchanged.ID)
	invalidator.ids = nil
	result, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs:            []int64{changed.ID, unchanged.ID},
		GroupIDs:              &emptyGroups,
		SkipMixedChannelCheck: true,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{changed.ID, unchanged.ID}, result.SuccessIDs)
	require.Equal(t, []int64{60, 61}, invalidator.snapshot(),
		"the account whose group binding is already equal must keep its generation")
}

func TestAdminBulkCredentialPersistenceInvalidatesWhenLaterGroupBindingFails(t *testing.T) {
	account := &Account{
		ID:          65,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"access_token": "old-token"},
		GroupIDs:    []int64{9},
	}
	repo := &openAIAccountRuntimeStateRepo{
		accounts:     []*Account{account},
		shadows:      map[int64][]*Account{account.ID: {{ID: 66, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}},
		bindGroupErr: map[int64]error{account.ID: errors.New("bind failed")},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	emptyGroups := []int64{}

	result, err := (&adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}).BulkUpdateAccounts(
		context.Background(),
		&BulkUpdateAccountsInput{
			AccountIDs:            []int64{account.ID},
			Credentials:           map[string]any{"access_token": "new-token"},
			GroupIDs:              &emptyGroups,
			SkipMixedChannelCheck: true,
		},
	)

	require.NoError(t, err)
	require.Equal(t, []int64{account.ID}, result.FailedIDs)
	require.Equal(t, "new-token", account.Credentials["access_token"], "the identity write committed before BindGroups failed")
	require.Equal(t, []int64{65, 66}, invalidator.snapshot(),
		"a later group failure must not retain runtime state for the committed credential identity")
}

func TestAdminBulkGroupFailureAndEqualCredentialPatchDoNotInvalidate(t *testing.T) {
	t.Run("group only failure", func(t *testing.T) {
		account := &Account{
			ID:          67,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			GroupIDs:    []int64{9},
		}
		repo := &openAIAccountRuntimeStateRepo{
			accounts:     []*Account{account},
			bindGroupErr: map[int64]error{account.ID: errors.New("bind failed")},
		}
		invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
		emptyGroups := []int64{}

		result, err := (&adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}).BulkUpdateAccounts(
			context.Background(),
			&BulkUpdateAccountsInput{
				AccountIDs:            []int64{account.ID},
				GroupIDs:              &emptyGroups,
				SkipMixedChannelCheck: true,
			},
		)

		require.NoError(t, err)
		require.Equal(t, []int64{account.ID}, result.FailedIDs)
		require.Empty(t, invalidator.snapshot(), "an uncommitted group-only change must preserve the current generation")
	})

	t.Run("equal credential patch", func(t *testing.T) {
		account := &Account{
			ID:          68,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"access_token": "same-token"},
		}
		repo := &openAIAccountRuntimeStateRepo{accounts: []*Account{account}}
		invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}

		result, err := (&adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}).BulkUpdateAccounts(
			context.Background(),
			&BulkUpdateAccountsInput{
				AccountIDs:  []int64{account.ID},
				Credentials: map[string]any{"access_token": "same-token"},
			},
		)

		require.NoError(t, err)
		require.Equal(t, []int64{account.ID}, result.SuccessIDs)
		require.Empty(t, invalidator.snapshot(), "an equal persisted identity must not rotate or trigger early recollection")
	})
}

func TestAdminSetAccountSchedulableInvalidatesOnlyOnActualChange(t *testing.T) {
	parent := &Account{ID: 70, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	repo := &openAIAccountRuntimeStateRepo{
		accounts: []*Account{parent},
		shadows: map[int64][]*Account{
			parent.ID: {{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth}},
		},
	}
	invalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	svc := &adminServiceImpl{accountRepo: repo, runtimeBlocker: invalidator}

	updated, err := svc.SetAccountSchedulable(context.Background(), parent.ID, false)
	require.NoError(t, err)
	require.False(t, updated.Schedulable)
	require.Equal(t, []int64{70, 71}, invalidator.snapshot())

	invalidator.ids = nil
	_, err = svc.SetAccountSchedulable(context.Background(), parent.ID, false)
	require.NoError(t, err)
	require.Empty(t, invalidator.snapshot(), "an unchanged schedulable flag must not rotate the runtime generation")
}

func TestAdminAvailabilityPersistenceFailuresDoNotInvalidateRuntimeState(t *testing.T) {
	persistErr := errors.New("persist failed")

	updateAccount := &Account{
		ID:          80,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       map[string]any{codexFingerprintSeedExtraKey: testCodexFingerprintSeed},
	}
	updateRepo := &openAIAccountRuntimeStateRepo{accounts: []*Account{updateAccount}, updateErr: persistErr}
	updateInvalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	_, err := (&adminServiceImpl{accountRepo: updateRepo, runtimeBlocker: updateInvalidator}).UpdateAccount(
		context.Background(), updateAccount.ID, &UpdateAccountInput{Status: StatusDisabled},
	)
	require.ErrorIs(t, err, persistErr)
	require.Empty(t, updateInvalidator.snapshot())

	groupAccount := &Account{
		ID:       83,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Extra:    map[string]any{codexFingerprintSeedExtraKey: testCodexFingerprintSeed},
		GroupIDs: []int64{9},
	}
	groupRepo := &openAIAccountRuntimeStateRepo{
		accounts:     []*Account{groupAccount},
		bindGroupErr: map[int64]error{groupAccount.ID: persistErr},
	}
	groupInvalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	emptyGroups := []int64{}
	_, err = (&adminServiceImpl{accountRepo: groupRepo, runtimeBlocker: groupInvalidator}).UpdateAccount(
		context.Background(), groupAccount.ID,
		&UpdateAccountInput{GroupIDs: &emptyGroups, SkipMixedChannelCheck: true},
	)
	require.ErrorIs(t, err, persistErr)
	require.Empty(t, groupInvalidator.snapshot())

	schedulableAccount := &Account{ID: 81, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	schedulableRepo := &openAIAccountRuntimeStateRepo{accounts: []*Account{schedulableAccount}, setSchedulableErr: persistErr}
	schedulableInvalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	_, err = (&adminServiceImpl{accountRepo: schedulableRepo, runtimeBlocker: schedulableInvalidator}).SetAccountSchedulable(
		context.Background(), schedulableAccount.ID, false,
	)
	require.ErrorIs(t, err, persistErr)
	require.Empty(t, schedulableInvalidator.snapshot())

	bulkAccount := &Account{ID: 82, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	bulkRepo := &openAIAccountRuntimeStateRepo{accounts: []*Account{bulkAccount}, bulkUpdateErr: persistErr}
	bulkInvalidator := &openAIAccountRuntimeStateInvalidatorSpy{}
	schedulable := false
	_, err = (&adminServiceImpl{accountRepo: bulkRepo, runtimeBlocker: bulkInvalidator}).BulkUpdateAccounts(
		context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{bulkAccount.ID}, Schedulable: &schedulable},
	)
	require.ErrorIs(t, err, persistErr)
	require.Empty(t, bulkInvalidator.snapshot())
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
	accountCompatCtx, _ := newTurnStateTestContext(t, 9, "compat-account")
	siblingCompatCtx, _ := newTurnStateTestContext(t, 9, "compat-sibling")
	accountCompatState := collectorTestToken(t, now, 2, 3)
	siblingCompatState := collectorTestToken(t, now, 2, 4)
	svc.bindOpenAICompatSessionTurnState(context.Background(), accountCompatCtx, &Account{ID: 30}, "compat-key", accountCompatState, "gpt-5")
	svc.bindOpenAICompatSessionResponseID(context.Background(), accountCompatCtx, &Account{ID: 30}, "compat-key", "resp-account", "gpt-5")
	svc.bindOpenAICompatSessionTurnState(context.Background(), siblingCompatCtx, &Account{ID: 31}, "compat-key", siblingCompatState, "gpt-5")
	svc.bindOpenAICompatSessionResponseID(context.Background(), siblingCompatCtx, &Account{ID: 31}, "compat-key", "resp-sibling", "gpt-5")
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
	_, accountCompatExists := svc.openaiCompatSessionResponses.Load(openAICompatSessionResponseKey(accountCompatCtx, &Account{ID: 30}, "compat-key"))
	siblingCompatRaw, siblingCompatExists := svc.openaiCompatSessionResponses.Load(openAICompatSessionResponseKey(siblingCompatCtx, &Account{ID: 31}, "compat-key"))
	require.False(t, accountCompatExists)
	require.True(t, siblingCompatExists)
	siblingCompatBinding, ok := siblingCompatRaw.(openAICompatSessionResponseBinding)
	require.True(t, ok)
	require.Equal(t, siblingCompatState, siblingCompatBinding.TurnState)
	require.Equal(t, "resp-sibling", siblingCompatBinding.ResponseID)
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

func TestOpenAIGatewayShadowLookupFailureInvalidatesAllIdentityState(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	shadowKey := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 91, Scope: "shadow-session", Model: "gpt-5"})
	otherKey := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 92, Scope: "other-session", Model: "gpt-5"})
	shadowState := collectorTestToken(t, now, 2, 31)
	otherState := collectorTestToken(t, now, 2, 32)
	require.True(t, collector.OfferValueMust(shadowKey, shadowState, "shadow-route", now))
	require.True(t, collector.OfferValueMust(otherKey, otherState, "other-route", now))

	stateStore := NewOpenAIWSStateStore(nil)
	stateStore.BindSessionTurnState(1, 91, "shadow-session", shadowState, time.Hour, "gpt-5")
	stateStore.BindSessionTurnState(1, 92, "other-session", otherState, time.Hour, "gpt-5")
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSFakeDialer{})
	t.Cleanup(pool.Close)
	for _, accountID := range []int64{91, 92} {
		lease, err := pool.Acquire(context.Background(), openAIWSAcquireRequest{
			Account: &Account{ID: accountID, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			WSURL:   "wss://example.com/v1/responses",
			Headers: http.Header{},
		})
		require.NoError(t, err)
		lease.Release()
	}

	parent := &Account{
		ID:          90,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"access_token": "old-parent-token"},
	}
	repo := &openAIAccountRuntimeStateRepo{
		accounts:        []*Account{parent},
		shadowLookupErr: errors.New("database unavailable"),
	}
	svc := &OpenAIGatewayService{
		accountRepo:             repo,
		codexTurnStateCollector: collector,
		openaiWSStateStore:      stateStore,
		openaiWSPool:            pool,
	}
	compatCtx, _ := newTurnStateTestContext(t, 17, "shadow-session")
	shadowAccount := &Account{ID: 91}
	compatKey := openAICompatSessionResponseKey(compatCtx, shadowAccount, "shadow-prompt")
	svc.openaiCompatSessionResponses.Store(compatKey, openAICompatSessionResponseBinding{
		ResponseID:          "resp-old-parent",
		TurnState:           shadowState,
		Model:               "gpt-5",
		TurnStateGeneration: shadowKey.generation,
		ExpiresAt:           now.Add(time.Hour),
	})
	svc.openaiCodexTurnStateOrigins.Store("shadow-origin", openAICodexTurnStateOrigin{accountID: 91, generation: shadowKey.generation})
	svc.openaiCodexTurnStateInvalidations.Store("shadow-invalidation", openAICodexTurnStateInvalidation{generation: shadowKey.generation})

	result, err := (&adminServiceImpl{accountRepo: repo, runtimeBlocker: svc}).BulkUpdateAccounts(
		context.Background(),
		&BulkUpdateAccountsInput{
			AccountIDs:  []int64{parent.ID},
			Credentials: map[string]any{"access_token": "new-parent-token"},
		},
	)
	require.NoError(t, err)
	require.Equal(t, []int64{parent.ID}, result.SuccessIDs)
	require.Equal(t, "new-parent-token", parent.Credentials["access_token"])

	require.False(t, collector.IsCurrentKey(shadowKey))
	require.False(t, collector.IsCurrentKey(otherKey))
	_, shadowUsable := collector.Acquire(shadowKey, now)
	_, otherUsable := collector.Acquire(otherKey, now)
	require.False(t, shadowUsable)
	require.False(t, otherUsable)
	newShadowKey := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 91, Scope: "shadow-session", Model: "gpt-5"})
	require.NotEqual(t, shadowKey.generation, newShadowKey.generation,
		"a request from the old parent identity must not become current again")

	_, compatExists := svc.openaiCompatSessionResponses.Load(compatKey)
	require.False(t, compatExists)
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), compatCtx, shadowAccount, "shadow-prompt", "gpt-5"),
		"the next shadow request must not reuse state minted by the previous parent identity")
	_, originExists := svc.openaiCodexTurnStateOrigins.Load("shadow-origin")
	_, invalidationExists := svc.openaiCodexTurnStateInvalidations.Load("shadow-invalidation")
	require.False(t, originExists)
	require.False(t, invalidationExists)
	_, shadowWSStateExists := stateStore.GetSessionTurnState(1, 91, "shadow-session", "gpt-5")
	_, otherWSStateExists := stateStore.GetSessionTurnState(1, 92, "other-session", "gpt-5")
	require.False(t, shadowWSStateExists)
	require.False(t, otherWSStateExists)
	_, _, shadowConns := pool.AccountPoolLoad(91)
	_, _, otherConns := pool.AccountPoolLoad(92)
	require.Zero(t, shadowConns)
	require.Zero(t, otherConns)
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
