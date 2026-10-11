package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	// Kept as a compatibility alias for package-local tests and diagnostics.
	// Runtime decisions use the value loaded from OpenAIOAuthTTFTGuardSettings.
	openAIOAuthTTFTSlowThresholdMillis = OpenAIOAuthTTFTGuardDefaultFirstTokenThresholdSeconds * 1000
	openAIOAuthTTFTSlowStreakThreshold = 5
	// A recovery probe must be allowed to wait through the configured first-token
	// threshold and still receive the terminal event. Keep the default timeout
	// compatible with the original 30-second probe while deriving longer values
	// from the configured threshold (which may be as high as 300 seconds).
	openAIOAuthTTFTProbeCompletionGrace = 10 * time.Second
	openAIOAuthTTFTRecoveryOuterGrace   = 5 * time.Second
	openAIOAuthTTFTProbeLeaseGrace      = 30 * time.Second
	openAIOAuthTTFTProbeDefaultTimeout  = 30 * time.Second
	openAIOAuthTTFTProbeLeaseDuration   = 2 * time.Minute
	openAIOAuthTTFTRecoveryPollInterval = 30 * time.Second
)

func openAIOAuthTTFTThresholdSeconds(settings *OpenAIOAuthTTFTGuardSettings) int {
	seconds := OpenAIOAuthTTFTGuardDefaultFirstTokenThresholdSeconds
	if settings != nil && settings.FirstTokenThresholdSeconds >= OpenAIOAuthTTFTGuardMinFirstTokenThresholdSeconds {
		seconds = settings.FirstTokenThresholdSeconds
	}
	if seconds > OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds {
		seconds = OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds
	}
	return seconds
}

func openAIOAuthTTFTThresholdMillis(settings *OpenAIOAuthTTFTGuardSettings) int {
	return openAIOAuthTTFTThresholdSeconds(settings) * 1000
}

func openAIOAuthTTFTProbeTimeoutForThreshold(thresholdSeconds int) time.Duration {
	if thresholdSeconds < OpenAIOAuthTTFTGuardMinFirstTokenThresholdSeconds {
		thresholdSeconds = OpenAIOAuthTTFTGuardDefaultFirstTokenThresholdSeconds
	}
	if thresholdSeconds > OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds {
		thresholdSeconds = OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds
	}
	timeout := time.Duration(thresholdSeconds)*time.Second + openAIOAuthTTFTProbeCompletionGrace
	if timeout < openAIOAuthTTFTProbeDefaultTimeout {
		return openAIOAuthTTFTProbeDefaultTimeout
	}
	return timeout
}

func openAIOAuthTTFTProbeLeaseDurationForThreshold(thresholdSeconds int) time.Duration {
	lease := openAIOAuthTTFTProbeTimeoutForThreshold(thresholdSeconds) + openAIOAuthTTFTProbeLeaseGrace
	if lease < openAIOAuthTTFTProbeLeaseDuration {
		return openAIOAuthTTFTProbeLeaseDuration
	}
	return lease
}

type openAIOAuthTTFTStreak struct {
	mu     sync.Mutex
	count  int
	model  string
	lastAt time.Time
}

type openAIOAuthTTFTStreakKey struct {
	service *OpenAIGatewayService
	id      int64
}

var openAIOAuthTTFTStreaks sync.Map // map[openAIOAuthTTFTStreakKey]*openAIOAuthTTFTStreak

// startOpenAIOAuthTTFTGuardWorker starts the recovery scanner only for the
// production repository implementation (or a test double that implements the
// narrow guard contract). Hand-built gateway services without that capability
// remain entirely synchronous and do not leak a goroutine.
func (s *OpenAIGatewayService) startOpenAIOAuthTTFTGuardWorker() {
	if s == nil || s.accountRepo == nil {
		return
	}
	if _, ok := s.accountRepo.(OpenAIOAuthTTFTGuardRepository); !ok {
		return
	}
	s.openAIOAuthTTFTGuardWorkerOnce.Do(func() {
		s.openAIOAuthTTFTGuardStop = make(chan struct{})
		s.openAIOAuthTTFTGuardWG.Add(1)
		go func() {
			defer s.openAIOAuthTTFTGuardWG.Done()
			// Poll at a bounded cadence. Waiting for the first tick lets the
			// dependency injector finish proxy and token-provider wiring before a
			// recovery probe uses the gateway.
			ticker := time.NewTicker(openAIOAuthTTFTRecoveryPollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.runOpenAIOAuthTTFTRecoveryOnce()
				case <-s.openAIOAuthTTFTGuardStop:
					return
				}
			}
		}()
	})
}

func (s *OpenAIGatewayService) runOpenAIOAuthTTFTRecoveryOnce() {
	// Bound settings loading as well as the probe. RunOpenAIOAuthTTFTRecovery
	// applies the tighter timeout from the settings snapshot it actually uses.
	ctx, cancel := context.WithTimeout(context.Background(), openAIOAuthTTFTProbeTimeoutForThreshold(OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds)+openAIOAuthTTFTRecoveryOuterGrace)
	defer cancel()
	if s != nil && s.openAIOAuthTTFTGuardStop != nil {
		go func() {
			select {
			case <-s.openAIOAuthTTFTGuardStop:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	if err := s.RunOpenAIOAuthTTFTRecovery(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("openai_oauth_ttft_guard_recovery_failed", "error", err)
		}
	}
}

func (s *OpenAIGatewayService) stopOpenAIOAuthTTFTGuardWorker() {
	if s == nil {
		return
	}
	s.openAIOAuthTTFTGuardStopOnce.Do(func() {
		if s.openAIOAuthTTFTGuardStop != nil {
			close(s.openAIOAuthTTFTGuardStop)
		}
	})
	s.openAIOAuthTTFTGuardWG.Wait()
	openAIOAuthTTFTStreaks.Range(func(key, _ any) bool {
		if streakKey, ok := key.(openAIOAuthTTFTStreakKey); ok && streakKey.service == s {
			openAIOAuthTTFTStreaks.Delete(key)
		}
		return true
	})
}

// ObserveOpenAIOAuthTTFT records one actual GPT text invocation. A missing or
// threshold-compliant first-token measurement breaks the consecutive streak;
// exactly five measurements strictly above the configured threshold trigger one persistent
// account pause. The write is delegated to the narrow repository CAS so
// concurrent gateway requests and multiple instances cannot replace another
// subsystem's pause state.
func (s *OpenAIGatewayService) ObserveOpenAIOAuthTTFT(account *Account, model string, firstTokenMs *int) {
	if s == nil || account == nil || !account.IsOpenAIOAuthLike() || account.ParentAccountID != nil || account.ID <= 0 {
		return
	}
	if IsGPTImageGenerationModel(model) {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		// A first-token measurement without a model cannot be used for a
		// recovery probe. Treat it as an unusable observation rather than
		// persisting a guard state that could never recover.
		s.resetOpenAIOAuthTTFTStreak(account.ID)
		s.resetDistributedOpenAIOAuthTTFTStreak(account.ID)
		return
	}
	settings := DefaultOpenAIOAuthTTFTGuardSettings()
	if s.settingService != nil {
		loaded, err := s.settingService.GetOpenAIOAuthTTFTGuardSettings(context.Background())
		if err != nil {
			slog.Warn("openai_oauth_ttft_guard_settings_failed", "account_id", account.ID, "error", err)
			s.resetOpenAIOAuthTTFTStreak(account.ID)
			s.resetDistributedOpenAIOAuthTTFTStreak(account.ID)
			return
		}
		if loaded != nil {
			settings = loaded
		}
	}
	if settings == nil || !settings.Enabled {
		s.resetOpenAIOAuthTTFTStreak(account.ID)
		s.resetDistributedOpenAIOAuthTTFTStreak(account.ID)
		return
	}

	streak := s.openAIOAuthTTFTStreak(account.ID)
	streak.mu.Lock()
	// Keep local observation, distributed update, and trigger consumption in
	// the same per-account order. Unlocking between them lets a concurrent
	// fast observation be overwritten by an older slow observation, or lets a
	// consumed trigger erase slow observations already counted by another call.
	defer streak.mu.Unlock()
	if firstTokenMs == nil || *firstTokenMs <= openAIOAuthTTFTThresholdMillis(settings) {
		streak.count = 0
		streak.model = ""
		streak.lastAt = time.Now()
		s.resetDistributedOpenAIOAuthTTFTStreak(account.ID)
		return
	}
	streak.count++
	streak.model = strings.TrimSpace(model)
	streak.lastAt = time.Now()
	localCount := streak.count
	triggerModel := streak.model
	count := int64(localCount)
	if distributed, ok := s.cache.(OpenAIOAuthTTFTStreakCache); ok && distributed != nil {
		// The rule counts consecutive actual calls, regardless of idle time.
		// Recovery timing must not expire the preceding slow-call history.
		value, err := distributed.IncrementOpenAIOAuthTTFTStreak(context.Background(), account.ID, 0)
		if err != nil {
			slog.Warn("openai_oauth_ttft_guard_streak_cache_failed", "account_id", account.ID, "error", err)
		} else {
			count = value
		}
	}
	if count < openAIOAuthTTFTSlowStreakThreshold {
		return
	}
	// Consume this five-sample trigger before the database write. If the write
	// races with another pause, the repository returns false and future actual
	// calls can form a fresh streak without repeatedly writing the same state.
	streak.count = 0
	streak.model = ""
	streak.lastAt = time.Now()
	s.resetDistributedOpenAIOAuthTTFTStreak(account.ID)
	now := time.Now()
	next := now.Add(time.Duration(settings.RecoveryIntervalMinutes) * time.Minute)
	repo, ok := s.accountRepo.(OpenAIOAuthTTFTGuardRepository)
	if !ok || repo == nil {
		slog.Warn("openai_oauth_ttft_guard_repository_unavailable", "account_id", account.ID)
		return
	}
	changed, err := repo.PauseOpenAIOAuthTTFT(context.Background(), account.ID, triggerModel, now, next)
	if err != nil && !changed {
		slog.Warn("openai_oauth_ttft_guard_pause_failed", "account_id", account.ID, "error", err)
		return
	}
	if !changed {
		if err != nil {
			slog.Warn("openai_oauth_ttft_guard_pause_failed", "account_id", account.ID, "error", err)
		}
		return
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	} else {
		copied := make(map[string]any, len(account.Extra)+1)
		for key, value := range account.Extra {
			copied[key] = value
		}
		account.Extra = copied
	}
	account.Extra[OpenAIOAuthTTFTGuardStateExtraKey] = map[string]any{
		"paused_at":     now.UTC().Format(time.RFC3339),
		"next_probe_at": next.UTC().Format(time.RFC3339),
		"model":         triggerModel,
	}
	if err != nil {
		slog.Warn("openai_oauth_ttft_guard_pause_snapshot_failed", "account_id", account.ID, "error", err)
	}
	slog.Warn("openai_oauth_ttft_guard_paused", "account_id", account.ID, "model", triggerModel, "next_probe_at", next.UTC())
}

func (s *OpenAIGatewayService) openAIOAuthTTFTStreak(accountID int64) *openAIOAuthTTFTStreak {
	key := openAIOAuthTTFTStreakKey{service: s, id: accountID}
	value, _ := openAIOAuthTTFTStreaks.LoadOrStore(key, &openAIOAuthTTFTStreak{})
	return value.(*openAIOAuthTTFTStreak)
}

func (s *OpenAIGatewayService) resetOpenAIOAuthTTFTStreak(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	key := openAIOAuthTTFTStreakKey{service: s, id: accountID}
	if value, ok := openAIOAuthTTFTStreaks.Load(key); ok {
		state := value.(*openAIOAuthTTFTStreak)
		state.mu.Lock()
		state.count = 0
		state.model = ""
		state.lastAt = time.Now()
		state.mu.Unlock()
	}
}

func (s *OpenAIGatewayService) resetDistributedOpenAIOAuthTTFTStreak(accountID int64) {
	if s == nil || s.cache == nil || accountID <= 0 {
		return
	}
	if distributed, ok := s.cache.(OpenAIOAuthTTFTStreakCache); ok && distributed != nil {
		if err := distributed.ResetOpenAIOAuthTTFTStreak(context.Background(), accountID); err != nil {
			slog.Warn("openai_oauth_ttft_guard_streak_reset_failed", "account_id", accountID, "error", err)
		}
	}
}

// RunOpenAIOAuthTTFTRecovery performs one bounded recovery claim/probe cycle.
// A caller can invoke it from the process scheduler; Claim uses SKIP LOCKED so
// multiple instances safely distribute probes. A successful probe clears only
// the dedicated TTFT guard state, while failure advances its next deadline.
func (s *OpenAIGatewayService) RunOpenAIOAuthTTFTRecovery(ctx context.Context) error {
	if s == nil || s.accountRepo == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	settings := DefaultOpenAIOAuthTTFTGuardSettings()
	if s.settingService != nil {
		loaded, err := s.settingService.GetOpenAIOAuthTTFTGuardSettings(ctx)
		if err != nil {
			return err
		}
		if loaded != nil {
			settings = loaded
		}
	}
	// Turning the guard off stops new pauses; existing pauses still need a
	// recovery probe so that disabling the feature cannot strand an account.
	if settings == nil {
		return nil
	}
	repo, ok := s.accountRepo.(OpenAIOAuthTTFTGuardRepository)
	if !ok || repo == nil {
		return nil
	}
	now := time.Now()
	thresholdSeconds := openAIOAuthTTFTThresholdSeconds(settings)
	ctx, cancel := context.WithTimeout(ctx, openAIOAuthTTFTProbeTimeoutForThreshold(thresholdSeconds)+openAIOAuthTTFTRecoveryOuterGrace)
	defer cancel()
	leaseUntil := now.Add(openAIOAuthTTFTProbeLeaseDurationForThreshold(thresholdSeconds))
	token, err := newOpenAIOAuthTTFTProbeToken()
	if err != nil {
		return err
	}
	account, err := repo.ClaimOpenAIOAuthTTFTProbe(ctx, now, token, leaseUntil)
	if err != nil || account == nil {
		return err
	}
	state, valid := ParseOpenAIOAuthTTFTGuardState(account.Extra)
	if !valid || strings.TrimSpace(state.Model) == "" {
		_, finishErr := repo.FinishOpenAIOAuthTTFTProbe(ctx, account.ID, token, false, time.Now().Add(time.Duration(settings.RecoveryIntervalMinutes)*time.Minute))
		if err == nil {
			err = finishErr
		}
		return err
	}
	_, probeErr := s.ProbeOpenAIOAuthTTFT(ctx, account, state.Model, thresholdSeconds)
	recovered := probeErr == nil
	next := time.Now().Add(time.Duration(settings.RecoveryIntervalMinutes) * time.Minute)
	// Even if shutdown or the caller cancels the probe, release its lease and
	// persist the next recovery deadline using a short independent cleanup.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), openAIOAuthTTFTRecoveryOuterGrace)
	defer finishCancel()
	_, finishErr := repo.FinishOpenAIOAuthTTFTProbe(finishCtx, account.ID, token, recovered, next)
	if finishErr != nil {
		return finishErr
	}
	if probeErr != nil && !errors.Is(probeErr, context.Canceled) && !errors.Is(probeErr, context.DeadlineExceeded) {
		slog.Warn("openai_oauth_ttft_guard_probe_failed", "account_id", account.ID, "error", probeErr, "next_probe_at", next.UTC())
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func newOpenAIOAuthTTFTProbeToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate openai oauth ttft probe token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}
