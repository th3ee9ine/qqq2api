//go:build unit

package service

import (
	"context"
	"sync"
)

type settingRepoStub struct {
	mu               sync.Mutex
	values           map[string]string
	err              error
	getValueCalls    int
	getMultipleCalls int
}

func (s *settingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *settingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getValueCalls++
	if s.err != nil {
		return "", s.err
	}
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", ErrSettingNotFound
}

func (s *settingRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getMultipleCalls++
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if v, ok := s.values[key]; ok {
			result[key] = v
		}
	}
	return result, nil
}

func (s *settingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

type turnstileVerifierSpy struct {
	called    int
	lastToken string
	result    *TurnstileVerifyResponse
	err       error
}

func (s *turnstileVerifierSpy) VerifyToken(_ context.Context, _ string, token, _ string) (*TurnstileVerifyResponse, error) {
	s.called++
	s.lastToken = token
	if s.err != nil {
		return nil, s.err
	}
	if s.result != nil {
		return s.result, nil
	}
	return &TurnstileVerifyResponse{Success: true}, nil
}

// rpmUserRepoStub 复用 admin_service_update_balance_test.go 的基础 stub 结构，
// 只在 Update 时把入参克隆一份，便于断言修改后的 RPMLimit。
type rpmUserRepoStub struct {
	*userRepoStub
	lastUpdated *User
}

func (s *rpmUserRepoStub) Update(_ context.Context, user *User, _ UserUpdateFields) error {
	if user == nil {
		return nil
	}
	clone := *user
	s.lastUpdated = &clone
	if s.userRepoStub != nil {
		s.userRepoStub.user = &clone
	}
	return nil
}

type authCacheInvalidatorStub struct {
	userIDs  []int64
	groupIDs []int64
	keys     []string
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByKey(ctx context.Context, key string) {
	s.keys = append(s.keys, key)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	s.userIDs = append(s.userIDs, userID)
}

func (s *authCacheInvalidatorStub) InvalidateAuthCacheByGroupID(ctx context.Context, groupID int64) {
	s.groupIDs = append(s.groupIDs, groupID)
}
