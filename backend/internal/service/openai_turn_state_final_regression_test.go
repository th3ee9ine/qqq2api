package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type finalTurnStateAccountRepo struct {
	AccountRepository
	current *Account
	calls   atomic.Int32
}

func (r *finalTurnStateAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	r.calls.Add(1)
	return r.current, nil
}

func finalTurnStateChatResponse(state, model string) *http.Response {
	body := `data: {"type":"response.completed","response":{"id":"resp_turn_state","object":"response","model":"` + model + `","status":"completed","output":[{"type":"message","id":"msg_turn_state","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n" +
		"data: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func finalTurnStateChatContext(t *testing.T, body []byte, state string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("session_id", "turn-state-final-regression-session")
	if state != "" {
		c.Request.Header.Set(openAICodexTurnStateHeader, state)
	}
	return c, recorder
}

func TestTurnStateFinalRegressionChatCompletionsCollectsMatchingState(t *testing.T) {
	for _, test := range []struct {
		name   string
		stream bool
		marker byte
	}{
		{name: "buffered", stream: false, marker: 201},
		{name: "streaming", stream: true, marker: 202},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			state := collectorTestToken(t, now.Add(-time.Minute), 2, test.marker)
			upstream := &httpUpstreamRecorder{resp: finalTurnStateChatResponse(state, "gpt-5.5")}
			svc := newCodexTurnStateGatewayTestService(upstream)
			svc.SetCodexTurnStateRuntimeSettings(false, true)
			account := codexTurnStateGatewayTestAccount(2000 + int64(test.marker))
			body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			if test.stream {
				body = []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"stream":true}`)
			}
			c, recorder := finalTurnStateChatContext(t, body, "")

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "gpt-5.5")

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "gpt-5.5", result.UpstreamResponseModel)
			require.Len(t, upstream.requests, 1)
			require.Empty(t, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, state, recorder.Header().Get(openAICodexTurnStateHeader), "the client must actually receive the state before it becomes reusable")

			key := svc.codexTurnStateKey(c, account, "gpt-5.5")
			active, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
			require.True(t, usable)
			require.Equal(t, state, active.Token.Value)
			require.False(t, svc.codexTurnStateCollector.Status(key, time.Now()).Ready, "the first successful observation must not leave a standby candidate")
		})
	}
}

func TestTurnStateFinalRegressionChatCompletionsMismatchInvalidatesRequestState(t *testing.T) {
	for _, test := range []struct {
		name   string
		stream bool
		marker byte
	}{
		{name: "buffered", stream: false, marker: 211},
		{name: "streaming", stream: true, marker: 212},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			requestState := collectorTestToken(t, now.Add(-time.Minute), 2, test.marker)
			responseState := collectorTestToken(t, now, 2, test.marker+10)
			mismatchResponse := finalTurnStateChatResponse(responseState, "gpt-6-astra")
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				mismatchResponse,
				{
					StatusCode: http.StatusBadRequest,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"stop after request capture"}}`)),
				},
			}}
			svc := newCodexTurnStateGatewayTestService(upstream)
			svc.SetCodexTurnStateRuntimeSettings(false, true)
			account := codexTurnStateGatewayTestAccount(2100 + int64(test.marker))
			body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			if test.stream {
				body = []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"stream":true}`)
			}
			c, recorder := finalTurnStateChatContext(t, body, "")
			BindOpenAICodexTurnStateExecutionScope(c, body)
			key := svc.codexTurnStateKey(c, account, "gpt-5.5")
			require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, requestState, "seed", now))

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "gpt-5.5")

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, requestState, upstream.requests[0].Header.Get(openAICodexTurnStateHeader), "the mismatch must retire the state actually used by this request")
			require.Empty(t, mismatchResponse.Header.Get(openAICodexTurnStateHeader))
			require.Empty(t, recorder.Header().Get(openAICodexTurnStateHeader), "a mismatched response state must not reach the client")
			_, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
			require.False(t, usable)
			require.True(t, svc.openAICodexTurnStateInvalidated(c, "gpt-5.5", requestState))
			require.False(t, svc.openAICodexTurnStateInvalidated(c, "gpt-5.5", responseState))

			cNext, _ := finalTurnStateChatContext(t, body, requestState)
			nextResult, nextErr := svc.ForwardAsChatCompletions(context.Background(), cNext, account, body, "", "gpt-5.5")
			require.Error(t, nextErr)
			require.Nil(t, nextResult)
			require.Len(t, upstream.requests, 2)
			require.Empty(t, upstream.requests[1].Header.Get(openAICodexTurnStateHeader), "the next request must not replay the tombstoned state")
		})
	}
}

func TestTurnStateFinalRegressionBufferedFailureEvidenceInvalidatesRequestState(t *testing.T) {
	for index, test := range []struct {
		name      string
		response  func(string) *http.Response
		wantError bool
	}{
		{
			name: "failed terminal",
			response: func(state string) *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
						http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
					},
					Body: io.NopCloser(strings.NewReader(`data: {"type":"response.failed","response":{"id":"resp_failed","model":"gpt-6-astra","status":"failed","output":[],"error":{"code":"server_error","message":"synthetic failure"}}}` + "\n\n")),
				}
			},
			wantError: true,
		},
		{
			name: "incomplete terminal",
			response: func(state string) *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
						http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
					},
					Body: io.NopCloser(strings.NewReader(`data: {"type":"response.incomplete","response":{"id":"resp_incomplete","model":"gpt-6-astra","status":"incomplete","output":[],"incomplete_details":{"reason":"max_output_tokens"}}}` + "\n\n")),
				}
			},
		},
		{
			name: "read error after mismatched event",
			response: func(state string) *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"text/event-stream"},
						http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
					},
					Body: &codexTurnStateErrorBody{reader: strings.NewReader(`data: {"type":"response.created","response":{"id":"resp_read_error","model":"gpt-6-astra","status":"in_progress"}}` + "\n\n")},
				}
			},
			wantError: true,
		},
		{
			name: "http error body",
			response: func(state string) *http.Response {
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
						http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","model":"gpt-6-astra","message":"wrong model"}}`)),
				}
			},
			wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			now := time.Now().UTC()
			requestState := collectorTestToken(t, now.Add(-time.Minute), 2, byte(221+index))
			responseState := collectorTestToken(t, now, 2, byte(241+index))
			response := test.response(responseState)
			upstream := &httpUpstreamRecorder{resp: response}
			svc := newCodexTurnStateGatewayTestService(upstream)
			svc.SetCodexTurnStateRuntimeSettings(false, true)
			account := codexTurnStateGatewayTestAccount(int64(2500 + index))
			body := []byte(`{"model":"gpt-5.5","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			c, recorder := finalTurnStateChatContext(t, body, "")
			BindOpenAICodexTurnStateExecutionScope(c, body)
			key := svc.codexTurnStateKey(c, account, "gpt-5.5")
			require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, requestState, "seed", now))

			_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "gpt-5.5")

			if test.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, upstream.requests, 1)
			require.Equal(t, requestState, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
			require.Empty(t, response.Header.Get(openAICodexTurnStateHeader), "mismatch evidence must remove the response state even on an unsuccessful response")
			require.Empty(t, recorder.Header().Get(openAICodexTurnStateHeader))
			_, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
			require.False(t, usable, "mismatch evidence must retire the request state before returning the response error")
			require.True(t, svc.openAICodexTurnStateInvalidated(c, "gpt-5.5", requestState))
		})
	}
}

func TestTurnStateFinalRegressionLateBindAdminDisableFencesCollection(t *testing.T) {
	now := time.Now().UTC()
	responseState := collectorTestToken(t, now, 2, 231)
	upstream := &httpUpstreamRecorder{resp: finalTurnStateChatResponse(responseState, "gpt-5.5")}
	svc := newCodexTurnStateGatewayTestService(upstream)
	svc.SetCodexTurnStateRuntimeSettings(true, true)
	stale := codexTurnStateGatewayTestAccount(2301)
	current := *stale
	current.Schedulable = false
	repo := &finalTurnStateAccountRepo{current: &current}
	svc.accountRepo = repo
	c := newCodexTurnStateGatewayTestContext(t, "late-bind-admin-disable")

	svc.codexTurnStateCollector.DeleteAccount(stale.ID)
	snapshot, forwarded := svc.prepareCodexTurnState(context.Background(), c, stale, "gpt-5.5", make(http.Header), true)

	require.False(t, forwarded)
	require.Empty(t, snapshot.Token.Value)
	require.Empty(t, upstream.requests, "the first post-invalidation bind must re-read eligibility before probing")
	key := svc.codexTurnStateKey(c, stale, "gpt-5.5")
	response := http.Header{}
	response.Set(openAICodexTurnStateHeader, responseState)
	svc.observeCodexTurnStateResponse(c, stale, "gpt-5.5", response, "gpt-5.5")
	_, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.False(t, usable, "a response from the stale schedulable snapshot must not publish after admin disable")
	require.GreaterOrEqual(t, repo.calls.Load(), int32(2), "probe preparation and response publication must both use the authoritative row")
}

func TestTurnStateFinalRegressionLateBindOAuthIdentityChangeFencesResponse(t *testing.T) {
	now := time.Now().UTC()
	responseState := collectorTestToken(t, now, 2, 241)
	upstream := &httpUpstreamRecorder{}
	svc := newCodexTurnStateGatewayTestService(upstream)
	svc.SetCodexTurnStateRuntimeSettings(false, true)
	stale := codexTurnStateGatewayTestAccount(2401)
	stale.Credentials = map[string]any{
		"access_token":       "old-access-token",
		"chatgpt_account_id": "old-chatgpt-account",
	}
	current := *stale
	current.Credentials = map[string]any{
		"access_token":       "new-access-token",
		"chatgpt_account_id": "new-chatgpt-account",
	}
	repo := &finalTurnStateAccountRepo{current: &current}
	svc.accountRepo = repo
	c := newCodexTurnStateGatewayTestContext(t, "late-bind-oauth-identity-change")
	_, err := svc.prepareCodexAccountIdentitySource(context.Background(), c, stale)
	require.NoError(t, err)

	svc.codexTurnStateCollector.DeleteAccount(stale.ID)
	snapshot, forwarded := svc.prepareCodexTurnState(context.Background(), c, stale, "gpt-5.5", make(http.Header), false)
	require.False(t, forwarded)
	require.Empty(t, snapshot.Token.Value)
	key := svc.codexTurnStateKey(c, stale, "gpt-5.5")

	response := http.Header{}
	response.Set(openAICodexTurnStateHeader, responseState)
	svc.observeCodexTurnStateResponse(c, stale, "gpt-5.5", response, "gpt-5.5")

	_, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.False(t, usable, "a response minted by the old OAuth identity must not enter the new collector generation")
	require.Empty(t, upstream.requests)
	require.GreaterOrEqual(t, repo.calls.Load(), int32(1), "response publication must compare against the authoritative identity")
}
