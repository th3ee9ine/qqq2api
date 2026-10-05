package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/tidwall/gjson"
)

func TestProxyOpenAIWSHTTPBridgeTurnPromptCacheOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, reject := range []bool{false, true} {
			name := accountType + "/supported"
			if reject {
				name = accountType + "/rejected_then_success"
			}
			t.Run(name, func(t *testing.T) {
				upstream := &httpUpstreamSequenceRecorder{}
				if reject {
					upstream.responses = append(upstream.responses, &http.Response{
						StatusCode: http.StatusBadRequest,
						Header:     make(http.Header),
						Body: io.NopCloser(strings.NewReader(
							`{"error":{"type":"invalid_request_error","message":"prompt_cache_options is not supported on this model"}}`,
						)),
					})
				}
				upstream.responses = append(upstream.responses, &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader(
						"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_cache_options\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
					)),
				})
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				account := &Account{ID: 9191, Platform: PlatformOpenAI, Type: accountType, Concurrency: 1}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
				payload := []byte(`{"type":"response.create","model":"gpt-5.4","input":[{"role":"user","content":"hello"}],"prompt_cache_options":{"retention":"24h"},"prompt_cache_key":"keep-cache-key"}`)
				var writes [][]byte
				result, err := svc.proxyOpenAIWSHTTPBridgeTurn(
					context.Background(), c, account, "test-token", payload, len(payload),
					"gpt-5.4", "", "", "", "", 1,
					func(message []byte) error {
						writes = append(writes, append([]byte(nil), message...))
						return nil
					},
				)
				require.NoError(t, err)
				require.NotNil(t, result)
				expectedCalls := 1
				if reject {
					expectedCalls = 2
				}
				require.Equal(t, expectedCalls, upstream.callCount)
				require.Len(t, upstream.bodies, expectedCalls)
				require.JSONEq(t, `{"retention":"24h"}`, gjson.GetBytes(upstream.bodies[0], "prompt_cache_options").Raw)
				if reject {
					require.False(t, gjson.GetBytes(upstream.bodies[1], "prompt_cache_options").Exists())
					require.JSONEq(t, gjson.GetBytes(upstream.bodies[0], "input").Raw, gjson.GetBytes(upstream.bodies[1], "input").Raw)
				}
				for _, body := range upstream.bodies {
					require.Equal(t, "keep-cache-key", gjson.GetBytes(body, "prompt_cache_key").String())
				}
				require.Len(t, writes, 1, "the rejected attempt must not reach the websocket client")
				require.Equal(t, "response.completed", gjson.GetBytes(writes[0], "type").String())
			})
		}
	}
}
