package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountAdminOwnershipRepoStub struct {
	AccountRepository
	accountIDs []int64
	updateErr  error
	refreshed  []int64
	sequence   *[]string
}

type accountAdminStatusUserRepoStub struct {
	*supplyRateUserRepoStub
}

func (s *accountAdminStatusUserRepoStub) Update(_ context.Context, user *User, fields UserUpdateFields) error {
	s.lastFields = fields
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "user")
	}
	clone := *user
	s.user = &clone
	return nil
}

func (s *accountAdminOwnershipRepoStub) ReleaseAccountAdminOwnership(ctx context.Context, accountAdminID int64) ([]int64, error) {
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "release")
	}
	if ctx.Value(supplyRateTxContextKey{}) != true {
		return nil, errors.New("ownership release did not receive transaction context")
	}
	if accountAdminID <= 0 {
		return nil, errors.New("invalid account administrator id")
	}
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return append([]int64(nil), s.accountIDs...), nil
}

func (s *accountAdminOwnershipRepoStub) RefreshSchedulerAccountSnapshots(_ context.Context, accountIDs []int64) {
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "refresh")
	}
	s.refreshed = append([]int64(nil), accountIDs...)
}

func TestAdminServiceDemotingAccountAdminReleasesOwnedAccountsAtomically(t *testing.T) {
	sequence := make([]string, 0, 4)
	userRepo := &supplyRateUserRepoStub{
		user:     &User{ID: 42, Email: "operator@example.com", Role: RoleAccountAdmin, SupplyRateMultiplier: 0.375},
		sequence: &sequence,
	}
	accountRepo := &accountAdminOwnershipRepoStub{
		accountIDs: []int64{11, 29},
		sequence:   &sequence,
	}
	svc := &adminServiceImpl{userRepo: userRepo, accountRepo: accountRepo}

	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{Role: RoleUser})

	require.NoError(t, err)
	require.Equal(t, RoleUser, updated.Role)
	require.Equal(t, 1.0, updated.SupplyRateMultiplier)
	require.Equal(t, UserUpdateFields{Role: true, SupplyRateMultiplier: true}, userRepo.lastFields)
	require.Equal(t, []int64{11, 29}, accountRepo.refreshed)
	require.Equal(t, []string{"user", "release", "commit", "refresh"}, sequence)
}

func TestAdminServiceDemotingAccountAdminRollsBackWhenOwnershipReleaseFails(t *testing.T) {
	failure := errors.New("ownership release failed")
	sequence := make([]string, 0, 3)
	userRepo := &supplyRateUserRepoStub{
		user:     &User{ID: 42, Email: "operator@example.com", Role: RoleAccountAdmin, SupplyRateMultiplier: 0.5},
		sequence: &sequence,
	}
	accountRepo := &accountAdminOwnershipRepoStub{updateErr: failure, sequence: &sequence}
	svc := &adminServiceImpl{userRepo: userRepo, accountRepo: accountRepo}

	_, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{Role: RoleUser})

	require.ErrorIs(t, err, failure)
	require.False(t, userRepo.committed)
	require.True(t, userRepo.rolledBack)
	require.Empty(t, accountRepo.refreshed)
	require.Equal(t, []string{"user", "release", "rollback"}, sequence)
}

func TestAdminServiceDisablingAccountAdminRetainsOwnedAccountsForReactivation(t *testing.T) {
	sequence := make([]string, 0, 2)
	userRepo := &accountAdminStatusUserRepoStub{&supplyRateUserRepoStub{
		user:     &User{ID: 42, Email: "operator@example.com", Role: RoleAccountAdmin, Status: StatusActive, SupplyRateMultiplier: 0.375},
		sequence: &sequence,
	}}
	accountRepo := &accountAdminOwnershipRepoStub{accountIDs: []int64{11, 29}, sequence: &sequence}
	svc := &adminServiceImpl{userRepo: userRepo, accountRepo: accountRepo}

	disabled, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{Status: StatusDisabled})
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, disabled.Status)
	require.Equal(t, 0.375, disabled.SupplyRateMultiplier)
	require.Equal(t, UserUpdateFields{Status: true}, userRepo.lastFields)
	require.Equal(t, []string{"user"}, sequence)
	require.Empty(t, accountRepo.refreshed)

	reactivated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{Status: StatusActive})
	require.NoError(t, err)
	require.Equal(t, StatusActive, reactivated.Status)
	require.Equal(t, 0.375, reactivated.SupplyRateMultiplier)
	require.Equal(t, UserUpdateFields{Status: true}, userRepo.lastFields)
	require.Equal(t, []string{"user", "user"}, sequence)
	require.Empty(t, accountRepo.refreshed)
}
