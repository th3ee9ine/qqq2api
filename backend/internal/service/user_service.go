package service

import (
	"context"

	"fmt"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"

	"log/slog"

	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

var (
	ErrUserNotFound      = infraerrors.NotFound("USER_NOT_FOUND", "user not found")
	ErrPasswordIncorrect = infraerrors.BadRequest("PASSWORD_INCORRECT", "current password is incorrect")
	ErrBalanceNegative   = infraerrors.BadRequest("BALANCE_NEGATIVE", "balance cannot be negative")
	ErrInsufficientPerms = infraerrors.Forbidden("INSUFFICIENT_PERMISSIONS", "insufficient permissions")
)

const (
	userLastActiveMinTouch    = 10 * time.Minute
	userLastActiveFailBackoff = 30 * time.Second
)

// UserListFilters contains all filter options for listing users
type UserListFilters struct {
	Status    string // User status filter
	Role      string // User role filter
	Search    string // Search in email, username
	GroupName string // Filter by allowed group name (fuzzy match)
	// APIKeyGroupID filters users who own at least one non-soft-deleted API key
	// bound to this group (api_keys.group_id). 0 = no filter. Covers all three
	// group types since it matches the key's group directly, not allowed_groups.
	APIKeyGroupID int64
	// IncludeSubscriptions controls whether ListWithFilters should load active subscriptions.
	// For large datasets this can be expensive; admin list pages should enable it on demand.
	// nil means not specified (default: load subscriptions for backward compatibility).
	IncludeSubscriptions *bool
	// IncludeDeleted 为 true 时绕过软删除过滤，返回含已删除（deleted_at 非空）的用户。
	// 仅供 /admin/usage 的 SearchUsers 端点使用，其他列表调用方不要设置。
	IncludeDeleted bool
}

// UserUpdateFields 声明 UserRepository.Update 允许写回的列。
//
// 未声明的列保持数据库当前值，不会被调用方手里的快照覆盖。用户行上有多条
// 不经过 Update 的原子写入路径（DeductBalance/UpdateBalance 扣加余额、
// UpdateConcurrency、BatchUpdateLimits、UpdateUserLastActiveAt 等），
// status/role 也可能被其他流程并发改写。若 Update 无条件整行回写，
// 一次"读-改-写"就会静默回滚这些并发结果（lost update），
// 因此每个调用方必须显式声明它真正要改的列。
//
// 注意这里没有 balance / total_recharged：余额只能经由 AdjustBalance、
// SetBalance、UpdateBalance、DeductBalance 等原子接口修改，Update 永远不碰它们。
type UserUpdateFields struct {
	Email        bool
	Username     bool
	Notes        bool
	PasswordHash bool
	Role         bool
	Status       bool
	Concurrency  bool
	RPMLimit     bool
	// SupplyRateMultiplier 覆盖账号管理员供货收益倍率。
	SupplyRateMultiplier bool
	SignupSource         bool
	LastLoginAt          bool
	LastActiveAt         bool
	// BalanceNotifySettings 覆盖 balance_notify_enabled / _threshold_type / _threshold。
	BalanceNotifySettings bool
	// BalanceNotifyExtraEmails 与上一项分开，避免"改通知阈值"覆盖并发的"加通知邮箱"。
	BalanceNotifyExtraEmails bool
	// AllowedGroups 为 true 时才同步 user_allowed_groups 关联表。
	AllowedGroups bool
	// RestrictPublicGroups 覆盖 restrict_public_groups 列。
	RestrictPublicGroups bool
}

// BalanceChange 记录一次余额变更前后的值。
type BalanceChange struct {
	Old float64
	New float64
}

// IsEmpty 报告该次 Update 是否不写任何列（此时仓储直接返回，不产生写操作）。
func (f UserUpdateFields) IsEmpty() bool {
	return f == UserUpdateFields{}
}

type UserRepository interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id int64) (*User, error)
	// GetByIDIncludeDeleted 绕过软删除过滤按 ID 取用户（含已删）。仅供管理员审计/usage 点击使用。
	GetByIDIncludeDeleted(ctx context.Context, id int64) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetFirstAdmin(ctx context.Context) (*User, error)
	// Update 只写 fields 中显式声明的列，其余列保持库中当前值。
	Update(ctx context.Context, user *User, fields UserUpdateFields) error
	Delete(ctx context.Context, id int64) error

	List(ctx context.Context, params pagination.PaginationParams) ([]User, *pagination.PaginationResult, error)
	ListWithFilters(ctx context.Context, params pagination.PaginationParams, filters UserListFilters) ([]User, *pagination.PaginationResult, error)
	GetLatestUsedAtByUserIDs(ctx context.Context, userIDs []int64) (map[int64]*time.Time, error)
	GetLatestUsedAtByUserID(ctx context.Context, userID int64) (*time.Time, error)
	UpdateUserLastActiveAt(ctx context.Context, userID int64, activeAt time.Time) error

	UpdateBalance(ctx context.Context, id int64, amount float64) error
	DeductBalance(ctx context.Context, id int64, amount float64) error
	// AdjustBalance 原子地把 delta 累加到余额上，并返回变更前后的值。结果为负时
	// 拒绝写入并返回 ErrBalanceNegative。管理员的加/扣款必须走这里而不是
	// "读余额→算新值→整行写回"，否则并发的计费扣款会被旧快照抹掉。
	AdjustBalance(ctx context.Context, id int64, delta float64) (BalanceChange, error)
	// SetBalance 原子地把余额置为 value（value 必须 >= 0），返回变更前后的值。
	SetBalance(ctx context.Context, id int64, value float64) (BalanceChange, error)
	UpdateConcurrency(ctx context.Context, id int64, amount int) error
	BatchSetConcurrency(ctx context.Context, userIDs []int64, value int) (int, error)
	BatchAddConcurrency(ctx context.Context, userIDs []int64, delta int) (int, error)
	BatchUpdateLimits(ctx context.Context, userIDs []int64, concurrency, rpmLimit *int) (int, error)
	ExistsByEmail(ctx context.Context, email string) (bool, error)
	RemoveGroupFromAllowedGroups(ctx context.Context, groupID int64) (int64, error)
	// AddGroupToAllowedGroups 将指定分组增量添加到用户的 allowed_groups（幂等，冲突忽略）
	AddGroupToAllowedGroups(ctx context.Context, userID int64, groupID int64) error
	// RemoveGroupFromUserAllowedGroups 移除单个用户的指定分组权限
	RemoveGroupFromUserAllowedGroups(ctx context.Context, userID int64, groupID int64) error

	// TOTP 双因素认证
	UpdateTotpSecret(ctx context.Context, userID int64, encryptedSecret *string) error
	EnableTotp(ctx context.Context, userID int64) error
	DisableTotp(ctx context.Context, userID int64) error
}

type userAdminTxRunner interface {
	WithUserAdminTx(ctx context.Context, fn func(txCtx context.Context) error) error
}

// UserService 用户服务
type UserService struct {
	userRepo             UserRepository
	settingRepo          SettingRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCache         BillingCache
	lastActiveTouchL1    sync.Map
	lastActiveTouchSF    singleflight.Group
}

// NewUserService 创建用户服务实例
func NewUserService(userRepo UserRepository, settingRepo SettingRepository, authCacheInvalidator APIKeyAuthCacheInvalidator, billingCache BillingCache) *UserService {
	return &UserService{
		userRepo:             userRepo,
		settingRepo:          settingRepo,
		authCacheInvalidator: authCacheInvalidator,
		billingCache:         billingCache,
	}
}

// GetFirstAdmin 获取首个管理员用户（用于 Admin API Key 认证）
func (s *UserService) GetFirstAdmin(ctx context.Context) (*User, error) {
	admin, err := s.userRepo.GetFirstAdmin(ctx)
	if err != nil {
		return nil, fmt.Errorf("get first admin: %w", err)
	}
	return admin, nil
}

// GetByEmail resolves the configured administrator row for authentication
// middleware. It intentionally does not fall back to the first role=admin row.
func (s *UserService) GetByEmail(ctx context.Context, email string) (*User, error) {
	if s == nil || s.userRepo == nil {
		return nil, ErrUserNotFound
	}
	return s.userRepo.GetByEmail(ctx, strings.TrimSpace(email))
}

// GetByID 根据ID获取用户（管理员功能）
func (s *UserService) GetByID(ctx context.Context, id int64) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	normalizeLoadedUserTokenVersion(user)
	return user, nil
}

func normalizeLoadedUserTokenVersion(user *User) {
	if user == nil || user.TokenVersionResolved {
		return
	}
	user.TokenVersion = resolvedTokenVersion(user)
	user.TokenVersionResolved = true
}

// TouchLastActive 通过防抖更新 users.last_active_at，减少鉴权热路径写放大。
// 该操作为尽力而为，不应中断正常请求。
func (s *UserService) TouchLastActive(ctx context.Context, userID int64) {
	if s == nil || s.userRepo == nil || userID <= 0 {
		return
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		slog.Debug("skip touch user last active after load failure", "user_id", userID, "error", err)
		return
	}
	s.TouchLastActiveForUser(ctx, user)
}

// TouchLastActiveForUser 使用已加载的用户信息更新 last_active_at，避免重复读取数据库。
func (s *UserService) TouchLastActiveForUser(ctx context.Context, user *User) {
	if s == nil || s.userRepo == nil || user == nil || user.ID <= 0 {
		return
	}

	now := time.Now()
	if userLastActiveFresh(user.LastActiveAt, now) {
		return
	}
	if v, ok := s.lastActiveTouchL1.Load(user.ID); ok {
		if nextAllowedAt, ok := v.(time.Time); ok && now.Before(nextAllowedAt) {
			return
		}
	}

	_, err, _ := s.lastActiveTouchSF.Do(strconv.FormatInt(user.ID, 10), func() (any, error) {
		latest := time.Now()
		if v, ok := s.lastActiveTouchL1.Load(user.ID); ok {
			if nextAllowedAt, ok := v.(time.Time); ok && latest.Before(nextAllowedAt) {
				return nil, nil
			}
		}
		if userLastActiveFresh(user.LastActiveAt, latest) {
			return nil, nil
		}
		if err := s.userRepo.UpdateUserLastActiveAt(ctx, user.ID, latest); err != nil {
			s.lastActiveTouchL1.Store(user.ID, latest.Add(userLastActiveFailBackoff))
			return nil, fmt.Errorf("touch user last active: %w", err)
		}
		s.lastActiveTouchL1.Store(user.ID, latest.Add(userLastActiveMinTouch))
		return nil, nil
	})
	if err != nil {
		slog.Warn("touch user last active failed", "user_id", user.ID, "error", err)
	}
}

func userLastActiveFresh(lastActiveAt *time.Time, now time.Time) bool {
	if lastActiveAt == nil {
		return false
	}
	return now.Before(lastActiveAt.Add(userLastActiveMinTouch))
}

// UpdateStatus 更新用户状态（管理员功能）
func (s *UserService) UpdateStatus(ctx context.Context, userID int64, status string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}

	user.Status = status

	if err := s.userRepo.Update(ctx, user, UserUpdateFields{Status: true}); err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}

	return nil
}
