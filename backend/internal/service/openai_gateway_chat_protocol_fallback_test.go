//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai_compat"
	"github.com/tidwall/gjson"
)

// The Responses leg is deliberately made to fail before it writes anything to
// the client.  A successful second response proves that protocol fallback is
// a single, replay-safe retry rather than a second attempt at Responses.
func TestForwardAsChatCompletions_StructuredInputRejectionFallsBackOnce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  int
		gotType string
	}{
		{name: "400 array", status: http.StatusBadRequest, gotType: "array"},
		{name: "422 object", status: http.StatusUnprocessableEntity, gotType: "object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			body := []byte(`{"model":"client-model","messages":[{"role":"user","content":"hello"}],"stream":false,"stop":["END"],"max_tokens":32,"temperature":0.2,"top_p":0.9,"response_format":{"type":"json_object"},"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],"tool_choice":"auto","parallel_tool_calls":false}`)
			recorder, c := protocolFallbackContext(http.MethodPost, "/v1/chat/completions", body)
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				protocolFallbackErrorResponse(tc.status, fmt.Sprintf("Invalid type for 'input': expected a string, but got a %s instead.", tc.gotType)),
				protocolFallbackChatResponse(),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := protocolFallbackUnknownAPIKeyAccount()
			account.Credentials["model_mapping"] = map[string]any{"client-model": "mapped-model"}

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, 2, "structured-input rejection permits exactly one protocol retry")
			require.Equal(t, "/v1/responses", upstream.requests[0].URL.Path)
			require.Equal(t, "/v1/chat/completions", upstream.requests[1].URL.Path)

			// The retry is the original Chat request, not the lossy Responses
			// representation.  In particular, preserve Chat-only fields.
			retryBody := upstream.bodies[1]
			require.Equal(t, "mapped-model", gjson.GetBytes(retryBody, "model").String())
			require.Equal(t, "hello", gjson.GetBytes(retryBody, "messages.0.content").String())
			require.Equal(t, "END", gjson.GetBytes(retryBody, "stop.0").String())
			require.Equal(t, int64(32), gjson.GetBytes(retryBody, "max_tokens").Int())
			require.InDelta(t, 0.2, gjson.GetBytes(retryBody, "temperature").Float(), 0.0001)
			require.InDelta(t, 0.9, gjson.GetBytes(retryBody, "top_p").Float(), 0.0001)
			require.Equal(t, "json_object", gjson.GetBytes(retryBody, "response_format.type").String())
			require.Equal(t, "lookup", gjson.GetBytes(retryBody, "tools.0.function.name").String())
			require.Equal(t, "auto", gjson.GetBytes(retryBody, "tool_choice").String())
			require.False(t, gjson.GetBytes(retryBody, "parallel_tool_calls").Bool())
			require.Equal(t, "ok", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
		})
	}
}

// A Responses-shaped body can arrive on /v1/chat/completions (Cursor and a
// few other clients do this).  Both explicit force_chat and an HTTP-error
// fallback must lower it to Chat messages; forwarding input unchanged would
// make a Chat-only upstream reject the second request again.
func TestForwardAsChatCompletions_ResponsesShapeIsConvertedForForcedChatAndErrorFallback(t *testing.T) {
	for _, tc := range []struct {
		name       string
		account    func() *Account
		responses  []*http.Response
		wantCalls  int
		wantFirst  string
		wantSecond bool
	}{
		{
			name:      "force_chat",
			account:   protocolFallbackForceChatAccount,
			responses: []*http.Response{protocolFallbackChatResponse()},
			wantCalls: 1,
			wantFirst: "/v1/chat/completions",
		},
		{
			name:    "structured_error_fallback",
			account: protocolFallbackUnknownAPIKeyAccount,
			responses: []*http.Response{
				protocolFallbackErrorResponse(http.StatusBadRequest, "Invalid type for 'input': expected a string, but got an array instead."),
				protocolFallbackChatResponse(),
			},
			wantCalls:  2,
			wantFirst:  "/v1/responses",
			wantSecond: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			body := protocolFallbackResponsesShapeBody()
			recorder, c := protocolFallbackContext(http.MethodPost, "/v1/chat/completions", body)
			upstream := &httpUpstreamRecorder{responses: tc.responses}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			account := tc.account()
			account.Credentials["model_mapping"] = map[string]any{"client-model": "mapped-model"}

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Len(t, upstream.requests, tc.wantCalls)
			require.Equal(t, tc.wantFirst, upstream.requests[0].URL.Path)
			if tc.wantSecond {
				require.Equal(t, "/v1/chat/completions", upstream.requests[1].URL.Path)
			}

			// Inspect the final Chat request for both paths.  The first request in
			// error-fallback is the original Responses shape; the second is the
			// converted body that must be accepted by a Chat-only upstream.
			chatBody := upstream.bodies[len(upstream.bodies)-1]
			require.Equal(t, "mapped-model", gjson.GetBytes(chatBody, "model").String())
			require.False(t, gjson.GetBytes(chatBody, "input").Exists())
			require.GreaterOrEqual(t, len(gjson.GetBytes(chatBody, "messages").Array()), 4)
			require.Equal(t, "system", gjson.GetBytes(chatBody, "messages.0.role").String())
			require.Equal(t, "Be concise.", gjson.GetBytes(chatBody, "messages.0.content").String())
			require.Equal(t, "user", gjson.GetBytes(chatBody, "messages.1.role").String())
			require.Equal(t, "hello", gjson.GetBytes(chatBody, "messages.1.content").String())
			require.Equal(t, "assistant", gjson.GetBytes(chatBody, "messages.2.role").String())
			require.Equal(t, "lookup", gjson.GetBytes(chatBody, "messages.2.tool_calls.0.function.name").String())
			require.Equal(t, "tool", gjson.GetBytes(chatBody, "messages.3.role").String())
			require.Equal(t, "call_lookup", gjson.GetBytes(chatBody, "messages.3.tool_call_id").String())
			require.Equal(t, int64(123), gjson.GetBytes(chatBody, "max_completion_tokens").Int())
			require.Equal(t, "high", gjson.GetBytes(chatBody, "reasoning_effort").String())
			require.Equal(t, "json_object", gjson.GetBytes(chatBody, "response_format.type").String())
			require.Equal(t, "lookup", gjson.GetBytes(chatBody, "tools.0.function.name").String())
			require.Equal(t, "ok", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
		})
	}
}

func TestForwardAsChatCompletions_DoesNotFallbackForUnsafeOrKnownResponsesErrors(t *testing.T) {
	t.Parallel()

	structural := `{"error":{"message":"Invalid type for 'input': expected a string, but got an array instead."}}`
	cases := []struct {
		name       string
		status     int
		errorBody  string
		account    func() *Account
		wantCalls  int
		wantResult bool
	}{
		{name: "ordinary input content validation", status: http.StatusBadRequest, errorBody: `{"error":{"param":"input[0].content","message":"Invalid 'input[0].content': array too long."}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "generic bad request", status: http.StatusBadRequest, errorBody: `{"error":{"message":"invalid request"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "model not found on 400", status: http.StatusBadRequest, errorBody: `{"error":{"code":"model_not_found","message":"model not found"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "model not found on 404", status: http.StatusNotFound, errorBody: `{"error":{"code":"model_not_found","message":"model not found"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "unauthorized", status: http.StatusUnauthorized, errorBody: `{"error":{"message":"invalid api key"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "forbidden", status: http.StatusForbidden, errorBody: `{"error":{"message":"forbidden"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "rate limited", status: http.StatusTooManyRequests, errorBody: `{"error":{"message":"rate limited"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "server error", status: http.StatusInternalServerError, errorBody: `{"error":{"message":"temporary failure"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 1},
		{name: "confirmed supported", status: http.StatusBadRequest, errorBody: structural, account: protocolFallbackSupportedAPIKeyAccount, wantCalls: 1},
		{name: "force responses", status: http.StatusBadRequest, errorBody: structural, account: protocolFallbackForceResponsesAccount, wantCalls: 1},
		{name: "oauth", status: http.StatusBadRequest, errorBody: structural, account: protocolFallbackOAuthAccount, wantCalls: 1},
		{name: "404 endpoint missing", status: http.StatusNotFound, errorBody: `{"error":{"message":"endpoint not found"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 2, wantResult: true},
		{name: "405 endpoint missing", status: http.StatusMethodNotAllowed, errorBody: `{"error":{"message":"method not allowed"}}`, account: protocolFallbackUnknownAPIKeyAccount, wantCalls: 2, wantResult: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			_, c := protocolFallbackContext(http.MethodPost, "/v1/chat/completions", body)
			responses := []*http.Response{protocolFallbackJSONErrorResponse(tc.status, tc.errorBody)}
			if tc.wantCalls == 2 {
				responses = append(responses, protocolFallbackChatResponse())
			}
			upstream := &httpUpstreamRecorder{responses: responses}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, tc.account(), body, "", "")
			if tc.wantResult {
				require.NoError(t, err)
				require.NotNil(t, result)
			} else {
				require.Error(t, err)
				require.Nil(t, result)
			}
			require.Len(t, upstream.requests, tc.wantCalls)
		})
	}
}

func TestForwardAsChatCompletions_DoesNotRetryAfterCancellationOrCommittedOutput(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "cancelled_context"
		if committed {
			name = "response_already_committed"
		}
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":false}`)
			_, c := protocolFallbackContext(http.MethodPost, "/v1/chat/completions", body)
			if committed {
				// Model the Responses leg having already published output. A
				// protocol retry would duplicate that output under a new contract.
				_, _ = c.Writer.Write([]byte("partial output"))
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				protocolFallbackErrorResponse(http.StatusBadRequest, "Invalid type for 'input': expected a string, but got an array instead."),
				protocolFallbackChatResponse(),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			ctx := context.Background()
			if !committed {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			result, err := svc.ForwardAsChatCompletions(ctx, c, protocolFallbackUnknownAPIKeyAccount(), body, "", "")
			require.Error(t, err)
			require.Nil(t, result)
			require.Len(t, upstream.requests, 1, "unsafe replay must never issue a Chat protocol retry")
		})
	}
}

func protocolFallbackContext(method, path string, body []byte) (*httptest.ResponseRecorder, *gin.Context) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return recorder, c
}

func protocolFallbackErrorResponse(status int, message string) *http.Response {
	return protocolFallbackJSONErrorResponse(status, fmt.Sprintf(`{"error":{"message":%q}}`, message))
}

func protocolFallbackJSONErrorResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func protocolFallbackChatResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}, "x-request-id": []string{"rid-chat-fallback"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"chatcmpl_fallback","object":"chat.completion","model":"mapped-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
		)),
	}
}

func protocolFallbackUnknownAPIKeyAccount() *Account {
	a := rawChatCompletionsTestAccount()
	a.Extra = map[string]any{openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeAuto)}
	return a
}

func protocolFallbackSupportedAPIKeyAccount() *Account {
	a := protocolFallbackUnknownAPIKeyAccount()
	a.Extra[openai_compat.ExtraKeyResponsesSupported] = true
	return a
}

func protocolFallbackForceResponsesAccount() *Account {
	a := protocolFallbackUnknownAPIKeyAccount()
	a.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceResponses)
	return a
}

func protocolFallbackForceChatAccount() *Account {
	a := protocolFallbackUnknownAPIKeyAccount()
	a.Extra[openai_compat.ExtraKeyResponsesMode] = string(openai_compat.ResponsesSupportModeForceChatCompletions)
	return a
}

func protocolFallbackOAuthAccount() *Account {
	return &Account{
		ID: 701, Name: "protocol-fallback-oauth", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Concurrency: 1, Credentials: map[string]any{"access_token": "oauth-token", "chatgpt_account_id": "chatgpt-account", "base_url": "http://upstream.example"},
		Status: StatusActive, Schedulable: true,
	}
}

func protocolFallbackResponsesShapeBody() []byte {
	return []byte(`{"model":"client-model","instructions":"Be concise.","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"think first"}]},{"type":"function_call","id":"fc_1","call_id":"call_lookup","name":"lookup","arguments":"{\"city\":\"Paris\"}"},{"type":"function_call_output","call_id":"call_lookup","output":"{\"temperature\":20}"}],"tools":[{"type":"function","name":"lookup","description":"lookup weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}],"text":{"format":{"type":"json_object"}},"reasoning":{"effort":"high"},"max_output_tokens":123,"stream":false}`)
}
