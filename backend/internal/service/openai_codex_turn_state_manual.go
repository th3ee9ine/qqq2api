package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

const (
	CodexTurnStateManualStatusQueued       = "queued"
	CodexTurnStateManualStatusAlreadyValid = "already_valid"
	CodexTurnStateManualStatusRejected     = "rejected"
)

// CodexTurnStateManualCollectionResult is the redacted result returned by an
// administrator-triggered collection request. It never contains the opaque
// Turn State itself.
type CodexTurnStateManualCollectionResult struct {
	AccountID          int64                   `json:"account_id"`
	Model              string                  `json:"model"`
	Status             string                  `json:"status"`
	Reason             string                  `json:"reason,omitempty"`
	Message            string                  `json:"message,omitempty"`
	RetryAtMS          int64                   `json:"retry_at_ms,omitempty"`
	CodexTurnStateAuto *CodexTurnStateAutoInfo `json:"codex_turn_state_auto,omitempty"`
}

func newCodexTurnStateManualResult(account *Account, model, status, reason, message string, now time.Time) *CodexTurnStateManualCollectionResult {
	result := &CodexTurnStateManualCollectionResult{
		Model:   model,
		Status:  status,
		Reason:  reason,
		Message: message,
	}
	if account != nil {
		result.AccountID = account.ID
		result.CodexTurnStateAuto = CodexTurnStateAutoInfoForAccount(account, now)
	}
	return result
}

// RequestCodexTurnStateCollection schedules one bounded maintenance probe for
// an account. Manual collection is independent of the automatic injection
// switch and is not gated by schedulability, proxy availability, or the
// account's serving concurrency. The configured model scope still applies, and
// only a missing or already-expired verified state may start a new probe.
func (s *OpenAIGatewayService) RequestCodexTurnStateCollection(ctx context.Context, account *Account, requestedModel string) (*CodexTurnStateManualCollectionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_COLLECTION_UNAVAILABLE", "Codex Turn State collection service is unavailable")
	}
	if account == nil || account.ID <= 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "INVALID_ACCOUNT", "invalid account")
	}

	// Always re-read the credential/proxy snapshot before deciding whether a
	// collection is allowed. The admin list can be stale while another process
	// has just published a valid state.
	current, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || current == nil || current.ID != account.ID {
		return nil, infraerrors.New(http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account not found")
	}
	account = current
	now := time.Now()
	if !codexTurnStateAutoEligible(account) {
		return newCodexTurnStateManualResult(account, "", CodexTurnStateManualStatusRejected, "account_not_eligible", "account does not support Codex Turn State", now), nil
	}
	cfg := OpenAICodexTurnStateConfig{ModelScopeValid: true}
	if s.settingService != nil {
		cfg = s.settingService.GetOpenAICodexTurnState(ctx)
	}
	model := strings.TrimSpace(requestedModel)
	if model == "" {
		model = strings.TrimSpace(cfg.DefaultModel)
	}
	normalized, normalizeErr := NormalizeOpenAICodexTurnStateDefaultModel(model)
	if normalizeErr != nil {
		return newCodexTurnStateManualResult(account, model, CodexTurnStateManualStatusRejected, "invalid_model", "invalid Turn State collection model", now), nil
	}
	model = normalized
	if !cfg.ModelScopeValid || !codexTurnStateModelMatches(cfg.Models, model) {
		s.enforceCodexTurnStateScopeForAccount(cfg, account.ID, model)
		return newCodexTurnStateManualResult(account, model, CodexTurnStateManualStatusRejected, "model_out_of_scope", "model is outside the configured Turn State model scope", now), nil
	}
	// Refresh the account-scoped cache from the authoritative source before the
	// strict missing/expired check. This also serializes with a concurrent
	// publication on another gateway process.
	if err := s.refreshCodexTurnStateSource(ctx, account, model, true); err != nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_LOOKUP_FAILED", "failed to load account Turn State")
	}
	now = time.Now()
	// Scope can change while the authoritative account slot is being loaded.
	// Recheck immediately before an existing candidate can be promoted or new
	// worker intent can be recorded.
	if s.settingService != nil {
		cfg = s.settingService.GetOpenAICodexTurnState(ctx)
	}
	if !cfg.ModelScopeValid || !codexTurnStateModelMatches(cfg.Models, model) {
		s.enforceCodexTurnStateScopeForAccount(cfg, account.ID, model)
		return newCodexTurnStateManualResult(account, model, CodexTurnStateManualStatusRejected, "model_out_of_scope", "model is outside the configured Turn State model scope", now), nil
	}

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	info := newCodexTurnStateManualResult(account, model, CodexTurnStateManualStatusRejected, "", "", now)
	info.CodexTurnStateAuto = CodexTurnStateAutoInfoForAccount(account, now)
	if s.codexTurnStateManualStateAlreadyValidLocked(entry, account, now) {
		s.openaiTurnStateMu.Unlock()
		info.Status = CodexTurnStateManualStatusAlreadyValid
		info.Reason = "state_still_valid"
		info.Message = "account already has a valid Turn State"
		return info, nil
	}
	if candidatePending := s.codexTurnStateUsageCandidateActiveLocked(entry, now); candidatePending && !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
		s.discardCodexTurnStateCandidateLocked(entry)
	} else if candidatePending {
		// The candidate already passed collection and same-route replay. An explicit
		// admin request adopts and publishes it through the ordinary CAS persistence
		// worker, without depending on later schedulable API traffic.
		entry.candidate.manual = true
		published := s.publishManualCodexTurnStateCandidateLocked(entry, now)
		if !published {
			// Manual collection has one explicit effective model. Replace the queued
			// task only when this request actually needs another probe.
			if !entry.probe {
				replaceCodexTurnStateProbeModelsLocked(entry, model, model)
				entry.probe = true
				entry.forceProbe = true
				entry.manualProbe = true
			}
		}
		s.startCodexTurnStateWorkerLocked(account.ID, entry)
		s.openaiTurnStateMu.Unlock()
		info.Status = CodexTurnStateManualStatusQueued
		info.Reason = "collection_in_flight"
		info.Message = "an existing Turn State candidate has been accepted for publication"
		return info, nil
	}
	if entry.reconciling {
		// Reconciliation owns the slot until its CAS winner has been loaded. Keep
		// the manual intent on the entry; the reconciliation finisher will retain a
		// forced probe only when the authoritative winner is still missing/expired.
		replaceCodexTurnStateProbeModelsLocked(entry, model, model)
		entry.probe = true
		entry.forceProbe = true
		entry.manualProbe = true
		s.openaiTurnStateMu.Unlock()
		info.Status = CodexTurnStateManualStatusQueued
		info.Reason = "state_reconciliation_in_flight"
		info.Message = "account state reconciliation is in progress"
		return info, nil
	}
	if notBefore := s.codexTurnStateAccountProbeNotBeforeLocked(account.ID); notBefore > now.UnixMilli() {
		s.openaiTurnStateMu.Unlock()
		info.Reason = "probe_cooldown"
		info.Message = "upstream probe cooldown is active"
		info.RetryAtMS = notBefore
		return info, nil
	}
	// A new explicit request may retry a local lookup/transport deferral
	// immediately. Durable account-wide 429 cooldowns were handled above and are
	// never cleared here.
	entry.probeRetryAfter = time.Time{}
	entry.probeWakeAt = time.Time{}

	missing := entry.token == ""
	if entry.running || entry.dirty || entry.probe || entry.manualProbe {
		// Upgrade existing automatic work to a manual request. In particular, an
		// automatic worker that observes the feature being disabled must leave this
		// forced probe for its next iteration instead of returning a false queued
		// result with no runnable work.
		replaceCodexTurnStateProbeModelsLocked(entry, model, model)
		entry.probe = true
		entry.forceProbe = true
		entry.manualProbe = true
		entry.retryAfter = time.Time{}
		entry.retryWakeAt = time.Time{}
		s.startCodexTurnStateWorkerLocked(account.ID, entry)
		s.openaiTurnStateMu.Unlock()
		info.Status = CodexTurnStateManualStatusQueued
		info.Reason = "collection_in_flight"
		info.Message = "Turn State collection is already in progress"
		return info, nil
	}

	replaceCodexTurnStateProbeModelsLocked(entry, model, model)
	entry.probe = true
	entry.forceProbe = true
	entry.manualProbe = true
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	s.openaiTurnStateMu.Unlock()

	info.Status = CodexTurnStateManualStatusQueued
	if missing {
		info.Reason = "state_missing"
	} else {
		info.Reason = "state_expired"
	}
	info.Message = "Turn State collection has been queued"
	return info, nil
}
