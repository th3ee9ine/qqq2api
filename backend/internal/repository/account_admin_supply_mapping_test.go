package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	dbent "github.com/th3ee9ine/qqq2api/ent"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestAccountAdminSupplyFieldsMapToServiceModels(t *testing.T) {
	accountAdminID := int64(42)
	account := accountEntityToService(&dbent.Account{
		AccountAdminID: &accountAdminID,
		RateMultiplier: 0.375,
	})
	require.NotNil(t, account)
	require.Equal(t, &accountAdminID, account.AccountAdminID)
	require.NotNil(t, account.RateMultiplier)
	require.Equal(t, 0.375, *account.RateMultiplier)

	user := userEntityToService(&dbent.User{SupplyRateMultiplier: 0.375})
	require.NotNil(t, user)
	require.Equal(t, 0.375, user.SupplyRateMultiplier)
}

func TestSchedulerMetadataPreservesAccountAdminID(t *testing.T) {
	accountAdminID := int64(42)
	metadata := buildSchedulerMetadataAccount(service.Account{AccountAdminID: &accountAdminID})
	require.Equal(t, &accountAdminID, metadata.AccountAdminID)
}

func TestUserRepositoryPersistsSupplyRateMultiplierDefaultsAndZero(t *testing.T) {
	repo, _ := newUserEntRepo(t)
	ctx := context.Background()

	ordinary := &service.User{
		Email:        "ordinary-supply-rate@example.com",
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, ordinary))
	require.Equal(t, 1.0, ordinary.SupplyRateMultiplier)

	accountAdmin := &service.User{
		Email:                "account-admin-zero-rate@example.com",
		PasswordHash:         "hash",
		Role:                 service.RoleAccountAdmin,
		Status:               service.StatusActive,
		SupplyRateMultiplier: 0,
	}
	require.NoError(t, repo.Create(ctx, accountAdmin))
	require.Equal(t, float64(0), accountAdmin.SupplyRateMultiplier)

	accountAdmin.SupplyRateMultiplier = 0.625
	require.NoError(t, repo.Update(ctx, accountAdmin, service.UserUpdateFields{SupplyRateMultiplier: true}))
	updated, err := repo.GetByID(ctx, accountAdmin.ID)
	require.NoError(t, err)
	require.Equal(t, 0.625, updated.SupplyRateMultiplier)

	invalid := &service.User{
		Email:                "account-admin-negative-rate@example.com",
		PasswordHash:         "hash",
		Role:                 service.RoleAccountAdmin,
		Status:               service.StatusActive,
		SupplyRateMultiplier: -0.001,
	}
	require.Error(t, repo.Create(ctx, invalid))
}
