package admin

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// AccountOwnershipScope performs the request-level account ownership check for
// routes that carry an account ID. The account-admin scope is injected by the
// outer authentication middleware; super-admin and background requests remain
// unscoped and pass through unchanged.
//
// The check deliberately runs before the route handler so account-specific
// caches, mutations, and upstream calls cannot happen before authorization.
// Static routes and malformed/non-positive IDs are left to their handlers so
// they retain their existing 400/error behavior.
func (h *AccountHandler) AccountOwnershipScope() gin.HandlerFunc {
	return func(c *gin.Context) {
		ownerID, scoped := ctxkey.AccountAdminIDFromContext(c.Request.Context())
		if !scoped {
			c.Next()
			return
		}

		rawID := strings.TrimSpace(c.Param("id"))
		if rawID == "" {
			c.Next()
			return
		}
		accountID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil || accountID <= 0 {
			c.Next()
			return
		}

		if h == nil || h.adminService == nil {
			response.ErrorFrom(c, infraerrors.InternalServer(
				"ACCOUNT_OWNERSHIP_NOT_CONFIGURED",
				"account ownership service is not configured",
			))
			c.Abort()
			return
		}

		account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
		if err != nil {
			response.ErrorFrom(c, err)
			c.Abort()
			return
		}
		if account == nil || account.ID != accountID || account.AccountAdminID == nil || *account.AccountAdminID != ownerID {
			response.ErrorFrom(c, service.ErrAccountNotFound)
			c.Abort()
			return
		}

		c.Next()
	}
}

// requireExplicitAccountOwnership verifies a batch of account IDs before the
// caller performs any cache, Redis, mutation, or upstream work.  The account
// repository normally applies the same scope at query time, but keeping this
// exact-set check at the HTTP boundary is important: a scoped query can return
// only the visible subset, and proceeding with that subset would turn a mixed
// request into a partial operation.
//
// Super administrators deliberately remain unscoped and do not pay for an
// extra lookup here.  A missing or foreign ID is reported as the generic
// account-not-found error so the endpoint does not disclose whether the ID
// belongs to another administrator.
func (h *AccountHandler) requireExplicitAccountOwnership(c *gin.Context, ids []int64) bool {
	if err := checkExplicitAccountOwnership(c.Request.Context(), h.adminService, ids); err != nil {
		// Preserve a generic not-found boundary for a scoped lookup.  In
		// particular, do not turn an implementation-specific query error into
		// an account existence oracle for another administrator.
		if errors.Is(err, service.ErrAccountNotFound) {
			response.ErrorFrom(c, service.ErrAccountNotFound)
		} else {
			response.ErrorFrom(c, err)
		}
		c.Abort()
		return false
	}
	return true
}

// checkExplicitAccountOwnership validates an exact set of account IDs for a
// restricted account administrator. It is shared by account handlers and
// body-based actions in sibling handlers (for example OpenAI session cleanup)
// whose routes do not carry an :id path parameter. Unscoped callers are left
// unchanged, while a scoped lookup fails closed instead of silently dropping
// foreign IDs and partially applying a batch operation.
func checkExplicitAccountOwnership(ctx context.Context, adminService service.AdminService, ids []int64) error {
	ownerID, scoped := ctxkey.AccountAdminIDFromContext(ctx)
	if !scoped {
		return nil
	}

	requested := normalizeInt64IDList(ids)
	if len(requested) == 0 {
		return nil
	}
	if adminService == nil {
		return infraerrors.InternalServer(
			"ACCOUNT_OWNERSHIP_NOT_CONFIGURED",
			"account ownership service is not configured",
		)
	}

	accounts, err := adminService.GetAccountsByIDs(ctx, requested)
	if err != nil {
		return err
	}

	byID := make(map[int64]*service.Account, len(accounts))
	for _, account := range accounts {
		if account != nil {
			byID[account.ID] = account
		}
	}
	for _, accountID := range requested {
		account := byID[accountID]
		if account == nil || account.AccountAdminID == nil || *account.AccountAdminID != ownerID {
			return service.ErrAccountNotFound
		}
	}
	return nil
}

// accountOwnershipCacheScope returns a process-local cache namespace.  The
// owner ID is part of the key for restricted administrators so a snapshot
// generated for one request principal can never be reused across principals.
func accountOwnershipCacheScope(ctx context.Context) string {
	if ownerID, scoped := ctxkey.AccountAdminIDFromContext(ctx); scoped {
		return "owner:" + strconv.FormatInt(ownerID, 10)
	}
	return "global"
}
