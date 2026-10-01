//go:build unit

package service

import (
	"context"

	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

// --- mock: UserRepository ---

type mockUserRepo struct {
	updateBalanceErr         error
	updateBalanceFn          func(ctx context.Context, id int64, amount float64) error
	deductBalanceFn          func(ctx context.Context, id int64, amount float64) error
	deductAvailableBalanceFn func(ctx context.Context, id int64, amount float64) (float64, error)
	getByIDUser              *User
	getByIDErr               error
	updateLastActiveErr      error
	updateLastActiveUserIDs  []int64
	updateLastActiveAt       []time.Time
	updateFn                 func(ctx context.Context, user *User) error
	updateCalls              int
	updateFields             []UserUpdateFields
}

type mockUserSettingRepo struct {
	values map[string]string
}

func (m *mockUserSettingRepo) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (m *mockUserSettingRepo) GetValue(context.Context, string) (string, error) {
	panic("unexpected GetValue call")
}

func (m *mockUserSettingRepo) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (m *mockUserSettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := m.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (m *mockUserSettingRepo) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (m *mockUserSettingRepo) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (m *mockUserSettingRepo) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func (m *mockUserRepo) Create(context.Context, *User) error { return nil }

func (m *mockUserRepo) GetByID(ctx context.Context, _ int64) (*User, error) {
	if m.getByIDErr != nil {
		return nil, m.getByIDErr
	}
	if m.getByIDUser != nil {
		cloned := *m.getByIDUser
		return &cloned, nil
	}
	return &User{}, nil
}
func (m *mockUserRepo) GetByEmail(context.Context, string) (*User, error) { return &User{}, nil }
func (m *mockUserRepo) GetFirstAdmin(context.Context) (*User, error)      { return &User{}, nil }
func (m *mockUserRepo) Update(ctx context.Context, user *User, fields UserUpdateFields) error {
	m.updateCalls++
	m.updateFields = append(m.updateFields, fields)
	if m.updateFn != nil {
		return m.updateFn(ctx, user)
	}
	return nil
}
func (m *mockUserRepo) Delete(context.Context, int64) error { return nil }

func (m *mockUserRepo) List(context.Context, pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *mockUserRepo) ListWithFilters(context.Context, pagination.PaginationParams, UserListFilters) ([]User, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *mockUserRepo) UpdateBalance(ctx context.Context, id int64, amount float64) error {
	if m.updateBalanceFn != nil {
		return m.updateBalanceFn(ctx, id, amount)
	}
	return m.updateBalanceErr
}
func (m *mockUserRepo) UpdateUserLastActiveAt(_ context.Context, userID int64, activeAt time.Time) error {
	m.updateLastActiveUserIDs = append(m.updateLastActiveUserIDs, userID)
	m.updateLastActiveAt = append(m.updateLastActiveAt, activeAt)
	return m.updateLastActiveErr
}
func (m *mockUserRepo) DeductBalance(ctx context.Context, id int64, amount float64) error {
	if m.deductBalanceFn != nil {
		return m.deductBalanceFn(ctx, id, amount)
	}
	return nil
}

func (m *mockUserRepo) DeductAvailableBalance(ctx context.Context, id int64, amount float64) (float64, error) {
	if m.deductAvailableBalanceFn != nil {
		return m.deductAvailableBalanceFn(ctx, id, amount)
	}
	return amount, nil
}

func (m *mockUserRepo) AdjustBalance(ctx context.Context, id int64, delta float64) (BalanceChange, error) {
	panic("unexpected AdjustBalance call")
}

func (m *mockUserRepo) SetBalance(ctx context.Context, id int64, value float64) (BalanceChange, error) {
	panic("unexpected SetBalance call")
}
func (m *mockUserRepo) UpdateConcurrency(context.Context, int64, int) error { return nil }
func (m *mockUserRepo) ExistsByEmail(context.Context, string) (bool, error) { return false, nil }

func (m *mockUserRepo) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	return 0, nil
}

func (m *mockUserRepo) BatchSetConcurrency(context.Context, []int64, int) (int, error) { return 0, nil }
func (m *mockUserRepo) BatchAddConcurrency(context.Context, []int64, int) (int, error) { return 0, nil }
func (m *mockUserRepo) BatchUpdateLimits(context.Context, []int64, *int, *int) (int, error) {
	return 0, nil
}
func (m *mockUserRepo) AddGroupToAllowedGroups(context.Context, int64, int64) error { return nil }

func (m *mockUserRepo) GetLatestUsedAtByUserIDs(context.Context, []int64) (map[int64]*time.Time, error) {
	return map[int64]*time.Time{}, nil
}
func (m *mockUserRepo) GetLatestUsedAtByUserID(context.Context, int64) (*time.Time, error) {
	return nil, nil
}
func (m *mockUserRepo) UpdateTotpSecret(context.Context, int64, *string) error { return nil }
func (m *mockUserRepo) EnableTotp(context.Context, int64) error                { return nil }
func (m *mockUserRepo) DisableTotp(context.Context, int64) error               { return nil }
func (m *mockUserRepo) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	return nil
}

func (m *mockUserRepo) GetByIDIncludeDeleted(ctx context.Context, id int64) (*User, error) {
	return m.GetByID(ctx, id)
}

// --- mock: APIKeyAuthCacheInvalidator ---

type mockAuthCacheInvalidator struct {
	invalidatedUserIDs []int64
	mu                 sync.Mutex
}

func (m *mockAuthCacheInvalidator) InvalidateAuthCacheByKey(context.Context, string)    {}
func (m *mockAuthCacheInvalidator) InvalidateAuthCacheByGroupID(context.Context, int64) {}
func (m *mockAuthCacheInvalidator) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invalidatedUserIDs = append(m.invalidatedUserIDs, userID)
}

// --- mock: BillingCache ---

type mockBillingCache struct {
	invalidateErr       error
	invalidateCallCount atomic.Int64
	invalidatedUserIDs  []int64
	mu                  sync.Mutex
}

func (m *mockBillingCache) GetUserBalance(context.Context, int64) (float64, error)  { return 0, nil }
func (m *mockBillingCache) SetUserBalance(context.Context, int64, float64) error    { return nil }
func (m *mockBillingCache) DeductUserBalance(context.Context, int64, float64) error { return nil }
func (m *mockBillingCache) InvalidateUserBalance(_ context.Context, userID int64) error {
	m.invalidateCallCount.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invalidatedUserIDs = append(m.invalidatedUserIDs, userID)
	return m.invalidateErr
}
func (m *mockBillingCache) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return nil, nil
}
func (m *mockBillingCache) SetSubscriptionCache(context.Context, int64, int64, *SubscriptionCacheData) error {
	return nil
}
func (m *mockBillingCache) UpdateSubscriptionUsage(context.Context, int64, int64, float64) error {
	return nil
}
func (m *mockBillingCache) InvalidateSubscriptionCache(context.Context, int64, int64) error {
	return nil
}
func (m *mockBillingCache) GetAPIKeyRateLimit(context.Context, int64) (*APIKeyRateLimitCacheData, error) {
	return nil, nil
}
func (m *mockBillingCache) SetAPIKeyRateLimit(context.Context, int64, *APIKeyRateLimitCacheData) error {
	return nil
}
func (m *mockBillingCache) UpdateAPIKeyRateLimitUsage(context.Context, int64, float64) error {
	return nil
}
func (m *mockBillingCache) InvalidateAPIKeyRateLimit(context.Context, int64) error {
	return nil
}

func (m *mockBillingCache) GetUserPlatformQuotaCache(context.Context, int64, string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func (m *mockBillingCache) SetUserPlatformQuotaCache(context.Context, int64, string, *UserPlatformQuotaCacheEntry, time.Duration) error {
	return nil
}

func (m *mockBillingCache) DeleteUserPlatformQuotaCache(context.Context, int64, string) error {
	return nil
}

func (m *mockBillingCache) IncrUserPlatformQuotaUsageCache(context.Context, int64, string, float64, time.Duration, bool) error {
	return nil
}

func (m *mockBillingCache) PopDirtyUserPlatformQuotaKeys(context.Context, int) ([]UserPlatformQuotaKey, error) {
	return nil, nil
}

func (m *mockBillingCache) ReaddDirtyUserPlatformQuotaKeys(context.Context, []UserPlatformQuotaKey) error {
	return nil
}

func (m *mockBillingCache) BatchGetUserPlatformQuotaCache(context.Context, []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error) {
	return nil, nil
}

// --- 测试 ---

func TestTouchLastActive_UpdatesWhenStale(t *testing.T) {
	stale := time.Now().Add(-11 * time.Minute)
	repo := &mockUserRepo{
		getByIDUser: &User{
			ID:           42,
			LastActiveAt: &stale,
		},
	}
	svc := NewUserService(repo, nil, nil, nil)

	svc.TouchLastActive(context.Background(), 42)

	require.Equal(t, []int64{42}, repo.updateLastActiveUserIDs)
	require.Len(t, repo.updateLastActiveAt, 1)
	require.WithinDuration(t, time.Now(), repo.updateLastActiveAt[0], 2*time.Second)
}

func TestTouchLastActive_SkipsWhenRecent(t *testing.T) {
	recent := time.Now().Add(-time.Minute)
	repo := &mockUserRepo{
		getByIDUser: &User{
			ID:           42,
			LastActiveAt: &recent,
		},
	}
	svc := NewUserService(repo, nil, nil, nil)

	svc.TouchLastActive(context.Background(), 42)

	require.Empty(t, repo.updateLastActiveUserIDs)
	require.Empty(t, repo.updateLastActiveAt)
}

func TestNewUserService_FieldsAssignment(t *testing.T) {
	repo := &mockUserRepo{}
	auth := &mockAuthCacheInvalidator{}
	cache := &mockBillingCache{}

	svc := NewUserService(repo, nil, auth, cache)
	require.NotNil(t, svc)
	require.Equal(t, repo, svc.userRepo)
	require.Equal(t, auth, svc.authCacheInvalidator)
	require.Equal(t, cache, svc.billingCache)
}
