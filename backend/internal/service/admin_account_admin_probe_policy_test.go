package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

func accountAdminScopedContext(ownerID int64) context.Context {
	return context.WithValue(context.Background(), ctxkey.AccountAdminID, ownerID)
}

func accountAdminProbePolicyUser(ownerID int64, multiplier float64) *User {
	return &User{
		ID:                   ownerID,
		Role:                 RoleAccountAdmin,
		Status:               StatusActive,
		SupplyRateMultiplier: multiplier,
	}
}

func TestAccountAdminUpdateAccountCannotReenableUpstreamBillingPolicy(t *testing.T) {
	ownerID := int64(41)
	accountID := int64(101)
	oldRate := 0.25
	account := &Account{
		ID:             accountID,
		Name:           "owned",
		Platform:       PlatformOpenAI,
		Type:           AccountTypeAPIKey,
		Status:         StatusActive,
		AccountAdminID: &ownerID,
		RateMultiplier: &oldRate,
		Credentials:    map[string]any{"api_key": "sk-test"},
		Extra: map[string]any{
			UpstreamBillingProbeEnabledExtraKey:    true,
			UpstreamBillingRateSyncEnabledExtraKey: true,
		},
	}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{accountID: account}}
	users := &supplyRateUserRepoStub{user: accountAdminProbePolicyUser(ownerID, 0.375)}
	svc := &adminServiceImpl{accountRepo: repo, userRepo: users}
	enabled := true

	updated, err := svc.UpdateAccount(accountAdminScopedContext(ownerID), accountID, &UpdateAccountInput{
		ProbeEnabled: &enabled,
		Extra: map[string]any{
			UpstreamBillingProbeEnabledExtraKey:    true,
			UpstreamBillingRateSyncEnabledExtraKey: true,
			"operator_note":                        "edited",
		},
	})

	require.NoError(t, err)
	require.NotNil(t, updated)
	require.NotNil(t, updated.AccountAdminID)
	require.Equal(t, ownerID, *updated.AccountAdminID)
	require.Equal(t, 0.375, *updated.RateMultiplier)
	require.False(t, updated.Extra[UpstreamBillingProbeEnabledExtraKey].(bool))
	require.False(t, updated.Extra[UpstreamBillingRateSyncEnabledExtraKey].(bool))
	require.Equal(t, "edited", updated.Extra["operator_note"])
}

func TestAccountAdminBulkUpdateCannotReenableUpstreamBillingPolicy(t *testing.T) {
	ownerID := int64(42)
	accountID := int64(102)
	oldRate := 0.2
	account := &Account{
		ID:             accountID,
		Name:           "owned",
		Platform:       PlatformOpenAI,
		Type:           AccountTypeOAuth,
		Status:         StatusActive,
		AccountAdminID: &ownerID,
		RateMultiplier: &oldRate,
	}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{accountID: account}}
	users := &supplyRateUserRepoStub{user: accountAdminProbePolicyUser(ownerID, 0.625)}
	svc := &adminServiceImpl{accountRepo: repo, userRepo: users}
	enabled := true

	result, err := svc.BulkUpdateAccounts(accountAdminScopedContext(ownerID), &BulkUpdateAccountsInput{
		AccountIDs:   []int64{accountID},
		ProbeEnabled: &enabled,
		Extra: map[string]any{
			UpstreamBillingProbeEnabledExtraKey:    true,
			UpstreamBillingRateSyncEnabledExtraKey: true,
			"operator_note":                        "bulk-edited",
		},
	})

	require.NoError(t, err)
	require.Equal(t, 1, result.Success)
	require.Len(t, repo.bulkUpdates, 1)
	update := repo.bulkUpdates[0]
	require.Nil(t, update.ProbeEnabled)
	require.NotNil(t, update.RateMultiplier)
	require.Equal(t, 0.625, *update.RateMultiplier)
	require.False(t, update.Extra[UpstreamBillingProbeEnabledExtraKey].(bool))
	require.False(t, update.Extra[UpstreamBillingRateSyncEnabledExtraKey].(bool))
	require.Equal(t, "bulk-edited", update.Extra["operator_note"])
}

type accountAdminFilterScopeRepo struct {
	*upstreamBillingProbeAccountRepo
	listOwnerID int64
	bulkIDs     []int64
}

func (r *accountAdminFilterScopeRepo) ListWithFilters(
	ctx context.Context,
	_ pagination.PaginationParams,
	_ string,
	_ string,
	_ string,
	_ string,
	_ int64,
	_ string,
) ([]Account, *pagination.PaginationResult, error) {
	ownerID, _ := ctxkey.AccountAdminIDFromContext(ctx)
	r.listOwnerID = ownerID
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account == nil || account.AccountAdminID == nil || *account.AccountAdminID != ownerID {
			continue
		}
		accounts = append(accounts, *account)
	}
	return accounts, &pagination.PaginationResult{Total: int64(len(accounts))}, nil
}

func (r *accountAdminFilterScopeRepo) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	r.bulkIDs = append([]int64(nil), ids...)
	return r.upstreamBillingProbeAccountRepo.BulkUpdate(ctx, ids, updates)
}

func TestAccountAdminBulkUpdateFiltersStayWithinOwnerScope(t *testing.T) {
	ownerID := int64(42)
	foreignID := int64(77)
	owned := &Account{ID: 101, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, AccountAdminID: &ownerID}
	foreign := &Account{ID: 202, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, AccountAdminID: &foreignID}
	repo := &accountAdminFilterScopeRepo{
		upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
			accounts: map[int64]*Account{owned.ID: owned, foreign.ID: foreign},
		},
	}
	svc := &adminServiceImpl{
		accountRepo: repo,
		userRepo:    &supplyRateUserRepoStub{user: accountAdminProbePolicyUser(ownerID, 0.5)},
	}
	schedulable := true

	result, err := svc.BulkUpdateAccounts(accountAdminScopedContext(ownerID), &BulkUpdateAccountsInput{
		Filters:     &BulkUpdateAccountFilters{Platform: PlatformOpenAI, Status: StatusActive},
		Schedulable: &schedulable,
	})

	require.NoError(t, err)
	require.Equal(t, ownerID, repo.listOwnerID, "filter resolution must carry the authenticated owner scope")
	require.Equal(t, []int64{owned.ID}, repo.bulkIDs)
	require.Equal(t, []int64{owned.ID}, result.SuccessIDs)
	require.NotContains(t, repo.bulkIDs, foreign.ID)
}
