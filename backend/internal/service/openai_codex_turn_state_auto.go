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
)

const (
	CodexTurnStateAutoExtraKey          = "codex_turn_state_auto"
	CodexTurnStateAutoSetAtExtraKey     = "codex_turn_state_auto_set_at_ms"
	CodexTurnStateAutoProbeAtExtraKey   = "codex_turn_state_auto_probe_at_ms"
	CodexTurnStateAutoLastErrorExtraKey = "codex_turn_state_auto_last_error"
	codexTurnStateAutoProbeInterval     = 5 * time.Minute
	codexTurnStateAutoRenewBefore       = 10 * time.Minute
	codexTurnStateAutoMaxBody           = 128 << 10
	codexTurnStateAutoMaxWorkers        = 8
)

// Only fixed error codes are persisted or logged: provider errors can contain
// bearer tokens, proxy credentials, URLs or response bodies.
type codexTurnStateAutoError string

func (e codexTurnStateAutoError) Error() string { return string(e) }

// CodexTurnStateAutoInfo contains no token. Expiry is a reference TTL, not an
// upstream guarantee. Unknown envelopes age from their first collection time.
type CodexTurnStateAutoInfo struct {
	Models          map[string]CodexTurnStateAutoInfo `json:"models,omitempty"`
	Configured      bool                              `json:"configured"`
	SetAtMS         int64                             `json:"set_at_ms,omitempty"`
	ProbeAtMS       int64                             `json:"probe_at_ms,omitempty"`
	ExpiresAtMS     int64                             `json:"expires_at_ms,omitempty"`
	Due             bool                              `json:"due"`
	LastError       string                            `json:"last_error,omitempty"`
	RecoveryPending bool                              `json:"recovery_pending"`
	InvalidatedAtMS int64                             `json:"invalidated_at_ms,omitempty"`
}

type codexTurnStateAutoEntry struct {
	model          string
	loadedAt       time.Time
	knownTokens    map[string]int64
	token          string
	setAt, probeAt int64
	lastError      string
	lastUsed       time.Time
	dirty, running bool
	queued         bool
	retryAfter     time.Time
	probe          bool
	forceProbe     bool
	recovery       codexTurnStateRecovery
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
	info := CodexTurnStateAutoInfo{Configured: token != "", SetAtMS: codexTurnStateAutoInt64(account, CodexTurnStateAutoSetAtExtraKey), ProbeAtMS: codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeAtExtraKey)}
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
	case "state_312", "recovery_requires_new_292", "account_unavailable", "persistence_failed", "auth_failed", "transport_failed", "timeout", "missing_state", "invalid_state", "empty_response", "request_failed":
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
func (s *OpenAIGatewayService) codexTurnStateAutoEnabled(ctx context.Context) bool {
	return s != nil && s.settingService != nil && s.settingService.GetOpenAICodexTurnState(ctx).AutoEnabled
}

// Must hold openaiTurnStateMu. Account snapshots are read-only: background
// work never mutates the scheduler/request's Extra or Credentials maps.
func (s *OpenAIGatewayService) codexTurnStateEntryLocked(account *Account, now time.Time, models ...string) *codexTurnStateAutoEntry {
	model := s.codexTurnStateModel(context.Background(), models...)
	key := codexTurnStateKey{account.ID, model}
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
		entry = &codexTurnStateAutoEntry{model: model, knownTokens: make(map[string]int64), recovery: codexTurnStateRecoveryFromAccount(account), lastError: safeCodexTurnStateAutoError(account.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))}
		s.openaiTurnStates[key] = entry
	}
	persistedRecovery := codexTurnStateRecoveryFromAccount(account)
	if persistedRecovery.InvalidatedAtMS > entry.recovery.InvalidatedAtMS {
		entry.recovery = persistedRecovery
		entry.token = ""
	}
	setAt := codexTurnStateAutoInt64(account, CodexTurnStateAutoSetAtExtraKey)
	token := codexTurnStateAutoToken(account)
	if !entry.dirty && (setAt > entry.setAt || entry.token == "") && entry.recovery.allows(token, now) {
		entry.token, entry.setAt = token, setAt
		if entry.recovery.Pending {
			entry.recovery.Pending = false
			entry.dirty = true
		}
	}
	if at := codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeAtExtraKey); at > entry.probeAt {
		entry.probeAt = at
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

	expiry := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	if entry.recovery.Pending || entry.token == "" || expiry == 0 || now.Add(codexTurnStateAutoRenewBefore).UnixMilli() >= expiry {
		if entry.forceProbe || entry.probeAt <= 0 || now.Sub(time.UnixMilli(entry.probeAt)) >= codexTurnStateAutoProbeInterval {
			entry.probe = true
		}
	}
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	if entry.recovery.Pending || !entry.recovery.allows(entry.token, now) || expiry == 0 || now.UnixMilli() >= expiry {
		return ""
	}
	return entry.token
}

// Called only at a committed HTTP response or successful WS handshake. The
// worker coalesces updates and serializes collection with renewal per account.
func (s *OpenAIGatewayService) collectOpenAICodexTurnState(ctx context.Context, account *Account, state string, sent ...string) {
	s.collectOpenAICodexTurnStateAtEpoch(ctx, account, state, "", sent...)
}
func (s *OpenAIGatewayService) collectOpenAICodexTurnStateAtEpoch(ctx context.Context, account *Account, state, epoch string, sent ...string) {
	state = strings.TrimSpace(state)
	if state == "" || ValidateOpenAICodexTurnState(state) != nil || !codexTurnStateAutoEligible(account) || !s.codexTurnStateAutoEnabled(ctx) {
		return
	}
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	entry := s.codexTurnStateEntryLocked(account, now, s.codexTurnStateModel(ctx))
	if epoch != "" && epoch != strconv.FormatInt(entry.recovery.InvalidatedAtMS, 10) {
		return
	}
	if codexTurnStateIsRecoverySignal(state) {
		s.invalidateCodexTurnStateLocked(entry, state, now, sent...)
	} else {
		s.setCodexTurnStateLocked(entry, state, now)
	}
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
}
func (s *OpenAIGatewayService) setCodexTurnStateLocked(entry *codexTurnStateAutoEntry, state string, now time.Time) {
	if !entry.recovery.allows(state, now) {
		return
	}
	entry.recovery.Pending = false
	entry.forceProbe = false
	if state == entry.token && entry.setAt > 0 {
		// Successful reuse clears a previous failure without renewing token age.
		if entry.lastError != "" {
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
	entry.token, entry.setAt, entry.lastError, entry.dirty = state, now.UnixMilli(), "", true
	s.rememberCodexTurnStateLocked(entry, now)
}
func (s *OpenAIGatewayService) startCodexTurnStateWorkerLocked(id int64, entry *codexTurnStateAutoEntry) {
	if s.accountRepo == nil || entry.running || (!entry.dirty && !entry.probe) || time.Now().Before(entry.retryAfter) {
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
		}
	}
}
func (s *OpenAIGatewayService) persistCodexTurnState(id int64, entry *codexTurnStateAutoEntry) error {
	s.openaiTurnStateMu.Lock()
	updates := map[string]any{codexTurnStateModelExtraKey(entry.model): map[string]any{CodexTurnStateAutoExtraKey: entry.token, CodexTurnStateAutoSetAtExtraKey: entry.setAt, CodexTurnStateAutoProbeAtExtraKey: entry.probeAt, CodexTurnStateAutoLastErrorExtraKey: entry.lastError, CodexTurnStateAutoRecoveryExtraKey: entry.recovery.clone()}}
	s.openaiTurnStateMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.accountRepo.UpdateExtra(ctx, id, updates)
}
func (s *OpenAIGatewayService) runCodexTurnStateProbe(id int64, entry *codexTurnStateAutoEntry) {
	if s.httpUpstream == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), codexTurnStateProbeTotalTimeout)
	defer cancel()
	account, err := s.accountRepo.GetByID(ctx, id)
	if err != nil || !codexTurnStateAutoEligible(account) || !account.IsSchedulable() {
		return
	}
	// Re-read persisted timestamps before spending upstream quota. This reduces
	// cross-instance duplicates; it is deliberately not a distributed lock.
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	s.codexTurnStateEntryLocked(account, now, entry.model)
	expiry := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	if !entry.forceProbe && ((!entry.recovery.Pending && entry.token != "" && expiry > now.Add(codexTurnStateAutoRenewBefore).UnixMilli()) || (entry.probeAt > 0 && now.Sub(time.UnixMilli(entry.probeAt)) < codexTurnStateAutoProbeInterval)) {
		s.openaiTurnStateMu.Unlock()
		return
	}
	entry.probeAt = now.UnixMilli()
	entry.forceProbe = false
	generation := entry.recovery.InvalidatedAtMS
	before := entry.token
	s.openaiTurnStateMu.Unlock()
	if !s.codexTurnStateAutoEnabled(ctx) {
		return
	}
	if err := s.persistCodexTurnState(id, entry); err != nil {
		return
	}
	// The account route always gets the first attempt. Load the pool only after
	// failure, so healthy accounts do not query it or consume additional quota.
	routes := []string{codexTurnStateAccountProxy(account)}
	for attempt := 0; attempt < len(routes); attempt++ {
		if ctx.Err() != nil || !s.codexTurnStateAutoEnabled(ctx) {
			return
		}
		s.openaiTurnStateMu.Lock()
		stale := entry.token != before || entry.recovery.InvalidatedAtMS != generation
		s.openaiTurnStateMu.Unlock()
		if stale {
			return
		}
		// Renewal and every IP retry must use the model that owns this state.
		model := entry.model
		attemptCtx, cancelAttempt := context.WithTimeout(ctx, codexTurnStateProbeAttemptTimeout)
		state, probeErr := s.probeOpenAICodexTurnStateViaProxy(attemptCtx, account, model, routes[attempt])
		cancelAttempt()
		if !s.codexTurnStateAutoEnabled(ctx) {
			return
		}
		s.openaiTurnStateMu.Lock()
		if entry.token != before || entry.recovery.InvalidatedAtMS != generation {
			s.openaiTurnStateMu.Unlock()
			return
		}
		err = probeErr
		var revocation bool
		now := time.Now()
		if codexTurnStateIsRecoverySignal(state) {
			s.invalidateCodexTurnStateLocked(entry, state, now)
			// Continue this bounded round, never recursively schedule probes.
			entry.forceProbe, entry.probe = false, false
			generation, before = entry.recovery.InvalidatedAtMS, entry.token
			revocation = true
			if err == nil {
				err = codexTurnStateAutoError("state_312")
			}
		} else if err == nil {
			if codexTurnStateFresh292(state, now) && entry.recovery.allows(state, now) {
				s.setCodexTurnStateLocked(entry, state, now)
				s.openaiTurnStateMu.Unlock()
				return
			}
			err = codexTurnStateAutoError("recovery_requires_new_292")
		}
		s.openaiTurnStateMu.Unlock()
		// Persist revocation before more network work so a restart during a
		// slow pool round cannot reload the now-revoked state.
		if revocation {
			if persistErr := s.persistCodexTurnState(id, entry); persistErr != nil {
				return // The owning worker retains dirty state and retries persistence.
			}
		}
		// Authentication, quota and invalid payload failures are not route
		// failures. Preserve the upstream rejection rather than rotate around it.
		if !codexTurnStateProbeRetryable(err) || ctx.Err() != nil {
			break
		}
		if attempt == 0 {
			routes = append(routes, s.codexTurnStatePoolRoutes(ctx, routes[0])...)
		}
	}
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	if entry.token != before || entry.recovery.InvalidatedAtMS != generation {
		return
	}
	code := safeCodexTurnStateAutoError(err.Error())
	if code == "" {
		code = "request_failed"
	}
	entry.lastError = code
	// This worker owns persistence; release the cache mutex during DB I/O.
	s.openaiTurnStateMu.Unlock()
	_ = s.persistCodexTurnState(id, entry)
	slog.Warn("openai_codex_turn_state_probe_failed", "account_id", id, "code", code)
	s.openaiTurnStateMu.Lock()
}

func (s *OpenAIGatewayService) probeOpenAICodexTurnState(ctx context.Context, account *Account, model string) (string, error) {
	return s.probeOpenAICodexTurnStateViaProxy(ctx, account, model, codexTurnStateAccountProxy(account))
}

func (s *OpenAIGatewayService) probeOpenAICodexTurnStateViaProxy(ctx context.Context, account *Account, model, proxyURL string) (string, error) {
	if s == nil || s.httpUpstream == nil || !codexTurnStateAutoEligible(account) {
		return "", codexTurnStateAutoError("account_unavailable")
	}
	if strings.TrimSpace(model) == "" {
		model = s.settingService.GetOpenAICodexTurnState(ctx).DefaultModel
	}
	payload := createOpenAITestPayload(model, true)
	payload["instructions"] = "Reply with OK only."
	payloadBytes, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexAPIURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", codexTurnStateAutoError("request_failed")
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil {
		return "", codexTurnStateAutoError("auth_failed")
	}
	applyOpenAIAccountTestHeaders(req, account, "responses", payloadBytes)
	// Setup tokens use exactly the same Codex endpoint and identity as OAuth.
	req.Host = "chatgpt.com"
	setOpenAIChatGPTAccountHeaders(req.Header, account)
	enforceCodexIdentityHeadersWithAccount(req.Header, account)
	auth, err := s.buildOpenAIAuthenticationHeaders(ctx, account, token)
	if err != nil {
		return "", codexTurnStateAutoError("auth_failed")
	}
	for name, values := range auth {
		req.Header[name] = values
	}
	req.Header.Del(openAICodexTurnStateHeader)
	SanitizeOutboundGatewayIdentity(req.Header)
	// Match the OpenAI gateway's ordinary transport (including its proxy).
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", codexTurnStateAutoError("timeout")
		}
		return "", codexTurnStateAutoError("transport_failed")
	}
	if resp == nil {
		return "", codexTurnStateAutoError("empty_response")
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}
	state := extractOpenAICodexTurnState(resp.Header)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if !codexTurnStateIsRecoverySignal(state) {
			state = ""
		}
		return state, codexTurnStateAutoError(fmt.Sprintf("http_%d", resp.StatusCode))
	}
	if state == "" {
		return "", codexTurnStateAutoError("missing_state")
	}
	if ValidateOpenAICodexTurnState(state) != nil {
		return "", codexTurnStateAutoError("invalid_state")
	}
	// Allow the tiny response to finish for connection reuse. Cap only this
	// maintenance response, never a user stream. The context bounds slow bodies.
	if resp.Body != nil && !codexTurnStateIsRecoverySignal(state) {
		_, _ = io.CopyN(io.Discard, resp.Body, codexTurnStateAutoMaxBody)
	}
	return state, nil
}

// StripCodexTurnStateAutoExtra returns a copy suitable for account imports,
// exports and user edits. Collected state must never move to another account.
func StripCodexTurnStateAutoExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	result := make(map[string]any, len(extra))
	for key, value := range extra {
		if strings.HasPrefix(key, CodexTurnStateModelExtraPrefix) {
			continue
		}
		switch key {
		case CodexTurnStateAutoExtraKey, CodexTurnStateAutoSetAtExtraKey, CodexTurnStateAutoProbeAtExtraKey, CodexTurnStateAutoLastErrorExtraKey, CodexTurnStateAutoRecoveryExtraKey:
			continue
		}
		result[key] = value
	}
	return result
}
