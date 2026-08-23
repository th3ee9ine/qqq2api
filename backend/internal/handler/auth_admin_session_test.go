//go:build unit

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerAdministratorPasswordLogin(t *testing.T) {
	admin := newAdminAuthTestUser(t)
	cache := newAdminAuthRefreshTokenCache()
	authService := newAdminAuthService(admin, cache)
	handler := &AuthHandler{authService: authService}

	recorder := performAuthJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    admin.Email,
		"password": "admin-password",
	}, handler.Login)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			User         struct {
				Role string `json:"role"`
			} `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.NotEmpty(t, resp.Data.AccessToken)
	require.NotEmpty(t, resp.Data.RefreshToken)
	require.Equal(t, service.RoleAdmin, resp.Data.User.Role)
}

func TestAuthHandlerRejectsOtherDatabaseAdministratorPasswordLogin(t *testing.T) {
	otherAdmin := newAdminAuthTestUser(t)
	otherAdmin.ID = 2
	otherAdmin.Email = "other-admin@example.com"
	cache := newAdminAuthRefreshTokenCache()
	handler := &AuthHandler{authService: newAdminAuthService(otherAdmin, cache)}

	recorder := performAuthJSONRequest(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    otherAdmin.Email,
		"password": "admin-password",
	}, handler.Login)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ADMIN_ONLY_MODE")
	require.Empty(t, cache.tokens)
}

func TestAuthHandlerAdministratorRefreshAndLogout(t *testing.T) {
	admin := newAdminAuthTestUser(t)
	cache := newAdminAuthRefreshTokenCache()
	authService := newAdminAuthService(admin, cache)
	handler := &AuthHandler{authService: authService}

	pair, err := authService.GenerateTokenPair(context.Background(), admin, "")
	require.NoError(t, err)

	refreshRecorder := performAuthJSONRequest(t, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": pair.RefreshToken,
	}, handler.RefreshToken)
	require.Equal(t, http.StatusOK, refreshRecorder.Code)

	var refreshResp struct {
		Code int `json:"code"`
		Data struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(refreshRecorder.Body.Bytes(), &refreshResp))
	require.Equal(t, 0, refreshResp.Code)
	require.NotEmpty(t, refreshResp.Data.AccessToken)
	require.NotEmpty(t, refreshResp.Data.RefreshToken)
	require.NotEqual(t, pair.RefreshToken, refreshResp.Data.RefreshToken)

	logoutRecorder := performAuthJSONRequest(t, http.MethodPost, "/api/v1/auth/logout", map[string]string{
		"refresh_token": refreshResp.Data.RefreshToken,
	}, handler.Logout)
	require.Equal(t, http.StatusOK, logoutRecorder.Code)
	require.Empty(t, cache.tokens)
}

func TestAuthHandlerRejectsLegacyRefreshTokenForOtherAdministrator(t *testing.T) {
	otherAdmin := newAdminAuthTestUser(t)
	otherAdmin.ID = 2
	otherAdmin.Email = "other-admin@example.com"
	cache := newAdminAuthRefreshTokenCache()
	cfg := &config.Config{JWT: config.JWTConfig{
		Secret:                 "admin-auth-test-secret",
		ExpireHour:             1,
		RefreshTokenExpireDays: 7,
	}}
	cfg.Default.AdminEmail = otherAdmin.Email
	repo := &userHandlerRepoStub{user: otherAdmin}
	authService := service.NewAuthService(nil, repo, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	pair, err := authService.GenerateTokenPair(context.Background(), otherAdmin, "")
	require.NoError(t, err)

	// Simulate an upgrade where this historical admin already has a refresh
	// token before ADMIN_EMAIL selects the canonical administrator.
	cfg.Default.AdminEmail = "admin@example.com"
	handler := &AuthHandler{authService: authService}
	recorder := performAuthJSONRequest(t, http.MethodPost, "/api/v1/auth/refresh", map[string]string{
		"refresh_token": pair.RefreshToken,
	}, handler.RefreshToken)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ADMIN_ONLY_MODE")
	require.Empty(t, cache.tokens)
}

func TestAuthHandlerAdministratorLogin2FA(t *testing.T) {
	admin := newAdminAuthTestUser(t)
	secret := "JBSWY3DPEHPK3PXP"
	admin.TotpEnabled = true
	admin.TotpSecretEncrypted = &secret

	refreshCache := newAdminAuthRefreshTokenCache()
	totpCache := &adminAuthTotpCache{
		loginSessions: map[string]*service.TotpLoginSession{
			"temp-token": {
				UserID:      admin.ID,
				Email:       admin.Email,
				TokenExpiry: time.Now().Add(time.Minute),
			},
		},
	}
	repo := &userHandlerRepoStub{user: admin}
	totpService := service.NewTotpService(repo, adminAuthTotpEncryptor{}, totpCache, nil, nil, nil)
	handler := &AuthHandler{
		authService: newAdminAuthServiceWithRepo(repo, refreshCache),
		userService: service.NewUserService(repo, nil, nil, nil),
		totpService: totpService,
	}

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	recorder := performAuthJSONRequest(t, http.MethodPost, "/api/v1/auth/login/2fa", map[string]string{
		"temp_token": "temp-token",
		"totp_code":  code,
	}, handler.Login2FA)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.NotContains(t, totpCache.loginSessions, "temp-token")
	require.NotEmpty(t, refreshCache.tokens)
}

func TestAuthHandlerRejectsOtherDatabaseAdministratorLogin2FA(t *testing.T) {
	otherAdmin := newAdminAuthTestUser(t)
	otherAdmin.ID = 2
	otherAdmin.Email = "other-admin@example.com"
	secret := "JBSWY3DPEHPK3PXP"
	otherAdmin.TotpEnabled = true
	otherAdmin.TotpSecretEncrypted = &secret

	refreshCache := newAdminAuthRefreshTokenCache()
	totpCache := &adminAuthTotpCache{loginSessions: map[string]*service.TotpLoginSession{
		"other-temp-token": {
			UserID:      otherAdmin.ID,
			Email:       otherAdmin.Email,
			TokenExpiry: time.Now().Add(time.Minute),
		},
	}}
	repo := &userHandlerRepoStub{user: otherAdmin}
	totpService := service.NewTotpService(repo, adminAuthTotpEncryptor{}, totpCache, nil, nil, nil)
	handler := &AuthHandler{
		authService: newAdminAuthServiceWithRepo(repo, refreshCache),
		userService: service.NewUserService(repo, nil, nil, nil),
		totpService: totpService,
	}

	code, err := totp.GenerateCode(secret, time.Now())
	require.NoError(t, err)
	recorder := performAuthJSONRequest(t, http.MethodPost, "/api/v1/auth/login/2fa", map[string]string{
		"temp_token": "other-temp-token",
		"totp_code":  code,
	}, handler.Login2FA)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ADMIN_ONLY_MODE")
	require.Contains(t, totpCache.loginSessions, "other-temp-token")
	require.Empty(t, refreshCache.tokens)
}

func newAdminAuthTestUser(t *testing.T) *service.User {
	t.Helper()
	user := &service.User{
		ID:       1,
		Email:    "admin@example.com",
		Username: "admin",
		Role:     service.RoleAdmin,
		Status:   service.StatusActive,
	}
	require.NoError(t, user.SetPassword("admin-password"))
	return user
}

func newAdminAuthService(user *service.User, cache service.RefreshTokenCache) *service.AuthService {
	return newAdminAuthServiceWithRepo(&userHandlerRepoStub{user: user}, cache)
}

func newAdminAuthServiceWithRepo(repo service.UserRepository, cache service.RefreshTokenCache) *service.AuthService {
	cfg := &config.Config{JWT: config.JWTConfig{
		Secret:                 "admin-auth-test-secret",
		ExpireHour:             1,
		RefreshTokenExpireDays: 7,
	}}
	cfg.Default.AdminEmail = "admin@example.com"
	return service.NewAuthService(nil, repo, nil, cache, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
}

func performAuthJSONRequest(
	t *testing.T,
	method string,
	path string,
	body any,
	handler gin.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	return recorder
}

type adminAuthRefreshTokenCache struct {
	tokens map[string]*service.RefreshTokenData
}

func newAdminAuthRefreshTokenCache() *adminAuthRefreshTokenCache {
	return &adminAuthRefreshTokenCache{tokens: make(map[string]*service.RefreshTokenData)}
}

func (s *adminAuthRefreshTokenCache) StoreRefreshToken(_ context.Context, tokenHash string, data *service.RefreshTokenData, _ time.Duration) error {
	cloned := *data
	s.tokens[tokenHash] = &cloned
	return nil
}

func (s *adminAuthRefreshTokenCache) GetRefreshToken(_ context.Context, tokenHash string) (*service.RefreshTokenData, error) {
	data, ok := s.tokens[tokenHash]
	if !ok {
		return nil, service.ErrRefreshTokenNotFound
	}
	cloned := *data
	return &cloned, nil
}

func (s *adminAuthRefreshTokenCache) DeleteRefreshToken(_ context.Context, tokenHash string) error {
	delete(s.tokens, tokenHash)
	return nil
}

func (s *adminAuthRefreshTokenCache) DeleteUserRefreshTokens(_ context.Context, userID int64) error {
	for tokenHash, data := range s.tokens {
		if data.UserID == userID {
			delete(s.tokens, tokenHash)
		}
	}
	return nil
}

func (s *adminAuthRefreshTokenCache) DeleteTokenFamily(_ context.Context, familyID string) error {
	for tokenHash, data := range s.tokens {
		if data.FamilyID == familyID {
			delete(s.tokens, tokenHash)
		}
	}
	return nil
}

func (s *adminAuthRefreshTokenCache) AddToUserTokenSet(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *adminAuthRefreshTokenCache) AddToFamilyTokenSet(context.Context, string, string, time.Duration) error {
	return nil
}

func (s *adminAuthRefreshTokenCache) GetUserTokenHashes(context.Context, int64) ([]string, error) {
	return nil, nil
}

func (s *adminAuthRefreshTokenCache) GetFamilyTokenHashes(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *adminAuthRefreshTokenCache) IsTokenInFamily(context.Context, string, string) (bool, error) {
	return false, nil
}

type adminAuthTotpEncryptor struct{}

func (adminAuthTotpEncryptor) Encrypt(value string) (string, error) { return value, nil }
func (adminAuthTotpEncryptor) Decrypt(value string) (string, error) { return value, nil }

type adminAuthTotpCache struct {
	loginSessions map[string]*service.TotpLoginSession
	attempts      int
}

func (s *adminAuthTotpCache) GetSetupSession(context.Context, int64) (*service.TotpSetupSession, error) {
	return nil, nil
}

func (s *adminAuthTotpCache) SetSetupSession(context.Context, int64, *service.TotpSetupSession, time.Duration) error {
	return nil
}

func (s *adminAuthTotpCache) DeleteSetupSession(context.Context, int64) error { return nil }

func (s *adminAuthTotpCache) GetLoginSession(_ context.Context, tempToken string) (*service.TotpLoginSession, error) {
	return s.loginSessions[tempToken], nil
}

func (s *adminAuthTotpCache) SetLoginSession(_ context.Context, tempToken string, session *service.TotpLoginSession, _ time.Duration) error {
	s.loginSessions[tempToken] = session
	return nil
}

func (s *adminAuthTotpCache) DeleteLoginSession(_ context.Context, tempToken string) error {
	delete(s.loginSessions, tempToken)
	return nil
}

func (s *adminAuthTotpCache) IncrementVerifyAttempts(context.Context, int64) (int, error) {
	s.attempts++
	return s.attempts, nil
}

func (s *adminAuthTotpCache) GetVerifyAttempts(context.Context, int64) (int, error) {
	return s.attempts, nil
}

func (s *adminAuthTotpCache) ClearVerifyAttempts(context.Context, int64) error {
	s.attempts = 0
	return nil
}

func (s *adminAuthTotpCache) SetStepUpGrant(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *adminAuthTotpCache) HasStepUpGrant(context.Context, int64, string) (bool, error) {
	return false, nil
}
