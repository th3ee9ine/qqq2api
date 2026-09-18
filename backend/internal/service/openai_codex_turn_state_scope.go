package service

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	"golang.org/x/sync/singleflight"
)

// Separate top-level JSONB keys let concurrent model workers update atomically
// through UpdateExtra without replacing another model's state.
const CodexTurnStateModelExtraPrefix = "codex_turn_state_auto_model:"

type codexTurnStateKey struct {
	accountID int64
	model     string
}
type codexTurnStateModelContextKey struct{}
type codexTurnStateRequestPolicyKey struct{}
type codexTurnStateRequestPolicy struct {
	native string
	models []string
}
type codexTurnStateSourceLoads struct{ group singleflight.Group }

type CodexTurnStateSourceRepository interface {
	GetCodexTurnStateSource(context.Context, int64) (*Account, error)
}

var errCodexTurnStateLookup = errors.New("codex turn state lookup failed")

func codexTurnStateModelExtraKey(model string) string {
	return CodexTurnStateModelExtraPrefix + base64.RawURLEncoding.EncodeToString([]byte(model))
}

func codexTurnStateModelAccount(account *Account, model string) *Account {
	if account == nil {
		return nil
	}
	copy := *account
	copy.Extra, _ = account.Extra[codexTurnStateModelExtraKey(model)].(map[string]any)
	return &copy
}

func codexTurnStateScopedInfo(account *Account, now time.Time) *CodexTurnStateAutoInfo {
	result := &CodexTurnStateAutoInfo{Due: true, Models: make(map[string]CodexTurnStateAutoInfo)}
	for key := range account.Extra {
		if !strings.HasPrefix(key, CodexTurnStateModelExtraPrefix) {
			continue
		}
		decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(key, CodexTurnStateModelExtraPrefix))
		if err != nil || len(decoded) == 0 {
			continue
		}
		model := string(decoded)
		if _, err := NormalizeOpenAICodexTurnStateDefaultModel(model); err != nil {
			continue
		}
		info := codexTurnStateAutoInfo(codexTurnStateModelAccount(account, model), now)
		result.Models[model] = info
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
		if info.InvalidatedAtMS > result.InvalidatedAtMS {
			result.InvalidatedAtMS = info.InvalidatedAtMS
		}
		if info.ExpiresAtMS > 0 && (result.ExpiresAtMS == 0 || info.ExpiresAtMS < result.ExpiresAtMS) {
			result.ExpiresAtMS = info.ExpiresAtMS
		}
	}
	return result
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
	return request, s.applyOpenAICodexTurnState(request.Context(), account, request.Header, models...)
}

func (s *OpenAIGatewayService) refreshCodexTurnStateRequest(request *http.Request, account *Account) (*http.Request, error) {
	if request == nil {
		return request, nil
	}
	policy, ok := request.Context().Value(codexTurnStateRequestPolicyKey{}).(codexTurnStateRequestPolicy)
	if !ok {
		return request, nil
	}
	native := policy.native
	// Compatibility bridges may attach their cached continuation after the
	// initial prepare step. Preserve that value across the final refresh while
	// still validating it against the current account/model scope below.
	if native == "" {
		native = request.Header.Get(openAICodexTurnStateHeader)
	}

	request = request.Clone(request.Context())
	request.Header.Del(openAICodexTurnStateHeader)
	if native != "" {
		request.Header.Set(openAICodexTurnStateHeader, native)
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
