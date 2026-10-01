//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// totpVMUserRepoStub 仅实现 TOTP 验证方式测试所需方法；未桩方法调用即 panic（嵌入 nil 接口）。
type totpVMUserRepoStub struct {
	UserRepository
	user          *User
	totpDisabled  bool
	disableCalled bool
}

func (s *totpVMUserRepoStub) GetByID(ctx context.Context, id int64) (*User, error) {
	if s.user == nil {
		return nil, errors.New("user not found")
	}
	return s.user, nil
}

func (s *totpVMUserRepoStub) DisableTotp(ctx context.Context, userID int64) error {
	s.disableCalled = true
	s.totpDisabled = true
	return nil
}

func newTotpVMService(t *testing.T, user *User) (*TotpService, *totpVMUserRepoStub) {
	t.Helper()
	userRepo := &totpVMUserRepoStub{user: user}
	return NewTotpService(userRepo, nil, nil, nil), userRepo
}

func TestGetVerificationMethodAdminAlwaysPassword(t *testing.T) {
	admin := &User{ID: 1, Email: "admin@example.com", Role: RoleAdmin}
	svc, _ := newTotpVMService(t, admin)

	method, err := svc.GetVerificationMethod(context.Background(), admin.ID)
	require.NoError(t, err)
	require.Equal(t, "password", method.Method)
}

func TestGetVerificationMethodAccountAdminAlwaysPassword(t *testing.T) {
	accountAdmin := &User{ID: 3, Email: "operator@example.com", Role: RoleAccountAdmin}
	svc, _ := newTotpVMService(t, accountAdmin)

	method, err := svc.GetVerificationMethod(context.Background(), accountAdmin.ID)
	require.NoError(t, err)
	require.Equal(t, "password", method.Method)
}

func TestTotpDisableAdminUsesPassword(t *testing.T) {
	admin := &User{ID: 1, Email: "admin@example.com", Role: RoleAdmin, TotpEnabled: true}
	require.NoError(t, admin.SetPassword("correct-password"))
	svc, userRepo := newTotpVMService(t, admin)

	// 缺密码时拒绝停用。
	err := svc.Disable(context.Background(), admin.ID, "")
	require.ErrorIs(t, err, ErrPasswordRequired)

	// 密码错误 → 拒绝。
	err = svc.Disable(context.Background(), admin.ID, "wrong-password")
	require.ErrorIs(t, err, ErrPasswordIncorrect)

	// 密码正确时成功停用。
	err = svc.Disable(context.Background(), admin.ID, "correct-password")
	require.NoError(t, err)
	require.True(t, userRepo.disableCalled)
}

func TestTotpDisableAccountAdminUsesPassword(t *testing.T) {
	accountAdmin := &User{ID: 3, Email: "operator@example.com", Role: RoleAccountAdmin, TotpEnabled: true}
	require.NoError(t, accountAdmin.SetPassword("correct-password"))
	svc, userRepo := newTotpVMService(t, accountAdmin)

	err := svc.Disable(context.Background(), accountAdmin.ID, "correct-password")
	require.NoError(t, err)
	require.True(t, userRepo.disableCalled)
}
