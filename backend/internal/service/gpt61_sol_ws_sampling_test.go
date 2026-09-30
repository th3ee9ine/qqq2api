package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type gpt61SolSamplingWSConn struct {
	*stagedPassthroughConn
}

func (c *gpt61SolSamplingWSConn) WriteJSON(ctx context.Context, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.WriteFrame(ctx, coderws.MessageText, payload)
}

func TestGPT61SolAPIKeyWebSocketSamplingUsesFinalModelEachTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		for _, tc := range []struct {
			name         string
			requested    string
			upstream     string
			keepSampling bool
			reasoning    string
		}{
			{name: "direct inherited model", requested: "gpt-6.1-sol", upstream: "gpt-6.1-sol", reasoning: "max"},
			{name: "mapped to GPT61", requested: "gpt-4o", upstream: "gpt-6.1-sol", reasoning: "max"},
			{name: "mapped away from GPT61", requested: "gpt-6.1-sol", upstream: "gpt-4o", keepSampling: true, reasoning: "high"},
			{name: "GPT6 none still permits sampling", requested: "gpt-6-sol", upstream: "gpt-6-sol", keepSampling: true, reasoning: "none"},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				controlCtx, cancelControl := context.WithCancelCause(context.Background())
				defer cancelControl(context.Canceled)
				upstream := &gpt61SolSamplingWSConn{newStagedPassthroughConn()}
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
				cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
				cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
				svc := newPassthroughLifecycleService(cfg, upstream.stagedPassthroughConn)
				account := passthroughLifecycleAccount()
				account.Extra["openai_apikey_responses_websockets_v2_mode"] = mode
				hooks := &OpenAIWSIngressHooks{}
				if mode == OpenAIWSIngressModeCtxPool {
					account.Credentials["model_mapping"] = map[string]any{tc.requested: tc.upstream}
					pool := newOpenAIWSConnPool(cfg)
					pool.setClientDialerForTest(&stagedPassthroughDialer{conn: upstream})
					svc.openaiWSPool = pool
					defer pool.Close()
				} else if tc.requested != tc.upstream {
					// Passthrough preserves ordinary account mappings; routing hooks
					// can still resolve a public model to its actual upstream model.
					hooks.MapRequestModel = func(_ int, _ string) (string, error) {
						return tc.upstream, nil
					}
				}
				server, serverErr := startPassthroughHookRecordingServer(t, controlCtx, svc, account, hooks)
				defer server.Close()
				dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
				client, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
				cancelDial()
				require.NoError(t, err)
				defer func() { _ = client.CloseNow() }()

				for turn := 1; turn <= 2; turn++ {
					modelField := ""
					if turn == 1 {
						modelField = fmt.Sprintf(`,"model":%q`, tc.requested)
					}
					payload := fmt.Sprintf(`{"type":"response.create"%s,"input":[{"role":"user","content":[{"type":"input_text","text":"hello"}]}],"reasoning":{"effort":%q},"temperature":0.3,"top_p":0.7,"top_logprobs":3,"logprobs":true,"include":["message.output_text.logprobs","reasoning.encrypted_content"]}`, modelField, tc.reasoning)
					writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
					err = client.Write(writeCtx, coderws.MessageText, []byte(payload))
					cancelWrite()
					require.NoError(t, err)
					forwarded := requirePassthroughUpstreamWrite(t, upstream.stagedPassthroughConn, 3*time.Second)
					require.Equal(t, tc.upstream, gjson.GetBytes(forwarded, "model").String())
					require.Equal(t, tc.reasoning, gjson.GetBytes(forwarded, "reasoning.effort").String())
					for _, field := range []string{"temperature", "top_p", "top_logprobs", "logprobs"} {
						require.Equal(t, tc.keepSampling, gjson.GetBytes(forwarded, field).Exists(), "turn %d field %s", turn, field)
					}
					require.Equal(t, tc.keepSampling, strings.Contains(gjson.GetBytes(forwarded, "include").Raw, "message.output_text.logprobs"))
					require.Contains(t, gjson.GetBytes(forwarded, "include").Raw, "reasoning.encrypted_content")
					upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_sampling_%d","model":%q,"usage":{"input_tokens":1,"output_tokens":1}}}`, turn, tc.upstream))
					completed, readErr := readPassthroughLifecycleFrame(t, client, 3*time.Second)
					require.NoError(t, readErr)
					require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
				}
				_ = client.Close(coderws.StatusNormalClosure, "done")
				select {
				case err := <-serverErr:
					require.NoError(t, err)
				case <-time.After(3 * time.Second):
					t.Fatal("websocket sampling test did not exit")
				}
			})
		}
	}
}
