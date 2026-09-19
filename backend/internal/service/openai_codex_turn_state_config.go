package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/pkg/openai"
)

const (
	codexTurnStateModelsMaxLen = 1024
	codexTurnStateMaxLength    = 4096
	codexTurnStateTTL          = time.Hour
)

// Only the HTTP representation is validated. Envelope heuristics are diagnostics,
// not signature verification, and do not prove quality or resource availability.
func ValidateOpenAICodexTurnState(value string) error {
	value = strings.TrimSpace(value)
	if len(value) > codexTurnStateMaxLength {
		return fmt.Errorf("openai_codex_turn_state must be at most %d bytes", codexTurnStateMaxLength)
	}
	for _, ch := range []byte(value) {
		if ch < 0x20 || ch > 0x7e {
			return fmt.Errorf("openai_codex_turn_state must contain only printable ASCII characters")
		}
	}
	return nil
}

func NormalizeOpenAICodexTurnStateModels(raw string) (string, error) {
	seen := make(map[string]bool)
	out := []string{}
	for _, entry := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if entry == "" {
			continue
		}
		body := strings.TrimSuffix(entry, "*")
		for _, ch := range body {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-_.:/", ch)) {
				return "", fmt.Errorf("openai_codex_turn_state_models contains an invalid model pattern")
			}
		}
		if !seen[entry] {
			out = append(out, entry)
			seen[entry] = true
		}
	}
	value := strings.Join(out, ",")
	if len(value) > codexTurnStateModelsMaxLen {
		return "", fmt.Errorf("openai_codex_turn_state_models must be at most %d bytes", codexTurnStateModelsMaxLen)
	}
	return value, nil
}

// An exact, freely editable model ID; empty resets to the built-in default.
func NormalizeOpenAICodexTurnStateDefaultModel(raw string) (string, error) {
	model := strings.TrimSpace(raw)
	if model == "" {
		return openai.DefaultTestModel, nil
	}
	if len(model) > 128 {
		return "", fmt.Errorf("openai_codex_turn_state_default_model must be at most 128 bytes")
	}
	for _, ch := range model {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-_.:/", ch)) {
			return "", fmt.Errorf("openai_codex_turn_state_default_model must be a single model ID without spaces or wildcards")
		}
	}
	return model, nil
}

func codexTurnStateModelMatches(scope string, models ...string) bool {
	if scope == "" {
		return true
	}
	known := false
	for _, model := range models {
		model = strings.ToLower(strings.TrimSpace(model))
		if model == "" {
			continue
		}
		known = true
		for _, pattern := range strings.Split(strings.ToLower(scope), ",") {
			pattern = strings.TrimSpace(pattern)
			if pattern == model || strings.HasSuffix(pattern, "*") && strings.HasPrefix(model, strings.TrimSuffix(pattern, "*")) {
				return true
			}
		}
	}
	return !known
}

// NormalizeOpenAICodexTurnStateProxyID accepts an empty value as the legacy
// automatic-selection mode and otherwise requires a non-negative proxy ID.
func NormalizeOpenAICodexTurnStateProxyID(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, fmt.Errorf("openai_codex_turn_state_proxy_id must be zero or a positive proxy ID")
	}
	return id, nil
}

// Only account-scoped lifecycle settings are read. Legacy global manual tokens
// remain inert. ModelScopeValid keeps malformed historical values fail-closed
// without discarding an independently valid default model or dedicated proxy.
type OpenAICodexTurnStateConfig struct {
	DefaultModel    string
	Models          string
	ModelScopeValid bool
	AutoEnabled     bool
	ProxyID         int64
}
type cachedOpenAICodexTurnState struct {
	config    OpenAICodexTurnStateConfig
	expiresAt time.Time
}

// GetOpenAICodexTurnState is instance-local and hot-path cached. The same mutex
// as persistence serializes cold reads/publication with saves, so a slow stale
// load cannot overwrite an administrator's newly disabled setting.
func (s *SettingService) GetOpenAICodexTurnState(ctx context.Context) OpenAICodexTurnStateConfig {
	if s == nil || s.settingRepo == nil {
		return OpenAICodexTurnStateConfig{DefaultModel: openai.DefaultTestModel, ModelScopeValid: true}
	}
	if cached, ok := s.openAICodexTurnStateCache.Load().(*cachedOpenAICodexTurnState); ok && cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.config
	}
	s.settingsUpdateMu.Lock()
	defer s.settingsUpdateMu.Unlock()
	if cached, ok := s.openAICodexTurnStateCache.Load().(*cachedOpenAICodexTurnState); ok && cached != nil && time.Now().Before(cached.expiresAt) {
		return cached.config
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeyOpenAICodexTurnStateDefaultModel, SettingKeyOpenAICodexTurnStateModels, SettingKeyOpenAICodexTurnStateAutoEnabled, SettingKeyOpenAICodexTurnStateProxyID})
	// A repository read failure cannot prove that the stored scope is empty. Keep
	// both automatic and manual collection fail-closed until a later cache fill.
	config := OpenAICodexTurnStateConfig{DefaultModel: openai.DefaultTestModel}
	ttl := gatewayForwardingCacheTTL
	if err == nil {
		if models, modelsErr := NormalizeOpenAICodexTurnStateModels(values[SettingKeyOpenAICodexTurnStateModels]); modelsErr == nil {
			config.Models = models
			config.ModelScopeValid = true
		}
		if defaultModel, modelErr := NormalizeOpenAICodexTurnStateDefaultModel(values[SettingKeyOpenAICodexTurnStateDefaultModel]); modelErr == nil {
			config.DefaultModel = defaultModel
		}
		if proxyID, proxyIDErr := NormalizeOpenAICodexTurnStateProxyID(values[SettingKeyOpenAICodexTurnStateProxyID]); proxyIDErr == nil {
			config.ProxyID = proxyID
		}
		config.AutoEnabled = config.ModelScopeValid && values[SettingKeyOpenAICodexTurnStateAutoEnabled] == "true"
	} else {
		ttl = gatewayForwardingErrorTTL
	}
	s.openAICodexTurnStateCache.Store(&cachedOpenAICodexTurnState{config: config, expiresAt: time.Now().Add(ttl)})
	return config
}

// Called under settingsUpdateMu after the commit; no database work here.
func (s *SettingService) InvalidateOpenAICodexTurnStateCache() {
	if s != nil {
		s.openAICodexTurnStateCache.Store(&cachedOpenAICodexTurnState{})
	}
}

func (s *OpenAIGatewayService) applyOpenAICodexTurnState(ctx context.Context, account *Account, headers http.Header, models ...string) error {
	if s == nil || headers == nil || s.settingService == nil {
		return nil
	}
	noteSource := func(source string) {
		if trace := DebugWorkbenchTraceFromContext(ctx); trace != nil {
			trace.NoteTurnStateSource(source)
		}
	}
	model := s.codexTurnStateModel(ctx, models...)
	scopeModels := appendCodexTurnStateScopeModels(nil, models...)
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, model)
	policy := openAICodexTurnStateInjectionPolicy(ctx)
	if policy == codexTurnStateInjectionDisabled {
		headers.Del(openAICodexTurnStateHeader)
		noteSource("none")
		return nil
	}
	if native := strings.TrimSpace(headers.Get(openAICodexTurnStateHeader)); native != "" &&
		!s.codexTurnStateNativeScopeAllowed(ctx, account, model, native) {
		headers.Del(openAICodexTurnStateHeader)
	}
	if policy == codexTurnStateInjectionNativeOnly {
		if strings.TrimSpace(headers.Get(openAICodexTurnStateHeader)) != "" {
			noteSource("native")
		} else {
			noteSource("none")
		}
		return nil
	}
	cfg := s.settingService.GetOpenAICodexTurnState(ctx)
	if !codexTurnStateAutoEligible(account) {
		if strings.TrimSpace(headers.Get(openAICodexTurnStateHeader)) != "" {
			noteSource("native")
		} else {
			noteSource("none")
		}
		return nil
	}
	ctx = withCodexTurnStateModel(ctx, model)
	if !codexTurnStateScopeAllows(cfg, scopeModels...) {
		s.enforceCodexTurnStateScopeForAccount(cfg, account.ID, model)
		// The configured range gates managed collection and injection only. A
		// native client continuation remains authoritative for its own request.
		if strings.TrimSpace(headers.Get(openAICodexTurnStateHeader)) != "" {
			noteSource("native")
		} else {
			noteSource("none")
		}
		return nil
	}
	autoEnabled := cfg.AutoEnabled
	auto := ""
	if autoEnabled {
		auto = s.autoTurnStateForAccount(ctx, account, models...)
		// Check the same account in persistent storage before treating the
		// applicable pool as empty. Other accounts and other models are never
		// candidates.
		if err := s.refreshCodexTurnStateSource(ctx, account, model, auto == ""); err != nil {
			return err
		}
		current := s.settingService.GetOpenAICodexTurnState(ctx)
		cfg = current
		autoEnabled = current.AutoEnabled && codexTurnStateScopeAllows(current, scopeModels...)
		if autoEnabled {
			auto = s.autoTurnStateForAccount(ctx, account, models...)
		} else {
			return nil
		}
	}
	native := headers.Get(openAICodexTurnStateHeader)
	s.openaiTurnStateMu.Lock()
	now := time.Now()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	// Re-evaluate under the same lock as revocation/collection. The automatic
	// result obtained before acquiring this lock may already have been revoked.
	auto = ""
	if autoEnabled && !entry.reconciling && !entry.recovery.Pending && entry.recovery.allows(entry.token, now) && codexTurnStateAutoExpiry(entry.token, entry.setAt, now) > now.UnixMilli() {
		auto = entry.token
	}
	// Native continuation has priority over automatic cache injection. It need
	// not be present in this process's cache: official clients can legitimately
	// bring a state produced before a restart. Validation here is representation
	// and recovery-epoch only; account provenance is enforced separately.
	nativeAllowed := native != "" && ValidateOpenAICodexTurnState(native) == nil && entry.recovery.allows(native, now)
	candidate := ""
	candidateInScope := entry.candidate.state == "" || codexTurnStateUsageCandidateInScopeLocked(cfg, entry)
	if !candidateInScope {
		s.discardCodexTurnStateCandidateLocked(entry)
	}
	if candidateInScope && !nativeAllowed && !codexTurnStateManualVerification(ctx) && (autoEnabled || entry.candidate.manual) {
		candidate = s.codexTurnStateUsageCandidateForRequestLocked(
			ctx,
			entry,
			firstCodexTurnStateRequestModel(model, models...),
			now,
		)
	}
	if !nativeAllowed {
		headers.Del(openAICodexTurnStateHeader)
		if candidate != "" {
			headers.Set(openAICodexTurnStateHeader, candidate)
		} else if auto != "" {
			headers.Set(openAICodexTurnStateHeader, auto)
		}
	}
	source := "none"
	if nativeAllowed {
		source = "native"
	} else if candidate != "" {
		source = "candidate"
	} else if auto != "" {
		source = "automatic"
	}
	s.openaiTurnStateMu.Unlock()
	noteSource(source)
	return nil
}

func codexTurnStateDigest(value string) string {
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// Envelope parsing is deliberately separate from injection eligibility.
func parseCodexTurnState(value string) (time.Time, int, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > codexTurnStateMaxLength {
		return time.Time{}, 0, false
	}
	body := strings.TrimRight(value, "=")
	if len(value)-len(body) > 2 || body == "" {
		return time.Time{}, 0, false
	}
	for _, ch := range body {
		if !(ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return time.Time{}, 0, false
		}
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(body)
	if err != nil || len(raw) < 73 || raw[0] != 0x80 {
		return time.Time{}, 0, false
	}
	cipherBytes := len(raw) - 57
	if cipherBytes%16 != 0 {
		return time.Time{}, 0, false
	}
	seconds := binary.BigEndian.Uint64(raw[1:9])
	if seconds < 1577836800 || seconds >= 4102444800 {
		return time.Time{}, 0, false
	}
	return time.Unix(int64(seconds), 0), cipherBytes / 16, true
}

// NativeState is kept separately so disabling automatic injection restores the
// original continuation instead of replaying a cached automatic value.
type openAIWSTurnStatePolicy struct {
	Settings    *SettingService
	Gateway     *OpenAIGatewayService
	Models      []string
	NativeState string
}

func (req openAIWSAcquireRequest) withCurrentTurnState(ctx context.Context) openAIWSAcquireRequest {
	if req.TurnState.Settings == nil {
		return req
	}
	config := req.TurnState.Settings.GetOpenAICodexTurnState(ctx)
	req.Headers = cloneHeader(req.Headers)
	if req.Headers == nil {
		req.Headers = make(http.Header)
	}
	req.Headers.Del(openAICodexTurnStateHeader)
	if req.TurnState.NativeState != "" {
		req.Headers.Set(openAICodexTurnStateHeader, req.TurnState.NativeState)
	}
	req.turnStateFingerprint = ""
	req.turnStateRecoveryEpoch = ""
	req.turnStateError = nil
	req.turnStateModel = ""
	if req.TurnState.Gateway != nil {
		req.turnStateModel = req.TurnState.Gateway.codexTurnStateModel(ctx, req.TurnState.Models...)
		ctx = withCodexTurnStateModel(ctx, req.turnStateModel)
		req.turnStateError = req.TurnState.Gateway.applyOpenAICodexTurnState(ctx, req.Account, req.Headers, req.TurnState.Models...)
		req.turnStateRecoveryEpoch = req.TurnState.Gateway.codexTurnStateRecoveryEpoch(ctx, req.Account)
	}
	scopeModels := appendCodexTurnStateScopeModels(nil, req.TurnState.Models...)
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, req.turnStateModel)
	if token := req.Headers.Get(openAICodexTurnStateHeader); token != "" && config.AutoEnabled && codexTurnStateScopeAllows(config, scopeModels...) {
		req.turnStateFingerprint = codexTurnStateDigest(token + "\x00" + req.turnStateModel + "\x00" + config.Models)
	}
	return req
}
