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
	codexTurnStateAutoMaxWorkers             = 8
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
	queued          bool
	retryAfter      time.Time // persistence retry boundary
	probeRetryAfter time.Time
	probe           bool
	forceProbe      bool
	recovery        codexTurnStateRecovery
	candidate       codexTurnStateUsageCandidate
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
		"response_model_mismatch", "response_model_missing", "response_not_completed", "response_failed", "state_replay_failed", "maintenance_route_unavailable",
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
// window before a new exit, same-exit replay, or daily-route replay is sent.
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

// Must hold openaiTurnStateMu. Account snapshots are read-only: background
// work never mutates the scheduler/request's Extra or Credentials maps.
func (s *OpenAIGatewayService) codexTurnStateEntryLocked(account *Account, now time.Time, models ...string) *codexTurnStateAutoEntry {
	requestModel := s.codexTurnStateModel(context.Background(), models...)
	model := codexTurnStateOwnerModel(requestModel)
	key := codexTurnStateKey{account.ID, model}
	accountProbeNotBefore := codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeNotBeforeExtraKey)
	account = codexTurnStateModelAccount(account, model)
	if s.openaiTurnStates == nil {
		s.openaiTurnStates = make(map[codexTurnStateKey]*codexTurnStateAutoEntry)
	}
	if now.Sub(s.openaiTurnStateSweep) >= time.Hour {
		for id, entry := range s.openaiTurnStates {
			if !entry.running && !entry.queued && now.Sub(entry.lastUsed) > 2*time.Hour {
				delete(s.openaiTurnStates, id)
			}
		}
		s.openaiTurnStateSweep = now
	}
	entry := s.openaiTurnStates[key]
	if entry == nil {
		entry = &codexTurnStateAutoEntry{model: model, requestModel: requestModel, knownTokens: make(map[string]int64), recovery: codexTurnStateRecoveryFromAccount(account), lastError: safeCodexTurnStateAutoError(account.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))}
		s.openaiTurnStates[key] = entry
	} else if requestModel != "" {
		entry.requestModel = requestModel
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

// Native continuation > account automatic fallback.
// Near-expiry values remain usable while renewal runs; expired automatic values
// are omitted. No probe is awaited by the caller.
func (s *OpenAIGatewayService) autoTurnStateForAccount(ctx context.Context, account *Account, models ...string) string {
	if !codexTurnStateAutoEligible(account) || !s.codexTurnStateAutoEnabled(ctx) {
		return ""
	}
	cfg := s.settingService.GetOpenAICodexTurnState(ctx)
	if !codexTurnStateModelMatches(cfg.Models, models...) {
		return ""
	}
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	entry := s.codexTurnStateEntryLocked(account, now, s.codexTurnStateModel(ctx, models...))
	if entry.reconciling {
		return ""
	}

	expiry := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	candidatePending := codexTurnStateUsageCandidateActiveLocked(entry, now)
	if candidatePending {
		// A candidate already passed the bounded maintenance round and is waiting
		// for its dedicated usage-evidence request. Do not launch another probe from
		// the very request that reserves it.
		entry.probe = false
	}
	if !codexTurnStateManualVerification(ctx) && !candidatePending && (entry.recovery.Pending || entry.token == "" || expiry == 0 || now.Add(codexTurnStateAutoRenewBefore).UnixMilli() >= expiry) {
		if !now.Before(time.UnixMilli(entry.probeNotBefore)) && (entry.forceProbe || entry.probeAt <= 0 || now.Sub(time.UnixMilli(entry.probeAt)) >= codexTurnStateAutoProbeInterval) {
			entry.probe = true
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
	entry.forceProbe = false
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
	if s.openaiTurnStateWorkers >= codexTurnStateAutoMaxWorkers {
		if !entry.queued {
			s.openaiTurnStatePending = append(s.openaiTurnStatePending, codexTurnStateKey{id, entry.model})
			entry.queued = true
		}
		return
	}
	entry.running = true
	s.openaiTurnStateWorkers++
	go s.runCodexTurnStateWorker(id, entry)
}
func (s *OpenAIGatewayService) runCodexTurnStateWorker(id int64, entry *codexTurnStateAutoEntry) {
	defer func() {
		enabled := s.codexTurnStateAutoEnabled(context.Background())
		s.openaiTurnStateMu.Lock()
		defer s.openaiTurnStateMu.Unlock()
		entry.running = false
		s.openaiTurnStateWorkers--
		if !enabled {
			return
		}
		// Drain the bounded-concurrency queue even if a busy account receives
		// no subsequent requests. Each account occupies at most one queue slot.
		for len(s.openaiTurnStatePending) > 0 && s.openaiTurnStateWorkers < codexTurnStateAutoMaxWorkers {
			nextID := s.openaiTurnStatePending[0]
			s.openaiTurnStatePending = s.openaiTurnStatePending[1:]
			if next := s.openaiTurnStates[nextID]; next != nil {
				next.queued = false
				s.startCodexTurnStateWorkerLocked(nextID.accountID, next)
			}
		}
		if len(s.openaiTurnStatePending) == 0 {
			s.openaiTurnStatePending = nil
		}
		s.startCodexTurnStateWorkerLocked(id, entry)
	}()
	// Bounded even under a continuous stream of new collected states. Dirty work
	// left after the bound yields to queued accounts before continuing.
	for n := 0; n < 16; n++ {
		if !s.codexTurnStateAutoEnabled(context.Background()) {
			return
		}
		s.openaiTurnStateMu.Lock()
		dirty, probe := entry.dirty, entry.probe
		entry.dirty, entry.probe = false, false
		s.openaiTurnStateMu.Unlock()
		if !dirty && !probe {
			return
		}
		if dirty {
			err := s.persistCodexTurnState(id, entry)
			if err != nil {
				s.openaiTurnStateMu.Lock()
				entry.dirty = true
				entry.retryAfter = time.Now().Add(5 * time.Second)
				s.openaiTurnStateMu.Unlock()
				slog.Warn("openai_codex_turn_state_collect_failed", "account_id", id, "code", "persistence_failed")
				return
			}
		}
		if probe {
			s.runCodexTurnStateProbe(id, entry)
			s.openaiTurnStateMu.Lock()
			persistenceDeferred := entry.dirty && time.Now().Before(entry.retryAfter)
			s.openaiTurnStateMu.Unlock()
			if persistenceDeferred {
				return
			}
		}
	}
}
func (s *OpenAIGatewayService) persistCodexTurnState(id int64, entry *codexTurnStateAutoEntry) error {
	s.openaiTurnStateMu.Lock()
	accountProbeNotBefore := s.codexTurnStateAccountProbeNotBeforeLocked(id)
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
		return nil
	}
	return s.accountRepo.UpdateExtra(ctx, id, updates)
}

// Probe failures may carry an account-wide Retry-After boundary. Retry only the
// database write a bounded number of times; never schedule another upstream
// request as a side effect. If every attempt fails, retain dirty work for a
// later request while the in-memory boundary continues to block probing.
func (s *OpenAIGatewayService) persistCodexTurnStateProbeOutcome(id int64, entry *codexTurnStateAutoEntry) bool {
	for attempt := 0; attempt < codexTurnStateProbePersistAttempts; attempt++ {
		if err := s.persistCodexTurnState(id, entry); err == nil {
			return true
		}
	}
	s.openaiTurnStateMu.Lock()
	entry.dirty = true
	entry.probe = false
	entry.retryAfter = time.Now().Add(5 * time.Second)
	s.openaiTurnStateMu.Unlock()
	return false
}

// A zero-row CAS means another process owns the authoritative slot. Block all
// automatic/candidate injection while that winner is read back; otherwise the
// losing process can keep serving its local value during the database round trip.
func (s *OpenAIGatewayService) beginCodexTurnStateCASReconciliationLocked(entry *codexTurnStateAutoEntry) {
	entry.reconciling = true
	entry.token, entry.setAt = "", 0
	entry.verifiedAt, entry.verifiedModel = 0, ""
	entry.candidate = codexTurnStateUsageCandidate{}
	entry.knownTokens = make(map[string]int64)
	entry.dirty, entry.probe, entry.forceProbe = false, false, false
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
	entry.reconciling = false
	entry.token, entry.setAt = "", 0
	entry.verifiedAt, entry.verifiedModel = 0, ""
	entry.candidate = codexTurnStateUsageCandidate{}
	entry.knownTokens = make(map[string]int64)
	entry.dirty, entry.probe, entry.forceProbe = false, false, false
	entry.retryAfter, entry.probeRetryAfter = time.Time{}, time.Time{}
	entry.loadedAt = now
	if current == nil {
		return
	}

	accountProbeNotBefore := codexTurnStateAutoInt64(current, CodexTurnStateAutoProbeNotBeforeExtraKey)
	slot := codexTurnStateModelAccount(current, entry.model)
	entry.setAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoSetAtExtraKey)
	entry.verifiedAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoVerifiedAtExtraKey)
	entry.verifiedModel = codexTurnStateOwnerModel(slot.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey))
	entry.token = codexTurnStateAutoToken(slot)
	if entry.verifiedModel != entry.model {
		entry.token = ""
	}
	entry.recovery = codexTurnStateRecoveryFromAccount(slot)
	entry.probeNotBefore = codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeNotBeforeExtraKey)
	if accountProbeNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = accountProbeNotBefore
	}
	if sharedNotBefore := s.codexTurnStateAccountProbeNotBeforeLocked(current.ID); sharedNotBefore > entry.probeNotBefore {
		entry.probeNotBefore = sharedNotBefore
	}
	entry.probeAt = codexTurnStateAutoInt64(slot, CodexTurnStateAutoProbeAtExtraKey)
	entry.lastError = safeCodexTurnStateAutoError(slot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
	if entry.probeNotBefore > now.UnixMilli() {
		entry.probeRetryAfter = time.UnixMilli(entry.probeNotBefore)
	}
	s.rememberCodexTurnStateLocked(entry, now)
}
func (s *OpenAIGatewayService) runCodexTurnStateProbe(id int64, entry *codexTurnStateAutoEntry) {
	if s.httpUpstream == nil {
		return
	}
	lookupCtx, cancelLookup := context.WithTimeout(context.Background(), gatewayForwardingDBTimeout)
	account, err := s.accountRepo.GetByID(lookupCtx, id)
	cancelLookup()
	if err != nil || !codexTurnStateAutoEligible(account) || !account.IsSchedulable() {
		return
	}
	// Re-read persisted timestamps before spending upstream quota. This reduces
	// cross-instance duplicates; it is deliberately not a distributed lock.
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	s.codexTurnStateEntryLocked(account, now, entry.model)
	// startCodexTurnStateWorkerLocked checks this boundary before launching a
	// worker. Check it again here because a concurrent invalidation can enqueue a
	// forced probe while the current worker is still handling the 429 response.
	if now.Before(entry.probeRetryAfter) || now.Before(time.UnixMilli(entry.probeNotBefore)) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	expiry := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	if !entry.forceProbe && ((!entry.recovery.Pending && entry.token != "" && expiry > now.Add(codexTurnStateAutoRenewBefore).UnixMilli()) || (entry.probeAt > 0 && now.Sub(time.UnixMilli(entry.probeAt)) < codexTurnStateAutoProbeInterval)) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	entry.probeAt = now.UnixMilli()
	entry.forceProbe = false
	generation := entry.recovery.InvalidatedAtMS
	before := entry.token
	burst := codexTurnStateProbeNeedsBurst(entry, now)
	burstGeneration := codexTurnStateProbeBurstGeneration(entry)
	burstModel := codexTurnStateProbeBurstOwnerModel(entry.model)
	probeModel := entry.requestModel
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
	if !s.codexTurnStateAutoEnabled(ctx) {
		return
	}
	// Probe metadata persistence is database-only. A storage failure must stop
	// before any upstream request and leave bounded retry work behind.
	if !s.persistCodexTurnStateProbeOutcome(id, entry) {
		return
	}
	// Maintenance collection requires a dedicated route that differs from the
	// account's daily route. Falling back to the daily route would turn a normal
	// request path into a purported fresh-exit collection and could publish a
	// candidate without ever proving cross-route reuse.
	primaryRoute := codexTurnStateAccountProxy(account)
	routes := s.codexTurnStatePoolRoutes(ctx, primaryRoute)
	if len(routes) == 0 {
		s.openaiTurnStateMu.Lock()
		if entry.token == before && entry.recovery.InvalidatedAtMS == generation {
			entry.lastError = "maintenance_route_unavailable"
			s.openaiTurnStateMu.Unlock()
			if !s.persistCodexTurnStateProbeOutcome(id, entry) {
				slog.Warn("openai_codex_turn_state_probe_persist_failed", "account_id", id, "code", "persistence_failed")
			}
			slog.Warn("openai_codex_turn_state_probe_failed", "account_id", id, "code", "maintenance_route_unavailable")
			return
		}
		s.openaiTurnStateMu.Unlock()
		return
	}
	for attempt := 0; attempt < len(routes); attempt++ {
		if ctx.Err() != nil || !s.codexTurnStateAutoEnabled(ctx) {
			return
		}
		s.openaiTurnStateMu.Lock()
		now := time.Now()
		stale := entry.token != before || entry.recovery.InvalidatedAtMS != generation
		blocked := now.Before(time.UnixMilli(s.codexTurnStateAccountProbeNotBeforeLocked(id)))
		s.openaiTurnStateMu.Unlock()
		if stale || blocked {
			return
		}
		attemptDeadline := time.Time{}
		reservation := codexTurnStateProbeBurstReservation{}
		if burst {
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
		// Renewal and every IP retry must use the model that owns this state.
		model := probeModel
		attemptCtx, cancelAttempt := codexTurnStateProbeAttemptContext(ctx, attemptDeadline)
		state, probeErr := s.probeOpenAICodexTurnStateViaProxy(attemptCtx, account, model, routes[attempt])
		cancelAttempt()
		if errors.Is(probeErr, errCodexTurnStateAccountProbeCooldown) {
			return
		}
		if probeErr == nil && primaryRoute != routes[attempt] {
			// The candidate already passed a same-route replay. Before publication,
			// prove that the exact state also works after returning to the account's
			// normal proxy; a failed daily-route replay cannot replace the old value.
			if s.codexTurnStateAccountProbeBlocked(id, time.Now()) {
				return
			}
			replayCtx, cancelReplay := codexTurnStateProbeReplayContext(ctx, attemptDeadline)
			replayErr := s.replayOpenAICodexTurnStateViaProxy(replayCtx, account, model, primaryRoute, state)
			cancelReplay()
			if replayErr != nil {
				if errors.Is(replayErr, errCodexTurnStateAccountProbeCooldown) {
					return
				}
				var httpErr *codexTurnStateProbeHTTPError
				if errors.Is(replayErr, errCodexTurnStateProbeBoundaryRefresh) {
					probeErr = replayErr
				} else if errors.As(replayErr, &httpErr) {
					probeErr = httpErr
				} else if errors.Is(replayErr, errCodexTurnStateResponseModelMismatch) || errors.Is(replayErr, errCodexTurnStateResponseModelMissing) ||
					errors.Is(replayErr, errCodexTurnStateResponseNotCompleted) || errors.Is(replayErr, errCodexTurnStateResponseFailed) {
					probeErr = replayErr
				} else {
					probeErr = codexTurnStateAutoError("state_replay_failed")
				}
			}
		}
		if !s.codexTurnStateAutoEnabled(ctx) {
			return
		}
		if probeErr == nil && burst {
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
			won, pendingErr := s.markCodexTurnStateProbeCandidatePending(ctx, id, burstModel, burstGeneration, reservation)
			if pendingErr != nil {
				probeErr = pendingErr
			} else if !won {
				// Another instance already staged the first valid candidate for this
				// model/generation. Discard this opaque blob without persisting it.
				return
			}
		}
		s.openaiTurnStateMu.Lock()
		now = time.Now()
		if entry.token != before || entry.recovery.InvalidatedAtMS != generation ||
			now.Before(time.UnixMilli(s.codexTurnStateAccountProbeNotBeforeLocked(id))) {
			s.openaiTurnStateMu.Unlock()
			return
		}
		err = probeErr
		if err == nil {
			// The maintenance checks make this eligible for a formal acceptance
			// request, but they are not durable usage evidence. Keep the old
			// verified token active until /v1/responses persists a matching row.
			collectedAt := time.UnixMilli(entry.probeAt)
			if collectedAt.IsZero() || collectedAt.After(now) {
				collectedAt = now
			}
			s.stageCodexTurnStateUsageCandidateLocked(entry, state, generation, collectedAt)
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
		if retryable && burst {
			if releaseErr := s.releaseCodexTurnStateProbeBurstAttempt(ctx, id, burstModel, burstGeneration, reservation); releaseErr != nil {
				err = releaseErr
				retryable = false
			}
		}
		if !retryable {
			break
		}
	}
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	if entry.token != before || entry.recovery.InvalidatedAtMS != generation {
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
	if !s.persistCodexTurnStateProbeOutcome(id, entry) {
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

func (s *OpenAIGatewayService) replayOpenAICodexTurnStateViaProxy(ctx context.Context, account *Account, model, proxyURL, state string) error {
	_, _, err := s.requestOpenAICodexTurnStateViaProxy(ctx, account, model, proxyURL, state, false)
	return err
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
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
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
