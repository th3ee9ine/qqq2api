package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/pkg/proxyurl"
	"github.com/th3ee9ine/qqq2api/internal/pkg/proxyutil"
	"github.com/tidwall/gjson"
	golangproxy "golang.org/x/net/proxy"
)

const (
	codexTurnStateLiveOptInEnv          = "CODEX_TURN_STATE_LIVE"
	codexTurnStateLiveBaselineOptInEnv  = "CODEX_TURN_STATE_LIVE_BASELINE"
	codexTurnStateLiveAccountFileEnv    = "CODEX_TURN_STATE_LIVE_ACCOUNT_FILE"
	codexTurnStateLiveModelEnv          = "CODEX_TURN_STATE_LIVE_MODEL"
	codexTurnStateLiveDailyProxyEnv     = "CODEX_TURN_STATE_LIVE_DAILY_PROXY_URL"
	codexTurnStateLiveDynamicProxiesEnv = "CODEX_TURN_STATE_LIVE_DYNAMIC_PROXY_URLS"
	codexTurnStateLivePreProxyEnv       = "CODEX_TURN_STATE_LIVE_PRE_PROXY_URL"
	codexTurnStateLiveForceHTTPProxyEnv = "CODEX_TURN_STATE_LIVE_FORCE_HTTP_PROXY"
	codexTurnStateLiveClientVersionEnv  = "CODEX_TURN_STATE_LIVE_CLIENT_VERSION"
	codexTurnStateLivePrompt            = codexTurnStateProbePrompt
	codexTurnStateLiveDefaultModel      = "gpt-6-astra"
	codexTurnStateLiveRequestLimit      = 12
)

// TestCodexTurnStateLiveBaseline is deliberately separate from the recovery
// integration test: it sends exactly one short request on the account's daily
// route, without a native or cached state, and never publishes the response
// header. This makes the pre-change model evidence repeatable without requiring
// or consuming a dynamic sticky session.
func TestCodexTurnStateLiveBaseline(t *testing.T) {
	if !codexTurnStateLiveEnabled(os.Getenv(codexTurnStateLiveBaselineOptInEnv)) {
		t.Skip("set CODEX_TURN_STATE_LIVE_BASELINE=1 to explicitly enable one real baseline request")
	}

	accountPath := strings.TrimSpace(os.Getenv(codexTurnStateLiveAccountFileEnv))
	dailyRaw := strings.TrimSpace(os.Getenv(codexTurnStateLiveDailyProxyEnv))
	missing := make([]string, 0, 2)
	if accountPath == "" {
		missing = append(missing, codexTurnStateLiveAccountFileEnv)
	}
	if dailyRaw == "" {
		missing = append(missing, codexTurnStateLiveDailyProxyEnv)
	}
	if len(missing) > 0 {
		t.Fatalf("missing live configuration fields: %s", strings.Join(missing, ", "))
	}

	account := codexTurnStateLiveLoadAccount(t, accountPath)
	model := strings.TrimSpace(os.Getenv(codexTurnStateLiveModelEnv))
	if model == "" {
		model = codexTurnStateLiveDefaultModel
	}
	if _, err := NormalizeOpenAICodexTurnStateDefaultModel(model); err != nil {
		t.Fatalf("invalid %s", codexTurnStateLiveModelEnv)
	}
	dailyProxy, _, _, err := codexTurnStateLiveNormalizeSOCKS5(dailyRaw, false)
	if err != nil {
		t.Fatalf("invalid %s", codexTurnStateLiveDailyProxyEnv)
	}

	network := newCodexTurnStateLiveHTTP(t, account.ID, model, []string{dailyProxy})
	t.Cleanup(network.close)
	settings, settingsRepo := turnStateTestSettings("", "")
	settingsRepo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "true"
	settingsRepo.values[SettingKeyOpenAICodexTurnStateDefaultModel] = model
	codexTurnStateLiveInstallIdentity(t, settings, settingsRepo)
	service := &OpenAIGatewayService{
		settingService: settings,
		httpUpstream:   network,
		cfg:            &config.Config{},
	}

	phase := "baseline_without_state"
	_, observer, requestErr := codexTurnStateLiveRequest(
		t, service, network, account, model, dailyProxy, "", false, phase,
	)
	codexTurnStateLiveStopOn429(t, phase, requestErr)
	expected := codexTurnStateExpectedResponseModel(model)
	codexTurnStateLiveLogEvidence(t, phase, expected, observer, requestErr)
	if requestErr != nil {
		t.Fatalf("phase=%s failed code=%s", phase, codexTurnStateLiveErrorCode(requestErr))
	}
}

// Explicit opt-in only: this test sends real requests and spends quota. It
// keeps credentials, proxy URLs and issued states in memory and never prints
// them. One run is pinned to exactly one exported account and at most three
// unique sticky proxy sessions.
func TestCodexTurnStateLiveIntegration(t *testing.T) {
	if !codexTurnStateLiveEnabled(os.Getenv(codexTurnStateLiveOptInEnv)) {
		t.Skip("set CODEX_TURN_STATE_LIVE=1 to explicitly enable real upstream requests")
	}

	accountPath := strings.TrimSpace(os.Getenv(codexTurnStateLiveAccountFileEnv))
	dailyRaw := strings.TrimSpace(os.Getenv(codexTurnStateLiveDailyProxyEnv))
	dynamicRaw := strings.TrimSpace(os.Getenv(codexTurnStateLiveDynamicProxiesEnv))
	missing := make([]string, 0, 3)
	if accountPath == "" {
		missing = append(missing, codexTurnStateLiveAccountFileEnv)
	}
	if dailyRaw == "" {
		missing = append(missing, codexTurnStateLiveDailyProxyEnv)
	}
	if dynamicRaw == "" {
		missing = append(missing, codexTurnStateLiveDynamicProxiesEnv)
	}
	if len(missing) > 0 {
		t.Fatalf("missing live configuration fields: %s", strings.Join(missing, ", "))
	}

	account := codexTurnStateLiveLoadAccount(t, accountPath)
	model := strings.TrimSpace(os.Getenv(codexTurnStateLiveModelEnv))
	if model == "" {
		model = codexTurnStateLiveDefaultModel
	}
	if _, err := NormalizeOpenAICodexTurnStateDefaultModel(model); err != nil {
		t.Fatalf("invalid %s", codexTurnStateLiveModelEnv)
	}
	expectedModel := codexTurnStateExpectedResponseModel(model)

	dailyProxy, dynamicProxies := codexTurnStateLiveLoadProxies(t, dailyRaw, dynamicRaw)
	allowedProxies := append([]string{dailyProxy}, dynamicProxies...)
	network := newCodexTurnStateLiveHTTP(t, account.ID, model, allowedProxies)
	t.Cleanup(network.close)

	repo := &turnStateAutoRepo{accounts: map[int64]*Account{account.ID: account}}
	settings, settingsRepo := turnStateTestSettings("", "")
	settingsRepo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "true"
	settingsRepo.values[SettingKeyOpenAICodexTurnStateDefaultModel] = model
	codexTurnStateLiveInstallIdentity(t, settings, settingsRepo)
	service := &OpenAIGatewayService{
		settingService: settings,
		accountRepo:    repo,
		httpUpstream:   network,
		cfg:            &config.Config{},
	}
	t.Cleanup(func() { codexTurnStateLiveWait(t, service) })
	gin.SetMode(gin.TestMode)

	snapshot := func() *Account {
		stored, err := repo.GetByID(context.Background(), account.ID)
		if err != nil {
			t.Fatal("live account snapshot failed")
		}
		return stored
	}
	if state := codexTurnStateAutoToken(codexTurnStateModelAccount(snapshot(), model)); state != "" {
		t.Fatal("live account was not stripped of cached state")
	}

	t.Logf("requested_model=%q expected_response_model=%q dynamic_sessions=%d", model, expectedModel, len(dynamicProxies))

	// Phase 1: establish the real baseline on the account's daily route without
	// injecting any old state. A mismatch is recorded but is not mistaken for a
	// successful recovery and does not prevent trying a fresh sticky session.
	baselinePhase := "baseline_without_state"
	_, baselineObserver, baselineErr := codexTurnStateLiveRequest(
		t, service, network, snapshot(), model, dailyProxy, "", false, baselinePhase,
	)
	codexTurnStateLiveStopOn429(t, baselinePhase, baselineErr)
	codexTurnStateLiveLogEvidence(t, baselinePhase, expectedModel, baselineObserver, baselineErr)
	if baselineErr != nil && !errors.Is(baselineErr, errCodexTurnStateResponseModelMismatch) {
		t.Fatalf("phase=%s failed code=%s", baselinePhase, codexTurnStateLiveErrorCode(baselineErr))
	}
	if state := codexTurnStateAutoToken(codexTurnStateModelAccount(snapshot(), model)); state != "" {
		t.Fatal("baseline request unexpectedly populated the automatic cache")
	}

	// Phases 2-3: collect on a new sticky route, then replay the exact opaque blob
	// on that same route. Only a candidate with matching response.created and
	// response.completed evidence at both phases is eligible for publication. A
	// 429 stops the entire run immediately.
	var acceptedState string
	for index, dynamicProxy := range dynamicProxies {
		attempt := index + 1
		collectPhase := fmt.Sprintf("dynamic_collect_%d", attempt)
		candidate, observer, err := codexTurnStateLiveRequest(
			t, service, network, snapshot(), model, dynamicProxy, "", true, collectPhase,
		)
		codexTurnStateLiveStopOn429(t, collectPhase, err)
		codexTurnStateLiveLogEvidence(t, collectPhase, expectedModel, observer, err)
		if err != nil {
			if !codexTurnStateProbeRetryable(err) {
				t.Fatalf("phase=%s failed code=%s", collectPhase, codexTurnStateLiveErrorCode(err))
			}
			continue
		}

		stickyPhase := fmt.Sprintf("sticky_replay_%d", attempt)
		_, observer, err = codexTurnStateLiveRequest(
			t, service, network, snapshot(), model, dynamicProxy, candidate, false, stickyPhase,
		)
		codexTurnStateLiveStopOn429(t, stickyPhase, err)
		codexTurnStateLiveLogEvidence(t, stickyPhase, expectedModel, observer, err)
		if err != nil {
			if !codexTurnStateProbeRetryable(err) {
				t.Fatalf("phase=%s failed code=%s", stickyPhase, codexTurnStateLiveErrorCode(err))
			}
			continue
		}

		acceptedState = candidate
		break
	}
	if acceptedState == "" {
		t.Fatal("no candidate passed dynamic collection and same-route sticky replay")
	}

	// Dynamic collection and same-route replay may only stage a memory-only candidate.
	// Publication additionally requires a formal short /v1/responses request to
	// reserve that exact candidate and a newly inserted usage row containing the
	// same request ID, API key, account, requested model, outbound state, endpoint,
	// and raw created/completed model evidence. Never use the internal replay marker
	// here: doing so would bypass the production publication gate this test exists
	// to exercise.
	now := time.Now()
	service.openaiTurnStateMu.Lock()
	entry := service.codexTurnStateEntryLocked(snapshot(), now, model)
	service.stageCodexTurnStateUsageCandidateLocked(entry, acceptedState, entry.recovery.InvalidatedAtMS, now)
	service.openaiTurnStateMu.Unlock()
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	service.usageLogRepo = usageRepo
	codexTurnStateLiveUsageAcceptance(
		t, service, network, usageRepo, snapshot(), model, expectedModel, dailyProxy, acceptedState,
	)
	codexTurnStateLiveWait(t, service)
	stored := snapshot()
	scoped := codexTurnStateModelAccount(stored, model)
	if codexTurnStateAutoToken(scoped) != acceptedState ||
		strings.TrimSpace(scoped.GetExtraString(CodexTurnStateAutoVerifiedModelExtraKey)) != model {
		t.Fatal("validated state was not atomically published to the same account and model")
	}
	repo.mu.Lock()
	writes := repo.writes
	repo.mu.Unlock()
	if writes != 1 || usageRepo.calls != 1 {
		t.Fatal("validated state publication was not a single atomic account update")
	}
	t.Logf("phase=state_publish model=%q state_chars=%d usage_rows=%d account_writes=%d", model, len(acceptedState), usageRepo.calls, writes)

	// Final acceptance: construct an ordinary gateway request without a native
	// state header. Sub2API must inject the just-published value automatically,
	// the adapter verifies the exact outbound blob, and the real response must
	// again declare the expected model in both lifecycle events.
	codexTurnStateLiveAutomaticAcceptance(
		t, service, network, snapshot(), model, expectedModel, dailyProxy, acceptedState,
	)
}

func codexTurnStateLiveUsageAcceptance(
	t *testing.T,
	service *OpenAIGatewayService,
	network *codexTurnStateLiveHTTP,
	usageRepo *openAIRecordUsageLogRepoStub,
	account *Account,
	model, expectedModel, dailyProxy, acceptedState string,
) {
	t.Helper()
	const (
		apiKeyID       int64 = 701
		localRequestID       = "turn-state-live-usage-acceptance"
	)
	ctx, cancel := context.WithTimeout(context.Background(), codexTurnStateProbeSessionTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, ctxkey.RequestID, localRequestID)
	ctx = WithOpenAICodexTurnStateUsageVerification(ctx, apiKeyID)
	payload := createOpenAICodexTurnStateProbePayload(model)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal("usage acceptance request encoding failed")
	}
	if !IsOpenAICodexTurnStateUsageVerificationRequest(body, "") {
		t.Fatal("usage acceptance request did not match the strict short-request predicate")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	req, err := service.buildUpstreamRequest(
		ctx, c, account, body, account.GetOpenAIAccessToken(), true, "", true, model,
	)
	if err != nil {
		t.Fatal("usage acceptance request construction failed")
	}
	if req.Header.Get(openAICodexTurnStateHeader) != acceptedState {
		t.Fatal("usage acceptance did not reserve and inject the staged candidate")
	}

	phase := "usage_row_acceptance"
	network.setPhase(phase, acceptedState)
	resp, err := service.doOpenAIUpstream(req, dailyProxy, account)
	if err != nil {
		t.Fatalf("phase=%s transport_failed=true", phase)
	}
	if resp == nil {
		t.Fatalf("phase=%s empty_response=true", phase)
	}
	defer resp.Body.Close()
	if code, delay := codexTurnStateProbe429Diagnostic(resp.StatusCode, resp.Header, time.Now()); code != "" {
		retryAfterSeconds := (delay + time.Second - 1) / time.Second
		t.Fatalf("phase=%s stopped_on_429=true code=%s retry_after_seconds=%d", phase, code, retryAfterSeconds)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("phase=%s http=%d", phase, resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, codexTurnStateAutoMaxBody+1))
	if err != nil || len(responseBody) > codexTurnStateAutoMaxBody {
		t.Fatalf("phase=%s response_not_completed=true", phase)
	}
	observer := &upstreamResponseModelObserver{}
	if bodyHasSSEFraming(responseBody) {
		observeOpenAISSEBody(observer, string(responseBody))
	} else if len(bytes.TrimSpace(responseBody)) > 0 {
		observer.ObserveOpenAI(responseBody, strings.TrimSpace(gjson.GetBytes(responseBody, "type").String()))
	}
	validationErr := validateCodexTurnStateResponseEvidence(observer, expectedModel)
	codexTurnStateLiveLogEvidence(t, phase, expectedModel, observer, validationErr)
	if validationErr != nil {
		t.Fatalf("phase=%s failed code=%s", phase, codexTurnStateLiveErrorCode(validationErr))
	}
	if output := codexTurnStateLiveOutputText(responseBody); output != "OK" {
		t.Fatalf("phase=%s output_not_exact=true", phase)
	}
	createdModel, completedModel, failed := observer.CodexTurnStateEvidence()
	endpoint := codexTurnStateUsageVerificationEndpoint
	sentState := req.Header.Get(openAICodexTurnStateHeader)
	usageLog := &UsageLog{
		RequestID:             "local:" + localRequestID,
		APIKeyID:              apiKeyID,
		AccountID:             account.ID,
		RequestedModel:        model,
		UpstreamEndpoint:      &endpoint,
		UpstreamTurnState:     &sentState,
		UpstreamResponseModel: &completedModel,
	}
	input := &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{
		UpstreamResponseModel:                completedModel,
		CodexTurnStateResponseCreatedModel:   createdModel,
		CodexTurnStateResponseCompletedModel: completedModel,
		CodexTurnStateResponseFailed:         failed,
		UpstreamResponseModelConflict:        observer.Conflict(),
	}}
	service.writeOpenAIUsageLogWithTurnStateGate(ctx, input, usageLog)
	if usageRepo.calls != 1 || usageRepo.lastLog != usageLog {
		t.Fatal("usage acceptance did not insert exactly one new evidence row")
	}
	t.Logf("phase=%s candidate_reserved=true usage_row_inserted=true output_exact=true state_chars=%d", phase, len(acceptedState))
}

func codexTurnStateLiveEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func codexTurnStateLiveInstallIdentity(t *testing.T, settings *SettingService, repo *codexHeaderSettingRepoStub) {
	t.Helper()
	version := strings.TrimSpace(os.Getenv(codexTurnStateLiveClientVersionEnv))
	if version != "" {
		version = normalizeStableCodexClientVersion(version)
		if version == "" {
			t.Fatalf("invalid %s", codexTurnStateLiveClientVersionEnv)
		}
		repo.values[SettingKeyOpenAICodexClientVersionSynced] = version
	}

	codexCanonicalUAMu.RLock()
	previousUAResolver := codexCanonicalUAResolver
	codexCanonicalUAMu.RUnlock()
	codexCanonicalOriginatorMu.RLock()
	previousOriginatorResolver := codexCanonicalOriginator
	codexCanonicalOriginatorMu.RUnlock()
	codexCanonicalResponsesVersionMu.RLock()
	previousVersionResolver := codexCanonicalResponsesVersion
	codexCanonicalResponsesVersionMu.RUnlock()
	SetCodexCanonicalUserAgentResolver(func() string {
		return settings.GetOpenAICodexCanonicalUserAgent(context.Background())
	})
	SetCodexCanonicalOriginatorResolver(func() string {
		return settings.GetOpenAICodexOriginator(context.Background())
	})
	SetCodexCanonicalResponsesVersionResolver(func() string {
		return settings.GetOpenAICodexResponsesVersion(context.Background())
	})
	t.Cleanup(func() {
		SetCodexCanonicalUserAgentResolver(previousUAResolver)
		SetCodexCanonicalOriginatorResolver(previousOriginatorResolver)
		SetCodexCanonicalResponsesVersionResolver(previousVersionResolver)
	})
}

func codexTurnStateLiveLoadAccount(t *testing.T, path string) *Account {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s", codexTurnStateLiveAccountFileEnv)
	}
	var exported struct {
		Accounts []struct {
			Platform    string         `json:"platform"`
			Type        string         `json:"type"`
			Credentials map[string]any `json:"credentials"`
			Extra       map[string]any `json:"extra"`
			ProxyID     *int64         `json:"proxy_id"`
			ProxyRef    string         `json:"proxy_ref"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(data, &exported); err != nil || len(exported.Accounts) != 1 {
		t.Fatal("live test requires exactly one exported account")
	}
	source := exported.Accounts[0]
	account := &Account{
		ID:          1,
		Platform:    source.Platform,
		Type:        source.Type,
		Credentials: source.Credentials,
		Extra:       StripCodexTurnStateAutoExtra(source.Extra),
		Concurrency: 1,
		Status:      StatusActive,
		Schedulable: true,
	}
	if !codexTurnStateAutoEligible(account) || source.ProxyID != nil || source.ProxyRef != "" {
		t.Fatal("live test requires one unbound OpenAI Codex account")
	}
	if account.GetOpenAIAccessToken() == "" {
		t.Fatal("live account has no access token")
	}
	return account
}

func codexTurnStateLiveLoadProxies(t *testing.T, dailyRaw, dynamicRaw string) (string, []string) {
	t.Helper()
	daily, _, _, err := codexTurnStateLiveNormalizeSOCKS5(dailyRaw, false)
	if err != nil {
		t.Fatalf("invalid %s", codexTurnStateLiveDailyProxyEnv)
	}

	seenSessions := make(map[string]bool)
	dynamic := make([]string, 0, codexTurnStateProbePoolSize)
	dynamicHost := ""
	for _, raw := range strings.FieldsFunc(dynamicRaw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	}) {
		normalized, candidateHost, session, parseErr := codexTurnStateLiveNormalizeSOCKS5(raw, true)
		if parseErr != nil {
			t.Fatalf("invalid %s", codexTurnStateLiveDynamicProxiesEnv)
		}
		if normalized == daily || seenSessions[session] {
			continue
		}
		if dynamicHost == "" {
			dynamicHost = candidateHost
		} else if dynamicHost != candidateHost {
			t.Fatalf("%s must use one controlled fixed-country host", codexTurnStateLiveDynamicProxiesEnv)
		}
		seenSessions[session] = true
		dynamic = append(dynamic, normalized)
		if len(dynamic) == codexTurnStateProbePoolSize {
			break
		}
	}
	if len(dynamic) == 0 {
		t.Fatalf("%s has no unique sticky sid", codexTurnStateLiveDynamicProxiesEnv)
	}
	return daily, dynamic
}

func codexTurnStateLiveNormalizeSOCKS5(raw string, requireSession bool) (normalized, fixedCountryHost, session string, err error) {
	if !requireSession {
		normalized, parsed, parseErr := proxyurl.Parse(raw)
		if parseErr != nil || parsed == nil || (parsed.Scheme != "socks5" && parsed.Scheme != "socks5h") {
			return "", "", "", errors.New("invalid live proxy configuration")
		}
		return normalized, "", "", nil
	}

	template, parseErr := parseCodexTurnStateProbeProxyTemplate(strings.TrimSpace(raw))
	if parseErr != nil || template.dynamic || (template.protocol != "socks5" && template.protocol != "socks5h") {
		return "", "", "", errors.New("invalid live dynamic proxy configuration")
	}
	// Preserve the provider-authenticated country and sticky session. A live
	// collection run may replace only the sid; it must never rewrite region-US
	// into region-Rand or another country.
	return template.route(template.sessionID), template.host, template.sessionID, nil
}

func codexTurnStateLiveRequest(
	t *testing.T,
	service *OpenAIGatewayService,
	network *codexTurnStateLiveHTTP,
	account *Account,
	model, proxyURL, sentState string,
	requireState bool,
	phase string,
) (string, *upstreamResponseModelObserver, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), codexTurnStateProbeSessionTimeout)
	defer cancel()
	network.setPhase(phase, sentState)
	return service.requestOpenAICodexTurnStateViaProxy(ctx, account, model, proxyURL, sentState, requireState)
}

func codexTurnStateLiveLogEvidence(t *testing.T, phase, expected string, observer *upstreamResponseModelObserver, requestErr error) {
	t.Helper()
	created, completed, failed := observer.CodexTurnStateEvidence()
	matched := validateCodexTurnStateResponseEvidence(observer, expected) == nil
	t.Logf(
		"phase=%s created_model=%q completed_model=%q expected_model=%q failed_event=%t model_match=%t code=%s",
		phase, created, completed, expected, failed, matched, codexTurnStateLiveErrorCode(requestErr),
	)
}

func codexTurnStateLiveErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if code := safeCodexTurnStateAutoError(err.Error()); code != "" {
		return code
	}
	return "request_failed"
}

func codexTurnStateLiveStopOn429(t *testing.T, phase string, err error) {
	t.Helper()
	var httpErr *codexTurnStateProbeHTTPError
	if !errors.As(err, &httpErr) {
		return
	}
	delay := httpErr.retryAfter
	if delay <= 0 {
		delay = codexTurnStateProbe429Fallback
	}
	retryAfterSeconds := (delay + time.Second - 1) / time.Second
	t.Fatalf(
		"phase=%s stopped_on_429=true code=%s retry_after_seconds=%d",
		phase, httpErr.Error(), retryAfterSeconds,
	)
}

func codexTurnStateLiveAutomaticAcceptance(
	t *testing.T,
	service *OpenAIGatewayService,
	network *codexTurnStateLiveHTTP,
	account *Account,
	model, expectedModel, dailyProxy, acceptedState string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), codexTurnStateProbeSessionTimeout)
	defer cancel()
	payload := createOpenAICodexTurnStateProbePayload(model)
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal("automatic acceptance request encoding failed")
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	req, err := service.buildUpstreamRequest(
		ctx, c, account, body, account.GetOpenAIAccessToken(), true, "", true, model,
	)
	if err != nil {
		t.Fatal("automatic acceptance request construction failed")
	}
	if req.Header.Get(openAICodexTurnStateHeader) != acceptedState {
		t.Fatal("automatic acceptance did not inject the validated state")
	}

	phase := "automatic_injection_acceptance"
	network.setPhase(phase, acceptedState)
	resp, err := service.doOpenAIUpstream(req, dailyProxy, account)
	if err != nil {
		t.Fatalf("phase=%s transport_failed=true", phase)
	}
	if resp == nil {
		t.Fatalf("phase=%s empty_response=true", phase)
	}
	defer resp.Body.Close()
	if code, delay := codexTurnStateProbe429Diagnostic(resp.StatusCode, resp.Header, time.Now()); code != "" {
		retryAfterSeconds := (delay + time.Second - 1) / time.Second
		t.Fatalf("phase=%s stopped_on_429=true code=%s retry_after_seconds=%d", phase, code, retryAfterSeconds)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("phase=%s http=%d", phase, resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, codexTurnStateAutoMaxBody+1))
	if err != nil || len(responseBody) > codexTurnStateAutoMaxBody {
		t.Fatalf("phase=%s response_not_completed=true", phase)
	}
	observer := &upstreamResponseModelObserver{}
	if bodyHasSSEFraming(responseBody) {
		observeOpenAISSEBody(observer, string(responseBody))
	} else if len(bytes.TrimSpace(responseBody)) > 0 {
		observer.ObserveOpenAI(responseBody, strings.TrimSpace(gjson.GetBytes(responseBody, "type").String()))
	}
	validationErr := validateCodexTurnStateResponseEvidence(observer, expectedModel)
	codexTurnStateLiveLogEvidence(t, phase, expectedModel, observer, validationErr)
	if validationErr != nil {
		t.Fatalf("phase=%s failed code=%s", phase, codexTurnStateLiveErrorCode(validationErr))
	}
	if output := codexTurnStateLiveOutputText(responseBody); output != "OK" {
		t.Fatalf("phase=%s output_not_exact=true", phase)
	}
	if sent := upstreamTurnStateFromResponse(resp); sent == nil || *sent != acceptedState {
		t.Fatal("automatic acceptance lost the outbound state snapshot")
	}
	t.Logf("phase=%s auto_injected=true output_exact=true state_chars=%d", phase, len(acceptedState))
}

func codexTurnStateLiveOutputText(body []byte) string {
	text := ""
	doneText := ""
	var deltas []string
	extract := func(payload []byte) {
		response := gjson.GetBytes(payload, "response")
		if !response.Exists() {
			response = gjson.ParseBytes(payload)
		}
		var parts []string
		for _, output := range response.Get("output").Array() {
			for _, content := range output.Get("content").Array() {
				if content.Get("type").String() == "output_text" {
					parts = append(parts, content.Get("text").String())
				}
			}
		}
		if len(parts) > 0 {
			text = strings.Join(parts, "")
		}
	}
	if bodyHasSSEFraming(body) {
		forEachOpenAISSEFrame(string(body), func(eventType string, payload []byte) {
			switch eventType {
			case "response.output_text.delta":
				deltas = append(deltas, gjson.GetBytes(payload, "delta").String())
			case "response.output_text.done":
				doneText = gjson.GetBytes(payload, "text").String()
			case "response.completed", "response.done":
				extract(payload)
			}
		})
	} else {
		extract(body)
	}
	if text == "" {
		text = doneText
	}
	if text == "" {
		text = strings.Join(deltas, "")
	}
	return strings.TrimSpace(text)
}

func TestCodexTurnStateLiveOutputTextReadsResponsesSSETextEvents(t *testing.T) {
	body := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"O"}`,
		``,
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"K"}`,
		``,
		`event: response.output_text.done`,
		`data: {"type":"response.output_text.done","text":"OK"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"model":"gpt-6-astra"}}`,
		``,
	}, "\n")
	require.Equal(t, "OK", codexTurnStateLiveOutputText([]byte(body)))
}

func codexTurnStateLiveWait(t *testing.T, service *OpenAIGatewayService) {
	t.Helper()
	requireBy := time.Now().Add(30 * time.Second)
	for time.Now().Before(requireBy) {
		service.openaiTurnStateMu.Lock()
		idle := service.openaiTurnStateWorkers == 0
		service.openaiTurnStateMu.Unlock()
		if idle {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("live turn-state worker did not finish within 30 seconds")
}

type codexTurnStateLiveHTTP struct {
	HTTPUpstream

	t         *testing.T
	accountID int64
	model     string
	allowed   map[string]bool

	mu            sync.Mutex
	phase         string
	expectedState string
	count         int
	clients       map[string]*http.Client
	transports    []*http.Transport
}

func newCodexTurnStateLiveHTTP(t *testing.T, accountID int64, model string, proxies []string) *codexTurnStateLiveHTTP {
	allowed := make(map[string]bool, len(proxies))
	for _, proxyURL := range proxies {
		allowed[proxyURL] = true
	}
	return &codexTurnStateLiveHTTP{
		t: t, accountID: accountID, model: model, allowed: allowed,
		clients: make(map[string]*http.Client),
	}
}

func (u *codexTurnStateLiveHTTP) setPhase(phase, expectedState string) {
	u.mu.Lock()
	u.phase, u.expectedState = phase, expectedState
	u.mu.Unlock()
}

func (u *codexTurnStateLiveHTTP) Do(req *http.Request, proxyURL string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.count++
	requestNumber := u.count
	phase, expectedState := u.phase, u.expectedState
	allowed := u.allowed[proxyURL]
	u.mu.Unlock()

	if requestNumber > codexTurnStateLiveRequestLimit || accountID != u.accountID || !allowed ||
		req == nil || req.URL.Scheme != "https" || req.URL.Host != "chatgpt.com" || req.URL.Path != "/backend-api/codex/responses" {
		return nil, errors.New("live request guard rejected request")
	}
	if req.Body == nil {
		return nil, errors.New("live request body guard rejected request")
	}
	if req.Header.Get(openAICodexTurnStateHeader) != expectedState {
		return nil, errors.New("live request state guard rejected request")
	}
	payload, err := io.ReadAll(io.LimitReader(req.Body, codexTurnStateAutoMaxBody+1))
	if err != nil || len(payload) > codexTurnStateAutoMaxBody {
		return nil, errors.New("live request body guard rejected request")
	}
	req.Body = io.NopCloser(bytes.NewReader(payload))
	if strings.TrimSpace(gjson.GetBytes(payload, "model").String()) != u.model ||
		strings.TrimSpace(gjson.GetBytes(payload, "instructions").String()) == "" ||
		gjson.GetBytes(payload, "input.0.type").String() != "message" ||
		gjson.GetBytes(payload, "input.0.content.0.text").String() != codexTurnStateLivePrompt {
		return nil, errors.New("live request payload guard rejected request")
	}

	client, err := u.client(proxyURL)
	if err != nil {
		return nil, errors.New("live proxy client creation failed")
	}
	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		u.t.Logf("request=%d phase=%s transport_failed=true elapsed_ms=%d", requestNumber, phase, time.Since(started).Milliseconds())
		return nil, errors.New("live upstream transport failed")
	}
	if resp == nil {
		return nil, errors.New("live upstream returned empty response")
	}
	if resp.StatusCode >= http.StatusBadRequest && resp.Body != nil {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, codexTurnStateAutoMaxBody+1))
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if readErr == nil {
			u.t.Logf(
				"request=%d phase=%s error_body_bytes=%d error_json=%t error_shape=%s error_category=%s error_type=%q error_code=%q error_param=%q",
				requestNumber,
				phase,
				len(body),
				gjson.ValidBytes(body),
				codexTurnStateLiveErrorShape(body),
				codexTurnStateLiveErrorCategory(body),
				codexTurnStateLiveDiagnosticValue(gjson.GetBytes(body, "error.type").String()),
				codexTurnStateLiveDiagnosticValue(gjson.GetBytes(body, "error.code").String()),
				codexTurnStateLiveDiagnosticValue(gjson.GetBytes(body, "error.param").String()),
			)
		}
	}
	u.t.Logf(
		"request=%d phase=%s http=%d header_ms=%d sent_state=%t received_state_chars=%d",
		requestNumber,
		phase,
		resp.StatusCode,
		time.Since(started).Milliseconds(),
		expectedState != "",
		len(extractOpenAICodexTurnState(resp.Header)),
	)
	return resp, nil
}

func codexTurnStateLiveErrorShape(body []byte) string {
	switch {
	case gjson.GetBytes(body, "error").IsObject():
		return "error_object"
	case gjson.GetBytes(body, "error").Exists():
		return "error_value"
	case gjson.GetBytes(body, "detail").Exists():
		return "detail"
	case gjson.GetBytes(body, "message").Exists():
		return "message"
	default:
		return "unknown"
	}
}

// Only a fixed category is emitted. Provider text can contain account IDs,
// request content or proxy details and is never logged.
func codexTurnStateLiveErrorCategory(body []byte) string {
	message := strings.ToLower(strings.TrimSpace(firstNonEmptyString(
		gjson.GetBytes(body, "error.message").String(),
		gjson.GetBytes(body, "message").String(),
		gjson.GetBytes(body, "detail").String(),
	)))
	switch {
	case message == "":
		return "unclassified"
	case strings.Contains(message, "model") &&
		(strings.Contains(message, "unsupported") || strings.Contains(message, "not supported") || strings.Contains(message, "not found")):
		return "model_rejected"
	case strings.Contains(message, "input") &&
		(strings.Contains(message, "type") || strings.Contains(message, "message") || strings.Contains(message, "format")):
		return "input_shape"
	case strings.Contains(message, "stream") || strings.Contains(message, "store"):
		return "request_options"
	case strings.Contains(message, "instruction"):
		return "instructions"
	case strings.Contains(message, "account") || strings.Contains(message, "workspace"):
		return "account_scope"
	case strings.Contains(message, "auth") || strings.Contains(message, "token"):
		return "authentication"
	case strings.Contains(message, "permission") || strings.Contains(message, "forbidden") || strings.Contains(message, "denied"):
		return "access_denied"
	case strings.Contains(message, "country") || strings.Contains(message, "region") || strings.Contains(message, "location"):
		return "geo_restricted"
	case strings.Contains(message, "version") || strings.Contains(message, "client") || strings.Contains(message, "update"):
		return "client_identity"
	case strings.Contains(message, "quota") || strings.Contains(message, "rate limit") || strings.Contains(message, "usage limit"):
		return "quota"
	case strings.Contains(message, "json") || strings.Contains(message, "payload") || strings.Contains(message, "request body") ||
		strings.Contains(message, "malformed") || strings.Contains(message, "parse") || strings.Contains(message, "validation"):
		return "request_validation"
	default:
		return "provider_rejected"
	}
}

func TestCodexTurnStateLiveErrorDiagnosticsStayCategorical(t *testing.T) {
	tests := []struct {
		body, shape, category string
	}{
		{`{"error":{"message":"model is not supported"}}`, "error_object", "model_rejected"},
		{`{"detail":"request body validation failed"}`, "detail", "request_validation"},
		{`{"message":"client version must be updated"}`, "message", "client_identity"},
		{`{"detail":"private provider prose"}`, "detail", "provider_rejected"},
	}
	for _, test := range tests {
		require.Equal(t, test.shape, codexTurnStateLiveErrorShape([]byte(test.body)))
		require.Equal(t, test.category, codexTurnStateLiveErrorCategory([]byte(test.body)))
	}
}

func codexTurnStateLiveDiagnosticValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 80 {
		return ""
	}
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '_', '-', '.', '/', '[', ']':
			continue
		default:
			return ""
		}
	}
	return raw
}

func (u *codexTurnStateLiveHTTP) client(proxyURL string) (*http.Client, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if client := u.clients[proxyURL]; client != nil {
		return client, nil
	}
	_, parsed, err := proxyurl.Parse(proxyURL)
	if err != nil || parsed == nil {
		return nil, errors.New("invalid live proxy")
	}
	parsedCopy := *parsed
	parsed = &parsedCopy
	// Some proxy vendors document a SOCKS-shaped credential URI while the
	// advertised endpoint actually speaks HTTP CONNECT. This opt-in exists only
	// for the explicit live test and never changes production proxy semantics.
	if codexTurnStateLiveEnabled(os.Getenv(codexTurnStateLiveForceHTTPProxyEnv)) {
		parsed.Scheme = "http"
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
	}
	if preProxyRaw := strings.TrimSpace(os.Getenv(codexTurnStateLivePreProxyEnv)); preProxyRaw != "" {
		_, preProxy, parseErr := proxyurl.Parse(preProxyRaw)
		if parseErr != nil || preProxy == nil || (preProxy.Scheme != "socks5" && preProxy.Scheme != "socks5h") {
			return nil, errors.New("invalid live pre-proxy")
		}
		var auth *golangproxy.Auth
		if preProxy.User != nil {
			password, _ := preProxy.User.Password()
			auth = &golangproxy.Auth{User: preProxy.User.Username(), Password: password}
		}
		dialer, dialErr := golangproxy.SOCKS5(
			"tcp",
			preProxy.Host,
			auth,
			&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
		)
		if dialErr != nil {
			return nil, errors.New("live pre-proxy configuration failed")
		}
		if contextDialer, ok := dialer.(golangproxy.ContextDialer); ok {
			transport.DialContext = contextDialer.DialContext
		} else {
			transport.DialContext = func(_ context.Context, network, address string) (net.Conn, error) {
				return dialer.Dial(network, address)
			}
		}
	}
	if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
		return nil, errors.New("live proxy configuration failed")
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	u.clients[proxyURL] = client
	u.transports = append(u.transports, transport)
	return client, nil
}

func (u *codexTurnStateLiveHTTP) close() {
	u.mu.Lock()
	transports := append([]*http.Transport(nil), u.transports...)
	u.mu.Unlock()
	for _, transport := range transports {
		transport.CloseIdleConnections()
	}
}
