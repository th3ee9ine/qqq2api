//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type userRPMCacheStub struct {
	userGroupCalls int32
	userCalls      int32

	userGroupCounts []int
	userGroupErr    error
}

func (s *userRPMCacheStub) IncrementUserGroupRPM(_ context.Context, _, _ int64) (int, error) {
	idx := int(atomic.AddInt32(&s.userGroupCalls, 1)) - 1
	if s.userGroupErr != nil {
		return 0, s.userGroupErr
	}
	if idx < len(s.userGroupCounts) {
		return s.userGroupCounts[idx], nil
	}
	return 1, nil
}

func (s *userRPMCacheStub) IncrementUserRPM(_ context.Context, _ int64) (int, error) {
	atomic.AddInt32(&s.userCalls, 1)
	return 1, nil
}

func (s *userRPMCacheStub) GetUserGroupRPM(_ context.Context, _, _ int64) (int, error) {
	return 0, nil
}

func (s *userRPMCacheStub) GetUserRPM(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

type rpmOverrideRepoStub struct {
	UserGroupRateRepository
	calls int32
}

func (s *rpmOverrideRepoStub) GetRPMOverrideByUserAndGroup(_ context.Context, _, _ int64) (*int, error) {
	atomic.AddInt32(&s.calls, 1)
	zero := 0
	return &zero, nil
}

func newBillingServiceForRPM(t *testing.T, cache UserRPMCache, rateRepo UserGroupRateRepository) *BillingCacheService {
	t.Helper()
	svc := NewBillingCacheService(nil, nil, nil, nil, cache, rateRepo, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	return svc
}

func TestBillingCacheService_CheckRPM_EnforcesOnlySystemGroupLimit(t *testing.T) {
	cache := &userRPMCacheStub{userGroupCounts: []int{1, 2, 3}}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	// The legacy user ceiling and user×group override must not affect a global key.
	user := &User{ID: 1, RPMLimit: 1}
	group := &Group{ID: 10, RPMLimit: 2}

	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.NoError(t, svc.checkRPM(context.Background(), user, group))
	require.ErrorIs(t, svc.checkRPM(context.Background(), user, group), ErrGroupRPMExceeded)
	require.EqualValues(t, 3, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
	require.Zero(t, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_CheckRPM_LegacyOverrideCannotDisableGroupLimit(t *testing.T) {
	cache := &userRPMCacheStub{userGroupCounts: []int{6}}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	err := svc.checkRPM(context.Background(), &User{ID: 1}, &Group{ID: 10, RPMLimit: 5})
	require.ErrorIs(t, err, ErrGroupRPMExceeded)
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&repo.calls), "legacy user×group overrides must not be queried")
}

func TestBillingCacheService_CheckRPM_IgnoresLegacyUserLimitWhenGroupUnlimited(t *testing.T) {
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	user := &User{ID: 1, RPMLimit: 1}
	group := &Group{ID: 10, RPMLimit: 0}
	for i := 0; i < 10; i++ {
		require.NoError(t, svc.checkRPM(context.Background(), user, group))
	}
	require.Zero(t, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
	require.Zero(t, atomic.LoadInt32(&repo.calls))
}

func TestBillingCacheService_CheckRPM_RedisErrorFailsOpen(t *testing.T) {
	cache := &userRPMCacheStub{userGroupErr: errors.New("redis unavailable")}
	svc := newBillingServiceForRPM(t, cache, &rpmOverrideRepoStub{})

	require.NoError(t, svc.checkRPM(context.Background(), &User{ID: 1}, &Group{ID: 10, RPMLimit: 5}))
	require.EqualValues(t, 1, atomic.LoadInt32(&cache.userGroupCalls))
}

func TestBillingCacheService_CheckRPM_NoGroupIsUnlimited(t *testing.T) {
	cache := &userRPMCacheStub{}
	svc := newBillingServiceForRPM(t, cache, &rpmOverrideRepoStub{})

	require.NoError(t, svc.checkRPM(context.Background(), &User{ID: 1, RPMLimit: 1}, nil))
	require.Zero(t, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
}

func TestBillingCacheService_CheckRPM_NilTechnicalSubjectIsNoop(t *testing.T) {
	cache := &userRPMCacheStub{}
	repo := &rpmOverrideRepoStub{}
	svc := newBillingServiceForRPM(t, cache, repo)

	require.NoError(t, svc.checkRPM(context.Background(), nil, &Group{ID: 1, RPMLimit: 10}))
	require.Zero(t, atomic.LoadInt32(&cache.userGroupCalls))
	require.Zero(t, atomic.LoadInt32(&cache.userCalls))
	require.Zero(t, atomic.LoadInt32(&repo.calls))
}
