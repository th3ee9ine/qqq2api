package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
	"github.com/th3ee9ine/qqq2api/internal/pkg/usagestats"
	"golang.org/x/net/http/httpguts"
)

const (
	DebugWorkbenchMaxBodyBytes     = 8 << 20
	DebugWorkbenchMaxEnvelopeBytes = 9 << 20
	debugWorkbenchTimeout          = 120 * time.Second
	debugWorkbenchUsageLookupTries = 3
	debugWorkbenchUsageLookupDelay = 150 * time.Millisecond
)

// DebugWorkbenchAccountSource keeps the execution path independent of the admin
// transport. Accounts and proxy credentials never come from the submitted JSON.
type DebugWorkbenchAccountSource interface {
	GetAccount(context.Context, int64) (*Account, error)
	GetProxy(context.Context, int64) (*Proxy, error)
}

type debugWorkbenchVerificationSource interface {
	GetGroup(context.Context, int64) (*Group, error)
	GetGroupAPIKeys(context.Context, int64, int, int) ([]APIKey, int64, error)
	ListAccounts(context.Context, int, int, string, string, string, string, int64, string, string, string) ([]Account, int64, error)
}

type DebugWorkbenchInputError struct {
	StatusCode int
	Message    string
}

func (e *DebugWorkbenchInputError) Error() string { return e.Message }
func debugInputError(status int, message string) error {
	return &DebugWorkbenchInputError{StatusCode: status, Message: message}
}

type DebugWorkbenchService struct {
	gateway             *OpenAIGatewayService
	accounts            DebugWorkbenchAccountSource
	apiKeys             *APIKeyService
	sessions            *DebugWorkbenchSessionStore
	slots               chan struct{}
	verificationMu      sync.Mutex
	verificationBudgets map[debugWorkbenchVerificationBudgetKey]*debugWorkbenchVerificationBudget
}

func NewDebugWorkbenchService(gateway *OpenAIGatewayService, accounts DebugWorkbenchAccountSource, apiKeyServices ...*APIKeyService) *DebugWorkbenchService {
	var apiKeys *APIKeyService
	if len(apiKeyServices) > 0 {
		apiKeys = apiKeyServices[0]
	}
	return &DebugWorkbenchService{gateway: gateway, accounts: accounts, apiKeys: apiKeys, sessions: NewDebugWorkbenchSessionStore(), slots: make(chan struct{}, 4)}
}

// ValidateDebugWorkbenchRequest validates the complete edited request, rather
// than reconstructing a smaller connectivity-test payload from model/prompt.
func ValidateDebugWorkbenchRequest(input DebugWorkbenchRequest) error {
	switch input.Endpoint {
	case "responses", "chat/completions", "images/generations":
	default:
		return debugInputError(http.StatusBadRequest, "unsupported debug endpoint")
	}
	if len(input.Body) > DebugWorkbenchMaxBodyBytes {
		return debugInputError(http.StatusRequestEntityTooLarge, "debug body exceeds 8 MiB")
	}
	body := bytes.TrimSpace(input.Body)
	if len(body) == 0 || body[0] != '{' || !json.Valid(body) {
		return debugInputError(http.StatusBadRequest, "body must be a valid JSON object")
	}
	if input.ProxyID != nil && *input.ProxyID < 0 {
		return debugInputError(http.StatusBadRequest, "proxy_id must be zero (direct) or a positive proxy ID")
	}
	if len(input.Headers) > 64 {
		return debugInputError(http.StatusBadRequest, "at most 64 request headers are supported")
	}
	seen := make(map[string]bool, len(input.Headers))
	total := 0
	for name, value := range input.Headers {
		lower := strings.ToLower(name)
		if len(name) > 200 || !httpguts.ValidHeaderFieldName(name) {
			return debugInputError(http.StatusBadRequest, "invalid request header name")
		}
		if seen[lower] {
			return debugInputError(http.StatusBadRequest, "duplicate request header names (case insensitive)")
		}
		seen[lower] = true
		if len(value) > 8192 || !httpguts.ValidHeaderFieldValue(value) {
			return debugInputError(http.StatusBadRequest, "invalid or oversized request header value")
		}
		total += len(name) + len(value)
	}
	if total > 64<<10 {
		return debugInputError(http.StatusBadRequest, "request headers exceed 64 KiB")
	}
	if len(input.Session.ID) > 128 {
		return debugInputError(http.StatusBadRequest, "invalid debug session ID")
	}
	switch input.Session.Action {
	case "", "new_session", "new_turn", "continue_turn", "replay_capture":
	default:
		return debugInputError(http.StatusBadRequest, "invalid debug session action")
	}
	stage := strings.TrimSpace(input.VerificationStage)
	switch stage {
	case "":
		return nil
	case DebugVerificationStageBaseline, DebugVerificationStageCapture, DebugVerificationStageReplay, DebugVerificationStageAutomatic:
	default:
		return debugInputError(http.StatusBadRequest, "invalid state verification stage")
	}
	if input.Endpoint != "responses" {
		return debugInputError(http.StatusBadRequest, "state verification requires the responses endpoint")
	}
	if input.APIKeyID <= 0 {
		return debugInputError(http.StatusConflict, "state verification requires an explicit dedicated API key ID")
	}
	for name, value := range input.Headers {
		if strings.EqualFold(name, openAICodexTurnStateHeader) && strings.TrimSpace(value) != "" {
			return debugInputError(http.StatusBadRequest, "state verification does not accept client-supplied turn state")
		}
	}
	if !IsOpenAICodexTurnStateUsageVerificationRequest(input.Body, "") {
		return debugInputError(http.StatusBadRequest, "state verification requires the exact short acceptance prompt")
	}
	var request struct {
		Stream *bool `json:"stream"`
	}
	if err := json.Unmarshal(input.Body, &request); err != nil || request.Stream == nil || !*request.Stream {
		return debugInputError(http.StatusBadRequest, "state verification requires stream=true lifecycle evidence")
	}
	action := strings.TrimSpace(input.Session.Action)
	if stage == DebugVerificationStageReplay {
		if action != "replay_capture" || strings.TrimSpace(input.Session.ID) == "" {
			return debugInputError(http.StatusConflict, "state replay requires a verified capture handle")
		}
	} else if action != "new_session" || strings.TrimSpace(input.Session.ID) != "" {
		return debugInputError(http.StatusConflict, "this state verification stage requires a fresh session")
	}
	return nil
}

func (s *DebugWorkbenchService) Run(ctx context.Context, ownerID, accountID int64, input DebugWorkbenchRequest) (*DebugWorkbenchResult, error) {
	if err := ValidateDebugWorkbenchRequest(input); err != nil {
		return nil, err
	}
	if ownerID <= 0 {
		return nil, debugInputError(http.StatusUnauthorized, "authenticated administrator required")
	}
	if accountID <= 0 {
		return nil, debugInputError(http.StatusBadRequest, "invalid account ID")
	}
	if s == nil || s.gateway == nil || s.accounts == nil || s.sessions == nil {
		return nil, debugInputError(http.StatusServiceUnavailable, "debug workbench service is not configured")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return nil, debugInputError(http.StatusTooManyRequests, "four debug requests are already running; retry after one completes")
	}
	ctx, cancel := context.WithTimeout(ctx, debugWorkbenchTimeout)
	defer cancel()
	selected, err := s.accounts.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, debugInputError(http.StatusNotFound, "account not found")
	}
	if !selected.IsOpenAI() {
		return nil, debugInputError(http.StatusBadRequest, "debug workbench requires an OpenAI account")
	}
	verificationStage := strings.TrimSpace(input.VerificationStage)
	var verificationScope *debugWorkbenchVerificationScope
	if verificationStage != "" {
		verificationScope, err = s.resolveDebugWorkbenchVerificationScope(ctx, selected, input.APIKeyID)
		if err != nil {
			return nil, err
		}
		if s.gateway.concurrencyService == nil {
			return nil, debugInputError(http.StatusServiceUnavailable, "state verification API key concurrency limiter is unavailable")
		}
		keySlot, slotErr := s.gateway.concurrencyService.AcquireAPIKeySlot(ctx, verificationScope.apiKey.ID, 1)
		if slotErr != nil {
			return nil, debugInputError(http.StatusServiceUnavailable, "state verification API key concurrency limiter failed")
		}
		if keySlot == nil || !keySlot.Acquired {
			return nil, debugInputError(http.StatusConflict, "the dedicated API key already has an active request")
		}
		if keySlot.ReleaseFunc != nil {
			defer keySlot.ReleaseFunc()
		}
	}
	// Never change the repository account or a shared service containing mutexes.
	account := *selected
	account.Extra = maps.Clone(selected.Extra)
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra["openai_ws_force_http"] = true
	account.Credentials = maps.Clone(selected.Credentials)
	if input.ProxyID != nil {
		if *input.ProxyID == 0 {
			account.ProxyID, account.Proxy = nil, nil
		} else {
			proxy, loadErr := s.accounts.GetProxy(ctx, *input.ProxyID)
			if loadErr != nil {
				return nil, loadErr
			}
			if proxy == nil {
				return nil, debugInputError(http.StatusNotFound, "proxy not found")
			}
			if !proxy.IsActive() || proxy.IsExpired(time.Now()) {
				return nil, debugInputError(http.StatusBadRequest, "selected proxy is inactive or expired")
			}
			id := proxy.ID
			account.ProxyID, account.Proxy = &id, proxy
		}
	} else if account.ProxyID != nil && account.Proxy == nil {
		proxy, loadErr := s.accounts.GetProxy(ctx, *account.ProxyID)
		if loadErr != nil {
			return nil, loadErr
		}
		if proxy == nil {
			return nil, debugInputError(http.StatusNotFound, "account proxy not found")
		}
		account.Proxy = proxy
	}
	input.Session.model = debugWorkbenchRequestModel(input.Body)
	input.Session.verificationStage = verificationStage
	input.Session.proxyURL = codexTurnStateAccountProxy(&account)
	if verificationScope != nil {
		input.Session.apiKeyID = verificationScope.apiKey.ID
		if err := s.reserveDebugWorkbenchVerificationContext(ctx, verificationScope.apiKey.ID, &account, selected, input); err != nil {
			return nil, err
		}
		ctx = context.WithValue(ctx, codexTurnStateManualVerificationContextKey{}, true)
	}
	lease, err := s.sessions.Acquire(ownerID, accountID, input.Session)
	if err != nil {
		return nil, debugWorkbenchSessionError(err)
	}
	finished := false
	defer func() {
		if !finished {
			lease.Finish("", false)
		}
	}()

	submitted := make(http.Header, len(input.Headers))
	for name, value := range input.Headers {
		submitted.Set(name, value)
	}
	secrets := debugWorkbenchCredentialSecrets(account.Credentials)
	if account.Proxy != nil {
		if account.Proxy.Password != "" {
			secrets = append(secrets, account.Proxy.Password)
		}
		if account.Proxy.Username != "" {
			secrets = append(secrets, account.Proxy.Username)
		}
	}
	trace := NewDebugWorkbenchTrace(submitted, secrets)
	ctx = trace.Context(ctx)
	requestID := uuid.NewString()
	ctx = context.WithValue(ctx, ctxkey.ClientRequestID, requestID)
	switch verificationStage {
	case DebugVerificationStageBaseline, DebugVerificationStageCapture:
		ctx = withOpenAICodexTurnStateInjectionPolicy(ctx, codexTurnStateInjectionDisabled)
	case DebugVerificationStageReplay:
		ctx = withOpenAICodexTurnStateInjectionPolicy(ctx, codexTurnStateInjectionNativeOnly)
	case DebugVerificationStageAutomatic:
		ctx = WithOpenAICodexTurnStateUsageVerification(ctx, verificationScope.apiKey.ID)
	}
	// Snapshot exactly what the editor submitted, before account and session rules.
	inbound, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/"+input.Endpoint, bytes.NewReader(input.Body))
	inbound.Header = submitted.Clone()
	result := &DebugWorkbenchResult{RequestID: requestID, Endpoint: input.Endpoint, Transport: "http", Inbound: trace.SnapshotRequest(inbound)}

	req := inbound.Clone(ctx)
	req.Header = debugWorkbenchExecutableHeaders(submitted)
	req.Header.Set("Content-Type", "application/json")
	if v := strings.TrimSpace(req.Header.Get("X-Client-Request-Id")); v == "" || strings.Contains(v, "<generated-") || strings.Contains(v, "••") {
		req.Header.Set("X-Client-Request-Id", requestID)
	}
	for name, values := range lease.Headers() {
		req.Header[name] = append([]string(nil), values...)
	}
	if verificationStage != DebugVerificationStageReplay && verificationStage != "" {
		req.Header.Del(openAICodexTurnStateHeader)
	}
	executionBody, metadataChanged, metadataErr := debugWorkbenchAlignSessionMetadata(req.Header, input.Body, lease.View())
	if metadataErr != nil {
		return nil, debugInputError(http.StatusBadRequest, metadataErr.Error())
	}
	executionBody, promptCacheKey, cacheNote := debugWorkbenchSessionPromptCache(&account, input.Endpoint, executionBody, lease.View().SessionID)
	req.Body = io.NopCloser(bytes.NewReader(executionBody))
	req.ContentLength = int64(len(executionBody))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(executionBody)), nil }
	recorder := NewDebugWorkbenchResponseWriter()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	if verificationScope != nil {
		c.Set("api_key", verificationScope.apiKey)
	} else {
		// A separate negative namespace isolates ordinary administrator debug
		// sessions from API-key traffic; it is never used for billing.
		c.Set("api_key", &APIKey{ID: -ownerID, UserID: ownerID})
	}
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	start := time.Now()
	var forwardResult *OpenAIForwardResult
	switch input.Endpoint {
	case "responses":
		forwardResult, err = s.gateway.Forward(ctx, c, &account, executionBody)
		s.gateway.CaptureOpenAICodexTurnStateUsageEvidence(c, forwardResult)
	case "chat/completions":
		_, err = s.gateway.ForwardAsChatCompletions(ctx, c, &account, executionBody, promptCacheKey, "")
	case "images/generations":
		var parsed *OpenAIImagesRequest
		parsed, err = s.gateway.ParseOpenAIImagesRequest(c, executionBody)
		if err == nil {
			_, err = s.gateway.ForwardImages(ctx, c, &account, executionBody, parsed, "")
		}
	}
	usageRecordWarning := ""
	if verificationScope != nil && forwardResult != nil {
		if usageErr := s.recordDebugWorkbenchVerificationUsage(ctx, c, &account, verificationScope, executionBody, forwardResult, start); usageErr != nil {
			usageRecordWarning = "本次专用 API Key 的 usage 记录失败；不会重试计费或把该请求作为 state 发布证据。"
		}
	}
	trace.AddSecrets(debugWorkbenchCredentialSecrets(codexAccountIdentitySource(c, &account).Credentials))
	result.Inbound = trace.SnapshotRequest(inbound)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil && !c.Writer.Written() {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		c.JSON(status, gin.H{"error": gin.H{"type": "debug_execution_error", "message": trace.RedactText(err.Error())}})
	}
	result.Success = err == nil && recorder.Status() >= 200 && recorder.Status() < 300
	result.Outbound = recorder.Snapshot(trace)
	result.Attempts = trace.Attempts()
	verificationPersistenceWarning := ""
	if verificationScope != nil {
		if persistErr := s.noteDebugWorkbenchVerificationLimitContext(ctx, verificationScope.apiKey.ID, accountID, result.Attempts); persistErr != nil {
			verificationPersistenceWarning = "Turn State 验证预算或 429 冷却未能持久化；本进程仍保持阻断，本次不会通过更换出口继续请求。"
		}
	}
	usageEvidenceWarning := ""
	if input.Endpoint == "responses" {
		result.StateVerification = debugWorkbenchStateVerification(executionBody, result.Attempts, trace, upstreamResponseModelObserverFromContext(c), forwardResult, lease.replayState())
		if verificationScope != nil && (usageRecordWarning != "" || !s.attachDebugWorkbenchUsageEvidence(ctx, requestID, accountID, verificationScope.apiKey.ID, result.StateVerification, forwardResult)) {
			usageEvidenceWarning = "未找到与本次 request_id、同账号且同请求模型匹配的持久化 usage_logs 记录；usage-log 验收保持未通过。"
		}
		if verificationStage == DebugVerificationStageBaseline && verificationScope != nil {
			if persistErr := s.noteDebugWorkbenchVerificationBaselineContext(ctx, verificationScope.apiKey.ID, accountID, result.StateVerification); persistErr != nil {
				verificationPersistenceWarning = "基线证据未能持久化到专用账号预算；本次基线不会解锁后续采集。"
			}
		}
	}
	result.Warnings = append([]string{"调试执行复用正式网关的 HTTP 请求构造与响应转换；本次固定使用 HTTP，不进行 WebSocket 握手。", "会话上下文关联请求标识与上游回合状态，不自动补写历史消息；完整历史或 previous_response_id 由 Body 显式提供。"}, trace.Warnings()...)
	if usageRecordWarning != "" {
		result.Warnings = append(result.Warnings, usageRecordWarning)
	}
	if usageEvidenceWarning != "" {
		result.Warnings = append(result.Warnings, usageEvidenceWarning)
	}
	if verificationPersistenceWarning != "" {
		result.Warnings = append(result.Warnings, verificationPersistenceWarning)
	}
	if cacheNote != "" {
		result.Warnings = append(result.Warnings, cacheNote)
	}
	if metadataChanged {
		result.Warnings = append(result.Warnings, "已将 X-Codex-Turn-Metadata 与 Body.client_metadata 中现有的当前会话、线程、回合和窗口标识对齐至托管会话；父级与子代理上下文保留，入站快照保留原值。")
	}
	if len(result.Attempts) == 0 {
		result.Warnings = append(result.Warnings, "本次没有可记录的 HTTP 客户端调用；请检查本地校验错误或账号插件传输。")
	}
	if err != nil {
		result.Error = trace.RedactText(err.Error())
	}
	if verificationStage == DebugVerificationStageReplay && result.Success && debugWorkbenchVerifiedReplay(result.StateVerification, accountID, verificationScope.apiKey.ID) {
		state := lease.replayState()
		dailyVerified := codexTurnStateAccountProxy(selected) == codexTurnStateAccountProxy(&account)
		dailyForwardResult := forwardResult
		dailyVerification := result.StateVerification
		if !dailyVerified {
			result.DailyReplay, dailyForwardResult, err = s.runDebugWorkbenchDailyReplay(ctx, ownerID, selected, verificationScope, input, state)
			dailyVerified = err == nil && result.DailyReplay != nil && result.DailyReplay.Success && debugWorkbenchVerifiedReplay(result.DailyReplay.StateVerification, accountID, verificationScope.apiKey.ID)
			if result.DailyReplay != nil {
				dailyVerification = result.DailyReplay.StateVerification
			}
		}
		result.StateVerification.DailyRouteVerified = dailyVerified
		if dailyVerified {
			publishEvidence := debugWorkbenchStatePublicationEvidence{
				apiKeyID:           verificationScope.apiKey.ID,
				state:              state,
				collectedAt:        lease.session.collectedAt,
				dailyRouteVerified: dailyVerified,
				replay: debugWorkbenchStateReplayPublicationEvidence{
					result:       forwardResult,
					verification: result.StateVerification,
				},
				dailyReplay: debugWorkbenchStateReplayPublicationEvidence{
					result:       dailyForwardResult,
					verification: dailyVerification,
				},
			}
			if publishErr := s.publishDebugWorkbenchState(ctx, selected, publishEvidence); publishErr != nil {
				result.Warnings = append(result.Warnings, "回放已通过，但 state 原子发布失败；旧有效值保持不变。")
			} else {
				result.StateVerification.StatePublished = true
			}
		} else {
			result.Warnings = append(result.Warnings, "切回日常代理后的回放未通过；候选 state 未发布，旧有效值保持不变。")
		}
	}
	acceptSessionState := result.Success
	if verificationStage != "" {
		acceptSessionState = false
		if verificationStage == DebugVerificationStageCapture && result.Success && result.StateVerification != nil && result.StateVerification.StateReceived && result.StateVerification.UsageLogVerified {
			acceptSessionState = validateCodexTurnStateResponseEvidence(
				upstreamResponseModelObserverFromContext(c),
				codexTurnStateExpectedResponseModel(result.StateVerification.RequestedModel),
			) == nil
		}
	}
	result.Session = lease.Finish(trace.LastTurnState(), acceptSessionState, start)
	finished = true
	return result, nil
}

func debugWorkbenchRequestModel(body []byte) string {
	var request struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}
	return strings.TrimSpace(request.Model)
}

func debugWorkbenchStateVerification(body []byte, attempts []DebugUpstreamAttempt, trace *DebugWorkbenchTrace, observer *upstreamResponseModelObserver, result *OpenAIForwardResult, replayState string) *DebugStateVerification {
	var request struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &request)
	evidence := &DebugStateVerification{RequestedModel: strings.TrimSpace(request.Model)}
	if trace != nil {
		evidence.StateSource = trace.TurnStateSource()
	}
	if observer != nil {
		evidence.ResponseCreatedModel, evidence.ResponseCompletedModel, _ = observer.CodexTurnStateEvidence()
		evidence.ResponseModel = observer.Model()
	}
	selected := -1
	for i := len(attempts) - 1; i >= 0; i-- {
		if attempts[i].Response != nil {
			selected = i
			break
		}
	}
	if selected < 0 && len(attempts) > 0 {
		selected = len(attempts) - 1
	}
	if selected < 0 {
		return evidence
	}
	attempt := attempts[selected]
	evidence.ActualAccountID = attempt.AccountID
	evidence.StateSent, _ = debugTurnStateHeaderEvidence(attempt.Request.Headers)
	if attempt.Response != nil {
		evidence.StateReceived, evidence.StateLength = debugTurnStateHeaderEvidence(attempt.Response.Headers)
	}
	if replayState != "" && result != nil && result.UpstreamTurnState != nil {
		evidence.StateMatchesCapture = strings.TrimSpace(*result.UpstreamTurnState) == strings.TrimSpace(replayState)
	}
	return evidence
}

// attachDebugWorkbenchUsageEvidence links a debug run to the durable usage log
// recorded with the real dedicated API key after Forward. Ordinary synthetic
// administrator runs are not billed. The lookup is bounded and fail-closed;
// it never creates substitute evidence. A row is accepted only when its request ID, account,
// and requested model all belong to this run. The upstream response model is
// copied verbatim; it is evidence, not a value to rewrite in the response.
func (s *DebugWorkbenchService) attachDebugWorkbenchUsageEvidence(ctx context.Context, requestID string, accountID, apiKeyID int64, evidence *DebugStateVerification, result *OpenAIForwardResult) bool {
	if s == nil || s.gateway == nil || s.gateway.usageLogRepo == nil || evidence == nil || result == nil {
		return false
	}
	requestID = strings.TrimSpace(requestID)
	expectedModel := strings.TrimSpace(evidence.RequestedModel)
	if requestID == "" || accountID <= 0 || apiKeyID <= 0 || expectedModel == "" {
		return false
	}
	actualAccountID := evidence.ActualAccountID
	createdModel := strings.TrimSpace(evidence.ResponseCreatedModel)
	completedModel := strings.TrimSpace(evidence.ResponseCompletedModel)
	rawResponseModel := strings.TrimSpace(result.UpstreamResponseModel)
	if actualAccountID <= 0 || createdModel == "" || completedModel == "" || rawResponseModel == "" {
		return false
	}
	resultCreated := strings.TrimSpace(result.CodexTurnStateResponseCreatedModel)
	resultCompleted := strings.TrimSpace(result.CodexTurnStateResponseCompletedModel)
	if result.CodexTurnStateResponseFailed || result.UpstreamResponseModelConflict ||
		createdModel != resultCreated || completedModel != resultCompleted || rawResponseModel != completedModel {
		return false
	}
	sentState := ""
	if result.UpstreamTurnState != nil {
		sentState = strings.TrimSpace(*result.UpstreamTurnState)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	lookupCtx, cancel := context.WithTimeout(ctx, debugWorkbenchUsageLookupDelay*time.Duration(debugWorkbenchUsageLookupTries+1))
	defer cancel()
	requestIDs := []string{"client:" + requestID, requestID}
	params := pagination.PaginationParams{Page: 1, PageSize: 4, SortBy: "id", SortOrder: pagination.SortOrderDesc}
	for attempt := 0; attempt < debugWorkbenchUsageLookupTries; attempt++ {
		for _, candidate := range requestIDs {
			logs, _, err := s.gateway.usageLogRepo.ListWithFilters(lookupCtx, params, usagestats.UsageLogFilters{
				RequestID: candidate,
				SkipCount: true,
			})
			if err != nil {
				// A database error is not evidence of a match. Stop rather than
				// retrying indefinitely or turning a transient error into a pass.
				return false
			}
			for i := range logs {
				log := &logs[i]
				if log.RequestID != candidate || log.AccountID != accountID || log.AccountID != actualAccountID || log.APIKeyID != apiKeyID {
					continue
				}
				loggedModel := strings.TrimSpace(log.RequestedModel)
				if loggedModel == "" {
					loggedModel = strings.TrimSpace(log.Model)
				}
				// Requested model is the exact client value. Family equivalence is
				// used only for upstream response acceptance, never to make a usage
				// row from another requested model look like this request.
				if loggedModel == "" || loggedModel != expectedModel {
					continue
				}
				if optionalStringValue(log.InboundEndpoint) != codexTurnStateUsageVerificationEndpoint ||
					optionalStringValue(log.UpstreamEndpoint) != codexTurnStateUsageVerificationEndpoint ||
					optionalStringValue(log.UpstreamTurnState) != sentState ||
					log.UpstreamResponseModel == nil || strings.TrimSpace(*log.UpstreamResponseModel) != rawResponseModel {
					// The account/model row is real, but without the raw upstream
					// lifecycle and state identity it cannot satisfy verification.
					continue
				}
				evidence.UsageLogAccountID = log.AccountID
				evidence.UsageLogAPIKeyID = log.APIKeyID
				evidence.UsageLogRequestedModel = loggedModel
				evidence.UsageLogStateSent = sentState != ""
				// Preserve the exact stored string. Astra family normalization is
				// performed only by the verifier, never by the evidence display.
				evidence.UpstreamResponseModel = *log.UpstreamResponseModel
				evidence.UsageLogVerified = true
				return true
			}
		}
		if attempt+1 < debugWorkbenchUsageLookupTries {
			timer := time.NewTimer(debugWorkbenchUsageLookupDelay * time.Duration(attempt+1))
			select {
			case <-lookupCtx.Done():
				return false
			case <-timer.C:
			}
		}
	}
	return false
}

func debugTurnStateHeaderEvidence(headers map[string][]string) (present bool, length int) {
	for name, values := range headers {
		if !strings.EqualFold(name, openAICodexTurnStateHeader) || len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if value == "" {
			return false, 0
		}
		const prefix = "[redacted:"
		if strings.HasPrefix(value, prefix) && strings.HasSuffix(value, "]") {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(value, prefix), "]"))
			if err == nil && n >= 0 {
				return true, n
			}
		}
		return true, len(value)
	}
	return false, 0
}

func debugWorkbenchSessionError(err error) error {
	switch {
	case errors.Is(err, ErrDebugSessionBusy):
		return debugInputError(http.StatusConflict, "debug session already has an active request")
	case errors.Is(err, ErrDebugSessionModelMismatch):
		return debugInputError(http.StatusConflict, "debug session belongs to a different model; start a new session before changing models")
	case errors.Is(err, ErrDebugSessionAPIKeyMismatch):
		return debugInputError(http.StatusConflict, "debug session belongs to a different dedicated API key; restart the verification sequence")
	case errors.Is(err, ErrDebugSessionCaptureMissing):
		return debugInputError(http.StatusConflict, "debug capture state is missing or was not verified; run capture again")
	case errors.Is(err, ErrDebugSessionProxyMismatch):
		return debugInputError(http.StatusConflict, "replay must use the capture's exact sticky proxy; daily-route verification is performed by the server")
	case errors.Is(err, ErrDebugSessionCapacity):
		return debugInputError(http.StatusServiceUnavailable, "debug session capacity reached")
	case errors.Is(err, ErrDebugSessionNotFound):
		return debugInputError(http.StatusNotFound, "debug session not found or expired for this account")
	default:
		return debugInputError(http.StatusBadRequest, "invalid debug session input")
	}
}

// Gateway allowlists and account overrides still decide final forwarding. This
// initial pass excludes transport, credentials and managed session values, so
// editor placeholders or an administrator bearer can never become credentials.
func debugWorkbenchExecutableHeaders(submitted http.Header) http.Header {
	out := make(http.Header, len(submitted))
	for name, values := range submitted {
		lower := strings.ToLower(name)
		if debugWorkbenchManagedHeader(lower) {
			continue
		}
		if len(values) != 1 || strings.Contains(values[0], "••") || strings.Contains(values[0], "<generated-") {
			continue
		}
		out[name] = append([]string(nil), values...)
	}
	return out
}

func debugWorkbenchManagedHeader(name string) bool {
	switch name {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-goog-api-key", "chatgpt-account-id", "chatgpt-account-id-fallback",
		"host", "content-length", "transfer-encoding", "connection", "keep-alive", "proxy-authenticate", "proxy-connection", "te", "trailer", "upgrade", "accept-encoding", "content-type",
		"session-id", "session_id", "conversation_id", "thread-id", "thread_id", "turn-id", "turn_id", "x-codex-window-id", "x-codex-turn-state":
		return true
	}
	return strings.HasPrefix(name, "sec-websocket-")
}

// OAuth's formal HTTP builder derives session_id from prompt_cache_key. Fill
// that native body field only when absent, rather than bypassing its account
// isolation by overwriting the final request headers. Explicit keys are retained.
func debugWorkbenchSessionPromptCache(account *Account, endpoint string, body []byte, sessionID string) ([]byte, string, string) {
	if !account.UsesOpenAICodexProtocol() {
		return body, "", ""
	}
	if endpoint == "images/generations" {
		return body, "", "OAuth 生图使用正式 Images→Responses 承载转换；最终 Session 头与缓存键遵循该转换，不承诺所有协议具有相同的会话头。"
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return body, "", ""
	}
	if raw, exists := payload["prompt_cache_key"]; exists {
		var key string
		_ = json.Unmarshal(raw, &key)
		return body, key, "已保留显式 prompt_cache_key；上游 session_id 根据该值及所选账号的原生隔离规则重建，空值或类型错误交由正式网关处理。"
	}
	payload["prompt_cache_key"], _ = json.Marshal(sessionID)
	rebuilt, err := json.Marshal(payload)
	if err != nil {
		return body, "", ""
	}
	return rebuilt, sessionID, "Body 未指定 prompt_cache_key，已使用本次托管会话的稳定 Session ID 补齐；上游 session_id 继续由正式网关进行账号隔离。"
}

// Align only existing current identity fields. It does not invent metadata,
// ancestry, installation IDs or subagent roles, and does not change other body
// parameters. RawMessage keeps large numeric ordinals lossless on re-encoding.
func debugWorkbenchAlignSessionMetadata(headers http.Header, body []byte, view DebugSessionView) ([]byte, bool, error) {
	changed := false
	if raw := headers.Get("X-Codex-Turn-Metadata"); raw != "" {
		normalized, edited, err := debugWorkbenchAlignMetadataObject([]byte(raw), view)
		if err != nil {
			return nil, false, fmt.Errorf("X-Codex-Turn-Metadata must be a JSON object")
		}
		if edited {
			headers.Set("X-Codex-Turn-Metadata", string(normalized))
			changed = true
		}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("body must be a JSON object")
	}
	raw, exists := payload["client_metadata"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return body, changed, nil
	}
	normalized, edited, err := debugWorkbenchAlignMetadataObject(raw, view)
	if err != nil {
		return nil, false, fmt.Errorf("body.client_metadata must be a JSON object")
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(normalized, &metadata); err != nil {
		return nil, false, fmt.Errorf("body.client_metadata must be a JSON object")
	}
	if embedded, ok := metadata["x-codex-turn-metadata"]; ok {
		var encoded string
		if err := json.Unmarshal(embedded, &encoded); err != nil {
			return nil, false, fmt.Errorf("body.client_metadata.x-codex-turn-metadata must be a JSON object encoded as a string")
		}
		embeddedBody, embeddedChanged, err := debugWorkbenchAlignMetadataObject([]byte(encoded), view)
		if err != nil {
			return nil, false, fmt.Errorf("body.client_metadata.x-codex-turn-metadata must be a JSON object encoded as a string")
		}
		if embeddedChanged {
			metadata["x-codex-turn-metadata"], _ = json.Marshal(string(embeddedBody))
			edited = true
		}
	}
	if !edited {
		return body, changed, nil
	}
	payload["client_metadata"], _ = json.Marshal(metadata)
	rebuilt, err := json.Marshal(payload)
	return rebuilt, true, err
}

func debugWorkbenchAlignMetadataObject(raw []byte, view DebugSessionView) ([]byte, bool, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return nil, false, fmt.Errorf("metadata must be a JSON object")
	}
	replacements := map[string]string{
		"session_id": view.SessionID, "session-id": view.SessionID, "conversation_id": view.SessionID,
		"thread_id": view.ThreadID, "thread-id": view.ThreadID,
		"turn_id": view.TurnID, "turn-id": view.TurnID,
		"window_id": view.WindowID, "x-codex-window-id": view.WindowID,
	}
	changed := false
	for key, value := range replacements {
		if previous, exists := metadata[key]; exists {
			var old string
			_ = json.Unmarshal(previous, &old)
			if old != value {
				metadata[key], _ = json.Marshal(value)
				changed = true
			}
		}
	}
	if !changed {
		return raw, false, nil
	}
	rebuilt, err := json.Marshal(metadata)
	return rebuilt, true, err
}

func debugWorkbenchCredentialSecrets(credentials map[string]any) []string {
	var secrets []string
	var collect func(any)
	collect = func(value any) {
		switch v := value.(type) {
		case string:
			if len(v) >= 4 {
				secrets = append(secrets, v)
			}
		case map[string]any:
			for _, nested := range v {
				collect(nested)
			}
		case map[string]string:
			for _, nested := range v {
				collect(nested)
			}
		case []any:
			for _, nested := range v {
				collect(nested)
			}
		}
	}
	// Non-secret configuration (base URLs, model mappings, routing enums) must
	// remain visible. Custom header values are opaque and always treated private.
	for key, value := range credentials {
		name := strings.ToLower(key)
		if name == "header_overrides" || strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "password") || strings.Contains(name, "cookie") || strings.Contains(name, "api_key") || strings.Contains(name, "private_key") || strings.HasPrefix(name, "chatgpt_") {
			collect(value)
		}
	}
	return secrets
}

// Ensure extra JSON after the envelope is rejected by the handler's decoder.
func DecodeDebugWorkbenchRequest(r io.Reader) (DebugWorkbenchRequest, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var input DebugWorkbenchRequest
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return input, fmt.Errorf("debug request must contain exactly one JSON object")
	}
	return input, nil
}
