package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"

	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/config"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/logger"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials      = infraerrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password")
	ErrUserNotActive           = infraerrors.Forbidden("USER_NOT_ACTIVE", "user is not active")
	ErrEmailExists             = infraerrors.Conflict("EMAIL_EXISTS", "email already exists")
	ErrInvalidToken            = infraerrors.Unauthorized("INVALID_TOKEN", "invalid token")
	ErrTokenExpired            = infraerrors.Unauthorized("TOKEN_EXPIRED", "token has expired")
	ErrAccessTokenExpired      = infraerrors.Unauthorized("ACCESS_TOKEN_EXPIRED", "access token has expired")
	ErrTokenTooLarge           = infraerrors.BadRequest("TOKEN_TOO_LARGE", "token too large")
	ErrTokenRevoked            = infraerrors.Unauthorized("TOKEN_REVOKED", "token has been revoked")
	ErrRefreshTokenInvalid     = infraerrors.Unauthorized("REFRESH_TOKEN_INVALID", "invalid refresh token")
	ErrRefreshTokenExpired     = infraerrors.Unauthorized("REFRESH_TOKEN_EXPIRED", "refresh token has expired")
	ErrRefreshTokenReused      = infraerrors.Unauthorized("REFRESH_TOKEN_REUSED", "refresh token has been reused")
	ErrAdminOnlyMode           = infraerrors.Forbidden("ADMIN_ONLY_MODE", "only the configured administrator account can sign in")
	ErrServiceUnavailable      = infraerrors.ServiceUnavailable("SERVICE_UNAVAILABLE", "service temporarily unavailable")
	ErrCaptchaProviderConflict = infraerrors.ServiceUnavailable("CAPTCHA_PROVIDER_CONFLICT", "multiple captcha providers are enabled")
)

// maxTokenLength 限制 token 大小，避免超长 header 触发解析时的异常内存分配。
const maxTokenLength = 8192

// refreshTokenPrefix is the prefix for refresh tokens to distinguish them from access tokens.
const refreshTokenPrefix = "rt_"

// JWTClaims JWT载荷数据
type JWTClaims struct {
	UserID       int64  `json:"user_id"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	TokenVersion int64  `json:"token_version"` // Used to invalidate tokens on password change
	// SessionID 会话 ID（与 refresh token family 对应），用于单会话撤销与 step-up 授权绑定。
	SessionID string `json:"sid,omitempty"`
	// BindingHash 会话指纹哈希（IP+UA），会话绑定开启时校验；空值表示旧 token（平滑升级）。
	BindingHash string `json:"bnd,omitempty"`
	jwt.RegisteredClaims
}

// AuthService 认证服务
type AuthService struct {
	userRepo              UserRepository
	refreshTokenCache     RefreshTokenCache
	cfg                   *config.Config
	settingService        *SettingService
	turnstileService      *TurnstileService
	tencentCaptchaService *TencentCaptchaService
	aliyunCaptchaService  *AliyunCaptchaService
}

type CaptchaProof struct {
	// TurnstileToken 承载 Cloudflare Turnstile token；阿里云验证码复用该字段承载 captchaVerifyParam
	TurnstileToken string
	TencentTicket  string
	TencentRandstr string
}

// NewAuthService 创建认证服务实例
func NewAuthService(
	userRepo UserRepository,
	refreshTokenCache RefreshTokenCache,
	cfg *config.Config,
	settingService *SettingService,
	turnstileService *TurnstileService,
) *AuthService {
	return &AuthService{
		userRepo:          userRepo,
		refreshTokenCache: refreshTokenCache,
		cfg:               cfg,
		settingService:    settingService,
		turnstileService:  turnstileService,
	}
}

// ConfiguredAdminEmail returns the single interactive administrator identity.
// Config loading explicitly binds this field to ADMIN_EMAIL so setup and
// runtime authentication cannot disagree about which database row is trusted.
func (s *AuthService) ConfiguredAdminEmail() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	return strings.TrimSpace(s.cfg.Default.AdminEmail)
}

// IsConfiguredAdmin reports whether user is the unique administrator selected
// by ADMIN_EMAIL/default.admin_email. Old installations whose config predates
// that field fall back to the first administrator until setup persists the
// selected email; once an email is configured there is no role-only fallback.
func (s *AuthService) IsConfiguredAdmin(ctx context.Context, user *User) bool {
	if user == nil || !user.IsAdmin() {
		return false
	}
	configuredEmail := s.ConfiguredAdminEmail()
	if configuredEmail != "" {
		return strings.EqualFold(strings.TrimSpace(user.Email), configuredEmail)
	}
	if s == nil || s.userRepo == nil {
		return false
	}
	legacyAdmin, err := s.userRepo.GetFirstAdmin(ctx)
	return err == nil && legacyAdmin != nil && legacyAdmin.ID == user.ID
}

// CanAccessAdminPanel reports whether a user may establish an interactive
// control-panel session. The configured RoleAdmin remains the sole super
// administrator, while RoleAccountAdmin identities are intentionally allowed
// to sign in and are constrained by route-level authorization.
func (s *AuthService) CanAccessAdminPanel(ctx context.Context, user *User) bool {
	if user == nil {
		return false
	}
	if user.IsAccountAdmin() {
		return true
	}
	return s.IsConfiguredAdmin(ctx, user)
}

func (s *AuthService) SetTencentCaptchaService(tencentCaptchaService *TencentCaptchaService) {
	s.tencentCaptchaService = tencentCaptchaService
}

func (s *AuthService) SetAliyunCaptchaService(aliyunCaptchaService *AliyunCaptchaService) {
	s.aliyunCaptchaService = aliyunCaptchaService
}

func (s *AuthService) VerifyCaptcha(ctx context.Context, proof CaptchaProof, remoteIP string) error {
	required := s.cfg != nil && s.cfg.Server.Mode == "release" && s.cfg.Turnstile.Required
	if s.settingService == nil {
		if required {
			return ErrTurnstileNotConfigured
		}
		return nil
	}

	providerConfig, err := s.settingService.GetCaptchaProviderConfig(ctx)
	if err != nil {
		logger.LegacyPrintf("service.auth", "%s", "[Auth] Failed to read captcha provider settings")
		return ErrServiceUnavailable
	}
	turnstileEnabled := providerConfig.TurnstileEnabled
	tencentEnabled := providerConfig.Tencent.Enabled
	aliyunEnabled := providerConfig.Aliyun.Enabled
	if captchaProvidersConflict(turnstileEnabled, tencentEnabled, aliyunEnabled) {
		return ErrCaptchaProviderConflict
	}
	if tencentEnabled {
		if s.tencentCaptchaService == nil {
			return ErrTencentCaptchaNotConfigured
		}
		return s.tencentCaptchaService.VerifyTicketWithConfig(ctx, providerConfig.Tencent, proof.TencentTicket, proof.TencentRandstr, remoteIP)
	}
	if aliyunEnabled {
		if s.aliyunCaptchaService == nil {
			return ErrAliyunCaptchaNotConfigured
		}
		return s.aliyunCaptchaService.VerifyParamWithConfig(ctx, providerConfig.Aliyun, proof.TurnstileToken)
	}
	if turnstileEnabled {
		if s.turnstileService == nil || strings.TrimSpace(providerConfig.TurnstileSecretKey) == "" {
			return ErrTurnstileNotConfigured
		}
		return s.turnstileService.VerifyTokenWithSecret(ctx, providerConfig.TurnstileSecretKey, proof.TurnstileToken, remoteIP)
	}
	if required {
		return ErrTurnstileNotConfigured
	}
	return nil
}

// captchaProvidersConflict 同一时间仅允许启用一家人机验证服务商
func captchaProvidersConflict(enabled ...bool) bool {
	count := 0
	for _, e := range enabled {
		if e {
			count++
		}
	}
	return count > 1
}

// VerifyTurnstile 保留旧内部接口，生产 handler 使用 VerifyCaptcha。
func (s *AuthService) VerifyTurnstile(ctx context.Context, token string, remoteIP string) error {
	return s.VerifyCaptcha(ctx, CaptchaProof{TurnstileToken: token}, remoteIP)
}

// IsTurnstileEnabled 检查是否启用Turnstile验证
func (s *AuthService) IsTurnstileEnabled(ctx context.Context) bool {
	if s.turnstileService == nil {
		return false
	}
	return s.turnstileService.IsEnabled(ctx)
}

// Login 用户登录，返回JWT token
func (s *AuthService) Login(ctx context.Context, email, password string) (string, *User, error) {
	// 查找用户
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", nil, ErrInvalidCredentials
		}
		// 记录数据库错误但不暴露给用户
		logger.LegacyPrintf("service.auth", "[Auth] Database error during login: %v", err)
		return "", nil, ErrServiceUnavailable
	}

	// 验证密码
	if !s.CheckPassword(password, user.PasswordHash) {
		return "", nil, ErrInvalidCredentials
	}

	// 检查用户状态
	if !user.IsActive() {
		return "", nil, ErrUserNotActive
	}
	if !s.CanAccessAdminPanel(ctx, user) {
		return "", nil, ErrAdminOnlyMode
	}

	// 生成JWT token
	token, err := s.GenerateToken(ctx, user)
	if err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}

	return token, user, nil
}

// RecordSuccessfulLogin updates the operator's activity without an OAuth identity dependency.
func (s *AuthService) RecordSuccessfulLogin(ctx context.Context, userID int64) {
	if s == nil || s.userRepo == nil || userID <= 0 {
		return
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || user == nil {
		return
	}
	now := time.Now().UTC()
	user.LastLoginAt, user.LastActiveAt = &now, &now
	if err := s.userRepo.Update(ctx, user, UserUpdateFields{LastLoginAt: true, LastActiveAt: true}); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to touch login timestamps: user_id=%d err=%v", userID, err)
	}
}

// ValidateToken 验证JWT token并返回用户声明
func (s *AuthService) ValidateToken(tokenString string) (*JWTClaims, error) {
	// 先做长度校验，尽早拒绝异常超长 token，降低 DoS 风险。
	if len(tokenString) > maxTokenLength {
		return nil, ErrTokenTooLarge
	}

	// 使用解析器并限制可接受的签名算法，防止算法混淆。
	parser := jwt.NewParser(jwt.WithValidMethods([]string{
		jwt.SigningMethodHS256.Name,
		jwt.SigningMethodHS384.Name,
		jwt.SigningMethodHS512.Name,
	}))

	// 保留默认 claims 校验（exp/nbf），避免放行过期或未生效的 token。
	token, err := parser.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		// 验证签名方法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWT.Secret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			// token 过期但仍返回 claims（用于 RefreshToken 等场景）
			// jwt-go 在解析时即使遇到过期错误，token.Claims 仍会被填充
			if claims, ok := token.Claims.(*JWTClaims); ok {
				return claims, ErrTokenExpired
			}
			return nil, ErrTokenExpired
		}
		return nil, ErrInvalidToken
	}

	if claims, ok := token.Claims.(*JWTClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrInvalidToken
}

func randomHexString(byteLength int) (string, error) {
	if byteLength <= 0 {
		byteLength = 16
	}
	buf := make([]byte, byteLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// GenerateToken 生成JWT access token
// 使用新的access_token_expire_minutes配置项（如果配置了），否则回退到expire_hour。
// 会话指纹（IP/UA）从 ctx 中提取（由 HTTP 入口中间件注入），缺失时生成不带绑定的 token。
func (s *AuthService) GenerateToken(ctx context.Context, user *User) (string, error) {
	sessionID, err := randomHexString(8)
	if err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return s.generateAccessToken(user, sessionID, sessionBindingHashFromContext(ctx))
}

// generateAccessToken 生成带会话 ID 与绑定指纹的 access token。
func (s *AuthService) generateAccessToken(user *User, sessionID, bindingHash string) (string, error) {
	now := time.Now()
	var expiresAt time.Time
	if s.cfg.JWT.AccessTokenExpireMinutes > 0 {
		expiresAt = now.Add(time.Duration(s.cfg.JWT.AccessTokenExpireMinutes) * time.Minute)
	} else {
		// 向后兼容：使用旧的expire_hour配置
		expiresAt = now.Add(time.Duration(s.cfg.JWT.ExpireHour) * time.Hour)
	}

	claims := &JWTClaims{
		UserID:       user.ID,
		Email:        user.Email,
		Role:         user.Role,
		TokenVersion: resolvedTokenVersion(user),
		SessionID:    sessionID,
		BindingHash:  bindingHash,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}

	return tokenString, nil
}

// GetAccessTokenExpiresIn 返回Access Token的有效期（秒）
// 用于前端设置刷新定时器
func (s *AuthService) GetAccessTokenExpiresIn() int {
	if s.cfg.JWT.AccessTokenExpireMinutes > 0 {
		return s.cfg.JWT.AccessTokenExpireMinutes * 60
	}
	return s.cfg.JWT.ExpireHour * 3600
}

// HashPassword 使用bcrypt加密密码
func (s *AuthService) HashPassword(password string) (string, error) {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hashedBytes), nil
}

// CheckPassword 验证密码是否匹配
func (s *AuthService) CheckPassword(password, hashedPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// RefreshToken 刷新token
func (s *AuthService) RefreshToken(ctx context.Context, oldTokenString string) (string, error) {
	// 验证旧token（即使过期也允许，用于刷新）
	claims, err := s.ValidateToken(oldTokenString)
	if err != nil && !errors.Is(err, ErrTokenExpired) {
		return "", err
	}

	// 获取最新的用户信息
	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			return "", ErrInvalidToken
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error refreshing token: %v", err)
		return "", ErrServiceUnavailable
	}

	// 检查用户状态
	if !user.IsActive() {
		return "", ErrUserNotActive
	}

	// Security: Check TokenVersion to prevent refreshing revoked tokens
	// This ensures tokens issued before a password change cannot be refreshed
	if claims.TokenVersion != resolvedTokenVersion(user) {
		return "", ErrTokenRevoked
	}

	// 会话绑定检查：指纹变化的旧 token 不允许换发新 token。
	if s.settingService != nil && s.settingService.IsSessionBindingEnabled(ctx) && claims.BindingHash != "" {
		if current := sessionBindingHashFromContext(ctx); current != "" && current != claims.BindingHash {
			_ = s.RevokeSessionFamily(ctx, claims.SessionID)
			return "", ErrSessionBindingMismatch
		}
	}

	// 生成新token
	return s.GenerateToken(ctx, user)
}

// ==================== Refresh Token Methods ====================

// TokenPair 包含Access Token和Refresh Token
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // Access Token有效期（秒）
}

// TokenPairWithUser extends TokenPair with user role for backend mode checks
type TokenPairWithUser struct {
	TokenPair
	UserRole string
}

// GenerateTokenPair 生成Access Token和Refresh Token对
// familyID: 可选的Token家族ID，用于Token轮转时保持家族关系
func (s *AuthService) GenerateTokenPair(ctx context.Context, user *User, familyID string) (*TokenPair, error) {
	// 检查 refreshTokenCache 是否可用
	if s.refreshTokenCache == nil {
		return nil, errors.New("refresh token cache not configured")
	}

	// 提前确定家族ID：作为 access token 的会话ID（sid），保证同一会话的
	// access/refresh token 可以互相关联（单会话撤销、step-up 授权绑定）。
	if familyID == "" {
		familyBytes := make([]byte, 16)
		if _, err := rand.Read(familyBytes); err != nil {
			return nil, fmt.Errorf("generate family id: %w", err)
		}
		familyID = hex.EncodeToString(familyBytes)
	}

	// 生成Access Token（携带会话ID与绑定指纹）
	accessToken, err := s.generateAccessToken(user, familyID, sessionBindingHashFromContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("generate access token: %w", err)
	}

	// 生成Refresh Token
	refreshToken, err := s.generateRefreshToken(ctx, user, familyID)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    s.GetAccessTokenExpiresIn(),
	}, nil
}

// generateRefreshToken 生成并存储Refresh Token
func (s *AuthService) generateRefreshToken(ctx context.Context, user *User, familyID string) (string, error) {
	// 生成随机Token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	rawToken := refreshTokenPrefix + hex.EncodeToString(tokenBytes)

	// 计算Token哈希（存储哈希而非原始Token）
	tokenHash := hashToken(rawToken)

	// 如果没有提供familyID，生成新的
	if familyID == "" {
		familyBytes := make([]byte, 16)
		if _, err := rand.Read(familyBytes); err != nil {
			return "", fmt.Errorf("generate family id: %w", err)
		}
		familyID = hex.EncodeToString(familyBytes)
	}

	now := time.Now()
	ttl := time.Duration(s.cfg.JWT.RefreshTokenExpireDays) * 24 * time.Hour

	data := &RefreshTokenData{
		UserID:       user.ID,
		TokenVersion: resolvedTokenVersion(user),
		FamilyID:     familyID,
		BindingHash:  sessionBindingHashFromContext(ctx),
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl),
	}

	// 存储Token数据
	if err := s.refreshTokenCache.StoreRefreshToken(ctx, tokenHash, data, ttl); err != nil {
		return "", fmt.Errorf("store refresh token: %w", err)
	}

	// 添加到用户Token集合
	if err := s.refreshTokenCache.AddToUserTokenSet(ctx, user.ID, tokenHash, ttl); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to add token to user set: %v", err)
		// 不影响主流程
	}

	// 添加到家族Token集合
	if err := s.refreshTokenCache.AddToFamilyTokenSet(ctx, familyID, tokenHash, ttl); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to add token to family set: %v", err)
		// 不影响主流程
	}

	return rawToken, nil
}

// RefreshTokenPair 使用Refresh Token刷新Token对
// 实现Token轮转：每次刷新都会生成新的Refresh Token，旧Token立即失效
func (s *AuthService) RefreshTokenPair(ctx context.Context, refreshToken string) (*TokenPairWithUser, error) {
	// 检查 refreshTokenCache 是否可用
	if s.refreshTokenCache == nil {
		return nil, ErrRefreshTokenInvalid
	}

	// 验证Token格式
	if !strings.HasPrefix(refreshToken, refreshTokenPrefix) {
		return nil, ErrRefreshTokenInvalid
	}

	tokenHash := hashToken(refreshToken)

	// 获取Token数据
	data, err := s.refreshTokenCache.GetRefreshToken(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			// Token不存在，可能是已被使用（Token轮转）或已过期
			logger.LegacyPrintf("service.auth", "[Auth] Refresh token not found, possible reuse attack")
			return nil, ErrRefreshTokenInvalid
		}
		logger.LegacyPrintf("service.auth", "[Auth] Error getting refresh token: %v", err)
		return nil, ErrServiceUnavailable
	}

	// 检查Token是否过期
	if time.Now().After(data.ExpiresAt) {
		// 删除过期Token
		_ = s.refreshTokenCache.DeleteRefreshToken(ctx, tokenHash)
		return nil, ErrRefreshTokenExpired
	}

	// 获取用户信息
	user, err := s.userRepo.GetByID(ctx, data.UserID)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// 用户已删除，撤销整个Token家族
			_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
			return nil, ErrRefreshTokenInvalid
		}
		logger.LegacyPrintf("service.auth", "[Auth] Database error getting user for token refresh: %v", err)
		return nil, ErrServiceUnavailable
	}

	// 检查用户状态
	if !user.IsActive() {
		// 用户被禁用，撤销整个Token家族
		_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
		return nil, ErrUserNotActive
	}
	if !s.CanAccessAdminPanel(ctx, user) {
		_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
		return nil, ErrAdminOnlyMode
	}

	// 检查TokenVersion（密码更改后所有Token失效）
	if data.TokenVersion != resolvedTokenVersion(user) {
		// TokenVersion不匹配，撤销整个Token家族
		_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
		return nil, ErrTokenRevoked
	}

	// 会话绑定检查：IP/UA 任一变化即撤销整个会话家族。
	// data.BindingHash 为空表示功能开启前签发的旧会话，放行并在轮转时补齐绑定。
	if s.settingService != nil && s.settingService.IsSessionBindingEnabled(ctx) && data.BindingHash != "" {
		if current := sessionBindingHashFromContext(ctx); current != "" && current != data.BindingHash {
			_ = s.refreshTokenCache.DeleteTokenFamily(ctx, data.FamilyID)
			logger.LegacyPrintf("service.auth", "[Auth] Session binding mismatch on refresh for user %d, family revoked", data.UserID)
			return nil, ErrSessionBindingMismatch
		}
	}

	// Token轮转：立即使旧Token失效
	if err := s.refreshTokenCache.DeleteRefreshToken(ctx, tokenHash); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to delete old refresh token: %v", err)
		// 继续处理，不影响主流程
	}

	// 生成新的Token对，保持同一个家族ID
	pair, err := s.GenerateTokenPair(ctx, user, data.FamilyID)
	if err != nil {
		return nil, err
	}
	return &TokenPairWithUser{
		TokenPair: *pair,
		UserRole:  user.Role,
	}, nil
}

// RevokeRefreshToken 撤销单个Refresh Token
func (s *AuthService) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	if s.refreshTokenCache == nil {
		return nil // No-op if cache not configured
	}
	if !strings.HasPrefix(refreshToken, refreshTokenPrefix) {
		return ErrRefreshTokenInvalid
	}

	tokenHash := hashToken(refreshToken)
	return s.refreshTokenCache.DeleteRefreshToken(ctx, tokenHash)
}

// RevokeSessionFamily 撤销单个会话家族（该会话的所有 refresh token）。
// 用于会话绑定失效等单会话级撤销场景，不影响用户的其他设备会话。
func (s *AuthService) RevokeSessionFamily(ctx context.Context, familyID string) error {
	if s.refreshTokenCache == nil || familyID == "" {
		return nil
	}
	return s.refreshTokenCache.DeleteTokenFamily(ctx, familyID)
}

// RevokeAllUserSessions 撤销用户的所有会话（所有Refresh Token）
// 用于密码更改或用户主动登出所有设备
func (s *AuthService) RevokeAllUserSessions(ctx context.Context, userID int64) error {
	if s.refreshTokenCache == nil {
		return nil // No-op if cache not configured
	}
	return s.refreshTokenCache.DeleteUserRefreshTokens(ctx, userID)
}

// RevokeAllUserTokens invalidates both stateless access tokens and refresh sessions.
//
// 注意：users 表没有 token_version 列（resolvedTokenVersion 由 email+password_hash
// 指纹推导），因此对 user.TokenVersion 自增只影响内存副本。之前紧跟其后的整行
// Update 不写任何有效数据，却会用旧快照覆盖并发写入的列，故已移除。
// 会话撤销由下面的 refresh session 清理承担；改密路径通过 password_hash 变化
// 改变指纹，从而使旧 token 失效。
func (s *AuthService) RevokeAllUserTokens(ctx context.Context, userID int64) error {
	if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	if err := s.RevokeAllUserSessions(ctx, userID); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to revoke refresh sessions after token invalidation for user %d: %v", userID, err)
	}
	return nil
}

// hashToken 计算Token的SHA256哈希
func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func resolvedTokenVersion(user *User) int64 {
	if user == nil {
		return 0
	}
	if user.TokenVersionResolved {
		return user.TokenVersion
	}

	material := strings.ToLower(strings.TrimSpace(user.Email)) + "\n" + user.PasswordHash
	sum := sha256.Sum256([]byte(material))
	fingerprint := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	return user.TokenVersion ^ fingerprint
}
