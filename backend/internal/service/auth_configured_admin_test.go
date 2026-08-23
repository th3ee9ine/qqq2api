//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type configuredAdminRepo struct {
	UserRepository
	first *User
}

func (r *configuredAdminRepo) GetFirstAdmin(context.Context) (*User, error) {
	return r.first, nil
}

func TestAuthServiceConfiguredAdminUsesConfiguredEmailStrictly(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.AdminEmail = "env-admin@example.com"
	legacyAdmin := &User{ID: 1, Email: "legacy-admin@example.com", Role: RoleAdmin}
	svc := NewAuthService(nil, &configuredAdminRepo{first: legacyAdmin}, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)

	require.True(t, svc.IsConfiguredAdmin(context.Background(), &User{
		ID: 2, Email: "ENV-ADMIN@example.com", Role: RoleAdmin,
	}))
	require.False(t, svc.IsConfiguredAdmin(context.Background(), legacyAdmin))
	require.False(t, svc.IsConfiguredAdmin(context.Background(), &User{
		ID: 2, Email: "env-admin@example.com", Role: RoleUser,
	}))
}

func TestAuthServiceConfiguredAdminLegacyFallbackUsesFirstAdmin(t *testing.T) {
	legacyAdmin := &User{ID: 1, Email: "legacy-admin@example.com", Role: RoleAdmin}
	svc := NewAuthService(nil, &configuredAdminRepo{first: legacyAdmin}, nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil)

	require.True(t, svc.IsConfiguredAdmin(context.Background(), &User{
		ID: legacyAdmin.ID, Email: legacyAdmin.Email, Role: RoleAdmin,
	}))
	require.False(t, svc.IsConfiguredAdmin(context.Background(), &User{
		ID: 2, Email: "other-admin@example.com", Role: RoleAdmin,
	}))
}
