package admin

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/handler/dto"
	"github.com/th3ee9ine/qqq2api/internal/model"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// Account administrators receive only the lookup fields required by account
// create/edit forms. Internal pricing, routing, provider quota, proxy binding,
// and TLS fingerprint details remain exclusive to the super administrator.
type accountAdminGroupOption struct {
	ID                        int64  `json:"id"`
	Name                      string `json:"name"`
	Description               string `json:"description"`
	Platform                  string `json:"platform"`
	Status                    string `json:"status"`
	SubscriptionType          string `json:"subscription_type"`
	LongContextPricingEnabled bool   `json:"long_context_pricing_enabled"`
}

type accountAdminTLSProfileOption struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type accountAdminWebSearchProviderOption struct {
	Type string `json:"type"`
}

type accountAdminWebSearchAvailability struct {
	Enabled   bool                                  `json:"enabled"`
	Providers []accountAdminWebSearchProviderOption `json:"providers"`
}

func isAccountAdminRequest(c *gin.Context) bool {
	role, ok := middleware.GetUserRoleFromContext(c)
	return ok && role == service.RoleAccountAdmin
}

func accountAdminGroupOptions(groups []service.Group) []accountAdminGroupOption {
	out := make([]accountAdminGroupOption, 0, len(groups))
	for i := range groups {
		group := &groups[i]
		out = append(out, accountAdminGroupOption{
			ID:                        group.ID,
			Name:                      group.Name,
			Description:               group.Description,
			Platform:                  group.Platform,
			Status:                    group.Status,
			SubscriptionType:          group.SubscriptionType,
			LongContextPricingEnabled: group.LongContextPricingEnabled,
		})
	}
	return out
}

func accountAdminTLSProfileOptions(profiles []*model.TLSFingerprintProfile) []accountAdminTLSProfileOption {
	out := make([]accountAdminTLSProfileOption, 0, len(profiles))
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		out = append(out, accountAdminTLSProfileOption{ID: profile.ID, Name: profile.Name})
	}
	return out
}

func accountAdminWebSearchConfig(cfg *service.WebSearchEmulationConfig) accountAdminWebSearchAvailability {
	out := accountAdminWebSearchAvailability{Providers: []accountAdminWebSearchProviderOption{}}
	if cfg == nil {
		return out
	}
	out.Enabled = cfg.Enabled
	for _, provider := range cfg.Providers {
		out.Providers = append(out.Providers, accountAdminWebSearchProviderOption{Type: provider.Type})
	}
	return out
}

func redactAccountAdminPricingExtra(ctx context.Context, extra map[string]any) map[string]any {
	// Keep list/detail and export aligned: neither response may disclose
	// provider billing observations or the managed probe policy. The export
	// projection copies the map, preserving a cached super-admin snapshot.
	return accountExportExtraForContext(ctx, extra)
}

// accountAdminAccountResponse removes internal group pricing and remote billing
// discovery state from the restricted account-manager view. Group IDs and
// account-group priorities remain available for the create/edit workflow.
func accountAdminAccountResponse(ctx context.Context, account *dto.Account) *dto.Account {
	if _, scoped := ctxkey.AccountAdminIDFromContext(ctx); !scoped || account == nil {
		return account
	}
	out := *account
	out.Extra = redactAccountAdminPricingExtra(ctx, account.Extra)
	out.Groups = nil
	if len(account.AccountGroups) > 0 {
		out.AccountGroups = append([]dto.AccountGroup(nil), account.AccountGroups...)
		for i := range out.AccountGroups {
			out.AccountGroups[i].Account = nil
			out.AccountGroups[i].Group = nil
		}
	}
	return &out
}
