package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

type openAICompatSessionResponseBinding struct {
	ResponseID string
	// ResponseModel is the final upstream model that issued ResponseID. It is
	// kept separate from Turn-State metadata because either continuation value
	// may be retired independently.
	ResponseModel string
	TurnState     string
	// Model is the final upstream model that issued TurnState. The prompt-cache
	// key intentionally remains shared across IPs, but a state must never cross
	// model boundaries within the same account/session.
	Model string
	// TurnStateGeneration fences compatibility-cache writes against account
	// runtime invalidation. It is intentionally independent from ResponseID,
	// which has its own continuation lifecycle.
	TurnStateGeneration  uint64
	ContinuationDisabled bool
	ExpiresAt            time.Time
}

func openAICompatContinuationEnabled(account *Account, model string) bool {
	if account == nil || account.Type != AccountTypeAPIKey {
		return false
	}
	return shouldAutoInjectPromptCacheKeyForCompat(model)
}

func trimAnthropicCompatResponsesInputToLatestTurn(req *apicompat.ResponsesRequest) {
	if req == nil || len(req.Input) == 0 {
		return
	}

	var items []apicompat.ResponsesInputItem
	if err := json.Unmarshal(req.Input, &items); err != nil || len(items) == 0 {
		return
	}

	start := latestAnthropicCompatResponsesInputTurnStart(items)
	trimmed := append([]apicompat.ResponsesInputItem(nil), items[start:]...)
	if len(trimmed) == len(items) {
		return
	}
	if input, err := json.Marshal(trimmed); err == nil {
		req.Input = input
	}
}

func latestAnthropicCompatResponsesInputTurnStart(items []apicompat.ResponsesInputItem) int {
	if len(items) == 0 {
		return 0
	}

	start := len(items) - 1
	last := items[start]
	switch {
	case last.Type == "function_call_output":
		for start > 0 && items[start-1].Type == "function_call_output" {
			start--
		}
	case last.Type == "message" && last.Role == "user":
		for start > 0 && items[start-1].Type == "function_call_output" {
			start--
		}
	default:
		return start
	}

	return expandAnthropicCompatResponsesInputToolCallStart(items, start)
}

func expandAnthropicCompatResponsesInputToolCallStart(items []apicompat.ResponsesInputItem, start int) int {
	if start < 0 || start >= len(items) {
		return start
	}

	needed := make(map[string]struct{})
	for i := start; i < len(items); i++ {
		if items[i].Type != "function_call_output" {
			continue
		}
		callID := strings.TrimSpace(items[i].CallID)
		if callID != "" {
			needed[callID] = struct{}{}
		}
	}
	if len(needed) == 0 {
		return start
	}

	expandedStart := start
	for i := start - 1; i >= 0 && len(needed) > 0; i-- {
		if items[i].Type != "function_call" {
			continue
		}
		callID := strings.TrimSpace(items[i].CallID)
		if _, ok := needed[callID]; !ok {
			continue
		}
		delete(needed, callID)
		expandedStart = i
	}
	return expandedStart
}

func isOpenAICompatPreviousResponseNotFound(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if statusCode != http.StatusBadRequest && statusCode != http.StatusNotFound {
		return false
	}
	check := func(s string) bool {
		lower := strings.ToLower(strings.TrimSpace(s))
		return strings.Contains(lower, "previous_response_not_found") ||
			lower == "previous_response_id is not available for this user" ||
			(strings.Contains(lower, "previous response") && strings.Contains(lower, "not found")) ||
			(strings.Contains(lower, "unsupported parameter") && strings.Contains(lower, "previous_response_id"))
	}
	if check(upstreamMsg) || check(string(upstreamBody)) {
		return true
	}
	return check(gjson.GetBytes(upstreamBody, "error.code").String()) ||
		check(gjson.GetBytes(upstreamBody, "error.message").String())
}

func isOpenAICompatPreviousResponseUnsupported(statusCode int, upstreamMsg string, upstreamBody []byte) bool {
	if statusCode != http.StatusBadRequest {
		return false
	}
	check := func(s string) bool {
		lower := strings.ToLower(strings.TrimSpace(s))
		if !strings.Contains(lower, "previous_response_id") {
			return false
		}
		return strings.Contains(lower, "unsupported parameter") ||
			strings.Contains(lower, "only supported on responses websocket") ||
			strings.Contains(lower, "not supported") ||
			strings.Contains(lower, "is not available for this user") ||
			strings.Contains(lower, "requires an openai api-key account for http requests")
	}
	if check(upstreamMsg) || check(string(upstreamBody)) {
		return true
	}
	return check(gjson.GetBytes(upstreamBody, "error.code").String()) ||
		check(gjson.GetBytes(upstreamBody, "error.message").String())
}

func openAICompatSessionResponseKey(c *gin.Context, account *Account, promptCacheKey string) string {
	key := strings.TrimSpace(promptCacheKey)
	if account == nil || key == "" {
		return ""
	}
	apiKeyID := int64(0)
	if c != nil {
		apiKeyID = getAPIKeyIDFromContext(c)
	}
	return strings.Join([]string{
		strconv.FormatInt(account.ID, 10),
		strconv.FormatInt(apiKeyID, 10),
		key,
	}, "\x00")
}

func (s *OpenAIGatewayService) invalidateOpenAICompatSessionResponsesForAccount(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	prefix := strconv.FormatInt(accountID, 10) + "\x00"
	s.openaiCompatSessionResponses.Range(func(rawKey, _ any) bool {
		key, ok := rawKey.(string)
		if !ok || !strings.HasPrefix(key, prefix) {
			return true
		}
		for {
			raw, loaded := s.openaiCompatSessionResponses.Load(key)
			if !loaded {
				return true
			}
			binding, ok := raw.(openAICompatSessionResponseBinding)
			if !ok {
				s.openaiCompatSessionResponses.Delete(key)
				return true
			}
			// DeleteAccount advances the lifecycle before this scan. Preserve a
			// binding created by a request that already joined the new generation;
			// stale writers perform the symmetric post-store check and remove their
			// own value if this Range completed before they became visible.
			if s.codexTurnStateCollector != nil && binding.TurnStateGeneration != 0 &&
				s.openAICompatTurnStateGenerationCurrent(accountID, binding.TurnStateGeneration) {
				return true
			}
			if s.openaiCompatSessionResponses.CompareAndDelete(key, binding) {
				return true
			}
		}
	})
}

// openAICompatResponseBindingModel reads response-ID provenance across both
// binding layouts. The current layout uses ResponseModel; an in-memory binding
// created by the short-lived model-aware implementation may still have stored
// that value in Model. Treating a non-empty Model as response provenance only
// for a response-ID or disabled-continuation binding preserves old unscoped
// bindings while preventing known old-model continuation state from crossing a
// model boundary.
func openAICompatResponseBindingModel(binding openAICompatSessionResponseBinding) string {
	if model := strings.TrimSpace(binding.ResponseModel); model != "" {
		return model
	}
	if strings.TrimSpace(binding.ResponseID) != "" || binding.ContinuationDisabled {
		return strings.TrimSpace(binding.Model)
	}
	return ""
}

func (s *OpenAIGatewayService) getOpenAICompatSessionResponseID(_ context.Context, c *gin.Context, account *Account, promptCacheKey string, models ...string) string {
	if s == nil {
		return ""
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return ""
	}
	for {
		raw, ok := s.openaiCompatSessionResponses.Load(key)
		if !ok {
			return ""
		}
		binding, ok := raw.(openAICompatSessionResponseBinding)
		if !ok {
			s.openaiCompatSessionResponses.Delete(key)
			return ""
		}
		if !binding.ExpiresAt.IsZero() && time.Now().After(binding.ExpiresAt) {
			if s.openaiCompatSessionResponses.CompareAndDelete(key, binding) {
				return ""
			}
			continue
		}
		if binding.ContinuationDisabled {
			return ""
		}
		if len(models) > 0 {
			requestedModel := strings.TrimSpace(models[0])
			if requestedModel == "" {
				return ""
			}
			responseModel := openAICompatResponseBindingModel(binding)
			if responseModel != "" && !openAICompatTurnStateModelMatches(responseModel, requestedModel) {
				next := binding
				next.ResponseID = ""
				next.ResponseModel = ""
				if strings.TrimSpace(next.TurnState) == "" && !next.ContinuationDisabled {
					if s.openaiCompatSessionResponses.CompareAndDelete(key, binding) {
						return ""
					}
					continue
				}
				next.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
				if s.openaiCompatSessionResponses.CompareAndSwap(key, binding, next) {
					return ""
				}
				continue
			}
		}
		if strings.TrimSpace(binding.ResponseID) == "" {
			if strings.TrimSpace(binding.TurnState) == "" && !binding.ContinuationDisabled {
				if !s.openaiCompatSessionResponses.CompareAndDelete(key, binding) {
					continue
				}
			}
			return ""
		}
		return strings.TrimSpace(binding.ResponseID)
	}
}

func (s *OpenAIGatewayService) bindOpenAICompatSessionResponseID(_ context.Context, c *gin.Context, account *Account, promptCacheKey, responseID string, models ...string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	id := strings.TrimSpace(responseID)
	if key == "" || id == "" {
		return
	}
	model := ""
	modelAware := len(models) > 0
	if modelAware {
		model = strings.TrimSpace(models[0])
		if model == "" {
			return
		}
	}
	base := openAICompatSessionResponseBinding{
		ResponseID:    id,
		ResponseModel: model,
		ExpiresAt:     time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	for {
		binding := base
		raw, loaded := s.openaiCompatSessionResponses.Load(key)
		if loaded {
			existing, ok := raw.(openAICompatSessionResponseBinding)
			if !ok {
				s.openaiCompatSessionResponses.Delete(key)
				continue
			}
			existingResponseModel := openAICompatResponseBindingModel(existing)
			if existing.ContinuationDisabled {
				// A disabled continuation is sticky only for the model that
				// triggered it when that model is known. Legacy bindings retain
				// the historical global disabled behavior.
				sameResponseModel := !modelAware || existingResponseModel == "" ||
					openAICompatTurnStateModelMatches(existingResponseModel, model)
				if sameResponseModel {
					next := existing
					next.ResponseID = ""
					next.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
					if s.openaiCompatSessionResponses.CompareAndSwap(key, existing, next) {
						return
					}
					continue
				}
				// A new model is allowed to establish its own continuation after
				// an older model disabled previous_response_id.
			}
			// Preserve a cached Turn-State only for the same known model. An
			// unscoped legacy state is intentionally not promoted to a model-aware
			// binding here; getOpenAICompatSessionTurnState remains fail-closed.
			if !modelAware || (existing.Model != "" && openAICompatTurnStateModelMatches(existing.Model, model)) {
				binding.TurnState = existing.TurnState
				binding.Model = existing.Model
				binding.TurnStateGeneration = existing.TurnStateGeneration
			}
			if !modelAware {
				binding.ResponseModel = existingResponseModel
				binding.Model = existing.Model
			}
			if !s.openaiCompatSessionResponses.CompareAndSwap(key, existing, binding) {
				continue
			}
			return
		}
		if _, loaded = s.openaiCompatSessionResponses.LoadOrStore(key, binding); loaded {
			continue
		}
		return
	}
}

func (s *OpenAIGatewayService) deleteOpenAICompatSessionResponseID(_ context.Context, c *gin.Context, account *Account, promptCacheKey string, models ...string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return
	}
	for {
		raw, ok := s.openaiCompatSessionResponses.Load(key)
		if !ok {
			return
		}
		binding, ok := raw.(openAICompatSessionResponseBinding)
		if !ok {
			s.openaiCompatSessionResponses.Delete(key)
			return
		}
		if len(models) > 0 {
			requestedModel := strings.TrimSpace(models[0])
			if requestedModel == "" {
				return
			}
			responseModel := openAICompatResponseBindingModel(binding)
			if responseModel != "" && !openAICompatTurnStateModelMatches(responseModel, requestedModel) {
				return
			}
		}
		next := binding
		next.ResponseID = ""
		next.ResponseModel = ""
		if strings.TrimSpace(next.TurnState) == "" && !next.ContinuationDisabled {
			if s.openaiCompatSessionResponses.CompareAndDelete(key, binding) {
				return
			}
			continue
		}
		next.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
		if s.openaiCompatSessionResponses.CompareAndSwap(key, binding, next) {
			return
		}
	}
}

func (s *OpenAIGatewayService) disableOpenAICompatSessionContinuation(_ context.Context, c *gin.Context, account *Account, promptCacheKey string, models ...string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return
	}
	base := openAICompatSessionResponseBinding{
		ContinuationDisabled: true,
		ExpiresAt:            time.Now().Add(s.openAIWSResponseStickyTTL()),
	}
	modelAware := len(models) > 0
	if modelAware {
		base.ResponseModel = strings.TrimSpace(models[0])
		if base.ResponseModel == "" {
			return
		}
	}
	for {
		binding := base
		raw, loaded := s.openaiCompatSessionResponses.Load(key)
		if loaded {
			existing, ok := raw.(openAICompatSessionResponseBinding)
			if !ok {
				s.openaiCompatSessionResponses.Delete(key)
				continue
			}
			existingResponseModel := openAICompatResponseBindingModel(existing)
			sameResponseModel := !modelAware || existingResponseModel == "" ||
				openAICompatTurnStateModelMatches(existingResponseModel, binding.ResponseModel)
			if sameResponseModel {
				binding.TurnState = existing.TurnState
				binding.Model = existing.Model
				binding.TurnStateGeneration = existing.TurnStateGeneration
				if !modelAware {
					binding.ResponseModel = existingResponseModel
				}
			}
			if !s.openaiCompatSessionResponses.CompareAndSwap(key, existing, binding) {
				continue
			}
			return
		}
		if _, loaded = s.openaiCompatSessionResponses.LoadOrStore(key, binding); loaded {
			continue
		}
		return
	}
}

func (s *OpenAIGatewayService) isOpenAICompatSessionContinuationDisabled(_ context.Context, c *gin.Context, account *Account, promptCacheKey string, models ...string) bool {
	if s == nil {
		return false
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return false
	}
	for {
		raw, ok := s.openaiCompatSessionResponses.Load(key)
		if !ok {
			return false
		}
		binding, ok := raw.(openAICompatSessionResponseBinding)
		if !ok {
			s.openaiCompatSessionResponses.Delete(key)
			return false
		}
		if !binding.ExpiresAt.IsZero() && time.Now().After(binding.ExpiresAt) {
			if s.openaiCompatSessionResponses.CompareAndDelete(key, binding) {
				return false
			}
			continue
		}
		if len(models) > 0 {
			requestedModel := strings.TrimSpace(models[0])
			if requestedModel == "" {
				return false
			}
			responseModel := openAICompatResponseBindingModel(binding)
			if responseModel != "" && !openAICompatTurnStateModelMatches(responseModel, requestedModel) {
				return false
			}
		}
		return binding.ContinuationDisabled
	}
}

// clearOpenAICompatSessionTurnState removes only the model-scoped Turn-State
// from a compatibility binding. Response-ID continuation is intentionally kept
// independent: a malformed/expired state must not discard an otherwise valid
// Responses continuation unless the caller explicitly disables it.
func (s *OpenAIGatewayService) clearOpenAICompatSessionTurnStateBinding(
	key string,
	shouldClear func(openAICompatSessionResponseBinding) bool,
) {
	if s == nil {
		return
	}
	if key == "" {
		return
	}
	for {
		raw, ok := s.openaiCompatSessionResponses.Load(key)
		if !ok {
			return
		}
		binding, ok := raw.(openAICompatSessionResponseBinding)
		if !ok {
			s.openaiCompatSessionResponses.Delete(key)
			return
		}
		if shouldClear != nil && !shouldClear(binding) {
			return
		}
		next := binding
		next.TurnState = ""
		next.Model = ""
		next.TurnStateGeneration = 0
		if strings.TrimSpace(next.ResponseID) == "" && !next.ContinuationDisabled {
			if s.openaiCompatSessionResponses.CompareAndDelete(key, binding) {
				return
			}
			continue
		}
		next.ExpiresAt = time.Now().Add(s.openAIWSResponseStickyTTL())
		if s.openaiCompatSessionResponses.CompareAndSwap(key, binding, next) {
			return
		}
	}
}

func (s *OpenAIGatewayService) clearOpenAICompatSessionTurnState(c *gin.Context, account *Account, promptCacheKey string) {
	s.clearOpenAICompatSessionTurnStateBinding(openAICompatSessionResponseKey(c, account, promptCacheKey), nil)
}

// invalidateOpenAICompatSessionTurnStateOnModelMismatch retires the state for
// exactly one account/API-key/prompt-cache binding after the upstream declares
// a different model. The next request will therefore enter the normal state
// acquisition path instead of replaying a state minted for the wrong model.
func (s *OpenAIGatewayService) invalidateOpenAICompatSessionTurnStateOnModelMismatch(c *gin.Context, account *Account, promptCacheKey string) {
	s.clearOpenAICompatSessionTurnState(c, account, promptCacheKey)
}

func openAICompatTurnStateModelMatches(bindingModel, requestedModel string) bool {
	bindingModel = strings.TrimSpace(bindingModel)
	requestedModel = strings.TrimSpace(requestedModel)
	if bindingModel == "" || requestedModel == "" {
		return false
	}
	return codexTurnStateModelIdentitiesMatch(bindingModel, requestedModel)
}

func (s *OpenAIGatewayService) openAICompatTurnStateGenerationCurrent(accountID int64, generation uint64) bool {
	if s == nil || s.codexTurnStateCollector == nil {
		return true
	}
	if accountID <= 0 || generation == 0 {
		return false
	}
	return s.codexTurnStateCollector.IsCurrentKey(OpenAICodexTurnStateKey{
		AccountID:  accountID,
		generation: generation,
	})
}

// openAICompatTurnStateResponseModelMismatch reports a response-level model
// conflict for the current Messages attempt. An absent model is tolerated by
// compatibility upstreams that omit it from SSE frames; a declared mismatch or
// an observer conflict is fail-closed because the returned Turn-State cannot be
// trusted for the requested model.
func openAICompatTurnStateResponseModelMismatch(c *gin.Context, requestedModel string) bool {
	if observedUpstreamResponseModelConflict(c) {
		return true
	}
	observed := strings.TrimSpace(observedUpstreamResponseModel(c))
	requestedModel = strings.TrimSpace(requestedModel)
	if observed == "" || requestedModel == "" {
		return false
	}
	return !codexTurnStateModelIdentitiesMatch(requestedModel, observed)
}

const openAICompatTurnStateTerminalContextKey = "openai_compat_turn_state_successful_terminal"

// clearOpenAICompatTurnStateTerminal starts a fresh Messages attempt. A Gin
// context can survive account failover/retry, so a completed event from an
// earlier attempt must never authorize publishing a later attempt's state.
func clearOpenAICompatTurnStateTerminal(c *gin.Context) {
	if c != nil {
		c.Set(openAICompatTurnStateTerminalContextKey, false)
	}
}

// markOpenAICompatTurnStateTerminal records whether the current Responses
// stream reached a successful terminal event. Incomplete/failed terminals may
// contain a state header too, but they are not proof that the state is reusable.
func markOpenAICompatTurnStateTerminal(c *gin.Context, eventType string) {
	if c == nil {
		return
	}
	switch strings.TrimSpace(eventType) {
	case "response.completed", "response.done":
		c.Set(openAICompatTurnStateTerminalContextKey, true)
	case "response.incomplete", "response.failed", "response.cancelled", "response.canceled", "error":
		c.Set(openAICompatTurnStateTerminalContextKey, false)
	}
}

func openAICompatTurnStateSuccessfulTerminal(c *gin.Context) bool {
	if c == nil {
		return false
	}
	raw, ok := c.Get(openAICompatTurnStateTerminalContextKey)
	if !ok {
		return false
	}
	success, _ := raw.(bool)
	return success
}

// observeOpenAICompatResponseModelPayload extends the SSE observer to HTTP
// error bodies. Some compatible upstreams return a model declaration only in a
// JSON error payload; that declaration must invalidate a cached Turn-State just
// like a mismatched streaming response does.
func observeOpenAICompatResponseModelPayload(c *gin.Context, requestedModel string, payload []byte) bool {
	if len(payload) == 0 {
		return openAICompatTurnStateResponseModelMismatch(c, requestedModel)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer != nil {
		eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
		observer.ObserveOpenAI(payload, eventType)
	}
	if strings.TrimSpace(requestedModel) != "" {
		for _, path := range []string{"response.model", "model", "error.model", "response.error.model"} {
			observed := strings.TrimSpace(gjson.GetBytes(payload, path).String())
			if observed != "" && !codexTurnStateModelIdentitiesMatch(requestedModel, observed) {
				return true
			}
		}
	}
	return openAICompatTurnStateResponseModelMismatch(c, requestedModel)
}

// stageOpenAICompatSessionTurnState temporarily exposes a cached compatibility
// state through the normal request header path. buildUpstreamRequest performs
// validation, provenance checks, and collector admission against that header;
// restoring only matching header keys afterwards keeps the client request
// context unchanged for retries and downstream bookkeeping.
func stageOpenAICompatSessionTurnState(c *gin.Context, state string) func() {
	if c == nil || c.Request == nil || c.Request.Header == nil || strings.TrimSpace(state) == "" {
		return func() {}
	}
	clearTrustedInjection := func() {
		clearOpenAICodexTurnStateTrustedInjection(c)
	}
	original := make(map[string][]string)
	nativeStatePresent := false
	for key, values := range c.Request.Header {
		if strings.EqualFold(key, openAICodexTurnStateHeader) {
			original[key] = append([]string(nil), values...)
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					nativeStatePresent = true
					break
				}
			}
		}
	}
	if nativeStatePresent {
		// The outbound guard decides whether the native/client-owned value has
		// exact provenance. The compatibility marker authorizes only its own hash,
		// so it cannot whitelist a different client value.
		return clearTrustedInjection
	}
	c.Request.Header.Set(openAICodexTurnStateHeader, strings.TrimSpace(state))
	return func() {
		defer clearTrustedInjection()
		if c == nil || c.Request == nil || c.Request.Header == nil {
			return
		}
		for key := range c.Request.Header {
			if strings.EqualFold(key, openAICodexTurnStateHeader) {
				delete(c.Request.Header, key)
			}
		}
		for key, values := range original {
			c.Request.Header[key] = append([]string(nil), values...)
		}
	}
}

func (s *OpenAIGatewayService) getOpenAICompatSessionTurnState(_ context.Context, c *gin.Context, account *Account, promptCacheKey string, models ...string) string {
	clearOpenAICodexTurnStateTrustedInjection(c)
	if s == nil {
		return ""
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	if key == "" {
		return ""
	}
	raw, ok := s.openaiCompatSessionResponses.Load(key)
	if !ok {
		return ""
	}
	binding, ok := raw.(openAICompatSessionResponseBinding)
	if !ok {
		s.openaiCompatSessionResponses.Delete(key)
		return ""
	}
	if strings.TrimSpace(binding.TurnState) == "" {
		return ""
	}
	loadedState, loadedModel, loadedGeneration := binding.TurnState, binding.Model, binding.TurnStateGeneration
	clearLoadedTurnState := func() {
		s.clearOpenAICompatSessionTurnStateBinding(key, func(current openAICompatSessionResponseBinding) bool {
			return current.TurnState == loadedState && current.Model == loadedModel &&
				current.TurnStateGeneration == loadedGeneration
		})
	}
	if !s.openAICompatTurnStateGenerationCurrent(account.ID, binding.TurnStateGeneration) {
		clearLoadedTurnState()
		return ""
	}
	now := time.Now()
	if !binding.ExpiresAt.IsZero() && now.After(binding.ExpiresAt) {
		clearLoadedTurnState()
		return ""
	}
	state := strings.TrimSpace(binding.TurnState)
	policy := s.codexTurnStatePolicy().normalized()
	if _, err := ValidateOpenAICodexTurnState(state, policy, now); err != nil {
		clearLoadedTurnState()
		return ""
	}
	// A model-aware caller must fail closed for legacy bindings that predate the
	// Model field. The variadic form keeps older internal/test callers usable, but
	// production Messages forwarding always supplies the final mapped model.
	requestedModel := ""
	modelAware := len(models) > 0
	if modelAware {
		requestedModel = strings.TrimSpace(models[0])
		if requestedModel == "" || !openAICompatTurnStateModelMatches(binding.Model, requestedModel) {
			clearLoadedTurnState()
			return ""
		}
	}
	if policy.RefreshBefore > 0 {
		token, err := ValidateOpenAICodexTurnState(state, policy, now)
		if err != nil || now.Add(policy.RefreshBefore).After(token.IssuedAt.Add(policy.TTL)) {
			clearLoadedTurnState()
			return ""
		}
	}
	trustedModel := binding.Model
	if modelAware {
		trustedModel = requestedModel
	}
	s.markOpenAICodexTurnStateTrustedInjection(c, account, trustedModel, state, binding.TurnStateGeneration)
	return state
}

func (s *OpenAIGatewayService) bindOpenAICompatSessionTurnState(ctx context.Context, c *gin.Context, account *Account, promptCacheKey, turnState string, models ...string) {
	if s == nil {
		return
	}
	key := openAICompatSessionResponseKey(c, account, promptCacheKey)
	state := strings.TrimSpace(turnState)
	if key == "" || state == "" {
		return
	}
	generation, current := s.codexTurnStateRequestGenerationCurrent(c, account.ID)
	if !current {
		return
	}
	now := time.Now()
	policy := s.codexTurnStatePolicy().normalized()
	model := ""
	modelAware := len(models) > 0
	if modelAware {
		model = strings.TrimSpace(models[0])
	}
	clearCurrentGeneration := func() {
		s.clearOpenAICompatSessionTurnStateBinding(key, func(existing openAICompatSessionResponseBinding) bool {
			if existing.TurnStateGeneration != generation {
				return false
			}
			if modelAware {
				return model != "" && existing.Model != "" && openAICompatTurnStateModelMatches(existing.Model, model)
			}
			return existing.Model == ""
		})
	}
	if _, err := ValidateOpenAICodexTurnState(state, policy, now); err != nil {
		// Do not retain an unexpected upstream value, even when an older binding
		// exists for this exact generation/model. A stale writer must not clear a
		// replacement generation that won the same prompt key concurrently.
		clearCurrentGeneration()
		return
	}
	if modelAware {
		if model == "" {
			clearCurrentGeneration()
			return
		}
	}
	if codexTurnStateEligibleAccount(account) && !s.canCommitOpenAICodexTurnState(ctx, c, account, model, state) {
		// The state may have been minted by an in-flight request whose account was
		// disabled, deleted, or replaced under the same database ID. Do not let it
		// enter the trusted compatibility map for a later request.
		return
	}
	binding := openAICompatSessionResponseBinding{
		TurnState:           state,
		Model:               model,
		TurnStateGeneration: generation,
		ExpiresAt:           now.Add(s.openAIWSResponseStickyTTL()),
	}
	for {
		if !s.openAICompatTurnStateGenerationCurrent(account.ID, generation) {
			return
		}
		next := binding
		raw, loaded := s.openaiCompatSessionResponses.Load(key)
		if loaded {
			existing, ok := raw.(openAICompatSessionResponseBinding)
			if !ok {
				s.openaiCompatSessionResponses.Delete(key)
				continue
			}
			// Turn-State metadata is preserved only for the same known model. A
			// legacy unscoped state is deliberately dropped for model-aware writes.
			sameTurnStateModel := !modelAware ||
				(existing.Model != "" && openAICompatTurnStateModelMatches(existing.Model, model))
			if sameTurnStateModel {
				// A successful response may carry a newly issued opaque state on
				// every turn. Preserve the current account/model/generation binding
				// until its 55-minute refresh window opens; otherwise repeated turns
				// slide IssuedAt forever and prevent the intended refresh lifecycle.
				existingState := strings.TrimSpace(existing.TurnState)
				if existing.TurnStateGeneration == generation && existingState != "" {
					if token, err := ValidateOpenAICodexTurnState(existingState, policy, now); err == nil &&
						!openAICodexTurnStateTokenNeedsRefresh(token, policy, now) {
						next.TurnState = existing.TurnState
						next.Model = existing.Model
						next.TurnStateGeneration = existing.TurnStateGeneration
					}
				}
			}
			// Response-ID metadata has its own model scope. Legacy response IDs
			// remain usable for compatibility, while a known mismatched model is
			// never carried into this binding.
			existingResponseModel := openAICompatResponseBindingModel(existing)
			sameResponseModel := !modelAware || existingResponseModel == "" ||
				openAICompatTurnStateModelMatches(existingResponseModel, model)
			if sameResponseModel {
				next.ResponseID = existing.ResponseID
				next.ResponseModel = existingResponseModel
				next.ContinuationDisabled = existing.ContinuationDisabled
			}
			if !s.openaiCompatSessionResponses.CompareAndSwap(key, existing, next) {
				continue
			}
		} else {
			if _, loaded = s.openaiCompatSessionResponses.LoadOrStore(key, next); loaded {
				continue
			}
		}
		if !s.openAICompatTurnStateGenerationCurrent(account.ID, generation) {
			writtenState, writtenModel, writtenGeneration := next.TurnState, next.Model, next.TurnStateGeneration
			s.clearOpenAICompatSessionTurnStateBinding(key, func(current openAICompatSessionResponseBinding) bool {
				return current.TurnState == writtenState && current.Model == writtenModel && current.TurnStateGeneration == writtenGeneration
			})
		}
		return
	}
}
