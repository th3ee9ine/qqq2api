package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/model"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// Account administrators receive only the lookup fields required by account
// create/edit forms. Internal pricing, routing, provider quota, proxy binding,
// and TLS fingerprint details remain exclusive to the super administrator.
type accountAdminGroupOption struct {
	ID                        int64   `json:"id"`
	Name                      string  `json:"name"`
	Description               string  `json:"description"`
	Platform                  string  `json:"platform"`
	RateMultiplier            float64 `json:"rate_multiplier"`
	Status                    string  `json:"status"`
	SubscriptionType          string  `json:"subscription_type"`
	LongContextPricingEnabled bool    `json:"long_context_pricing_enabled"`
	AccountCount              int64   `json:"account_count,omitempty"`
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
			RateMultiplier:            group.RateMultiplier,
			Status:                    group.Status,
			SubscriptionType:          group.SubscriptionType,
			LongContextPricingEnabled: group.LongContextPricingEnabled,
			AccountCount:              group.AccountCount,
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
