//go:build unit

package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type billingCacheMissStub struct {
	setBalanceCalls             atomic.Int64
	setSubscriptionCalls        atomic.Int64
	updateSubscriptionCalls     atomic.Int64
	invalidateSubscriptionCalls atomic.Int64
}

func (s *billingCacheMissStub) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	return 0, errors.New("cache miss")
}

func (s *billingCacheMissStub) SetUserBalance(ctx context.Context, userID int64, balance float64) error {
	s.setBalanceCalls.Add(1)
	return nil
}

func (s *billingCacheMissStub) DeductUserBalance(ctx context.Context, userID int64, amount float64) error {
	return nil
}

func (s *billingCacheMissStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	return nil
}

func (s *billingCacheMissStub) GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error) {
	return nil, errors.New("cache miss")
}

func (s *billingCacheMissStub) SetSubscriptionCache(ctx context.Context, userID, groupID int64, data *SubscriptionCacheData) error {
	s.setSubscriptionCalls.Add(1)
	return nil
}

func (s *billingCacheMissStub) UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, cost float64) error {
	s.updateSubscriptionCalls.Add(1)
	return nil
}

func (s *billingCacheMissStub) InvalidateSubscriptionCache(ctx context.Context, userID, groupID int64) error {
	s.invalidateSubscriptionCalls.Add(1)
	return nil
}

func (s *billingCacheMissStub) GetAPIKeyRateLimit(ctx context.Context, keyID int64) (*APIKeyRateLimitCacheData, error) {
	return nil, errors.New("cache miss")
}

func (s *billingCacheMissStub) SetAPIKeyRateLimit(ctx context.Context, keyID int64, data *APIKeyRateLimitCacheData) error {
	return nil
}

func (s *billingCacheMissStub) UpdateAPIKeyRateLimitUsage(ctx context.Context, keyID int64, cost float64) error {
	return nil
}

func (s *billingCacheMissStub) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	return nil
}

func (s *billingCacheMissStub) GetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func (s *billingCacheMissStub) SetUserPlatformQuotaCache(ctx context.Context, userID int64, platform string, entry *UserPlatformQuotaCacheEntry, ttl time.Duration) error {
	return nil
}

func (s *billingCacheMissStub) DeleteUserPlatformQuotaCache(ctx context.Context, userID int64, platform string) error {
	return nil
}

func (s *billingCacheMissStub) IncrUserPlatformQuotaUsageCache(ctx context.Context, userID int64, platform string, cost float64, ttl time.Duration, markDirty bool) error {
	return nil
}

func (s *billingCacheMissStub) PopDirtyUserPlatformQuotaKeys(ctx context.Context, n int) ([]UserPlatformQuotaKey, error) {
	return nil, nil
}

func (s *billingCacheMissStub) ReaddDirtyUserPlatformQuotaKeys(ctx context.Context, keys []UserPlatformQuotaKey) error {
	return nil
}

func (s *billingCacheMissStub) BatchGetUserPlatformQuotaCache(ctx context.Context, keys []UserPlatformQuotaKey) ([]*UserPlatformQuotaCacheEntry, error) {
	return nil, nil
}

type balanceLoadUserRepoStub struct {
	mockUserRepo
	calls   atomic.Int64
	delay   time.Duration
	balance float64
}

func (s *balanceLoadUserRepoStub) GetByID(ctx context.Context, id int64) (*User, error) {
	s.calls.Add(1)
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &User{ID: id, Balance: s.balance}, nil
}

func TestBillingCacheServiceGetUserBalance_Singleflight(t *testing.T) {
	cache := &billingCacheMissStub{}
	userRepo := &balanceLoadUserRepoStub{
		delay:   80 * time.Millisecond,
		balance: 12.34,
	}
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	const goroutines = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	balCh := make(chan float64, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			bal, err := svc.GetUserBalance(context.Background(), 99)
			errCh <- err
			balCh <- bal
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)
	close(balCh)

	for err := range errCh {
		require.NoError(t, err)
	}
	for bal := range balCh {
		require.Equal(t, 12.34, bal)
	}

	require.Equal(t, int64(1), userRepo.calls.Load(), "并发穿透应被 singleflight 合并")
	require.Eventually(t, func() bool {
		return cache.setBalanceCalls.Load() >= 1
	}, time.Second, 10*time.Millisecond)
}

type subscriptionLoadRepoStub struct {
	UserSubscriptionRepository
	calls  atomic.Int64
	result *UserSubscription
}

func (s *subscriptionLoadRepoStub) GetActiveByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*UserSubscription, error) {
	s.calls.Add(1)
	return s.result, nil
}

type subscriptionGenerationCacheStub struct {
	billingCacheMissStub
	mu              sync.Mutex
	data            *SubscriptionCacheData
	generation      atomic.Int64
	fillCalls       atomic.Int64
	invalidateCalls atomic.Int64
	fillExpected    atomic.Int64
	fillResult      bool
	generationErr   error
	generationHook  func(context.Context) error
}

func (s *subscriptionGenerationCacheStub) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data == nil {
		return nil, errors.New("cache miss")
	}
	data := *s.data
	return &data, nil
}

func (s *subscriptionGenerationCacheStub) GetSubscriptionCacheGeneration(ctx context.Context, _, _ int64) (int64, error) {
	if s.generationHook != nil {
		if err := s.generationHook(ctx); err != nil {
			return 0, err
		}
	}
	return s.generation.Load(), s.generationErr
}

func (s *subscriptionGenerationCacheStub) SetSubscriptionCacheIfGeneration(_ context.Context, _ int64, _ int64, expectedGeneration int64, data *SubscriptionCacheData) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fillCalls.Add(1)
	s.fillExpected.Store(expectedGeneration)
	if expectedGeneration != s.generation.Load() || s.data != nil || !s.fillResult {
		return false, nil
	}
	copy := *data
	s.data = &copy
	return true, nil
}

func (s *subscriptionGenerationCacheStub) InvalidateSubscriptionCacheAndBumpGeneration(context.Context, int64, int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidateCalls.Add(1)
	s.generation.Add(1)
	s.data = nil
	return nil
}

func TestBillingCacheServiceGetSubscriptionStatus_SingleflightAndConditionalFill(t *testing.T) {
	cache := &subscriptionGenerationCacheStub{fillResult: true}
	subRepo := newSubscriptionControlledRepoStub()
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	firstCtx := newSubscriptionWaitContext(context.Background())
	firstResult := startSubscriptionStatusLoad(svc, firstCtx)
	firstLoad := awaitSubscriptionLoad(t, subRepo.started)
	awaitSubscriptionWait(t, firstCtx.waiting)

	secondCtx := newSubscriptionWaitContext(context.Background())
	secondResult := startSubscriptionStatusLoad(svc, secondCtx)
	awaitSubscriptionWait(t, secondCtx.waiting)
	firstLoad.result <- subscriptionWithUsage(1.25)

	first := awaitSubscriptionStatus(t, firstResult)
	second := awaitSubscriptionStatus(t, secondResult)
	require.NoError(t, first.err)
	require.NoError(t, second.err)
	require.Equal(t, 1.25, first.data.DailyUsage)
	require.Equal(t, 1.25, second.data.DailyUsage)
	require.Equal(t, int64(1), subRepo.calls.Load(), "concurrent subscription misses should share one DB read")
	require.Equal(t, int64(1), cache.fillCalls.Load(), "singleflight should conditionally fill once")
	require.Zero(t, cache.fillExpected.Load())
	first.data.DailyUsage = 99
	require.Equal(t, 1.25, second.data.DailyUsage, "callers must not share mutable cache data")
}

func TestBillingCacheServiceQueueUpdateSubscriptionUsageInvalidatesGeneration(t *testing.T) {
	cache := &subscriptionGenerationCacheStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	svc.QueueUpdateSubscriptionUsage(42, 84, 1.5)
	svc.Stop()

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(1), cache.generation.Load())
	require.Zero(t, cache.updateSubscriptionCalls.Load(), "persisted usage must not also be added as a cache delta")
}

// The cache stubs never inspect Done. Observing it here means the caller has
// registered its singleflight wait, so overlapping loads need no timing sleeps.
type subscriptionWaitContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func newSubscriptionWaitContext(ctx context.Context) *subscriptionWaitContext {
	return &subscriptionWaitContext{Context: ctx, waiting: make(chan struct{})}
}

func (c *subscriptionWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

type subscriptionControlledLoad struct {
	ctx    context.Context
	result chan *UserSubscription
}

type subscriptionControlledRepoStub struct {
	UserSubscriptionRepository
	calls   atomic.Int64
	started chan subscriptionControlledLoad
}

func newSubscriptionControlledRepoStub() *subscriptionControlledRepoStub {
	return &subscriptionControlledRepoStub{started: make(chan subscriptionControlledLoad, 4)}
}

func (s *subscriptionControlledRepoStub) GetActiveByUserIDAndGroupID(ctx context.Context, _, _ int64) (*UserSubscription, error) {
	s.calls.Add(1)
	load := subscriptionControlledLoad{ctx: ctx, result: make(chan *UserSubscription, 1)}
	select {
	case s.started <- load:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-load.result:
		return result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type subscriptionStatusResult struct {
	data *subscriptionCacheData
	err  error
}

func startSubscriptionStatusLoad(svc *BillingCacheService, ctx context.Context) <-chan subscriptionStatusResult {
	result := make(chan subscriptionStatusResult, 1)
	go func() {
		data, err := svc.GetSubscriptionStatus(ctx, 42, 84)
		result <- subscriptionStatusResult{data: data, err: err}
	}()
	return result
}

func awaitSubscriptionLoad(t *testing.T, loads <-chan subscriptionControlledLoad) subscriptionControlledLoad {
	t.Helper()
	select {
	case load := <-loads:
		return load
	case <-time.After(time.Second):
		t.Fatal("subscription DB load did not start")
		return subscriptionControlledLoad{}
	}
}

func awaitSubscriptionWait(t *testing.T, waiting <-chan struct{}) {
	t.Helper()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("subscription caller did not begin waiting for the shared load")
	}
}

func awaitSubscriptionStatus(t *testing.T, results <-chan subscriptionStatusResult) subscriptionStatusResult {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-time.After(time.Second):
		t.Fatal("subscription status did not return")
		return subscriptionStatusResult{}
	}
}

func subscriptionWithUsage(usage float64) *UserSubscription {
	return &UserSubscription{
		Status:          SubscriptionStatusActive,
		ExpiresAt:       time.Now().Add(time.Hour),
		DailyUsageUSD:   usage,
		WeeklyUsageUSD:  usage * 2,
		MonthlyUsageUSD: usage * 3,
		UpdatedAt:       time.Unix(123, 0),
	}
}

func TestBillingCacheServiceGetSubscriptionStatus_CallerCancellationDoesNotCancelSharedLoad(t *testing.T) {
	cache := &subscriptionGenerationCacheStub{fillResult: true}
	subRepo := newSubscriptionControlledRepoStub()
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	firstCtx := newSubscriptionWaitContext(ctx)
	firstResult := startSubscriptionStatusLoad(svc, firstCtx)
	load := awaitSubscriptionLoad(t, subRepo.started)
	awaitSubscriptionWait(t, firstCtx.waiting)

	secondCtx := newSubscriptionWaitContext(context.Background())
	secondResult := startSubscriptionStatusLoad(svc, secondCtx)
	awaitSubscriptionWait(t, secondCtx.waiting)
	cancel()
	first := awaitSubscriptionStatus(t, firstResult)
	require.ErrorIs(t, first.err, context.Canceled)
	require.Nil(t, first.data)
	require.NoError(t, load.ctx.Err(), "one caller must not cancel the shared DB context")
	deadline, hasDeadline := load.ctx.Deadline()
	require.True(t, hasDeadline, "detached DB work must retain a bounded timeout")
	require.LessOrEqual(t, time.Until(deadline), balanceLoadTimeout)

	load.result <- subscriptionWithUsage(2.5)
	second := awaitSubscriptionStatus(t, secondResult)
	require.NoError(t, second.err)
	require.Equal(t, 2.5, second.data.DailyUsage)
	require.Equal(t, int64(1), subRepo.calls.Load())
	require.Equal(t, int64(1), cache.fillCalls.Load())
}

func TestBillingCacheServiceGetSubscriptionStatus_InvalidationSeparatesGenerationsAndRejectsOldFill(t *testing.T) {
	cache := &subscriptionGenerationCacheStub{fillResult: true}
	subRepo := newSubscriptionControlledRepoStub()
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	oldCtx := newSubscriptionWaitContext(context.Background())
	oldResult := startSubscriptionStatusLoad(svc, oldCtx)
	oldLoad := awaitSubscriptionLoad(t, subRepo.started)
	awaitSubscriptionWait(t, oldCtx.waiting)

	require.NoError(t, svc.InvalidateSubscription(context.Background(), 42, 84))
	require.Equal(t, int64(1), cache.generation.Load())
	newCtx := newSubscriptionWaitContext(context.Background())
	newResult := startSubscriptionStatusLoad(svc, newCtx)
	newLoad := awaitSubscriptionLoad(t, subRepo.started)
	awaitSubscriptionWait(t, newCtx.waiting)
	require.Equal(t, int64(2), subRepo.calls.Load(), "new-generation requests must not join a stale in-flight DB read")

	oldLoad.result <- subscriptionWithUsage(1)
	old := awaitSubscriptionStatus(t, oldResult)
	require.NoError(t, old.err)
	require.Equal(t, 1.0, old.data.DailyUsage)
	staleCache, err := cache.GetSubscriptionCache(context.Background(), 42, 84)
	require.Error(t, err, "the old snapshot must not fill the invalidated cache")
	require.Nil(t, staleCache)
	require.Zero(t, cache.fillExpected.Load())

	newLoad.result <- subscriptionWithUsage(7)
	fresh := awaitSubscriptionStatus(t, newResult)
	require.NoError(t, fresh.err)
	require.Equal(t, 7.0, fresh.data.DailyUsage)
	stored, err := cache.GetSubscriptionCache(context.Background(), 42, 84)
	require.NoError(t, err)
	require.Equal(t, 7.0, stored.DailyUsage)
	require.Equal(t, int64(1), cache.fillExpected.Load())
	require.Equal(t, int64(2), cache.fillCalls.Load())
}

func TestBillingCacheServiceGetSubscriptionStatus_GenerationReadErrorSkipsFill(t *testing.T) {
	cache := &subscriptionGenerationCacheStub{
		fillResult:    true,
		generationErr: errors.New("generation unavailable"),
	}
	subRepo := &subscriptionLoadRepoStub{result: subscriptionWithUsage(4)}
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	data, err := svc.GetSubscriptionStatus(context.Background(), 42, 84)
	require.NoError(t, err)
	require.Equal(t, 4.0, data.DailyUsage)
	require.Equal(t, int64(1), subRepo.calls.Load())
	require.Zero(t, cache.fillCalls.Load(), "an unknown generation cannot authorize a cache fill")
	require.Zero(t, cache.setSubscriptionCalls.Load(), "the failure must not fall back to an unconditional fill")
}

func TestBillingCacheServiceGetSubscriptionStatus_LateMissRechecksCacheBeforeDB(t *testing.T) {
	var generationReads atomic.Int64
	secondRead := make(chan struct{})
	releaseSecondRead := make(chan struct{})
	cache := &subscriptionGenerationCacheStub{
		fillResult: true,
		generationHook: func(ctx context.Context) error {
			if generationReads.Add(1) != 2 {
				return nil
			}
			close(secondRead)
			select {
			case <-releaseSecondRead:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	subRepo := newSubscriptionControlledRepoStub()
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	firstResult := startSubscriptionStatusLoad(svc, context.Background())
	firstLoad := awaitSubscriptionLoad(t, subRepo.started)
	secondCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	secondResult := startSubscriptionStatusLoad(svc, secondCtx)
	awaitSubscriptionWait(t, secondRead)

	firstLoad.result <- subscriptionWithUsage(6)
	first := awaitSubscriptionStatus(t, firstResult)
	require.NoError(t, first.err)
	require.Equal(t, 6.0, first.data.DailyUsage)
	// The second caller already missed the cache, but has not joined the
	// singleflight group when the first call finishes and fills the cache.
	close(releaseSecondRead)
	second := awaitSubscriptionStatus(t, secondResult)
	require.NoError(t, second.err)
	require.Equal(t, 6.0, second.data.DailyUsage)
	require.Equal(t, int64(1), subRepo.calls.Load())
	require.Equal(t, int64(1), cache.fillCalls.Load())
}

func TestBillingCacheServiceGetSubscriptionStatus_LegacyCacheCompatibility(t *testing.T) {
	cache := &billingCacheMissStub{}
	subRepo := &subscriptionLoadRepoStub{result: subscriptionWithUsage(3)}
	svc := NewBillingCacheService(cache, nil, subRepo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	data, err := svc.GetSubscriptionStatus(context.Background(), 42, 84)
	require.NoError(t, err)
	require.Equal(t, 3.0, data.DailyUsage)
	svc.QueueUpdateSubscriptionUsage(42, 84, 1.5)
	require.NoError(t, svc.UpdateSubscriptionUsage(context.Background(), 42, 84, 1.5))
	require.NoError(t, svc.InvalidateSubscription(context.Background(), 42, 84))
	svc.Stop() // Drain the compatibility write queue before inspecting it.

	require.Equal(t, int64(1), cache.setSubscriptionCalls.Load())
	require.Equal(t, int64(2), cache.updateSubscriptionCalls.Load())
	require.Equal(t, int64(1), cache.invalidateSubscriptionCalls.Load())
}

func TestBillingCacheServiceUpdateSubscriptionUsageInvalidatesGeneration(t *testing.T) {
	cache := &subscriptionGenerationCacheStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	require.NoError(t, svc.UpdateSubscriptionUsage(context.Background(), 42, 84, 1.5))
	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(1), cache.generation.Load())
	require.Zero(t, cache.updateSubscriptionCalls.Load(), "persisted usage must not also be added as a cache delta")
}
