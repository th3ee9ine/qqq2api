package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestProxyResponsesWebSocketFromClientPromptCacheOptionsCtxPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, reject := range []bool{false, true} {
			name := accountType + "/supported"
			if reject {
				name = accountType + "/rejected_then_success"
			}
			t.Run(name, func(t *testing.T) {
				cfg := newOpenAIWSExecutionScopeTestConfig()
				upstream := &openAIWSCaptureConn{}
				if reject {
					upstream.events = append(upstream.events, []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"prompt_cache_options is not supported on this model"}}`))
				}
				upstream.events = append(upstream.events, []byte(`{"type":"response.completed","response":{"id":"resp_cache_options","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`))
				dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{upstream}}
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(dialer)
				defer pool.Close()
				svc := &OpenAIGatewayService{
					cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
					openaiWSResolver: NewOpenAIWSProtocolResolver(cfg),
					toolCorrector:    NewCodexToolCorrector(), openaiWSPool: pool,
				}
				account := &Account{
					ID: 9192, Platform: PlatformOpenAI, Type: accountType,
					Status: StatusActive, Schedulable: true, Concurrency: 1,
					Credentials: map[string]any{"api_key": "test-token", "access_token": "test-token"},
					Extra: map[string]any{
						"openai_apikey_responses_websockets_v2_enabled": true,
						"openai_apikey_responses_websockets_v2_mode":    OpenAIWSIngressModeCtxPool,
						"openai_oauth_responses_websockets_v2_enabled":  true,
						"openai_oauth_responses_websockets_v2_mode":     OpenAIWSIngressModeCtxPool,
					},
				}
				serverErrCh := make(chan error, 1)
				wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						serverErrCh <- err
						return
					}
					defer conn.CloseNow()
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = r
					readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
					_, firstMessage, err := conn.Read(readCtx)
					cancel()
					if err != nil {
						serverErrCh <- err
						return
					}
					serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, account, "test-token", firstMessage, nil)
				}))
				defer wsServer.Close()
				client := dialOpenAIWSExecutionScopeClient(t, wsServer.URL, "cache-options")
				defer client.CloseNow()
				writeOpenAIWSExecutionScopeRequest(t, client, `{"type":"response.create","model":"gpt-5.4","input":[{"role":"user","content":"hello"}],"prompt_cache_options":{"retention":"24h"},"prompt_cache_key":"keep-cache-key"}`)
				readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, message, err := client.Read(readCtx)
				cancel()
				require.NoError(t, err)
				require.Equal(t, "response.completed", gjson.GetBytes(message, "type").String(), "the rejected attempt must not reach the client")
				require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
				select {
				case err := <-serverErrCh:
					require.NoError(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("websocket ingress did not finish")
				}
				upstream.mu.Lock()
				defer upstream.mu.Unlock()
				expectedCalls := 1
				if reject {
					expectedCalls = 2
				}
				require.Len(t, upstream.writes, expectedCalls)
				require.Equal(t, map[string]any{"retention": "24h"}, upstream.writes[0]["prompt_cache_options"])
				if reject {
					require.NotContains(t, upstream.writes[1], "prompt_cache_options")
					require.Equal(t, upstream.writes[0]["input"], upstream.writes[1]["input"])
				}
				for _, payload := range upstream.writes {
					require.Equal(t, "keep-cache-key", payload["prompt_cache_key"])
				}
				require.Equal(t, 1, dialer.DialCount(), "the compatibility retry should reuse the current upstream connection")
			})
		}
	}
}
