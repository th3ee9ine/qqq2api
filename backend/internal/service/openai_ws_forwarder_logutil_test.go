package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResolveOpenAIWSSessionHeadersPrefersCodexHyphenHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("session-id", "codex-session")
	c.Request.Header.Set("session_id", "legacy-session")

	resolution := resolveOpenAIWSSessionHeaders(c, "prompt-cache")

	require.Equal(t, "codex-session", resolution.SessionID)
	require.Equal(t, "header_session-id", resolution.SessionSource)
}

func TestResolveOpenAIWSSessionHeadersFallsBackToLegacyHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("session_id", "legacy-session")

	resolution := resolveOpenAIWSSessionHeaders(c, "prompt-cache")

	require.Equal(t, "legacy-session", resolution.SessionID)
	require.Equal(t, "header_session_id", resolution.SessionSource)
}

func TestOpenAIWSHeaderValueForLogRedactsSessionIdentity(t *testing.T) {
	headers := make(http.Header)
	headers.Set("session_id", "sensitive-legacy-session")
	headers.Set("session-id", "sensitive-codex-session")
	headers.Set("conversation_id", "sensitive-conversation")
	headers.Set("user-agent", "codex-test/1.0")

	for _, key := range []string{"session_id", "session-id", "conversation_id", "SESSION_ID"} {
		t.Run(key, func(t *testing.T) {
			logged := openAIWSHeaderValueForLog(headers, key)
			require.Equal(t, "[redacted]", logged)
			require.NotContains(t, logged, "sensitive")
		})
	}
	require.Equal(t, "codex-test/1.0", openAIWSHeaderValueForLog(headers, "user-agent"))
	require.Equal(t, "-", openAIWSHeaderValueForLog(headers, "missing"))
	require.Equal(t, "-", openAIWSHeaderValueForLog(nil, "session_id"))
	headers.Set("session_id", "  ")
	require.Equal(t, "-", openAIWSHeaderValueForLog(headers, "session_id"))
}
