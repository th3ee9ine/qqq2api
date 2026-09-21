package service

// This file connects the opaque turn-state collector to the OpenAI gateway.
// The collector deliberately knows nothing about accounts, HTTP transports, or
// SSE. Keeping those concerns here makes it possible to test the state machine
// independently and ensures API-key/relay routes never accidentally opt in.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

const (
	openAICodexTurnStateContextKey      = "openai_codex_turn_state_request_binding"
	openAICodexTurnStateProbeMaxDefault = int64(256 * 1024)
	openAICodexTurnStateErrorMaxDefault = int64(64 * 1024)
)

var openAICodexTurnStateProbeIdentityHeaders = [...]string{
	"Authorization",
	"ChatGPT-Account-Id",
	"User-Agent",
	"Version",
	"Originator",
	"OpenAI-Beta",
}

func copyCodexTurnStateProbeIdentity(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for _, name := range openAICodexTurnStateProbeIdentityHeaders {
		for _, value := range src.Values(name) {
			if value = strings.TrimSpace(value); value != "" {
				dst.Add(name, value)
			}
		}
	}
}

func copyCodexTurnStateHeaderValues(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

type openAICodexTurnStateRequestBinding struct {
	key      OpenAICodexTurnStateKey
	model    string
	snapshot OpenAICodexTurnStateSnapshot
	used     bool
}

type openAICodexTurnStateProbeFailure struct {
	code       string
	statusCode int
	err        error
}

func (e *openAICodexTurnStateProbeFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.err == nil {
		return e.code
	}
	return e.code + ": " + e.err.Error()
}

func (e *openAICodexTurnStateProbeFailure) Unwrap() error { return e.err }

func (s *OpenAIGatewayService) initCodexTurnStateCollector() {
	if s == nil || s.cfg == nil {
		return
	}
	cfg := s.cfg.Gateway.CodexTurnState
	if !cfg.Enabled {
		return
	}
	s.codexTurnStateCollector = NewOpenAICodexTurnStateCollector(s.codexTurnStatePolicy())
	s.codexTurnStateEnabled = true
	s.codexTurnStateInjection = cfg.InjectionEnabled
	// Store an initial value so atomic.Value.Load is always well-defined even
	// before the first probe failure.
	s.codexTurnStateLastError.Store("")
}

func (s *OpenAIGatewayService) codexTurnStatePolicy() OpenAICodexTurnStatePolicy {
	cfg := s.codexTurnStateConfig()
	return OpenAICodexTurnStatePolicy{
		Blocks:        cfg.ExpectedBlocks,
		TTL:           time.Duration(cfg.TTLSeconds) * time.Second,
		RefreshBefore: time.Duration(cfg.RefreshBeforeSeconds) * time.Second,
		ProbeCooldown: time.Duration(cfg.CooldownSeconds) * time.Second,
		MaxEntries:    cfg.MaxEntries,
		MaxTokenBytes: cfg.MaxTokenBytes,
		MaxProbeSlots: 1,
		HoldActive:    cfg.HoldActive,
	}
}

func (s *OpenAIGatewayService) codexTurnStateConfig() config.GatewayCodexTurnStateConfig {
	if s == nil || s.cfg == nil {
		return config.GatewayCodexTurnStateConfig{}
	}
	return s.cfg.Gateway.CodexTurnState
}

func codexTurnStateEligibleAccount(account *Account) bool {
	return account != nil && account.IsOpenAIOAuthLike() && account.UsesOpenAICodexProtocol()
}

func (s *OpenAIGatewayService) codexTurnStateEligible(account *Account) bool {
	return s != nil && s.codexTurnStateEnabled && s.codexTurnStateCollector != nil && codexTurnStateEligibleAccount(account)
}

func codexTurnStateModel(model string) string {
	return strings.TrimSpace(model)
}

func (s *OpenAIGatewayService) codexTurnStateKey(c *gin.Context, account *Account, model string) OpenAICodexTurnStateKey {
	var scope string
	if c != nil {
		scope, _ = boundOpenAICodexTurnStateExecutionScope(c)
		if scope == "" {
			scope = openAICodexTurnStateSeed(c)
		}
	}
	key := OpenAICodexTurnStateKey{AccountID: account.ID, Scope: scope, Model: codexTurnStateModel(model)}
	if s != nil && s.codexTurnStateCollector != nil {
		key = s.codexTurnStateCollector.BindKey(key)
	}
	return key
}

func (s *OpenAIGatewayService) bindCodexTurnStateRequest(c *gin.Context, key OpenAICodexTurnStateKey, model string, snapshot OpenAICodexTurnStateSnapshot, used bool) {
	if c == nil {
		return
	}
	c.Set(openAICodexTurnStateContextKey, openAICodexTurnStateRequestBinding{
		key: key, model: codexTurnStateModel(model), snapshot: snapshot, used: used,
	})
}

func (s *OpenAIGatewayService) codexTurnStateBinding(c *gin.Context, account *Account, model string) (openAICodexTurnStateRequestBinding, bool) {
	if c != nil {
		if raw, ok := c.Get(openAICodexTurnStateContextKey); ok {
			if binding, ok := raw.(openAICodexTurnStateRequestBinding); ok &&
				binding.key.AccountID == account.ID && strings.EqualFold(binding.model, codexTurnStateModel(model)) {
				return binding, true
			}
		}
	}
	return openAICodexTurnStateRequestBinding{key: s.codexTurnStateKey(c, account, model), model: codexTurnStateModel(model)}, false
}

// prepareCodexTurnState admits a client-owned state, then reuses or obtains a
// cached snapshot. Probing is synchronous only on a miss and is bounded by the
// configured timeout; a probe failure never prevents the original request from
// proceeding without injection.
func (s *OpenAIGatewayService) prepareCodexTurnState(ctx context.Context, c *gin.Context, account *Account, model string, incoming http.Header, allowProbe bool, probeIdentity ...http.Header) (OpenAICodexTurnStateSnapshot, bool) {
	if !s.codexTurnStateEligible(account) {
		return OpenAICodexTurnStateSnapshot{}, false
	}
	key := s.codexTurnStateKey(c, account, model)
	now := time.Now()
	reliableKey := strings.TrimSpace(key.Scope) != "" && strings.TrimSpace(key.Model) != ""
	if incoming != nil {
		if value := strings.TrimSpace(incoming.Get(openAICodexTurnStateHeader)); value != "" {
			if token, err := ValidateOpenAICodexTurnState(value, s.codexTurnStatePolicy(), now); err == nil {
				// A valid native client value is authoritative for this attempt.
				// Envelope checks do not authenticate an opaque state, so client input
				// must never become a reusable collector candidate until the selected
				// HTTPS upstream returns it on an authoritative successful response.
				s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{Token: token, Route: "client"}, false)
				return OpenAICodexTurnStateSnapshot{Token: token, Route: "client"}, true
			}
			// Never forward a malformed/expired value when a usable cached value
			// or a bounded probe can be selected below.
			incoming.Del(openAICodexTurnStateHeader)
		}
	}
	// Cache reuse and probing require the complete isolation key. A native
	// client value above can still pass through unchanged, but an unknown final
	// upstream model must never be folded into another model's cache entry.
	if !reliableKey || !s.codexTurnStateInjection {
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	if snapshot, usable := s.codexTurnStateCollector.Acquire(key, now); usable {
		s.bindCodexTurnStateRequest(c, key, model, snapshot, true)
		return snapshot, true
	}
	if !allowProbe {
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	// Generation requests deliberately detach their upstream context so billing
	// can finish after the downstream disappears. A synthetic probe is different:
	// it exists only for this client attempt and must stop with that client.
	probeCtx := ctx
	if c != nil && c.Request != nil {
		probeCtx = c.Request.Context()
	}
	if probeCtx == nil {
		probeCtx = context.Background()
	}
	if probeCtx.Err() != nil {
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	probe, started := s.codexTurnStateCollector.StartProbe(key, now)
	if !started {
		// A concurrent request may be collecting the value. Wait only for that
		// bounded flight; if it completes, the next acquire can inject it.
		_, _ = s.codexTurnStateCollector.WaitProbe(probeCtx, key)
		if probeCtx.Err() != nil {
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		if snapshot, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
			s.bindCodexTurnStateRequest(c, key, model, snapshot, true)
			return snapshot, true
		}
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	discardProbeEntry := false
	defer func() {
		if discardProbeEntry {
			s.codexTurnStateCollector.AbortProbe(probe, time.Now())
			return
		}
		s.codexTurnStateCollector.FinishProbe(probe, time.Now())
	}()
	identity := incoming
	if len(probeIdentity) > 0 && probeIdentity[0] != nil {
		identity = probeIdentity[0]
	}
	if token, failure := s.probeCodexTurnState(probeCtx, account, key.Model, identity); failure == nil {
		if err := probeCtx.Err(); err != nil {
			discardProbeEntry = true
			s.recordCodexTurnStateProbeFailure(codexTurnStateProbeContextFailure(probeCtx, err).code)
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		if s.codexTurnStateCollector.Offer(key, token, "probe", time.Now()) {
			s.recordCodexTurnStateProbeSuccess()
			if snapshot, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
				s.bindCodexTurnStateRequest(c, key, model, snapshot, true)
				return snapshot, true
			}
		}
		s.recordCodexTurnStateProbeFailure("invalid_state")
	} else {
		if probeCtx.Err() != nil {
			discardProbeEntry = true
		}
		s.recordCodexTurnStateProbeFailure(failure.code)
	}
	s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
	return OpenAICodexTurnStateSnapshot{}, false
}

func (s *OpenAIGatewayService) probeCodexTurnState(parent context.Context, account *Account, model string, identityHeaders ...http.Header) (OpenAICodexTurnStateToken, *openAICodexTurnStateProbeFailure) {
	if s == nil || s.httpUpstream == nil || !codexTurnStateEligibleAccount(account) {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "unavailable", err: errors.New("turn-state probe is unavailable")}
	}
	if parent == nil {
		parent = context.Background()
	}
	cfg := s.codexTurnStateConfig()
	timeout := time.Duration(cfg.ProbeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	probeCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	model = codexTurnStateModel(model)
	if model == "" {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "invalid_model", err: errors.New("final upstream model is unavailable")}
	}
	payload, err := json.Marshal(map[string]any{
		"model":               model,
		"instructions":        "Reply with OK.",
		"input":               []map[string]any{{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "Reply with OK."}}}},
		"parallel_tool_calls": true,
		"include":             []string{"reasoning.encrypted_content"},
		"stream":              true,
		"store":               false,
	})
	if err != nil {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: err}
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(probeCtx, HTTPUpstreamProfileOpenAI)), http.MethodPost, chatgptCodexURL, bytes.NewReader(payload))
	if err != nil {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: err}
	}
	if len(identityHeaders) > 0 {
		copyCodexTurnStateProbeIdentity(req.Header, identityHeaders[0])
	}
	if strings.TrimSpace(req.Header.Get("Authorization")) == "" {
		token, _, tokenErr := s.GetAccessToken(probeCtx, account)
		if tokenErr != nil {
			return OpenAICodexTurnStateToken{}, codexTurnStateProbeContextFailure(probeCtx, tokenErr)
		}
		authHeaders, authErr := s.buildOpenAIAuthenticationHeaders(probeCtx, account, token)
		if authErr != nil {
			return OpenAICodexTurnStateToken{}, codexTurnStateProbeContextFailure(probeCtx, authErr)
		}
		copyCodexTurnStateHeaderValues(req.Header, authHeaders)
	}
	req.Host = "chatgpt.com"
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(probeCtx, s.accountRepo, req.Header, account); err != nil {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: err}
	}
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cache-control", "no-cache")
	if strings.TrimSpace(req.Header.Get("originator")) == "" {
		req.Header.Set("originator", resolveCodexOutboundIdentityForAccount(account).originator)
	}
	if strings.TrimSpace(req.Header.Get("OpenAI-Beta")) == "" {
		applyOpenAICodexBetaFeatures(nil, account, req.Header)
	}
	enforceCodexIdentityHeadersWithAccount(req.Header, account)
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := resolveAccountProxyURL(account)
	credentialAccount, credentialErr := resolveCredentialAccount(probeCtx, s.accountRepo, account)
	if credentialErr != nil {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: credentialErr}
	}
	response, _, err := doOpenAIOAuthTransportWithCredentialAccount(
		req,
		proxyURL,
		account,
		credentialAccount,
		s.pluginManager,
		s.httpUpstream,
		s.tlsFPProfileService,
		true,
	)
	if err != nil {
		return OpenAICodexTurnStateToken{}, codexTurnStateProbeContextFailure(probeCtx, err)
	}
	if response == nil {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: errors.New("nil probe response")}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody := readCodexTurnStateProbeErrorBody(response.Body, cfg.MaxProbeResponseBytes)
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			s.handleCodexTurnStateProbeUpstreamError(parent, account, response.StatusCode, response.Header, responseBody)
		}
		code := "transport_error"
		switch {
		case response.StatusCode == http.StatusUnauthorized:
			code = "upstream_401"
		case response.StatusCode == http.StatusForbidden:
			code = "upstream_403"
		case response.StatusCode == http.StatusTooManyRequests:
			code = "upstream_429"
		case response.StatusCode >= http.StatusInternalServerError:
			code = "upstream_5xx"
		}
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: code, statusCode: response.StatusCode, err: errors.New(code)}
	}
	maxBytes := cfg.MaxProbeResponseBytes
	if maxBytes <= 0 {
		maxBytes = openAICodexTurnStateProbeMaxDefault
	}
	outcome, readErr := inspectCodexTurnStateProbeSSE(response.Body, maxBytes)
	if readErr != nil {
		code := "incomplete_stream"
		if errors.Is(readErr, context.Canceled) || errors.Is(probeCtx.Err(), context.Canceled) {
			code = "cancelled"
		} else if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			code = "probe_timeout"
		}
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: code, err: readErr}
	}
	if outcome.failureEvent != "" {
		code := classifyCodexTurnStateProbeStreamFailure(outcome.failureEvent, outcome.failureCode)
		if code == "upstream_rate_limited" {
			s.handleCodexTurnStateProbeUpstreamError(parent, account, http.StatusTooManyRequests, response.Header, nil)
		}
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: code, err: errors.New(code)}
	}
	if !outcome.completed {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "incomplete_stream", err: errors.New("probe did not receive response.completed")}
	}
	if !codexTurnStateProbeModelsMatch(model, outcome.models) {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "response_model_mismatch", err: errors.New("response_model_mismatch")}
	}
	state := strings.TrimSpace(response.Header.Get(openAICodexTurnStateHeader))
	if state == "" {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "invalid_state", err: errors.New("probe response omitted turn state")}
	}
	tokenValue, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), time.Now())
	if err != nil {
		return OpenAICodexTurnStateToken{}, &openAICodexTurnStateProbeFailure{code: "invalid_state", err: err}
	}
	if err := probeCtx.Err(); err != nil {
		return OpenAICodexTurnStateToken{}, codexTurnStateProbeContextFailure(probeCtx, err)
	}
	return tokenValue, nil
}

type openAICodexTurnStateProbeSSEOutcome struct {
	completed    bool
	failureEvent string
	failureCode  string
	models       []string
}

func codexTurnStateProbeContextFailure(ctx context.Context, err error) *openAICodexTurnStateProbeFailure {
	code := "transport_error"
	if errors.Is(err, context.Canceled) || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)) {
		code = "cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		code = "probe_timeout"
	}
	return &openAICodexTurnStateProbeFailure{code: code, err: err}
}

func readCodexTurnStateProbeErrorBody(body io.Reader, configuredLimit int64) []byte {
	if body == nil {
		return nil
	}
	limit := configuredLimit
	if limit <= 0 || limit > openAICodexTurnStateErrorMaxDefault {
		limit = openAICodexTurnStateErrorMaxDefault
	}
	data, _ := io.ReadAll(io.LimitReader(body, limit))
	return data
}

func (s *OpenAIGatewayService) handleCodexTurnStateProbeUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte) {
	if s == nil || account == nil {
		return
	}
	s.handleOpenAIAuxiliaryUpstreamError(ctx, account, statusCode, headers, responseBody)
	if statusCode != http.StatusTooManyRequests || !isOpenAIOAuthAccount(account) || account.IsShadow() {
		return
	}
	// A probe has no generation retry path. Park this credential immediately so
	// Retry-After is shared with normal scheduling instead of starting the
	// request-local same-account retry window used by generation requests.
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	disposition, resetAt := classifyOpenAIOAuth429(headers, responseBody)
	now := time.Now()
	cooldownUntil := s.openAIOAuth429CooldownUntil(stateCtx, account, headers, resetAt, now)
	reason := "turn_state_probe_429"
	if disposition != openAIOAuth429Transient {
		reason = "turn_state_probe_quota"
	}
	s.BlockAccountScheduling(account, cooldownUntil, reason)
	s.openaiOAuth429RetryStartedAt.Delete(account.ID)
}

func classifyCodexTurnStateProbeStreamFailure(eventType, code string) string {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "server_is_overloaded", "slow_down":
		return "model_capacity"
	case "rate_limit_exceeded", "insufficient_quota":
		return "upstream_rate_limited"
	}
	switch eventType {
	case "slow_down":
		return "model_capacity"
	case "response.incomplete":
		return "incomplete_stream"
	default:
		return "response_failed"
	}
}

func codexTurnStateProbeModelFamily(model string) string {
	model = strings.TrimSpace(model)
	if normalized := normalizeKnownOpenAICodexModel(model); normalized != "" {
		return strings.ToLower(normalized)
	}
	return strings.ToLower(canonicalizeOpenAIModelAliasSpelling(model))
}

func codexTurnStateProbeModelsMatch(requested string, observed []string) bool {
	want := codexTurnStateProbeModelFamily(requested)
	if want == "" || len(observed) == 0 {
		return false
	}
	for _, model := range observed {
		if got := codexTurnStateProbeModelFamily(model); got == "" || got != want {
			return false
		}
	}
	return true
}

func inspectCodexTurnStateProbeSSE(body io.Reader, maxBytes int64) (openAICodexTurnStateProbeSSEOutcome, error) {
	var outcome openAICodexTurnStateProbeSSEOutcome
	if body == nil {
		return outcome, errors.New("nil probe body")
	}
	if maxBytes <= 0 {
		maxBytes = openAICodexTurnStateProbeMaxDefault
	}
	reader := bufio.NewReader(io.LimitReader(body, maxBytes+1))
	var data bytes.Buffer
	var consumed int64
	var eventName string
	flush := func() {
		if data.Len() == 0 {
			eventName = ""
			return
		}
		payload := bytes.TrimSpace(data.Bytes())
		data.Reset()
		defer func() { eventName = "" }()
		if bytes.Equal(payload, []byte("[DONE]")) {
			return
		}
		if !json.Valid(payload) {
			return
		}
		var envelope struct {
			Type  string `json:"type"`
			Code  string `json:"code"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
			Response struct {
				Model string `json:"model"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal(payload, &envelope) != nil {
			return
		}
		typ := strings.TrimSpace(envelope.Type)
		if typ == "" {
			typ = strings.TrimSpace(eventName)
		}
		switch typ {
		case "response.created", "response.completed":
			if model := strings.TrimSpace(envelope.Response.Model); model != "" {
				outcome.models = append(outcome.models, model)
			}
			if typ == "response.completed" {
				outcome.completed = true
			}
		case "response.failed", "response.incomplete", "error", "slow_down":
			code := strings.TrimSpace(envelope.Response.Error.Code)
			if code == "" {
				code = strings.TrimSpace(envelope.Error.Code)
			}
			if code == "" {
				code = strings.TrimSpace(envelope.Code)
			}
			currentClass := classifyCodexTurnStateProbeStreamFailure(typ, code)
			priorClass := classifyCodexTurnStateProbeStreamFailure(outcome.failureEvent, outcome.failureCode)
			if outcome.failureEvent == "" || currentClass == "upstream_rate_limited" || priorClass == "response_failed" {
				outcome.failureEvent = typ
				outcome.failureCode = code
			}
		}
	}
	for {
		line, readErr := reader.ReadString('\n')
		consumed += int64(len(line))
		if consumed > maxBytes {
			return openAICodexTurnStateProbeSSEOutcome{}, errors.New("probe response exceeded bounded body")
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		} else if strings.HasPrefix(trimmed, "data:") {
			part := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(part)
		} else if trimmed == "" {
			flush()
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				flush()
				break
			}
			return openAICodexTurnStateProbeSSEOutcome{}, readErr
		}
	}
	return outcome, nil
}

// readCodexTurnStateProbeSSE is kept as the narrow test-facing shape used by
// the original collector tests. The probe itself consumes the richer outcome
// above so model and stable error-code checks cannot be skipped.
func readCodexTurnStateProbeSSE(body io.Reader, maxBytes int64) (model string, completed bool, terminal string, err error) {
	outcome, err := inspectCodexTurnStateProbeSSE(body, maxBytes)
	if len(outcome.models) > 0 {
		model = outcome.models[len(outcome.models)-1]
	}
	return model, outcome.completed, outcome.failureEvent, err
}

func (s *OpenAIGatewayService) observeCodexTurnStateResponse(c *gin.Context, account *Account, model string, upstream http.Header) {
	if !s.codexTurnStateEligible(account) || upstream == nil {
		return
	}
	if c != nil && c.Request != nil {
		select {
		case <-c.Request.Context().Done():
			return
		default:
		}
	}
	state := strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
	if state == "" {
		if binding, bound := s.codexTurnStateBinding(c, account, model); bound && binding.used {
			s.codexTurnStateCollector.ObserveMissing(binding.key, binding.snapshot, time.Now())
		}
		return
	}
	binding, bound := s.codexTurnStateBinding(c, account, model)
	if !bound {
		binding.key = s.codexTurnStateKey(c, account, model)
	}
	// A response can still be relayed to a native client without a reliable
	// execution scope, but it must never create a reusable collector entry.
	if strings.TrimSpace(binding.key.Scope) == "" || strings.TrimSpace(binding.key.Model) == "" {
		return
	}
	now := time.Now()
	if binding.used {
		s.codexTurnStateCollector.Observe(binding.key, state, binding.snapshot, now)
	}
	token, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), now)
	if err != nil {
		s.recordCodexTurnStateProbeFailure("invalid_state")
		return
	}
	if !s.codexTurnStateCollector.Offer(binding.key, token, "response", now) {
		s.recordCodexTurnStateProbeFailure("state_time_rejected")
		return
	}
	s.recordCodexTurnStateProbeSuccess()
}

func (s *OpenAIGatewayService) recordCodexTurnStateProbeSuccess() {
	if s == nil {
		return
	}
	s.codexTurnStateProbeSuccesses.Add(1)
	s.codexTurnStateLastSuccessUnix.Store(time.Now().Unix())
}

func (s *OpenAIGatewayService) recordCodexTurnStateProbeFailure(code string) {
	if s == nil {
		return
	}
	if code == "" {
		code = "other"
	}
	s.codexTurnStateProbeFailures.Add(1)
	s.codexTurnStateLastFailureUnix.Store(time.Now().Unix())
	s.codexTurnStateLastError.Store(code)
}

// CodexTurnStateReliabilitySnapshot implements the optional Ops projection.
// Only aggregate counts and allow-listed error codes leave this service.
func (s *OpenAIGatewayService) CodexTurnStateReliabilitySnapshot(ctx context.Context) OpenAICodexTurnStateReliabilitySnapshot {
	if s == nil || !s.codexTurnStateEnabled || s.codexTurnStateCollector == nil {
		return OpenAICodexTurnStateReliabilitySnapshot{Enabled: false, Status: "disabled"}
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return OpenAICodexTurnStateReliabilitySnapshot{Enabled: true, Status: "unavailable"}
		default:
		}
	}
	now := time.Now()
	metrics := s.codexTurnStateCollector.Metrics()
	statuses := s.codexTurnStateCollector.Statuses(now, metrics.Entries)
	active, ready, collecting := 0, 0, false
	cooling := false
	for _, status := range statuses {
		if status.Usable {
			active++
		}
		if status.Ready {
			ready++
		}
		collecting = collecting || status.ProbeInFlight
		cooling = cooling || (!status.NextProbeAt.IsZero() && now.Before(status.NextProbeAt))
	}
	status := "idle"
	switch {
	case collecting:
		status = "collecting"
	case active > 0:
		status = "ready"
	case cooling:
		status = "cooldown"
	case s.codexTurnStateProbeFailures.Load() > 0:
		status = "degraded"
	case len(statuses) > 0:
		status = "stale"
	}
	var lastSuccess, lastFailure *time.Time
	if unix := s.codexTurnStateLastSuccessUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0).UTC()
		lastSuccess = &t
	}
	if unix := s.codexTurnStateLastFailureUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0).UTC()
		lastFailure = &t
	}
	lastError := ""
	if raw := s.codexTurnStateLastError.Load(); raw != nil {
		lastError, _ = raw.(string)
	}
	return OpenAICodexTurnStateReliabilitySnapshot{
		Enabled:          true,
		InjectionEnabled: s.codexTurnStateInjection,
		Status:           status,
		Ready:            active > 0,
		Collecting:       collecting,
		ActiveEntries:    active,
		ReadyCandidates:  ready,
		Observations:     metrics.Observations,
		Successes:        s.codexTurnStateProbeSuccesses.Load(),
		Failures:         s.codexTurnStateProbeFailures.Load(),
		LastSuccessAt:    lastSuccess,
		LastFailureAt:    lastFailure,
		LastErrorCode:    lastError,
	}
}

// compile-time check: keep the provider contract visible at the implementation
// site so a future Ops refactor cannot silently drop the projection.
var _ OpenAICodexTurnStateReliabilityProvider = (*OpenAIGatewayService)(nil)
