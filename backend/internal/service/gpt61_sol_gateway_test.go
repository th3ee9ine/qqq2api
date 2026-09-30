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
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/tidwall/gjson"
)

func TestGPT61SolResponsesForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, passthrough := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				for _, model := range []string{"gpt-6.1-sol", "gpt-6.1-sol-high"} {
					t.Run(fmt.Sprintf("%s/passthrough=%t/stream=%t/%s", accountType, passthrough, stream, model), func(t *testing.T) {
						body := []byte(fmt.Sprintf(`{"model":%q,"stream":%t,"instructions":"test","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"reasoning":{"effort":"max"},"service_tier":"priority","temperature":0.3,"top_p":0.7,"top_logprobs":3,"include":["message.output_text.logprobs","reasoning.encrypted_content"]}`, model, stream))
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
						c.Request.Header.Set("Content-Type", "application/json")
						c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
						account := rawGPT56ResponsesAPIKeyAccount(model, model)
						wantURL := "https://api.example.com/v1/responses"
						if accountType == AccountTypeOAuth {
							account = rawGPT56ResponsesOAuthAccount(model, model)
							wantURL = "https://chatgpt.com/backend-api/codex/responses"
						}
						account.Extra = map[string]any{"openai_passthrough": passthrough}
						upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_gpt61", model)}
						if accountType == AccountTypeAPIKey && !stream {
							upstream.resp = &http.Response{
								StatusCode: http.StatusOK,
								Header:     http.Header{"Content-Type": []string{"application/json"}},
								Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"resp_gpt61","object":"response","status":"completed","model":%q,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`, model))),
							}
						}
						svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
						result, err := svc.Forward(context.Background(), c, account, body)
						require.NoError(t, err)
						require.NotNil(t, result)
						require.Equal(t, http.StatusOK, rec.Code)
						require.Equal(t, wantURL, upstream.lastReq.URL.String())
						require.Equal(t, model, gjson.GetBytes(upstream.lastBody, "model").String())
						require.Equal(t, "max", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
						require.Equal(t, "priority", gjson.GetBytes(upstream.lastBody, "service_tier").String())
						for _, field := range []string{"temperature", "top_p", "top_logprobs"} {
							require.False(t, gjson.GetBytes(upstream.lastBody, field).Exists(), field)
						}
						require.NotContains(t, gjson.GetBytes(upstream.lastBody, "include").Raw, "message.output_text.logprobs")
						require.Contains(t, gjson.GetBytes(upstream.lastBody, "include").Raw, "reasoning.encrypted_content")
						if !stream && !(accountType == AccountTypeOAuth && passthrough) {
							require.Equal(t, model, gjson.GetBytes(rec.Body.Bytes(), "model").String())
						} else {
							require.Contains(t, rec.Body.String(), `"model":"`+model+`"`)
						}
					})
				}
			}
		}
	}
}

func TestGPT61SolChatAndAnthropicForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		for _, protocol := range []string{"chat", "anthropic"} {
			t.Run(accountType+"/"+protocol, func(t *testing.T) {
				body := []byte(`{"model":"gpt-6.1-sol","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"max","temperature":0.3,"top_p":0.7,"stream":false}`)
				path := "/v1/chat/completions"
				if protocol == "anthropic" {
					path = "/v1/messages"
					body = []byte(`{"model":"gpt-6.1-sol","max_tokens":64,"messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"max"},"temperature":0.3,"top_p":0.7,"stream":false}`)
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")
				account := rawGPT56ResponsesAPIKeyAccount("gpt-6.1-sol", "gpt-6.1-sol")
				if accountType == AccountTypeOAuth {
					account = rawGPT56ResponsesOAuthAccount("gpt-6.1-sol", "gpt-6.1-sol")
				}
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_gpt61_compat", "gpt-6.1-sol")}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				var result *OpenAIForwardResult
				var err error
				if protocol == "chat" {
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				} else {
					result, err = svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, "gpt-6.1-sol", result.UpstreamModel)
				require.Equal(t, "gpt-6.1-sol", result.BillingModel)
				require.NotNil(t, result.ReasoningEffort)
				require.Equal(t, "max", *result.ReasoningEffort)
				require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(t, "max", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
				require.False(t, gjson.GetBytes(upstream.lastBody, "temperature").Exists())
				require.False(t, gjson.GetBytes(upstream.lastBody, "top_p").Exists())
				require.Equal(t, http.StatusOK, rec.Code)
				require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(rec.Body.Bytes(), "model").String())
				if protocol == "chat" {
					require.Equal(t, "ok", gjson.GetBytes(rec.Body.Bytes(), "choices.0.message.content").String())
				} else {
					require.Equal(t, "ok", gjson.GetBytes(rec.Body.Bytes(), "content.0.text").String())
				}
			})
		}
	}
}

func TestGPT61SolResponseIdentityDoesNotCollapse(t *testing.T) {
	svc := &OpenAIGatewayService{}
	for _, model := range []string{"gpt-6.1-sol-high", "gpt-6.1-sol-openai-compact"} {
		t.Run(model, func(t *testing.T) {
			body := `{"id":"resp_gpt61","model":"` + model + `","output":[]}`
			require.Equal(t, body, string(svc.replaceModelInResponseBody([]byte(body), model, "gpt-6.1-sol")))
			sse := "data: " + body + "\n\ndata: [DONE]\n\n"
			require.Equal(t, sse, svc.replaceModelInSSEBody(sse, model, "gpt-6.1-sol"))
			require.True(t, codexTurnStateResponseModelMismatch(model, "gpt-6.1-sol", false))
			require.False(t, codexTurnStateProbeModelsMatch(model, []string{"gpt-6.1-sol"}))
		})
	}
	for _, older := range []string{"gpt-6-sol", "gpt-6-luna", "gpt-6-astra"} {
		require.True(t, codexTurnStateResponseModelMismatch("gpt-6.1-sol", older, false))
		require.False(t, codexTurnStateProbeModelsMatch("gpt-6.1-sol", []string{older}))
	}
}

func TestGPT61SolResponsesSamplingAlwaysRemoved(t *testing.T) {
	for _, effort := range []string{"", "none", "high", "max"} {
		t.Run("effort="+effort, func(t *testing.T) {
			reasoning := ""
			if effort != "" {
				reasoning = `,"reasoning":{"effort":"` + effort + `"}`
			}
			body := []byte(`{"model":"gpt-6.1-sol","temperature":0.3,"top_p":0.7,"top_logprobs":3,"logprobs":true,"include":["message.output_text.logprobs","reasoning.encrypted_content"]` + reasoning + `}`)
			normalized, changed, err := normalizeOpenAIResponsesReasoningMode(body)
			require.NoError(t, err)
			require.True(t, changed)
			for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs"} {
				require.False(t, gjson.GetBytes(normalized, field).Exists(), field)
			}
			require.Equal(t, `["reasoning.encrypted_content"]`, gjson.GetBytes(normalized, "include").Raw)
			require.Equal(t, effort, gjson.GetBytes(normalized, "reasoning.effort").String(), "sampling cleanup must not silently rewrite reasoning effort")
		})
	}
}

func TestGPT61SolAPIKeySamplingUsesFinalWireModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, requested, mapped, wantModel string
		passthrough, wantSampling          bool
	}{
		{name: "native alias to GPT61", requested: "custom-sol", mapped: "gpt-6.1-sol", wantModel: "gpt-6.1-sol"},
		{name: "native alias to older model", requested: "gpt-6.1-sol", mapped: "gpt-5.5", wantModel: "gpt-5.5", wantSampling: true},
		{name: "passthrough preserves alias", requested: "custom-sol", mapped: "gpt-6.1-sol", wantModel: "custom-sol", passthrough: true, wantSampling: true},
		{name: "passthrough keeps GPT61", requested: "gpt-6.1-sol", mapped: "gpt-5.5", wantModel: "gpt-6.1-sol", passthrough: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":%q,"input":"hello","stream":false,"reasoning":{"effort":"high","mode":"pro"},"temperature":0.3,"top_p":0.7}`, tc.requested))
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
			account := rawGPT56ResponsesAPIKeyAccount(tc.requested, tc.mapped)
			account.Extra = map[string]any{"openai_passthrough": tc.passthrough}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(fmt.Sprintf(`{"id":"resp_gpt61_mapped","model":%q,"output":[],"usage":{"input_tokens":1,"output_tokens":1}}`, tc.wantModel))),
			}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			result, err := svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tc.wantModel, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, tc.wantSampling, gjson.GetBytes(upstream.lastBody, "temperature").Exists())
			require.Equal(t, tc.wantSampling, gjson.GetBytes(upstream.lastBody, "top_p").Exists())
			require.Equal(t, "pro", gjson.GetBytes(upstream.lastBody, "reasoning.mode").String(), "API-key sampling cleanup must not apply OAuth reasoning-mode conversion")
			require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
		})
	}
}
