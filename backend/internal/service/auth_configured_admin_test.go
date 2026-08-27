//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
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

func TestAuthServiceCanAccessAdminPanel(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Default.AdminEmail = "super-admin@example.com"
	svc := NewAuthService(nil, &configuredAdminRepo{}, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)

	for _, tt := range []struct {
		name string
		user *User
		want bool
	}{
		{name: "nil user", user: nil, want: false},
		{name: "configured super administrator", user: &User{ID: 1, Email: "SUPER-ADMIN@example.com", Role: RoleAdmin}, want: true},
		{name: "account administrator", user: &User{ID: 2, Email: "operator@example.com", Role: RoleAccountAdmin}, want: true},
		{name: "unconfigured administrator", user: &User{ID: 3, Email: "other-admin@example.com", Role: RoleAdmin}, want: false},
		{name: "ordinary user with configured email", user: &User{ID: 4, Email: "super-admin@example.com", Role: RoleUser}, want: false},
		{name: "ordinary user", user: &User{ID: 5, Email: "user@example.com", Role: RoleUser}, want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, svc.CanAccessAdminPanel(context.Background(), tt.user))
		})
	}
}
