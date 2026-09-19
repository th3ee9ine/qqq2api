package service

import (
	"context"
	"encoding/json"
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

type CodexTurnStateManualModelTarget struct {
	Model string `json:"model"`
	Owner string `json:"owner"`
}

// CodexTurnStateManualCollectionResult is the redacted result returned by an
// administrator-triggered collection request. It never contains the opaque
// Turn State itself. TargetModels retains concrete configured/catalog IDs,
// while queued/already-valid lists use the canonical owner keys exposed by the
// per-model diagnostics map.
type CodexTurnStateManualCollectionResult struct {
	AccountID           int64                             `json:"account_id"`
	Model               string                            `json:"model"`
	TargetModels        []string                          `json:"target_models"`
	ModelTargets        []CodexTurnStateManualModelTarget `json:"model_targets"`
	QueuedModels        []string                          `json:"queued_models"`
	AlreadyValidModels  []string                          `json:"already_valid_models"`
	SuccessfulModels    []string                          `json:"successful_models"`
	CollectionSucceeded bool                              `json:"collection_succeeded"`
	Status              string                            `json:"status"`
	Reason              string                            `json:"reason,omitempty"`
	Message             string                            `json:"message,omitempty"`
	RetryAtMS           int64                             `json:"retry_at_ms,omitempty"`
	CodexTurnStateAuto  *CodexTurnStateAutoInfo           `json:"codex_turn_state_auto"`
}

func (s *OpenAIGatewayService) newCodexTurnStateManualResult(ctx context.Context, account *Account, model, status, reason, message string, now time.Time) *CodexTurnStateManualCollectionResult {
	result := &CodexTurnStateManualCollectionResult{
		Model:              model,
		TargetModels:       []string{},
		ModelTargets:       []CodexTurnStateManualModelTarget{},
		QueuedModels:       []string{},
		AlreadyValidModels: []string{},
		SuccessfulModels:   []string{},
		Status:             status,
		Reason:             reason,
		Message:            message,
	}
	if account != nil {
		result.AccountID = account.ID
		result.CodexTurnStateAuto = s.CodexTurnStateAutoInfoForAccount(ctx, account, now)
		if result.CodexTurnStateAuto != nil {
			result.SuccessfulModels = append(result.SuccessfulModels, result.CodexTurnStateAuto.SuccessfulModels...)
			result.CollectionSucceeded = result.CodexTurnStateAuto.CollectionSucceeded
		}
	}
	return result
}

func applyCodexTurnStateManualTargets(result *CodexTurnStateManualCollectionResult, targets []CodexTurnStateManualModelTarget) {
	if result == nil {
		return
	}
	result.ModelTargets = append(result.ModelTargets[:0], targets...)
	result.TargetModels = result.TargetModels[:0]
	for _, target := range targets {
		result.TargetModels = append(result.TargetModels, target.Model)
	}
	if len(targets) > 0 {
		result.Model = targets[0].Model
	}
}

func appendCodexTurnStateManualTarget(targets []CodexTurnStateManualModelTarget, seen map[string]struct{}, model string) []CodexTurnStateManualModelTarget {
	model = strings.TrimSpace(model)
	if model == "" || strings.Contains(model, "*") || isCodexDedicatedMediaModel(model) {
		return targets
	}
	if normalized, err := NormalizeOpenAICodexTurnStateDefaultModel(model); err != nil || normalized != model {
		return targets
	}
	key := strings.ToLower(model)
	if _, exists := seen[key]; exists {
		return targets
	}
	seen[key] = struct{}{}
	return append(targets, CodexTurnStateManualModelTarget{Model: model, Owner: codexTurnStateOwnerModel(model)})
}

// resolveCodexTurnStateManualTargets expands wildcard or allow-all scopes from
// the selected account's authoritative model catalog. Exact configured IDs are
// retained even when the catalog does not list them. A catalog failure is
// returned to the caller so a broad scope can never silently degrade to only
// the default model.
func (s *OpenAIGatewayService) resolveCodexTurnStateManualTargets(ctx context.Context, account *Account, cfg OpenAICodexTurnStateConfig) ([]CodexTurnStateManualModelTarget, error) {
	if !cfg.ModelScopeValid {
		return nil, codexTurnStateAutoError("model_scope_invalid")
	}
	patterns := []string{}
	if cfg.Models != "" {
		patterns = strings.Split(cfg.Models, ",")
	}
	seen := make(map[string]struct{})
	targets := make([]CodexTurnStateManualModelTarget, 0, len(patterns))
	needsCatalog := len(patterns) == 0
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if strings.HasSuffix(pattern, "*") {
			needsCatalog = true
			continue
		}
		targets = appendCodexTurnStateManualTarget(targets, seen, pattern)
	}
	if !needsCatalog {
		return targets, nil
	}

	response, err := s.FetchOpenAIModelsList(ctx, account)
	if err != nil || response == nil {
		return nil, codexTurnStateAutoError("model_catalog_unavailable")
	}
	_, entries, err := modelCatalogEntries(response.Body, "data")
	if err != nil {
		return nil, codexTurnStateAutoError("model_catalog_unavailable")
	}
	for _, raw := range entries {
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, codexTurnStateAutoError("model_catalog_unavailable")
		}
		model := strings.TrimSpace(item.ID)
		if model == "" || !codexTurnStateModelMatches(cfg.Models, model) {
			continue
		}
		targets = appendCodexTurnStateManualTarget(targets, seen, model)
	}
	return targets, nil
}

func codexTurnStateManualOwnerPlans(targets []CodexTurnStateManualModelTarget) []CodexTurnStateManualModelTarget {
	plans := make([]CodexTurnStateManualModelTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		owner := strings.TrimSpace(target.Owner)
		if owner == "" {
			continue
		}
		key := strings.ToLower(owner)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		plans = append(plans, target)
	}
	return plans
}

// RequestCodexTurnStateCollection schedules one bounded maintenance round for
// every concrete model in the configured scope. The legacy requestedModel
// argument is intentionally ignored: the server-side scope and account catalog
// are authoritative. Manual collection remains independent of the automatic
// switch and bypasses existing cooldowns, while normal account lifecycle state
// and model scope still apply.
func (s *OpenAIGatewayService) RequestCodexTurnStateCollection(ctx context.Context, account *Account, _ string) (*CodexTurnStateManualCollectionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_COLLECTION_UNAVAILABLE", "Codex Turn State collection service is unavailable")
	}
	if account == nil || account.ID <= 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "INVALID_ACCOUNT", "invalid account")
	}

	current, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || current == nil || current.ID != account.ID {
		return nil, infraerrors.New(http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account not found")
	}
	account = current
	now := time.Now()
	if !codexTurnStateAutoEligible(account) {
		return s.newCodexTurnStateManualResult(ctx, account, "", CodexTurnStateManualStatusRejected, "account_not_eligible", "account does not support Codex Turn State", now), nil
	}
	if !account.IsSchedulable() {
		return s.newCodexTurnStateManualResult(ctx, account, "", CodexTurnStateManualStatusRejected, "account_not_schedulable", "account is not currently schedulable", now), nil
	}

	cfg := OpenAICodexTurnStateConfig{ModelScopeValid: true}
	if s.settingService != nil {
		cfg = s.settingService.GetOpenAICodexTurnState(ctx)
	}
	targets, resolveErr := s.resolveCodexTurnStateManualTargets(ctx, account, cfg)
	if resolveErr != nil {
		reason := safeCodexTurnStateManualResolutionError(resolveErr)
		return s.newCodexTurnStateManualResult(ctx, account, "", CodexTurnStateManualStatusRejected, reason, "failed to resolve configured Turn State models", now), nil
	}
	if len(targets) == 0 {
		return s.newCodexTurnStateManualResult(ctx, account, "", CodexTurnStateManualStatusRejected, "model_scope_empty", "configured Turn State model scope contains no available models", now), nil
	}
	plans := codexTurnStateManualOwnerPlans(targets)

	// Refresh every owner slot before making the batch decision. No work is
	// queued until all authoritative reads have completed.
	for _, plan := range plans {
		if err := s.refreshCodexTurnStateSource(ctx, account, plan.Owner, true); err != nil {
			return nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_LOOKUP_FAILED", "failed to load account Turn State")
		}
	}
	current, err = s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || current == nil || current.ID != account.ID {
		return nil, infraerrors.New(http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account not found")
	}
	account = current
	now = time.Now()
	if !codexTurnStateCollectionEligible(account) {
		result := s.newCodexTurnStateManualResult(ctx, account, "", CodexTurnStateManualStatusRejected, "account_not_schedulable", "account is not currently schedulable", now)
		applyCodexTurnStateManualTargets(result, targets)
		return result, nil
	}
	if s.settingService != nil {
		latest := s.settingService.GetOpenAICodexTurnState(ctx)
		if !latest.ModelScopeValid || latest.Models != cfg.Models {
			result := s.newCodexTurnStateManualResult(ctx, account, "", CodexTurnStateManualStatusRejected, "model_scope_changed", "Turn State model scope changed; retry collection", now)
			applyCodexTurnStateManualTargets(result, targets)
			return result, nil
		}
		cfg = latest
	}

	result := s.newCodexTurnStateManualResult(ctx, account, "", CodexTurnStateManualStatusRejected, "", "", now)
	applyCodexTurnStateManualTargets(result, targets)
	s.openaiTurnStateMu.Lock()
	type queuedOwner struct {
		plan  CodexTurnStateManualModelTarget
		entry *codexTurnStateAutoEntry
	}
	queued := make([]queuedOwner, 0, len(plans))
	busy := false
	scopeChanged := false
	for _, plan := range plans {
		entry := s.codexTurnStateEntryLocked(account, now, plan.Model)
		if !codexTurnStateScopeAllows(cfg, plan.Model, plan.Owner) {
			scopeChanged = true
			break
		}
		candidatePending := s.codexTurnStateUsageCandidateActiveLocked(entry, now)
		if entry.running || entry.reconciling || entry.dirty || entry.probe || entry.manualProbe || candidatePending {
			busy = true
			break
		}
		if s.codexTurnStateManualStateAlreadyValidLocked(entry, account, now) {
			result.AlreadyValidModels = append(result.AlreadyValidModels, plan.Owner)
			continue
		}
		queued = append(queued, queuedOwner{plan: plan, entry: entry})
	}
	if scopeChanged {
		s.openaiTurnStateMu.Unlock()
		result.Status = CodexTurnStateManualStatusRejected
		result.Reason = "model_scope_changed"
		result.Message = "Turn State model scope changed; retry collection"
		return result, nil
	}
	if busy {
		s.openaiTurnStateMu.Unlock()
		result.Status = CodexTurnStateManualStatusRejected
		result.Reason = "collection_already_in_flight"
		result.Message = "another Turn State collection is already in progress; retry after it finishes"
		return result, nil
	}

	// Record every owner before starting any worker. A second manual request sees
	// the complete batch as busy, and all workers serialize on the account-level
	// manual lock so a broad model scope cannot create unbounded upstream load.
	for _, item := range queued {
		plan, entry := item.plan, item.entry
		// Every explicit batch may retry immediately. The durable account-wide
		// boundary remains stored for later automatic work.
		entry.probeRetryAfter = time.Time{}
		entry.probeWakeAt = time.Time{}
		replaceCodexTurnStateProbeModelsLocked(entry, plan.Model, plan.Model, plan.Owner)
		entry.probe = true
		entry.forceProbe = true
		entry.manualProbe = true
		entry.retryAfter = time.Time{}
		entry.retryWakeAt = time.Time{}
		result.QueuedModels = append(result.QueuedModels, plan.Owner)
	}
	for _, item := range queued {
		s.startCodexTurnStateWorkerLocked(account.ID, item.entry)
	}
	s.openaiTurnStateMu.Unlock()

	if len(result.QueuedModels) > 0 {
		result.Status = CodexTurnStateManualStatusQueued
		result.Reason = "collection_in_flight"
		result.Message = "Turn State collection has been queued for all configured models"
		return result, nil
	}
	if len(result.AlreadyValidModels) == len(plans) {
		result.Status = CodexTurnStateManualStatusAlreadyValid
		result.Reason = "state_still_valid"
		result.Message = "all configured models already have a valid Turn State"
		return result, nil
	}
	result.Status = CodexTurnStateManualStatusRejected
	result.Reason = "model_scope_changed"
	result.Message = "Turn State model scope changed; retry collection"
	return result, nil
}

func safeCodexTurnStateManualResolutionError(err error) string {
	if err == nil {
		return "model_catalog_unavailable"
	}
	switch err.Error() {
	case "model_scope_invalid", "model_catalog_unavailable":
		return err.Error()
	default:
		return "model_catalog_unavailable"
	}
}
