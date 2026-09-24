package admin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/handler/dto"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestAccountAdminAccountResponseOmitsPricingGraphsAndProbeSnapshot(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.AccountAdminID, int64(41))
	account := &dto.Account{
		ID:             9,
		RateMultiplier: 0.75,
		GroupIDs:       []int64{3},
		Extra: map[string]any{
			"harmless_setting":                             true,
			service.UpstreamBillingProbeExtraKey:           map[string]any{"group_rate_multiplier": 8.5},
			service.UpstreamBillingProbeEnabledExtraKey:    true,
			service.UpstreamBillingRateSyncEnabledExtraKey: true,
			"grok_billing_snapshot":                        map[string]any{"used_cents": 1000},
			"grok_usage_snapshot":                          map[string]any{"monthly_used": 100},
		},
		Groups: []*dto.Group{{ID: 3, RateMultiplier: 8.5}},
		AccountGroups: []dto.AccountGroup{{
			AccountID: 9,
			GroupID:   3,
			Priority:  2,
			Account:   &dto.Account{ID: 9},
			Group:     &dto.Group{ID: 3, RateMultiplier: 8.5},
		}},
	}

	view := accountAdminAccountResponse(ctx, account)
	payload, err := json.Marshal(view)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"rate_multiplier":0.75`)
	require.Contains(t, string(payload), `"group_ids":[3]`)
	require.Contains(t, string(payload), `"priority":2`)
	require.Contains(t, string(payload), `"harmless_setting":true`)
	require.NotContains(t, string(payload), "upstream_billing")
	require.NotContains(t, string(payload), "grok_billing_snapshot")
	require.NotContains(t, string(payload), "grok_usage_snapshot")
	require.NotContains(t, string(payload), `"groups"`)
	require.NotContains(t, string(payload), `"group"`)
	// The restricted projection must not mutate the source snapshot. A later
	// super-administrator response may reuse the same account object.
	require.Contains(t, account.Extra, service.UpstreamBillingProbeExtraKey)
	require.Contains(t, account.Extra, service.UpstreamBillingProbeEnabledExtraKey)
	require.Contains(t, account.Extra, service.UpstreamBillingRateSyncEnabledExtraKey)
	require.Contains(t, account.Extra, "grok_billing_snapshot")
}

func TestSuperAdminAccountResponseRemainsUnchanged(t *testing.T) {
	account := &dto.Account{
		Extra:  map[string]any{service.UpstreamBillingProbeEnabledExtraKey: true},
		Groups: []*dto.Group{{ID: 3, RateMultiplier: 8.5}},
	}
	require.Same(t, account, accountAdminAccountResponse(context.Background(), account))
	require.Contains(t, account.Extra, service.UpstreamBillingProbeEnabledExtraKey)
	require.Len(t, account.Groups, 1)
}
