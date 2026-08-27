package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai"
)

func TestCopyOpenAICodexResidencyHeader(t *testing.T) {
	t.Run("preserves the native value", func(t *testing.T) {
		src := make(http.Header)
		src.Set(openAICodexResidencyHeader, "US")
		dst := make(http.Header)

		copyOpenAICodexResidencyHeader(dst, src)

		require.Equal(t, "us", dst.Get(openAICodexResidencyHeader))
	})

	t.Run("drops values the native client does not emit", func(t *testing.T) {
		src := make(http.Header)
		src.Set(openAICodexResidencyHeader, "eu")
		dst := make(http.Header)
		dst.Set(openAICodexResidencyHeader, "stale")

		copyOpenAICodexResidencyHeader(dst, src)

		require.Empty(t, dst.Get(openAICodexResidencyHeader))
	})

	t.Run("drops ambiguous duplicates", func(t *testing.T) {
		src := http.Header{
			"X-Openai-Internal-Codex-Residency": {"us"},
			"x-openai-internal-codex-residency": {"us"},
		}
		dst := make(http.Header)

		copyOpenAICodexResidencyHeader(dst, src)

		require.Empty(t, dst.Get(openAICodexResidencyHeader))
	})
}

func TestCopyOpenAIResponsesTimingMetricsHeader(t *testing.T) {
	src := make(http.Header)
	src.Set(openAIResponsesTimingMetricsHeader, " true ")
	dst := make(http.Header)

	copyOpenAIResponsesTimingMetricsHeader(dst, src)

	require.Equal(t, "true", dst.Get(openAIResponsesTimingMetricsHeader))
}

func TestCopyOpenAIMemgenRequestHeaderRequiresNativeSubagentPair(t *testing.T) {
	t.Run("preserves the native pair", func(t *testing.T) {
		src := make(http.Header)
		src.Set(openAISubagentHeader, "memory_consolidation")
		src.Set(openAIMemgenRequestHeader, "true")
		dst := make(http.Header)

		copyOpenAIMemgenRequestHeader(dst, src)

		require.Equal(t, "true", dst.Get(openAIMemgenRequestHeader))
	})

	t.Run("drops a standalone memgen flag", func(t *testing.T) {
		src := make(http.Header)
		src.Set(openAISubagentHeader, "review")
		src.Set(openAIMemgenRequestHeader, "true")
		dst := make(http.Header)

		copyOpenAIMemgenRequestHeader(dst, src)

		require.Empty(t, dst.Get(openAIMemgenRequestHeader))
	})
}

func TestOpenAICodexNativeOptionalHeadersStayAlignedAcrossTransports(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("User-Agent", codexCLIUserAgent)
		c.Request.Header.Set(openAICodexResidencyHeader, "us")
		c.Request.Header.Set(openAIResponsesTimingMetricsHeader, "true")
		c.Request.Header.Set(openAISubagentHeader, "memory_consolidation")
		c.Request.Header.Set(openAIMemgenRequestHeader, "true")
		return c
	}
	account := &Account{
		ID:          9101,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-account",
		},
	}
	svc := &OpenAIGatewayService{}

	assertHTTPHeaders := func(t *testing.T, headers http.Header) {
		t.Helper()
		require.Equal(t, "us", headers.Get(openAICodexResidencyHeader))
		require.Equal(t, "memory_consolidation", headers.Get(openAISubagentHeader))
		require.Equal(t, "true", headers.Get(openAIMemgenRequestHeader))
		// Native Codex emits this flag only on the WebSocket handshake.
		require.Empty(t, headers.Get(openAIResponsesTimingMetricsHeader))
	}

	t.Run("normal HTTP", func(t *testing.T) {
		req, err := svc.buildUpstreamRequest(
			context.Background(), newContext(), account, []byte(`{"model":"gpt-5.6","stream":true}`),
			"oauth-token", true, "", true,
		)
		require.NoError(t, err)
		assertHTTPHeaders(t, req.Header)
	})

	t.Run("passthrough HTTP", func(t *testing.T) {
		req, err := svc.buildUpstreamRequestOpenAIPassthrough(
			context.Background(), newContext(), account, []byte(`{"model":"gpt-5.6","stream":true}`), "oauth-token",
		)
		require.NoError(t, err)
		assertHTTPHeaders(t, req.Header)
	})

	t.Run("WebSocket handshake", func(t *testing.T) {
		headers, _, err := svc.buildOpenAIWSHeaders(
			context.Background(), newContext(), account, "oauth-token",
			OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
			true, "", "", "", "gpt-5.6", "",
		)
		require.NoError(t, err)
		require.Equal(t, "us", headers.Get(openAICodexResidencyHeader))
		require.Equal(t, "memory_consolidation", headers.Get(openAISubagentHeader))
		require.Equal(t, "true", headers.Get(openAIMemgenRequestHeader))
		require.Equal(t, "true", headers.Get(openAIResponsesTimingMetricsHeader))
		require.Equal(t, openai.CodexDefaultOriginator, headers.Get("originator"))
	})
}
