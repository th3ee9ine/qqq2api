package service

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	"golang.org/x/sync/singleflight"
)

// Separate top-level JSONB keys let concurrent model workers update atomically
// through UpdateExtra without replacing another model's state.
const CodexTurnStateModelExtraPrefix = "codex_turn_state_auto_model:"

// CodexTurnStateScopeCleanupRepository removes account-owned Turn State
// records without replacing the account's unrelated Extra fields. It is kept
// optional so lightweight repository implementations used by tests and
// alternate deployments can continue to provide the AccountRepository
// contract without a model-specific SQL operation.
type CodexTurnStateScopeCleanupRepository interface {
	DeleteCodexTurnStateScopeExtras(context.Context, int64, []string) error
}

type codexTurnStateKey struct {
	accountID int64
	model     string
}
type codexTurnStateModelContextKey struct{}
type codexTurnStateRequestPolicyKey struct{}
type codexTurnStateRequestPolicy struct {
	native        string
	nativeAllowed bool
	compat        string
	models        []string
}
type codexTurnStateSourceLoads struct{ group singleflight.Group }

type CodexTurnStateSourceRepository interface {
	GetCodexTurnStateSource(context.Context, int64) (*Account, error)
}

var errCodexTurnStateLookup = errors.New("codex turn state lookup failed")

func codexTurnStateModelExtraKey(model string) string {
	return codexTurnStateRawModelExtraKey(codexTurnStateOwnerModel(model))
}

func codexTurnStateRawModelExtraKey(model string) string {
	return CodexTurnStateModelExtraPrefix + base64.RawURLEncoding.EncodeToString([]byte(strings.TrimSpace(model)))
}

// codexTurnStateScopeAllowsOwner is a conservative scope check for persisted
// owner slots. A canonical owner (for example gpt-6-astra) can represent a
// configured dated alias (for example gpt-6-astra-2026-09-19), so checking only
// the owner string would incorrectly delete an otherwise in-scope slot when
// its request-model metadata is missing from an older record.
func codexTurnStateScopeAllowsOwner(cfg OpenAICodexTurnStateConfig, owner string, models ...string) bool {
	if !cfg.ModelScopeValid {
		return false
	}
	owner = strings.TrimSpace(owner)
	if owner == "" || codexTurnStateScopeAllows(cfg, models...) {
		return true
	}
	for _, raw := range strings.Split(cfg.Models, ",") {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			continue
		}
		body := strings.TrimSuffix(pattern, "*")
		if body != "" && codexTurnStateOwnerModel(body) == owner {
			return true
		}
	}
	return false
}

func codexTurnStateScopeStringValue(raw any, key string) string {
	values, ok := raw.(map[string]any)
	if !ok || values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

// codexTurnStateScopeCleanupKeys returns only model-scoped records that are
// provably outside the configured collection range. Invalid settings fail
// closed and return no keys; an empty valid scope means all models and also
// returns no keys. Legacy unscoped fields and native provenance are retained.
// The result is stable to make repository writes and tests deterministic.
func codexTurnStateScopeCleanupKeys(account *Account, cfg OpenAICodexTurnStateConfig) []string {
	if account == nil || len(account.Extra) == 0 || !cfg.ModelScopeValid || strings.TrimSpace(cfg.Models) == "" {
		return nil
	}
	keys := make([]string, 0)
	for key, raw := range account.Extra {
		switch {
		case strings.HasPrefix(key, CodexTurnStateModelExtraPrefix):
			if key == CodexTurnStateNativeProvenanceExtraKey {
				continue
			}
			decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, CodexTurnStateModelExtraPrefix))
			if err != nil || len(decoded) == 0 {
				// The prefix is reserved for generated model slots; a malformed
				// key cannot be in the configured collection range.
				keys = append(keys, key)
				continue
			}
			decodedModel := strings.TrimSpace(string(decoded))
			owner := codexTurnStateOwnerModel(decodedModel)
			candidates := []string{decodedModel, owner}
			candidates = append(candidates,
				codexTurnStateScopeStringValue(raw, CodexTurnStateAutoProbeModelExtraKey),
				codexTurnStateScopeStringValue(raw, CodexTurnStateAutoVerifiedModelExtraKey),
			)
			// A persisted slot may predate probe-model metadata. Account-level
			// aliases still identify its owner, so retain the slot when either
			// side of a configured model mapping belongs to that owner.
			for alias, target := range account.GetModelMapping() {
				if codexTurnStateOwnerModel(alias) == owner || codexTurnStateOwnerModel(target) == owner {
					candidates = append(candidates, alias, target)
				}
			}
			if !codexTurnStateScopeAllowsOwner(cfg, owner, candidates...) {
				keys = append(keys, key)
			}
		case strings.HasPrefix(key, CodexTurnStateProbeBurstBudgetExtraPrefix):
			encoded := strings.TrimPrefix(key, CodexTurnStateProbeBurstBudgetExtraPrefix)
			decoded, err := base64.RawURLEncoding.DecodeString(encoded)
			if err != nil || len(decoded) == 0 {
				keys = append(keys, key)
				continue
			}
			model := strings.TrimSpace(string(decoded))
			owner := codexTurnStateOwnerModel(model)
			if !codexTurnStateScopeAllowsOwner(cfg, owner, model, owner) {
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func codexTurnStateAccountWithoutScopeRecords(account *Account, cfg OpenAICodexTurnStateConfig, preserveOwners ...string) (*Account, []string) {
	if account == nil {
		return nil, nil
	}
	keys := codexTurnStateScopeCleanupKeys(account, cfg)
	if len(keys) > 0 && len(preserveOwners) > 0 {
		kept := keys[:0]
		for _, key := range keys {
			owner := codexTurnStateScopeCleanupOwner(key)
			if owner != "" && codexTurnStateScopeOwnerMatches(owner, preserveOwners...) {
				continue
			}
			kept = append(kept, key)
		}
		keys = kept
	}
	if len(keys) == 0 {
		return account, nil
	}
	copy := *account
	copy.Extra = make(map[string]any, len(account.Extra)-len(keys))
	remove := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		remove[key] = struct{}{}
	}
	for key, value := range account.Extra {
		if _, drop := remove[key]; !drop {
			copy.Extra[key] = value
		}
	}
	return &copy, keys
}

func codexTurnStateScopeOwnerMatches(owner string, candidates ...string) bool {
	owner = strings.TrimSpace(codexTurnStateOwnerModel(owner))
	if owner == "" {
		return false
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(codexTurnStateOwnerModel(candidate))
		if candidate != "" && strings.EqualFold(owner, candidate) {
			return true
		}
	}
	return false
}

func codexTurnStateScopeCleanupOwner(key string) string {
	var encoded string
	switch {
	case strings.HasPrefix(key, CodexTurnStateModelExtraPrefix):
		if key == CodexTurnStateNativeProvenanceExtraKey {
			return ""
		}
		encoded = strings.TrimPrefix(key, CodexTurnStateModelExtraPrefix)
	case strings.HasPrefix(key, CodexTurnStateProbeBurstBudgetExtraPrefix):
		encoded = strings.TrimPrefix(key, CodexTurnStateProbeBurstBudgetExtraPrefix)
	default:
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return ""
	}
	return codexTurnStateOwnerModel(string(decoded))
}

func codexTurnStateEntryIdleForScopeCleanup(entry *codexTurnStateAutoEntry) bool {
	return entry != nil && !entry.running && !entry.reconciling && !entry.dirty && !entry.probe &&
		!entry.manualProbe && !entry.renewalProbe && !entry.manualOutcomePending &&
		entry.collectionTaskID == "" && entry.candidate.state == "" && entry.pendingOwner.version <= 0 &&
		entry.retryWakeAt.IsZero() && entry.probeWakeAt.IsZero()
}

// cleanupCodexTurnStateScope removes stale persisted model records while
// preserving unrelated account extras. The returned account is a filtered
// snapshot even when the optional repository cleanup is unavailable or fails,
// so the current request/scan cannot accidentally collect an out-of-scope
// model. Active local workers are excluded from the delete set and continue
// through the existing scope-change terminal handling.
func (s *OpenAIGatewayService) cleanupCodexTurnStateScope(ctx context.Context, account *Account, cfg OpenAICodexTurnStateConfig, preserveOwners ...string) (*Account, error) {
	cleaned, keys := codexTurnStateAccountWithoutScopeRecords(account, cfg, preserveOwners...)
	if len(keys) == 0 || s == nil || s.accountRepo == nil || account == nil {
		return cleaned, nil
	}
	activeOwners := make(map[string]struct{})
	s.openaiTurnStateMu.Lock()
	for _, key := range keys {
		owner := codexTurnStateScopeCleanupOwner(key)
		if owner == "" {
			continue
		}
		if entry := s.openaiTurnStates[codexTurnStateKey{accountID: account.ID, model: owner}]; entry != nil && !codexTurnStateEntryIdleForScopeCleanup(entry) {
			activeOwners[owner] = struct{}{}
		}
	}
	s.openaiTurnStateMu.Unlock()
	deleteKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		owner := codexTurnStateScopeCleanupOwner(key)
		if owner != "" {
			if _, active := activeOwners[owner]; active {
				continue
			}
		}
		deleteKeys = append(deleteKeys, key)
	}
	if len(deleteKeys) == 0 {
		return cleaned, nil
	}
	// Clear idle process-local slots even when an alternate repository cannot
	// perform the durable delete. Otherwise a later scope expansion could
	// resurrect a state that this request already treated as out of scope.
	s.openaiTurnStateMu.Lock()
	for _, key := range deleteKeys {
		owner := codexTurnStateScopeCleanupOwner(key)
		if owner == "" {
			continue
		}
		entryKey := codexTurnStateKey{accountID: account.ID, model: owner}
		if entry := s.openaiTurnStates[entryKey]; codexTurnStateEntryIdleForScopeCleanup(entry) {
			delete(s.openaiTurnStates, entryKey)
		}
	}
	s.openaiTurnStateMu.Unlock()
	repo, ok := s.accountRepo.(CodexTurnStateScopeCleanupRepository)
	if !ok {
		// Alternate repositories may not support atomic JSONB key deletion yet.
		// Do not issue nil-valued UpdateExtra writes: those create JSON nulls and
		// make a supposedly removed record visible to later readers.
		return cleaned, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	err := repo.DeleteCodexTurnStateScopeExtras(cleanupCtx, account.ID, deleteKeys)
	cancel()
	if err != nil {
		return cleaned, err
	}
	return cleaned, nil
}

// codexTurnStateOwnerModel affects only state ownership. The requested model
// remains unchanged in outbound payloads and usage logs.
func codexTurnStateOwnerModel(model string) string {
	model = strings.TrimSpace(model)
	if isCodexAutoReviewFamilyModel(model) {
		return "codex-auto-review"
	}
	if isOpenAIGPT6AstraModel(model) {
		return "gpt-6-astra"
	}
	return model
}

// CodexTurnStateOwnerModel exposes the canonical state owner to repository
// adapters without changing the model carried by requests or usage logs.
func CodexTurnStateOwnerModel(model string) string {
	return codexTurnStateOwnerModel(model)
}

func codexTurnStateModelAccount(account *Account, model string) *Account {
	if account == nil {
		return nil
	}
	copy := *account
	owner := codexTurnStateOwnerModel(model)
	if canonical, ok := account.Extra[codexTurnStateModelExtraKey(owner)].(map[string]any); ok {
		copy.Extra = canonical
		return &copy
	}

	// Compatibility for deployments that wrote exact variant slots before
	// family ownership was introduced. A canonical slot, even an invalid one,
	// always wins above. Without one, accept exactly one verified same-family
	// slot; multiple legacy claims fail closed. The fallback stays read-only
	// until a later atomic canonical publish.
	var selected map[string]any
	ambiguous := false
	for key, raw := range account.Extra {
		if !strings.HasPrefix(key, CodexTurnStateModelExtraPrefix) || key == CodexTurnStateNativeProvenanceExtraKey {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, CodexTurnStateModelExtraPrefix))
		if err != nil || codexTurnStateOwnerModel(string(decoded)) != owner || string(decoded) == owner {
			continue
		}
		slot, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		slotAccount := *account
		slotAccount.Extra = slot
		verifiedModel := strings.TrimSpace(slotAccount.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
		if codexTurnStateOwnerModel(verifiedModel) != owner || codexTurnStateAutoToken(&slotAccount) == "" {
			continue
		}
		if selected != nil {
			ambiguous = true
			break
		}
		selected = slot
	}
	if ambiguous {
		selected = nil
	}
	copy.Extra = selected
	return &copy
}

func codexTurnStateScopedInfoWithInterval(account *Account, now time.Time, intervalMinutes int) *CodexTurnStateAutoInfo {
	result := &CodexTurnStateAutoInfo{
		Due:              true,
		Models:           make(map[string]CodexTurnStateAutoInfo),
		SuccessfulModels: []string{},
	}
	successfulModels := make(map[string]struct{})
	for key := range account.Extra {
		if !strings.HasPrefix(key, CodexTurnStateModelExtraPrefix) {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, CodexTurnStateModelExtraPrefix))
		if err != nil || len(decoded) == 0 {
			continue
		}
		model := codexTurnStateOwnerModel(string(decoded))
		if _, exists := result.Models[model]; exists {
			continue
		}
		if _, err := NormalizeOpenAICodexTurnStateDefaultModel(model); err != nil {
			continue
		}
		info := codexTurnStateAutoInfoWithInterval(codexTurnStateModelAccount(account, model), now, intervalMinutes)
		info.CollectionSucceeded = info.CollectionSucceeded &&
			codexTurnStateOwnerModel(info.VerifiedModel) == model &&
			!info.RecoveryPending && info.ExpiresAtMS > now.UnixMilli() && info.LastError == ""
		result.Models[model] = info
		if info.CollectionSucceeded {
			successfulModel := strings.TrimSpace(info.VerifiedModel)
			if successfulModel == "" {
				successfulModel = model
			}
			if _, exists := successfulModels[successfulModel]; !exists {
				successfulModels[successfulModel] = struct{}{}
				result.SuccessfulModels = append(result.SuccessfulModels, successfulModel)
			}
		}
		result.Configured = result.Configured || info.Configured
		result.RecoveryPending = result.RecoveryPending || info.RecoveryPending
		if len(result.Models) == 1 {
			result.Due = info.Due
		} else {
			result.Due = result.Due || info.Due
		}
		if info.SetAtMS > result.SetAtMS {
			result.SetAtMS = info.SetAtMS
			result.LastError = info.LastError
		}
		if info.ProbeAtMS > result.ProbeAtMS {
			result.ProbeAtMS = info.ProbeAtMS
		}
		if info.VerifiedAtMS > result.VerifiedAtMS {
			result.VerifiedAtMS = info.VerifiedAtMS
			result.VerifiedModel = info.VerifiedModel
			result.StateLength = info.StateLength
		}
		if info.ProbeNotBeforeMS > result.ProbeNotBeforeMS {
			result.ProbeNotBeforeMS = info.ProbeNotBeforeMS
		}
		if info.InvalidatedAtMS > result.InvalidatedAtMS {
			result.InvalidatedAtMS = info.InvalidatedAtMS
		}
		if info.ExpiresAtMS > 0 && (result.ExpiresAtMS == 0 || info.ExpiresAtMS < result.ExpiresAtMS) {
			result.ExpiresAtMS = info.ExpiresAtMS
		}
	}
	sort.Strings(result.SuccessfulModels)
	result.CollectionSucceeded = len(result.SuccessfulModels) > 0
	return result
}

func codexTurnStateScopedInfo(account *Account, now time.Time) *CodexTurnStateAutoInfo {
	return codexTurnStateScopedInfoWithInterval(account, now, OpenAICodexTurnStateDefaultAutoIntervalMinutes)
}

func (s *OpenAIGatewayService) rememberCodexTurnStateLocked(entry *codexTurnStateAutoEntry, now time.Time) {
	if entry.knownTokens == nil {
		entry.knownTokens = make(map[string]int64)
	}
	for digest, until := range entry.knownTokens {
		if until <= now.UnixMilli() {
			delete(entry.knownTokens, digest)
		}
	}
	expiry := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	if entry.token != "" && expiry > now.UnixMilli() && entry.recovery.allows(entry.token, now) {
		if _, exists := entry.knownTokens[codexTurnStateCanonicalDigest(entry.token)]; !exists && len(entry.knownTokens) >= codexTurnStateSignalHistoryLimit {
			var oldest string
			for digest, until := range entry.knownTokens {
				if oldest == "" || until < entry.knownTokens[oldest] {
					oldest = digest
				}
			}
			delete(entry.knownTokens, oldest)
		}
		entry.knownTokens[codexTurnStateCanonicalDigest(entry.token)] = expiry
	}
}

func (s *OpenAIGatewayService) codexTurnStateModel(ctx context.Context, models ...string) string {
	// Scope matching may use aliases; ownership always uses the final actual
	// upstream model. IP/proxy is deliberately absent from this identity.
	for i := len(models) - 1; i >= 0; i-- {
		if model := strings.TrimSpace(models[i]); model != "" {
			return model
		}
	}
	if ctx != nil {
		if model, _ := ctx.Value(codexTurnStateModelContextKey{}).(string); model != "" {
			return model
		}
	}
	return s.settingService.GetOpenAICodexTurnState(ctx).DefaultModel
}

func withCodexTurnStateModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, codexTurnStateModelContextKey{}, model)
}

func (s *OpenAIGatewayService) refreshCodexTurnStateSource(ctx context.Context, account *Account, model string, force bool) error {
	if s.accountRepo == nil {
		return nil
	}
	model = codexTurnStateOwnerModel(model)
	key := codexTurnStateKey{account.ID, model}
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, time.Now(), model)
	observed := entry.loadedAt
	fresh := time.Since(observed) < 5*time.Second
	s.openaiTurnStateMu.Unlock()
	if fresh && !force {
		return nil
	}
	ch := s.openaiTurnStateLoads.group.DoChan(strings.Join([]string{strconv.FormatInt(account.ID, 10), model}, "\x00"), func() (any, error) {
		s.openaiTurnStateMu.Lock()
		alreadyLoaded := s.openaiTurnStates[key] != nil && s.openaiTurnStates[key].loadedAt.After(observed)
		s.openaiTurnStateMu.Unlock()
		if alreadyLoaded {
			return nil, nil
		}
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
		defer cancel()
		var current *Account
		var err error
		if repo, ok := s.accountRepo.(CodexTurnStateSourceRepository); ok {
			current, err = repo.GetCodexTurnStateSource(dbCtx, account.ID)
		} else {
			current, err = s.accountRepo.GetByID(dbCtx, account.ID)
		}
		if err != nil {
			return nil, errCodexTurnStateLookup
		}
		if !codexTurnStateAutoEligible(current) {
			return nil, errCodexTurnStateLookup
		}
		s.openaiTurnStateMu.Lock()
		s.codexTurnStateEntryLocked(current, time.Now(), model).loadedAt = time.Now()
		s.openaiTurnStateMu.Unlock()
		return nil, nil
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-ch:
		return result.Err
	}
}

func (s *OpenAIGatewayService) prepareCodexTurnStateRequest(ctx context.Context, request *http.Request, account *Account, models ...string) (*http.Request, error) {
	policy := codexTurnStateRequestPolicy{native: request.Header.Get(openAICodexTurnStateHeader), models: append([]string(nil), models...)}
	request = request.WithContext(context.WithValue(request.Context(), codexTurnStateRequestPolicyKey{}, policy))
	request = request.WithContext(withCodexTurnStateModel(request.Context(), s.codexTurnStateModel(ctx, models...)))
	if err := s.applyOpenAICodexTurnState(request.Context(), account, request.Header, models...); err != nil {
		return request, err
	}
	policy.nativeAllowed = strings.TrimSpace(policy.native) != "" &&
		request.Header.Get(openAICodexTurnStateHeader) == policy.native
	request = request.WithContext(context.WithValue(request.Context(), codexTurnStateRequestPolicyKey{}, policy))
	return request, nil
}

// preferOpenAICompatTurnState gives a Messages bridge continuation priority
// over candidate/automatic injection without displacing an accepted client
// native continuation. The final transport refresh validates the selected
// value against the current account/model provenance before it is sent.
func preferOpenAICompatTurnState(request *http.Request, state string) *http.Request {
	if request == nil || request.Header == nil {
		return request
	}
	state = strings.TrimSpace(state)
	if state == "" {
		return request
	}
	policy, ok := request.Context().Value(codexTurnStateRequestPolicyKey{}).(codexTurnStateRequestPolicy)
	if !ok {
		if strings.TrimSpace(request.Header.Get(openAICodexTurnStateHeader)) == "" {
			request.Header.Set(openAICodexTurnStateHeader, state)
		}
		return request
	}
	if policy.nativeAllowed {
		return request
	}
	policy.compat = state
	request.Header.Set(openAICodexTurnStateHeader, state)
	return request.WithContext(context.WithValue(request.Context(), codexTurnStateRequestPolicyKey{}, policy))
}

func (s *OpenAIGatewayService) refreshCodexTurnStateRequest(request *http.Request, account *Account) (*http.Request, error) {
	if request == nil {
		return request, nil
	}
	policy, ok := request.Context().Value(codexTurnStateRequestPolicyKey{}).(codexTurnStateRequestPolicy)
	if !ok {
		return request, nil
	}
	preferred := ""
	if policy.nativeAllowed {
		preferred = policy.native
	} else if policy.compat != "" {
		preferred = policy.compat
	}

	request = request.Clone(request.Context())
	request.Header.Del(openAICodexTurnStateHeader)
	if preferred != "" {
		request.Header.Set(openAICodexTurnStateHeader, preferred)
	}
	return request, s.applyOpenAICodexTurnState(request.Context(), account, request.Header, policy.models...)
}

func (s *OpenAIGatewayService) checkCodexTurnStatePassthrough(ctx context.Context, account *Account, sent, handshakeModel string, models ...string) error {
	if s == nil || !codexTurnStateAutoEligible(account) || !s.codexTurnStateAutoEnabled(ctx) {
		return nil
	}
	model := s.codexTurnStateModel(ctx, models...)
	headers := http.Header{}
	if sent != "" && model == handshakeModel {
		headers.Set(openAICodexTurnStateHeader, sent)
	}
	if err := s.applyOpenAICodexTurnState(ctx, account, headers, models...); err != nil {
		return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "Turn State unavailable; please reconnect", err)
	}
	if headers.Get(openAICodexTurnStateHeader) != sent || sent != "" && model != handshakeModel {
		return NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, "Turn State changed; please reconnect", nil)
	}
	return nil
}
