package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/securityaudit"
	middleware2 "github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestCachesSecurityAuditCompletionSkipsWebSocketStages(t *testing.T) {
	require.True(t, cachesSecurityAuditCompletion("http"))
	require.True(t, cachesSecurityAuditCompletion(""))
	require.False(t, cachesSecurityAuditCompletion("first_turn"))
	require.False(t, cachesSecurityAuditCompletion("subsequent_turn"))
	require.True(t, isSecurityAuditWebSocketStage("first_turn"))
	require.True(t, isSecurityAuditWebSocketStage("subsequent_turn"))
	require.False(t, isSecurityAuditWebSocketStage("http"))
}

func TestRunSecurityAuditDoesNotSkipSubsequentWebSocketTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeAsync}
	coordinator := securityaudit.NewCoordinator(nil, engine)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	subject := middleware2.AuthSubject{UserID: 7, Concurrency: 1}
	first := runSecurityAudit(c, nil, coordinator, nil, nil, subject, "openai_responses", "gpt-test",
		[]byte(`{"type":"response.create","response":{"input":"benign"}}`), "first_turn")
	require.NotNil(t, first)
	require.True(t, first.AllowNextStage)
	require.Equal(t, int64(1), engine.enqueues.Load())
	_, cached := c.Get(securityAuditCompletedContextKey)
	require.False(t, cached, "WebSocket stages must not set the HTTP completion cache")

	// Even if an HTTP path previously cached completion on this Context, WS turns
	// must still audit every response.create payload.
	c.Set(securityAuditCompletedContextKey, true)

	second := runSecurityAudit(c, nil, coordinator, nil, nil, subject, "openai_responses", "gpt-test",
		[]byte(`{"type":"response.create","response":{"input":"malicious follow-up"}}`), "subsequent_turn")
	require.NotNil(t, second)
	require.Equal(t, int64(2), engine.enqueues.Load(), "subsequent WebSocket turns must be audited again")
}

func TestRunSecurityAuditClearsCachedDecisionMetadataBeforeCompletionReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	first := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", []byte(`{"input":"first"}`), "http")
	require.NotNil(t, first)
	if _, ok := c.Get(securityAuditDecisionContextKey); !ok {
		t.Fatal("first audit should publish decision metadata")
	}
	second := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", []byte(`{"input":"cached"}`), "http")
	require.Nil(t, second)
	value, exists := c.Get(securityAuditDecisionContextKey)
	require.True(t, exists)
	require.Nil(t, value)
}

func TestSetSecurityAuditDecisionMetadataBoundsRemoteEvidence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	longEvidence := strings.Repeat("证据", maxSecurityAuditMetadataValueBytes)
	longMessage := strings.Repeat("消息", maxSecurityAuditMetadataValueBytes)
	decision := &securityaudit.Decision{
		Kind:           securityaudit.DecisionBlock,
		ErrorCode:      longMessage,
		AllowNextStage: false,
		Legacy: &securityaudit.LegacyDecision{
			Message: longMessage,
		},
		Prompt: &securityaudit.PromptDecision{
			Kind: securityaudit.DecisionBlock,
			Result: &securityaudit.NormalizedResult{
				MatchedScanners: []string{longMessage},
				ScannerEvidence: map[string]string{"jailbreak": longEvidence},
			},
		},
	}
	setSecurityAuditDecisionMetadata(c, decision)
	value, exists := c.Get(securityAuditDecisionContextKey)
	require.True(t, exists)
	metadata, ok := value.(map[string]any)
	require.True(t, ok)
	require.LessOrEqual(t, len(metadata["error_code"].(string)), maxSecurityAuditMetadataValueBytes)
	legacy := metadata["legacy"].(map[string]any)
	require.LessOrEqual(t, len(legacy["message"].(string)), maxSecurityAuditMetadataValueBytes)
	prompt := metadata["prompt"].(map[string]any)
	evidence := prompt["scanner_evidence"].(map[string]string)
	require.LessOrEqual(t, len(evidence["jailbreak"]), maxSecurityAuditMetadataValueBytes)
	scanners := prompt["matched_scanners"].([]string)
	require.LessOrEqual(t, len(scanners[0]), maxSecurityAuditMetadataValueBytes)
}

func TestRunSecurityAuditDeduplicatesRepeatedPayloadWithinWebSocketTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	payload := []byte(`{"type":"response.create","response":{"input":"same turn"}}`)
	c.Set(securityAuditWSTurnContextKey, 2)
	first := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	second := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.True(t, first.AllowNextStage)
	require.True(t, second.AllowNextStage)
	require.Equal(t, int64(1), engine.evaluates.Load())

	// The cache holds only one successful same-turn result.
	entry, exists := c.Get(securityAuditWSDedupeContextKey)
	require.True(t, exists)
	require.IsType(t, securityAuditWSDedupeEntry{}, entry)

	c.Set(securityAuditWSTurnContextKey, 3)
	runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestRunSecurityAuditDoesNotCacheFailedWebSocketDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{
		mode: securityaudit.ModeBlocking,
		decisions: []*securityaudit.PromptDecision{
			{Kind: securityaudit.DecisionUnavailable, AllowNextStage: false},
			{Kind: securityaudit.DecisionAllow, AllowNextStage: true},
		},
	}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(securityAuditWSTurnContextKey, 2)
	payload := []byte(`{"type":"response.create","response":{"input":"retry me"}}`)

	first := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	_, cachedAfterFailure := c.Get(securityAuditWSDedupeContextKey)
	second := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")

	require.False(t, first.AllowNextStage)
	require.False(t, cachedAfterFailure)
	require.True(t, second.AllowNextStage)
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestRunSecurityAuditDoesNotCacheFlaggedWebSocketDecision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{
		mode: securityaudit.ModeBlocking,
		decisions: []*securityaudit.PromptDecision{
			{Kind: securityaudit.DecisionFlag, AllowNextStage: true},
			{Kind: securityaudit.DecisionAllow, AllowNextStage: true},
		},
	}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(securityAuditWSTurnContextKey, 2)
	payload := []byte(`{"type":"response.create","response":{"input":"retry flagged"}}`)

	first := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	_, cachedAfterFlag := c.Get(securityAuditWSDedupeContextKey)
	second := runSecurityAudit(c, nil, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")

	require.Equal(t, securityaudit.DecisionFlag, first.Kind)
	require.True(t, first.AllowNextStage)
	require.False(t, cachedAfterFlag)
	require.Equal(t, securityaudit.DecisionAllow, second.Kind)
	require.Equal(t, int64(2), engine.evaluates.Load())
}

func TestRunSecurityAuditLogsWebSocketChecksAndCacheHits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := &turnCountingEngine{mode: securityaudit.ModeBlocking}
	coordinator := securityaudit.NewCoordinator(nil, engine)
	core, logs := observer.New(zap.InfoLevel)
	reqLog := zap.New(core)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(securityAuditWSTurnContextKey, 2)
	payload := []byte(`{"type":"response.create","response":{"input":"same turn"}}`)

	runSecurityAudit(c, reqLog, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")
	runSecurityAudit(c, reqLog, coordinator, nil, nil, middleware2.AuthSubject{UserID: 7}, "openai_responses", "gpt-test", payload, "subsequent_turn")

	startLogs := logs.FilterMessage("security_audit.gateway_check_start").All()
	require.Len(t, startLogs, 1)
	require.Equal(t, false, startLogs[0].ContextMap()["cached"])

	doneLogs := logs.FilterMessage("security_audit.gateway_check_done").All()
	require.Len(t, doneLogs, 2)
	require.Equal(t, false, doneLogs[0].ContextMap()["cached"])
	require.Equal(t, true, doneLogs[1].ContextMap()["cached"])
	require.Equal(t, "allow", doneLogs[1].ContextMap()["decision"])
	require.Equal(t, "subsequent_turn", doneLogs[1].ContextMap()["stage"])
	require.Equal(t, int64(1), engine.evaluates.Load())
}

type turnCountingEngine struct {
	mode      securityaudit.Mode
	enqueues  atomic.Int64
	evaluates atomic.Int64
	decisions []*securityaudit.PromptDecision
}

func (e *turnCountingEngine) EffectiveMode() securityaudit.Mode { return e.mode }
func (e *turnCountingEngine) Enqueue(context.Context, securityaudit.Request) error {
	e.enqueues.Add(1)
	return nil
}
func (e *turnCountingEngine) Evaluate(context.Context, securityaudit.Request) (*securityaudit.PromptDecision, error) {
	call := e.evaluates.Add(1)
	if int(call) <= len(e.decisions) {
		return e.decisions[call-1], nil
	}
	return &securityaudit.PromptDecision{Kind: securityaudit.DecisionAllow, AllowNextStage: true}, nil
}
