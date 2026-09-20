package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/pkg/usagestats"
	"github.com/th3ee9ine/qqq2api/internal/pkg/xai"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func accountAdminEarningsTestContext() context.Context {
	return context.WithValue(context.Background(), ctxkey.AccountAdminID, int64(41))
}

func TestAccountAdminWindowStatsResponseOmitsInternalCosts(t *testing.T) {
	view := accountAdminWindowStatsResponse(accountAdminEarningsTestContext(), &service.WindowStats{
		Requests: 3, Tokens: 9, Cost: 1.25, StandardCost: 7.5, UserCost: 8.5,
	})
	payload, err := json.Marshal(view)
	require.NoError(t, err)
	require.JSONEq(t, `{"requests":3,"tokens":9,"cost":1.25}`, string(payload))
}

func TestAccountAdminUsageStatsResponseContainsOnlyEarningsCost(t *testing.T) {
	stats := &usagestats.AccountUsageStatsResponse{
		History: []usagestats.AccountUsageHistory{{
			Date: "2026-09-20", Label: "09/20", Requests: 2, Tokens: 12,
			Cost: 99, ActualCost: 4.5, UserCost: 88,
		}},
		Summary: usagestats.AccountUsageSummary{
			Days: 1, ActualDaysUsed: 1, TotalCost: 4.5, TotalUserCost: 88,
			TotalStandardCost: 99, TotalRequests: 2, TotalTokens: 12,
			AvgDailyCost: 4.5, AvgDailyUserCost: 88,
		},
		Models: []usagestats.ModelStat{{
			Model: "gpt-test", Requests: 2, TotalTokens: 12,
			Cost: 99, ActualCost: 88, AccountCost: 4.5,
		}},
		Endpoints: []usagestats.EndpointStat{{Endpoint: "/v1/responses", Cost: 99, ActualCost: 88}},
	}

	payload, err := json.Marshal(accountAdminUsageStatsResponse(accountAdminEarningsTestContext(), stats))
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, json.Unmarshal(payload, &response))
	require.NotContains(t, string(payload), "user_cost")
	require.NotContains(t, string(payload), "standard_cost")
	require.NotContains(t, string(payload), "account_cost")
	require.Contains(t, string(payload), `"actual_cost":4.5`)
	require.Contains(t, string(payload), `"total_cost":4.5`)
	require.Contains(t, string(payload), `"endpoints":[]`)
}

func TestSuperAdminUsageStatsResponseRemainsUnchanged(t *testing.T) {
	stats := &usagestats.AccountUsageStatsResponse{
		Summary: usagestats.AccountUsageSummary{TotalUserCost: 3.25},
	}
	require.Same(t, stats, accountAdminUsageStatsResponse(context.Background(), stats))
}

func TestAccountAdminUsageInfoResponseProjectsEveryWindowStatsField(t *testing.T) {
	resetAt := time.Date(2026, 9, 20, 8, 30, 0, 0, time.UTC)
	window := &service.WindowStats{
		Requests: 5, Tokens: 21, Cost: 2.5, StandardCost: 8.5, UserCost: 12.5,
	}
	usage := &service.UsageInfo{
		Source:                "passive",
		FiveHour:              &service.UsageProgress{Utilization: 15, ResetsAt: &resetAt, WindowStats: window},
		SevenDay:              &service.UsageProgress{Utilization: 25, WindowStats: window},
		GeminiSharedDaily:     &service.UsageProgress{Utilization: 35, WindowStats: window},
		GrokLocalUsage:        window,
		GrokLocalUsage24h:     window,
		GrokLocalUsage7d:      window,
		GrokLocalUsageMonthly: window,
		ThirtyDay:             &service.UsageProgress{Utilization: 45, WindowStats: window},
	}

	payload, err := json.Marshal(accountAdminUsageInfoResponse(accountAdminEarningsTestContext(), usage))
	require.NoError(t, err)
	require.NotContains(t, string(payload), "standard_cost")
	require.NotContains(t, string(payload), "user_cost")
	require.Contains(t, string(payload), `"window_stats":{"requests":5,"tokens":21,"cost":2.5}`)
	require.Contains(t, string(payload), `"grok_local_usage":{"requests":5,"tokens":21,"cost":2.5}`)
	require.Contains(t, string(payload), `"source":"passive"`)
	require.Contains(t, string(payload), `"resets_at":"2026-09-20T08:30:00Z"`)
}

func TestAccountAdminUsageInfoResponseOmitsProviderBillingAmounts(t *testing.T) {
	prepaid := 12.5
	usage := &service.UsageInfo{
		GrokBilling: &xai.BillingSummary{
			PrepaidBalance: &prepaid,
			MonthlyUsed:    &prepaid,
			Plan:           "SuperGrok",
		},
	}

	payload, err := json.Marshal(accountAdminUsageInfoResponse(accountAdminEarningsTestContext(), usage))
	require.NoError(t, err)
	require.NotContains(t, string(payload), "grok_billing")
	require.NotContains(t, string(payload), "prepaid_balance")
	require.NotContains(t, string(payload), "monthly_used")
}

func TestAccountAdminUsageInfoMapResponsePreservesNilAndSuperAdminShape(t *testing.T) {
	window := &service.WindowStats{Requests: 1, Cost: 3, StandardCost: 9, UserCost: 11}
	usage := &service.UsageInfo{FiveHour: &service.UsageProgress{WindowStats: window}}
	items := map[int64]*service.UsageInfo{7: usage, 8: nil}

	projected, ok := accountAdminUsageInfoMapResponse(accountAdminEarningsTestContext(), items).(map[int64]*accountAdminUsageInfo)
	require.True(t, ok)
	require.NotNil(t, projected[7])
	require.Nil(t, projected[8])
	require.Equal(t, 3.0, projected[7].FiveHour.WindowStats.Cost)
	require.Equal(t, items, accountAdminUsageInfoMapResponse(context.Background(), items))
	require.Same(t, usage, accountAdminUsageInfoResponse(context.Background(), usage))
}

func TestAccountWindowCostResponseUsesEarningsOnlyForScopedAdmin(t *testing.T) {
	stats := &usagestats.AccountStats{Cost: 2.75, StandardCost: 9.5, UserCost: 12}
	require.Equal(t, 2.75, accountWindowCostResponse(accountAdminEarningsTestContext(), stats))
	require.Equal(t, 9.5, accountWindowCostResponse(context.Background(), stats))
}
