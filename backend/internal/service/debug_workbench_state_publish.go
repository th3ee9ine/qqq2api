package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
)

type debugWorkbenchVerificationBudgetKey struct{ accountID, apiKeyID int64 }
type debugWorkbenchVerificationBudget struct {
	startedAt time.Time
	proxies   map[string]bool
	baselines map[string]bool
	notBefore time.Time
	country   string
}

var debugStickyResidentialUsername = regexp.MustCompile(`-region-([A-Za-z]{2})-sid-([A-Za-z0-9]+)-t-([1-9][0-9]*)$`)

// The budget is shared across models for this dedicated key/account. Changing
// the model or rerunning baseline cannot erase a 429 or buy more proxy sessions.
func (s *DebugWorkbenchService) reserveDebugWorkbenchVerification(apiKeyID int64, account, daily *Account, input DebugWorkbenchRequest) error {
	s.verificationMu.Lock()
	defer s.verificationMu.Unlock()
	now := time.Now()
	if daily.RateLimitResetAt != nil && daily.RateLimitResetAt.After(now) {
		return debugInputError(http.StatusTooManyRequests, "the selected account's upstream quota cooldown is active; do not rotate the proxy")
	}
	if s.verificationBudgets == nil {
		s.verificationBudgets = make(map[debugWorkbenchVerificationBudgetKey]*debugWorkbenchVerificationBudget)
	}
	key := debugWorkbenchVerificationBudgetKey{account.ID, apiKeyID}
	budget := s.verificationBudgets[key]
	if budget == nil || (now.Sub(budget.startedAt) >= time.Hour && !now.Before(budget.notBefore)) {
		budget = &debugWorkbenchVerificationBudget{startedAt: now, proxies: map[string]bool{}, baselines: map[string]bool{}}
		s.verificationBudgets[key] = budget
	}
	if now.Before(budget.notBefore) {
		return debugInputError(http.StatusTooManyRequests, "upstream Retry-After cooldown is active; do not rotate the proxy")
	}
	model := debugWorkbenchRequestModel(input.Body)
	if input.VerificationStage == DebugVerificationStageBaseline {
		if codexTurnStateAccountProxy(account) != codexTurnStateAccountProxy(daily) {
			return debugInputError(http.StatusConflict, "baseline must use the account's daily route")
		}
		return nil
	}
	if !budget.baselines[model] {
		return debugInputError(http.StatusConflict, "run this model's no-state baseline first")
	}
	if input.VerificationStage == DebugVerificationStageAutomatic && codexTurnStateAccountProxy(account) != codexTurnStateAccountProxy(daily) {
		return debugInputError(http.StatusConflict, "automatic acceptance must use the account's daily route")
	}
	if input.VerificationStage != DebugVerificationStageCapture {
		return nil
	}
	proxy := account.Proxy
	if proxy == nil || proxy.Host != "us.1024proxy.io" || proxy.Port != 3000 || proxy.Password == "" ||
		strings.EqualFold(proxy.Protocol, "socks5") == false || debugStickyResidentialUsername.FindStringSubmatch(proxy.Username) == nil {
		return debugInputError(http.StatusConflict, "capture requires a fixed-country 1024Proxy sticky SID route (region-US, not region-Rand)")
	}
	url := codexTurnStateAccountProxy(account)
	country := debugStickyResidentialUsername.FindStringSubmatch(proxy.Username)[1]
	if budget.country != "" && !strings.EqualFold(budget.country, country) {
		return debugInputError(http.StatusConflict, "all capture sessions must keep the same country")
	}
	if url == codexTurnStateAccountProxy(daily) {
		return debugInputError(http.StatusConflict, "capture requires a new dedicated exit, not the daily proxy")
	}
	if budget.proxies[url] {
		return debugInputError(http.StatusConflict, "this sticky proxy session has already been used for capture")
	}
	if len(budget.proxies) >= 3 {
		return debugInputError(http.StatusConflict, "the three-new-proxy-session budget is exhausted")
	}
	budget.proxies[url] = true
	budget.country = country
	return nil
}

// A baseline is complete only after the real response lifecycle and its
// dedicated-key usage row agree on account, key, requested model, and raw
// upstream model. A genuine Luna baseline is still useful evidence and may
// proceed to capture; a 429 or model-free HTTP 200 may not.
func debugWorkbenchVerifiedBaseline(e *DebugStateVerification, accountID, apiKeyID int64) bool {
	if e == nil || !e.UsageLogVerified || e.StateSent ||
		e.ActualAccountID != accountID || e.UsageLogAccountID != accountID || e.UsageLogAPIKeyID != apiKeyID ||
		e.RequestedModel == "" || e.UsageLogRequestedModel != e.RequestedModel ||
		e.ResponseCreatedModel == "" || e.ResponseCompletedModel == "" ||
		!upstreamResponseModelsEquivalent(e.ResponseCreatedModel, e.ResponseCompletedModel) {
		return false
	}
	return e.UpstreamResponseModel == e.ResponseCompletedModel
}

func (s *DebugWorkbenchService) noteDebugWorkbenchVerificationBaseline(apiKeyID, accountID int64, evidence *DebugStateVerification) {
	if !debugWorkbenchVerifiedBaseline(evidence, accountID, apiKeyID) {
		return
	}
	s.verificationMu.Lock()
	defer s.verificationMu.Unlock()
	if budget := s.verificationBudgets[debugWorkbenchVerificationBudgetKey{accountID, apiKeyID}]; budget != nil {
		budget.baselines[evidence.RequestedModel] = true
	}
}

func (s *DebugWorkbenchService) noteDebugWorkbenchVerificationLimit(apiKeyID, accountID int64, attempts []DebugUpstreamAttempt) {
	for _, attempt := range attempts {
		if attempt.Response == nil || attempt.Response.StatusCode != http.StatusTooManyRequests {
			continue
		}
		header := http.Header(attempt.Response.Headers)
		delay, ok := codexTurnStateProbeRetryAfter(header, time.Now())
		if !ok {
			delay = time.Hour
		}
		var body struct {
			Error struct {
				ResetsAt int64 `json:"resets_at"`
			} `json:"error"`
		}
		if len(attempt.Response.Body) > 0 && json.Unmarshal(attempt.Response.Body, &body) == nil && body.Error.ResetsAt > 0 {
			if resetDelay := time.Until(time.Unix(body.Error.ResetsAt, 0)); resetDelay > delay {
				delay = resetDelay
			}
		}
		s.verificationMu.Lock()
		if budget := s.verificationBudgets[debugWorkbenchVerificationBudgetKey{accountID, apiKeyID}]; budget != nil {
			if boundary := time.Now().Add(delay); boundary.After(budget.notBefore) {
				budget.notBefore = boundary
			}
		}
		s.verificationMu.Unlock()
	}
}

func debugWorkbenchVerifiedReplay(e *DebugStateVerification, accountID, apiKeyID int64) bool {
	if e == nil || !e.UsageLogVerified || !e.StateSent || !e.StateMatchesCapture || !e.UsageLogStateSent ||
		e.ActualAccountID != accountID || e.UsageLogAccountID != accountID || e.UsageLogAPIKeyID != apiKeyID ||
		e.UsageLogRequestedModel != e.RequestedModel {
		return false
	}
	expected := codexTurnStateExpectedResponseModel(e.RequestedModel)
	return codexTurnStateResponseModelsMatch(expected, e.ResponseCreatedModel) &&
		codexTurnStateResponseModelsMatch(expected, e.ResponseCompletedModel) &&
		codexTurnStateResponseModelsMatch(e.ResponseCreatedModel, e.ResponseCompletedModel) &&
		e.UpstreamResponseModel == e.ResponseCompletedModel
}

// A second fresh session uses the unchanged repository account's daily proxy.
// It stays inside the original dedicated-key concurrency lease and never retries.
func (s *DebugWorkbenchService) runDebugWorkbenchDailyReplay(ctx context.Context, ownerID int64, selected *Account, scope *debugWorkbenchVerificationScope, input DebugWorkbenchRequest, state string) (*DebugWorkbenchResult, *OpenAIForwardResult, error) {
	account := *selected
	account.Extra = maps.Clone(selected.Extra)
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra["openai_ws_force_http"] = true
	if account.ProxyID != nil && account.Proxy == nil {
		proxy, err := s.accounts.GetProxy(ctx, *account.ProxyID)
		if err != nil {
			return nil, nil, err
		}
		if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
			return nil, nil, errors.New("daily proxy unavailable")
		}
		account.Proxy = proxy
	}
	lease, err := s.sessions.Acquire(ownerID, account.ID, DebugSessionInput{Action: "new_session", model: debugWorkbenchRequestModel(input.Body), apiKeyID: scope.apiKey.ID})
	if err != nil {
		return nil, nil, err
	}
	defer lease.Finish("", false)
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(input.Body, &payload); err != nil {
		return nil, nil, err
	}
	delete(payload, "prompt_cache_key")
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	body, _, _ = debugWorkbenchSessionPromptCache(&account, input.Endpoint, body, lease.View().SessionID)
	headers := debugWorkbenchExecutableHeaders(http.Header{})
	for name, value := range input.Headers {
		headers.Set(name, value)
	}
	headers = debugWorkbenchExecutableHeaders(headers)
	for name, values := range lease.Headers() {
		headers[name] = values
	}
	body, _, err = debugWorkbenchAlignSessionMetadata(headers, body, lease.View())
	if err != nil {
		return nil, nil, err
	}
	headers.Set(openAICodexTurnStateHeader, state)
	headers.Set("Content-Type", "application/json")
	requestID := uuid.NewString()
	headers.Set("X-Client-Request-Id", requestID)
	trace := NewDebugWorkbenchTrace(headers, debugWorkbenchCredentialSecrets(account.Credentials))
	ctx = trace.Context(withOpenAICodexTurnStateInjectionPolicy(ctx, codexTurnStateInjectionNativeOnly))
	ctx = context.WithValue(ctx, ctxkey.ClientRequestID, requestID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/responses", bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header = headers
	writer := NewDebugWorkbenchResponseWriter()
	c, _ := gin.CreateTestContext(writer)
	c.Request = req
	c.Set("api_key", scope.apiKey)
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	start := time.Now()
	forward, forwardErr := s.gateway.Forward(ctx, c, &account, body)
	s.gateway.CaptureOpenAICodexTurnStateUsageEvidence(c, forward)
	usageErr := error(nil)
	if forward != nil {
		usageErr = s.recordDebugWorkbenchVerificationUsage(ctx, c, &account, scope, body, forward, start)
	}
	result := &DebugWorkbenchResult{RequestID: requestID, Endpoint: "responses", Transport: "http", DurationMS: time.Since(start).Milliseconds(), Success: forwardErr == nil && writer.Status() >= 200 && writer.Status() < 300, Inbound: trace.SnapshotRequest(req), Outbound: writer.Snapshot(trace), Attempts: trace.Attempts(), Session: lease.View()}
	result.StateVerification = debugWorkbenchStateVerification(body, result.Attempts, trace, upstreamResponseModelObserverFromContext(c), forward, state)
	if usageErr == nil {
		s.attachDebugWorkbenchUsageEvidence(ctx, requestID, account.ID, scope.apiKey.ID, result.StateVerification, forward)
	}
	if persistErr := s.noteDebugWorkbenchVerificationLimitContext(ctx, scope.apiKey.ID, account.ID, result.Attempts); persistErr != nil {
		result.Warnings = append(result.Warnings, "Turn State 验证预算或 429 冷却未能持久化；本进程仍保持阻断，本次不会通过更换出口继续请求。")
	}
	if forwardErr != nil {
		result.Error = trace.RedactText(forwardErr.Error())
	}
	return result, forward, nil
}

type CodexTurnStateAtomicRepository interface {
	UpdateCodexTurnState(context.Context, int64, string, map[string]any) (bool, error)
}

type debugWorkbenchStateReplayPublicationEvidence struct {
	result       *OpenAIForwardResult
	verification *DebugStateVerification
}

type debugWorkbenchStatePublicationEvidence struct {
	apiKeyID           int64
	state              string
	collectedAt        time.Time
	dailyRouteVerified bool
	replay             debugWorkbenchStateReplayPublicationEvidence
	dailyReplay        debugWorkbenchStateReplayPublicationEvidence
}

func validateDebugWorkbenchStateReplayPublicationEvidence(accountID, apiKeyID int64, state, requestedModel string, replay debugWorkbenchStateReplayPublicationEvidence) error {
	result, verification := replay.result, replay.verification
	if result == nil || verification == nil || accountID <= 0 || apiKeyID <= 0 ||
		strings.TrimSpace(state) == "" || strings.TrimSpace(requestedModel) == "" {
		return errors.New("state publication evidence is incomplete")
	}
	if result.CodexTurnStateResponseFailed || result.UpstreamResponseModelConflict {
		return errors.New("state replay response failed or reported conflicting models")
	}
	if !debugWorkbenchVerifiedReplay(verification, accountID, apiKeyID) {
		return errors.New("state replay scope or usage evidence is invalid")
	}
	if verification.RequestedModel != requestedModel || verification.UsageLogRequestedModel != requestedModel ||
		upstreamSentModel(result.Model, result.UpstreamModel) != requestedModel {
		return errors.New("state replay requested model does not match publication scope")
	}
	sentState := ""
	if result.UpstreamTurnState != nil {
		sentState = strings.TrimSpace(*result.UpstreamTurnState)
	}
	if sentState == "" || sentState != strings.TrimSpace(state) {
		return errors.New("state replay did not send the publication candidate")
	}

	created := strings.TrimSpace(result.CodexTurnStateResponseCreatedModel)
	completed := strings.TrimSpace(result.CodexTurnStateResponseCompletedModel)
	rawTerminal := strings.TrimSpace(result.UpstreamResponseModel)
	usageModel := strings.TrimSpace(verification.UpstreamResponseModel)
	if created == "" || completed == "" || rawTerminal == "" || usageModel == "" {
		return errors.New("state replay lifecycle or usage model evidence is missing")
	}
	if created != strings.TrimSpace(verification.ResponseCreatedModel) ||
		completed != strings.TrimSpace(verification.ResponseCompletedModel) ||
		rawTerminal != usageModel {
		return errors.New("state replay raw lifecycle and usage evidence disagree")
	}

	expected := codexTurnStateExpectedResponseModel(requestedModel)
	for _, observed := range []string{created, completed, rawTerminal, usageModel} {
		if !codexTurnStateResponseModelsMatch(expected, observed) {
			return errCodexTurnStateResponseModelMismatch
		}
	}
	if !codexTurnStateResponseModelsMatch(created, completed) ||
		!codexTurnStateResponseModelsMatch(completed, rawTerminal) {
		return errCodexTurnStateResponseModelMismatch
	}
	return nil
}

func (s *DebugWorkbenchService) publishDebugWorkbenchState(ctx context.Context, account *Account, evidence debugWorkbenchStatePublicationEvidence) error {
	state := strings.TrimSpace(evidence.state)
	collectedAt := evidence.collectedAt
	if s == nil || s.gateway == nil || account == nil || state == "" || collectedAt.IsZero() || time.Since(collectedAt) >= codexTurnStateUsageCandidateTTL || ValidateOpenAICodexTurnState(state) != nil {
		return errors.New("candidate expired or invalid")
	}
	if evidence.replay.result == nil || !evidence.dailyRouteVerified {
		return errors.New("state publication replay evidence is missing")
	}
	model := upstreamSentModel(evidence.replay.result.Model, evidence.replay.result.UpstreamModel)
	if err := validateDebugWorkbenchStateReplayPublicationEvidence(account.ID, evidence.apiKeyID, state, model, evidence.replay); err != nil {
		return err
	}
	if err := validateDebugWorkbenchStateReplayPublicationEvidence(account.ID, evidence.apiKeyID, state, model, evidence.dailyReplay); err != nil {
		return err
	}
	repo, ok := s.gateway.accountRepo.(CodexTurnStateAtomicRepository)
	if !ok {
		return errors.New("atomic state repository unavailable")
	}
	s.gateway.openaiTurnStateMu.Lock()
	defer s.gateway.openaiTurnStateMu.Unlock()
	now := time.Now()
	entry := s.gateway.codexTurnStateEntryLocked(account, now, model)
	ownerModel := entry.model
	if !entry.recovery.allows(state, now) {
		return errors.New("candidate revoked")
	}
	setAt := collectedAt.UnixMilli()
	if state == entry.token && entry.setAt > 0 {
		setAt = entry.setAt
	}
	if codexTurnStateAutoExpiry(state, setAt, now) <= now.UnixMilli() {
		return errors.New("candidate expired")
	}
	recovery := entry.recovery.clone()
	recovery.Pending = false
	updates := map[string]any{
		CodexTurnStateAutoExtraKey:               state,
		CodexTurnStateAutoSetAtExtraKey:          setAt,
		CodexTurnStateAutoVerifiedAtExtraKey:     now.UnixMilli(),
		CodexTurnStateAutoVerifiedModelExtraKey:  ownerModel,
		CodexTurnStateAutoRecoveryExtraKey:       recovery,
		CodexTurnStateAutoLastErrorExtraKey:      "",
		CodexTurnStateAutoProbeNotBeforeExtraKey: entry.probeNotBefore,
		CodexTurnStateAutoProbeAtExtraKey:        entry.probeAt,
	}
	updated, err := repo.UpdateCodexTurnState(ctx, account.ID, codexTurnStateModelExtraKey(ownerModel), updates)
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("newer state or revocation exists")
	}
	entry.token, entry.setAt, entry.verifiedAt, entry.verifiedModel = state, setAt, now.UnixMilli(), ownerModel
	pendingOwner := entry.pendingOwner
	candidateOwner := entry.candidate.pendingOwner
	entry.recovery, entry.lastError, entry.candidate = recovery, "", codexTurnStateUsageCandidate{}
	entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
	s.gateway.releaseCodexTurnStateCandidateOwnerAsync(pendingOwner)
	s.gateway.releaseCodexTurnStateCandidateOwnerAsync(candidateOwner)
	entry.probe, entry.forceProbe, entry.dirty = false, false, false
	s.gateway.rememberCodexTurnStateLocked(entry, now)
	return nil
}
