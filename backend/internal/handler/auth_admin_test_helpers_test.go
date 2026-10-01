//go:build unit

package handler

import (
	"context"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"time"
)

// Administrator test fixtures provide only authentication and session storage.
type userHandlerRepoStub struct {
	service.UserRepository
	user *service.User
}

func (s *userHandlerRepoStub) GetByID(context.Context, int64) (*service.User, error) {
	if s.user == nil {
		return nil, service.ErrUserNotFound
	}
	cloned := *s.user
	return &cloned, nil
}
func (s *userHandlerRepoStub) GetByEmail(ctx context.Context, _ string) (*service.User, error) {
	return s.GetByID(ctx, 0)
}
func (s *userHandlerRepoStub) GetFirstAdmin(ctx context.Context) (*service.User, error) {
	return s.GetByID(ctx, 0)
}
func (s *userHandlerRepoStub) Update(_ context.Context, user *service.User, _ service.UserUpdateFields) error {
	cloned := *user
	s.user = &cloned
	return nil
}
func (s *userHandlerRepoStub) UpdateUserLastActiveAt(_ context.Context, _ int64, at time.Time) error {
	s.user.LastActiveAt = &at
	return nil
}

type userHandlerRefreshTokenCacheStub struct {
	service.RefreshTokenCache
	revokedUserIDs []int64
}

func (s *userHandlerRefreshTokenCacheStub) DeleteUserRefreshTokens(_ context.Context, userID int64) error {
	s.revokedUserIDs = append(s.revokedUserIDs, userID)
	return nil
}
