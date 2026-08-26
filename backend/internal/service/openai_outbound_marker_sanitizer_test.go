//go:build unit

package service

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeLegacyOpenAIOutboundJSONRewritesRetiredMarkers(t *testing.T) {
	body := []byte(`{"instructions":"<sub2api-codex-image-generation>x</sub2api-codex-image-generation>","input":[{"content":"<sub2api-claude-code-todo-guard>todo</sub2api-claude-code-todo-guard>"}],"tools":[{"name":"python__sub2api"}]}`)

	normalized, changed := normalizeLegacyOpenAIOutboundJSON(body)

	require.True(t, changed)
	require.NotContains(t, strings.ToLower(string(normalized)), "sub2api")
	require.Contains(t, string(normalized), codexImageGenerationBridgeMarker)
	require.Contains(t, string(normalized), openAICompatClaudeCodeTodoGuardMarker)
	require.Contains(t, string(normalized), `"`+codexPythonToolAlias+`"`)
}

func TestNormalizeLegacyOpenAIOutboundRequestBodyRebuildsReplayableBody(t *testing.T) {
	body := []byte(`{"input":"<sub2api-codex-spark-image-unsupported>x</sub2api-codex-spark-image-unsupported>"}`)
	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/responses", bytes.NewReader(body))
	require.NoError(t, err)

	normalizeLegacyOpenAIOutboundRequestBody(req)

	actual, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(actual)), "sub2api")
	require.Equal(t, int64(len(actual)), req.ContentLength)
	replay, err := req.GetBody()
	require.NoError(t, err)
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, actual, replayed)
}

func TestNormalizeOpenAIResponsesWebSocketCompatibilityBodyRemovesLegacyMarkersForEveryAccountType(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"type":"message","role":"user","content":"<sub2api-claude-code-todo-guard>x</sub2api-claude-code-todo-guard>"}],"tools":[{"type":"function","name":"python__sub2api","parameters":{"type":"object"}}]}`)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		normalized, changed, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, &Account{Platform: PlatformOpenAI, Type: accountType}, false)
		require.NoError(t, err)
		require.True(t, changed)
		require.NotContains(t, strings.ToLower(string(normalized)), "sub2api")
	}
}
