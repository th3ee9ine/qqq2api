package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

// Inject ownership changes exactly at the Redis operation boundaries. These
// tests exercise the public legacy selectors with the production capabilities,
// without relying on goroutine timing to reproduce a competing request.
type legacyAtomicStickyTrace struct {
	mu      sync.Mutex
	events  []string
	held    map[string]int64
	invalid bool
}

func (r *legacyAtomicStickyTrace) record(event string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *legacyAtomicStickyTrace) snapshot() ([]string, int, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...), len(r.held), r.invalid
}

type legacyAtomicStickyCache struct {
	*atomicSchedulerGatewayCache
	trace        *legacyAtomicStickyTrace
	claimHook    func(context.Context, int64, string, int64, time.Duration)
	deleteHook   func(context.Context, int64, string, int64)
	rejectClaims bool
}

func newLegacyAtomicStickyCache(owner int64) *legacyAtomicStickyCache {
	bindings := make(map[string]int64)
	if owner > 0 {
		bindings["openai:atomic-legacy"] = owner
	}
	return &legacyAtomicStickyCache{
		atomicSchedulerGatewayCache: &atomicSchedulerGatewayCache{
			schedulerTestGatewayCache: schedulerTestGatewayCache{sessionBindings: bindings},
		},
		trace: &legacyAtomicStickyTrace{held: make(map[string]int64)},
	}
}

func (c *legacyAtomicStickyCache) GetSessionAccountID(ctx context.Context, groupID int64, hash string) (int64, error) {
	owner, err := c.atomicSchedulerGatewayCache.GetSessionAccountID(ctx, groupID, hash)
	c.trace.record(fmt.Sprintf("read:%d", owner))
	return owner, err
}

func (c *legacyAtomicStickyCache) SetSessionAccountID(ctx context.Context, groupID int64, hash string, id int64, ttl time.Duration) error {
	c.trace.record(fmt.Sprintf("set:%d", id))
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.schedulerTestGatewayCache.SetSessionAccountID(ctx, groupID, hash, id, ttl)
}

func (c *legacyAtomicStickyCache) DeleteSessionAccountID(ctx context.Context, groupID int64, hash string) error {
	c.trace.record("delete")
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.schedulerTestGatewayCache.DeleteSessionAccountID(ctx, groupID, hash)
}

func (c *legacyAtomicStickyCache) ClaimSessionAccountID(ctx context.Context, groupID int64, hash string, id int64, ttl time.Duration) (bool, error) {
	c.trace.record(fmt.Sprintf("claim:%d", id))
	if c.claimHook != nil {
		c.claimHook(ctx, groupID, hash, id, ttl)
	}
	if c.rejectClaims {
		return false, nil
	}
	return c.atomicSchedulerGatewayCache.ClaimSessionAccountID(ctx, groupID, hash, id, ttl)
}

func (c *legacyAtomicStickyCache) CompareAndSetSessionAccountID(ctx context.Context, groupID int64, hash string, expected, id int64, ttl time.Duration) (bool, error) {
	c.trace.record(fmt.Sprintf("cas:%d:%d", expected, id))
	return c.atomicSchedulerGatewayCache.CompareAndSetSessionAccountID(ctx, groupID, hash, expected, id, ttl)
}

func (c *legacyAtomicStickyCache) CompareAndDeleteSessionAccountID(ctx context.Context, groupID int64, hash string, expected int64) (bool, error) {
	c.trace.record(fmt.Sprintf("delete-if:%d", expected))
	if c.deleteHook != nil {
		c.deleteHook(ctx, groupID, hash, expected)
	}
	return c.atomicSchedulerGatewayCache.CompareAndDeleteSessionAccountID(ctx, groupID, hash, expected)
}

type legacyAtomicSlotCache struct {
	schedulerTestConcurrencyCache
	trace *legacyAtomicStickyTrace
}

func (c *legacyAtomicSlotCache) AcquireAccountSlot(ctx context.Context, id int64, max int, requestID string) (bool, error) {
	acquired, err := c.schedulerTestConcurrencyCache.AcquireAccountSlot(ctx, id, max, requestID)
	c.trace.mu.Lock()
	defer c.trace.mu.Unlock()
	c.trace.events = append(c.trace.events, fmt.Sprintf("acquire:%d", id))
	if acquired {
		c.trace.held[requestID] = id
	}
	return acquired, err
}

func (c *legacyAtomicSlotCache) ReleaseAccountSlot(_ context.Context, id int64, requestID string) error {
	c.trace.mu.Lock()
	defer c.trace.mu.Unlock()
	c.trace.events = append(c.trace.events, fmt.Sprintf("release:%d", id))
	if c.trace.held[requestID] != id {
		c.trace.invalid = true
	}
	delete(c.trace.held, requestID)
	return nil
}

func newLegacyAtomicSchedulerService(cache *legacyAtomicStickyCache, accounts []Account, slots *legacyAtomicSlotCache) *OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	cfg.Gateway.Scheduling.StickySessionMaxWaiting = 1
	svc := &OpenAIGatewayService{
		accountRepo: stubOpenAIAccountRepo{accounts: accounts},
		cache:       cache,
		cfg:         cfg,
	}
	if slots != nil {
		svc.concurrencyService = NewConcurrencyService(slots)
	}
	return svc
}

func legacyAtomicSchedulerAccounts() []Account {
	return []Account{
		{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1},
		{ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 2},
	}
}

func requireLegacyAtomicEventOrder(t *testing.T, events []string, expected ...string) {
	t.Helper()
	next := 0
	for _, event := range events {
		if next < len(expected) && event == expected[next] {
			next++
		}
	}
	require.Equal(t, len(expected), next, "missing ordered events %v in %v", expected, events)
}

func TestOpenAILegacyAtomicStickyClaimConflictReleasesSlotBeforeRetry(t *testing.T) {
	for _, failLoadBatch := range []bool{false, true} {
		t.Run(fmt.Sprintf("load_batch_error=%t", failLoadBatch), func(t *testing.T) {
			cache := newLegacyAtomicStickyCache(0)
			var firstClaim sync.Once
			cache.claimHook = func(ctx context.Context, groupID int64, hash string, _ int64, ttl time.Duration) {
				firstClaim.Do(func() {
					claimed, err := cache.atomicSchedulerGatewayCache.ClaimSessionAccountID(ctx, groupID, hash, 2, ttl)
					require.NoError(t, err)
					require.True(t, claimed)
				})
			}
			slots := &legacyAtomicSlotCache{trace: cache.trace}
			if failLoadBatch {
				slots.loadBatchErr = errors.New("load batch unavailable")
			}
			svc := newLegacyAtomicSchedulerService(cache, legacyAtomicSchedulerAccounts(), slots)
			selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "atomic-legacy", "gpt-4", nil)
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.Equal(t, int64(2), selection.Account.ID)
			require.True(t, selection.Acquired)
			t.Cleanup(selection.ReleaseFunc)
			events, held, invalid := cache.trace.snapshot()
			requireLegacyAtomicEventOrder(t, events, "acquire:1", "claim:1", "release:1", "read:2", "acquire:2")
			require.Equal(t, 1, held, "only the winning selection may retain a slot")
			require.False(t, invalid)
			selection.ReleaseFunc()
			_, held, invalid = cache.trace.snapshot()
			require.Zero(t, held)
			require.False(t, invalid)
		})
	}
}

func TestOpenAILegacyAtomicStickyBasicClaimConflictRereadsOwner(t *testing.T) {
	cache := newLegacyAtomicStickyCache(0)
	var firstClaim sync.Once
	cache.claimHook = func(ctx context.Context, groupID int64, hash string, _ int64, ttl time.Duration) {
		firstClaim.Do(func() {
			claimed, err := cache.atomicSchedulerGatewayCache.ClaimSessionAccountID(ctx, groupID, hash, 2, ttl)
			require.NoError(t, err)
			require.True(t, claimed)
		})
	}
	svc := newLegacyAtomicSchedulerService(cache, legacyAtomicSchedulerAccounts(), nil)
	account, err := svc.SelectAccountForModelWithExclusions(context.Background(), nil, "atomic-legacy", "gpt-4", nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), account.ID)
	events, _, _ := cache.trace.snapshot()
	requireLegacyAtomicEventOrder(t, events, "claim:1", "read:2")
}

func TestOpenAILegacyAtomicStickyCleanupPreservesConcurrentOwner(t *testing.T) {
	for _, withLoad := range []bool{false, true} {
		t.Run(fmt.Sprintf("with_load=%t", withLoad), func(t *testing.T) {
			cache := newLegacyAtomicStickyCache(1)
			cache.deleteHook = func(ctx context.Context, groupID int64, hash string, expected int64) {
				require.Equal(t, int64(1), expected)
				replaced, err := cache.atomicSchedulerGatewayCache.CompareAndSetSessionAccountID(ctx, groupID, hash, 1, 2, time.Hour)
				require.NoError(t, err)
				require.True(t, replaced)
			}
			accounts := legacyAtomicSchedulerAccounts()
			accounts[0].Status = StatusDisabled
			accounts = append(accounts, Account{ID: 3, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1})
			slots := &legacyAtomicSlotCache{trace: cache.trace}
			svc := newLegacyAtomicSchedulerService(cache, accounts, slots)
			var selectedID int64
			if withLoad {
				selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "atomic-legacy", "gpt-4", nil)
				require.NoError(t, err)
				require.NotNil(t, selection)
				t.Cleanup(selection.ReleaseFunc)
				selectedID = selection.Account.ID
				selection.ReleaseFunc()
			} else {
				account, err := svc.SelectAccountForModelWithExclusions(context.Background(), nil, "atomic-legacy", "gpt-4", nil)
				require.NoError(t, err)
				selectedID = account.ID
			}
			owner, err := cache.atomicSchedulerGatewayCache.GetSessionAccountID(context.Background(), 0, "openai:atomic-legacy")
			require.NoError(t, err)
			require.Equal(t, int64(2), owner)
			require.Equal(t, owner, selectedID)
			events, held, invalid := cache.trace.snapshot()
			require.Contains(t, events, "delete-if:1")
			require.NotContains(t, events, "delete", "cleanup must compare the observed owner")
			require.Zero(t, held)
			require.False(t, invalid)
		})
	}
}

func TestOpenAILegacyAtomicStickyExcludedOwnerMigratesConditionally(t *testing.T) {
	for _, withLoad := range []bool{false, true} {
		t.Run(fmt.Sprintf("with_load=%t", withLoad), func(t *testing.T) {
			cache := newLegacyAtomicStickyCache(1)
			svc := newLegacyAtomicSchedulerService(cache, legacyAtomicSchedulerAccounts(), &legacyAtomicSlotCache{trace: cache.trace})
			excluded := map[int64]struct{}{1: {}}
			if withLoad {
				selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "atomic-legacy", "gpt-4", excluded)
				require.NoError(t, err)
				require.Equal(t, int64(2), selection.Account.ID)
				t.Cleanup(selection.ReleaseFunc)
				selection.ReleaseFunc()
			} else {
				account, err := svc.SelectAccountForModelWithExclusions(context.Background(), nil, "atomic-legacy", "gpt-4", excluded)
				require.NoError(t, err)
				require.Equal(t, int64(2), account.ID)
			}
			owner, err := cache.atomicSchedulerGatewayCache.GetSessionAccountID(context.Background(), 0, "openai:atomic-legacy")
			require.NoError(t, err)
			require.Equal(t, int64(2), owner)
			events, held, invalid := cache.trace.snapshot()
			require.Contains(t, events, "cas:1:2")
			require.NotContains(t, events, "set:2")
			require.Zero(t, held)
			require.False(t, invalid)
		})
	}
}

func TestOpenAILegacyAtomicStickyCapacitySpilloverPreservesOwner(t *testing.T) {
	cache := newLegacyAtomicStickyCache(1)
	slots := &legacyAtomicSlotCache{
		trace: cache.trace,
		schedulerTestConcurrencyCache: schedulerTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			waitCounts:     map[int64]int{1: 1},
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, LoadRate: 100},
				2: {AccountID: 2, LoadRate: 10},
			},
		},
	}
	svc := newLegacyAtomicSchedulerService(cache, legacyAtomicSchedulerAccounts(), slots)
	selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "atomic-legacy", "gpt-4", nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), selection.Account.ID)
	require.True(t, selection.Acquired)
	t.Cleanup(selection.ReleaseFunc)
	owner, err := cache.atomicSchedulerGatewayCache.GetSessionAccountID(context.Background(), 0, "openai:atomic-legacy")
	require.NoError(t, err)
	require.Equal(t, int64(1), owner)
	selection.ReleaseFunc()
	events, held, invalid := cache.trace.snapshot()
	requireLegacyAtomicEventOrder(t, events, "acquire:1", "acquire:2", "release:2")
	require.NotContains(t, events, "claim:2")
	require.NotContains(t, events, "cas:1:2")
	require.Zero(t, held)
	require.False(t, invalid)
}

func TestOpenAILegacyAtomicStickyConflictRetriesAreBoundedWithoutSlotLeaks(t *testing.T) {
	for _, withLoad := range []bool{false, true} {
		t.Run(fmt.Sprintf("with_load=%t", withLoad), func(t *testing.T) {
			cache := newLegacyAtomicStickyCache(0)
			cache.rejectClaims = true
			svc := newLegacyAtomicSchedulerService(cache, legacyAtomicSchedulerAccounts(), &legacyAtomicSlotCache{trace: cache.trace})
			if withLoad {
				selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "atomic-legacy", "gpt-4", nil)
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, selection)
			} else {
				account, err := svc.SelectAccountForModelWithExclusions(context.Background(), nil, "atomic-legacy", "gpt-4", nil)
				require.ErrorIs(t, err, ErrNoAvailableAccounts)
				require.Nil(t, account)
			}
			events, held, invalid := cache.trace.snapshot()
			claims, acquired, released := 0, 0, 0
			for _, event := range events {
				switch event {
				case "claim:1":
					claims++
				case "acquire:1":
					acquired++
				case "release:1":
					released++
				}
			}
			require.Equal(t, 3, claims, "persistent contention must terminate after bounded retries: %v", events)
			require.Equal(t, acquired, released)
			if withLoad {
				require.Equal(t, claims, acquired)
			}
			require.Zero(t, held)
			require.False(t, invalid)
		})
	}
}
