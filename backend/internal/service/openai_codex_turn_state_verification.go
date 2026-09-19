package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
)

type codexTurnStatePendingObservationContextKey struct{}

type codexTurnStateUsageVerificationAPIKeyContextKey struct{}

type codexTurnStateInjectionPolicyContextKey struct{}

type codexTurnStateManualVerificationContextKey struct{}

func codexTurnStateManualVerification(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(codexTurnStateManualVerificationContextKey{}).(bool)
	return value
}

type codexTurnStateInjectionPolicy uint8

const (
	codexTurnStateInjectionDefault codexTurnStateInjectionPolicy = iota
	codexTurnStateInjectionDisabled
	codexTurnStateInjectionNativeOnly
)

const (
	codexTurnStateUsageCandidateTTL         = 10 * time.Minute
	codexTurnStateUsageReservationTTL       = 2 * time.Minute
	codexTurnStateUsageVerificationEndpoint = "/v1/responses"
)

// codexTurnStateUsageCandidate is deliberately memory-only. A maintenance
// probe may stage an opaque candidate, but only a newly inserted usage_logs row
// from a formal /v1/responses request can promote it to entry.token.
type codexTurnStateUsageCandidate struct {
	state                 string
	verifiedModel         string
	collectedAt           time.Time
	recoveryGeneration    int64
	manual                bool
	scopeModels           []string
	pendingOwner          codexTurnStateProbeCandidatePendingOwner
	apiKeyID              int64
	requestID             string
	requestedModel        string
	expectedResponseModel string
	reservedAt            time.Time
}

type codexTurnStateUsageAttemptEvidence struct {
	requestID             string
	apiKeyID              int64
	accountID             int64
	requestedModel        string
	upstreamEndpoint      string
	sentState             string
	upstreamResponseModel string
	createdModel          string
	completedModel        string
	responseFailed        bool
	responseConflict      bool
}

type codexTurnStatePendingObservation struct {
	account     *Account
	model       string
	scopeModels []string
	state       string
}

var (
	errCodexTurnStateResponseModelMismatch = errors.New("response_model_mismatch")
	errCodexTurnStateResponseModelMissing  = errors.New("response_model_missing")
	errCodexTurnStateResponseNotCompleted  = errors.New("response_not_completed")
	errCodexTurnStateResponseFailed        = errors.New("response_failed")
	errCodexTurnStateUsageLogMissing       = errors.New("usage_log_missing")
	errCodexTurnStateUsageRequestMismatch  = errors.New("usage_request_id_mismatch")
	errCodexTurnStateUsageAPIKeyMismatch   = errors.New("usage_api_key_mismatch")
	errCodexTurnStateUsageAccountMismatch  = errors.New("usage_account_mismatch")
	errCodexTurnStateUsageModelMismatch    = errors.New("usage_model_mismatch")
	errCodexTurnStateUsageStateMismatch    = errors.New("usage_state_mismatch")
	errCodexTurnStateUsageEndpointMismatch = errors.New("usage_endpoint_mismatch")
)

// WithOpenAICodexTurnStateUsageVerification marks a real HTTP /v1/responses
// request as eligible to consume one pending candidate. Other endpoints and
// background probes never receive candidates through automatic injection.
func WithOpenAICodexTurnStateUsageVerification(ctx context.Context, apiKeyID int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if apiKeyID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, codexTurnStateUsageVerificationAPIKeyContextKey{}, apiKeyID)
}

func withOpenAICodexTurnStateInjectionPolicy(ctx context.Context, policy codexTurnStateInjectionPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, codexTurnStateInjectionPolicyContextKey{}, policy)
}

func openAICodexTurnStateInjectionPolicy(ctx context.Context) codexTurnStateInjectionPolicy {
	if ctx == nil {
		return codexTurnStateInjectionDefault
	}
	policy, _ := ctx.Value(codexTurnStateInjectionPolicyContextKey{}).(codexTurnStateInjectionPolicy)
	return policy
}

// IsOpenAICodexTurnStateUsageVerificationRequest accepts only the deliberately
// small acceptance prompt used by Turn State maintenance. A normal Responses
// request must never reserve a staged candidate merely because it happens to
// use the same endpoint. Unknown top-level request options are allowed because
// the official client may add transport controls, but the semantic input must
// be exactly one user message containing exactly one input_text block.
func IsOpenAICodexTurnStateUsageVerificationRequest(body []byte, nativeState string) bool {
	if len(body) == 0 || nativeState != "" {
		return false
	}
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type inputItem struct {
		Type    string         `json:"type"`
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	}
	var request struct {
		Model string      `json:"model"`
		Input []inputItem `json:"input"`
	}
	if err := json.Unmarshal(body, &request); err != nil || strings.TrimSpace(request.Model) == "" || len(request.Input) != 1 {
		return false
	}
	item := request.Input[0]
	if item.Type != "message" || item.Role != "user" || len(item.Content) != 1 {
		return false
	}
	content := item.Content[0]
	return content.Type == "input_text" && content.Text == codexTurnStateProbePrompt
}

func codexTurnStateUsageVerificationIdentity(ctx context.Context) (apiKeyID int64, requestID string, ok bool) {
	if ctx == nil {
		return 0, "", false
	}
	apiKeyID, _ = ctx.Value(codexTurnStateUsageVerificationAPIKeyContextKey{}).(int64)
	if apiKeyID <= 0 {
		return 0, "", false
	}
	if clientRequestID, _ := ctx.Value(ctxkey.ClientRequestID).(string); strings.TrimSpace(clientRequestID) != "" {
		return apiKeyID, "client:" + strings.TrimSpace(clientRequestID), true
	}
	if localRequestID, _ := ctx.Value(ctxkey.RequestID).(string); strings.TrimSpace(localRequestID) != "" {
		return apiKeyID, "local:" + strings.TrimSpace(localRequestID), true
	}
	return 0, "", false
}

// codexTurnStateExpectedResponseModel keeps each supported request model in its
// own cache slot and checks the raw model reported by the upstream response.
// codex-auto-review is a real model identity: its response must also report
// codex-auto-review or a same-family variant, never gpt-6-astra.
func codexTurnStateExpectedResponseModel(model string) string {
	model = strings.TrimSpace(model)
	if isCodexAutoReviewFamilyModel(model) {
		return "codex-auto-review"
	}
	if isOpenAIGPT6AstraModel(model) {
		return "gpt-6-astra"
	}
	return model
}

func codexTurnStateResponseModelsMatch(expected, actual string) bool {
	return upstreamResponseModelsEquivalent(expected, actual)
}

func validateCodexTurnStateResponseEvidence(observer *upstreamResponseModelObserver, expected string) error {
	if observer == nil {
		return errCodexTurnStateResponseModelMissing
	}
	created, completed, failed := observer.CodexTurnStateEvidence()
	if failed {
		return errCodexTurnStateResponseFailed
	}
	if created == "" || completed == "" {
		return errCodexTurnStateResponseModelMissing
	}
	if observer.Conflict() || !codexTurnStateResponseModelsMatch(expected, created) ||
		!codexTurnStateResponseModelsMatch(expected, completed) ||
		!codexTurnStateResponseModelsMatch(created, completed) {
		return errCodexTurnStateResponseModelMismatch
	}
	return nil
}

// CaptureOpenAICodexTurnStateUsageEvidence freezes the raw lifecycle evidence
// on the completed forward result before the handler submits asynchronous usage
// recording. It does not validate, normalize, or rewrite any model name.
func (s *OpenAIGatewayService) CaptureOpenAICodexTurnStateUsageEvidence(c *gin.Context, result *OpenAIForwardResult) {
	if s == nil || result == nil {
		return
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		return
	}
	result.CodexTurnStateResponseCreatedModel, result.CodexTurnStateResponseCompletedModel, result.CodexTurnStateResponseFailed = observer.CodexTurnStateEvidence()
}

// stageCodexTurnStateUsageCandidateLocked records the maintenance result without
// replacing the old verified token. Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) stageCodexTurnStateUsageCandidateLocked(entry *codexTurnStateAutoEntry, state string, generation int64, now time.Time) {
	s.stageCodexTurnStateUsageCandidateWithModeLocked(entry, state, generation, now, false)
}

// stageCodexTurnStateUsageCandidateWithModeLocked records whether an
// administrator explicitly requested the collection. Manual candidates are
// promoted after the bounded collection and same-route replay complete, while
// automatic candidates retain the dedicated usage-evidence gate. Neither path
// relaxes account/model ownership, expiry or CAS publication checks.
func (s *OpenAIGatewayService) stageCodexTurnStateUsageCandidateWithModeLocked(entry *codexTurnStateAutoEntry, state string, generation int64, now time.Time, manual bool) bool {
	var scopeModels []string
	if entry != nil {
		scopeModels = entry.scopeModels
	}
	return s.stageCodexTurnStateUsageCandidateWithScopeLocked(entry, state, generation, now, manual, scopeModels)
}

func (s *OpenAIGatewayService) stageCodexTurnStateUsageCandidateWithScopeLocked(entry *codexTurnStateAutoEntry, state string, generation int64, now time.Time, manual bool, taskScopeModels []string) bool {
	verifiedModel := ""
	if entry != nil {
		verifiedModel = entry.model
	}
	return s.stageCodexTurnStateUsageCandidateWithVerifiedModelLocked(entry, state, verifiedModel, generation, now, manual, taskScopeModels)
}

func canStageCodexTurnStateUsageCandidateLocked(entry *codexTurnStateAutoEntry, state, verifiedModel string, generation int64, now time.Time) bool {
	verifiedModel = strings.TrimSpace(verifiedModel)
	if entry == nil || entry.reconciling || strings.TrimSpace(state) == "" || generation != entry.recovery.InvalidatedAtMS ||
		verifiedModel == "" || codexTurnStateOwnerModel(verifiedModel) != entry.model || !entry.recovery.allows(state, now) {
		return false
	}
	state = strings.TrimSpace(state)
	if issued, _, ok := parseCodexTurnState(state); ok && !issued.After(now.Add(time.Minute)) {
		for _, previous := range []string{entry.token, entry.candidate.state} {
			if previousIssued, _, previousOK := parseCodexTurnState(previous); previousOK && issued.Before(previousIssued) {
				return false
			}
		}
	}
	return true
}

func (s *OpenAIGatewayService) stageCodexTurnStateUsageCandidateWithVerifiedModelLocked(entry *codexTurnStateAutoEntry, state, verifiedModel string, generation int64, now time.Time, manual bool, taskScopeModels []string) bool {
	if !canStageCodexTurnStateUsageCandidateLocked(entry, state, verifiedModel, generation, now) {
		return false
	}
	state = strings.TrimSpace(state)
	verifiedModel = strings.TrimSpace(verifiedModel)
	scopeModels := appendCodexTurnStateScopeModels(nil, taskScopeModels...)
	if len(scopeModels) == 0 {
		scopeModels = appendCodexTurnStateScopeModels(scopeModels, entry.requestModel, entry.model)
	}
	previousOwner := entry.candidate.pendingOwner
	entry.candidate = codexTurnStateUsageCandidate{
		state:                 state,
		verifiedModel:         verifiedModel,
		collectedAt:           now,
		recoveryGeneration:    generation,
		manual:                manual,
		scopeModels:           scopeModels,
		expectedResponseModel: entry.model,
	}
	s.releaseCodexTurnStateCandidateOwnerAsync(previousOwner)
	return true
}

// publishManualCodexTurnStateCandidateLocked promotes a candidate that already
// passed collection plus same-route replay. Unlike automatic collection, an
// explicit administrator request must be self-contained: an inactive account
// cannot depend on later schedulable API traffic to produce a usage-log
// acceptance request. Persistence still goes through the ordinary worker and
// repository CAS. Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) publishManualCodexTurnStateCandidateLocked(entry *codexTurnStateAutoEntry, now time.Time) bool {
	if !s.codexTurnStateUsageCandidateActiveLocked(entry, now) || !entry.candidate.manual {
		return false
	}
	queuedTask := entry.probe
	candidate := entry.candidate
	entry.candidate = codexTurnStateUsageCandidate{}
	entry.pendingOwner = candidate.pendingOwner
	candidateSetAt := candidate.collectedAt.UnixMilli()
	if candidateSetAt <= 0 || codexTurnStateAutoExpiry(candidate.state, candidateSetAt, now) <= now.UnixMilli() {
		s.releaseCodexTurnStateCandidateOwnerAsync(entry.pendingOwner)
		entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
		entry.lastError = "invalid_state"
		entry.dirty = true
		entry.manualProbe = true
		entry.retryAfter = time.Time{}
		entry.retryWakeAt = time.Time{}
		return false
	}
	previousToken := entry.token
	s.setCodexTurnStateLocked(entry, candidate.state, now)
	if entry.token == candidate.state && codexTurnStateOwnerModel(candidate.verifiedModel) == entry.model {
		entry.verifiedModel = candidate.verifiedModel
		entry.dirty = true
	}
	if entry.token == candidate.state && previousToken != candidate.state {
		entry.setAt = candidate.collectedAt.UnixMilli()
	}
	published := entry.token == candidate.state && codexTurnStateAutoExpiry(entry.token, entry.setAt, now) > now.UnixMilli()
	if !published {
		s.releaseCodexTurnStateCandidateOwnerAsync(entry.pendingOwner)
		entry.pendingOwner = codexTurnStateProbeCandidatePendingOwner{}
		entry.lastError = "invalid_state"
		entry.dirty = true
	}
	if !queuedTask {
		entry.manualProbe = true
	}
	entry.retryAfter = time.Time{}
	entry.retryWakeAt = time.Time{}
	return published
}

func firstCodexTurnStateRequestModel(fallback string, models ...string) string {
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" {
			return model
		}
	}
	return strings.TrimSpace(fallback)
}

func (s *OpenAIGatewayService) codexTurnStateUsageCandidateActiveLocked(entry *codexTurnStateAutoEntry, now time.Time) bool {
	if entry == nil || entry.reconciling || entry.candidate.state == "" {
		return false
	}
	candidate := &entry.candidate
	if candidate.collectedAt.IsZero() || now.Sub(candidate.collectedAt) >= codexTurnStateUsageCandidateTTL ||
		candidate.recoveryGeneration != entry.recovery.InvalidatedAtMS || !entry.recovery.allows(candidate.state, now) {
		s.discardCodexTurnStateCandidateLocked(entry)
		return false
	}
	return true
}

// Caller must hold openaiTurnStateMu. A candidate keeps the exact alias and
// actual-model set that authorized its collection; a later request sharing the
// same canonical owner cannot lend its own allowed alias to that candidate.
func codexTurnStateUsageCandidateInScopeLocked(cfg OpenAICodexTurnStateConfig, entry *codexTurnStateAutoEntry) bool {
	if entry == nil || entry.candidate.state == "" {
		return false
	}
	models := appendCodexTurnStateScopeModels(nil, entry.candidate.scopeModels...)
	if len(models) == 0 {
		models = appendCodexTurnStateScopeModels(models, entry.candidate.expectedResponseModel, entry.model)
	}
	return codexTurnStateScopeAllows(cfg, models...)
}

// codexTurnStateUsageCandidateForRequestLocked reserves one candidate for one
// API key and one request ID. A retry of request construction may reuse the same
// reservation; unrelated traffic continues to receive the old verified token.
// Caller must hold openaiTurnStateMu.
func (s *OpenAIGatewayService) codexTurnStateUsageCandidateForRequestLocked(ctx context.Context, entry *codexTurnStateAutoEntry, requestedModel string, now time.Time) string {
	if !s.codexTurnStateUsageCandidateActiveLocked(entry, now) {
		return ""
	}
	candidate := &entry.candidate
	apiKeyID, requestID, ok := codexTurnStateUsageVerificationIdentity(ctx)
	if !ok {
		return ""
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ""
	}
	// The request that consumes a candidate must name the model family that owns
	// that candidate. This accepts Astra date/build variants, while the special
	// codex-auto-review identity remains exact through the matcher below. A custom
	// channel alias is not enough evidence that this is the intended acceptance
	// request for the staged account/model slot.
	expectedModel := codexTurnStateExpectedResponseModel(candidate.expectedResponseModel)
	if expectedModel == "" || !codexTurnStateResponseModelsMatch(expectedModel, requestedModel) {
		return ""
	}
	if candidate.requestID == "" || now.Sub(candidate.reservedAt) >= codexTurnStateUsageReservationTTL {
		candidate.apiKeyID = apiKeyID
		candidate.requestID = requestID
		candidate.requestedModel = requestedModel
		candidate.reservedAt = now
	}
	if candidate.apiKeyID != apiKeyID || candidate.requestID != requestID || candidate.requestedModel != requestedModel {
		return ""
	}
	return candidate.state
}

func codexTurnStateUsageAttemptEvidenceFrom(input *OpenAIRecordUsageInput, usageLog *UsageLog) codexTurnStateUsageAttemptEvidence {
	evidence := codexTurnStateUsageAttemptEvidence{}
	if usageLog != nil {
		evidence.requestID = strings.TrimSpace(usageLog.RequestID)
		evidence.apiKeyID = usageLog.APIKeyID
		evidence.accountID = usageLog.AccountID
		evidence.requestedModel = strings.TrimSpace(usageLog.RequestedModel)
		evidence.upstreamEndpoint = optionalStringValue(usageLog.UpstreamEndpoint)
		evidence.sentState = optionalStringValue(usageLog.UpstreamTurnState)
		evidence.upstreamResponseModel = optionalStringValue(usageLog.UpstreamResponseModel)
	}
	if input != nil && input.Result != nil {
		evidence.createdModel = strings.TrimSpace(input.Result.CodexTurnStateResponseCreatedModel)
		evidence.completedModel = strings.TrimSpace(input.Result.CodexTurnStateResponseCompletedModel)
		evidence.responseFailed = input.Result.CodexTurnStateResponseFailed
		evidence.responseConflict = input.Result.UpstreamResponseModelConflict
	}
	return evidence
}

func validateCodexTurnStateUsageEvidence(key codexTurnStateKey, candidate codexTurnStateUsageCandidate, evidence codexTurnStateUsageAttemptEvidence) error {
	if evidence.requestID == "" || candidate.requestID == "" || evidence.requestID != candidate.requestID {
		return errCodexTurnStateUsageRequestMismatch
	}
	if evidence.apiKeyID <= 0 || candidate.apiKeyID <= 0 || evidence.apiKeyID != candidate.apiKeyID {
		return errCodexTurnStateUsageAPIKeyMismatch
	}
	if evidence.accountID <= 0 || evidence.accountID != key.accountID {
		return errCodexTurnStateUsageAccountMismatch
	}
	if evidence.requestedModel == "" || candidate.requestedModel == "" || evidence.requestedModel != candidate.requestedModel {
		return errCodexTurnStateUsageModelMismatch
	}
	if evidence.upstreamEndpoint != codexTurnStateUsageVerificationEndpoint {
		return errCodexTurnStateUsageEndpointMismatch
	}
	if evidence.sentState == "" || evidence.sentState != candidate.state {
		return errCodexTurnStateUsageStateMismatch
	}
	if evidence.responseFailed {
		return errCodexTurnStateResponseFailed
	}
	if evidence.createdModel == "" || evidence.completedModel == "" || evidence.upstreamResponseModel == "" {
		return errCodexTurnStateResponseModelMissing
	}
	expected := codexTurnStateExpectedResponseModel(candidate.expectedResponseModel)
	if evidence.responseConflict || !codexTurnStateResponseModelsMatch(expected, evidence.createdModel) ||
		!codexTurnStateResponseModelsMatch(expected, evidence.completedModel) ||
		!codexTurnStateResponseModelsMatch(evidence.createdModel, evidence.completedModel) ||
		evidence.upstreamResponseModel != evidence.completedModel {
		return errCodexTurnStateResponseModelMismatch
	}
	return nil
}

// codexTurnStateUsageReservationMatches identifies the one staged candidate a
// usage attempt is allowed to affect. Request IDs alone are client-influenced
// and can collide, so they must never clear a candidate reserved by another API
// key, account, or requested model.
func codexTurnStateUsageReservationMatches(key codexTurnStateKey, candidate codexTurnStateUsageCandidate, evidence codexTurnStateUsageAttemptEvidence) bool {
	return candidate.state != "" && candidate.requestID != "" &&
		candidate.requestID == evidence.requestID &&
		candidate.apiKeyID > 0 && candidate.apiKeyID == evidence.apiKeyID &&
		key.accountID > 0 && key.accountID == evidence.accountID &&
		candidate.requestedModel != "" && candidate.requestedModel == evidence.requestedModel
}

func (s *OpenAIGatewayService) codexTurnStateUsageRequiresDurableInsert(input *OpenAIRecordUsageInput, usageLog *UsageLog) bool {
	if s == nil || input == nil || usageLog == nil || strings.TrimSpace(usageLog.RequestID) == "" {
		return false
	}
	evidence := codexTurnStateUsageAttemptEvidenceFrom(input, usageLog)
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	for key, entry := range s.openaiTurnStates {
		if entry != nil && codexTurnStateUsageReservationMatches(key, entry.candidate, evidence) {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) noteCodexTurnStateUsageCandidateErrorLocked(accountID int64, entry *codexTurnStateAutoEntry, err error) {
	if entry == nil || err == nil {
		return
	}
	manual := entry.candidate.manual
	owner := entry.candidate.pendingOwner
	entry.candidate = codexTurnStateUsageCandidate{}
	s.releaseCodexTurnStateCandidateOwnerAsync(owner)
	entry.lastError = err.Error()
	entry.dirty = true
	if manual && !entry.probe {
		entry.manualProbe = true
	}
	if manual {
		entry.retryAfter = time.Time{}
		entry.retryWakeAt = time.Time{}
	}
	s.startCodexTurnStateWorkerLocked(accountID, entry)
}

// confirmCodexTurnStateUsageLog runs only after UsageLogRepository.Create has
// returned. inserted=false is intentionally not accepted because the conflicting
// row's contents were not read and therefore cannot be asserted to match.
func (s *OpenAIGatewayService) confirmCodexTurnStateUsageLog(input *OpenAIRecordUsageInput, usageLog *UsageLog, inserted bool) {
	if s == nil || input == nil || usageLog == nil {
		return
	}
	evidence := codexTurnStateUsageAttemptEvidenceFrom(input, usageLog)
	if evidence.requestID == "" {
		return
	}
	cfg := s.codexTurnStateRuntimeConfig(context.Background())
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	for key, entry := range s.openaiTurnStates {
		if entry == nil || !codexTurnStateUsageReservationMatches(key, entry.candidate, evidence) {
			continue
		}
		candidate := entry.candidate
		if !codexTurnStateUsageCandidateInScopeLocked(cfg, entry) {
			s.discardCodexTurnStateCandidateLocked(entry)
			return
		}
		if !inserted {
			s.noteCodexTurnStateUsageCandidateErrorLocked(key.accountID, entry, errCodexTurnStateUsageLogMissing)
			continue
		}
		if err := validateCodexTurnStateUsageEvidence(key, candidate, evidence); err != nil {
			s.noteCodexTurnStateUsageCandidateErrorLocked(key.accountID, entry, err)
			continue
		}
		if candidate.collectedAt.IsZero() || now.Sub(candidate.collectedAt) >= codexTurnStateUsageCandidateTTL ||
			candidate.recoveryGeneration != entry.recovery.InvalidatedAtMS || !entry.recovery.allows(candidate.state, now) {
			s.noteCodexTurnStateUsageCandidateErrorLocked(key.accountID, entry, errCodexTurnStateUsageLogMissing)
			continue
		}
		entry.candidate = codexTurnStateUsageCandidate{}
		entry.pendingOwner = candidate.pendingOwner
		previousToken := entry.token
		s.setCodexTurnStateLocked(entry, candidate.state, now)
		if entry.token == candidate.state && codexTurnStateOwnerModel(evidence.completedModel) == entry.model {
			entry.verifiedModel = evidence.completedModel
			entry.dirty = true
		}
		if entry.token == candidate.state && previousToken != candidate.state {
			entry.setAt = candidate.collectedAt.UnixMilli()
		}
		if candidate.manual && !entry.probe {
			entry.manualProbe = true
		}
		if candidate.manual {
			entry.retryAfter = time.Time{}
			entry.retryWakeAt = time.Time{}
		}
		s.startCodexTurnStateWorkerLocked(key.accountID, entry)
		return
	}
}

func codexTurnStatePendingFromContext(c *gin.Context) *codexTurnStatePendingObservation {
	if c == nil {
		return nil
	}
	value, ok := c.Get("codex_turn_state_pending_observation")
	if !ok {
		return nil
	}
	pending, _ := value.(*codexTurnStatePendingObservation)
	return pending
}

func (s *OpenAIGatewayService) clearPendingCodexTurnStateObservation(c *gin.Context) {
	if c != nil {
		c.Set("codex_turn_state_pending_observation", (*codexTurnStatePendingObservation)(nil))
	}
}

func (s *OpenAIGatewayService) stagePendingCodexTurnStateObservation(c *gin.Context, account *Account, state string, requests ...*http.Request) {
	if c == nil || account == nil || strings.TrimSpace(state) == "" {
		return
	}
	model := ""
	scopeModels := []string(nil)
	if len(requests) > 0 && requests[0] != nil {
		request := requests[0]
		model, _ = request.Context().Value(codexTurnStateModelContextKey{}).(string)
		if policy, ok := request.Context().Value(codexTurnStateRequestPolicyKey{}).(codexTurnStateRequestPolicy); ok {
			scopeModels = appendCodexTurnStateScopeModels(scopeModels, policy.models...)
		}
	}
	if strings.TrimSpace(model) == "" {
		model = s.codexTurnStateModel(context.Background())
	}
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, model)
	c.Set("codex_turn_state_pending_observation", &codexTurnStatePendingObservation{
		account:     account,
		model:       strings.TrimSpace(model),
		scopeModels: scopeModels,
		state:       strings.TrimSpace(state),
	})
}

// commitPendingCodexTurnStateObservation is intentionally conservative. A
// missing event, failed event, or model mismatch leaves the previous verified
// value untouched and records no candidate for automatic injection.
func (s *OpenAIGatewayService) commitPendingCodexTurnStateObservation(c *gin.Context, force bool) {
	pending := codexTurnStatePendingFromContext(c)
	if pending == nil {
		return
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if !force {
		_, completed, failed := observer.CodexTurnStateEvidence()
		if !failed && completed == "" {
			return
		}
	}
	s.clearPendingCodexTurnStateObservation(c)
	if err := validateCodexTurnStateResponseEvidence(observer, codexTurnStateExpectedResponseModel(pending.model)); err != nil {
		ctx := context.Background()
		if c.Request != nil {
			ctx = c.Request.Context()
		}
		s.noteCodexTurnStateVerificationError(ctx, pending.account, pending.model, err, pending.scopeModels...)
		return
	}
	s.noteOpenAICodexTurnStateProvenanceForModel(c, pending.account, pending.state, pending.model)
	// This response proves only that the header came from the expected model.
	// Automatic publication remains with the maintenance workflow: it collects
	// the blob, replays it on the same route, then requires its dedicated
	// usage-evidence request. Native clients already received the header.
}

func (s *OpenAIGatewayService) noteCodexTurnStateVerificationError(ctx context.Context, account *Account, model string, err error, models ...string) {
	if s == nil || account == nil || err == nil || codexTurnStateManualVerification(ctx) || !codexTurnStateCollectionEligible(account) {
		return
	}
	cfg := s.codexTurnStateRuntimeConfig(ctx)
	scopeModels := appendCodexTurnStateScopeModels(nil, models...)
	scopeModels = appendCodexTurnStateScopeModels(scopeModels, model)
	if !cfg.AutoEnabled || !codexTurnStateScopeAllows(cfg, scopeModels...) {
		return
	}
	code := err.Error()
	if code == "" {
		return
	}
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	entry.lastError = code
	entry.dirty = true
	if !now.Before(time.UnixMilli(entry.probeNotBefore)) &&
		(entry.probeAt <= 0 || now.Sub(time.UnixMilli(entry.probeAt)) >= codexTurnStateAutoProbeInterval) {
		// This verification failure creates a new maintenance task. Its scope is
		// the current actual model, not the union of aliases seen by older tasks.
		replaceCodexTurnStateProbeModelsLocked(entry, model, scopeModels...)
		entry.forceProbe = true
		entry.probe = true
	}
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	s.openaiTurnStateMu.Unlock()
}
