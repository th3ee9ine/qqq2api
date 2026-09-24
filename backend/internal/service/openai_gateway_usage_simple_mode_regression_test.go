package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

// OpenAI has a separate RecordUsage implementation from the shared gateway
// path. Keep a regression test here so its simple-mode early return cannot
// silently bypass the opt-in API-key spending-window update.
func TestOpenAIGatewayRecordUsageSimpleModeRateLimitOnly(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(
		usageRepo,
		billingRepo,
		&openAIRecordUsageUserRepoStub{},
		&openAIRecordUsageSubRepoStub{},
		nil,
	)
	svc.cfg.RunMode = config.RunModeSimple
	svc.cfg.SimpleModeKeyRateLimitEnabled = true

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "simple-openai-window",
			Usage:     OpenAIUsage{InputTokens: 100, OutputTokens: 20},
			Model:     "gpt-5.1",
			Duration:  time.Second,
		},
		APIKey: &APIKey{
			ID:          1001,
			Quota:       100,
			RateLimit5h: 30,
			Group:       &Group{RateMultiplier: 1},
		},
		User:    &User{ID: 2001},
		Account: &Account{ID: 3001, Type: AccountTypeAPIKey, Extra: map[string]any{"quota_limit": 100}},
	})

	require.NoError(t, err)
	require.Equal(t, 1, usageRepo.calls)
	require.Equal(t, 1, billingRepo.calls)
	require.NotNil(t, billingRepo.lastCmd)
	require.Positive(t, billingRepo.lastCmd.APIKeyRateLimitCost)
	require.Zero(t, billingRepo.lastCmd.APIKeyQuotaCost)
	require.Zero(t, billingRepo.lastCmd.AccountQuotaCost)
	require.Zero(t, billingRepo.lastCmd.BalanceCost)
	require.Zero(t, billingRepo.lastCmd.SubscriptionCost)
}
