package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type supplyRateTxContextKey struct{}

type supplyRateUserRepoStub struct {
	UserRepository
	user       *User
	created    []*User
	updateErr  error
	lastFields UserUpdateFields
	sequence   *[]string
	committed  bool
	rolledBack bool
}

func (s *supplyRateUserRepoStub) Create(_ context.Context, user *User) error {
	clone := *user
	clone.ID = 91
	*user = clone
	s.created = append(s.created, &clone)
	return nil
}

func (s *supplyRateUserRepoStub) GetByID(_ context.Context, _ int64) (*User, error) {
	if s.user == nil {
		return nil, ErrUserNotFound
	}
	clone := *s.user
	return &clone, nil
}

func (s *supplyRateUserRepoStub) Update(ctx context.Context, user *User, fields UserUpdateFields) error {
	s.lastFields = fields
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "user")
	}
	if ctx.Value(supplyRateTxContextKey{}) != true {
		return errors.New("user update did not receive transaction context")
	}
	if s.updateErr != nil {
		return s.updateErr
	}
	clone := *user
	s.user = &clone
	return nil
}

func (s *supplyRateUserRepoStub) WithUserProfileIdentityTx(ctx context.Context, fn func(context.Context) error) error {
	txCtx := context.WithValue(ctx, supplyRateTxContextKey{}, true)
	if err := fn(txCtx); err != nil {
		s.rolledBack = true
		if s.sequence != nil {
			*s.sequence = append(*s.sequence, "rollback")
		}
		return err
	}
	s.committed = true
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "commit")
	}
	return nil
}

type supplyRateAccountRepoStub struct {
	AccountRepository
	accountIDs     []int64
	updateErr      error
	updatedAdminID int64
	updatedRate    float64
	refreshedIDs   []int64
	sequence       *[]string
}

func (s *supplyRateAccountRepoStub) UpdateSupplyRateMultiplierByAccountAdmin(ctx context.Context, accountAdminID int64, multiplier float64) ([]int64, error) {
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "accounts")
	}
	if ctx.Value(supplyRateTxContextKey{}) != true {
		return nil, errors.New("account update did not receive transaction context")
	}
	s.updatedAdminID = accountAdminID
	s.updatedRate = multiplier
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	return append([]int64(nil), s.accountIDs...), nil
}

func (s *supplyRateAccountRepoStub) RefreshSchedulerAccountSnapshots(_ context.Context, accountIDs []int64) {
	if s.sequence != nil {
		*s.sequence = append(*s.sequence, "refresh")
	}
	s.refreshedIDs = append([]int64(nil), accountIDs...)
}

func TestAdminServiceCreateAccountAdminSupplyRateDefaultsAndValidation(t *testing.T) {
	tests := []struct {
		name      string
		rate      *float64
		want      float64
		wantError bool
	}{
		{name: "default", want: 1},
		{name: "explicit zero", rate: float64Pointer(0), want: 0},
		{name: "fraction", rate: float64Pointer(0.375), want: 0.375},
		{name: "negative", rate: float64Pointer(-0.01), wantError: true},
		{name: "nan", rate: float64Pointer(math.NaN()), wantError: true},
		{name: "positive infinity", rate: float64Pointer(math.Inf(1)), wantError: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplyRateUserRepoStub{}
			svc := &adminServiceImpl{userRepo: repo}
			user, err := svc.CreateUser(context.Background(), &CreateUserInput{
				Email:                "operator@example.com",
				Password:             "pass123",
				Role:                 RoleAccountAdmin,
				SupplyRateMultiplier: tc.rate,
			})
			if tc.wantError {
				require.Error(t, err)
				require.Empty(t, repo.created)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, user.SupplyRateMultiplier)
			require.Len(t, repo.created, 1)
			require.Equal(t, tc.want, repo.created[0].SupplyRateMultiplier)
		})
	}
}

func TestAdminServiceUpdateAccountAdminSupplyRateCommitsBeforeCacheRefresh(t *testing.T) {
	sequence := make([]string, 0, 4)
	userRepo := &supplyRateUserRepoStub{
		user:     &User{ID: 42, Email: "operator@example.com", Role: RoleAccountAdmin, SupplyRateMultiplier: 1},
		sequence: &sequence,
	}
	accountRepo := &supplyRateAccountRepoStub{
		accountIDs: []int64{11, 29},
		sequence:   &sequence,
	}
	svc := &adminServiceImpl{userRepo: userRepo, accountRepo: accountRepo}
	rate := 0.0

	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{SupplyRateMultiplier: &rate})

	require.NoError(t, err)
	require.Zero(t, updated.SupplyRateMultiplier)
	require.True(t, userRepo.lastFields.SupplyRateMultiplier)
	require.True(t, userRepo.committed)
	require.False(t, userRepo.rolledBack)
	require.Equal(t, int64(42), accountRepo.updatedAdminID)
	require.Zero(t, accountRepo.updatedRate)
	require.Equal(t, []int64{11, 29}, accountRepo.refreshedIDs)
	require.Equal(t, []string{"user", "accounts", "commit", "refresh"}, sequence)
}

func TestAdminServiceUpdateAccountAdminSupplyRateRollsBackWithoutCacheRefresh(t *testing.T) {
	failure := errors.New("account rate update failed")
	sequence := make([]string, 0, 3)
	userRepo := &supplyRateUserRepoStub{
		user:     &User{ID: 42, Email: "operator@example.com", Role: RoleAccountAdmin, SupplyRateMultiplier: 1},
		sequence: &sequence,
	}
	accountRepo := &supplyRateAccountRepoStub{updateErr: failure, sequence: &sequence}
	svc := &adminServiceImpl{userRepo: userRepo, accountRepo: accountRepo}
	rate := 0.5

	_, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{SupplyRateMultiplier: &rate})

	require.ErrorIs(t, err, failure)
	require.False(t, userRepo.committed)
	require.True(t, userRepo.rolledBack)
	require.Empty(t, accountRepo.refreshedIDs)
	require.Equal(t, []string{"user", "accounts", "rollback"}, sequence)
}

func TestAdminServiceUpdateSupplyRateRejectsNonAccountAdminAndInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *User
		rate float64
	}{
		{name: "ordinary user", user: &User{ID: 42, Role: RoleUser, SupplyRateMultiplier: 1}, rate: 0.5},
		{name: "negative", user: &User{ID: 42, Role: RoleAccountAdmin, SupplyRateMultiplier: 1}, rate: -0.1},
		{name: "nan", user: &User{ID: 42, Role: RoleAccountAdmin, SupplyRateMultiplier: 1}, rate: math.NaN()},
		{name: "infinity", user: &User{ID: 42, Role: RoleAccountAdmin, SupplyRateMultiplier: 1}, rate: math.Inf(1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &supplyRateUserRepoStub{user: tc.user}
			svc := &adminServiceImpl{userRepo: repo}
			_, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{SupplyRateMultiplier: &tc.rate})
			require.Error(t, err)
			require.False(t, repo.committed)
		})
	}
}

func float64Pointer(value float64) *float64 { return &value }
