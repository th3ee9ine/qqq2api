package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Messages has its own response readers and therefore does not pass through
// every generic Responses commit hook. Keep the model-mismatch cases here so a
// future bridge refactor cannot accidentally leave an invalid collector entry
// reusable after an error or buffered response.
func TestForwardAsAnthropicHTTPErrorModelMismatchInvalidatesCollector(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 231)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusBadRequest,
		Header: http.Header{
			"Content-Type":             []string{"application/json"},
			openAICodexTurnStateHeader: []string{state},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(`{"error":{"model":"gpt-6-astra","message":"wrong model"}}`))),
	}}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(9231)
	body := []byte(`{"model":"gpt-5.5","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	bindOpenAICodexTurnStateExecutionScopeValue(c, "messages-http-error-mismatch")

	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, state, "seed", now))
	_, usable := svc.codexTurnStateCollector.Acquire(key, now)
	require.True(t, usable)

	_, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "gpt-5.5")
	require.Error(t, err)

	_, usable = svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.False(t, usable, "an error payload declaring another model must retire the cached state")
	require.Empty(t, upstream.resp.Header.Get(openAICodexTurnStateHeader), "the invalid state must not remain available for downstream header staging")
}

func TestForwardAsAnthropicBufferedModelMismatchClearsCompatTurnState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 232)
	response := openAICompatSSECompletedResponse("resp_messages_mismatch", "gpt-6-astra")
	response.Header.Set(openAICodexTurnStateHeader, state)
	upstream := &httpUpstreamRecorder{resp: response}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(9232)
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	bindOpenAICodexTurnStateExecutionScopeValue(c, "messages-buffered-mismatch")
	promptCacheKey := "stable-messages-cache"
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, promptCacheKey, state, "gpt-5.5")

	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, state, "seed", now))

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, promptCacheKey, "gpt-5.5")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, promptCacheKey, "gpt-5.5"), "a contradictory buffered response must clear the compatibility state")
	_, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.False(t, usable, "the collector state must be invalidated together with the compatibility binding")
	require.Empty(t, response.Header.Get(openAICodexTurnStateHeader), "the invalid state must not be relayed")
}

func TestMessagesTurnStateModelMismatchMarkerResetsForFailoverAttempt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	markOpenAIWSTurnStateModelMismatch(c)
	require.True(t, openAIWSTurnStateModelMismatchMarked(c))
	clearOpenAIWSTurnStateModelMismatch(c)
	require.False(t, openAIWSTurnStateModelMismatchMarked(c), "a replacement account attempt must not inherit the prior mismatch marker")
}
