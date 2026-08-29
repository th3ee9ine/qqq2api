package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationResponsesProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/responses", r.URL.Path)
		require.Equal(t, "Bearer sk-responses", r.Header.Get("Authorization"))
		var payload responsesModerationAPIRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "gpt-5.6-sol", payload.Model)
		require.Contains(t, payload.Instructions, "category_scores")
		require.Equal(t, float64(0), payload.Temperature)
		require.Equal(t, 256, payload.MaxOutputTokens)
		require.False(t, payload.Store)
		require.Equal(t, "hello", payload.Input)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"flagged\":false,\"category_scores\":{\"harassment\":0,\"violence\":0}}"}]}]}`))
	}))
	defer server.Close()

	cfg := defaultContentModerationConfig()
	cfg.BaseURL = server.URL
	cfg.Model = "gpt-5.6-sol"
	cfg.Protocol = ContentModerationEndpointProtocolResponses
	cfg.TimeoutMS = 1000

	status := 0
	result, err := (&ContentModerationService{}).callModerationOnceWithInput(context.Background(), cfg, "sk-responses", "hello", &status)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.False(t, result.Flagged)
	require.Equal(t, 0.0, result.CategoryScores["violence"])
}

func TestResponsesModerationInputConvertsMultimodalParts(t *testing.T) {
	input := []moderationAPIInputPart{
		{Type: "text", Text: "describe this"},
		{Type: "image_url", ImageURL: &moderationAPIImageURLRef{URL: "data:image/png;base64,AAA"}},
	}
	converted, ok := responsesModerationInput(input).([]responsesModerationInputMessage)
	require.True(t, ok)
	require.Len(t, converted, 1)
	require.Equal(t, "message", converted[0].Type)
	require.Equal(t, "user", converted[0].Role)
	require.Len(t, converted[0].Content, 2)
	require.Equal(t, "input_text", converted[0].Content[0].Type)
	require.Equal(t, "describe this", converted[0].Content[0].Text)
	require.Equal(t, "input_image", converted[0].Content[1].Type)
	require.Equal(t, "data:image/png;base64,AAA", converted[0].Content[1].ImageURL)
}

func TestExtractResponsesModerationContentSupportsOutputText(t *testing.T) {
	content, err := extractResponsesModerationContent([]byte(`{"output_text":"{\"flagged\":true,\"category_scores\":{\"violence\":0.99}}"}`))
	require.NoError(t, err)
	result, err := parseChatModerationResult(content)
	require.NoError(t, err)
	require.True(t, result.Flagged)
	require.Equal(t, 0.99, result.CategoryScores["violence"])
}

func TestExtractResponsesModerationContentSupportsGatewayOutputVariants(t *testing.T) {
	for _, body := range []string{
		`{"output":{"type":"message","content":{"type":"output_text","text":"{\"flagged\":false,\"category_scores\":{\"violence\":0}}"}}}`,
		`{"output":["{\"flagged\":false,\"category_scores\":{\"violence\":0}}"]}`,
		`{"choices":[{"message":{"content":"{\"flagged\":false,\"category_scores\":{\"violence\":0}}"}}]}`,
	} {
		content, err := extractResponsesModerationContent([]byte(body))
		require.NoError(t, err)
		result, err := parseChatModerationResult(content)
		require.NoError(t, err)
		require.False(t, result.Flagged)
		require.Equal(t, 0.0, result.CategoryScores["violence"])
	}
}

func TestContentModerationUpdateConfigPersistsResponsesProtocol(t *testing.T) {
	initial := defaultContentModerationConfig()
	raw, err := json.Marshal(initial)
	require.NoError(t, err)
	repo := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(raw),
	}}
	svc := NewContentModerationService(repo, nil, nil, nil, nil, nil, nil, nil)
	protocol := "/v1/responses"

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{Protocol: &protocol})
	require.NoError(t, err)
	require.Equal(t, ContentModerationEndpointProtocolResponses, view.Protocol)

	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyContentModerationConfig]), &saved))
	require.Equal(t, ContentModerationEndpointProtocolResponses, saved.Protocol)
}
