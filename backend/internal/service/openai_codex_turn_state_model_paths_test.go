package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Exercise the real passthrough handshake, after the client model has already
// been mapped, rather than only testing the shared model-matching helper.
func TestAutomaticCodexTurnStateWSPassthroughModelPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scope := range []string{"friendly-codex", "gpt-5.1", "unrelated-model"} {
		t.Run(scope, func(t *testing.T) {
			settings, _ := turnStateTestSettings("auto-path-state", scope)
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.OAuthEnabled = true
			upstream := &openAIWSCaptureConn{events: [][]byte{
				[]byte(`{"type":"response.completed","response":{"id":"resp_state_scope","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
			}}
			dialer := &openAIWSCaptureDialer{conn: upstream}
			svc := newPassthroughLifecycleService(cfg, nil)
			svc.settingService = settings
			svc.openaiWSPassthroughDialer = dialer
			account := passthroughLifecycleAccount()
			account.Type = AccountTypeOAuth
			account.Credentials = map[string]any{"access_token": "test-token"}
			account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
			seedAutomaticTurnState(account, "auto-path-state")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(*gin.Context) *OpenAIWSIngressHooks {
				return &OpenAIWSIngressHooks{MapRequestModel: func(_ int, model string) (string, error) {
					if model == "friendly-codex" {
						return "gpt-5.1", nil
					}
					return model, nil
				}}
			})
			defer server.Close()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &coderws.DialOptions{
				HTTPHeader: http.Header{},
			})
			require.NoError(t, err)
			defer client.CloseNow()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"friendly-codex","input":[]}`)))
			_, event, err := client.Read(ctx)
			require.NoError(t, err)
			require.Equal(t, "resp_state_scope", gjson.GetBytes(event, "response.id").String())
			_ = client.Close(coderws.StatusNormalClosure, "done")
			select {
			case <-serverErr:
			case <-ctx.Done():
				t.Fatal("passthrough did not finish")
			}
			want := "auto-path-state"
			if scope == "unrelated-model" {
				want = ""
			}
			require.Equal(t, want, dialer.lastHeaders.Get(openAICodexTurnStateHeader))
			require.Len(t, upstream.writes, 1)
			require.Equal(t, "gpt-5.1", gjson.Get(requestToJSONString(upstream.writes[0]), "model").String())
		})
	}
}

// The Responses bridge has a text-driver model at the top level, so model
// scope must also see the image model before and after channel/account mapping.
func TestAutomaticCodexTurnStateImagesResponsesModelPaths(t *testing.T) {
	for _, scope := range []string{"gpt-image-original", "gpt-image-channel", "gpt-image-1", "unrelated-model"} {
		t.Run(scope, func(t *testing.T) {
			settings, _ := turnStateTestSettings("auto-image-state", scope)
			body := []byte(`{"model":"gpt-image-original","prompt":"draw","response_format":"b64_json"}`)
			c, _ := newOpenAIImagesTestContext(t, body)

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader("data: " +
					`{"type":"response.completed","response":{"id":"resp_image_scope","output":[{"type":"image_generation_call","result":"aGVsbG8="}]}}` + "\n\ndata: [DONE]\n\n")),
			}}
			svc := newOpenAIImagesTestService(upstream)
			svc.settingService = settings
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := directImagesTestAccount()
			seedAutomaticTurnState(account, "auto-image-state")
			account.Credentials["model_mapping"] = map[string]any{"gpt-image-channel": "gpt-image-1"}
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "gpt-image-channel")
			require.NoError(t, err)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, chatgptCodexURL, upstream.lastReq.URL.String())
			require.Equal(t, "gpt-image-1", gjson.GetBytes(upstream.lastBody, "tools.0.model").String())
			want := "auto-image-state"
			if scope == "unrelated-model" {
				want = ""
			}
			require.Equal(t, want, upstream.lastReq.Header.Get(openAICodexTurnStateHeader))
			require.Empty(t, c.Request.Header.Get(openAICodexTurnStateHeader))
		})
	}
}
