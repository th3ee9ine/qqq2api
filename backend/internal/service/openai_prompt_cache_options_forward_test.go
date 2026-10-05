package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai_compat"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const promptCacheOptionsModelRejection = `{"error":{"type":"invalid_request_error","code":"invalid_parameter","param":"prompt_cache_options","message":"prompt_cache_options is not supported on this model"}}`

func TestPromptCacheOptionsResponsesForward(t *testing.T) {
	for _, route := range []struct {
		name        string
		oauth       bool
		codex       bool
		passthrough bool
	}{
		{name: "ordinary API key"},
		{name: "Codex API key", codex: true},
		{name: "ordinary OAuth", oauth: true},
		{name: "Codex OAuth", oauth: true, codex: true},
		{name: "API key HTTP passthrough", passthrough: true},
		{name: "OAuth HTTP passthrough", oauth: true, passthrough: true},
	} {
		for _, outcome := range []struct {
			name     string
			model    string
			reject   bool
			failLast bool
		}{
			{name: "supported new model keeps options", model: "gpt-5.6-sol"},
			{name: "custom upstream accepts older model", model: "gpt-5.4"},
			{name: "explicit rejection retries without options", model: "gpt-5.4", reject: true},
			{name: "repeated rejection is bounded", model: "gpt-5.4", reject: true, failLast: true},
		} {
			t.Run(route.name+"/"+outcome.name, func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":%q,"stream":false,"instructions":"keep instructions","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"prompt_cache_key":"keep-cache-key","reasoning":{"effort":"high"},"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"keep input"}]}]}`, outcome.model))
				account := promptCacheOptionsTestAccount(route.oauth, route.passthrough)
				upstream := promptCacheOptionsTestUpstream(outcome.reject, outcome.failLast, route.oauth && route.passthrough, false)
				c := newOpenAIRejectedFieldTestContext(body)
				SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
				if route.codex {
					c.Request.Header.Set("User-Agent", "codex_cli_rs/0.101.0")
					c.Request.Header.Set("Originator", "codex_cli_rs")
				}

				result, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), c, account, body)
				if outcome.failLast {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
				}
				assertPromptCacheOptionsAttempts(t, upstream, outcome.reject)
			})
		}
	}
}

func TestPromptCacheOptionsChatCompletionsForward(t *testing.T) {
	for _, route := range []struct {
		name           string
		oauth          bool
		raw            bool
		responsesShape bool
	}{
		{name: "API key Chat to Responses"},
		{name: "OAuth Chat to Responses", oauth: true},
		{name: "Responses shape through Chat", oauth: true, responsesShape: true},
		{name: "native Chat Completions", raw: true},
	} {
		for _, outcome := range []struct {
			name     string
			reject   bool
			failLast bool
		}{
			{name: "supported keeps options"},
			{name: "explicit rejection retries", reject: true},
			{name: "repeated rejection is bounded", reject: true, failLast: true},
		} {
			t.Run(route.name+"/"+outcome.name, func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.6-sol","stream":false,"prompt_cache_options":{"mode":"explicit","ttl":"30m"},"reasoning_effort":"high","messages":[{"role":"user","content":"keep input"}]}`)
				if route.responsesShape {
					body = []byte(`{"model":"gpt-5.6-sol","stream":false,"instructions":"keep instructions","prompt_cache_options":{"mode":"explicit","ttl":"30m"},"input":[{"type":"message","role":"user","content":"keep input"}]}`)
				}
				account := promptCacheOptionsTestAccount(route.oauth, false)
				if route.raw {
					account.Extra[openai_compat.ExtraKeyResponsesSupported] = false
				}
				upstream := promptCacheOptionsTestUpstream(outcome.reject, outcome.failLast, true, route.raw)
				c := newOpenAIRejectedFieldTestContext(body)
				c.Request.URL.Path = "/v1/chat/completions"

				result, err := newOpenAIRejectedFieldTestService(upstream).ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				if outcome.failLast {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.NotNil(t, result)
				}
				assertPromptCacheOptionsAttempts(t, upstream, outcome.reject)
				wantPath := "/v1/responses"
				if route.oauth {
					wantPath = "/backend-api/codex/responses"
				}
				if route.raw {
					wantPath = "/v1/chat/completions"
				}
				require.Equal(t, wantPath, upstream.lastReq.URL.Path)
			})
		}
	}
}

func TestPromptCacheOptionsChatPreservesUnrelatedError(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%v", raw), func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.6-sol","stream":false,"prompt_cache_options":{"mode":"explicit","ttl":"invalid"},"messages":[{"role":"user","content":"hi"}]}`)
			account := promptCacheOptionsTestAccount(false, false)
			account.Extra[openai_compat.ExtraKeyResponsesSupported] = !raw
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"invalid_parameter","param":"prompt_cache_options.ttl","message":"Invalid prompt_cache_options.ttl"}}`),
			}}
			c := newOpenAIRejectedFieldTestContext(body)
			c.Request.URL.Path = "/v1/chat/completions"
			_, err := newOpenAIRejectedFieldTestService(upstream).ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			require.Error(t, err)
			assertPromptCacheOptionsAttempts(t, upstream, false)
			require.Equal(t, http.StatusBadRequest, c.Writer.Status())
		})
	}
}

func promptCacheOptionsTestAccount(oauth, passthrough bool) *Account {
	account := newOpenAIRejectedFieldTestAccount()
	if oauth {
		account = newOpenAIOAuthNamespaceTestAccount()
		account.Extra = make(map[string]any)
	}
	account.Extra["openai_passthrough"] = passthrough
	account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeOff
	return account
}

func promptCacheOptionsTestUpstream(reject, failLast, chat, raw bool) *httpUpstreamRecorder {
	upstream := &httpUpstreamRecorder{}
	if reject {
		upstream.responses = append(upstream.responses, newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, promptCacheOptionsModelRejection))
	}
	if failLast {
		upstream.responses = append(upstream.responses, newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, promptCacheOptionsModelRejection))
		return upstream
	}
	response := newOpenAIRejectedFieldTestResponse(http.StatusOK, namespaceForwardOKResponse)
	if chat {
		if raw {
			response = newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"id":"chatcmpl-cache","object":"chat.completion","model":"gpt-5.6-sol","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
		} else {
			response.Header.Set("Content-Type", "text/event-stream")
			response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_cache\",\"model\":\"gpt-5.6-sol\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n"))
		}
	}
	upstream.responses = append(upstream.responses, response)
	return upstream
}

func assertPromptCacheOptionsAttempts(t *testing.T, upstream *httpUpstreamRecorder, retried bool) {
	t.Helper()
	wantAttempts := 1
	if retried {
		wantAttempts = 2
	}
	require.Len(t, upstream.bodies, wantAttempts)
	require.True(t, gjson.GetBytes(upstream.bodies[0], "prompt_cache_options").Exists(), "first attempt must preserve explicit cache options")
	if !retried {
		return
	}
	require.False(t, gjson.GetBytes(upstream.bodies[1], "prompt_cache_options").Exists())
	expected, err := sjson.DeleteBytes(upstream.bodies[0], "prompt_cache_options")
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(upstream.bodies[1]), "retry must preserve all other request fields")
	require.Equal(t, upstream.requests[0].Header.Get("Authorization"), upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, upstream.requests[0].Header.Get("session_id"), upstream.requests[1].Header.Get("session_id"))
}
