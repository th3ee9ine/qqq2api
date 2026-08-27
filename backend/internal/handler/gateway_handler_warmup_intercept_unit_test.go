//go:build unit

package handler

import (
	"context"
	"testing"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// Shared lightweight fixtures for active Claude gateway tests. The retired
// Antigravity warmup scenarios that originally lived here have been removed.

type fakeSchedulerCache struct {
	accounts []*service.Account
}

func (f *fakeSchedulerCache) GetSnapshot(_ context.Context, _ service.SchedulerBucket) ([]*service.Account, bool, error) {
	return f.accounts, true, nil
}
func (f *fakeSchedulerCache) CaptureBucketWriteToken(_ context.Context, bucket service.SchedulerBucket) (service.SchedulerBucketWriteToken, error) {
	return service.SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}
func (f *fakeSchedulerCache) SetSnapshot(_ context.Context, _ service.SchedulerBucket, _ service.SchedulerBucketWriteToken, _ []service.Account) error {
	return nil
}
func (f *fakeSchedulerCache) RetireBucket(_ context.Context, _ service.SchedulerBucket) error {
	return nil
}
func (f *fakeSchedulerCache) ReopenBucket(_ context.Context, bucket service.SchedulerBucket) (service.SchedulerBucketWriteToken, error) {
	return service.SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}
func (f *fakeSchedulerCache) TryAcquireGroupLifecycleLease(_ context.Context, _ int64, _ time.Duration) (service.SchedulerGroupLifecycleLease, bool, error) {
	return service.SchedulerGroupLifecycleLease{}, false, nil
}
func (f *fakeSchedulerCache) ReleaseGroupLifecycleLease(_ context.Context, _ service.SchedulerGroupLifecycleLease) error {
	return nil
}
func (f *fakeSchedulerCache) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	for _, account := range f.accounts {
		if account != nil && account.ID == id {
			return account, nil
		}
	}
	return nil, nil
}
func (f *fakeSchedulerCache) SetAccount(_ context.Context, _ *service.Account) error { return nil }
func (f *fakeSchedulerCache) DeleteAccount(_ context.Context, _ int64) error         { return nil }
func (f *fakeSchedulerCache) UpdateLastUsed(_ context.Context, _ map[int64]time.Time) error {
	return nil
}
func (f *fakeSchedulerCache) TryLockBucket(_ context.Context, _ service.SchedulerBucket, _ time.Duration) (bool, error) {
	return true, nil
}
func (f *fakeSchedulerCache) UnlockBucket(_ context.Context, _ service.SchedulerBucket) error {
	return nil
}
func (f *fakeSchedulerCache) ListBuckets(_ context.Context) ([]service.SchedulerBucket, error) {
	return nil, nil
}
func (f *fakeSchedulerCache) GetOutboxWatermark(_ context.Context) (int64, error) { return 0, nil }
func (f *fakeSchedulerCache) SetOutboxWatermark(_ context.Context, _ int64) error { return nil }

type fakeGroupRepo struct {
	group *service.Group
}

func (f *fakeGroupRepo) Create(context.Context, *service.Group) error { return nil }
func (f *fakeGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	return f.group, nil
}
func (f *fakeGroupRepo) GetByIDLite(context.Context, int64) (*service.Group, error) {
	return f.group, nil
}
func (f *fakeGroupRepo) Update(context.Context, *service.Group) error          { return nil }
func (f *fakeGroupRepo) Delete(context.Context, int64) error                   { return nil }
func (f *fakeGroupRepo) DeleteCascade(context.Context, int64) ([]int64, error) { return nil, nil }
func (f *fakeGroupRepo) List(context.Context, pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (f *fakeGroupRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]service.Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (f *fakeGroupRepo) ListActive(context.Context) ([]service.Group, error) { return nil, nil }
func (f *fakeGroupRepo) ListActiveByPlatform(context.Context, string) ([]service.Group, error) {
	return nil, nil
}
func (f *fakeGroupRepo) ExistsByName(context.Context, string) (bool, error) { return false, nil }
func (f *fakeGroupRepo) GetAccountCount(context.Context, int64) (int64, int64, error) {
	return 0, 0, nil
}
func (f *fakeGroupRepo) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	return 0, nil
}
func (f *fakeGroupRepo) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	return nil, nil
}
func (f *fakeGroupRepo) BindAccountsToGroup(context.Context, int64, []int64) error { return nil }
func (f *fakeGroupRepo) UpdateSortOrders(context.Context, []service.GroupSortOrderUpdate) error {
	return nil
}

type fakeConcurrencyCache struct{}

func (f *fakeConcurrencyCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) ReleaseAccountSlot(context.Context, int64, string) error { return nil }
func (f *fakeConcurrencyCache) GetAccountConcurrency(context.Context, int64) (int, error) {
	return 0, nil
}
func (f *fakeConcurrencyCache) IncrementAccountWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) DecrementAccountWaitCount(context.Context, int64) error { return nil }
func (f *fakeConcurrencyCache) GetAccountWaitingCount(context.Context, int64) (int, error) {
	return 0, nil
}
func (f *fakeConcurrencyCache) AcquireUserSlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) ReleaseUserSlot(context.Context, int64, string) error   { return nil }
func (f *fakeConcurrencyCache) GetUserConcurrency(context.Context, int64) (int, error) { return 0, nil }
func (f *fakeConcurrencyCache) IncrementWaitCount(context.Context, int64, int) (bool, error) {
	return true, nil
}
func (f *fakeConcurrencyCache) DecrementWaitCount(context.Context, int64) error { return nil }
func (f *fakeConcurrencyCache) GetAccountsLoadBatch(context.Context, []service.AccountWithConcurrency) (map[int64]*service.AccountLoadInfo, error) {
	return map[int64]*service.AccountLoadInfo{}, nil
}
func (f *fakeConcurrencyCache) GetUsersLoadBatch(context.Context, []service.UserWithConcurrency) (map[int64]*service.UserLoadInfo, error) {
	return map[int64]*service.UserLoadInfo{}, nil
}
func (f *fakeConcurrencyCache) GetAccountConcurrencyBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	result := make(map[int64]int, len(accountIDs))
	for _, id := range accountIDs {
		result[id] = 0
	}
	return result, nil
}
func (f *fakeConcurrencyCache) CleanupExpiredAccountSlots(context.Context, int64) error { return nil }
func (f *fakeConcurrencyCache) CleanupExpiredAccountSlotKeys(context.Context) error     { return nil }
func (f *fakeConcurrencyCache) CleanupStaleProcessSlots(context.Context, string) error  { return nil }

func newTestGatewayHandler(t *testing.T, group *service.Group, accounts []*service.Account) (*GatewayHandler, func()) {
	t.Helper()

	schedulerCache := &fakeSchedulerCache{accounts: accounts}
	schedulerSnapshot := service.NewSchedulerSnapshotService(schedulerCache, nil, nil, nil, nil)

	gwSvc := service.NewGatewayService(
		nil, // accountRepo (not used: scheduler snapshot hit)
		&fakeGroupRepo{group: group},
		nil, // usageLogRepo
		nil, // usageBillingRepo
		nil, // userRepo
		nil, // userSubRepo
		nil, // userGroupRateRepo
		nil, // cache (disable sticky)
		nil, // cfg
		schedulerSnapshot,
		nil, // concurrencyService (disable load-aware; tryAcquire always acquired)
		nil, // billingService
		nil, // rateLimitService
		nil, // billingCacheService
		nil, // identityService
		nil, // httpUpstream
		nil, // deferredService
		nil, // claudeTokenProvider
		nil, // sessionLimitCache
		nil, // rpmCache
		nil, // digestStore
		nil, // settingService
		nil, // tlsFPProfileService
		nil, // channelService
		nil, // resolver
		nil, // compositeResolver
		nil, // balanceNotifyService
		nil, // userPlatformQuotaRepo
	)

	// RunModeSimple：跳过计费检查，避免引入 repo/cache 依赖。
	cfg := &config.Config{RunMode: config.RunModeSimple}
	billingCacheSvc := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)

	concurrencySvc := service.NewConcurrencyService(&fakeConcurrencyCache{})
	concurrencyHelper := NewConcurrencyHelper(concurrencySvc, SSEPingFormatClaude, 0)

	h := &GatewayHandler{
		gatewayService:      gwSvc,
		billingCacheService: billingCacheSvc,
		concurrencyHelper:   concurrencyHelper,
		// 这些字段对本测试不敏感，保持较小即可
		maxAccountSwitches:       1,
		maxAccountSwitchesGemini: 1,
	}

	cleanup := func() {
		billingCacheSvc.Stop()
	}
	return h, cleanup
}
