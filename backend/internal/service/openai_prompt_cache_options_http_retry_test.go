package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptCacheOptionsHTTPRetryClosesResponseAndRebuildsRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-5.4","prompt_cache_options":{"mode":"explicit"},"messages":[{"role":"user","content":"keep"}]}`)
	closed := &passthroughCloseTrackingReadCloser{Reader: strings.NewReader(promptCacheOptionsModelRejection)}
	firstResponse := newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, "")
	firstResponse.Body = closed
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		firstResponse,
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"choices":[]}`),
	}}
	c := newOpenAIRejectedFieldTestContext(body)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://compat.example/v1/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer test")
	request.Header.Set("X-Custom-Header", "preserve")
	response, err := newOpenAIRejectedFieldTestService(upstream).doOpenAIUpstreamWithPromptCacheOptionsRetry(context.Background(), c, promptCacheOptionsTestAccount(false, false), request, "http://proxy.example", body)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.True(t, closed.closed, "rejected upstream response must release its transport body")
	assertPromptCacheOptionsAttempts(t, upstream, true)
	require.Equal(t, int64(len(upstream.bodies[1])), upstream.requests[1].ContentLength)
	replay, err := upstream.requests[1].GetBody()
	require.NoError(t, err)
	defer replay.Close()
	replayBody, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, upstream.bodies[1], replayBody)
	require.Equal(t, "preserve", upstream.requests[1].Header.Get("X-Custom-Header"))
	require.Equal(t, "http://proxy.example", upstream.lastProxyURL)
}

func TestPromptCacheOptionsHTTPRetrySafetyGuards(t *testing.T) {
	for _, testCase := range []string{"other platform", "committed response", "canceled before error", "canceled while reading error", "exhausted request retry budget"} {
		t.Run(testCase, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.6-sol","prompt_cache_options":{"mode":"explicit"},"messages":[{"role":"user","content":"keep"}]}`)
			account := promptCacheOptionsTestAccount(false, false)
			c := newOpenAIRejectedFieldTestContext(body)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			firstResponse := newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, promptCacheOptionsModelRejection)
			switch testCase {
			case "other platform":
				account.Platform = PlatformGrok
			case "committed response":
				c.Writer.WriteHeader(http.StatusOK)
				_, err := c.Writer.WriteString("already written")
				require.NoError(t, err)
			case "canceled before error":
				cancel()
			case "canceled while reading error":
				firstResponse.Body = io.NopCloser(&promptCacheOptionsCancelReader{Reader: strings.NewReader(promptCacheOptionsModelRejection), cancel: cancel})
			case "exhausted request retry budget":
				state := openAIResponsesRejectedFieldRetryStateForRequest(c, body)
				for i := 0; i < maxOpenAIResponsesRejectedFieldRetries; i++ {
					require.True(t, state.Allow([]byte(fmt.Sprintf(`{"attempt":%d}`, i))))
				}
			}
			upstream := &httpUpstreamRecorder{responses: []*http.Response{firstResponse}}
			// Actual upstream requests are detached from inbound cancellation. The
			// compatibility retry must still respect the original caller context.
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://compat.example/v1/chat/completions", bytes.NewReader(body))
			require.NoError(t, err)
			response, err := newOpenAIRejectedFieldTestService(upstream).doOpenAIUpstreamWithPromptCacheOptionsRetry(ctx, c, account, request, "", body)
			require.NoError(t, err)
			require.NotNil(t, response)
			defer response.Body.Close()
			require.Equal(t, http.StatusBadRequest, response.StatusCode)
			assertPromptCacheOptionsAttempts(t, upstream, false)
			returnedBody, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.JSONEq(t, promptCacheOptionsModelRejection, string(returnedBody), "non-retried error must remain readable by the normal error handler")
		})
	}
}

type promptCacheOptionsCancelReader struct {
	io.Reader
	cancel context.CancelFunc
}

func (r *promptCacheOptionsCancelReader) Read(p []byte) (int, error) {
	r.cancel()
	return r.Reader.Read(p)
}
