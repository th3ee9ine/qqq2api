package admin

import (
	"context"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/pkg/usagestats"
	"github.com/th3ee9ine/qqq2api/internal/pkg/xai"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type accountAdminWindowStats struct {
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	Cost     float64 `json:"cost"`
}

func accountAdminWindowStatsView(stats *service.WindowStats) *accountAdminWindowStats {
	if stats == nil {
		return nil
	}
	return &accountAdminWindowStats{
		Requests: stats.Requests,
		Tokens:   stats.Tokens,
		Cost:     stats.Cost,
	}
}

func accountAdminWindowStatsResponse(ctx context.Context, stats *service.WindowStats) any {
	if _, scoped := ctxkey.AccountAdminIDFromContext(ctx); !scoped || stats == nil {
		return stats
	}
	return accountAdminWindowStatsView(stats)
}

func accountAdminWindowStatsMapResponse(ctx context.Context, stats map[int64]*service.WindowStats) any {
	if _, scoped := ctxkey.AccountAdminIDFromContext(ctx); !scoped {
		return stats
	}
	out := make(map[int64]*accountAdminWindowStats, len(stats))
	for accountID, item := range stats {
		if item == nil {
			out[accountID] = nil
			continue
		}
		out[accountID] = accountAdminWindowStatsView(item)
	}
	return out
}

func accountWindowCostResponse(ctx context.Context, stats *usagestats.AccountStats) float64 {
	if stats == nil {
		return 0
	}
	if _, scoped := ctxkey.AccountAdminIDFromContext(ctx); scoped {
		return stats.Cost
	}
	return stats.StandardCost
}

type accountAdminUsageProgress struct {
	Utilization      float64                  `json:"utilization"`
	ResetsAt         *time.Time               `json:"resets_at"`
	RemainingSeconds int                      `json:"remaining_seconds"`
	WindowStats      *accountAdminWindowStats `json:"window_stats,omitempty"`
	UsedRequests     int64                    `json:"used_requests,omitempty"`
	LimitRequests    int64                    `json:"limit_requests,omitempty"`
}

func accountAdminUsageProgressView(progress *service.UsageProgress) *accountAdminUsageProgress {
	if progress == nil {
		return nil
	}
	return &accountAdminUsageProgress{
		Utilization:      progress.Utilization,
		ResetsAt:         progress.ResetsAt,
		RemainingSeconds: progress.RemainingSeconds,
		WindowStats:      accountAdminWindowStatsView(progress.WindowStats),
		UsedRequests:     progress.UsedRequests,
		LimitRequests:    progress.LimitRequests,
	}
}

// accountAdminUsageInfo embeds the provider quota response and overrides every
// local-cost-bearing field with the restricted earnings projection. Explicit
// fields win over equally named fields from the embedded value during JSON
// encoding, preserving the existing response shape without exposing internal
// standard or user-billed costs.
type accountAdminUsageInfo struct {
	*service.UsageInfo
	// GrokBilling contains provider-side absolute billing amounts. Keep the
	// field explicit and nil so the embedded UsageInfo cannot expose internal
	// provider costs to a restricted account administrator.
	GrokBilling           *xai.BillingSummary        `json:"grok_billing,omitempty"`
	FiveHour              *accountAdminUsageProgress `json:"five_hour"`
	SevenDay              *accountAdminUsageProgress `json:"seven_day,omitempty"`
	SevenDaySonnet        *accountAdminUsageProgress `json:"seven_day_sonnet,omitempty"`
	SevenDayFable         *accountAdminUsageProgress `json:"seven_day_fable,omitempty"`
	GeminiSharedDaily     *accountAdminUsageProgress `json:"gemini_shared_daily,omitempty"`
	GeminiProDaily        *accountAdminUsageProgress `json:"gemini_pro_daily,omitempty"`
	GeminiFlashDaily      *accountAdminUsageProgress `json:"gemini_flash_daily,omitempty"`
	GeminiSharedMinute    *accountAdminUsageProgress `json:"gemini_shared_minute,omitempty"`
	GeminiProMinute       *accountAdminUsageProgress `json:"gemini_pro_minute,omitempty"`
	GeminiFlashMinute     *accountAdminUsageProgress `json:"gemini_flash_minute,omitempty"`
	GrokLocalUsage        *accountAdminWindowStats   `json:"grok_local_usage,omitempty"`
	GrokLocalUsage24h     *accountAdminWindowStats   `json:"grok_local_usage_24h,omitempty"`
	GrokLocalUsage7d      *accountAdminWindowStats   `json:"grok_local_usage_7d,omitempty"`
	GrokLocalUsageMonthly *accountAdminWindowStats   `json:"grok_local_usage_monthly,omitempty"`
	ThirtyDay             *accountAdminUsageProgress `json:"thirty_day,omitempty"`
}

func accountAdminUsageInfoView(usage *service.UsageInfo) *accountAdminUsageInfo {
	if usage == nil {
		return nil
	}
	return &accountAdminUsageInfo{
		UsageInfo:             usage,
		GrokBilling:           nil,
		FiveHour:              accountAdminUsageProgressView(usage.FiveHour),
		SevenDay:              accountAdminUsageProgressView(usage.SevenDay),
		SevenDaySonnet:        accountAdminUsageProgressView(usage.SevenDaySonnet),
		SevenDayFable:         accountAdminUsageProgressView(usage.SevenDayFable),
		GeminiSharedDaily:     accountAdminUsageProgressView(usage.GeminiSharedDaily),
		GeminiProDaily:        accountAdminUsageProgressView(usage.GeminiProDaily),
		GeminiFlashDaily:      accountAdminUsageProgressView(usage.GeminiFlashDaily),
		GeminiSharedMinute:    accountAdminUsageProgressView(usage.GeminiSharedMinute),
		GeminiProMinute:       accountAdminUsageProgressView(usage.GeminiProMinute),
		GeminiFlashMinute:     accountAdminUsageProgressView(usage.GeminiFlashMinute),
		GrokLocalUsage:        accountAdminWindowStatsView(usage.GrokLocalUsage),
		GrokLocalUsage24h:     accountAdminWindowStatsView(usage.GrokLocalUsage24h),
		GrokLocalUsage7d:      accountAdminWindowStatsView(usage.GrokLocalUsage7d),
		GrokLocalUsageMonthly: accountAdminWindowStatsView(usage.GrokLocalUsageMonthly),
		ThirtyDay:             accountAdminUsageProgressView(usage.ThirtyDay),
	}
}

func accountAdminUsageInfoResponse(ctx context.Context, usage *service.UsageInfo) any {
	if _, scoped := ctxkey.AccountAdminIDFromContext(ctx); !scoped || usage == nil {
		return usage
	}
	return accountAdminUsageInfoView(usage)
}

func accountAdminUsageInfoMapResponse(ctx context.Context, usage map[int64]*service.UsageInfo) any {
	if _, scoped := ctxkey.AccountAdminIDFromContext(ctx); !scoped {
		return usage
	}
	out := make(map[int64]*accountAdminUsageInfo, len(usage))
	for accountID, item := range usage {
		out[accountID] = accountAdminUsageInfoView(item)
	}
	return out
}

type accountAdminUsageHistory struct {
	Date       string  `json:"date"`
	Label      string  `json:"label"`
	Requests   int64   `json:"requests"`
	Tokens     int64   `json:"tokens"`
	ActualCost float64 `json:"actual_cost"`
}

type accountAdminUsageToday struct {
	Date     string  `json:"date"`
	Cost     float64 `json:"cost"`
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
}

type accountAdminUsagePeak struct {
	Date     string  `json:"date"`
	Label    string  `json:"label"`
	Cost     float64 `json:"cost"`
	Requests int64   `json:"requests"`
}

type accountAdminUsageSummary struct {
	Days              int                     `json:"days"`
	ActualDaysUsed    int                     `json:"actual_days_used"`
	TotalCost         float64                 `json:"total_cost"`
	TotalRequests     int64                   `json:"total_requests"`
	TotalTokens       int64                   `json:"total_tokens"`
	AvgDailyCost      float64                 `json:"avg_daily_cost"`
	AvgDailyRequests  float64                 `json:"avg_daily_requests"`
	AvgDailyTokens    float64                 `json:"avg_daily_tokens"`
	AvgDurationMs     float64                 `json:"avg_duration_ms"`
	Today             *accountAdminUsageToday `json:"today"`
	HighestCostDay    *accountAdminUsagePeak  `json:"highest_cost_day"`
	HighestRequestDay *accountAdminUsagePeak  `json:"highest_request_day"`
}

type accountAdminModelEarnings struct {
	Model               string  `json:"model"`
	Requests            int64   `json:"requests"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	TotalTokens         int64   `json:"total_tokens"`
	ActualCost          float64 `json:"actual_cost"`
}

type accountAdminUsageStats struct {
	History           []accountAdminUsageHistory  `json:"history"`
	Summary           accountAdminUsageSummary    `json:"summary"`
	Models            []accountAdminModelEarnings `json:"models"`
	Endpoints         []any                       `json:"endpoints"`
	UpstreamEndpoints []any                       `json:"upstream_endpoints"`
}

func accountAdminUsageStatsResponse(ctx context.Context, stats *usagestats.AccountUsageStatsResponse) any {
	if _, scoped := ctxkey.AccountAdminIDFromContext(ctx); !scoped || stats == nil {
		return stats
	}

	history := make([]accountAdminUsageHistory, 0, len(stats.History))
	for _, item := range stats.History {
		history = append(history, accountAdminUsageHistory{
			Date:       item.Date,
			Label:      item.Label,
			Requests:   item.Requests,
			Tokens:     item.Tokens,
			ActualCost: item.ActualCost,
		})
	}

	models := make([]accountAdminModelEarnings, 0, len(stats.Models))
	for _, item := range stats.Models {
		models = append(models, accountAdminModelEarnings{
			Model:               item.Model,
			Requests:            item.Requests,
			InputTokens:         item.InputTokens,
			OutputTokens:        item.OutputTokens,
			CacheCreationTokens: item.CacheCreationTokens,
			CacheReadTokens:     item.CacheReadTokens,
			TotalTokens:         item.TotalTokens,
			ActualCost:          item.AccountCost,
		})
	}

	view := &accountAdminUsageStats{
		History: history,
		Summary: accountAdminUsageSummary{
			Days:             stats.Summary.Days,
			ActualDaysUsed:   stats.Summary.ActualDaysUsed,
			TotalCost:        stats.Summary.TotalCost,
			TotalRequests:    stats.Summary.TotalRequests,
			TotalTokens:      stats.Summary.TotalTokens,
			AvgDailyCost:     stats.Summary.AvgDailyCost,
			AvgDailyRequests: stats.Summary.AvgDailyRequests,
			AvgDailyTokens:   stats.Summary.AvgDailyTokens,
			AvgDurationMs:    stats.Summary.AvgDurationMs,
		},
		Models:            models,
		Endpoints:         []any{},
		UpstreamEndpoints: []any{},
	}
	if item := stats.Summary.Today; item != nil {
		view.Summary.Today = &accountAdminUsageToday{
			Date:     item.Date,
			Cost:     item.Cost,
			Requests: item.Requests,
			Tokens:   item.Tokens,
		}
	}
	if item := stats.Summary.HighestCostDay; item != nil {
		view.Summary.HighestCostDay = &accountAdminUsagePeak{
			Date: item.Date, Label: item.Label, Cost: item.Cost, Requests: item.Requests,
		}
	}
	if item := stats.Summary.HighestRequestDay; item != nil {
		view.Summary.HighestRequestDay = &accountAdminUsagePeak{
			Date: item.Date, Label: item.Label, Cost: item.Cost, Requests: item.Requests,
		}
	}
	return view
}
