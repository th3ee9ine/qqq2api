package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type passthroughTurnStateDisconnectWriter struct {
	gin.ResponseWriter
	failedAfter int
	writes      int
}

func (w *passthroughTurnStateDisconnectWriter) Write(data []byte) (int, error) {
	if w.writes >= w.failedAfter {
		return 0, errors.New("client disconnected")
	}
	w.writes++
	return w.ResponseWriter.Write(data)
}

func (w *passthroughTurnStateDisconnectWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

type turnStateCancelingWriteFailureWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *turnStateCancelingWriteFailureWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, errors.New("client disconnected during final response write")
}

func (w *turnStateCancelingWriteFailureWriter) WriteString(string) (int, error) {
	w.cancel()
	return 0, errors.New("client disconnected during final response write")
}

type turnStateShortWriteFailureWriter struct {
	gin.ResponseWriter
}

func (w *turnStateShortWriteFailureWriter) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	return len(data) - 1, nil
}

func (w *turnStateShortWriteFailureWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func newPassthroughTurnStateTestContext(t *testing.T, scope string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	bindOpenAICodexTurnStateExecutionScopeValue(c, scope)
	return c, recorder
}

func newPassthroughTurnStateResponse(state, contentType, body string) *http.Response {
	header := http.Header{"Content-Type": []string{contentType}}
	header.Set(openAICodexTurnStateHeader, state)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func requirePassthroughTurnStateCached(
	t *testing.T,
	svc *OpenAIGatewayService,
	c *gin.Context,
	account *Account,
	model string,
	want string,
) {
	t.Helper()
	snapshot, usable := svc.codexTurnStateCollector.Acquire(svc.codexTurnStateKey(c, account, model), time.Now())
	require.True(t, usable)
	require.Equal(t, want, snapshot.Token.Value)
}

func TestOpenAIPassthroughStreamingCompletedStagesAndCollectsTurnState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1201)
	c, recorder := newPassthroughTurnStateTestContext(t, "passthrough-stream-completed")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 21)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_turn_state_stream","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := newPassthroughTurnStateResponse(state, "text/event-stream", body)

	_, err := svc.handleStreamingResponsePassthrough(
		context.Background(), resp, c, account, time.Now(), "gpt-5.6-sol", "",
	)

	require.NoError(t, err)
	require.Equal(t, state, recorder.Result().Header.Get(openAICodexTurnStateHeader))
	requirePassthroughTurnStateCached(t, svc, c, account, "gpt-5.6-sol", state)
	_, provenanceRecorded := svc.openaiCodexTurnStateOrigins.Load("passthrough-stream-completed")
	require.True(t, provenanceRecorded)
}

func TestOpenAIPassthroughStreamingUnsuccessfulResponsesNeverCollectTurnState(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantError      bool
		wantStateRelay bool
	}{
		{
			name:      "failed before output",
			body:      `data: {"type":"response.failed","response":{"status":"failed","error":{"code":"content_policy","message":"blocked"}}}` + "\n\n",
			wantError: true,
		},
		{
			name: "incomplete before output",
			body: `data: {"type":"response.incomplete","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}` + "\n\n",
		},
		{
			name:           "disconnect after output",
			body:           `data: {"type":"response.output_text.delta","delta":"partial"}` + "\n\n",
			wantError:      true,
			wantStateRelay: true,
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := newCodexTurnStateGatewayTestService(nil)
			account := codexTurnStateGatewayTestAccount(int64(1210 + i))
			scope := "passthrough-stream-unsuccessful-" + test.name
			c, recorder := newPassthroughTurnStateTestContext(t, scope)
			state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, byte(30+i))
			resp := newPassthroughTurnStateResponse(state, "text/event-stream", test.body)

			_, err := svc.handleStreamingResponsePassthrough(
				context.Background(), resp, c, account, time.Now(), "gpt-5.6-sol", "",
			)

			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if test.wantStateRelay {
				require.Equal(t, state, recorder.Result().Header.Get(openAICodexTurnStateHeader))
			} else {
				require.Empty(t, recorder.Result().Header.Get(openAICodexTurnStateHeader))
				_, provenanceRecorded := svc.openaiCodexTurnStateOrigins.Load(scope)
				require.False(t, provenanceRecorded)
			}
			require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
		})
	}
}

func TestOpenAIPassthroughStreamingClientDisconnectDoesNotCollectCompletedTurnState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1220)
	c, _ := newPassthroughTurnStateTestContext(t, "passthrough-stream-client-disconnect")
	c.Writer = &passthroughTurnStateDisconnectWriter{ResponseWriter: c.Writer, failedAfter: 2}
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 40)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"partial"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_turn_state_disconnected","model":"gpt-5.6-sol","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`,
		``,
	}, "\n")
	resp := newPassthroughTurnStateResponse(state, "text/event-stream", body)

	_, err := svc.handleStreamingResponsePassthrough(
		context.Background(), resp, c, account, time.Now(), "gpt-5.6-sol", "",
	)

	require.NoError(t, err, "the handler drains a completed upstream after the downstream disconnects")
	require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
}

func TestOpenAIPassthroughNonStreamingSuccessCollectsTurnState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1221)
	c, recorder := newPassthroughTurnStateTestContext(t, "passthrough-json-completed")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 41)
	body := `{"id":"resp_turn_state_json","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`
	resp := newPassthroughTurnStateResponse(state, "application/json", body)

	_, err := svc.handleNonStreamingResponsePassthrough(
		context.Background(), resp, c, account, "gpt-5.6-sol", "",
	)

	require.NoError(t, err)
	require.Equal(t, state, recorder.Result().Header.Get(openAICodexTurnStateHeader))
	requirePassthroughTurnStateCached(t, svc, c, account, "gpt-5.6-sol", state)
}

func TestOpenAIPassthroughSSEToJSONCompletedCollectsTurnState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1231)
	c, recorder := newPassthroughTurnStateTestContext(t, "passthrough-sse-json-completed")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 51)
	body := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_turn_state_sse_json","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		``,
	}, "\n")
	resp := newPassthroughTurnStateResponse(state, "text/event-stream", body)

	_, err := svc.handleNonStreamingResponsePassthrough(
		context.Background(), resp, c, account, "gpt-5.6-sol", "",
	)

	require.NoError(t, err)
	require.Equal(t, state, recorder.Result().Header.Get(openAICodexTurnStateHeader))
	requirePassthroughTurnStateCached(t, svc, c, account, "gpt-5.6-sol", state)
}

func TestOpenAIPassthroughTurnStateDoesNotCacheWithoutExecutionScope(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1241)
	c, recorder := newPassthroughTurnStateTestContext(t, "")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 61)
	body := `{"id":"resp_turn_state_no_scope","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`
	resp := newPassthroughTurnStateResponse(state, "application/json", body)

	_, err := svc.handleNonStreamingResponsePassthrough(
		context.Background(), resp, c, account, "gpt-5.6-sol", "",
	)

	require.NoError(t, err)
	require.Equal(t, state, recorder.Result().Header.Get(openAICodexTurnStateHeader))
	require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
}

func TestOpenAIPassthroughFailedJSONDoesNotCollectTurnState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1251)
	c, _ := newPassthroughTurnStateTestContext(t, "passthrough-json-failed")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 71)
	body := `{"id":"resp_turn_state_failed","model":"gpt-5.6-sol","status":"failed","error":{"code":"server_error","message":"failed"}}`
	resp := newPassthroughTurnStateResponse(state, "application/json", body)

	_, err := svc.handleNonStreamingResponsePassthrough(
		context.Background(), resp, c, account, "gpt-5.6-sol", "",
	)

	require.NoError(t, err)
	require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
}

func TestOpenAINonStreamingWriteFailureDoesNotCommitTurnState(t *testing.T) {
	completedJSON := `{"id":"resp_turn_state_write_failure","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`
	completedSSE := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_turn_state_write_failure","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		``,
	}, "\n")
	tests := []struct {
		name        string
		passthrough bool
		bridge      bool
		shortWrite  bool
		contentType string
		body        string
	}{
		{name: "forward json", contentType: "application/json", body: completedJSON},
		{name: "forward sse to json", contentType: "text/event-stream", body: completedSSE},
		{name: "forward compact sse bridge", bridge: true, contentType: "application/json", body: completedJSON},
		{name: "passthrough json", passthrough: true, contentType: "application/json", body: completedJSON},
		{name: "passthrough json short write", passthrough: true, shortWrite: true, contentType: "application/json", body: completedJSON},
		{name: "passthrough sse to json", passthrough: true, contentType: "text/event-stream", body: completedSSE},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			svc := newCodexTurnStateGatewayTestService(nil)
			account := codexTurnStateGatewayTestAccount(int64(1260 + i))
			scope := "non-streaming-write-failure-" + test.name
			c, _ := newPassthroughTurnStateTestContext(t, scope)
			requestCtx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(requestCtx)
			if test.shortWrite {
				c.Writer = &turnStateShortWriteFailureWriter{ResponseWriter: c.Writer}
			} else {
				c.Writer = &turnStateCancelingWriteFailureWriter{ResponseWriter: c.Writer, cancel: cancel}
			}
			if test.bridge {
				MarkOpenAICompactClientStream(c)
			}

			state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, byte(80+i))
			resp := newPassthroughTurnStateResponse(state, test.contentType, test.body)

			var err error
			if test.passthrough {
				_, err = svc.handleNonStreamingResponsePassthrough(
					context.Background(), resp, c, account, "gpt-5.6-sol", "gpt-5.6-sol",
				)
			} else {
				_, err = svc.handleNonStreamingResponse(
					context.Background(), resp, c, account, "gpt-5.6-sol", "gpt-5.6-sol",
				)
			}

			require.NoError(t, err)
			if test.shortWrite {
				require.NoError(t, requestCtx.Err())
			} else {
				require.Error(t, requestCtx.Err())
			}
			require.True(t, c.IsAborted())
			require.NotEmpty(t, c.Errors)
			metrics := svc.codexTurnStateCollector.Metrics()
			require.Zero(t, metrics.Entries)
			require.Zero(t, metrics.Offers)
			_, provenanceRecorded := svc.openaiCodexTurnStateOrigins.Load(scope)
			require.False(t, provenanceRecorded)
		})
	}
}

func TestOpenAINonStreamingCommittedKeepaliveTurnStateCommitSemantics(t *testing.T) {
	t.Run("undelivered header is not recorded as provenance", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		account := codexTurnStateGatewayTestAccount(1270)
		scope := "committed-keepalive-undelivered-header"
		c, recorder := newPassthroughTurnStateTestContext(t, scope)
		MarkOpenAICompactClientStream(c)
		stop := StartOpenAICompactSSEKeepalive(c, keepaliveTestInterval)
		defer stop()
		waitForKeepaliveBeats()
		require.True(t, StopOpenAICompactSSEKeepaliveCommitted(c))

		state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 96)
		upstream := make(http.Header)
		upstream.Set(openAICodexTurnStateHeader, state)
		staged, headerDeliverable := svc.prepareOpenAICodexTurnStateForWrite(c, account, upstream)
		require.False(t, headerDeliverable)

		body := []byte(`{"id":"resp_committed_keepalive","model":"gpt-5.6-sol","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`)
		wrote := writeOpenAINonStreamingResponse(c, http.StatusOK, "application/json", body)
		require.True(t, wrote)
		svc.commitOpenAICodexTurnStateAfterWrite(
			c, account, "gpt-5.6-sol", upstream, staged, true, wrote, headerDeliverable,
		)

		require.Empty(t, recorder.Result().Header.Get(openAICodexTurnStateHeader))
		_, provenanceRecorded := svc.openaiCodexTurnStateOrigins.Load(scope)
		require.False(t, provenanceRecorded)
		requirePassthroughTurnStateCached(t, svc, c, account, "gpt-5.6-sol", state)
	})

	t.Run("failure frame write error is propagated", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		account := codexTurnStateGatewayTestAccount(1271)
		scope := "committed-keepalive-failure-write"
		c, _ := newPassthroughTurnStateTestContext(t, scope)
		c.Writer = &passthroughTurnStateDisconnectWriter{ResponseWriter: c.Writer, failedAfter: 1}
		MarkOpenAICompactClientStream(c)
		stop := StartOpenAICompactSSEKeepalive(c, keepaliveTestInterval)
		defer stop()
		waitForKeepaliveBeats()
		require.True(t, StopOpenAICompactSSEKeepaliveCommitted(c))

		state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 97)
		upstream := make(http.Header)
		upstream.Set(openAICodexTurnStateHeader, state)
		staged, headerDeliverable := svc.prepareOpenAICodexTurnStateForWrite(c, account, upstream)
		require.False(t, headerDeliverable)

		wrote := writeOpenAINonStreamingResponse(
			c,
			http.StatusBadGateway,
			"application/json",
			[]byte(`{"error":{"message":"upstream failed"}}`),
		)
		require.False(t, wrote)
		require.NoError(t, c.Request.Context().Err())
		require.True(t, c.IsAborted())
		require.NotEmpty(t, c.Errors)
		svc.commitOpenAICodexTurnStateAfterWrite(
			c, account, "gpt-5.6-sol", upstream, staged, false, wrote, headerDeliverable,
		)

		_, provenanceRecorded := svc.openaiCodexTurnStateOrigins.Load(scope)
		require.False(t, provenanceRecorded)
		metrics := svc.codexTurnStateCollector.Metrics()
		require.Zero(t, metrics.Entries)
		require.Zero(t, metrics.Offers)
	})
}
