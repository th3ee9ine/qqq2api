package service

import (
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
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai_compat"
	"github.com/tidwall/gjson"
)

func TestGPT61SolAnthropicEffortAliasesForwardDistinctLevels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		model      string
		upstream   string
		explicit   string
		wantEffort string
	}{
		{name: "high alias", model: "gpt-6.1-sol-high", upstream: "gpt-6.1-sol", wantEffort: "high"},
		{name: "xhigh alias", model: "gpt-6.1-sol-xhigh", upstream: "gpt-6.1-sol", wantEffort: "xhigh"},
		{name: "max alias", model: "gpt-6.1-sol-max", upstream: "gpt-6.1-sol", wantEffort: "max"},
		{name: "explicit max overrides xhigh", model: "gpt-6.1-sol-xhigh", upstream: "gpt-6.1-sol", explicit: "max", wantEffort: "max"},
		{name: "explicit xhigh overrides max", model: "gpt-6.1-sol-max", upstream: "gpt-6.1-sol", explicit: "xhigh", wantEffort: "xhigh"},
		{name: "explicit low overrides max", model: "gpt-6.1-sol-max", upstream: "gpt-6.1-sol", explicit: "low", wantEffort: "low"},
		{name: "legacy xhigh alias", model: "gpt-5.4-xhigh", upstream: "gpt-5.4", wantEffort: "xhigh"},
		{name: "legacy explicit max", model: "gpt-5.5", upstream: "gpt-5.5", explicit: "max", wantEffort: "xhigh"},
	}
	for _, protocol := range []string{"responses-api-key", "responses-oauth", "chat-api-key"} {
		for _, tc := range tests {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				effortJSON := ""
				if tc.explicit != "" {
					effortJSON = fmt.Sprintf(`,"output_config":{"effort":%q}`, tc.explicit)
				}
				body := fmt.Sprintf(`{"model":%q,"max_tokens":64,"messages":[{"role":"user","content":"hello"}],"stream":false%s}`, tc.model, effortJSON)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
				c.Request.Header.Set("Content-Type", "application/json")

				account := rawGPT56ResponsesAPIKeyAccount(tc.model, tc.upstream)
				upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_effort_alias", tc.upstream)}
				effortPath := "reasoning.effort"
				switch protocol {
				case "responses-oauth":
					account = rawGPT56ResponsesOAuthAccount(tc.model, tc.upstream)
				case "chat-api-key":
					account.Extra = map[string]any{
						openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
					}
					upstream.resp = &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body: io.NopCloser(strings.NewReader(fmt.Sprintf(
							`{"id":"chatcmpl_effort_alias","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`, tc.upstream))),
					}
					effortPath = "reasoning_effort"
				}
				svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
				result, err := svc.ForwardAsAnthropic(context.Background(), c, account, []byte(body), "", "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Equal(t, tc.upstream, gjson.GetBytes(upstream.lastBody, "model").String())
				require.Equal(t, tc.wantEffort, gjson.GetBytes(upstream.lastBody, effortPath).String())
				require.NotNil(t, result.ReasoningEffort)
				require.Equal(t, tc.wantEffort, *result.ReasoningEffort)
				require.Equal(t, http.StatusOK, rec.Code)
			})
		}
	}
}
