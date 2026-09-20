package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/th3ee9ine/qqq2api/internal/pkg/openai"
)

const (
	codexTurnStateModelsMaxLen                     = 1024
	codexTurnStateMaxLength                        = 4096
	codexTurnStateProxyIDsMaxSize                  = 256
	codexTurnStateProxyURLsMaxSize                 = 256
	codexTurnStateProxyURLMaxLen                   = 2048
	codexTurnStateTTL                              = time.Hour
	OpenAICodexTurnStateDefaultAutoIntervalMinutes = 50
	OpenAICodexTurnStateMinAutoIntervalMinutes     = 1
	OpenAICodexTurnStateMaxAutoIntervalMinutes     = 60
)

func NormalizeOpenAICodexTurnStateAutoIntervalMinutes(value int) (int, error) {
	if value < OpenAICodexTurnStateMinAutoIntervalMinutes || value > OpenAICodexTurnStateMaxAutoIntervalMinutes {
		return 0, fmt.Errorf("openai_codex_turn_state_auto_interval_minutes must be between %d and %d", OpenAICodexTurnStateMinAutoIntervalMinutes, OpenAICodexTurnStateMaxAutoIntervalMinutes)
	}
	return value, nil
}

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

// NormalizeOpenAICodexTurnStateProxyIDs validates and stably de-duplicates an
// administrator-selected pool. Order is retained because the first ID is also
// mirrored to the legacy single-proxy setting for mixed-version deployments.
func NormalizeOpenAICodexTurnStateProxyIDs(ids []int64) ([]int64, error) {
	if len(ids) > codexTurnStateProxyIDsMaxSize {
		return nil, fmt.Errorf("openai_codex_turn_state_proxy_ids must contain at most %d proxy IDs", codexTurnStateProxyIDsMaxSize)
	}
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("openai_codex_turn_state_proxy_ids must contain only positive proxy IDs")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result, nil
}

func ParseOpenAICodexTurnStateProxyIDs(raw string) ([]int64, error) {
	if raw == "" {
		return []int64{}, nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("openai_codex_turn_state_proxy_ids must be a JSON array of positive proxy IDs")
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil || ids == nil {
		return nil, fmt.Errorf("openai_codex_turn_state_proxy_ids must be a JSON array of positive proxy IDs")
	}
	return NormalizeOpenAICodexTurnStateProxyIDs(ids)
}

// NormalizeOpenAICodexTurnStateProxyURLs validates and canonicalizes the
// credential-bearing, Turn-State-only proxy pool. Errors deliberately identify
// only the field/index and never include the submitted URL or credentials.
func NormalizeOpenAICodexTurnStateProxyURLs(values []string) ([]string, error) {
	if len(values) > codexTurnStateProxyURLsMaxSize {
		return nil, fmt.Errorf("openai_codex_turn_state_proxy_urls must contain at most %d URLs", codexTurnStateProxyURLsMaxSize)
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, raw := range values {
		canonical, err := normalizeOpenAICodexTurnStateProxyURL(raw)
		if err != nil {
			return nil, fmt.Errorf("openai_codex_turn_state_proxy_urls[%d] must be a socks5 URL with username, password, host, and port", index)
		}
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	return result, nil
}

func normalizeOpenAICodexTurnStateProxyURL(raw string) (string, error) {
	if raw == "" || len(raw) > codexTurnStateProxyURLMaxLen || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\x00") {
		return "", errInvalidCodexTurnStateProbeProxy
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Scheme, "socks5") || parsed.Opaque != "" || parsed.User == nil ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", errInvalidCodexTurnStateProbeProxy
	}
	username := parsed.User.Username()
	password, passwordSet := parsed.User.Password()
	if username == "" || !passwordSet || password == "" || openAICodexTurnStateProxyURLHasControl(username) || openAICodexTurnStateProxyURLHasControl(password) {
		return "", errInvalidCodexTurnStateProbeProxy
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || len(host) > 253 || strings.IndexFunc(host, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", errInvalidCodexTurnStateProbeProxy
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port <= 0 || port > 65535 {
		return "", errInvalidCodexTurnStateProbeProxy
	}
	canonical := &url.URL{
		Scheme: "socks5",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		User:   url.UserPassword(username, password),
	}
	canonicalValue := canonical.String()
	if len(canonicalValue) > codexTurnStateProxyURLMaxLen {
		return "", errInvalidCodexTurnStateProbeProxy
	}
	return canonicalValue, nil
}

func openAICodexTurnStateProxyURLHasControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func ParseOpenAICodexTurnStateProxyURLs(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	if strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("openai_codex_turn_state_proxy_urls must be a JSON array of socks5 URLs")
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil || values == nil {
		return nil, fmt.Errorf("openai_codex_turn_state_proxy_urls must be a JSON array of socks5 URLs")
	}
	return NormalizeOpenAICodexTurnStateProxyURLs(values)
}

// Only account-scoped lifecycle settings are read. Legacy global manual tokens
// remain inert. ModelScopeValid keeps malformed historical values fail-closed
// without discarding an independently valid default model or dedicated proxy.
type OpenAICodexTurnStateConfig struct {
	DefaultModel        string
	Models              string
	ModelScopeValid     bool
	AutoEnabled         bool
	AutoIntervalMinutes int
	ProxyURLs           []string
	ProxyPoolConfigured bool
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
		return OpenAICodexTurnStateConfig{DefaultModel: openai.DefaultTestModel, ModelScopeValid: true, AutoIntervalMinutes: OpenAICodexTurnStateDefaultAutoIntervalMinutes}
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
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeyOpenAICodexTurnStateDefaultModel, SettingKeyOpenAICodexTurnStateModels, SettingKeyOpenAICodexTurnStateAutoEnabled, SettingKeyOpenAICodexTurnStateAutoIntervalMinutes, SettingKeyOpenAICodexTurnStateProxyURLs})
	// A repository read failure cannot prove that the stored scope is empty. Keep
	// both automatic and manual collection fail-closed until a later cache fill.
	config := OpenAICodexTurnStateConfig{
		DefaultModel:        openai.DefaultTestModel,
		AutoIntervalMinutes: OpenAICodexTurnStateDefaultAutoIntervalMinutes,
		// A failed settings read cannot prove that the dedicated pool is empty.
		// Treat it like a configured-but-unusable pool so routing cannot widen to
		// the global inventory until a later successful cache fill.
		ProxyPoolConfigured: err != nil,
	}
	ttl := gatewayForwardingCacheTTL
	if err == nil {
		if models, modelsErr := NormalizeOpenAICodexTurnStateModels(values[SettingKeyOpenAICodexTurnStateModels]); modelsErr == nil {
			config.Models = models
			config.ModelScopeValid = true
		}
		if defaultModel, modelErr := NormalizeOpenAICodexTurnStateDefaultModel(values[SettingKeyOpenAICodexTurnStateDefaultModel]); modelErr == nil {
			config.DefaultModel = defaultModel
		}
		rawProxyURLs := values[SettingKeyOpenAICodexTurnStateProxyURLs]
		if proxyURLs, proxyURLsErr := ParseOpenAICodexTurnStateProxyURLs(rawProxyURLs); proxyURLsErr == nil && len(proxyURLs) > 0 {
			config.ProxyURLs = proxyURLs
			config.ProxyPoolConfigured = true
		} else if proxyURLsErr != nil && rawProxyURLs != "" {
			// A malformed explicit pool must not silently widen collection to every
			// managed proxy record. Keep it configured but empty so routing fails
			// closed to direct transport.
			config.ProxyPoolConfigured = true
		}
		config.AutoEnabled = config.ModelScopeValid && values[SettingKeyOpenAICodexTurnStateAutoEnabled] == "true"
		if interval, parseErr := strconv.Atoi(strings.TrimSpace(values[SettingKeyOpenAICodexTurnStateAutoIntervalMinutes])); parseErr == nil {
			if normalized, intervalErr := NormalizeOpenAICodexTurnStateAutoIntervalMinutes(interval); intervalErr == nil {
				config.AutoIntervalMinutes = normalized
			}
		}
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
	model := s.codexTurnStateModel(ctx, models...)
	scopeModels := appendCodexTurnStateScopeModels(nil, models...)
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, model)
	policy := openAICodexTurnStateInjectionPolicy(ctx)
	if policy == codexTurnStateInjectionDisabled {
		headers.Del(openAICodexTurnStateHeader)
		return nil
	}
	if native := strings.TrimSpace(headers.Get(openAICodexTurnStateHeader)); native != "" &&
		!s.codexTurnStateNativeScopeAllowed(ctx, account, model, native) {
		headers.Del(openAICodexTurnStateHeader)
	}
	if policy == codexTurnStateInjectionNativeOnly {
		return nil
	}
	cfg := s.settingService.GetOpenAICodexTurnState(ctx)
	if !codexTurnStateAutoEligible(account) {
		return nil
	}
	ctx = withCodexTurnStateModel(ctx, model)
	if !codexTurnStateScopeAllows(cfg, scopeModels...) {
		// An out-of-scope request is unambiguous and can trigger lazy cleanup of
		// stale slots. For an in-scope request, retain the account snapshot until
		// the maintenance scanner/diagnostics can resolve aliases and mappings;
		// otherwise a client alias (for example friendly-codex -> gpt-5.1) could
		// make a valid slot look unrelated when older records lack alias metadata.
		if filtered, _ := s.cleanupCodexTurnStateScope(ctx, account, cfg); filtered != nil {
			account = filtered
		}
		s.enforceCodexTurnStateScopeForAccount(cfg, account.ID, model)
		// The configured range gates managed collection and injection only. A
		// native client continuation remains authoritative for its own request.
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
			s.enforceCodexTurnStateScopeForAccount(current, account.ID, model)
			return nil
		}
	}
	native := headers.Get(openAICodexTurnStateHeader)
	s.openaiTurnStateMu.Lock()
	now := time.Now()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	s.discardCanceledCodexTurnStateCollectionTaskLocked(entry)
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
		markCodexTurnStateBackgroundFailureLocked(entry, "model_scope_changed", now)
		if entry.collectionTaskID != "" {
			s.UpdateCodexTurnStateCollectionTask(entry.collectionTaskID, CodexTurnStateCollectionTaskStagePersisting, 85, 100)
		}
		s.startCodexTurnStateWorkerLocked(account.ID, entry)
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
	s.openaiTurnStateMu.Unlock()
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
