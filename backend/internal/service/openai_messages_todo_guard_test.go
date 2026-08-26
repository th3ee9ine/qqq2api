package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/stretchr/testify/require"
)

func TestAppendOpenAICompatClaudeCodeTodoGuard_UsesNeutralMarkerAndIsIdempotent(t *testing.T) {
	req := &apicompat.ResponsesRequest{
		Input: json.RawMessage(`[{"type":"message","role":"user","content":"hello"}]`),
	}

	require.True(t, appendOpenAICompatClaudeCodeTodoGuard(req))

	var items []apicompat.ResponsesInputItem
	require.NoError(t, json.Unmarshal(req.Input, &items))
	require.Len(t, items, 2)
	require.Equal(t, "developer", items[0].Role)

	var parts []apicompat.ResponsesContentPart
	require.NoError(t, json.Unmarshal(items[0].Content, &parts))
	require.Len(t, parts, 1)
	require.Contains(t, parts[0].Text, openAICompatClaudeCodeTodoGuardMarker)
	require.NotContains(t, strings.ToLower(parts[0].Text), "sub2api")
	require.NotContains(t, parts[0].Text, openAICompatClaudeCodeTodoGuardLegacyMarker)

	require.False(t, appendOpenAICompatClaudeCodeTodoGuard(req))
	body, err := json.Marshal(req)
	require.NoError(t, err)
	require.True(t, isOpenAICompatMessagesBridgeBody(body))
}

func TestOpenAICompatClaudeCodeTodoGuard_NormalizesLegacyMarker(t *testing.T) {
	legacyContent, err := json.Marshal([]apicompat.ResponsesContentPart{{
		Type: "input_text",
		Text: openAICompatClaudeCodeTodoGuardLegacyMarker + "\nlegacy guard\n" + openAICompatClaudeCodeTodoGuardLegacyClosingMarker,
	}})
	require.NoError(t, err)
	legacyInput, err := json.Marshal([]apicompat.ResponsesInputItem{{
		Type:    "message",
		Role:    "developer",
		Content: legacyContent,
	}})
	require.NoError(t, err)

	req := &apicompat.ResponsesRequest{Input: legacyInput}
	legacyBody, err := json.Marshal(req)
	require.NoError(t, err)
	require.True(t, isOpenAICompatMessagesBridgeBody(legacyBody))
	require.True(t, appendOpenAICompatClaudeCodeTodoGuard(req))
	body, err := json.Marshal(req)
	require.NoError(t, err)
	require.True(t, isOpenAICompatMessagesBridgeBody(body))
	require.NotContains(t, strings.ToLower(string(body)), "sub2api")
	require.Contains(t, string(body), "codex-compat-todo-guard")

	reqBody := map[string]any{
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "developer",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": openAICompatClaudeCodeTodoGuardLegacyMarker + "\nlegacy guard\n" + openAICompatClaudeCodeTodoGuardLegacyClosingMarker,
					},
				},
			},
		},
	}
	require.True(t, isOpenAICompatMessagesBridgeRequestBody(reqBody))
	require.True(t, appendOpenAICompatClaudeCodeTodoGuardToRequestBody(reqBody))
	require.True(t, isOpenAICompatMessagesBridgeRequestBody(reqBody))
	mapBody, err := json.Marshal(reqBody)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(mapBody)), "sub2api")
	require.Contains(t, string(mapBody), "codex-compat-todo-guard")
}
