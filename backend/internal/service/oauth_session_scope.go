package service

import (
	"context"

	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

// accountAdminOAuthSessionOwner returns the authenticated restricted account
// administrator, if this request is running on the account-admin surface.
// Super-administrator and background jobs deliberately remain unscoped.
func accountAdminOAuthSessionOwner(ctx context.Context) int64 {
	ownerID, scoped := ctxkey.AccountAdminIDFromContext(ctx)
	if !scoped {
		return 0
	}
	return ownerID
}

// authorizeAccountAdminOAuthSession prevents a restricted account
// administrator from exchanging another administrator's (or a
// super-administrator's) pending OAuth session. The session owner is zero for
// legacy/background sessions; those are only usable from an unscoped context.
func authorizeAccountAdminOAuthSession(ctx context.Context, sessionOwnerID int64) error {
	ownerID, scoped := ctxkey.AccountAdminIDFromContext(ctx)
	if !scoped {
		return nil
	}
	if sessionOwnerID <= 0 || sessionOwnerID != ownerID {
		return infraerrors.Forbidden(
			"OAUTH_SESSION_SCOPE",
			"OAuth session belongs to a different administrator",
		)
	}
	return nil
}
