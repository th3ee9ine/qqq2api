package service

import (
	"context"
	"log/slog"
	"sort"
	"time"
)

const OpenAIOAuthTTFTHistoryLimit = 5

// OpenAIOAuthTTFTObservation describes a completed actual invocation. A nil
// measurement means that the request produced no usable first-token timing.
type OpenAIOAuthTTFTObservation struct {
	FirstTokenMs *int      `json:"first_token_ms"`
	ObservedAt   time.Time `json:"observed_at"`
	Model        string    `json:"model"`
}

type OpenAIOAuthTTFTHistory struct {
	ThresholdMs    int                          `json:"threshold_ms"`
	RecentRequests []OpenAIOAuthTTFTObservation `json:"recent_requests"`
}

// OpenAIOAuthTTFTHistoryCache is optional so gateways without Redis still expose
// the bounded process-local history. History is independent of streak resets.
type OpenAIOAuthTTFTHistoryCache interface {
	AppendOpenAIOAuthTTFTObservation(context.Context, int64, OpenAIOAuthTTFTObservation) error
	GetOpenAIOAuthTTFTObservations(context.Context, []int64) (map[int64][]OpenAIOAuthTTFTObservation, error)
}

func cloneOpenAIOAuthTTFTObservations(observations []OpenAIOAuthTTFTObservation) []OpenAIOAuthTTFTObservation {
	cloned := make([]OpenAIOAuthTTFTObservation, len(observations))
	for i, observation := range observations {
		cloned[i] = observation
		cloned[i].FirstTokenMs = nil
		if observation.FirstTokenMs != nil && *observation.FirstTokenMs >= 0 {
			value := *observation.FirstTokenMs
			cloned[i].FirstTokenMs = &value
		}
	}
	return cloned
}

// recordOpenAIOAuthTTFTHistoryLocked shares the streak lock so history order and
// pause decisions describe the same sequence, including fast and missing TTFTs.
func (s *OpenAIGatewayService) recordOpenAIOAuthTTFTHistoryLocked(streak *openAIOAuthTTFTStreak, accountID int64, model string, firstTokenMs *int) {
	observation := cloneOpenAIOAuthTTFTObservations([]OpenAIOAuthTTFTObservation{{
		FirstTokenMs: firstTokenMs, ObservedAt: time.Now().UTC(), Model: model,
	}})[0]
	streak.history = append([]OpenAIOAuthTTFTObservation{observation}, streak.history...)
	if len(streak.history) > OpenAIOAuthTTFTHistoryLimit {
		streak.history = streak.history[:OpenAIOAuthTTFTHistoryLimit]
	}
	if cache, ok := s.cache.(OpenAIOAuthTTFTHistoryCache); ok && cache != nil {
		if err := cache.AppendOpenAIOAuthTTFTObservation(context.Background(), accountID, observation); err != nil {
			slog.Warn("openai_oauth_ttft_history_cache_write_failed", "account_id", accountID, "error", err)
		}
	}
}

// mergeOpenAIOAuthTTFTObservations retains local writes made during a Redis
// failure or while a batch read was in flight. Match occurrences individually
// so distinct requests with identical values are not accidentally collapsed.
func mergeOpenAIOAuthTTFTObservations(remote, local []OpenAIOAuthTTFTObservation) []OpenAIOAuthTTFTObservation {
	merged := cloneOpenAIOAuthTTFTObservations(remote)
	matched := make([]bool, len(remote))
	for _, observation := range local {
		found := false
		for i, other := range remote {
			sameTiming := observation.FirstTokenMs == nil && other.FirstTokenMs == nil ||
				observation.FirstTokenMs != nil && other.FirstTokenMs != nil && *observation.FirstTokenMs == *other.FirstTokenMs
			if !matched[i] && sameTiming && observation.ObservedAt.Equal(other.ObservedAt) && observation.Model == other.Model {
				matched[i], found = true, true
				break
			}
		}
		if !found {
			merged = append(merged, cloneOpenAIOAuthTTFTObservations([]OpenAIOAuthTTFTObservation{observation})[0])
		}
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].ObservedAt.After(merged[j].ObservedAt) })
	if len(merged) > OpenAIOAuthTTFTHistoryLimit {
		merged = merged[:OpenAIOAuthTTFTHistoryLimit]
	}
	return merged
}

// GetOpenAIOAuthTTFTHistory enriches only the supplied eligible accounts. Redis
// is read once in a batch; unavailable history falls back to local observations.
// Disabling the guard hides history without erasing preceding actual requests.
func (s *OpenAIGatewayService) GetOpenAIOAuthTTFTHistory(ctx context.Context, accounts []Account) (map[int64]*OpenAIOAuthTTFTHistory, error) {
	result := make(map[int64]*OpenAIOAuthTTFTHistory)
	if s == nil || len(accounts) == 0 {
		return result, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	settings, err := s.settingService.GetOpenAIOAuthTTFTGuardSettings(ctx)
	if err != nil {
		return result, err
	}
	if settings == nil || !settings.Enabled {
		return result, nil
	}
	ids := make([]int64, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		if account.ID <= 0 || !account.IsOpenAIOAuthLike() || account.ParentAccountID != nil {
			continue
		}
		if _, exists := result[account.ID]; exists {
			continue
		}
		ids = append(ids, account.ID)
		result[account.ID] = &OpenAIOAuthTTFTHistory{ThresholdMs: openAIOAuthTTFTThresholdMillis(settings)}
	}
	if len(ids) == 0 {
		return result, nil
	}
	var remote map[int64][]OpenAIOAuthTTFTObservation
	if cache, ok := s.cache.(OpenAIOAuthTTFTHistoryCache); ok && cache != nil {
		remote, err = cache.GetOpenAIOAuthTTFTObservations(ctx, ids)
		if err != nil {
			slog.Warn("openai_oauth_ttft_history_cache_read_failed", "error", err)
			remote = nil
		}
	}
	for _, id := range ids {
		streak := s.openAIOAuthTTFTStreak(id)
		streak.mu.Lock()
		streak.history = mergeOpenAIOAuthTTFTObservations(remote[id], streak.history)
		result[id].RecentRequests = cloneOpenAIOAuthTTFTObservations(streak.history)
		streak.mu.Unlock()
	}
	return result, nil
}
