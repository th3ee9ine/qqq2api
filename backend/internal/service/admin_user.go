package service

import (
	"context"

	"errors"
	"fmt"
	"math"

	"strings"

	dbent "github.com/th3ee9ine/qqq2api/ent"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/logger"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

// User management implementations
func (s *adminServiceImpl) ListUsers(ctx context.Context, page, pageSize int, filters UserListFilters, sortBy, sortOrder string) ([]User, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder}
	users, result, err := s.userRepo.ListWithFilters(ctx, params, filters)
	if err != nil {
		return nil, 0, err
	}
	if len(users) > 0 {
		userIDs := make([]int64, 0, len(users))
		for i := range users {
			userIDs = append(userIDs, users[i].ID)
		}
		lastUsedByUserID, latestErr := s.userRepo.GetLatestUsedAtByUserIDs(ctx, userIDs)
		if latestErr != nil {
			logger.LegacyPrintf("service.admin", "failed to load user last_used_at in batch: err=%v", latestErr)
		} else {
			for i := range users {
				users[i].LastUsedAt = lastUsedByUserID[users[i].ID]
			}
		}
	}
	// 批量加载用户专属分组倍率
	if s.userGroupRateRepo != nil && len(users) > 0 {
		if batchRepo, ok := s.userGroupRateRepo.(userGroupRateBatchReader); ok {
			userIDs := make([]int64, 0, len(users))
			for i := range users {
				userIDs = append(userIDs, users[i].ID)
			}
			ratesByUser, err := batchRepo.GetByUserIDs(ctx, userIDs)
			if err != nil {
				logger.LegacyPrintf("service.admin", "failed to load user group rates in batch: err=%v", err)
				s.loadUserGroupRatesOneByOne(ctx, users)
			} else {
				for i := range users {
					if rates, ok := ratesByUser[users[i].ID]; ok {
						users[i].GroupRates = rates
					}
				}
			}
		} else {
			s.loadUserGroupRatesOneByOne(ctx, users)
		}
	}
	return users, result.Total, nil
}

func (s *adminServiceImpl) loadUserGroupRatesOneByOne(ctx context.Context, users []User) {
	if s.userGroupRateRepo == nil {
		return
	}
	for i := range users {
		rates, err := s.userGroupRateRepo.GetByUserID(ctx, users[i].ID)
		if err != nil {
			logger.LegacyPrintf("service.admin", "failed to load user group rates: user_id=%d err=%v", users[i].ID, err)
			continue
		}
		users[i].GroupRates = rates
	}
}

func (s *adminServiceImpl) GetUser(ctx context.Context, id int64) (*User, error) {
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	lastUsedAt, latestErr := s.userRepo.GetLatestUsedAtByUserID(ctx, id)
	if latestErr != nil {
		logger.LegacyPrintf("service.admin", "failed to load user last_used_at: user_id=%d err=%v", id, latestErr)
	} else {
		user.LastUsedAt = lastUsedAt
	}
	// 加载用户专属分组倍率
	if s.userGroupRateRepo != nil {
		rates, err := s.userGroupRateRepo.GetByUserID(ctx, id)
		if err != nil {
			logger.LegacyPrintf("service.admin", "failed to load user group rates: user_id=%d err=%v", id, err)
		} else {
			user.GroupRates = rates
		}
	}
	return user, nil
}

func (s *adminServiceImpl) GetUserIncludeDeleted(ctx context.Context, id int64) (*User, error) {
	return s.userRepo.GetByIDIncludeDeleted(ctx, id)
}

// normalizeUserRole 校验并归一化角色输入。
// 空字符串返回 fallback(未提供时的默认角色);非法值返回错误。
func normalizeUserRole(role, fallback string) (string, error) {
	if role == "" {
		return fallback, nil
	}
	if role != RoleAdmin && role != RoleAccountAdmin && role != RoleUser {
		return "", fmt.Errorf("invalid role: %q (must be %s, %s or %s)", role, RoleAdmin, RoleAccountAdmin, RoleUser)
	}
	return role, nil
}

func validateSupplyRateMultiplier(multiplier float64) error {
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return infraerrors.BadRequest(
			"INVALID_SUPPLY_RATE_MULTIPLIER",
			"supply_rate_multiplier must be a finite non-negative number",
		)
	}
	return nil
}

func (s *adminServiceImpl) CreateUser(ctx context.Context, input *CreateUserInput) (*User, error) {
	balance := 0.0
	if input.Balance != nil {
		balance = *input.Balance
	}

	// 角色可由超级管理员在创建时指定；未提供时默认 user。
	role, err := normalizeUserRole(input.Role, RoleUser)
	if err != nil {
		return nil, err
	}
	supplyRateMultiplier := 1.0
	if input.SupplyRateMultiplier != nil {
		if role != RoleAccountAdmin {
			return nil, infraerrors.BadRequest("INVALID_SUPPLY_RATE_MULTIPLIER", "supply_rate_multiplier is only valid for account administrators")
		}
		if err := validateSupplyRateMultiplier(*input.SupplyRateMultiplier); err != nil {
			return nil, err
		}
		supplyRateMultiplier = *input.SupplyRateMultiplier
	}

	user := &User{
		Email:                input.Email,
		Username:             input.Username,
		Notes:                input.Notes,
		Role:                 role,
		Balance:              balance,
		Concurrency:          input.Concurrency,
		RPMLimit:             input.RPMLimit,
		Status:               StatusActive,
		AllowedGroups:        input.AllowedGroups,
		SupplyRateMultiplier: supplyRateMultiplier,

		RestrictPublicGroups: input.RestrictPublicGroups,
	}
	if err := user.SetPassword(input.Password); err != nil {
		return nil, err
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}
	// 创建管理员属权限敏感操作，落审计日志（含操作者），便于事后追溯。
	if user.IsPanelOperator() {
		logger.LegacyPrintf("service.admin", "audit: panel operator created actor_admin_id=%d target_user_id=%d role=%s",
			input.ActorAdminID, user.ID, user.Role)
	}
	return user, nil
}

// ensureNotLastAdmin 降级管理员前确认系统中仍存在其他管理员，防止零 admin 锁死。
// 注：读取与写入之间存在竞态窗口，极端并发下仍可能双双降级；作为后台低频操作
// 的兜底保护足够，彻底防护需依赖数据库层约束。
func (s *adminServiceImpl) ensureNotLastAdmin(ctx context.Context) error {
	noSubs := false
	_, result, err := s.userRepo.ListWithFilters(ctx,
		pagination.PaginationParams{Page: 1, PageSize: 1},
		UserListFilters{Role: RoleAdmin, IncludeSubscriptions: &noSubs},
	)
	if err != nil {
		return fmt.Errorf("count admin users: %w", err)
	}
	if result == nil || result.Total <= 1 {
		return errors.New("cannot demote the last admin user")
	}
	return nil
}

func (s *adminServiceImpl) UpdateUser(ctx context.Context, id int64, input *UpdateUserInput) (*User, error) {
	// 校验用户专属分组倍率：必须 > 0（nil 合法，表示清除专属倍率）
	if input.GroupRates != nil {
		for groupID, rate := range input.GroupRates {
			if rate != nil && *rate <= 0 {
				return nil, fmt.Errorf("rate_multiplier must be > 0 (group_id=%d)", groupID)
			}
		}
	}

	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Protect admin users: cannot disable admin accounts
	if user.Role == "admin" && input.Status == "disabled" {
		return nil, errors.New("cannot disable admin user")
	}

	oldConcurrency := user.Concurrency
	oldStatus := user.Status
	oldRole := user.Role
	oldRPMLimit := user.RPMLimit
	oldSupplyRateMultiplier := user.SupplyRateMultiplier
	oldAllowedGroups := append([]int64(nil), user.AllowedGroups...)

	// fields 与下面的 input.X 判空条件一一对应：管理员没提交的列不写回，
	// 避免这份快照回滚并发的扣费、状态变更或批量限额调整。
	var fields UserUpdateFields

	if input.Email != "" {
		user.Email = input.Email
		fields.Email = true
	}
	if input.Password != "" {
		if err := user.SetPassword(input.Password); err != nil {
			return nil, err
		}
		fields.PasswordHash = true
	}

	if input.Username != nil {
		user.Username = *input.Username
		fields.Username = true
	}
	if input.Notes != nil {
		user.Notes = *input.Notes
		fields.Notes = true
	}

	if input.Status != "" {
		user.Status = input.Status
		fields.Status = true
	}

	// 角色变更；空字符串表示不修改。
	if input.Role != "" {
		role, err := normalizeUserRole(input.Role, user.Role)
		if err != nil {
			return nil, err
		}
		// 防锁死保护：不允许降级系统中最后一个管理员（自我降级已在 handler 层拦截，
		// 此处兜底覆盖跨管理员互降导致零 admin 的场景）。
		if user.Role == RoleAdmin && role != RoleAdmin {
			if err := s.ensureNotLastAdmin(ctx); err != nil {
				return nil, err
			}
		}
		user.Role = role
		fields.Role = true
	}

	if input.Concurrency != nil {
		user.Concurrency = *input.Concurrency
		fields.Concurrency = true
	}

	if input.RPMLimit != nil {
		user.RPMLimit = *input.RPMLimit
		fields.RPMLimit = true
	}

	if input.AllowedGroups != nil {
		user.AllowedGroups = *input.AllowedGroups
		fields.AllowedGroups = true
	}

	oldRestrictPublicGroups := user.RestrictPublicGroups
	if input.RestrictPublicGroups != nil {
		user.RestrictPublicGroups = *input.RestrictPublicGroups
		fields.RestrictPublicGroups = true
	}

	// A soft-deleted account administrator still has a live users row, so the
	// accounts foreign key cannot clear ownership for a role demotion. Reset the
	// user-level payout multiplier as part of the same write; the owned account
	// rows are detached atomically below. Disabling an administrator only blocks
	// panel access: retain ownership so re-enabling restores their accounts and
	// earnings history without a lossy reassignment.
	releaseAccountAdminOwnership := oldRole == RoleAccountAdmin && user.Role != RoleAccountAdmin
	if releaseAccountAdminOwnership {
		user.SupplyRateMultiplier = 1.0
		fields.SupplyRateMultiplier = true
	}

	if input.SupplyRateMultiplier != nil {
		if user.Role != RoleAccountAdmin {
			return nil, infraerrors.BadRequest("INVALID_SUPPLY_RATE_MULTIPLIER", "supply_rate_multiplier is only valid for account administrators")
		}
		if err := validateSupplyRateMultiplier(*input.SupplyRateMultiplier); err != nil {
			return nil, err
		}
		user.SupplyRateMultiplier = *input.SupplyRateMultiplier
		fields.SupplyRateMultiplier = true
	}

	supplyRateChanged := input.SupplyRateMultiplier != nil && user.SupplyRateMultiplier != oldSupplyRateMultiplier
	if releaseAccountAdminOwnership {
		ownershipRepo, hasOwnershipRepo := s.accountRepo.(AccountAdminOwnershipRepository)
		if s.accountRepo != nil && !hasOwnershipRepo {
			return nil, errors.New("account repository does not support account administrator ownership release")
		}
		if !hasOwnershipRepo {
			// Narrow service unit doubles may omit account storage entirely. There
			// are no account rows to release in that configuration.
			if err := s.userRepo.Update(ctx, user, fields); err != nil {
				return nil, err
			}
		} else {
			txRunner, ok := s.userRepo.(userAdminTxRunner)
			if !ok {
				return nil, errors.New("user repository does not support atomic account administrator ownership release")
			}
			var releasedAccountIDs []int64
			if err := txRunner.WithUserAdminTx(ctx, func(txCtx context.Context) error {
				if err := s.userRepo.Update(txCtx, user, fields); err != nil {
					return err
				}
				var err error
				releasedAccountIDs, err = ownershipRepo.ReleaseAccountAdminOwnership(txCtx, user.ID)
				return err
			}); err != nil {
				return nil, err
			}
			ownershipRepo.RefreshSchedulerAccountSnapshots(ctx, releasedAccountIDs)
		}
	} else if supplyRateChanged {
		txRunner, ok := s.userRepo.(userAdminTxRunner)
		if !ok {
			return nil, errors.New("user repository does not support atomic account administrator rate updates")
		}
		supplyRepo, ok := s.accountRepo.(AccountAdminSupplyRateRepository)
		if !ok {
			return nil, errors.New("account repository does not support account administrator rate updates")
		}
		var changedAccountIDs []int64
		if err := txRunner.WithUserAdminTx(ctx, func(txCtx context.Context) error {
			if err := s.userRepo.Update(txCtx, user, fields); err != nil {
				return err
			}
			var err error
			changedAccountIDs, err = supplyRepo.UpdateSupplyRateMultiplierByAccountAdmin(txCtx, user.ID, user.SupplyRateMultiplier)
			return err
		}); err != nil {
			return nil, err
		}
		supplyRepo.RefreshSchedulerAccountSnapshots(ctx, changedAccountIDs)
	} else if err := s.userRepo.Update(ctx, user, fields); err != nil {
		return nil, err
	}

	// 角色变更属权限敏感操作，落审计日志（含操作者），便于事后追溯。
	if user.Role != oldRole {
		logger.LegacyPrintf("service.admin", "audit: user role changed actor_admin_id=%d target_user_id=%d old_role=%s new_role=%s",
			input.ActorAdminID, user.ID, oldRole, user.Role)
	}

	// 同步用户专属分组倍率
	if input.GroupRates != nil && s.userGroupRateRepo != nil {
		if err := s.userGroupRateRepo.SyncUserGroupRates(ctx, user.ID, input.GroupRates); err != nil {
			logger.LegacyPrintf("service.admin", "failed to sync user group rates: user_id=%d err=%v", user.ID, err)
		}
	}

	if s.authCacheInvalidator != nil {
		// RPMLimit 直接参与 billing_cache_service.checkRPM 的三级级联，
		// allowed_groups 参与 API Key 专属分组授权判断；不失效缓存会让修改在一个 L2 TTL 内失去效果。
		if user.Concurrency != oldConcurrency || user.Status != oldStatus || user.Role != oldRole || user.RPMLimit != oldRPMLimit || user.RestrictPublicGroups != oldRestrictPublicGroups || !sameInt64Set(user.AllowedGroups, oldAllowedGroups) {
			s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, user.ID)
		}
	}

	return user, nil
}

func sameInt64Set(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	counts := make(map[int64]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		if counts[v] == 0 {
			return false
		}
		counts[v]--
	}
	return true
}

func (s *adminServiceImpl) DeleteUser(ctx context.Context, id int64) error {
	// Protect admin users: cannot delete admin accounts
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if user.Role == "admin" {
		return errors.New("cannot delete admin user")
	}
	var ownershipRepo AccountAdminOwnershipRepository
	if user.Role == RoleAccountAdmin && s.accountRepo != nil {
		var ok bool
		ownershipRepo, ok = s.accountRepo.(AccountAdminOwnershipRepository)
		if !ok {
			return errors.New("account repository does not support account administrator ownership release")
		}
	}

	apiKeys, err := s.listUserAPIKeysForDeletion(ctx, id)
	if err != nil {
		return err
	}

	var releasedAccountIDs []int64
	if s.entClient != nil {
		tx, err := s.entClient.Tx(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()

		opCtx := dbent.NewTxContext(ctx, tx)
		if ownershipRepo != nil {
			releasedAccountIDs, err = ownershipRepo.ReleaseAccountAdminOwnership(opCtx, id)
			if err != nil {
				return err
			}
		}
		if err := s.deleteUserWithAPIKeys(opCtx, id, apiKeys); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	} else {
		if ownershipRepo != nil {
			releasedAccountIDs, err = ownershipRepo.ReleaseAccountAdminOwnership(ctx, id)
			if err != nil {
				return err
			}
		}
		if err := s.deleteUserWithAPIKeys(ctx, id, apiKeys); err != nil {
			return err
		}
	}
	if ownershipRepo != nil {
		ownershipRepo.RefreshSchedulerAccountSnapshots(ctx, releasedAccountIDs)
	}

	if s.authCacheInvalidator != nil {
		for _, key := range apiKeys {
			if keyValue := strings.TrimSpace(key.Key); keyValue != "" {
				s.authCacheInvalidator.InvalidateAuthCacheByKey(ctx, keyValue)
			}
		}
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, id)
	}
	return nil
}

func (s *adminServiceImpl) listUserAPIKeysForDeletion(ctx context.Context, userID int64) ([]APIKey, error) {
	if s.apiKeyRepo == nil {
		return nil, nil
	}

	const pageSize = 1000
	keys := make([]APIKey, 0)
	for page := 1; ; page++ {
		batch, result, err := s.apiKeyRepo.ListByUserID(ctx, userID, pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortBy:    "id",
			SortOrder: pagination.SortOrderAsc,
		}, APIKeyListFilters{})
		if err != nil {
			return nil, fmt.Errorf("list user api keys: %w", err)
		}
		keys = append(keys, batch...)
		if len(batch) == 0 || len(batch) < pageSize || result == nil || int64(len(keys)) >= result.Total {
			break
		}
	}
	return keys, nil
}

func (s *adminServiceImpl) deleteUserWithAPIKeys(ctx context.Context, userID int64, apiKeys []APIKey) error {
	if s.apiKeyRepo != nil {
		for _, key := range apiKeys {
			if key.ID <= 0 {
				continue
			}
			if err := s.apiKeyRepo.DeleteWithAudit(ctx, key.ID); err != nil {
				logger.LegacyPrintf("service.admin", "delete user api key failed: user_id=%d api_key_id=%d err=%v", userID, key.ID, err)
				return fmt.Errorf("delete user api key %d: %w", key.ID, err)
			}
		}
	}

	if err := s.userRepo.Delete(ctx, userID); err != nil {
		logger.LegacyPrintf("service.admin", "delete user failed: user_id=%d err=%v", userID, err)
		return err
	}
	return nil
}
