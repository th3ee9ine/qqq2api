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
	CodexTurnStateAutoExtraKey                 = "codex_turn_state_auto"
	CodexTurnStateAutoSetAtExtraKey            = "codex_turn_state_auto_set_at_ms"
	CodexTurnStateAutoProbeAtExtraKey          = "codex_turn_state_auto_probe_at_ms"
	CodexTurnStateAutoProbeCompletedAtExtraKey = "codex_turn_state_auto_probe_completed_at_ms"
	CodexTurnStateAutoLastErrorExtraKey        = "codex_turn_state_auto_last_error"
	CodexTurnStateAutoVerifiedAtExtraKey       = "codex_turn_state_auto_verified_at_ms"
	CodexTurnStateAutoVerifiedModelExtraKey    = "codex_turn_state_auto_verified_model"
	CodexTurnStateAutoProbeModelExtraKey       = "codex_turn_state_auto_probe_model"
	CodexTurnStateAutoProbeNotBeforeExtraKey   = "codex_turn_state_auto_probe_not_before_ms"
	codexTurnStateProbePrompt                  = "Reply with exactly OK."
	codexTurnStateAutoProbeInterval            = 5 * time.Minute
	codexTurnStateAutoMaxBody                  = 128 << 10
	codexTurnStateProbePersistAttempts         = 3
)

// Only fixed error codes are persisted or logged: provider errors can contain
// bearer tokens, proxy credentials, URLs or response bodies.
type codexTurnStateAutoError string

func (e codexTurnStateAutoError) Error() string { return string(e) }

// CodexTurnStateAutoInfo contains no token. Expiry is a reference TTL, not an
// upstream guarantee. Unknown envelopes age from their first collection time.
type CodexTurnStateAutoInfo struct {
	Models              map[string]CodexTurnStateAutoInfo `json:"models,omitempty"`
	SuccessfulModels    []string                          `json:"successful_models"`
	CollectionSucceeded bool                              `json:"collection_succeeded"`
	Configured          bool                              `json:"configured"`
	SetAtMS             int64                             `json:"set_at_ms,omitempty"`
	ProbeAtMS           int64                             `json:"probe_at_ms,omitempty"`
	VerifiedAtMS        int64                             `json:"verified_at_ms,omitempty"`
	VerifiedModel       string                            `json:"verified_model,omitempty"`
	ProbeNotBeforeMS    int64                             `json:"probe_not_before_ms,omitempty"`
	StateLength         int                               `json:"state_length,omitempty"`
	ExpiresAtMS         int64                             `json:"expires_at_ms,omitempty"`
	Due                 bool                              `json:"due"`
	LastError           string                            `json:"last_error,omitempty"`
	RecoveryPending     bool                              `json:"recovery_pending"`
	InvalidatedAtMS     int64                             `json:"invalidated_at_ms,omitempty"`
}

type codexTurnStateAutoEntry struct {
	model            string
	requestModel     string
	scopeModels      []string
	loadedAt         time.Time
	knownTokens      map[string]int64
	token            string
	setAt, probeAt   int64
	probeCompletedAt int64
	verifiedAt       int64
	verifiedModel    string
	probeNotBefore   int64
	lastError        string
	lastUsed         time.Time
	dirty, running   bool
	reconciling      bool
	retryAfter       time.Time // persistence retry boundary
	retryWakeAt      time.Time
	probeRetryAfter  time.Time
	probeWakeAt      time.Time
	probe            bool
	forceProbe       bool
	// manualProbe marks work explicitly requested by an administrator. Manual
	// collection remains runnable when automatic injection is disabled, while
	// retaining the same bounded worker and probe safeguards.
	manualProbe bool
	// renewalProbe is scheduled by the idle-account maintenance scanner. It keeps
	// all automatic gates and budgets, but publishes after the maintenance replay
	// so expiry and failure recovery do not depend on later business traffic.
	renewalProbe bool
	// renewalWorker owns one slot in the scheduled-maintenance pool. The context
	// is captured by the worker before these queue fields are cleared.
	renewalWorker  bool
	renewalContext context.Context
	// collectionTaskID binds this model slot to the admin-visible background
	// task. The registry-owned context is independent of the originating HTTP
	// request and is canceled only by an explicit task action or shutdown.
	collectionTaskID      string
	collectionTaskContext context.Context
	collectionTaskProbeAt int64
	// manualOutcomePending keeps terminal-write provenance separate from whether
	// another upstream probe is queued, including across persistence retries.
	manualOutcomePending bool
	recovery             codexTurnStateRecovery
	candidate            codexTurnStateUsageCandidate
	// pendingOwner is retained after a candidate is promoted locally until the
	// corresponding durable state write succeeds.
	pendingOwner codexTurnStateProbeCandidatePendingOwner
}

type CodexTurnStateAtomicRepository interface {
	UpdateCodexTurnState(context.Context, int64, string, map[string]any) (bool, error)
}

type codexTurnStateProbeTask struct {
	requestModel          string
	scopeModels           []string
	force                 bool
	collectionTaskID      string
	collectionTaskContext context.Context
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

// Legacy records predate the explicit terminal clock. Infer it only when the
// persisted fields prove that the recorded probe already reached an outcome.
// New writes always include the key, including zero for an in-progress round.
func codexTurnStateProbeCompletedAt(account *Account) int64 {
	if account == nil {
		return 0
	}
	if _, exists := account.Extra[CodexTurnStateAutoProbeCompletedAtExtraKey]; exists {
		return codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeCompletedAtExtraKey)
	}
	probeAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeAtExtraKey)
	if probeAt <= 0 {
		return 0
	}
	verifiedAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoVerifiedAtExtraKey)
	if safeCodexTurnStateAutoError(account.GetExtraString(CodexTurnStateAutoLastErrorExtraKey)) != "" {
		return probeAt
	}
	if verifiedAt >= probeAt {
		return verifiedAt
	}
	return 0
}

func markCodexTurnStateProbeCompletedLocked(entry *codexTurnStateAutoEntry, now time.Time) {
	if entry == nil {
		return
	}
	completedAt := now.UnixMilli()
	if completedAt < entry.probeAt {
		completedAt = entry.probeAt
	}
	if completedAt <= entry.probeCompletedAt {
		completedAt = entry.probeCompletedAt + 1
	}
	entry.probeCompletedAt = completedAt
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
func codexTurnStateAutoInterval(minutes int) time.Duration {
	if _, err := NormalizeOpenAICodexTurnStateAutoIntervalMinutes(minutes); err != nil {
		minutes = OpenAICodexTurnStateDefaultAutoIntervalMinutes
	}
	return time.Duration(minutes) * time.Minute
}

func codexTurnStateAutomaticCollectionDue(token string, setAt, verifiedAt int64, now time.Time, intervalMinutes int) bool {
	expiresAt := codexTurnStateAutoExpiry(token, setAt, now)
	if token == "" || setAt <= 0 || expiresAt <= 0 {
		return true
	}
	renewalBaseAt := setAt
	if verifiedAt > renewalBaseAt {
		renewalBaseAt = verifiedAt
	}
	dueAt := renewalBaseAt + codexTurnStateAutoInterval(intervalMinutes).Milliseconds()
	if expiresAt < dueAt {
		dueAt = expiresAt
	}
	return now.UnixMilli() >= dueAt
}

// The configurable interval controls successful renewal. This fixed backoff is
// only for failed automatic rounds so request traffic cannot retry them in a
// tight loop. Manual and forced recovery work bypass it.
func codexTurnStateAutomaticFailureBackoffActive(entry *codexTurnStateAutoEntry, now time.Time) bool {
	if entry == nil || entry.lastError == "" {
		return false
	}
	failureAt := entry.probeCompletedAt
	if failureAt < entry.probeAt {
		failureAt = entry.probeAt
	}
	return failureAt > 0 && now.Sub(time.UnixMilli(failureAt)) < codexTurnStateAutoProbeInterval
}

func codexTurnStateAutomaticRetryDue(entry *codexTurnStateAutoEntry, now time.Time, intervalMinutes int) bool {
	if entry == nil {
		return false
	}
	failed := entry.lastError != "" && entry.probeAt > 0
	return entry.recovery.Pending || failed ||
		codexTurnStateAutomaticCollectionDue(entry.token, entry.setAt, entry.verifiedAt, now, intervalMinutes)
}

func codexTurnStateAutoInfoWithInterval(account *Account, now time.Time, intervalMinutes int) CodexTurnStateAutoInfo {
	token := codexTurnStateAutoToken(account)
	info := CodexTurnStateAutoInfo{
		SuccessfulModels:    []string{},
		CollectionSucceeded: token != "",
		Configured:          token != "",
		SetAtMS:             codexTurnStateAutoInt64(account, CodexTurnStateAutoSetAtExtraKey),
		ProbeAtMS:           codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeAtExtraKey),
		VerifiedAtMS:        codexTurnStateAutoInt64(account, CodexTurnStateAutoVerifiedAtExtraKey),
		VerifiedModel:       strings.TrimSpace(account.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey)),
		ProbeNotBeforeMS:    codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeNotBeforeExtraKey),
	}
	if token != "" {
		info.StateLength = len(token)
	}
	info.ExpiresAtMS = codexTurnStateAutoExpiry(token, info.SetAtMS, now)
	recovery := codexTurnStateRecoveryFromAccount(account)
	info.RecoveryPending, info.InvalidatedAtMS = recovery.Pending, recovery.InvalidatedAtMS
	if account != nil {
		info.LastError = safeCodexTurnStateAutoError(account.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	}
	info.Due = recovery.Pending || !recovery.allows(token, now) ||
		(info.LastError != "" && info.ProbeAtMS > 0) ||
		codexTurnStateAutomaticCollectionDue(token, info.SetAtMS, info.VerifiedAtMS, now, intervalMinutes)
	return info
}

func codexTurnStateAutoInfo(account *Account, now time.Time) CodexTurnStateAutoInfo {
	return codexTurnStateAutoInfoWithInterval(account, now, OpenAICodexTurnStateDefaultAutoIntervalMinutes)
}
func CodexTurnStateAutoInfoForAccount(account *Account, now time.Time) *CodexTurnStateAutoInfo {
	if !codexTurnStateCollectionEligible(account) {
		return nil
	}
	return codexTurnStateScopedInfo(account, now)
}

// CodexTurnStateAutoInfoForAccount returns account diagnostics using the
// currently effective renewal interval. The package-level helper remains for
// callers without a gateway and uses the default interval for compatibility.
func (s *OpenAIGatewayService) CodexTurnStateAutoInfoForAccount(ctx context.Context, account *Account, now time.Time) *CodexTurnStateAutoInfo {
	if !codexTurnStateCollectionEligible(account) {
		return nil
	}
	intervalMinutes := OpenAICodexTurnStateDefaultAutoIntervalMinutes
	if s != nil {
		intervalMinutes = s.codexTurnStateRuntimeConfig(ctx).AutoIntervalMinutes
	}
	return codexTurnStateScopedInfoWithInterval(account, now, intervalMinutes)
}
func safeCodexTurnStateAutoError(value string) string {
	switch value {
	case "state_312", "recovery_requires_new_292", "account_unavailable", "persistence_failed", "auth_failed", "transport_failed", "timeout", "missing_state", "invalid_state", "empty_response", "request_failed",
		"response_model_mismatch", "response_model_missing", "response_not_completed", "response_failed", "state_replay_failed",
		"probe_boundary_refresh_failed", "probe_pool_unavailable", "probe_burst_exhausted", "probe_burst_persistence_failed", "probe_burst_candidate_pending", "probe_burst_in_flight",
		"account_not_schedulable", "model_scope_changed",
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

func codexTurnStateCollectionEligible(account *Account) bool {
	return codexTurnStateAutoEligible(account) && account.IsSchedulable()
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
		return OpenAICodexTurnStateConfig{AutoIntervalMinutes: OpenAICodexTurnStateDefaultAutoIntervalMinutes}
	}
	if s.settingService == nil {
		return OpenAICodexTurnStateConfig{ModelScopeValid: true, AutoIntervalMinutes: OpenAICodexTurnStateDefaultAutoIntervalMinutes}
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
	return codexTurnStateProbeTask{
		requestModel:          requestModel,
		scopeModels:           models,
		force:                 entry.forceProbe,
		collectionTaskID:      entry.collectionTaskID,
		collectionTaskContext: entry.collectionTaskContext,
	}
}

// Caller must hold openaiTurnStateMu. One owner slot can run only one collection
// task at a time; retries carry an already-created task through their context.
func (s *OpenAIGatewayService) bindCodexTurnStateCollectionTaskLocked(entry *codexTurnStateAutoEntry, taskID string, taskCtx context.Context) bool {
	if s == nil || entry == nil {
		return false
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	if entry.collectionTaskID != "" && entry.collectionTaskID != taskID {
		return false
	}
	if taskCtx == nil {
		var ok bool
		taskCtx, ok = s.CodexTurnStateCollectionTaskContext(taskID)
		if !ok {
			return false
		}
	}
	if taskCtx.Err() != nil {
		return false
	}
	entry.collectionTaskID = taskID
	entry.collectionTaskContext = taskCtx
	return true
}

// Caller must hold openaiTurnStateMu. Automatic schedulers fail closed when the
// bounded task registry cannot accept a record: untracked background collection
// is not started, and the due slot remains eligible for a later scan/request.
func (s *OpenAIGatewayService) createCodexTurnStateCollectionTaskLocked(parent context.Context, account *Account, entry *codexTurnStateAutoEntry, source string) bool {
	if s == nil || account == nil || entry == nil {
		return false
	}
	if entry.collectionTaskID != "" {
		if entry.collectionTaskContext != nil && entry.collectionTaskContext.Err() == nil {
			return true
		}
		s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
	}
	requestModel := strings.TrimSpace(entry.requestModel)
	if requestModel == "" {
		requestModel = entry.model
	}
	task, taskCtx, err := s.CreateCodexTurnStateCollectionTask(parent, CodexTurnStateCollectionTaskInput{
		AccountID:    account.ID,
		AccountName:  account.Name,
		RequestModel: requestModel,
		OwnerModel:   entry.model,
		Source:       source,
	})
	if err != nil || task == nil || !s.bindCodexTurnStateCollectionTaskLocked(entry, task.ID, taskCtx) {
		if task != nil {
			_, _ = s.CancelCodexTurnStateCollectionTask(task.ID)
		}
		return false
	}
	return true
}

// Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) clearCodexTurnStateCollectionTaskLocked(entry *codexTurnStateAutoEntry) string {
	if entry == nil {
		return ""
	}
	taskID := entry.collectionTaskID
	entry.collectionTaskID = ""
	entry.collectionTaskContext = nil
	entry.collectionTaskProbeAt = 0
	return taskID
}

// Caller must hold openaiTurnStateMu. Registry transitions are terminal and
// ignore late updates after an administrator cancellation.
func (s *OpenAIGatewayService) completeCodexTurnStateCollectionTaskLocked(entry *codexTurnStateAutoEntry) {
	if taskID := s.clearCodexTurnStateCollectionTaskLocked(entry); taskID != "" {
		s.CompleteCodexTurnStateCollectionTask(taskID)
	}
}

// Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) failCodexTurnStateCollectionTaskLocked(entry *codexTurnStateAutoEntry, code string) {
	if taskID := s.clearCodexTurnStateCollectionTaskLocked(entry); taskID != "" {
		s.FailCodexTurnStateCollectionTask(taskID, code)
	}
}

// Caller must hold openaiTurnStateMu. Cancellation never becomes a persisted
// provider failure: it only drops work owned by the canceled task.
func (s *OpenAIGatewayService) discardCanceledCodexTurnStateCollectionTaskLocked(entry *codexTurnStateAutoEntry) bool {
	if entry == nil || entry.collectionTaskContext == nil || entry.collectionTaskContext.Err() == nil {
		return false
	}
	s.clearCodexTurnStateCollectionTaskLocked(entry)
	entry.dirty = false
	entry.probe = false
	entry.forceProbe = false
	entry.manualProbe = false
	entry.renewalProbe = false
	entry.manualOutcomePending = false
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
	entry.probeRetryAfter = time.Time{}
	entry.probeWakeAt = time.Time{}
	s.discardCodexTurnStateCandidateLocked(entry)
	return true
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
	entry.renewalProbe = false
	return true
}

// Caller must hold openaiTurnStateMu. A manual request that was reported as
// queued must leave a new observable terminal result even when it stops before
// reaching the upstream. The worker persists this dirty entry on its next loop.
func markCodexTurnStateManualFailureLocked(entry *codexTurnStateAutoEntry, code string, now time.Time) {
	if entry == nil {
		return
	}
	code = safeCodexTurnStateAutoError(code)
	if code == "" {
		code = "request_failed"
	}
	probeAt := now.UnixMilli()
	if probeAt <= entry.probeAt {
		probeAt = entry.probeAt + 1
	}
	entry.probeAt = probeAt
	if entry.collectionTaskID != "" {
		entry.collectionTaskProbeAt = probeAt
	}
	entry.lastError = code
	markCodexTurnStateProbeCompletedLocked(entry, now)
	entry.probe = false
	entry.forceProbe = false
	entry.manualProbe = false
	entry.renewalProbe = false
	entry.manualOutcomePending = true
	entry.probeRetryAfter = time.Time{}
	entry.probeWakeAt = time.Time{}
	entry.dirty = true
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
}

// Caller must hold openaiTurnStateMu. Automatic and renewal tasks use the same
// durable failure envelope as manual work, but do not inherit manual retry
// intent. The task remains attached until the worker persists this outcome.
func markCodexTurnStateBackgroundFailureLocked(entry *codexTurnStateAutoEntry, code string, now time.Time) {
	if entry == nil || entry.collectionTaskID == "" {
		return
	}
	code = safeCodexTurnStateAutoError(code)
	if code == "" {
		code = "request_failed"
	}
	if entry.collectionTaskProbeAt <= 0 || entry.collectionTaskProbeAt != entry.probeAt {
		probeAt := now.UnixMilli()
		if probeAt <= entry.probeAt {
			probeAt = entry.probeAt + 1
		}
		entry.probeAt = probeAt
		entry.probeCompletedAt = 0
		entry.collectionTaskProbeAt = probeAt
	}
	entry.lastError = code
	markCodexTurnStateProbeCompletedLocked(entry, now)
	entry.probe = false
	entry.forceProbe = false
	entry.manualProbe = false
	entry.renewalProbe = false
	entry.manualOutcomePending = false
	entry.dirty = true
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
}

// Caller must hold openaiTurnStateMu. A concurrently published valid state can
// satisfy the requested owner without another upstream call. Refresh only the
// observable verification/probe clocks; never extend the state's original age.
func markCodexTurnStateManualSatisfiedLocked(entry *codexTurnStateAutoEntry, now time.Time) {
	if entry == nil {
		return
	}
	probeAt := now.UnixMilli()
	if probeAt <= entry.probeAt {
		probeAt = entry.probeAt + 1
	}
	entry.probeAt = probeAt
	if entry.collectionTaskID != "" {
		entry.collectionTaskProbeAt = probeAt
	}
	if entry.verifiedAt < probeAt {
		entry.verifiedAt = probeAt
	}
	entry.lastError = ""
	markCodexTurnStateProbeCompletedLocked(entry, now)
	entry.probe = false
	entry.forceProbe = false
	entry.manualProbe = false
	entry.renewalProbe = false
	entry.manualOutcomePending = true
	entry.dirty = true
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
}

type codexTurnStateAccountLock struct {
	token chan struct{}
}

func newCodexTurnStateAccountLock() *codexTurnStateAccountLock {
	return &codexTurnStateAccountLock{token: make(chan struct{}, 1)}
}

func (s *OpenAIGatewayService) lockCodexTurnStateAccountContext(ctx context.Context, accountID int64) (func(), bool) {
	if s == nil || accountID <= 0 {
		return func() {}, true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	value, _ := s.openaiTurnStateManualLocks.LoadOrStore(accountID, newCodexTurnStateAccountLock())
	lock := value.(*codexTurnStateAccountLock)
	select {
	case lock.token <- struct{}{}:
		return func() { <-lock.token }, true
	case <-ctx.Done():
		return func() {}, false
	}

}

func (s *OpenAIGatewayService) lockCodexTurnStateManualAccount(accountID int64) func() {
	unlock, _ := s.lockCodexTurnStateAccountContext(context.Background(), accountID)
	return unlock
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
	if s.discardCanceledCodexTurnStateCollectionTaskLocked(entry) {
		return false
	}
	taskAllowed := codexTurnStateEntryScopeAllowed(cfg, entry)
	if !cfg.AutoEnabled && entry.collectionTaskID != "" && !entry.manualProbe && !entry.manualOutcomePending {
		if entry.candidate.state != "" {
			s.discardCodexTurnStateCandidateLocked(entry)
		}
		markCodexTurnStateBackgroundFailureLocked(entry, "request_failed", time.Now())
	}
	if !taskAllowed {
		if entry.manualProbe || entry.manualOutcomePending {
			markCodexTurnStateManualFailureLocked(entry, "model_scope_changed", time.Now())
		} else if entry.collectionTaskID != "" {
			markCodexTurnStateBackgroundFailureLocked(entry, "model_scope_changed", time.Now())
		} else {
			entry.probe = false
			entry.forceProbe = false
			entry.renewalProbe = false
		}
	}
	if entry.candidate.state != "" && !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
		s.discardCodexTurnStateCandidateLocked(entry)
		if entry.collectionTaskID != "" {
			markCodexTurnStateBackgroundFailureLocked(entry, "model_scope_changed", time.Now())
		}
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
		s.startCodexTurnStateWorkerLocked(accountID, entry)
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

// Caller must hold openaiTurnStateMu. Once the account has been deleted there
// is no durable target for queued writes or probes. Invalidate every local wake
// and release only the exact durable candidate owners held by this process.
func (s *OpenAIGatewayService) discardCodexTurnStateWorkForMissingAccountLocked(entry *codexTurnStateAutoEntry) {
	if entry == nil {
		return
	}
	if entry.collectionTaskID != "" {
		s.failCodexTurnStateCollectionTaskLocked(entry, "account_unavailable")
	}
	pendingOwner := entry.pendingOwner
	entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
	s.releaseCodexTurnStateCandidateOwnerAsync(pendingOwner)
	s.discardCodexTurnStateCandidateLocked(entry)
	entry.dirty = false
	entry.probe = false
	entry.forceProbe = false
	entry.manualProbe = false
	entry.renewalProbe = false
	entry.manualOutcomePending = false
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
	entry.probeRetryAfter = time.Time{}
	entry.probeWakeAt = time.Time{}
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
			idle := !entry.running && !entry.reconciling && !entry.dirty && !entry.probe &&
				!entry.manualProbe && !entry.renewalProbe && !entry.manualOutcomePending &&
				entry.collectionTaskID == "" && entry.candidate.state == "" && entry.pendingOwner.version <= 0 &&
				entry.retryWakeAt.IsZero() && entry.probeWakeAt.IsZero()
			if idle && now.Sub(entry.lastUsed) > 2*time.Hour {
				delete(s.openaiTurnStates, id)
			}
		}
		s.openaiTurnStateSweep = now
	}
	entry := s.openaiTurnStates[key]
	if entry == nil {
		entry = &codexTurnStateAutoEntry{model: model, requestModel: requestModel, scopeModels: scopeModels, knownTokens: make(map[string]int64), recovery: codexTurnStateRecoveryFromAccount(account)}
		s.openaiTurnStates[key] = entry
	}
	persistedRecovery := codexTurnStateRecoveryFromAccount(account)
	if persistedRecovery.InvalidatedAtMS > entry.recovery.InvalidatedAtMS {
		entry.recovery = persistedRecovery
		entry.token = ""
	}
	setAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoSetAtExtraKey)
	verifiedAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoVerifiedAtExtraKey)
	verifiedModel := strings.TrimSpace(account.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
	token := codexTurnStateAutoToken(account)
	if codexTurnStateOwnerModel(verifiedModel) != model {
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
	persistedProbeAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeAtExtraKey)
	persistedCompletedAt := codexTurnStateProbeCompletedAt(account)
	if !entry.reconciling && !entry.dirty &&
		(persistedProbeAt > entry.probeAt || persistedProbeAt == entry.probeAt && persistedCompletedAt > entry.probeCompletedAt) {
		entry.probeAt = persistedProbeAt
		entry.probeCompletedAt = persistedCompletedAt
		entry.lastError = safeCodexTurnStateAutoError(account.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
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
		entry.token != "" && codexTurnStateOwnerModel(entry.verifiedModel) == entry.model &&
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
	verifiedModel := strings.TrimSpace(slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
	recovery := codexTurnStateRecoveryFromAccount(slot)
	setAt := codexTurnStateAutoInt64(slot, CodexTurnStateAutoSetAtExtraKey)
	lastError := safeCodexTurnStateAutoError(slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	persistedProbeAt := codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey)
	persistedCompletedAt := codexTurnStateProbeCompletedAt(slot)
	if persistedProbeAt > entry.probeAt || persistedProbeAt == entry.probeAt && persistedCompletedAt >= entry.probeCompletedAt {
		entry.probeAt = persistedProbeAt
		entry.probeCompletedAt = persistedCompletedAt
		entry.lastError = lastError
	}
	// A persisted, redacted failure means the latest collection did not
	// succeed. An administrator may explicitly retry even while the older token
	// itself remains inside its local validity window.
	if lastError != "" {
		return false
	}
	if codexTurnStateOwnerModel(verifiedModel) != entry.model || recovery.Pending || recovery.InvalidatedAtMS < entry.recovery.InvalidatedAtMS ||
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
	if probeNotBefore := codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeNotBeforeExtraKey); probeNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = probeNotBefore
	}
	entry.loadedAt = now
	if entry.probeNotBefore > now.UnixMilli() {
		entry.probeRetryAfter = time.UnixMilli(entry.probeNotBefore)
	}
	s.rememberCodexTurnStateLocked(entry, now)
	return true
}

// handleCodexTurnStateProbeCandidateLocked consumes an already completed
// collection result before another upstream probe can start. Manual and
// scheduled-renewal intents publish it locally; request-triggered automatic work
// exits because a second probe would duplicate the same account/model operation.
// Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) handleCodexTurnStateProbeCandidateLocked(cfg OpenAICodexTurnStateConfig, entry *codexTurnStateAutoEntry, now time.Time, manual, renewal bool) bool {
	if !s.codexTurnStateUsageCandidateActiveLocked(entry, now) {
		return false
	}
	if !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
		s.discardCodexTurnStateCandidateLocked(entry)
		return false
	}
	publishAfterReplay := manual || renewal || entry.candidate.manual
	if publishAfterReplay {
		entry.candidate.manual = true
		if !s.publishManualCodexTurnStateCandidateLocked(entry, now) {
			// Keep an explicit administrator request runnable if publication lost
			// its validity check. Scheduled work records the terminal error and lets
			// the ordinary automatic backoff govern its next attempt.
			if manual && !entry.probe {
				entry.probe = true
				entry.forceProbe = true
				entry.manualProbe = true
			}
		}
		if renewal && !manual {
			entry.manualProbe = false
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
	if !codexTurnStateCollectionEligible(account) || s == nil || s.settingService == nil {
		return ""
	}
	cfg := s.settingService.GetOpenAICodexTurnState(ctx)
	requestModel := s.codexTurnStateModel(ctx, models...)
	scopeModels := appendCodexTurnStateScopeModels(nil, models...)
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, requestModel)
	if !cfg.AutoEnabled {
		s.enforceCodexTurnStateScopeForAccount(cfg, account.ID, requestModel)
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
	if !codexTurnStateManualVerification(ctx) && !candidatePending && codexTurnStateAutomaticRetryDue(entry, now, cfg.AutoIntervalMinutes) {
		currentTaskAllowed := codexTurnStateEntryScopeAllowed(cfg, entry)
		if !now.Before(time.UnixMilli(entry.probeNotBefore)) && (!currentTaskAllowed || entry.forceProbe || !codexTurnStateAutomaticFailureBackoffActive(entry, now)) {
			replaceCodexTurnStateProbeModelsLocked(entry, requestModel, scopeModels...)
			if !currentTaskAllowed {
				entry.forceProbe = true
			}
			if s.createCodexTurnStateCollectionTaskLocked(context.Background(), account, entry, CodexTurnStateCollectionSourceAutomatic) {
				entry.probe = true
			}
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
		// Successful reuse does not renew token age, but it does provide fresh
		// verification evidence for this collection round. Keeping verifiedAt
		// current lets polling distinguish the success from an older result.
		entry.verifiedAt = now.UnixMilli()
		entry.lastError = ""
		markCodexTurnStateProbeCompletedLocked(entry, now)
		entry.dirty = true
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
	markCodexTurnStateProbeCompletedLocked(entry, now)
	s.rememberCodexTurnStateLocked(entry, now)
}
func (s *OpenAIGatewayService) startCodexTurnStateWorkerLocked(id int64, entry *codexTurnStateAutoEntry) {
	if s.openaiTurnStateStopping || s.accountRepo == nil || entry.running || entry.reconciling || (!entry.dirty && !entry.probe) {
		return
	}
	now := time.Now()
	if entry.dirty && now.Before(entry.retryAfter) {
		return
	}
	if !entry.dirty && entry.probe && !entry.manualProbe && now.Before(entry.probeRetryAfter) {
		return
	}
	entry.running = true
	s.openaiTurnStateWorkers++
	s.openaiTurnStateWorkersWG.Add(1)
	go func() {
		defer s.openaiTurnStateWorkersWG.Done()
		s.runCodexTurnStateWorker(id, entry)
	}()
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
	taskID := entry.collectionTaskID
	taskCtx := entry.collectionTaskContext
	go func(expected time.Time, expectedTaskID string, waitCtx context.Context) {
		if delay := time.Until(expected); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			if waitCtx == nil {
				<-timer.C
			} else {
				select {
				case <-timer.C:
				case <-waitCtx.Done():
					s.openaiTurnStateMu.Lock()
					if s.openaiTurnStates[key] == entry && entry.retryWakeAt.Equal(expected) {
						entry.retryWakeAt = time.Time{}
						if expectedTaskID == "" || entry.collectionTaskID == expectedTaskID {
							s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
						}
					}
					s.openaiTurnStateMu.Unlock()
					return
				}
			}
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
	}(retryAt, taskID, taskCtx)
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
	taskID := entry.collectionTaskID
	taskCtx := entry.collectionTaskContext
	go func(expected time.Time, expectedTaskID string, waitCtx context.Context) {
		if delay := time.Until(expected); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			if waitCtx == nil {
				<-timer.C
			} else {
				select {
				case <-timer.C:
				case <-waitCtx.Done():
					s.openaiTurnStateMu.Lock()
					if s.openaiTurnStates[key] == entry && entry.probeWakeAt.Equal(expected) {
						entry.probeWakeAt = time.Time{}
						if expectedTaskID == "" || entry.collectionTaskID == expectedTaskID {
							s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
						}
					}
					s.openaiTurnStateMu.Unlock()
					return
				}
			}
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
		if entry.dirty || cfg.AutoEnabled || entry.manualProbe {
			s.startCodexTurnStateWorkerLocked(id, entry)
		}
		if entry.probe && entry.manualProbe && !entry.running && time.Now().Before(entry.probeRetryAfter) {
			s.scheduleCodexTurnStateProbeRetryLocked(id, entry, entry.probeRetryAfter)
		}
	}(retryAt, taskID, taskCtx)
}

// Caller must hold openaiTurnStateMu. Automatic candidates wait for a later
// durable usage record, but that wait must remain cancelable and bounded so an
// abandoned verification cannot hold the account/model task forever.
func (s *OpenAIGatewayService) scheduleCodexTurnStateUsageCandidateDeadlineLocked(id int64, entry *codexTurnStateAutoEntry) {
	if s == nil || entry == nil || id <= 0 || entry.collectionTaskID == "" ||
		entry.collectionTaskContext == nil || entry.candidate.state == "" || entry.candidate.manual || entry.candidate.collectedAt.IsZero() {
		return
	}
	taskID := entry.collectionTaskID
	taskCtx := entry.collectionTaskContext
	collectedAt := entry.candidate.collectedAt
	deadline := collectedAt.Add(codexTurnStateUsageCandidateTTL)
	key := codexTurnStateKey{accountID: id, model: entry.model}
	go func() {
		canceled := false
		if delay := time.Until(deadline); delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-taskCtx.Done():
				canceled = true
			}
		}

		s.openaiTurnStateMu.Lock()
		defer s.openaiTurnStateMu.Unlock()
		if s.openaiTurnStates[key] != entry || entry.collectionTaskID != taskID {
			return
		}
		if canceled || taskCtx.Err() != nil {
			s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
			return
		}
		if entry.candidate.state != "" {
			if !entry.candidate.collectedAt.Equal(collectedAt) {
				return
			}
			s.discardCodexTurnStateCandidateLocked(entry)
		} else if entry.running || entry.reconciling || entry.dirty || entry.probe {
			// Confirmation or another lifecycle transition already owns the task.
			return
		}
		markCodexTurnStateBackgroundFailureLocked(entry, "usage_log_missing", time.Now())
		if entry.collectionTaskID != "" {
			s.UpdateCodexTurnStateCollectionTask(entry.collectionTaskID, CodexTurnStateCollectionTaskStagePersisting, 85, 100)
		}
		s.startCodexTurnStateWorkerLocked(id, entry)
	}()
}

func codexTurnStateProbeExecutionContext(taskCtx, renewalCtx context.Context, renewal bool) (context.Context, func()) {
	if taskCtx == nil {
		taskCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(taskCtx)
	stopRenewal := func() bool { return false }
	if renewal && renewalCtx != nil {
		stopRenewal = context.AfterFunc(renewalCtx, cancel)
	}
	return ctx, func() {
		stopRenewal()
		cancel()
	}
}

func (s *OpenAIGatewayService) runCodexTurnStateWorker(id int64, entry *codexTurnStateAutoEntry) {
	s.openaiTurnStateMu.Lock()
	renewalWorker := entry.renewalWorker
	renewalCtx := entry.renewalContext
	initialTaskID := entry.collectionTaskID
	entry.renewalWorker = false
	entry.renewalContext = nil
	s.openaiTurnStateMu.Unlock()
	if renewalCtx == nil {
		renewalCtx = context.Background()
	}
	if initialTaskID != "" {
		if task, ok := s.GetCodexTurnStateCollectionTask(initialTaskID); ok && task != nil && task.Status == CodexTurnStateCollectionTaskStatusQueued {
			s.StartCodexTurnStateCollectionTask(initialTaskID, CodexTurnStateCollectionTaskStagePreparing, 100)
		}
	}
	defer func() {
		cfg := s.codexTurnStateRuntimeConfig(context.Background())
		s.openaiTurnStateMu.Lock()
		entry.running = false
		s.openaiTurnStateWorkers--
		if renewalWorker {
			if s.openaiTurnStateRenewalWorkers > 0 {
				s.openaiTurnStateRenewalWorkers--
			}
			delete(s.openaiTurnStateRenewalAccounts, id)
		}
		canceled := s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
		inScope := !canceled && s.enforceCodexTurnStateScopeLocked(cfg, entry)
		if !canceled && (entry.dirty || inScope && (cfg.AutoEnabled || entry.manualProbe)) {
			s.startCodexTurnStateWorkerLocked(id, entry)
		}
		if entry.dirty && !entry.running && time.Now().Before(entry.retryAfter) {
			s.scheduleCodexTurnStatePersistenceRetryLocked(id, entry, entry.retryAfter)
		}
		if entry.probe && entry.manualProbe && !entry.running && time.Now().Before(entry.probeRetryAfter) {
			s.scheduleCodexTurnStateProbeRetryLocked(id, entry, entry.probeRetryAfter)
		}
		s.openaiTurnStateMu.Unlock()
		if renewalWorker {
			s.openaiTurnStateRenewalWG.Done()
		}
	}()
	if renewalWorker && renewalCtx.Err() != nil {
		if initialTaskID != "" {
			_, _ = s.CancelCodexTurnStateCollectionTask(initialTaskID)
		}
		s.openaiTurnStateMu.Lock()
		entry.probe = false
		entry.forceProbe = false
		entry.renewalProbe = false
		s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
		s.openaiTurnStateMu.Unlock()
		return
	}
	// Bound repeated persistence churn for one account/model. Independent slots
	// are not globally throttled; manual work keeps running when auto mode is off.
	for n := 0; n < 16; n++ {
		// Settings may require a cache fill or database read. Never perform that
		// work while holding the Turn State map lock.
		cfg := s.codexTurnStateRuntimeConfig(context.Background())
		s.openaiTurnStateMu.Lock()
		if s.discardCanceledCodexTurnStateCollectionTaskLocked(entry) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		s.enforceCodexTurnStateScopeLocked(cfg, entry)
		dirty, probe, manualRun, renewalRun := entry.dirty, entry.probe, entry.manualProbe, entry.renewalProbe
		manualPersist := manualRun || entry.manualOutcomePending
		task := codexTurnStateProbeTaskLocked(entry)
		if !dirty && !cfg.AutoEnabled && !manualRun {
			if renewalRun {
				entry.probe = false
				entry.forceProbe = false
				entry.renewalProbe = false
			}
			if entry.candidate.state != "" {
				s.discardCodexTurnStateCandidateLocked(entry)
			}
			if entry.collectionTaskID != "" {
				s.failCodexTurnStateCollectionTaskLocked(entry, "request_failed")
			}
			s.openaiTurnStateMu.Unlock()
			return
		}
		entry.dirty = false
		if dirty {
			entry.manualOutcomePending = false
		}
		if cfg.AutoEnabled || manualRun {
			entry.probe = false
			entry.forceProbe = false
			entry.manualProbe = false
			entry.renewalProbe = false
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
			if task.collectionTaskID != "" {
				s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStagePersisting, 90, 100)
			}
			err := s.persistCodexTurnStateWithModeContext(task.collectionTaskContext, id, entry, manualPersist)
			if err != nil {
				s.openaiTurnStateMu.Lock()
				if task.collectionTaskContext != nil && task.collectionTaskContext.Err() != nil {
					s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
					s.openaiTurnStateMu.Unlock()
					return
				}
				if errors.Is(err, ErrAccountNotFound) {
					s.failCodexTurnStateCollectionTaskLocked(entry, "account_unavailable")
					s.discardCodexTurnStateWorkForMissingAccountLocked(entry)
					s.openaiTurnStateMu.Unlock()
					return
				}
				entry.dirty = true
				if manualPersist {
					entry.manualOutcomePending = true
				}
				newerQueued := entry.probe
				if !newerQueued {
					entry.probe = probe
					if manualPersist {
						entry.forceProbe = true
						entry.manualProbe = true
					}
				}
				manualRetry := entry.manualProbe
				if manualPersist && !newerQueued {
					manualRetry = true
				}
				entry.manualProbe = manualRetry
				retryAt := time.Now().Add(5 * time.Second)
				entry.retryAfter = retryAt
				s.scheduleCodexTurnStatePersistenceRetryLocked(id, entry, retryAt)
				s.openaiTurnStateMu.Unlock()
				if task.collectionTaskID != "" {
					s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageRetryWait, 90, 100)
				}
				slog.Warn("openai_codex_turn_state_collect_failed", "account_id", id, "code", "persistence_failed")
				return
			}
			s.openaiTurnStateMu.Lock()
			if entry.collectionTaskID == task.collectionTaskID && entry.collectionTaskProbeAt > 0 && !entry.probe {
				if codexTurnStateEntryHasValidStateLocked(entry, time.Now()) && entry.lastError == "" {
					s.completeCodexTurnStateCollectionTaskLocked(entry)
				} else {
					code := entry.lastError
					if code == "" {
						code = "request_failed"
					}
					s.failCodexTurnStateCollectionTaskLocked(entry, code)
				}
			}
			s.openaiTurnStateMu.Unlock()
		}
		if probe {
			probeCtx, releaseProbeCtx := codexTurnStateProbeExecutionContext(task.collectionTaskContext, renewalCtx, renewalRun)
			s.runCodexTurnStateProbeTaskWithIntentContext(probeCtx, id, entry, manualRun, renewalRun, task)
			releaseProbeCtx()
			if renewalRun && renewalCtx.Err() != nil && task.collectionTaskID != "" {
				_, _ = s.CancelCodexTurnStateCollectionTask(task.collectionTaskID)
			}
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
	return s.persistCodexTurnStateWithModeContext(context.Background(), id, entry, manual)
}

func (s *OpenAIGatewayService) persistCodexTurnStateWithModeContext(parent context.Context, id int64, entry *codexTurnStateAutoEntry, manual bool) error {
	if parent == nil {
		parent = context.Background()
	}
	s.openaiTurnStateMu.Lock()
	accountProbeNotBefore := s.codexTurnStateAccountProbeNotBeforeLocked(id)
	pendingOwner := entry.pendingOwner
	updates := map[string]any{
		CodexTurnStateAutoProbeNotBeforeExtraKey: accountProbeNotBefore,
		codexTurnStateModelExtraKey(entry.model): map[string]any{
			CodexTurnStateAutoExtraKey:                 entry.token,
			CodexTurnStateAutoSetAtExtraKey:            entry.setAt,
			CodexTurnStateAutoProbeAtExtraKey:          entry.probeAt,
			CodexTurnStateAutoProbeCompletedAtExtraKey: entry.probeCompletedAt,
			CodexTurnStateAutoLastErrorExtraKey:        entry.lastError,
			CodexTurnStateAutoVerifiedAtExtraKey:       entry.verifiedAt,
			CodexTurnStateAutoVerifiedModelExtraKey:    entry.verifiedModel,
			CodexTurnStateAutoProbeModelExtraKey:       entry.requestModel,
			CodexTurnStateAutoProbeNotBeforeExtraKey:   accountProbeNotBefore,
			CodexTurnStateAutoRecoveryExtraKey:         entry.recovery.clone(),
		},
	}
	s.openaiTurnStateMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
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
			// The losing write belongs to an administrator request, so a concurrently
			// queued automatic task for this owner must also carry that manual intent.
			// Keep its newer request/scope snapshot, but do not let the observable
			// manual outcome disappear behind the CAS winner.
			if manual {
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
	return s.persistCodexTurnStateProbeOutcomeContext(context.Background(), id, entry, manual)
}

func (s *OpenAIGatewayService) persistCodexTurnStateProbeOutcomeContext(parent context.Context, id int64, entry *codexTurnStateAutoEntry, manual bool) bool {
	if parent == nil {
		parent = context.Background()
	}
	for attempt := 0; attempt < codexTurnStateProbePersistAttempts; attempt++ {
		err := s.persistCodexTurnStateWithModeContext(parent, id, entry, manual)
		if err == nil {
			return true
		}
		if parent.Err() != nil {
			return false
		}
		if errors.Is(err, ErrAccountNotFound) {
			s.openaiTurnStateMu.Lock()
			s.discardCodexTurnStateWorkForMissingAccountLocked(entry)
			s.openaiTurnStateMu.Unlock()
			return false
		}
	}
	s.openaiTurnStateMu.Lock()
	entry.dirty = true
	if manual {
		entry.manualOutcomePending = true
	}
	entry.retryAfter = time.Now().Add(5 * time.Second)
	s.scheduleCodexTurnStatePersistenceRetryLocked(id, entry, entry.retryAfter)
	s.openaiTurnStateMu.Unlock()
	return false
}

// A 429 observed mid-round must constrain later automatic rounds immediately,
// without publishing a model-slot failure while the current route snapshot is
// still being attempted. The terminal slot outcome is persisted only after the
// full round fails (or is cleared by a later successful route).
func (s *OpenAIGatewayService) persistCodexTurnStateProbeBoundary(id int64, entry *codexTurnStateAutoEntry, notBefore int64) bool {
	return s.persistCodexTurnStateProbeBoundaryContext(context.Background(), id, entry, notBefore)
}

func (s *OpenAIGatewayService) persistCodexTurnStateProbeBoundaryContext(parent context.Context, id int64, entry *codexTurnStateAutoEntry, notBefore int64) bool {
	if s == nil || s.accountRepo == nil || id <= 0 || notBefore <= 0 {
		return false
	}
	if parent == nil {
		parent = context.Background()
	}
	for attempt := 0; attempt < codexTurnStateProbePersistAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(parent, 3*time.Second)
		var err error
		if repo, ok := s.accountRepo.(CodexTurnStateAccountProbeBoundaryRepository); ok {
			err = repo.AdvanceCodexTurnStateProbeNotBefore(ctx, id, notBefore)
		} else {
			// Test and alternate repositories may not expose the monotonic helper.
			// They still receive only the root boundary, never a model-slot outcome.
			err = s.accountRepo.UpdateExtra(ctx, id, map[string]any{
				CodexTurnStateAutoProbeNotBeforeExtraKey: notBefore,
			})
		}
		cancel()
		if err == nil {
			return true
		}
		if parent.Err() != nil {
			return false
		}
		if errors.Is(err, ErrAccountNotFound) {
			s.openaiTurnStateMu.Lock()
			s.discardCodexTurnStateWorkForMissingAccountLocked(entry)
			s.openaiTurnStateMu.Unlock()
			return false
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
	queuedProbe, queuedForce, queuedManual, queuedRenewal := entry.probe, entry.forceProbe, entry.manualProbe, entry.renewalProbe
	pendingOwner := entry.pendingOwner
	entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
	s.releaseCodexTurnStateCandidateOwnerAsync(pendingOwner)
	entry.reconciling = false
	entry.token, entry.setAt = "", 0
	entry.verifiedAt, entry.verifiedModel = 0, ""
	s.discardCodexTurnStateCandidateLocked(entry)
	entry.knownTokens = make(map[string]int64)
	entry.recovery = codexTurnStateRecovery{}
	entry.probeAt, entry.probeCompletedAt, entry.probeNotBefore, entry.lastError = 0, 0, 0, ""
	entry.dirty, entry.probe, entry.forceProbe, entry.manualProbe = false, queuedProbe, queuedForce, queuedManual
	entry.renewalProbe = queuedRenewal
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
		entry.verifiedModel = strings.TrimSpace(slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
		entry.token = codexTurnStateAutoToken(slot)
		if codexTurnStateOwnerModel(entry.verifiedModel) != entry.model {
			entry.token = ""
		}
		entry.recovery = codexTurnStateRecoveryFromAccount(slot)
		entry.probeNotBefore = codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeNotBeforeExtraKey)
		entry.probeAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey)
		entry.probeCompletedAt = codexTurnStateProbeCompletedAt(slot)
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
	if codexTurnStateEntryHasValidStateLocked(entry, now) && entry.lastError == "" {
		entry.probe = false
		entry.forceProbe = false
		entry.manualProbe = false
		entry.renewalProbe = false
	}
	s.rememberCodexTurnStateLocked(entry, now)
}
func (s *OpenAIGatewayService) runCodexTurnStateProbe(id int64, entry *codexTurnStateAutoEntry) {
	s.runCodexTurnStateProbeWithMode(id, entry, false)
}

// runCodexTurnStateProbeWithMode performs the bounded maintenance probe. The
// manual mode bypasses the automatic-enable gate and a previously persisted
// probe cooldown. Protocol/account lifecycle eligibility, configured model
// scope, response validation and CAS publication remain mandatory.
func (s *OpenAIGatewayService) runCodexTurnStateProbeWithMode(id int64, entry *codexTurnStateAutoEntry, manual bool) {
	s.openaiTurnStateMu.Lock()
	task := codexTurnStateProbeTaskLocked(entry)
	s.openaiTurnStateMu.Unlock()
	s.runCodexTurnStateProbeTaskWithMode(id, entry, manual, task)
}

func (s *OpenAIGatewayService) runCodexTurnStateProbeTaskWithMode(id int64, entry *codexTurnStateAutoEntry, manual bool, task codexTurnStateProbeTask) {
	s.runCodexTurnStateProbeTaskWithIntent(id, entry, manual, false, task)
}

func (s *OpenAIGatewayService) runCodexTurnStateProbeTaskWithIntent(id int64, entry *codexTurnStateAutoEntry, manual, renewal bool, task codexTurnStateProbeTask) {
	s.runCodexTurnStateProbeTaskWithIntentContext(context.Background(), id, entry, manual, renewal, task)
}

func (s *OpenAIGatewayService) runCodexTurnStateProbeTaskWithIntentContext(parent context.Context, id int64, entry *codexTurnStateAutoEntry, manual, renewal bool, task codexTurnStateProbeTask) {
	if parent == nil {
		parent = context.Background()
	}
	if manual {
		unlock, acquired := s.lockCodexTurnStateAccountContext(parent, id)
		if !acquired {
			if task.collectionTaskID != "" {
				_, _ = s.CancelCodexTurnStateCollectionTask(task.collectionTaskID)
				s.openaiTurnStateMu.Lock()
				s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
				s.openaiTurnStateMu.Unlock()
			}
			return
		}
		defer unlock()
	} else if renewal {
		unlock, acquired := s.lockCodexTurnStateAccountContext(parent, id)
		if !acquired {
			if task.collectionTaskID != "" {
				_, _ = s.CancelCodexTurnStateCollectionTask(task.collectionTaskID)
				s.openaiTurnStateMu.Lock()
				s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
				s.openaiTurnStateMu.Unlock()
			}
			return
		}
		defer unlock()
	}
	if task.collectionTaskID != "" {
		s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageLoadingAccount, 5, 100)
	}
	markCollectionFailureLocked := func(code string, now time.Time) {
		if entry == nil {
			return
		}
		if manual {
			markCodexTurnStateManualFailureLocked(entry, code, now)
			return
		}
		if task.collectionTaskID == "" || entry.collectionTaskID != task.collectionTaskID {
			return
		}
		code = safeCodexTurnStateAutoError(code)
		if code == "" {
			code = "request_failed"
		}
		if entry.collectionTaskProbeAt <= 0 || entry.collectionTaskProbeAt != entry.probeAt {
			probeAt := now.UnixMilli()
			if probeAt <= entry.probeAt {
				probeAt = entry.probeAt + 1
			}
			entry.probeAt = probeAt
			entry.probeCompletedAt = 0
			entry.collectionTaskProbeAt = probeAt
		}
		entry.lastError = code
		markCodexTurnStateProbeCompletedLocked(entry, now)
		entry.probe = false
		entry.forceProbe = false
		entry.renewalProbe = false
		entry.dirty = true
	}
	finishCollectionFailure := func(code string) {
		s.openaiTurnStateMu.Lock()
		if s.discardCanceledCodexTurnStateCollectionTaskLocked(entry) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		markCollectionFailureLocked(code, time.Now())
		s.openaiTurnStateMu.Unlock()
	}
	failCollectionTaskOnly := func(code string) {
		s.openaiTurnStateMu.Lock()
		if !s.discardCanceledCodexTurnStateCollectionTaskLocked(entry) && entry.collectionTaskID == task.collectionTaskID {
			s.failCodexTurnStateCollectionTaskLocked(entry, code)
		}
		s.openaiTurnStateMu.Unlock()
	}
	finishPersistedCollectionFailure := func(code string, persisted bool) {
		retryWait := false
		retryProbe := false
		s.openaiTurnStateMu.Lock()
		if s.discardCanceledCodexTurnStateCollectionTaskLocked(entry) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		if entry.collectionTaskID == task.collectionTaskID {
			switch {
			case persisted && entry.probe:
				// A CAS winner can requeue an explicit request after this process
				// reconciles the authoritative slot. Keep the same traceable task.
				retryProbe = true
			case persisted && codexTurnStateEntryHasValidStateLocked(entry, time.Now()) && entry.lastError == "":
				s.completeCodexTurnStateCollectionTaskLocked(entry)
			case persisted:
				failureCode := entry.lastError
				if failureCode == "" {
					failureCode = code
				}
				s.failCodexTurnStateCollectionTaskLocked(entry, failureCode)
			case entry.dirty:
				retryWait = true
			default:
				s.failCodexTurnStateCollectionTaskLocked(entry, "persistence_failed")
			}
		}
		s.openaiTurnStateMu.Unlock()
		if retryWait {
			s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageRetryWait, 90, 100)
		} else if retryProbe {
			s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStagePreparing, 5, 100)
		}
	}
	finishCollectionStale := func() {
		now := time.Now()
		s.openaiTurnStateMu.Lock()
		if codexTurnStateEntryHasValidStateLocked(entry, now) && entry.lastError == "" {
			if manual {
				markCodexTurnStateManualSatisfiedLocked(entry, now)
			} else {
				s.completeCodexTurnStateCollectionTaskLocked(entry)
			}
		} else {
			markCollectionFailureLocked("request_failed", now)
		}
		s.openaiTurnStateMu.Unlock()
	}
	if s.httpUpstream == nil {
		finishCollectionFailure("request_failed")
		return
	}
	var (
		pendingOwner          codexTurnStateProbeCandidatePendingOwner
		pendingOwnerOwned     bool
		roundReservation      codexTurnStateProbeBurstReservation
		roundReservationOwned bool
		burstGeneration       int64
		burstModel            string
	)
	releaseRoundReservation := func() error {
		if !roundReservationOwned {
			return nil
		}
		roundReservationOwned = false
		releaseCtx, cancelRelease := context.WithTimeout(context.WithoutCancel(parent), gatewayForwardingDBTimeout)
		defer cancelRelease()
		return s.releaseCodexTurnStateProbeBurstAttempt(releaseCtx, id, burstModel, burstGeneration, roundReservation)
	}
	defer func() {
		_ = releaseRoundReservation()
		if pendingOwnerOwned {
			s.releaseCodexTurnStateCandidateOwnerAsync(pendingOwner)
		}
	}()
	lookupCtx, cancelLookup := context.WithTimeout(parent, gatewayForwardingDBTimeout)
	account, err := s.accountRepo.GetByID(lookupCtx, id)
	cancelLookup()
	if err != nil {
		if parent.Err() != nil {
			s.openaiTurnStateMu.Lock()
			s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
			s.openaiTurnStateMu.Unlock()
			return
		}
		if errors.Is(err, ErrAccountNotFound) {
			s.openaiTurnStateMu.Lock()
			s.failCodexTurnStateCollectionTaskLocked(entry, "account_unavailable")
			s.discardCodexTurnStateWorkForMissingAccountLocked(entry)
			s.openaiTurnStateMu.Unlock()
			return
		}
		finishCollectionFailure("account_unavailable")
		return
	}
	if !codexTurnStateCollectionEligible(account) {
		finishCollectionFailure("account_not_schedulable")
		return
	}
	if task.collectionTaskID != "" {
		s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageResolvingRoutes, 10, 100)
	}
	// Re-read persisted timestamps before spending upstream quota. This reduces
	// cross-instance duplicates; it is deliberately not a distributed lock.
	cfg := s.codexTurnStateRuntimeConfig(parent)
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	s.codexTurnStateEntryLocked(account, now, entry.model)
	if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
		markCollectionFailureLocked("model_scope_changed", now)
		s.openaiTurnStateMu.Unlock()
		return
	}
	manualIntent := manual
	if manualIntent && s.codexTurnStateManualStateAlreadyValidLocked(entry, account, now) {
		// A concurrent publisher won the exact account/model slot. Do not let
		// forceProbe turn a satisfied manual request into another upstream call.
		markCodexTurnStateManualSatisfiedLocked(entry, now)
		s.openaiTurnStateMu.Unlock()
		return
	}
	if s.handleCodexTurnStateProbeCandidateLocked(cfg, entry, now, manualIntent, renewal) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	// Existing automatic cooldowns block a new round. An explicit administrator
	// request bypasses both the durable boundary and local retry timer, but any
	// new 429 it sees remains durable for later automatic rounds.
	durableCooldown := now.Before(time.UnixMilli(entry.probeNotBefore))
	if !manualIntent && (now.Before(entry.probeRetryAfter) || durableCooldown) {
		s.failCodexTurnStateCollectionTaskLocked(entry, "request_failed")
		s.openaiTurnStateMu.Unlock()
		return
	}
	if !manualIntent && !task.force && (!codexTurnStateAutomaticRetryDue(entry, now, cfg.AutoIntervalMinutes) || codexTurnStateAutomaticFailureBackoffActive(entry, now)) {
		s.failCodexTurnStateCollectionTaskLocked(entry, "request_failed")
		s.openaiTurnStateMu.Unlock()
		return
	}
	probeAt := now.UnixMilli()
	if probeAt <= entry.probeAt {
		probeAt = entry.probeAt + 1
	}
	entry.probeAt = probeAt
	entry.probeCompletedAt = 0
	if task.collectionTaskID != "" && task.collectionTaskID == entry.collectionTaskID {
		entry.collectionTaskProbeAt = probeAt
	}
	generation := entry.recovery.InvalidatedAtMS
	before := entry.token
	claimedProbeAt := entry.probeAt
	roundStaleLocked := func() bool {
		return entry.probeAt != claimedProbeAt || entry.probeCompletedAt != 0 ||
			entry.token != before || entry.recovery.InvalidatedAtMS != generation
	}
	if manualIntent {
		// Make this round distinguishable from the previous terminal result. The
		// cleared error is persisted with probeAt before the first upstream call.
		entry.lastError = ""
	}
	// The durable burst budget limits background rounds. One reservation covers
	// the complete route snapshot; individual IP failures never consume another
	// attempt. Explicit administrator rounds bypass this automatic budget.
	burst := !manualIntent && codexTurnStateProbeNeedsBurst(entry, now)
	burstGeneration = codexTurnStateProbeBurstGeneration(entry)
	burstModel = codexTurnStateProbeBurstOwnerModel(entry.model)
	probeModel := task.requestModel
	if probeModel == "" {
		probeModel = entry.model
	}
	s.openaiTurnStateMu.Unlock()

	// Capture the pool once. This defines one collection round: every selected
	// route is attempted at most once, and a direct route is used only when the
	// configured and fallback pools are both empty.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	routes, poolErr := s.codexTurnStatePoolRouteSnapshot(ctx, "")
	if poolErr != nil {
		if ctx.Err() != nil {
			s.openaiTurnStateMu.Lock()
			s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
			s.openaiTurnStateMu.Unlock()
			return
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		now = time.Now()
		inScope := s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry)
		stale := roundStaleLocked()
		if !inScope || stale {
			switch {
			case !inScope:
				markCollectionFailureLocked("model_scope_changed", now)
			case codexTurnStateEntryHasValidStateLocked(entry, now) && entry.lastError == "":
				if manualIntent {
					markCodexTurnStateManualSatisfiedLocked(entry, now)
				} else {
					s.completeCodexTurnStateCollectionTaskLocked(entry)
				}
			default:
				markCollectionFailureLocked("request_failed", now)
			}
			s.openaiTurnStateMu.Unlock()
			return
		}
		entry.lastError = safeCodexTurnStateAutoError(poolErr.Error())
		if entry.lastError == "" {
			entry.lastError = "probe_pool_unavailable"
		}
		markCodexTurnStateProbeCompletedLocked(entry, now)
		s.openaiTurnStateMu.Unlock()
		if task.collectionTaskID != "" {
			s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStagePersisting, 90, 100)
		}
		persisted := s.persistCodexTurnStateProbeOutcomeContext(ctx, id, entry, manualIntent)
		if !persisted {
			slog.Warn("openai_codex_turn_state_probe_persist_failed", "account_id", id, "code", "persistence_failed")
		}
		finishPersistedCollectionFailure(entry.lastError, persisted)
		slog.Warn("openai_codex_turn_state_probe_failed", "account_id", id, "code", "probe_pool_unavailable")
		return
	}
	if len(routes) == 0 {
		routes = []string{""}
	}
	if task.collectionTaskID != "" {
		s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageCollecting, 20, 100)
	}
	if !manual && !s.codexTurnStateAutoEnabled(ctx) {
		failCollectionTaskOnly("request_failed")
		return
	}
	cfg = s.codexTurnStateRuntimeConfig(ctx)
	s.openaiTurnStateMu.Lock()
	if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
		markCollectionFailureLocked("model_scope_changed", time.Now())
		s.openaiTurnStateMu.Unlock()
		return
	}
	s.openaiTurnStateMu.Unlock()
	// Probe metadata persistence is database-only. A storage failure must stop
	// before any upstream request and leave bounded retry work behind.
	manualIntent = manual
	if task.collectionTaskID != "" {
		s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStagePersisting, 20, 100)
	}
	if !s.persistCodexTurnStateProbeOutcomeContext(ctx, id, entry, manualIntent) {
		if ctx.Err() != nil {
			s.openaiTurnStateMu.Lock()
			s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
			s.openaiTurnStateMu.Unlock()
			return
		}
		if manualIntent {
			s.openaiTurnStateMu.Lock()
			requeueCodexTurnStateManualTaskLocked(entry, task)
			s.openaiTurnStateMu.Unlock()
		}
		if task.collectionTaskID != "" {
			s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageRetryWait, 20, 100)
		}
		return
	}
	// A concurrent publisher or lifecycle update can win while probe metadata is
	// being persisted. Full account reads are mandatory here and before every
	// real request; the narrow Turn State source does not contain schedulability.
	stateSnapshot, err := s.loadCodexTurnStateCollectionAccount(ctx, id)
	if err != nil {
		finishCollectionFailure("account_unavailable")
		return
	}
	account = stateSnapshot
	cfg = s.codexTurnStateRuntimeConfig(ctx)
	s.openaiTurnStateMu.Lock()
	now = time.Now()
	s.codexTurnStateEntryLocked(stateSnapshot, now, entry.model)
	if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
		markCollectionFailureLocked("model_scope_changed", now)
		s.openaiTurnStateMu.Unlock()
		return
	}
	if manualIntent && codexTurnStateAutoToken(codexTurnStateModelAccount(stateSnapshot, entry.model)) != before &&
		s.codexTurnStateManualStateAlreadyValidLocked(entry, stateSnapshot, now) {
		markCodexTurnStateManualSatisfiedLocked(entry, now)
		s.openaiTurnStateMu.Unlock()
		return
	}
	if s.handleCodexTurnStateProbeCandidateLocked(cfg, entry, now, manualIntent, renewal) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	s.openaiTurnStateMu.Unlock()

	var roundErr error
	attemptDeadline := time.Time{}
	if burst {
		var reserveErr error
		roundReservation, reserveErr = s.reserveCodexTurnStateProbeRound(ctx, id, burstModel, burstGeneration)
		if reserveErr != nil {
			if errors.Is(reserveErr, errCodexTurnStateProbeBurstInFlight) || errors.Is(reserveErr, errCodexTurnStateProbeCandidatePending) {
				failCollectionTaskOnly(reserveErr.Error())
				return
			}
			roundErr = reserveErr
		} else {
			roundReservationOwned = true
		}
	}

	roundReady := roundErr == nil
	for attempt := 0; roundReady && attempt < len(routes); attempt++ {
		if task.collectionTaskID != "" {
			progress := 25 + attempt*45/len(routes)
			s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageCollecting, progress, 100)
		}
		if burst {
			var renewErr error
			roundReservation, renewErr = s.renewCodexTurnStateProbeBurstAttempt(ctx, id, burstModel, burstGeneration, roundReservation)
			if renewErr != nil {
				roundErr = renewErr
				break
			}
			attemptDeadline = roundReservation.deadline
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		if ctx.Err() != nil || !manual && !cfg.AutoEnabled {
			if ctx.Err() == nil {
				failCollectionTaskOnly("request_failed")
			}
			return
		}
		latest, loadErr := s.loadCodexTurnStateCollectionAccount(ctx, id)
		if loadErr != nil {
			finishCollectionFailure("account_unavailable")
			return
		}
		account, stateSnapshot = latest, latest
		s.openaiTurnStateMu.Lock()
		now = time.Now()
		s.codexTurnStateEntryLocked(stateSnapshot, now, entry.model)
		if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
			markCollectionFailureLocked("model_scope_changed", now)
			s.openaiTurnStateMu.Unlock()
			return
		}
		manualIntent = manual
		if manualIntent && codexTurnStateAutoToken(codexTurnStateModelAccount(stateSnapshot, entry.model)) != before &&
			s.codexTurnStateManualStateAlreadyValidLocked(entry, stateSnapshot, now) {
			markCodexTurnStateManualSatisfiedLocked(entry, now)
			s.openaiTurnStateMu.Unlock()
			return
		}
		if s.handleCodexTurnStateProbeCandidateLocked(cfg, entry, now, manualIntent, renewal) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		stale := roundStaleLocked()
		s.openaiTurnStateMu.Unlock()
		if stale {
			finishCollectionStale()
			return
		}
		// Reserving the durable round lease can itself take long enough for an
		// administrator to narrow the model scope. Recheck immediately before the
		// real upstream request; the deferred release owns the complete round.
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		inScope := s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry)
		if !inScope {
			markCollectionFailureLocked("model_scope_changed", time.Now())
		}
		s.openaiTurnStateMu.Unlock()
		if !inScope {
			return
		}
		// Renewal and every IP in the round use the same requested model.
		model := probeModel
		attemptCtx, cancelAttempt := codexTurnStateProbeAttemptContext(ctx, attemptDeadline)
		state, verifiedModel, probeErr := s.probeOpenAICodexTurnStateViaProxyWithModeEvidence(attemptCtx, account, model, routes[attempt], true)
		cancelAttempt()
		if ctx.Err() != nil {
			return
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		inScope = s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry)
		if !inScope {
			markCollectionFailureLocked("model_scope_changed", time.Now())
		}
		s.openaiTurnStateMu.Unlock()
		if !inScope {
			return
		}
		if probeErr != nil && probeErr.Error() == "account_unavailable" {
			finishCollectionFailure("account_unavailable")
			return
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		inScope = s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry)
		if !inScope {
			markCollectionFailureLocked("model_scope_changed", time.Now())
		}
		s.openaiTurnStateMu.Unlock()
		if !inScope {
			return
		}
		if !manual && !s.codexTurnStateAutoEnabled(ctx) {
			failCollectionTaskOnly("request_failed")
			return
		}
		if probeErr == nil {
			if task.collectionTaskID != "" {
				s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageVerifying, 75, 100)
			}
			latest, loadErr = s.loadCodexTurnStateCollectionAccount(ctx, id)
			if loadErr != nil {
				finishCollectionFailure("account_unavailable")
				return
			}
			account, stateSnapshot = latest, latest
			s.openaiTurnStateMu.Lock()
			now = time.Now()
			s.codexTurnStateEntryLocked(stateSnapshot, now, entry.model)
			if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
				markCollectionFailureLocked("model_scope_changed", now)
				s.openaiTurnStateMu.Unlock()
				return
			}
			if manualIntent && codexTurnStateAutoToken(codexTurnStateModelAccount(stateSnapshot, entry.model)) != before &&
				s.codexTurnStateManualStateAlreadyValidLocked(entry, stateSnapshot, now) {
				markCodexTurnStateManualSatisfiedLocked(entry, now)
				s.openaiTurnStateMu.Unlock()
				return
			}
			if s.handleCodexTurnStateProbeCandidateLocked(cfg, entry, now, manualIntent, renewal) {
				s.openaiTurnStateMu.Unlock()
				return
			}
			stale = roundStaleLocked()
			candidateRejected := false
			if !stale {
				collectedAt := time.UnixMilli(entry.probeAt)
				if collectedAt.IsZero() || collectedAt.After(now) {
					collectedAt = now
				}
				candidateRejected = !canStageCodexTurnStateUsageCandidateLocked(entry, state, verifiedModel, generation, collectedAt)
			}
			s.openaiTurnStateMu.Unlock()
			if stale {
				finishCollectionStale()
				return
			}
			if candidateRejected {
				var previousHTTPError *codexTurnStateProbeHTTPError
				if roundErr == nil || !errors.As(roundErr, &previousHTTPError) {
					roundErr = codexTurnStateAutoError("invalid_state")
				}
				continue
			}
		}
		if probeErr == nil && burst {
			var won bool
			var pendingErr error
			won, pendingOwner, pendingErr = s.markCodexTurnStateProbeCandidatePendingOwned(ctx, id, burstModel, burstGeneration, roundReservation)
			if pendingErr != nil {
				roundErr = pendingErr
				break
			} else if !won {
				// Another instance already staged the first valid candidate for this
				// model/generation. Discard this opaque blob without persisting it.
				failCollectionTaskOnly("probe_burst_candidate_pending")
				return
			}
			roundReservationOwned = false
			pendingOwnerOwned = true
			latest, loadErr = s.loadCodexTurnStateCollectionAccount(ctx, id)
			if loadErr != nil {
				finishCollectionFailure("account_unavailable")
				return
			}
			stateSnapshot = latest
		}
		cfg = s.codexTurnStateRuntimeConfig(ctx)
		s.openaiTurnStateMu.Lock()
		now = time.Now()
		s.codexTurnStateEntryLocked(stateSnapshot, now, entry.model)
		if !s.enforceActiveCodexTurnStateProbeScopeLocked(cfg, task, entry) {
			markCollectionFailureLocked("model_scope_changed", now)
			s.openaiTurnStateMu.Unlock()
			return
		}
		if roundStaleLocked() {
			if codexTurnStateEntryHasValidStateLocked(entry, now) && entry.lastError == "" {
				if manualIntent {
					markCodexTurnStateManualSatisfiedLocked(entry, now)
				} else {
					s.completeCodexTurnStateCollectionTaskLocked(entry)
				}
			} else {
				markCollectionFailureLocked("request_failed", now)
			}
			s.openaiTurnStateMu.Unlock()
			return
		}
		if probeErr == nil {
			// Request-triggered automatic collection keeps the dedicated usage gate.
			// Manual and scheduled-expiry collection are self-contained after the
			// same-route replay, then persist through the same CAS path.
			manualIntent = manual
			publishAfterReplay := manual || renewal
			collectedAt := time.UnixMilli(entry.probeAt)
			if collectedAt.IsZero() || collectedAt.After(now) {
				collectedAt = now
			}
			staged := s.stageCodexTurnStateUsageCandidateWithVerifiedModelLocked(entry, state, verifiedModel, generation, collectedAt, publishAfterReplay, task.scopeModels)
			if staged && pendingOwnerOwned {
				entry.candidate.pendingOwner = pendingOwner
				pendingOwnerOwned = false
			}
			if staged && !publishAfterReplay {
				s.scheduleCodexTurnStateUsageCandidateDeadlineLocked(id, entry)
			}
			published := staged
			if publishAfterReplay {
				published = staged && s.publishManualCodexTurnStateCandidateLocked(entry, now)
				if renewal && !manual {
					entry.manualProbe = false
				}
				if !published {
					entry.lastError = "invalid_state"
					markCodexTurnStateProbeCompletedLocked(entry, now)
					entry.dirty = false
					entry.manualProbe = false
					entry.retryAfter = time.Time{}
				}
			}
			s.openaiTurnStateMu.Unlock()
			if published {
				if task.collectionTaskID != "" {
					if publishAfterReplay {
						s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStagePersisting, 85, 100)
					} else {
						s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStageVerifying, 80, 100)
					}
				}
				return
			}
			var previousHTTPError *codexTurnStateProbeHTTPError
			if roundErr == nil || !errors.As(roundErr, &previousHTTPError) {
				roundErr = codexTurnStateAutoError("invalid_state")
			}
			if pendingOwnerOwned {
				restoreCtx, cancelRestore := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
				restored, restoreErr := s.restoreCodexTurnStateProbeRound(restoreCtx, pendingOwner)
				cancelRestore()
				if restoreErr != nil {
					roundErr = restoreErr
					break
				}
				pendingOwnerOwned = false
				roundReservation = restored
				roundReservationOwned = true
				attemptDeadline = restored.deadline
			}
			continue
		}
		var httpErr *codexTurnStateProbeHTTPError
		var probeBoundary int64
		if errors.As(probeErr, &httpErr) {
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
			probeBoundary = entry.probeNotBefore
		}
		s.openaiTurnStateMu.Unlock()
		if httpErr != nil {
			// Persist only the account boundary, then continue this already-started
			// round. A slot error here would expose a false terminal state to polling
			// clients before the remaining IPs have been attempted.
			_ = s.persistCodexTurnStateProbeBoundaryContext(ctx, id, entry, probeBoundary)
		}
		// Preserve a quota signal as the most meaningful exhausted-round error;
		// otherwise the last route error is the diagnostic result.
		var previousHTTPError *codexTurnStateProbeHTTPError
		if roundErr == nil || errors.As(probeErr, &httpErr) || !errors.As(roundErr, &previousHTTPError) {
			roundErr = probeErr
		}
	}
	if releaseErr := releaseRoundReservation(); releaseErr != nil {
		var quotaErr *codexTurnStateProbeHTTPError
		if roundErr == nil || !errors.As(roundErr, &quotaErr) {
			roundErr = releaseErr
		}
	}
	if parent.Err() != nil {
		s.openaiTurnStateMu.Lock()
		s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
		s.openaiTurnStateMu.Unlock()
		return
	}
	cfg = s.codexTurnStateRuntimeConfig(parent)
	s.openaiTurnStateMu.Lock()
	if !codexTurnStateProbeTaskAllows(cfg, task) {
		markCollectionFailureLocked("model_scope_changed", time.Now())
		s.openaiTurnStateMu.Unlock()
		return
	}
	if roundStaleLocked() {
		now := time.Now()
		if codexTurnStateEntryHasValidStateLocked(entry, now) && entry.lastError == "" {
			if manual {
				markCodexTurnStateManualSatisfiedLocked(entry, now)
			} else {
				s.completeCodexTurnStateCollectionTaskLocked(entry)
			}
		} else {
			markCollectionFailureLocked("request_failed", now)
		}
		s.openaiTurnStateMu.Unlock()
		return
	}
	code := "request_failed"
	if roundErr != nil {
		code = safeCodexTurnStateAutoError(roundErr.Error())
	}
	if code == "" {
		code = "request_failed"
	}
	entry.lastError = code
	markCodexTurnStateProbeCompletedLocked(entry, time.Now())
	// This worker owns persistence; release the cache mutex during DB I/O.
	s.openaiTurnStateMu.Unlock()
	if task.collectionTaskID != "" {
		s.UpdateCodexTurnStateCollectionTask(task.collectionTaskID, CodexTurnStateCollectionTaskStagePersisting, 90, 100)
	}
	persisted := s.persistCodexTurnStateProbeOutcomeContext(parent, id, entry, manual)
	if !persisted {
		slog.Warn("openai_codex_turn_state_probe_persist_failed", "account_id", id, "code", "persistence_failed")
	}
	finishPersistedCollectionFailure(code, persisted)
	slog.Warn("openai_codex_turn_state_probe_failed", "account_id", id, "code", code)
}

func (s *OpenAIGatewayService) probeOpenAICodexTurnState(ctx context.Context, account *Account, model string) (string, error) {
	return s.probeOpenAICodexTurnStateViaProxy(ctx, account, model, "")
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
	return s.probeOpenAICodexTurnStateViaProxyWithMode(ctx, account, model, proxyURL, false)
}

func (s *OpenAIGatewayService) probeOpenAICodexTurnStateViaProxyWithMode(ctx context.Context, account *Account, model, proxyURL string, activeRound bool) (string, error) {
	state, _, err := s.probeOpenAICodexTurnStateViaProxyWithModeEvidence(ctx, account, model, proxyURL, activeRound)
	return state, err
}

// probeOpenAICodexTurnStateViaProxyWithModeEvidence returns the raw model from
// the successful same-route replay. Ownership decisions still use the canonical
// family, while diagnostics and persistence retain the actual upstream model.
func (s *OpenAIGatewayService) probeOpenAICodexTurnStateViaProxyWithModeEvidence(ctx context.Context, account *Account, model, proxyURL string, activeRound bool) (string, string, error) {
	state, _, err := s.requestOpenAICodexTurnStateViaProxyWithMode(ctx, account, model, proxyURL, "", true, activeRound)
	if err != nil {
		return state, "", err
	}
	if !activeRound && s.codexTurnStateAccountProbeBlocked(account.ID, time.Now()) {
		return "", "", errCodexTurnStateAccountProbeCooldown
	}
	// The candidate is not cacheable until the exact blob succeeds in a new
	// request on the same sticky route with the same account and model.
	_, replayObserver, err := s.requestOpenAICodexTurnStateViaProxyWithMode(ctx, account, model, proxyURL, state, false, activeRound)
	if err != nil {
		if errors.Is(err, errCodexTurnStateAccountProbeCooldown) || errors.Is(err, errCodexTurnStateProbeBoundaryRefresh) {
			return "", "", err
		}
		var httpErr *codexTurnStateProbeHTTPError
		if errors.As(err, &httpErr) {
			// A replay can itself be rate limited. Preserve the typed 429 so the
			// worker installs the boundary for later automatic rounds while the
			// current round remains free to try its next IP.
			return "", "", httpErr
		}
		if errors.Is(err, errCodexTurnStateResponseModelMismatch) || errors.Is(err, errCodexTurnStateResponseModelMissing) ||
			errors.Is(err, errCodexTurnStateResponseNotCompleted) || errors.Is(err, errCodexTurnStateResponseFailed) {
			return "", "", err
		}
		return "", "", codexTurnStateAutoError("state_replay_failed")
	}
	_, completedModel, _ := replayObserver.CodexTurnStateEvidence()
	return state, strings.TrimSpace(completedModel), nil
}

func (s *OpenAIGatewayService) requestOpenAICodexTurnStateViaProxy(ctx context.Context, account *Account, model, proxyURL, sentState string, requireState bool) (string, *upstreamResponseModelObserver, error) {
	return s.requestOpenAICodexTurnStateViaProxyWithMode(ctx, account, model, proxyURL, sentState, requireState, false)
}

func (s *OpenAIGatewayService) loadCodexTurnStateCollectionAccount(ctx context.Context, accountID int64) (*Account, error) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return nil, codexTurnStateAutoError("account_unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	account, err := s.accountRepo.GetByID(dbCtx, accountID)
	if err != nil || account == nil || account.ID != accountID || !codexTurnStateCollectionEligible(account) {
		return nil, codexTurnStateAutoError("account_unavailable")
	}
	return account, nil
}

func (s *OpenAIGatewayService) requestOpenAICodexTurnStateViaProxyWithMode(ctx context.Context, account *Account, model, proxyURL, sentState string, requireState, activeRound bool) (string, *upstreamResponseModelObserver, error) {
	if s == nil || s.httpUpstream == nil || account == nil {
		return "", nil, codexTurnStateAutoError("account_unavailable")
	}
	account, err := s.loadCodexTurnStateCollectionAccount(ctx, account.ID)
	if err != nil {
		return "", nil, err
	}
	if !activeRound {
		blocked, boundaryErr := s.refreshCodexTurnStateAccountProbeBoundary(ctx, account.ID)
		if boundaryErr != nil {
			return "", nil, boundaryErr
		}
		if blocked {
			return "", nil, errCodexTurnStateAccountProbeCooldown
		}
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
		case CodexTurnStateAutoExtraKey, CodexTurnStateAutoSetAtExtraKey, CodexTurnStateAutoProbeAtExtraKey, CodexTurnStateAutoProbeCompletedAtExtraKey, CodexTurnStateAutoLastErrorExtraKey,
			CodexTurnStateAutoVerifiedAtExtraKey, CodexTurnStateAutoVerifiedModelExtraKey, CodexTurnStateAutoProbeModelExtraKey,
			CodexTurnStateAutoProbeNotBeforeExtraKey, CodexTurnStateAutoRecoveryExtraKey:
			continue
		}
		result[key] = value
	}
	return result
}
