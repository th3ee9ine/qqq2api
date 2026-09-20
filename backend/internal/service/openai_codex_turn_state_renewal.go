package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

const (
	codexTurnStateRenewalInitialDelay = 10 * time.Second
	codexTurnStateRenewalScanInterval = time.Minute
	codexTurnStateRenewalPageSize     = 100
	// Planning a page performs account-scoped cleanup and may fetch a model
	// catalog. Keep this deliberately small: it improves scan latency without
	// turning a large account page into an unbounded catalog-request burst.
	codexTurnStateRenewalScanWorkers = 4
	codexTurnStateRenewalMaxWorkers  = 3
)

type codexTurnStateRenewalPlan struct {
	requestModel string
	owner        string
}

type codexTurnStateRenewalCandidate struct {
	account Account
	plan    codexTurnStateRenewalPlan
}

type codexTurnStateRenewalAccountPlan struct {
	account       Account
	plans         []codexTurnStateRenewalPlan
	cleanupFailed bool
	catalogFailed bool
}

// StartOpenAICodexTurnStateRenewal starts the idle-account maintenance scanner.
// The request path still performs configured pre-expiry renewal; this scanner
// closes the gap for existing model slots that became hard-expired or whose
// latest collection failed without receiving another eligible business request.
func (s *OpenAIGatewayService) StartOpenAICodexTurnStateRenewal() {
	if s == nil || s.accountRepo == nil || s.settingService == nil || s.httpUpstream == nil {
		return
	}
	s.openaiTurnStateRenewalMu.Lock()
	defer s.openaiTurnStateRenewalMu.Unlock()
	if s.openaiTurnStateRenewalStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.openaiTurnStateRenewalStop = cancel
	s.openaiTurnStateRenewalDone = done
	go s.runOpenAICodexTurnStateRenewal(ctx, done)
}

func (s *OpenAIGatewayService) StopOpenAICodexTurnStateRenewal() {
	if s == nil {
		return
	}
	s.openaiTurnStateRenewalMu.Lock()
	cancel := s.openaiTurnStateRenewalStop
	done := s.openaiTurnStateRenewalDone
	s.openaiTurnStateRenewalMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	s.openaiTurnStateRenewalWG.Wait()
	s.openaiTurnStateRenewalMu.Lock()
	if s.openaiTurnStateRenewalDone == done {
		s.openaiTurnStateRenewalStop = nil
		s.openaiTurnStateRenewalDone = nil
	}
	s.openaiTurnStateRenewalMu.Unlock()
}

func (s *OpenAIGatewayService) runOpenAICodexTurnStateRenewal(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	timer := time.NewTimer(codexTurnStateRenewalInitialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.scanExpiredOpenAICodexTurnStates(ctx)
			timer.Reset(codexTurnStateRenewalScanInterval)
		}
	}
}

// cloneCodexTurnStateRenewalAccount detaches the top-level maps before a page
// is planned in parallel. Account snapshots are read-only inputs, but several
// lookup helpers maintain per-value caches and optional cleanup can construct a
// filtered Extra map; neither should race with another page worker or the
// repository's backing snapshot.
func cloneCodexTurnStateRenewalAccount(account Account) Account {
	account.Credentials = mergeMap(nil, account.Credentials)
	account.Extra = mergeMap(nil, account.Extra)
	return account
}

func (s *OpenAIGatewayService) planCodexTurnStateRenewalAccount(ctx context.Context, account Account, cfg OpenAICodexTurnStateConfig, now time.Time) codexTurnStateRenewalAccountPlan {
	account = cloneCodexTurnStateRenewalAccount(account)
	result := codexTurnStateRenewalAccountPlan{account: account}
	if ctx == nil || ctx.Err() != nil || !codexTurnStateCollectionEligible(&account) {
		return result
	}
	filtered, cleanupErr := s.cleanupCodexTurnStateScope(ctx, &account, cfg)
	if cleanupErr != nil {
		result.cleanupFailed = true
	}
	if filtered != nil {
		result.account = cloneCodexTurnStateRenewalAccount(*filtered)
	}
	plans, catalogFailed := s.resolveCodexTurnStateExpiredRenewalPlans(ctx, &result.account, cfg, now)
	result.plans = plans
	result.catalogFailed = catalogFailed
	return result
}

// planCodexTurnStateRenewalAccounts processes one repository page with a small
// bounded worker pool. Each result slot is written by exactly one worker and is
// flattened by the caller in repository order, keeping cursor/fairness stable.
func (s *OpenAIGatewayService) planCodexTurnStateRenewalAccounts(ctx context.Context, accounts []Account, cfg OpenAICodexTurnStateConfig, now time.Time) []codexTurnStateRenewalAccountPlan {
	if len(accounts) == 0 || ctx == nil || ctx.Err() != nil {
		return nil
	}
	results := make([]codexTurnStateRenewalAccountPlan, len(accounts))
	workerCount := codexTurnStateRenewalScanWorkers
	if workerCount > len(accounts) {
		workerCount = len(accounts)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					results[index] = s.planCodexTurnStateRenewalAccount(ctx, accounts[index], cfg, now)
				}
			}
		}()
	}
	for index := range accounts {
		select {
		case jobs <- index:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return results
}

func (s *OpenAIGatewayService) scanExpiredOpenAICodexTurnStates(ctx context.Context) {
	if s == nil || s.accountRepo == nil || ctx == nil || ctx.Err() != nil {
		return
	}
	cfg := s.codexTurnStateRuntimeConfig(ctx)
	if !cfg.AutoEnabled || !cfg.ModelScopeValid {
		return
	}

	candidates := make([]codexTurnStateRenewalCandidate, 0)
	for page := 1; ; page++ {
		accounts, pageInfo, err := s.accountRepo.ListWithFilters(ctx, pagination.PaginationParams{
			Page: page, PageSize: codexTurnStateRenewalPageSize,
		}, PlatformOpenAI, "", StatusActive, "", 0, "")
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("openai_codex_turn_state_renewal_scan_failed", "code", "account_list_failed")
			}
			return
		}
		planned := s.planCodexTurnStateRenewalAccounts(ctx, accounts, cfg, time.Now())
		if ctx.Err() != nil {
			return
		}
		for _, result := range planned {
			if result.cleanupFailed {
				slog.Warn("openai_codex_turn_state_scope_cleanup_failed", "account_id", result.account.ID, "code", "scope_cleanup_failed")
			}
			if result.catalogFailed {
				slog.Warn("openai_codex_turn_state_renewal_catalog_failed", "account_id", result.account.ID, "code", "model_catalog_unavailable")
			}
			for _, plan := range result.plans {
				candidates = append(candidates, codexTurnStateRenewalCandidate{account: result.account, plan: plan})
			}
		}
		if len(accounts) < codexTurnStateRenewalPageSize || pageInfo == nil || page >= pageInfo.Pages {
			break
		}
	}
	if len(candidates) == 0 || ctx.Err() != nil {
		return
	}

	s.openaiTurnStateMu.Lock()
	cursor := s.openaiTurnStateRenewalCursor
	s.openaiTurnStateMu.Unlock()
	start := 0
	for index := range candidates {
		candidate := candidates[index]
		if candidate.account.ID == cursor.accountID && candidate.plan.owner == cursor.model {
			start = (index + 1) % len(candidates)
			break
		}
	}
	for offset := range len(candidates) {
		if ctx.Err() != nil {
			return
		}
		candidate := &candidates[(start+offset)%len(candidates)]
		_, saturated := s.scheduleExpiredCodexTurnStateRenewalWithContext(ctx, &candidate.account, cfg, candidate.plan, time.Now())
		if saturated {
			return
		}
	}
}

func codexTurnStateRenewalCandidateModel(cfg OpenAICodexTurnStateConfig, owner string, candidates ...string) string {
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if normalized, err := NormalizeOpenAICodexTurnStateDefaultModel(candidate); err == nil &&
			codexTurnStateOwnerModel(normalized) == owner && codexTurnStateScopeAllows(cfg, normalized) {
			return normalized
		}
	}
	return ""
}

func codexTurnStateRenewalMappingRequestModel(account *Account, cfg OpenAICodexTurnStateConfig, owner string) string {
	if account == nil {
		return ""
	}
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		return ""
	}

	// The probe bypasses ordinary request mapping, so prefer concrete upstream
	// targets. Concrete public keys remain useful for passthrough-style mappings.
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	targets := make([]string, 0, len(keys))
	aliases := make([]string, 0, len(keys))
	for _, key := range keys {
		targets = append(targets, mapping[key])
		aliases = append(aliases, key)
	}
	if model := codexTurnStateRenewalCandidateModel(cfg, owner, targets...); model != "" {
		return model
	}
	return codexTurnStateRenewalCandidateModel(cfg, owner, aliases...)
}

func codexTurnStateRenewalRequestModel(account, slot *Account, cfg OpenAICodexTurnStateConfig, owner string) string {
	if model := codexTurnStateRenewalCandidateModel(cfg, owner,
		strings.TrimSpace(slot.GetExtraString(CodexTurnStateAutoProbeModelExtraKey)),
		strings.TrimSpace(slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey)),
	); model != "" {
		return model
	}

	// Older failure records predate probe-model persistence. An exact configured
	// alias is still enough to recover the owner deterministically after restart.
	for _, configured := range strings.Split(cfg.Models, ",") {
		configured = strings.TrimSpace(configured)
		if configured == "" || strings.Contains(configured, "*") {
			continue
		}
		if model := codexTurnStateRenewalCandidateModel(cfg, owner, configured); model != "" {
			return model
		}
	}
	if model := codexTurnStateRenewalCandidateModel(cfg, owner, owner); model != "" {
		return model
	}
	return codexTurnStateRenewalMappingRequestModel(account, cfg, owner)
}

func codexTurnStateExpiredRenewalPlanCandidates(account *Account, cfg OpenAICodexTurnStateConfig, now time.Time) []codexTurnStateRenewalPlan {
	if account == nil || len(account.Extra) == 0 || !cfg.AutoEnabled || !cfg.ModelScopeValid {
		return nil
	}
	keys := make([]string, 0, len(account.Extra))
	for key := range account.Extra {
		if strings.HasPrefix(key, CodexTurnStateModelExtraPrefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	plans := make([]codexTurnStateRenewalPlan, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, CodexTurnStateModelExtraPrefix))
		if err != nil || len(decoded) == 0 {
			continue
		}
		owner := codexTurnStateOwnerModel(string(decoded))
		if owner == "" {
			continue
		}
		if _, err := NormalizeOpenAICodexTurnStateDefaultModel(owner); err != nil {
			continue
		}
		ownerKey := strings.ToLower(owner)
		if _, exists := seen[ownerKey]; exists {
			continue
		}

		slot := codexTurnStateModelAccount(account, owner)
		if slot != nil && len(slot.Extra) == 0 {
			if legacy, ok := account.Extra[key].(map[string]any); ok {
				copy := *account
				copy.Extra = legacy
				slot = &copy
			}
		}
		token := codexTurnStateAutoToken(slot)
		setAt := codexTurnStateAutoInt64(slot, CodexTurnStateAutoSetAtExtraKey)
		verifiedAt := codexTurnStateAutoInt64(slot, CodexTurnStateAutoVerifiedAtExtraKey)
		verifiedModel := strings.TrimSpace(slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))

		expiresAt := codexTurnStateAutoExpiry(token, setAt, now)
		expired := token != "" && setAt > 0 && verifiedAt > 0 &&
			codexTurnStateOwnerModel(verifiedModel) == owner && expiresAt > 0 && now.UnixMilli() >= expiresAt
		failed := safeCodexTurnStateAutoError(slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey)) != "" &&
			codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey) > 0
		if !expired && !failed {
			continue
		}
		seen[ownerKey] = struct{}{}
		requestModel := codexTurnStateRenewalRequestModel(account, slot, cfg, owner)
		plans = append(plans, codexTurnStateRenewalPlan{requestModel: requestModel, owner: owner})
	}
	return plans
}

func codexTurnStateResolvedRenewalPlans(candidates []codexTurnStateRenewalPlan) []codexTurnStateRenewalPlan {
	plans := make([]codexTurnStateRenewalPlan, 0, len(candidates))
	for _, plan := range candidates {
		if plan.requestModel != "" {
			plans = append(plans, plan)
		}
	}
	return plans
}

func codexTurnStateExpiredRenewalPlans(account *Account, cfg OpenAICodexTurnStateConfig, now time.Time) []codexTurnStateRenewalPlan {
	return codexTurnStateResolvedRenewalPlans(codexTurnStateExpiredRenewalPlanCandidates(account, cfg, now))
}

func codexTurnStateRenewalCatalogModels(response *OpenAIModelsResponse) ([]string, error) {
	if response == nil {
		return nil, codexTurnStateAutoError("model_catalog_unavailable")
	}
	_, entries, err := modelCatalogEntries(response.Body, "data")
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(entries))
	for _, raw := range entries {
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		if model := strings.TrimSpace(item.ID); model != "" {
			models = append(models, model)
		}
	}
	return models, nil
}

// resolveCodexTurnStateExpiredRenewalPlans keeps every directly recoverable
// plan even if an older sibling slot needs a catalog that cannot be loaded.
// The catalog is fetched only when a due owner remains unresolved after its
// persisted metadata, configured exact models and account mapping are checked.
func (s *OpenAIGatewayService) resolveCodexTurnStateExpiredRenewalPlans(ctx context.Context, account *Account, cfg OpenAICodexTurnStateConfig, now time.Time) ([]codexTurnStateRenewalPlan, bool) {
	candidates := codexTurnStateExpiredRenewalPlanCandidates(account, cfg, now)
	needsCatalog := false
	for _, plan := range candidates {
		if plan.requestModel == "" {
			needsCatalog = true
			break
		}
	}
	if !needsCatalog {
		return candidates, false
	}

	response, err := s.FetchOpenAIModelsList(ctx, account)
	if err != nil {
		return codexTurnStateResolvedRenewalPlans(candidates), true
	}
	models, err := codexTurnStateRenewalCatalogModels(response)
	if err != nil {
		return codexTurnStateResolvedRenewalPlans(candidates), true
	}
	for index := range candidates {
		if candidates[index].requestModel == "" {
			candidates[index].requestModel = codexTurnStateRenewalCandidateModel(cfg, candidates[index].owner, models...)
		}
	}
	return codexTurnStateResolvedRenewalPlans(candidates), false
}

func (s *OpenAIGatewayService) scheduleExpiredCodexTurnStateRenewal(account *Account, cfg OpenAICodexTurnStateConfig, plan codexTurnStateRenewalPlan, now time.Time) (scheduled, saturated bool) {
	return s.scheduleExpiredCodexTurnStateRenewalWithContext(context.Background(), account, cfg, plan, now)
}

func (s *OpenAIGatewayService) scheduleExpiredCodexTurnStateRenewalWithContext(ctx context.Context, account *Account, cfg OpenAICodexTurnStateConfig, plan codexTurnStateRenewalPlan, now time.Time) (scheduled, saturated bool) {
	if s == nil || account == nil || account.ID <= 0 || plan.owner == "" || !cfg.AutoEnabled ||
		ctx == nil || ctx.Err() != nil || !codexTurnStateScopeAllows(cfg, plan.requestModel) {
		return false, false
	}

	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	// Manual, bulk, automatic, and renewal work share one account-level task
	// admission boundary. A different owner must wait for the existing task;
	// detached dirty persistence is not represented in the registry and remains
	// independently retryable.
	if s.codexTurnStateAccountCollectionTaskActive(account.ID, "") {
		return false, false
	}
	if s.openaiTurnStateRenewalWorkers >= codexTurnStateRenewalMaxWorkers {
		return false, true
	}
	if _, active := s.openaiTurnStateRenewalAccounts[account.ID]; active {
		return false, false
	}
	entry := s.codexTurnStateEntryLocked(account, now, plan.owner, plan.requestModel)
	s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
	expiresAt := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	expired := entry.token != "" && entry.setAt > 0 && entry.verifiedAt > 0 && expiresAt > 0 && now.UnixMilli() >= expiresAt
	failed := entry.lastError != "" && entry.probeAt > 0
	retryRecordedFailure := codexTurnStateEntryHasRecordedFailure(entry)
	if (!expired && !failed) ||
		entry.running || entry.reconciling || entry.dirty || entry.probe || entry.manualProbe ||
		codexTurnStateAutomaticFailureBackoffActive(entry, now) ||
		!retryRecordedFailure && (now.Before(entry.probeRetryAfter) || now.Before(time.UnixMilli(entry.probeNotBefore))) {
		return false, false
	}
	if !replaceCodexTurnStateProbeModelsLocked(entry, plan.requestModel, plan.requestModel, plan.owner) {
		return false, false
	}
	if !s.createCodexTurnStateCollectionTaskLocked(context.Background(), account, entry, CodexTurnStateCollectionSourceRenewal) {
		return false, false
	}
	entry.probe = true
	entry.forceProbe = false
	entry.manualProbe = false
	entry.renewalProbe = true
	entry.renewalWorker = true
	entry.renewalContext = ctx
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	if !entry.running {
		if taskID := s.clearCodexTurnStateCollectionTaskLocked(entry); taskID != "" {
			_, _ = s.CancelCodexTurnStateCollectionTask(taskID)
		}
		entry.renewalWorker = false
		entry.renewalContext = nil
		return false, false
	}
	if s.openaiTurnStateRenewalAccounts == nil {
		s.openaiTurnStateRenewalAccounts = make(map[int64]struct{})
	}
	s.openaiTurnStateRenewalAccounts[account.ID] = struct{}{}
	s.openaiTurnStateRenewalWorkers++
	s.openaiTurnStateRenewalCursor = codexTurnStateKey{accountID: account.ID, model: plan.owner}
	s.openaiTurnStateRenewalWG.Add(1)
	return true, false
}
