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

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

const (
	codexTurnStateModelsMaxLen = 1024
	codexTurnStateMaxLength    = 4096
	codexTurnStateTTL          = time.Hour
	codexTurnStateMaxBlocks    = 10
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

// Called under settingsUpdateMu after omitted keys have been dropped. The
// timestamp describes replacement, never a save, enable, or scope change.
func (s *SettingService) prepareOpenAICodexTurnStateUpdates(ctx context.Context, updates map[string]string) error {
	token, exists := updates[SettingKeyOpenAICodexTurnState]
	if !exists {
		return nil
	}
	if err := ValidateOpenAICodexTurnState(token); err != nil {
		return infraerrors.BadRequest("INVALID_OPENAI_CODEX_TURN_STATE", err.Error())
	}
	token = strings.TrimSpace(token)
	updates[SettingKeyOpenAICodexTurnState] = token
	if token == "" {
		updates[SettingKeyOpenAICodexTurnStateSetAtMS] = "0"
		return nil
	}
	old, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyOpenAICodexTurnState, SettingKeyOpenAICodexTurnStateSetAtMS})
	if err != nil {
		return fmt.Errorf("read existing Codex turn-state settings: %w", err)
	}
	setAt, _ := strconv.ParseInt(old[SettingKeyOpenAICodexTurnStateSetAtMS], 10, 64)
	if token != old[SettingKeyOpenAICodexTurnState] || setAt <= 0 {
		setAt = time.Now().UnixMilli()
	}
	updates[SettingKeyOpenAICodexTurnStateSetAtMS] = strconv.FormatInt(setAt, 10)
	return nil
}

type OpenAICodexTurnStateConfig struct {
	Enabled     bool
	Token       string `json:"-"`
	Models      string
	SetAtMS     int64
	AutoEnabled bool
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
		return OpenAICodexTurnStateConfig{}
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
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{SettingKeyOpenAICodexTurnStateEnabled, SettingKeyOpenAICodexTurnState, SettingKeyOpenAICodexTurnStateModels, SettingKeyOpenAICodexTurnStateSetAtMS, SettingKeyOpenAICodexTurnStateAutoEnabled})
	config := OpenAICodexTurnStateConfig{}
	ttl := gatewayForwardingCacheTTL
	if err == nil {
		token := strings.TrimSpace(values[SettingKeyOpenAICodexTurnState])
		models, modelsErr := NormalizeOpenAICodexTurnStateModels(values[SettingKeyOpenAICodexTurnStateModels])
		if ValidateOpenAICodexTurnState(token) == nil && modelsErr == nil {
			config = OpenAICodexTurnStateConfig{Enabled: values[SettingKeyOpenAICodexTurnStateEnabled] == "true", Token: token, Models: models, AutoEnabled: values[SettingKeyOpenAICodexTurnStateAutoEnabled] == "true"}
			config.SetAtMS, _ = strconv.ParseInt(values[SettingKeyOpenAICodexTurnStateSetAtMS], 10, 64)
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

func (config OpenAICodexTurnStateConfig) resolve(account *Account, models ...string) string {
	if account == nil || account.Platform != PlatformOpenAI || !account.UsesOpenAICodexProtocol() || !config.Enabled || config.Token == "" || !codexTurnStateModelMatches(config.Models, models...) {
		return ""
	}
	return config.Token
}
func (s *OpenAIGatewayService) applyOpenAICodexTurnState(ctx context.Context, account *Account, headers http.Header, models ...string) {
	if s == nil || headers == nil || s.settingService == nil {
		return
	}
	config := s.settingService.GetOpenAICodexTurnState(ctx)
	token := config.resolve(account, models...)
	// Track the routed model even while a manual/native value is in use.
	auto := s.autoTurnStateForAccount(ctx, account, models...)
	if !s.codexTurnStateAllowed(ctx, account, headers.Get(openAICodexTurnStateHeader)) {
		headers.Del(openAICodexTurnStateHeader)
	}
	if token != "" && s.codexTurnStateAllowed(ctx, account, token) {
		headers.Set(openAICodexTurnStateHeader, token)
	} else if headers.Get(openAICodexTurnStateHeader) == "" && auto != "" {
		headers.Set(openAICodexTurnStateHeader, auto)
	}
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

type CodexTurnStateStatus struct {
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	Active     bool   `json:"active"`
	Reason     string `json:"reason"`
	Verdict    string `json:"verdict"`
	Blocks     int    `json:"blocks"`
	IssuedAt   int64  `json:"issued_at,omitempty"`
	ExpiresAt  int64  `json:"expires_at,omitempty"`
}

// Active means eligible to inject, not upstream-valid. As in gpt-load, the
// reference one-hour TTL and ten-block baseline warn but never stop injection.
func InspectCodexTurnState(token string, enabled bool, now time.Time) *CodexTurnStateStatus {
	status := &CodexTurnStateStatus{Enabled: enabled, Configured: strings.TrimSpace(token) != "", Reason: "disabled", Verdict: "unknown"}
	issued, blocks, valid := parseCodexTurnState(token)
	if valid {
		status.Blocks, status.IssuedAt, status.ExpiresAt = blocks, issued.Unix(), issued.Add(codexTurnStateTTL).Unix()
		status.Verdict = "normal"
		if blocks > codexTurnStateMaxBlocks {
			status.Verdict = "suspect"
		}
	}
	if !enabled {
		return status
	}
	if !status.Configured {
		status.Reason = "missing"
		return status
	}
	if ValidateOpenAICodexTurnState(token) != nil {
		status.Reason = "invalid"
		return status
	}
	status.Active = true
	switch {
	case !valid:
		status.Reason = "unknown"
	case issued.After(now.Add(time.Minute)):
		status.Reason = "future"
	case !now.Before(issued.Add(codexTurnStateTTL)):
		status.Reason = "expired"
	case blocks > codexTurnStateMaxBlocks:
		status.Reason = "suspect"
	default:
		status.Reason = "ready"
	}
	return status
}

// NativeState is kept separately so disabling/replacing the system override
// restores the original continuation header rather than replaying a stale
// injected token. Values never enter headers/logs as internal markers.
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
	if req.TurnState.Gateway != nil {
		req.TurnState.Gateway.applyOpenAICodexTurnState(ctx, req.Account, req.Headers, req.TurnState.Models...)
		req.turnStateRecoveryEpoch = req.TurnState.Gateway.codexTurnStateRecoveryEpoch(ctx, req.Account)
	} else if token := config.resolve(req.Account, req.TurnState.Models...); token != "" {
		req.Headers.Set(openAICodexTurnStateHeader, token)
	}
	if token := req.Headers.Get(openAICodexTurnStateHeader); token != "" && token != req.TurnState.NativeState {
		req.turnStateFingerprint = codexTurnStateDigest(token + "\x00" + config.Models)
	}
	return req
}
