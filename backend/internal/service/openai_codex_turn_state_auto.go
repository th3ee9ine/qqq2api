package service

// Turn states are opaque, upstream-issued credentials. Generation/renewal means
// a bounded Responses request, never a locally fabricated Fernet signature.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	CodexTurnStateAutoExtraKey               = "codex_turn_state_auto"
	CodexTurnStateAutoSetAtExtraKey          = "codex_turn_state_auto_set_at_ms"
	CodexTurnStateAutoProbeAtExtraKey        = "codex_turn_state_auto_probe_at_ms"
	CodexTurnStateAutoLastErrorExtraKey      = "codex_turn_state_auto_last_error"
	CodexTurnStateAutoVerifiedAtExtraKey     = "codex_turn_state_auto_verified_at_ms"
	CodexTurnStateAutoVerifiedModelExtraKey  = "codex_turn_state_auto_verified_model"
	CodexTurnStateAutoProbeNotBeforeExtraKey = "codex_turn_state_auto_probe_not_before_ms"
	codexTurnStateProbePrompt                = "Reply with exactly OK."
	codexTurnStateAutoProbeInterval          = 5 * time.Minute
	codexTurnStateAutoRenewBefore            = 10 * time.Minute
	codexTurnStateAutoMaxBody                = 128 << 10
	codexTurnStateProbePersistAttempts       = 3
)

// Only fixed error codes are persisted or logged: provider errors can contain
// bearer tokens, proxy credentials, URLs or response bodies.
type codexTurnStateAutoError string

func (e codexTurnStateAutoError) Error() string { return string(e) }

// CodexTurnStateAutoInfo contains no token. Expiry is a reference TTL, not an
// upstream guarantee. Unknown envelopes age from their first collection time.
type CodexTurnStateAutoInfo struct {
	Models           map[string]CodexTurnStateAutoInfo `json:"models,omitempty"`
	Configured       bool                              `json:"configured"`
	SetAtMS          int64                             `json:"set_at_ms,omitempty"`
	ProbeAtMS        int64                             `json:"probe_at_ms,omitempty"`
	VerifiedAtMS     int64                             `json:"verified_at_ms,omitempty"`
	VerifiedModel    string                            `json:"verified_model,omitempty"`
	ProbeNotBeforeMS int64                             `json:"probe_not_before_ms,omitempty"`
	StateLength      int                               `json:"state_length,omitempty"`
	ExpiresAtMS      int64                             `json:"expires_at_ms,omitempty"`
	Due              bool                              `json:"due"`
	LastError        string                            `json:"last_error,omitempty"`
	RecoveryPending  bool                              `json:"recovery_pending"`
	InvalidatedAtMS  int64                             `json:"invalidated_at_ms,omitempty"`
}

type codexTurnStateAutoEntry struct {
	model           string
	requestModel    string
	scopeModels     []string
	loadedAt        time.Time
	knownTokens     map[string]int64
	token           string
	setAt, probeAt  int64
	verifiedAt      int64
	verifiedModel   string
	probeNotBefore  int64
	lastError       string
	lastUsed        time.Time
	dirty, running  bool
	reconciling     bool
	retryAfter      time.Time // persistence retry boundary
	retryWakeAt     time.Time
	probeRetryAfter time.Time
	probeWakeAt     time.Time
	probe           bool
	forceProbe      bool
	// manualProbe marks work explicitly requested by an administrator. Manual
	// collection remains runnable when automatic injection is disabled, while
	// retaining the same bounded worker and probe safeguards.
	manualProbe bool
	recovery    codexTurnStateRecovery
	candidate   codexTurnStateUsageCandidate
	// pendingOwner is retained after a candidate is promoted locally until the
	// corresponding durable state write succeeds.
	pendingOwner codexTurnStateProbeCandidatePendingOwner
}

type CodexTurnStateAtomicRepository interface {
	UpdateCodexTurnState(context.Context, int64, string, map[string]any) (bool, error)
}

type codexTurnStateProbeTask struct {
	requestModel string
	scopeModels  []string
	force        bool
}

func codexTurnStateAutoInt64(account *Account, key string) int64 {
	if account == nil {
		return 0
	}
	switch n := account.Extra[key].(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case json.Number:
		v, _ := n.Int64()
		return v
	case string:
		v, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return v
	default:
		return 0
	}
}
func codexTurnStateAutoToken(account *Account) string {
	if account == nil {
		return ""
	}
	if codexTurnStateAutoInt64(account, CodexTurnStateAutoVerifiedAtExtraKey) <= 0 || strings.TrimSpace(account.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey)) == "" {
		return ""
	}
	token := strings.TrimSpace(account.GetExtraString(CodexTurnStateAutoExtraKey))
	if ValidateOpenAICodexTurnState(token) != nil {
		return ""
	}
	return token
}
func codexTurnStateAutoExpiry(token string, setAt int64, now time.Time) int64 {
	// Never let an implausible future public timestamp extend the local lifetime.
	if issued, _, ok := parseCodexTurnState(token); ok && !issued.After(now.Add(time.Minute)) {
		expiry := issued.Add(codexTurnStateTTL).UnixMilli()
		if setAt <= 0 || expiry < setAt+codexTurnStateTTL.Milliseconds() {
			return expiry
		}
	}
	if setAt > 0 {
		return setAt + codexTurnStateTTL.Milliseconds()
	}
	return 0
}
func codexTurnStateAutoInfo(account *Account, now time.Time) CodexTurnStateAutoInfo {
	token := codexTurnStateAutoToken(account)
	info := CodexTurnStateAutoInfo{
		Configured:       token != "",
		SetAtMS:          codexTurnStateAutoInt64(account, CodexTurnStateAutoSetAtExtraKey),
		ProbeAtMS:        codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeAtExtraKey),
		VerifiedAtMS:     codexTurnStateAutoInt64(account, CodexTurnStateAutoVerifiedAtExtraKey),
		VerifiedModel:    strings.TrimSpace(account.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey)),
		ProbeNotBeforeMS: codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeNotBeforeExtraKey),
	}
	if token != "" {
		info.StateLength = len(token)
	}
	info.ExpiresAtMS = codexTurnStateAutoExpiry(token, info.SetAtMS, now)
	recovery := codexTurnStateRecoveryFromAccount(account)
	info.RecoveryPending, info.InvalidatedAtMS = recovery.Pending, recovery.InvalidatedAtMS
	info.Due = recovery.Pending || !recovery.allows(token, now) || token == "" || info.ExpiresAtMS == 0 || now.Add(codexTurnStateAutoRenewBefore).UnixMilli() >= info.ExpiresAtMS
	if account != nil {
		info.LastError = safeCodexTurnStateAutoError(account.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	}
	return info
}
func CodexTurnStateAutoInfoForAccount(account *Account, now time.Time) *CodexTurnStateAutoInfo {
	if !codexTurnStateAutoEligible(account) {
		return nil
	}
	return codexTurnStateScopedInfo(account, now)
}
func safeCodexTurnStateAutoError(value string) string {
	switch value {
	case "state_312", "recovery_requires_new_292", "account_unavailable", "persistence_failed", "auth_failed", "transport_failed", "timeout", "missing_state", "invalid_state", "empty_response", "request_failed",
		"response_model_mismatch", "response_model_missing", "response_not_completed", "response_failed", "state_replay_failed",
		"probe_boundary_refresh_failed", "probe_burst_exhausted", "probe_burst_persistence_failed", "probe_burst_candidate_pending", "probe_burst_in_flight",
		"usage_log_missing", "usage_request_id_mismatch", "usage_api_key_mismatch", "usage_account_mismatch", "usage_model_mismatch", "usage_state_mismatch", "usage_endpoint_mismatch",
		codexTurnStateProbe429RetryAfterCode, codexTurnStateProbe429NoRetryAfterCode:
		return value
	}
	if strings.HasPrefix(value, "http_") && len(value) == 8 {
		if code, err := strconv.Atoi(value[5:]); err == nil && code >= 100 && code <= 599 {
			return value
		}
	}
	return ""
}
func codexTurnStateAutoEligible(account *Account) bool {
	return account != nil && account.ID > 0 && account.Platform == PlatformOpenAI && account.UsesOpenAICodexProtocol()
}

type CodexTurnStateAccountProbeBoundaryRepository interface {
	AdvanceCodexTurnStateProbeNotBefore(context.Context, int64, int64) error
}

// Caller must hold openaiTurnStateMu. Retry-After is an account quota boundary,
// not a model-slot property: a 429 for Astra must also stop auto-review (and vice
// versa) before either worker selects another exit.
func (s *OpenAIGatewayService) codexTurnStateAccountProbeNotBeforeLocked(accountID int64) int64 {
	var notBefore int64
	for key, candidate := range s.openaiTurnStates {
		if key.accountID == accountID && candidate != nil && candidate.probeNotBefore > notBefore {
			notBefore = candidate.probeNotBefore
		}
	}
	return notBefore
}

func (s *OpenAIGatewayService) codexTurnStateAccountProbeBlocked(accountID int64, now time.Time) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	s.openaiTurnStateMu.Lock()
	notBefore := s.codexTurnStateAccountProbeNotBeforeLocked(accountID)
	s.openaiTurnStateMu.Unlock()
	return notBefore > 0 && now.Before(time.UnixMilli(notBefore))
}

// Refresh the durable account-wide boundary immediately before every upstream
// maintenance request. A worker in another process may have received a 429
// after this process selected its route; the narrow source read closes that
// window before a new exit or same-exit replay is sent.
func (s *OpenAIGatewayService) refreshCodexTurnStateAccountProbeBoundary(ctx context.Context, accountID int64) (bool, error) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return false, nil
	}
	now := time.Now()
	if s.codexTurnStateAccountProbeBlocked(accountID, now) {
		return true, nil
	}

	dbCtx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	var (
		current *Account
		err     error
	)
	if repo, ok := s.accountRepo.(CodexTurnStateSourceRepository); ok {
		current, err = repo.GetCodexTurnStateSource(dbCtx, accountID)
	} else {
		current, err = s.accountRepo.GetByID(dbCtx, accountID)
	}
	if err != nil || current == nil || current.ID != accountID {
		return false, errCodexTurnStateProbeBoundaryRefresh
	}

	durable := codexTurnStateAutoInt64(current, CodexTurnStateAutoProbeNotBeforeExtraKey)
	s.openaiTurnStateMu.Lock()
	if durable > 0 {
		for key, entry := range s.openaiTurnStates {
			if key.accountID != accountID || entry == nil || durable <= entry.probeNotBefore {
				continue
			}
			entry.probeNotBefore = durable
			entry.probeRetryAfter = time.UnixMilli(durable)
			if durable > now.UnixMilli() {
				entry.probe = false
			}
		}
	}
	notBefore := s.codexTurnStateAccountProbeNotBeforeLocked(accountID)
	if durable > notBefore {
		notBefore = durable
	}
	s.openaiTurnStateMu.Unlock()
	return notBefore > 0 && now.Before(time.UnixMilli(notBefore)), nil
}

func (s *OpenAIGatewayService) codexTurnStateAutoEnabled(ctx context.Context) bool {
	return s != nil && s.settingService != nil && s.settingService.GetOpenAICodexTurnState(ctx).AutoEnabled
}

func (s *OpenAIGatewayService) codexTurnStateRuntimeConfig(ctx context.Context) OpenAICodexTurnStateConfig {
	if s == nil {
		return OpenAICodexTurnStateConfig{}
	}
	if s.settingService == nil {
		return OpenAICodexTurnStateConfig{ModelScopeValid: true}
	}
	return s.settingService.GetOpenAICodexTurnState(ctx)
}

func codexTurnStateScopeAllows(cfg OpenAICodexTurnStateConfig, models ...string) bool {
	return cfg.ModelScopeValid && codexTurnStateModelMatches(cfg.Models, models...)
}

func appendCodexTurnStateScopeModels(dst []string, models ...string) []string {
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		seen := false
		for _, existing := range dst {
			if strings.EqualFold(existing, model) {
				seen = true
				break
			}
		}
		if !seen {
			dst = append(dst, model)
		}
	}
	return dst
}

// Caller must hold openaiTurnStateMu. Account/model cache refreshes deliberately
// do not call this helper: requestModel and scopeModels describe the current
// maintenance task, not every request that has read the same owner slot.
func replaceCodexTurnStateProbeModelsLocked(entry *codexTurnStateAutoEntry, requestModel string, scopeModels ...string) bool {
	if entry == nil {
		return false
	}
	requestModel = strings.TrimSpace(requestModel)
	if requestModel == "" || codexTurnStateOwnerModel(requestModel) != entry.model {
		return false
	}
	entry.requestModel = requestModel
	entry.scopeModels = appendCodexTurnStateScopeModels(nil, scopeModels...)
	entry.scopeModels = appendCodexTurnStateScopeModels(entry.scopeModels, requestModel)
	return true
}

// Caller must hold openaiTurnStateMu. The returned slices do not alias entry,
// so later requests can queue a new task without changing an in-flight probe.
func codexTurnStateProbeTaskLocked(entry *codexTurnStateAutoEntry) codexTurnStateProbeTask {
	if entry == nil {
		return codexTurnStateProbeTask{}
	}
	requestModel := strings.TrimSpace(entry.requestModel)
	if requestModel == "" {
		requestModel = entry.model
	}
	models := appendCodexTurnStateScopeModels(nil, entry.scopeModels...)
	models = appendCodexTurnStateScopeModels(models, requestModel)
	return codexTurnStateProbeTask{requestModel: requestModel, scopeModels: models, force: entry.forceProbe}
}

func codexTurnStateProbeTaskAllows(cfg OpenAICodexTurnStateConfig, task codexTurnStateProbeTask) bool {
	return codexTurnStateScopeAllows(cfg, task.scopeModels...)
}

func codexTurnStateEntryScopeAllowed(cfg OpenAICodexTurnStateConfig, entry *codexTurnStateAutoEntry) bool {
	if entry == nil {
		return false
	}
	models := appendCodexTurnStateScopeModels(nil, entry.scopeModels...)
	models = appendCodexTurnStateScopeModels(models, entry.requestModel, entry.model)
	return codexTurnStateScopeAllows(cfg, models...)
}

// Caller must hold openaiTurnStateMu. Requeue an active manual task only when
// no newer task has already been queued on the shared entry.
func requeueCodexTurnStateManualTaskLocked(entry *codexTurnStateAutoEntry, task codexTurnStateProbeTask) bool {
	if entry == nil {
		return false
	}
	if entry.probe {
		return false
	}
	if !replaceCodexTurnStateProbeModelsLocked(entry, task.requestModel, task.scopeModels...) {
		return false
	}
	entry.probe = true
	entry.forceProbe = true
	entry.manualProbe = true
	return true
}

// Caller must hold openaiTurnStateMu. This check may prune a separately staged
// candidate but never mutates the queued task fields on entry.
func (s *OpenAIGatewayService) enforceActiveCodexTurnStateProbeScopeLocked(cfg OpenAICodexTurnStateConfig, task codexTurnStateProbeTask, entry *codexTurnStateAutoEntry) bool {
	if entry != nil && entry.candidate.state != "" && !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
		s.discardCodexTurnStateCandidateLocked(entry)
	}
	return codexTurnStateProbeTaskAllows(cfg, task)
}

// Caller must hold openaiTurnStateMu. The probe task and staged candidate own
// separate model snapshots, so narrowing one alias must not discard work that
// is independently still allowed through another alias of the same owner.
func (s *OpenAIGatewayService) enforceCodexTurnStateScopeLocked(cfg OpenAICodexTurnStateConfig, entry *codexTurnStateAutoEntry) bool {
	if entry == nil {
		return false
	}
	taskAllowed := codexTurnStateEntryScopeAllowed(cfg, entry)
	if !taskAllowed {
		entry.probe = false
		entry.forceProbe = false
		entry.manualProbe = false
	}
	if entry.candidate.state != "" && !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
		s.discardCodexTurnStateCandidateLocked(entry)
	}
	return taskAllowed
}

func (s *OpenAIGatewayService) enforceCodexTurnStateScopeForAccount(cfg OpenAICodexTurnStateConfig, accountID int64, model string) {
	if s == nil || accountID <= 0 {
		return
	}
	s.openaiTurnStateMu.Lock()
	if entry := s.openaiTurnStates[codexTurnStateKey{accountID: accountID, model: codexTurnStateOwnerModel(model)}]; entry != nil {
		s.enforceCodexTurnStateScopeLocked(cfg, entry)
	}
	s.openaiTurnStateMu.Unlock()
}

// releaseCodexTurnStateCandidateOwnerAsync starts ownership cleanup without
// performing repository I/O while openaiTurnStateMu is held.
func (s *OpenAIGatewayService) releaseCodexTurnStateCandidateOwnerAsync(owner codexTurnStateProbeCandidatePendingOwner) {
	if s == nil || owner.version <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), gatewayForwardingDBTimeout)
		_ = s.clearCodexTurnStateProbeCandidatePending(ctx, owner)
		cancel()
	}()
}

// Caller must hold openaiTurnStateMu. Discarding a staged candidate also
// releases its durable cross-instance stopper, but only if this process still
// owns that exact marker.
func (s *OpenAIGatewayService) discardCodexTurnStateCandidateLocked(entry *codexTurnStateAutoEntry) {
	if entry == nil {
		return
	}
	owner := entry.candidate.pendingOwner
	entry.candidate = codexTurnStateUsageCandidate{}
	s.releaseCodexTurnStateCandidateOwnerAsync(owner)
}

func (s *OpenAIGatewayService) finishCodexTurnStateCandidatePersistence(entry *codexTurnStateAutoEntry, owner codexTurnStateProbeCandidatePendingOwner) {
	if owner.version <= 0 || entry == nil {
		return
	}
	s.openaiTurnStateMu.Lock()
	if entry.pendingOwner == owner {
		entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
		s.openaiTurnStateMu.Unlock()
		s.releaseCodexTurnStateCandidateOwnerAsync(owner)
		return
	}
	s.openaiTurnStateMu.Unlock()
}

// Must hold openaiTurnStateMu. Account snapshots are read-only: background
// work never mutates the scheduler/request's Extra or Credentials maps.
func (s *OpenAIGatewayService) codexTurnStateEntryLocked(account *Account, now time.Time, models ...string) *codexTurnStateAutoEntry {
	requestModel := ""
	for i := len(models) - 1; i >= 0; i-- {
		if requestModel = strings.TrimSpace(models[i]); requestModel != "" {
			break
		}
	}
	scopeModels := appendCodexTurnStateScopeModels(nil, models...)
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, requestModel)
	model := codexTurnStateOwnerModel(requestModel)
	key := codexTurnStateKey{account.ID, model}
	accountProbeNotBefore := codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeNotBeforeExtraKey)
	account = codexTurnStateModelAccount(account, model)
	if s.openaiTurnStates == nil {
		s.openaiTurnStates = make(map[codexTurnStateKey]*codexTurnStateAutoEntry)
	}
	if now.Sub(s.openaiTurnStateSweep) >= time.Hour {
		for id, entry := range s.openaiTurnStates {
			if !entry.running && now.Sub(entry.lastUsed) > 2*time.Hour {
				delete(s.openaiTurnStates, id)
			}
		}
		s.openaiTurnStateSweep = now
	}
	entry := s.openaiTurnStates[key]
	if entry == nil {
		entry = &codexTurnStateAutoEntry{model: model, requestModel: requestModel, scopeModels: scopeModels, knownTokens: make(map[string]int64), recovery: codexTurnStateRecoveryFromAccount(account), lastError: safeCodexTurnStateAutoError(account.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))}
		s.openaiTurnStates[key] = entry
	}
	persistedRecovery := codexTurnStateRecoveryFromAccount(account)
	if persistedRecovery.InvalidatedAtMS > entry.recovery.InvalidatedAtMS {
		entry.recovery = persistedRecovery
		entry.token = ""
	}
	setAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoSetAtExtraKey)
	verifiedAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoVerifiedAtExtraKey)
	verifiedModel := codexTurnStateOwnerModel(account.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
	token := codexTurnStateAutoToken(account)
	if verifiedModel != model {
		token = ""
	}
	if !entry.reconciling && !entry.dirty && (verifiedAt > entry.verifiedAt || setAt > entry.setAt && verifiedAt >= entry.verifiedAt || entry.token == "") && entry.recovery.allows(token, now) {
		entry.token, entry.setAt = token, setAt
		entry.verifiedAt, entry.verifiedModel = verifiedAt, verifiedModel
		if entry.recovery.Pending {
			entry.recovery.Pending = false
			entry.dirty = true
		}
	}
	if at := codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeAtExtraKey); at > entry.probeAt {
		entry.probeAt = at
	}
	if notBefore := codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeNotBeforeExtraKey); notBefore > entry.probeNotBefore {
		entry.probeNotBefore = notBefore
	}
	if accountProbeNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = accountProbeNotBefore
	}
	if sharedNotBefore := s.codexTurnStateAccountProbeNotBeforeLocked(key.accountID); sharedNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = sharedNotBefore
	}
	if entry.probeNotBefore > now.UnixMilli() {
		entry.probeRetryAfter = time.UnixMilli(entry.probeNotBefore)
	}
	entry.lastUsed = now
	s.rememberCodexTurnStateLocked(entry, now)
	return entry
}

// codexTurnStateEntryHasValidStateLocked reports whether the exact account/model
// slot already has a usable verified state. Manual collection is deliberately
// stricter than automatic renewal: any still-valid state satisfies the request,
// including one that is inside the automatic renewal window.
func codexTurnStateEntryHasValidStateLocked(entry *codexTurnStateAutoEntry, now time.Time) bool {
	return entry != nil && !entry.reconciling && !entry.recovery.Pending &&
		entry.token != "" && entry.verifiedModel == entry.model &&
		entry.recovery.allows(entry.token, now) &&
		codexTurnStateAutoExpiry(entry.token, entry.setAt, now) > now.UnixMilli()
}

// codexTurnStateManualStateAlreadyValidLocked treats the account snapshot just
// read by the caller as authoritative, then adopts that exact slot into memory.
// This closes both stale-cache invalidation and concurrent-publication races
// before a manual worker reaches the upstream transport.
// Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) codexTurnStateManualStateAlreadyValidLocked(entry *codexTurnStateAutoEntry, account *Account, now time.Time) bool {
	if entry == nil || entry.reconciling || entry.recovery.Pending || account == nil {
		return false
	}
	slot := codexTurnStateModelAccount(account, entry.model)
	if slot == nil {
		return false
	}
	token := codexTurnStateAutoToken(slot)
	verifiedModel := codexTurnStateOwnerModel(slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
	recovery := codexTurnStateRecoveryFromAccount(slot)
	setAt := codexTurnStateAutoInt64(slot, CodexTurnStateAutoSetAtExtraKey)
	if verifiedModel != entry.model || recovery.Pending || recovery.InvalidatedAtMS < entry.recovery.InvalidatedAtMS ||
		!recovery.allows(token, now) || codexTurnStateAutoExpiry(token, setAt, now) <= now.UnixMilli() {
		return false
	}

	// Adopt the authoritative snapshot so subsequent worker iterations cannot
	// fall back to the stale forced entry after this manual request is satisfied.
	entry.token = token
	entry.setAt = setAt
	entry.verifiedAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoVerifiedAtExtraKey)
	entry.verifiedModel = verifiedModel
	entry.recovery = recovery
	if probeAt := codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey); probeAt > entry.probeAt {
		entry.probeAt = probeAt
	}
	if probeNotBefore := codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeNotBeforeExtraKey); probeNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = probeNotBefore
	}
	entry.lastError = safeCodexTurnStateAutoError(slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	entry.loadedAt = now
	if entry.probeNotBefore > now.UnixMilli() {
		entry.probeRetryAfter = time.UnixMilli(entry.probeNotBefore)
	}
	s.rememberCodexTurnStateLocked(entry, now)
	return true
}

// handleCodexTurnStateProbeCandidateLocked consumes an already completed
// collection result before another upstream probe can start. A manual intent
// upgrades the candidate and publishes it locally; automatic work simply exits
// because a second probe would duplicate the same account/model operation.
// Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) handleCodexTurnStateProbeCandidateLocked(cfg OpenAICodexTurnStateConfig, entry *codexTurnStateAutoEntry, now time.Time, manual bool) bool {
	if !s.codexTurnStateUsageCandidateActiveLocked(entry, now) {
		return false
	}
	if !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
		s.discardCodexTurnStateCandidateLocked(entry)
		return false
	}
	manualIntent := manual || entry.candidate.manual
	if manualIntent {
		entry.candidate.manual = true
		if !s.publishManualCodexTurnStateCandidateLocked(entry, now) {
			// Keep the explicit request runnable if publication lost its validity
			// check (for example, a candidate expired at this boundary).
			if !entry.probe {
				entry.probe = true
				entry.forceProbe = true
				entry.manualProbe = true
			}
		}
	}
	return true
}

// loadCodexTurnStateProbeSnapshot reads only the account fields needed to
// decide whether a manual probe is still necessary. It intentionally prefers
// the source repository so credentials are not copied while closing the
// pre-upstream publication race.
func (s *OpenAIGatewayService) loadCodexTurnStateProbeSnapshot(ctx context.Context, accountID int64) (*Account, error) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return nil, errCodexTurnStateLookup
	}
	if ctx == nil {
		ctx = context.Background()
	}
	snapshotCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	defer cancel()
	if repo, ok := s.accountRepo.(CodexTurnStateSourceRepository); ok {
		return repo.GetCodexTurnStateSource(snapshotCtx, accountID)
	}
	return s.accountRepo.GetByID(snapshotCtx, accountID)
}

// Native continuation > account automatic fallback.
// Near-expiry values remain usable while renewal runs; expired automatic values
// are omitted. No probe is awaited by the caller.
func (s *OpenAIGatewayService) autoTurnStateForAccount(ctx context.Context, account *Account, models ...string) string {
	if !codexTurnStateAutoEligible(account) || s == nil || s.settingService == nil {
		return ""
	}
	cfg := s.settingService.GetOpenAICodexTurnState(ctx)
	requestModel := s.codexTurnStateModel(ctx, models...)
	scopeModels := appendCodexTurnStateScopeModels(nil, models...)
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, requestModel)
	if !cfg.AutoEnabled {
		return ""
	}
	if !codexTurnStateScopeAllows(cfg, scopeModels...) {
		s.enforceCodexTurnStateScopeForAccount(cfg, account.ID, requestModel)
		return ""
	}
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	entryModels := append([]string(nil), models...)
	entryModels = append(entryModels, requestModel)
	entry := s.codexTurnStateEntryLocked(account, now, entryModels...)
	if entry.reconciling {
		return ""
	}

	expiry := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	if entry.candidate.state != "" && !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
		s.discardCodexTurnStateCandidateLocked(entry)
	}
	candidatePending := s.codexTurnStateUsageCandidateActiveLocked(entry, now)
	if candidatePending && !entry.manualProbe {
		// A candidate already passed the bounded maintenance round and is waiting
		// for its dedicated usage-evidence request. Do not launch another probe from
		// the very request that reserves it.
		entry.probe = false
		entry.forceProbe = false
	}
	if !codexTurnStateManualVerification(ctx) && !candidatePending && (entry.recovery.Pending || entry.token == "" || expiry == 0 || now.Add(codexTurnStateAutoRenewBefore).UnixMilli() >= expiry) {
		currentTaskAllowed := codexTurnStateEntryScopeAllowed(cfg, entry)
		if !now.Before(time.UnixMilli(entry.probeNotBefore)) && (!currentTaskAllowed || entry.forceProbe || entry.probeAt <= 0 || now.Sub(time.UnixMilli(entry.probeAt)) >= codexTurnStateAutoProbeInterval) {
			entry.probe = true
			if !currentTaskAllowed {
				entry.forceProbe = true
			}
			replaceCodexTurnStateProbeModelsLocked(entry, requestModel, scopeModels...)
		}
	}
	if !codexTurnStateManualVerification(ctx) {
		s.startCodexTurnStateWorkerLocked(account.ID, entry)
	}
	if entry.recovery.Pending || !entry.recovery.allows(entry.token, now) || expiry == 0 || now.UnixMilli() >= expiry {
		return ""
	}
	return entry.token
}

// collectOpenAICodexTurnState is intentionally a compatibility sink for old
// response-header call sites. Neither a response header nor an in-process
// context marker may publish automatic state. The maintenance worker stages its
// candidate directly, and only confirmCodexTurnStateUsageLog can promote it.
func (s *OpenAIGatewayService) collectOpenAICodexTurnState(ctx context.Context, account *Account, state string, sent ...string) {
	s.collectOpenAICodexTurnStateAtEpoch(ctx, account, state, "", sent...)
}
func (s *OpenAIGatewayService) collectOpenAICodexTurnStateAtEpoch(ctx context.Context, account *Account, state, epoch string, sent ...string) {
}
func (s *OpenAIGatewayService) setCodexTurnStateLocked(entry *codexTurnStateAutoEntry, state string, now time.Time) {
	if entry == nil || entry.reconciling || !entry.recovery.allows(state, now) {
		return
	}
	hadProbeBoundary := entry.probeNotBefore > 0
	entry.recovery.Pending = false
	if !entry.probe {
		entry.forceProbe = false
	}
	// A successful state acceptance must not cancel an account-wide 429. The
	// boundary is shared by every model slot and remains authoritative until its
	// timestamp passes; only an already-expired local value can be cleared here.
	if entry.probeNotBefore <= now.UnixMilli() {
		entry.probeNotBefore = 0
	}
	if state == entry.token && entry.setAt > 0 {
		// Successful reuse clears a previous failure without renewing token age.
		if entry.lastError != "" || hadProbeBoundary {
			entry.lastError = ""
			entry.dirty = true
		}
		return
	}
	oldIssued, _, oldOK := parseCodexTurnState(entry.token)
	issued, _, ok := parseCodexTurnState(state)
	if oldOK && ok && !oldIssued.After(now.Add(time.Minute)) && issued.Before(oldIssued) {
		return
	}
	entry.verifiedAt = now.UnixMilli()
	entry.verifiedModel = entry.model
	entry.token, entry.setAt, entry.lastError, entry.dirty = state, now.UnixMilli(), "", true
	s.rememberCodexTurnStateLocked(entry, now)
}
func (s *OpenAIGatewayService) startCodexTurnStateWorkerLocked(id int64, entry *codexTurnStateAutoEntry) {
	if s.accountRepo == nil || entry.running || entry.reconciling || (!entry.dirty && !entry.probe) {
		return
	}
	now := time.Now()
	if entry.dirty && now.Before(entry.retryAfter) {
		return
	}
	if !entry.dirty && entry.probe && now.Before(entry.probeRetryAfter) {
		return
	}
	entry.running = true
	s.openaiTurnStateWorkers++
	go s.runCodexTurnStateWorker(id, entry)
}

// Caller must hold openaiTurnStateMu. Dirty persistence is independent of model
// scope, so a failed write needs its own wakeup even when future model requests
// are correctly rejected before they touch this entry.
func (s *OpenAIGatewayService) scheduleCodexTurnStatePersistenceRetryLocked(id int64, entry *codexTurnStateAutoEntry, retryAt time.Time) {
	if s == nil || entry == nil || retryAt.IsZero() {
		return
	}
	if !entry.retryWakeAt.IsZero() && !retryAt.Before(entry.retryWakeAt) {
		return
	}
	entry.retryWakeAt = retryAt
	key := codexTurnStateKey{accountID: id, model: entry.model}
	go func(expected time.Time) {
		if delay := time.Until(expected); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			<-timer.C
		}
		cfg := s.codexTurnStateRuntimeConfig(context.Background())
		s.openaiTurnStateMu.Lock()
		defer s.openaiTurnStateMu.Unlock()
		if s.openaiTurnStates[key] != entry || !entry.retryWakeAt.Equal(expected) {
			return
		}
		entry.retryWakeAt = time.Time{}
		if entry.running {
			return
		}
		s.enforceCodexTurnStateScopeLocked(cfg, entry)
		s.startCodexTurnStateWorkerLocked(id, entry)
		if entry.dirty && !entry.running && time.Now().Before(entry.retryAfter) {
			s.scheduleCodexTurnStatePersistenceRetryLocked(id, entry, entry.retryAfter)
		}
	}(retryAt)
}

// Caller must hold openaiTurnStateMu. Manual transport/source deferrals need a
// wakeup independent of request traffic; otherwise a queued administrator
// action could remain dormant after the worker exits.
func (s *OpenAIGatewayService) scheduleCodexTurnStateProbeRetryLocked(id int64, entry *codexTurnStateAutoEntry, retryAt time.Time) {
	if s == nil || entry == nil || retryAt.IsZero() {
		return
	}
	if !entry.probeWakeAt.IsZero() && !retryAt.Before(entry.probeWakeAt) {
		return
	}
	entry.probeWakeAt = retryAt
	key := codexTurnStateKey{accountID: id, model: entry.model}
	go func(expected time.Time) {
		if delay := time.Until(expected); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			<-timer.C
		}
		cfg := s.codexTurnStateRuntimeConfig(context.Background())
		s.openaiTurnStateMu.Lock()
		defer s.openaiTurnStateMu.Unlock()
		if s.openaiTurnStates[key] != entry || !entry.probeWakeAt.Equal(expected) {
			return
		}
		entry.probeWakeAt = time.Time{}
		if entry.running {
			return
		}
		s.enforceCodexTurnStateScopeLocked(cfg, entry)
		if cfg.AutoEnabled || entry.manualProbe {
			s.startCodexTurnStateWorkerLocked(id, entry)
		}
		if entry.probe && entry.manualProbe && !entry.running && time.Now().Before(entry.probeRetryAfter) {
			s.scheduleCodexTurnStateProbeRetryLocked(id, entry, entry.probeRetryAfter)
		}
	}(retryAt)
}

func (s *OpenAIGatewayService) runCodexTurnStateWorker(id int64, entry *codexTurnStateAutoEntry) {
	defer func() {
		cfg := s.codexTurnStateRuntimeConfig(context.Background())
		s.openaiTurnStateMu.Lock()
		defer s.openaiTurnStateMu.Unlock()
		entry.running = false
		s.openaiTurnStateWorkers--
		inScope := s.enforceCodexTurnStateScopeLocked(cfg, entry)
		if entry.dirty || inScope && (cfg.AutoEnabled || entry.manualProbe) {
			s.startCodexTurnStateWorkerLocked(id, entry)
		}
		if entry.dirty && !entry.running && time.Now().Before(entry.retryAfter) {
			s.scheduleCodexTurnStatePersistenceRetryLocked(id, entry, entry.retryAfter)
		}
		if entry.probe && entry.manualProbe && !entry.running && time.Now().Before(entry.probeRetryAfter) {
			s.scheduleCodexTurnStateProbeRetryLocked(id, entry, entry.probeRetryAfter)
		}
	}()
	// Bound repeated persistence churn for one account/model. Independent slots
	// are not globally throttled; manual work keeps running when auto mode is off.
	for n := 0; n < 16; n++ {
		// Settings may require a cache fill or database read. Never perform that
		// work while holding the Turn State map lock.
		cfg := s.codexTurnStateRuntimeConfig(context.Background())
		s.openaiTurnStateMu.Lock()
		s.enforceCodexTurnStateScopeLocked(cfg, entry)
		dirty, probe, manualRun := entry.dirty, entry.probe, entry.manualProbe
		task := codexTurnStateProbeTaskLocked(entry)
		if !dirty && !cfg.AutoEnabled && !manualRun {
			s.openaiTurnStateMu.Unlock()
			return
		}
		entry.dirty = false
		if cfg.AutoEnabled || manualRun {
			entry.probe = false
			entry.forceProbe = false
			entry.manualProbe = false
		} else {
			// Disabling automatic work does not discard an already durable-intent
			// write. Persist dirty state, but leave any automatic probe queued.
			probe = false
		}
		s.openaiTurnStateMu.Unlock()
		if !dirty && !probe {
			return
		}
		if dirty {
			err := s.persistCodexTurnStateWithMode(id, entry, manualRun)
			if err != nil {
				s.openaiTurnStateMu.Lock()
				entry.dirty = true
				newerQueued := entry.probe
				if !newerQueued {
					entry.probe = probe
					if manualRun {
						entry.forceProbe = true
						entry.manualProbe = true
					}
				}
				manualRetry := entry.manualProbe
				if manualRun && !newerQueued {
					manualRetry = true
				}
				entry.manualProbe = manualRetry
				retryAt := time.Now().Add(5 * time.Second)
				entry.retryAfter = retryAt
				s.scheduleCodexTurnStatePersistenceRetryLocked(id, entry, retryAt)
				s.openaiTurnStateMu.Unlock()
				slog.Warn("openai_codex_turn_state_collect_failed", "account_id", id, "code", "persistence_failed")
				return
			}
		}
		if probe {
			s.runCodexTurnStateProbeTaskWithMode(id, entry, manualRun, task)
			cfg = s.codexTurnStateRuntimeConfig(context.Background())
			s.openaiTurnStateMu.Lock()
			s.enforceCodexTurnStateScopeLocked(cfg, entry)
			now := time.Now()
			persistenceDeferred := entry.dirty && now.Before(entry.retryAfter)
			probeDeferred := entry.probe && entry.probeNotBefore <= now.UnixMilli() && now.Before(entry.probeRetryAfter)
			s.openaiTurnStateMu.Unlock()
			if persistenceDeferred {
				return
			}
			if probeDeferred {
				return
			}
		}
	}
}
func (s *OpenAIGatewayService) persistCodexTurnState(id int64, entry *codexTurnStateAutoEntry) error {
	return s.persistCodexTurnStateWithMode(id, entry, false)
}

func (s *OpenAIGatewayService) persistCodexTurnStateWithMode(id int64, entry *codexTurnStateAutoEntry, manual bool) error {
	s.openaiTurnStateMu.Lock()
	accountProbeNotBefore := s.codexTurnStateAccountProbeNotBeforeLocked(id)
	pendingOwner := entry.pendingOwner
	updates := map[string]any{
		CodexTurnStateAutoProbeNotBeforeExtraKey: accountProbeNotBefore,
		codexTurnStateModelExtraKey(entry.model): map[string]any{
			CodexTurnStateAutoExtraKey:               entry.token,
			CodexTurnStateAutoSetAtExtraKey:          entry.setAt,
			CodexTurnStateAutoProbeAtExtraKey:        entry.probeAt,
			CodexTurnStateAutoLastErrorExtraKey:      entry.lastError,
			CodexTurnStateAutoVerifiedAtExtraKey:     entry.verifiedAt,
			CodexTurnStateAutoVerifiedModelExtraKey:  entry.verifiedModel,
			CodexTurnStateAutoProbeNotBeforeExtraKey: accountProbeNotBefore,
			CodexTurnStateAutoRecoveryExtraKey:       entry.recovery.clone(),
		},
	}
	s.openaiTurnStateMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if accountProbeNotBefore > time.Now().UnixMilli() {
		if repo, ok := s.accountRepo.(CodexTurnStateAccountProbeBoundaryRepository); ok {
			if err := repo.AdvanceCodexTurnStateProbeNotBefore(ctx, id, accountProbeNotBefore); err != nil {
				return err
			}
		}
	}
	if repo, ok := s.accountRepo.(CodexTurnStateAtomicRepository); ok {
		updated, err := repo.UpdateCodexTurnState(ctx, id, codexTurnStateModelExtraKey(entry.model), updates[codexTurnStateModelExtraKey(entry.model)].(map[string]any))
		if err != nil {
			return err
		}
		if !updated {
			s.openaiTurnStateMu.Lock()
			// Restore the captured manual intent only when no newer task was queued
			// during the CAS. Never upgrade a newer automatic task into manual work.
			if manual && !entry.probe {
				entry.probe = true
				entry.forceProbe = true
				entry.manualProbe = true
			}
			s.beginCodexTurnStateCASReconciliationLocked(entry)
			s.openaiTurnStateMu.Unlock()

			current, loadErr := s.loadCodexTurnStateCASWinner(ctx, id)
			if loadErr == nil && (current == nil || current.ID != id || !codexTurnStateAutoEligible(current)) {
				loadErr = errCodexTurnStateLookup
			}
			now := time.Now()
			s.openaiTurnStateMu.Lock()
			defer s.openaiTurnStateMu.Unlock()
			if loadErr != nil {
				s.finishCodexTurnStateCASReconciliationLocked(entry, nil, now)
				return loadErr
			}
			s.finishCodexTurnStateCASReconciliationLocked(entry, current, now)
			return nil
		}
		s.finishCodexTurnStateCandidatePersistence(entry, pendingOwner)
		return nil
	}
	err := s.accountRepo.UpdateExtra(ctx, id, updates)
	if err == nil {
		s.finishCodexTurnStateCandidatePersistence(entry, pendingOwner)
	}
	return err
}

// Probe failures may carry an account-wide Retry-After boundary. Retry only the
// database write a bounded number of times; never schedule another upstream
// request as a side effect. If every attempt fails, retain dirty work for a
// later request while the in-memory boundary continues to block probing.
func (s *OpenAIGatewayService) persistCodexTurnStateProbeOutcome(id int64, entry *codexTurnStateAutoEntry, manual bool) bool {
	for attempt := 0; attempt < codexTurnStateProbePersistAttempts; attempt++ {
		if err := s.persistCodexTurnStateWithMode(id, entry, manual); err == nil {
			return true
		}
	}
	s.openaiTurnStateMu.Lock()
	entry.dirty = true
	entry.retryAfter = time.Now().Add(5 * time.Second)
	s.scheduleCodexTurnStatePersistenceRetryLocked(id, entry, entry.retryAfter)
	s.openaiTurnStateMu.Unlock()
	return false
}

// A zero-row CAS means another process owns the authoritative slot. Block all
// automatic/candidate injection while that winner is read back; otherwise the
// losing process can keep serving its local value during the database round trip.
func (s *OpenAIGatewayService) beginCodexTurnStateCASReconciliationLocked(entry *codexTurnStateAutoEntry) {
	pendingOwner := entry.pendingOwner
	entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
	s.releaseCodexTurnStateCandidateOwnerAsync(pendingOwner)
	entry.reconciling = true
	entry.token, entry.setAt = "", 0
	entry.verifiedAt, entry.verifiedModel = 0, ""
	s.discardCodexTurnStateCandidateLocked(entry)
	entry.knownTokens = make(map[string]int64)
	// Preserve any newer task queued while the failed CAS was in flight. The
	// reconciliation flag prevents it from running until the authoritative slot
	// has been loaded.
	entry.dirty = false
}

func (s *OpenAIGatewayService) loadCodexTurnStateCASWinner(ctx context.Context, id int64) (*Account, error) {
	if repo, ok := s.accountRepo.(CodexTurnStateSourceRepository); ok {
		return repo.GetCodexTurnStateSource(ctx, id)
	}
	return s.accountRepo.GetByID(ctx, id)
}

// Caller must hold openaiTurnStateMu. A failed read remains fail-closed: no
// local candidate is restored merely because the authoritative value could not
// be loaded before the bounded context expired.
func (s *OpenAIGatewayService) finishCodexTurnStateCASReconciliationLocked(entry *codexTurnStateAutoEntry, current *Account, now time.Time) {
	queuedProbe, queuedForce, queuedManual := entry.probe, entry.forceProbe, entry.manualProbe
	pendingOwner := entry.pendingOwner
	entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
	s.releaseCodexTurnStateCandidateOwnerAsync(pendingOwner)
	entry.reconciling = false
	entry.token, entry.setAt = "", 0
	entry.verifiedAt, entry.verifiedModel = 0, ""
	s.discardCodexTurnStateCandidateLocked(entry)
	entry.knownTokens = make(map[string]int64)
	entry.recovery = codexTurnStateRecovery{}
	entry.probeAt, entry.probeNotBefore, entry.lastError = 0, 0, ""
	entry.dirty, entry.probe, entry.forceProbe, entry.manualProbe = false, queuedProbe, queuedForce, queuedManual
	entry.retryAfter, entry.retryWakeAt = time.Time{}, time.Time{}
	entry.probeRetryAfter, entry.probeWakeAt = time.Time{}, time.Time{}
	entry.loadedAt = now
	if current == nil {
		return
	}

	accountProbeNotBefore := codexTurnStateAutoInt64(current, CodexTurnStateAutoProbeNotBeforeExtraKey)
	slot := codexTurnStateModelAccount(current, entry.model)
	if slot != nil {
		entry.setAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoSetAtExtraKey)
		entry.verifiedAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoVerifiedAtExtraKey)
		entry.verifiedModel = codexTurnStateOwnerModel(slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
		entry.token = codexTurnStateAutoToken(slot)
		if entry.verifiedModel != entry.model {
			entry.token = ""
		}
		entry.recovery = codexTurnStateRecoveryFromAccount(slot)
		entry.probeNotBefore = codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeNotBeforeExtraKey)
		entry.probeAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey)
		entry.lastError = safeCodexTurnStateAutoError(slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	}
	if accountProbeNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = accountProbeNotBefore
	}
	if sharedNotBefore := s.codexTurnStateAccountProbeNotBeforeLocked(current.ID); sharedNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = sharedNotBefore
	}
	if entry.probeNotBefore > now.UnixMilli() {
		entry.probeRetryAfter = time.UnixMilli(entry.probeNotBefore)
	}
	if codexTurnStateEntryHasValidStateLocked(entry, now) {
		entry.probe = false
		entry.forceProbe = false
		entry.manualProbe = false
	}
	s.rememberCodexTurnStateLocked(entry, now)
}
func (s *OpenAIGatewayService) runCodexTurnStateProbe(id int64, entry *codexTurnStateAutoEntry) {
	s.runCodexTurnStateProbeWithMode(id, entry, false)
}

// runCodexTurnStateProbeWithMode performs the bounded maintenance probe. The
// manual mode bypasses the automatic-enable gate. Protocol/account eligibility,
// configured model scope, account-wide upstream cooldowns, response validation
// and CAS publication are unchanged; schedulability, proxy record state and
// serving concurrency are deliberately not consulted.
func (s *OpenAIGatewayService) runCodexTurnStateProbeWithMode(id int64, entry *codexTurnStateAutoEntry, manual bool) {
	s.openaiTurnStateMu.Lock()
	task := codexTurnStateProbeTaskLocked(entry)
	s.openaiTurnStateMu.Unlock()
	s.runCodexTurnStateProbeTaskWithMode(id, entry, manual, task)
}

func (s *OpenAIGatewayService) runCodexTurnStateProbeTaskWithMode(id int64, entry *codexTurnStateAutoEntry, manual bool, task codexTurnStateProbeTask) {
	if s.httpUpstream == nil {
		return
	}
	var pendingOwner codexTurnStateProbeCandidatePendingOwner
	pendingOwnerOwned := false
	defer func() {
		if pendingOwnerOwned {
			s.releaseCodexTurnStateCandidateOwnerAsync(pendingOwner)
		}
	}()
	lookupCtx, cancelLookup := context.WithTimeout(context.Background(), gatewayForwardingDBTimeout)
	account, err := s.accountRepo.GetByID(lookupCtx, id)
	cancelLookup()
	if err != nil {
		s.openaiTurnStateMu.Lock()
		manualIntent := manual
		if manualIntent && !errors.Is(err, ErrAccountNotFound) {
			if requeueCodexTurnStateManualTaskLocked(entry, task) {
				entry.probeRetryAfter = time.Now().Add(5 * time.Second)
				s.scheduleCodexTurnStateProbeRetryLocked(id, entry, entry.probeRetryAfter)
			}
		}
		s.openaiTurnStateMu.Unlock()
		return
	}
	if !codexTurnStateAutoEligible(account) {
		return
	}
	// Re-read persisted timestamps before spending upstream quota. This reduces
	// cross-instance duplicates; it is deliberately not a distributed lock.
	cfg := s.codexTurnStateRuntimeConfig(context.Background())
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	s.codexTurnStateEntryLocked(account, now, entry.model)
	if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	manualIntent := manual
	if manualIntent && s.codexTurnStateManualStateAlreadyValidLocked(entry, account, now) {
		// A concurrent publisher won the exact account/model slot. Do not let
		// forceProbe turn a satisfied manual request into another upstream call.
		s.openaiTurnStateMu.Unlock()
		return
	}
	if s.handleCodexTurnStateProbeCandidateLocked(cfg, entry, now, manualIntent) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	// startCodexTurnStateWorkerLocked checks this boundary before launching a
	// worker. Check it again here because a concurrent invalidation can enqueue a
	// forced probe while the current worker is still handling the 429 response.
	durableCooldown := now.Before(time.UnixMilli(entry.probeNotBefore))
	if now.Before(entry.probeRetryAfter) || durableCooldown {
		if manualIntent && !durableCooldown {
			if requeueCodexTurnStateManualTaskLocked(entry, task) {
				s.scheduleCodexTurnStateProbeRetryLocked(id, entry, entry.probeRetryAfter)
			}
		}
		s.openaiTurnStateMu.Unlock()
		return
	}
	expiry := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	if !manualIntent && !task.force && ((!entry.recovery.Pending && entry.token != "" && expiry > now.Add(codexTurnStateAutoRenewBefore).UnixMilli()) || (entry.probeAt > 0 && now.Sub(time.UnixMilli(entry.probeAt)) < codexTurnStateAutoProbeInterval)) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	entry.probeAt = now.UnixMilli()
	generation := entry.recovery.InvalidatedAtMS
	before := entry.token
	// The durable burst budget limits background retries. An explicit
	// administrator request bypasses that automatic budget; response evidence,
	// account-wide 429 cooldowns and final CAS publication still apply.
	burst := !manualIntent && codexTurnStateProbeNeedsBurst(entry, now)
	burstGeneration := codexTurnStateProbeBurstGeneration(entry)
	burstModel := codexTurnStateProbeBurstOwnerModel(entry.model)
	probeModel := task.requestModel
	if probeModel == "" {
		probeModel = entry.model
	}
	s.openaiTurnStateMu.Unlock()
	totalTimeout := codexTurnStateProbeTotalTimeout
	if burst {
		totalTimeout = codexTurnStateProbeBurstWindow
	}
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()
	if !manual && !s.codexTurnStateAutoEnabled(ctx) {
		return
	}
	cfg = s.codexTurnStateRuntimeConfig(ctx)
	s.openaiTurnStateMu.Lock()
	if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	s.openaiTurnStateMu.Unlock()
	// Probe metadata persistence is database-only. A storage failure must stop
	// before any upstream request and leave bounded retry work behind.
	manualIntent = manual
	if !s.persistCodexTurnStateProbeOutcome(id, entry, manualIntent) {
		if manualIntent {
			s.openaiTurnStateMu.Lock()
			requeueCodexTurnStateManualTaskLocked(entry, task)
			s.openaiTurnStateMu.Unlock()
		}
		return
	}
	// A concurrent publisher can win while probe metadata is being persisted.
	// Refresh the exact slot immediately before selecting the first upstream
	// route; manual work fails closed if that authoritative read is unavailable.
	stateSnapshot := account
	manualIntent = manual
	if manualIntent {
		stateSnapshot, err = s.loadCodexTurnStateProbeSnapshot(ctx, id)
		if err != nil || !codexTurnStateAutoEligible(stateSnapshot) {
			s.openaiTurnStateMu.Lock()
			if requeueCodexTurnStateManualTaskLocked(entry, task) {
				entry.probeRetryAfter = time.Now().Add(5 * time.Second)
				s.scheduleCodexTurnStateProbeRetryLocked(id, entry, entry.probeRetryAfter)
			}
			s.openaiTurnStateMu.Unlock()
			return
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		now = time.Now()
		s.codexTurnStateEntryLocked(stateSnapshot, now, entry.model)
		if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		manualIntent = manual
		if manualIntent && s.codexTurnStateManualStateAlreadyValidLocked(entry, stateSnapshot, now) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		if s.handleCodexTurnStateProbeCandidateLocked(cfg, entry, now, manualIntent) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		s.openaiTurnStateMu.Unlock()
	}
	// A dedicated route is preferred when configured. Collection is not blocked
	// by proxy inventory or route shape: fall back to the account route, then
	// direct transport when neither is available.
	primaryRoute := codexTurnStateAccountProxy(account)
	routes := s.codexTurnStatePoolRoutes(ctx, primaryRoute)
	if len(routes) == 0 {
		routes = []string{primaryRoute}
	}
	for attempt := 0; attempt < len(routes); attempt++ {
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		if ctx.Err() != nil || !manual && !cfg.AutoEnabled {
			return
		}
		s.openaiTurnStateMu.Lock()
		now := time.Now()
		if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		manualIntent = manual
		if manualIntent && s.codexTurnStateManualStateAlreadyValidLocked(entry, stateSnapshot, now) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		if s.handleCodexTurnStateProbeCandidateLocked(cfg, entry, now, manualIntent) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		stale := entry.token != before || entry.recovery.InvalidatedAtMS != generation
		blocked := now.Before(time.UnixMilli(s.codexTurnStateAccountProbeNotBeforeLocked(id)))
		s.openaiTurnStateMu.Unlock()
		if stale || blocked {
			return
		}
		attemptBurst := burst && !manualIntent
		attemptDeadline := time.Time{}
		reservation := codexTurnStateProbeBurstReservation{}
		if attemptBurst {
			var reserveErr error
			reservation, reserveErr = s.reserveCodexTurnStateProbeBurstAttempt(ctx, id, burstModel, burstGeneration)
			if reserveErr != nil {
				if errors.Is(reserveErr, errCodexTurnStateProbeBurstInFlight) || errors.Is(reserveErr, errCodexTurnStateProbeCandidatePending) {
					return
				}
				err = reserveErr
				break
			}
			attemptDeadline = reservation.deadline
		}
		// Reserving the durable burst lease can itself take long enough for an
		// administrator to narrow the model scope. Recheck immediately before the
		// real upstream request and release only this attempt's lease on rejection.
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		inScope := s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry)
		s.openaiTurnStateMu.Unlock()
		if !inScope {
			if attemptBurst {
				releaseCtx, cancelRelease := context.WithTimeout(context.Background(), gatewayForwardingDBTimeout)
				_ = s.releaseCodexTurnStateProbeBurstAttempt(releaseCtx, id, burstModel, burstGeneration, reservation)
				cancelRelease()
			}
			return
		}
		// Renewal and every IP retry must use the model that owns this state.
		model := probeModel
		attemptCtx, cancelAttempt := codexTurnStateProbeAttemptContext(ctx, attemptDeadline)
		state, probeErr := s.probeOpenAICodexTurnStateViaProxy(attemptCtx, account, model, routes[attempt])
		cancelAttempt()
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		inScope = s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry)
		s.openaiTurnStateMu.Unlock()
		if !inScope {
			if attemptBurst {
				releaseCtx, cancelRelease := context.WithTimeout(context.Background(), gatewayForwardingDBTimeout)
				_ = s.releaseCodexTurnStateProbeBurstAttempt(releaseCtx, id, burstModel, burstGeneration, reservation)
				cancelRelease()
			}
			return
		}
		if errors.Is(probeErr, errCodexTurnStateAccountProbeCooldown) {
			return
		}
		// The last same-route replay can overlap a 429 observed by another
		// process. Refresh once more before staging so removal of the former daily
		// route replay does not reopen that cross-instance race.
		if probeErr == nil {
			blocked, boundaryErr := s.refreshCodexTurnStateAccountProbeBoundary(ctx, id)
			if boundaryErr != nil {
				probeErr = boundaryErr
			} else if blocked {
				return
			}
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		inScope = s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry)
		s.openaiTurnStateMu.Unlock()
		if !inScope {
			if attemptBurst {
				releaseCtx, cancelRelease := context.WithTimeout(context.Background(), gatewayForwardingDBTimeout)
				_ = s.releaseCodexTurnStateProbeBurstAttempt(releaseCtx, id, burstModel, burstGeneration, reservation)
				cancelRelease()
			}
			return
		}
		if !manual && !s.codexTurnStateAutoEnabled(ctx) {
			return
		}
		if probeErr == nil && attemptBurst {
			// Recheck the local/account boundary before publishing the durable
			// cross-instance stopper. A competing winner after this check is still
			// resolved by the budget CAS below; only that winner may stage state.
			s.openaiTurnStateMu.Lock()
			now = time.Now()
			stale = entry.token != before || entry.recovery.InvalidatedAtMS != generation
			blocked = now.Before(time.UnixMilli(s.codexTurnStateAccountProbeNotBeforeLocked(id)))
			s.openaiTurnStateMu.Unlock()
			if stale || blocked {
				return
			}
			var won bool
			var pendingErr error
			won, pendingOwner, pendingErr = s.markCodexTurnStateProbeCandidatePendingOwned(ctx, id, burstModel, burstGeneration, reservation)
			if pendingErr != nil {
				probeErr = pendingErr
			} else if !won {
				// Another instance already staged the first valid candidate for this
				// model/generation. Discard this opaque blob without persisting it.
				return
			}
			pendingOwnerOwned = true
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		now = time.Now()
		if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		if entry.token != before || entry.recovery.InvalidatedAtMS != generation ||
			now.Before(time.UnixMilli(s.codexTurnStateAccountProbeNotBeforeLocked(id))) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		err = probeErr
		if err == nil {
			// Automatic collection stages a candidate for its dedicated usage
			// acceptance request. Manual collection promotes the same maintenance
			// evidence immediately below, then persists through the same CAS path.
			manualIntent = manual
			collectedAt := time.UnixMilli(entry.probeAt)
			if collectedAt.IsZero() || collectedAt.After(now) {
				collectedAt = now
			}
			staged := s.stageCodexTurnStateUsageCandidateWithScopeLocked(entry, state, generation, collectedAt, manualIntent, task.scopeModels)
			if staged && pendingOwnerOwned {
				entry.candidate.pendingOwner = pendingOwner
				pendingOwnerOwned = false
			}
			published := staged
			if manualIntent {
				published = staged && s.publishManualCodexTurnStateCandidateLocked(entry, now)
				if !published {
					entry.lastError = "invalid_state"
					entry.dirty = true
					requeueCodexTurnStateManualTaskLocked(entry, task)
					entry.retryAfter = time.Time{}
				}
			}
			s.openaiTurnStateMu.Unlock()
			return
		}
		var httpErr *codexTurnStateProbeHTTPError
		if errors.As(err, &httpErr) {
			delay := httpErr.retryAfter
			if delay <= 0 {
				delay = codexTurnStateProbe429Fallback
			}
			if retryAfter := now.Add(delay); retryAfter.After(entry.probeRetryAfter) {
				entry.probeRetryAfter = retryAfter
			}
			if notBefore := now.Add(delay).UnixMilli(); notBefore > entry.probeNotBefore {
				entry.probeNotBefore = notBefore
			}
		}
		s.openaiTurnStateMu.Unlock()
		// Authentication, quota and invalid payload failures are not route
		// failures. Preserve the upstream rejection rather than rotate around it.
		retryable := codexTurnStateProbeRetryable(err) && ctx.Err() == nil
		if retryable && attemptBurst {
			if releaseErr := s.releaseCodexTurnStateProbeBurstAttempt(ctx, id, burstModel, burstGeneration, reservation); releaseErr != nil {
				err = releaseErr
				retryable = false
			}
		}
		if !retryable {
			break
		}
	}
	cfg = s.codexTurnStateRuntimeConfig(context.Background())
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	if !codexTurnStateProbeTaskAllows(cfg, task) || entry.token != before || entry.recovery.InvalidatedAtMS != generation {
		return
	}
	code := "request_failed"
	if err != nil {
		code = safeCodexTurnStateAutoError(err.Error())
	}
	if code == "" {
		code = "request_failed"
	}
	entry.lastError = code
	// This worker owns persistence; release the cache mutex during DB I/O.
	s.openaiTurnStateMu.Unlock()
	if !s.persistCodexTurnStateProbeOutcome(id, entry, manual) {
		slog.Warn("openai_codex_turn_state_probe_persist_failed", "account_id", id, "code", "persistence_failed")
	}
	slog.Warn("openai_codex_turn_state_probe_failed", "account_id", id, "code", code)
	s.openaiTurnStateMu.Lock()
}

func (s *OpenAIGatewayService) probeOpenAICodexTurnState(ctx context.Context, account *Account, model string) (string, error) {
	return s.probeOpenAICodexTurnStateViaProxy(ctx, account, model, codexTurnStateAccountProxy(account))
}

type codexTurnStateProbeHTTPError struct {
	code       string
	retryAfter time.Duration
}

var (
	errCodexTurnStateAccountProbeCooldown = errors.New("account_probe_cooldown")
	errCodexTurnStateProbeBoundaryRefresh = codexTurnStateAutoError("probe_boundary_refresh_failed")
)

func (e *codexTurnStateProbeHTTPError) Error() string {
	if e == nil || e.code == "" {
		return "request_failed"
	}
	return e.code
}

func (s *OpenAIGatewayService) probeOpenAICodexTurnStateViaProxy(ctx context.Context, account *Account, model, proxyURL string) (string, error) {
	state, _, err := s.requestOpenAICodexTurnStateViaProxy(ctx, account, model, proxyURL, "", true)
	if err != nil {
		return state, err
	}
	if s.codexTurnStateAccountProbeBlocked(account.ID, time.Now()) {
		return "", errCodexTurnStateAccountProbeCooldown
	}
	// The candidate is not cacheable until the exact blob succeeds in a new
	// request on the same sticky route with the same account and model.
	if _, _, err = s.requestOpenAICodexTurnStateViaProxy(ctx, account, model, proxyURL, state, false); err != nil {
		if errors.Is(err, errCodexTurnStateAccountProbeCooldown) || errors.Is(err, errCodexTurnStateProbeBoundaryRefresh) {
			return "", err
		}
		var httpErr *codexTurnStateProbeHTTPError
		if errors.As(err, &httpErr) {
			// A replay can itself be rate limited. Preserve the typed 429 so the
			// worker stops route rotation and installs the upstream retry boundary.
			return "", httpErr
		}
		if errors.Is(err, errCodexTurnStateResponseModelMismatch) || errors.Is(err, errCodexTurnStateResponseModelMissing) ||
			errors.Is(err, errCodexTurnStateResponseNotCompleted) || errors.Is(err, errCodexTurnStateResponseFailed) {
			return "", err
		}
		return "", codexTurnStateAutoError("state_replay_failed")
	}
	return state, nil
}

func (s *OpenAIGatewayService) requestOpenAICodexTurnStateViaProxy(ctx context.Context, account *Account, model, proxyURL, sentState string, requireState bool) (string, *upstreamResponseModelObserver, error) {
	if s == nil || s.httpUpstream == nil || !codexTurnStateAutoEligible(account) {
		return "", nil, codexTurnStateAutoError("account_unavailable")
	}
	blocked, err := s.refreshCodexTurnStateAccountProbeBoundary(ctx, account.ID)
	if err != nil {
		return "", nil, err
	}
	if blocked {
		return "", nil, errCodexTurnStateAccountProbeCooldown
	}
	if strings.TrimSpace(model) == "" {
		model = s.settingService.GetOpenAICodexTurnState(ctx).DefaultModel
	}
	payload := createOpenAICodexTurnStateProbePayload(model)
	payloadBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexAPIURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", nil, codexTurnStateAutoError("request_failed")
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return "", nil, codexTurnStateAutoError("auth_failed")
	}
	applyOpenAIAccountTestHeaders(req, account, "responses", payloadBytes)
	// Setup tokens use exactly the same Codex endpoint and identity as OAuth.
	req.Host = "chatgpt.com"
	setOpenAIChatGPTAccountHeaders(req.Header, account)
	enforceCodexIdentityHeadersWithAccount(req.Header, account)
	auth, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return "", nil, codexTurnStateAutoError("auth_failed")
	}
	for name, values := range auth {
		req.Header[name] = values
	}
	if strings.TrimSpace(sentState) == "" {
		req.Header.Del(openAICodexTurnStateHeader)
	} else {
		req.Header.Set(openAICodexTurnStateHeader, strings.TrimSpace(sentState))
	}
	SanitizeOutboundGatewayIdentity(req.Header)
	// Match the OpenAI gateway's ordinary transport (including its proxy).
	// Turn State maintenance is independent of the account's serving
	// concurrency. A zero value keeps the transport on its ordinary pool limits.
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, 0)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", nil, codexTurnStateAutoError("timeout")
		}
		return "", nil, codexTurnStateAutoError("transport_failed")
	}
	if resp == nil {
		return "", nil, codexTurnStateAutoError("empty_response")
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	state := extractOpenAICodexTurnState(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if code, retryAfter := codexTurnStateProbe429Diagnostic(resp.StatusCode, resp.Header, time.Now()); code != "" {
			return "", nil, &codexTurnStateProbeHTTPError{code: code, retryAfter: retryAfter}
		}
		return "", nil, codexTurnStateAutoError(fmt.Sprintf("http_%d", resp.StatusCode))
	}
	if requireState && state == "" {
		return "", nil, codexTurnStateAutoError("missing_state")
	}
	if state != "" && ValidateOpenAICodexTurnState(state) != nil {
		return "", nil, codexTurnStateAutoError("invalid_state")
	}
	body := []byte(nil)
	if resp.Body != nil {
		body, err = io.ReadAll(io.LimitReader(resp.Body, codexTurnStateAutoMaxBody+1))
		if err != nil || len(body) > codexTurnStateAutoMaxBody {
			return "", nil, errCodexTurnStateResponseNotCompleted
		}
	}
	observer := &upstreamResponseModelObserver{}
	if bodyHasSSEFraming(body) {
		observeOpenAISSEBody(observer, string(body))
	} else if len(bytes.TrimSpace(body)) > 0 {
		observer.ObserveOpenAI(body, strings.TrimSpace(gjson.GetBytes(body, "type").String()))
	}
	if err := validateCodexTurnStateResponseEvidence(observer, codexTurnStateExpectedResponseModel(model)); err != nil {
		return "", observer, err
	}
	return state, observer, nil
}

// createOpenAICodexTurnStateProbePayload keeps the quota-spending maintenance
// request intentionally small and deterministic. The requested short prompt is
// the actual user input; instructions must not be used as a substitute for it.
func createOpenAICodexTurnStateProbePayload(model string) map[string]any {
	payload := createOpenAITestPayload(model, true)
	payload["input"] = []map[string]any{
		{
			"type": "message",
			"role": "user",
			"content": []map[string]any{
				{
					"type": "input_text",
					"text": codexTurnStateProbePrompt,
				},
			},
		},
	}
	return payload
}

// StripCodexTurnStateAutoExtra returns a copy suitable for account imports,
// exports and user edits. Collected state must never move to another account.
func StripCodexTurnStateAutoExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	result := make(map[string]any, len(extra))
	for key, value := range extra {
		if strings.HasPrefix(key, CodexTurnStateModelExtraPrefix) || strings.HasPrefix(key, CodexTurnStateProbeBurstBudgetExtraPrefix) {
			continue
		}
		switch key {
		case CodexTurnStateAutoExtraKey, CodexTurnStateAutoSetAtExtraKey, CodexTurnStateAutoProbeAtExtraKey, CodexTurnStateAutoLastErrorExtraKey,
			CodexTurnStateAutoVerifiedAtExtraKey, CodexTurnStateAutoVerifiedModelExtraKey, CodexTurnStateAutoProbeNotBeforeExtraKey, CodexTurnStateAutoRecoveryExtraKey:
			continue
		}
		result[key] = value
	}
	return result
}
