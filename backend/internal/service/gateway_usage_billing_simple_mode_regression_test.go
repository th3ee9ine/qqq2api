package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Simple-mode spending-window opt-in must update only the API-key window
// counters. In particular, a key that also has a legacy quota and an
// account-level quota must not consume either quota or a user wallet.
func TestBuildUsageBillingCommandSimpleModeRateLimitOnlySkipsOtherQuotas(t *testing.T) {
	groupID := int64(7)
	cmd := buildUsageBillingCommand("simple-window-request", nil, &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 2.5, TotalCost: 3.0},
		User: &User{ID: 11},
		APIKey: &APIKey{
			ID:          13,
			Quota:       100,
			RateLimit5h: 10,
			GroupID:     &groupID,
		},
		Account: &Account{
			ID:    17,
			Type:  AccountTypeAPIKey,
			Extra: map[string]any{"quota_limit": 100},
		},
		SimpleModeKeyRateLimitOnly: true,
	})

	require.NotNil(t, cmd)
	require.Equal(t, 2.5, cmd.APIKeyRateLimitCost)
	require.Zero(t, cmd.APIKeyQuotaCost)
	require.Zero(t, cmd.AccountQuotaCost)
	require.Zero(t, cmd.BalanceCost)
	require.Zero(t, cmd.SubscriptionCost)
}
