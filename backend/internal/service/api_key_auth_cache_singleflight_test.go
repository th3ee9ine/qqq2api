//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type coalescingAuthCacheStub struct {
	APIKeyCache
	get     func(context.Context, string) (*APIKeyAuthCacheEntry, error)
	writes  atomic.Int32
	deletes atomic.Int32
	pubs    atomic.Int32
}

func (s *coalescingAuthCacheStub) GetAuthCache(ctx context.Context, key string) (*APIKeyAuthCacheEntry, error) {
	return s.get(ctx, key)
}

func (s *coalescingAuthCacheStub) SetAuthCache(context.Context, string, *APIKeyAuthCacheEntry, time.Duration) error {
	s.writes.Add(1)
	return nil
}

func (s *coalescingAuthCacheStub) DeleteAuthCache(context.Context, string) error {
	s.deletes.Add(1)
	return nil
}

func (s *coalescingAuthCacheStub) PublishAuthCacheInvalidation(context.Context, string) error {
	s.pubs.Add(1)
	return nil
}

// Signal when the caller starts waiting for I/O. This gives the concurrency
// tests a barrier without sleeps or assumptions about goroutine scheduling.
type authCacheWaitingContext struct {
	context.Context
	waiting chan<- struct{}
	once    sync.Once
}

func (c *authCacheWaitingContext) Done() <-chan struct{} {
	c.once.Do(func() { c.waiting <- struct{}{} })
	return c.Context.Done()
}

func waitAuthCacheCallers(t *testing.T, waiting <-chan struct{}, count int) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for i := 0; i < count; i++ {
		select {
		case <-waiting:
		case <-timer.C:
			t.Fatalf("only %d of %d callers began waiting", i, count)
		}
	}
}

func coalescingAuthEntry(status string) *APIKeyAuthCacheEntry {
	return &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{
		Version: apiKeyAuthSnapshotVersion, APIKeyID: 11, UserID: 2, Status: status,
		User: APIKeyAuthUserSnapshot{ID: 2, Status: StatusActive, Role: RoleUser},
	}}
}

func TestAPIKeyServiceAuthCacheSingleflightCoalescesL2AndDB(t *testing.T) {
	const callers = 64
	for _, singleflightEnabled := range []bool{false, true} {
		for _, outcome := range []string{"l2_hit", "l2_miss", "l2_error", "legacy_snapshot", "not_found"} {
			t.Run(fmt.Sprintf("singleflight=%t/%s", singleflightEnabled, outcome), func(t *testing.T) {
				var redisReads, dbReads atomic.Int32
				waiting := make(chan struct{}, callers)
				release := make(chan struct{})
				var releaseOnce sync.Once
				unblock := func() { releaseOnce.Do(func() { close(release) }) }
				defer unblock()
				cache := &coalescingAuthCacheStub{get: func(ctx context.Context, _ string) (*APIKeyAuthCacheEntry, error) {
					redisReads.Add(1)
					select {
					case <-release:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					switch outcome {
					case "l2_hit":
						return coalescingAuthEntry(StatusActive), nil
					case "legacy_snapshot":
						entry := coalescingAuthEntry(StatusActive)
						entry.Snapshot.Version--
						return entry, nil
					case "l2_error":
						return nil, errors.New("redis unavailable")
					default:
						return nil, redis.Nil
					}
				}}
				repo := &authRepoStub{getByKeyForAuth: func(context.Context, string) (*APIKey, error) {
					dbReads.Add(1)
					if outcome == "not_found" {
						return nil, ErrAPIKeyNotFound
					}
					return &APIKey{ID: 11, UserID: 2, Status: StatusActive, User: &User{ID: 2, Status: StatusActive}}, nil
				}}
				svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{
					APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60, Singleflight: singleflightEnabled},
				})
				errs := make(chan error, callers)
				for i := 0; i < callers; i++ {
					go func() {
						ctx := &authCacheWaitingContext{Context: context.Background(), waiting: waiting}
						key, err := svc.GetByKey(ctx, "shared-key")
						if err == nil && (key == nil || key.ID != 11) {
							err = fmt.Errorf("unexpected key: %v", key)
						}
						errs <- err
					}()
				}
				waitAuthCacheCallers(t, waiting, callers)
				unblock()
				for i := 0; i < callers; i++ {
					if outcome == "not_found" {
						require.ErrorIs(t, <-errs, ErrAPIKeyNotFound)
					} else {
						require.NoError(t, <-errs)
					}
				}
				wantReads := int32(callers)
				if singleflightEnabled {
					wantReads = 1
				}
				require.Equal(t, wantReads, redisReads.Load(), "L2 reads per concurrent burst")
				if outcome == "l2_hit" {
					require.Zero(t, dbReads.Load())
				} else {
					require.Equal(t, wantReads, dbReads.Load(), "DB reads per concurrent burst")
				}
				if outcome == "not_found" {
					require.Zero(t, cache.writes.Load(), "unknown credentials must not amplify Redis writes")
				}
				t.Logf("callers=%d redis_reads=%d db_reads=%d", callers, redisReads.Load(), dbReads.Load())
			})
		}
	}
}

func TestAPIKeyServiceAuthCacheSingleflightCallerCancellation(t *testing.T) {
	for _, canceledCaller := range []string{"leader", "follower"} {
		t.Run(canceledCaller, func(t *testing.T) {
			type traceKey struct{}
			waiting := make(chan struct{}, 2)
			entered := make(chan context.Context, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			var redisReads atomic.Int32
			cache := &coalescingAuthCacheStub{get: func(ctx context.Context, _ string) (*APIKeyAuthCacheEntry, error) {
				redisReads.Add(1)
				entered <- ctx
				select {
				case <-release:
					return coalescingAuthEntry(StatusActive), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}}
			svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, &config.Config{
				APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60, Singleflight: true},
			})
			leaderCtx, cancelLeader := context.WithCancel(context.WithValue(context.Background(), traceKey{}, "trace"))
			defer cancelLeader()
			followerCtx, cancelFollower := context.WithCancel(context.Background())
			defer cancelFollower()
			leaderResult, followerResult := make(chan error, 1), make(chan error, 1)
			call := func(ctx context.Context, result chan<- error) {
				_, err := svc.GetByKey(&authCacheWaitingContext{Context: ctx, waiting: waiting}, "shared-key")
				result <- err
			}
			go call(leaderCtx, leaderResult)
			loadCtx := <-entered
			go call(followerCtx, followerResult)
			waitAuthCacheCallers(t, waiting, 2)
			canceledResult, healthyResult := leaderResult, followerResult
			if canceledCaller == "leader" {
				cancelLeader()
			} else {
				cancelFollower()
				canceledResult, healthyResult = followerResult, leaderResult
			}
			select {
			case err := <-canceledResult:
				require.ErrorIs(t, err, context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("canceled caller remained blocked on the shared load")
			}
			require.NoError(t, loadCtx.Err(), "one canceled caller must not cancel the shared load")
			require.Equal(t, "trace", loadCtx.Value(traceKey{}), "shared load must retain context values")
			deadline, ok := loadCtx.Deadline()
			require.True(t, ok, "shared I/O must have a bounded lifetime")
			require.Positive(t, time.Until(deadline))
			require.LessOrEqual(t, time.Until(deadline), apiKeyAuthCacheReadTimeout)
			unblock()
			require.NoError(t, <-healthyResult)
			require.Equal(t, int32(1), redisReads.Load())
		})
	}
}

func TestAPIKeyServiceAuthCacheSingleflightKeepsLocalCacheAndInvalidation(t *testing.T) {
	var redisReads atomic.Int32
	status := StatusActive
	cache := &coalescingAuthCacheStub{get: func(context.Context, string) (*APIKeyAuthCacheEntry, error) {
		redisReads.Add(1)
		return coalescingAuthEntry(status), nil
	}}
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L1Size: 100, L1TTLSeconds: 60, L2TTLSeconds: 60, Singleflight: true},
	})
	t.Cleanup(svc.authCacheL1.Close)
	_, err := svc.GetByKey(context.Background(), "shared-key")
	require.NoError(t, err)
	svc.authCacheL1.Wait()
	_, err = svc.GetByKey(context.Background(), "shared-key")
	require.NoError(t, err)
	require.Equal(t, int32(1), redisReads.Load(), "warm L1 must avoid Redis and singleflight")

	status = StatusDisabled
	svc.InvalidateAuthCacheByKey(context.Background(), "shared-key")
	key, err := svc.GetByKey(context.Background(), "shared-key")
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, key.Status)
	require.Equal(t, int32(2), redisReads.Load())
	require.Equal(t, int32(1), cache.deletes.Load())
	require.Equal(t, int32(1), cache.pubs.Load(), "invalidation must still propagate to other replicas")
}

func TestAPIKeyServiceAuthCacheSingleflightSkipsCanceledMiss(t *testing.T) {
	cache := &coalescingAuthCacheStub{get: func(context.Context, string) (*APIKeyAuthCacheEntry, error) {
		t.Error("already canceled caller must not start shared I/O")
		return nil, redis.Nil
	}}
	svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60, Singleflight: true},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.GetByKey(ctx, "shared-key")
	require.ErrorIs(t, err, context.Canceled)
}

func TestAPIKeyServiceAuthCacheSingleflightInvalidationSeparatesInflightReads(t *testing.T) {
	for _, invalidation := range []string{"local_write", "pubsub"} {
		t.Run(invalidation, func(t *testing.T) {
			var redisReads atomic.Int32
			entered := make(chan struct{})
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			cache := &coalescingAuthCacheStub{get: func(ctx context.Context, _ string) (*APIKeyAuthCacheEntry, error) {
				if redisReads.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
						return coalescingAuthEntry(StatusActive), nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				return coalescingAuthEntry(StatusDisabled), nil
			}}
			svc := NewAPIKeyService(nil, nil, nil, nil, nil, cache, &config.Config{
				APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60, Singleflight: true},
			})
			firstResult := make(chan error, 1)
			go func() {
				_, err := svc.GetByKey(context.Background(), "shared-key")
				firstResult <- err
			}()
			<-entered
			if invalidation == "local_write" {
				svc.InvalidateAuthCacheByKey(context.Background(), "shared-key")
			} else {
				// This is the handler used by the existing Pub/Sub subscriber.
				svc.invalidateLocalAuthCache(svc.authCacheKey("shared-key"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			key, err := svc.GetByKey(ctx, "shared-key")
			require.NoError(t, err, "new request must not wait for the pre-invalidation read")
			require.Equal(t, StatusDisabled, key.Status)
			require.Equal(t, int32(2), redisReads.Load())
			unblock()
			require.NoError(t, <-firstResult)
		})
	}
}

func TestAPIKeyServiceAuthCacheSingleflightSlowRedisPreservesDBBudget(t *testing.T) {
	type traceKey struct{}
	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var redisReads, dbReads atomic.Int32
	cache := &coalescingAuthCacheStub{get: func(ctx context.Context, _ string) (*APIKeyAuthCacheEntry, error) {
		redisReads.Add(1)
		entered <- ctx
		// Model go-redis with ContextTimeoutEnabled=false: the network read
		// can remain blocked after the stage context has already expired.
		<-release
		return nil, ctx.Err()
	}}
	type dbObservation struct {
		err       error
		remaining time.Duration
		trace     any
	}
	observedDB := make(chan dbObservation, 1)
	repo := &authRepoStub{getByKeyForAuth: func(ctx context.Context, _ string) (*APIKey, error) {
		dbReads.Add(1)
		deadline, _ := ctx.Deadline()
		observedDB <- dbObservation{err: ctx.Err(), remaining: time.Until(deadline), trace: ctx.Value(traceKey{})}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &APIKey{ID: 11, UserID: 2, Status: StatusActive, User: &User{ID: 2, Status: StatusActive}}, nil
	}}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, cache, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{L2TTLSeconds: 60, Singleflight: true},
	})
	svc.authCfg.cacheReadTimeout = 10 * time.Millisecond
	svc.authCfg.dbLoadTimeout = 200 * time.Millisecond
	type lookupResult struct {
		key *APIKey
		err error
	}
	result := make(chan lookupResult, 1)
	go func() {
		key, err := svc.GetByKey(context.WithValue(context.Background(), traceKey{}, "trace"), "shared-key")
		result <- lookupResult{key, err}
	}()
	cacheCtx := <-entered
	select {
	case <-cacheCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("L2 stage context did not expire")
	}
	require.ErrorIs(t, cacheCtx.Err(), context.DeadlineExceeded)
	require.Zero(t, dbReads.Load(), "wait for the bounded Redis transport to return without spawning another I/O goroutine")
	unblock()
	select {
	case got := <-result:
		require.NoError(t, got.err, "a slow cache must not consume the healthy DB fallback's budget")
		require.Equal(t, int64(11), got.key.ID)
	case <-time.After(time.Second):
		t.Fatal("DB fallback did not complete")
	}
	observation := <-observedDB
	require.NoError(t, observation.err)
	require.Positive(t, observation.remaining)
	require.LessOrEqual(t, observation.remaining, svc.authCfg.dbLoadTimeout)
	require.Equal(t, "trace", observation.trace)
	require.Equal(t, int32(1), redisReads.Load())
	require.Equal(t, int32(1), dbReads.Load())
}

func TestAPIKeyServiceAuthCacheSingleflightDBFallbackHasOwnDeadline(t *testing.T) {
	repo := &authRepoStub{getByKeyForAuth: func(ctx context.Context, _ string) (*APIKey, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	svc := NewAPIKeyService(repo, nil, nil, nil, nil, nil, &config.Config{
		APIKeyAuth: config.APIKeyAuthCacheConfig{Singleflight: true},
	})
	svc.authCfg.dbLoadTimeout = 10 * time.Millisecond
	// A stalled DB must stop at its stage deadline even when callers allow longer.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := svc.GetByKey(ctx, "shared-key")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NoError(t, ctx.Err(), "the DB stage, not the caller, must impose this deadline")
}
