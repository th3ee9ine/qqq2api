package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationChatCompletionsProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer sk-chat", r.Header.Get("Authorization"))
		var payload chatModerationAPIRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "gpt-5.6-sol", payload.Model)
		require.Equal(t, float64(0), payload.Temperature)
		require.Equal(t, 256, payload.MaxTokens)
		require.Len(t, payload.Messages, 2)
		require.Equal(t, "system", payload.Messages[0].Role)
		require.Contains(t, payload.Messages[0].Content, "category_scores")
		require.Equal(t, "user", payload.Messages[1].Role)
		require.Equal(t, "hello", payload.Messages[1].Content)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"flagged\":false,\"category_scores\":{\"harassment\":0,\"violence\":0}}"}}]}`))
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.Model = "gpt-5.6-sol"
	cfg.Protocol = ContentModerationEndpointProtocolChatCompletions
	cfg.TimeoutMS = 1000

	status := 0
	result, err := (&ContentModerationService{}).callModerationOnceWithInput(context.Background(), cfg, "sk-chat", "hello", &status)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.False(t, result.Flagged)
	require.Equal(t, 0.0, result.CategoryScores["violence"])
}

func TestParseChatModerationResultSupportsCategoriesAndCodeFence(t *testing.T) {
	result, err := parseChatModerationResult("```json\n{\"flagged\":true,\"categories\":[\"violence\",\"illicit/violent\"]}\n```")
	require.NoError(t, err)
	require.True(t, result.Flagged)
	require.Equal(t, 1.0, result.CategoryScores["violence"])
	require.Equal(t, 1.0, result.CategoryScores["illicit/violent"])
}

func TestNormalizeContentModerationEndpointProtocol(t *testing.T) {
	for _, value := range []string{"", "moderations", "openai_moderations", "/v1/moderations"} {
		require.Equal(t, ContentModerationEndpointProtocolModerations, normalizeContentModerationEndpointProtocol(value))
	}
	for _, value := range []string{"chat_completions", "openai_chat_completions", "chat", "/v1/chat/completions"} {
		require.Equal(t, ContentModerationEndpointProtocolChatCompletions, normalizeContentModerationEndpointProtocol(value))
	}
	for _, value := range []string{"responses", "openai_responses", "response", "/v1/responses"} {
		require.Equal(t, ContentModerationEndpointProtocolResponses, normalizeContentModerationEndpointProtocol(value))
	}
	cfg := defaultContentModerationConfig()
	cfg.Protocol = "unsupported"
	cfg.normalize()
	require.Error(t, validateContentModerationEndpointProtocol(cfg.Protocol))
}
