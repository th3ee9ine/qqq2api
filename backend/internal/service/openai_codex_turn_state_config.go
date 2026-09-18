package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
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

// Only automatic lifecycle settings are read. Legacy manual tokens are inert.
type OpenAICodexTurnStateConfig struct {
	DefaultModel string
	Models       string
	AutoEnabled  bool
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
		return OpenAICodexTurnStateConfig{DefaultModel: openai.DefaultTestModel}
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
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeyOpenAICodexTurnStateDefaultModel, SettingKeyOpenAICodexTurnStateModels, SettingKeyOpenAICodexTurnStateAutoEnabled})
	config := OpenAICodexTurnStateConfig{DefaultModel: openai.DefaultTestModel}
	ttl := gatewayForwardingCacheTTL
	if err == nil {
		models, modelsErr := NormalizeOpenAICodexTurnStateModels(values[SettingKeyOpenAICodexTurnStateModels])
		defaultModel, modelErr := NormalizeOpenAICodexTurnStateDefaultModel(values[SettingKeyOpenAICodexTurnStateDefaultModel])
		if modelsErr == nil && modelErr == nil {
			config = OpenAICodexTurnStateConfig{DefaultModel: defaultModel, Models: models, AutoEnabled: values[SettingKeyOpenAICodexTurnStateAutoEnabled] == "true"}
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
	cfg := s.settingService.GetOpenAICodexTurnState(ctx)
	if !codexTurnStateAutoEligible(account) || !cfg.AutoEnabled {
		return nil
	}
	model := s.codexTurnStateModel(ctx, models...)
	ctx = withCodexTurnStateModel(ctx, model)
	if !codexTurnStateModelMatches(cfg.Models, models...) {
		// Model scope controls automatic injection, not permission to replay a
		// foreign/expired client state on a nonmatching model.
		s.openaiTurnStateMu.Lock()
		entry := s.codexTurnStateEntryLocked(account, time.Now(), model)
		native := headers.Get(openAICodexTurnStateHeader)
		if entry.knownTokens[codexTurnStateCanonicalDigest(native)] <= time.Now().UnixMilli() || !entry.recovery.allows(native, time.Now()) {
			headers.Del(openAICodexTurnStateHeader)
		}
		s.openaiTurnStateMu.Unlock()
		return nil
	}
	auto := s.autoTurnStateForAccount(ctx, account, models...)
	// Check the same account in persistent storage before treating the applicable
	// pool as empty. Other accounts and other models are never candidates.
	if err := s.refreshCodexTurnStateSource(ctx, account, model, auto == ""); err != nil {
		return err
	}
	current := s.settingService.GetOpenAICodexTurnState(ctx)
	if !current.AutoEnabled || !codexTurnStateModelMatches(current.Models, models...) {
		return nil
	}
	auto = s.autoTurnStateForAccount(ctx, account, models...)
	native := headers.Get(openAICodexTurnStateHeader)
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, time.Now(), model)
	// Re-evaluate under the same lock as revocation/collection. The automatic
	// result obtained before acquiring this lock may already have been revoked.
	auto = ""
	if !entry.recovery.Pending && entry.recovery.allows(entry.token, time.Now()) && codexTurnStateAutoExpiry(entry.token, entry.setAt, time.Now()) > time.Now().UnixMilli() {
		auto = entry.token
	}
	knownUntil := entry.knownTokens[codexTurnStateCanonicalDigest(native)]
	nativeAllowed := native != "" && knownUntil > time.Now().UnixMilli() && entry.recovery.allows(native, time.Now())
	if !nativeAllowed {
		headers.Del(openAICodexTurnStateHeader)
		if auto != "" {
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
	if token := req.Headers.Get(openAICodexTurnStateHeader); token != "" && config.AutoEnabled && codexTurnStateModelMatches(config.Models, req.TurnState.Models...) {
		req.turnStateFingerprint = codexTurnStateDigest(token + "\x00" + req.turnStateModel + "\x00" + config.Models)
	}
	return req
}
