package service

import (
	"encoding/json"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

const (
	openAICompatClaudeCodeTodoGuardMarker              = "<codex-compat-todo-guard>"
	openAICompatClaudeCodeTodoGuardClosingMarker       = "</codex-compat-todo-guard>"
	openAICompatClaudeCodeTodoGuardLegacyMarker        = "<sub2api-claude-code-todo-guard>"
	openAICompatClaudeCodeTodoGuardLegacyClosingMarker = "</sub2api-claude-code-todo-guard>"
	openAICompatClaudeCodeTodoGuardText                = openAICompatClaudeCodeTodoGuardMarker + "\nWhen using Claude Code todo or task tracking tools, keep the visible task list consistent. Do not send final or summary text while any item remains in_progress. Before finishing, asking the user to choose, or reporting a blocker, update the todo list so completed work is completed and deferred work is pending/open; leave an item in_progress only when active work will continue in the same turn.\n" + openAICompatClaudeCodeTodoGuardClosingMarker
)

func appendOpenAICompatClaudeCodeTodoGuard(req *apicompat.ResponsesRequest) bool {
	if req == nil || len(req.Input) == 0 {
		return false
	}

	normalizedInput, legacyNormalized := normalizeOpenAICompatClaudeCodeTodoGuardRawJSON(req.Input)
	if legacyNormalized {
		req.Input = normalizedInput
	}

	var items []apicompat.ResponsesInputItem
	if err := json.Unmarshal(req.Input, &items); err != nil {
		return legacyNormalized
	}
	if len(items) == 0 || responsesInputItemsContainTodoGuard(items) {
		return legacyNormalized
	}

	content, err := json.Marshal([]apicompat.ResponsesContentPart{{
		Type: "input_text",
		Text: openAICompatClaudeCodeTodoGuardText,
	}})
	if err != nil {
		return legacyNormalized
	}

	guard := apicompat.ResponsesInputItem{
		Type:    "message",
		Role:    "developer",
		Content: content,
	}

	insertAt := 0
	for insertAt < len(items) && items[insertAt].Type == "message" && items[insertAt].Role == "developer" {
		insertAt++
	}

	items = append(items, apicompat.ResponsesInputItem{})
	copy(items[insertAt+1:], items[insertAt:])
	items[insertAt] = guard

	input, err := json.Marshal(items)
	if err != nil {
		return legacyNormalized
	}
	req.Input = input
	return true
}

func appendOpenAICompatClaudeCodeTodoGuardToRequestBody(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}

	legacyNormalized := normalizeOpenAICompatClaudeCodeTodoGuardInRequestBody(reqBody)
	rawInput, ok := reqBody["input"]
	if !ok {
		return legacyNormalized
	}
	input, ok := rawInput.([]any)
	if !ok || len(input) == 0 || inputContainsTodoGuard(input) {
		return legacyNormalized
	}

	guard := map[string]any{
		"type": "message",
		"role": "developer",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": openAICompatClaudeCodeTodoGuardText,
			},
		},
	}

	insertAt := 0
	for insertAt < len(input) {
		item, ok := input[insertAt].(map[string]any)
		if !ok || strings.TrimSpace(firstNonEmptyString(item["type"])) != "message" || strings.TrimSpace(firstNonEmptyString(item["role"])) != "developer" {
			break
		}
		insertAt++
	}

	input = append(input, nil)
	copy(input[insertAt+1:], input[insertAt:])
	input[insertAt] = guard
	reqBody["input"] = input
	return true
}

func normalizeOpenAICompatClaudeCodeTodoGuardInRequestBody(reqBody map[string]any) bool {
	if reqBody == nil {
		return false
	}
	rawInput, ok := reqBody["input"]
	if !ok {
		return false
	}
	normalizedInput, changed := normalizeOpenAICompatClaudeCodeTodoGuardValue(rawInput)
	if changed {
		reqBody["input"] = normalizedInput
	}
	return changed
}

func normalizeOpenAICompatClaudeCodeTodoGuardRawJSON(raw json.RawMessage) (json.RawMessage, bool) {
	var value any
	if err := decodeOpenAIJSONUseNumber(raw, &value); err != nil {
		return raw, false
	}
	normalized, changed := normalizeOpenAICompatClaudeCodeTodoGuardValue(value)
	if !changed {
		return raw, false
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return raw, false
	}
	return encoded, true
}

func normalizeOpenAICompatClaudeCodeTodoGuardValue(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		normalized := strings.ReplaceAll(typed, openAICompatClaudeCodeTodoGuardLegacyMarker, openAICompatClaudeCodeTodoGuardMarker)
		normalized = strings.ReplaceAll(normalized, openAICompatClaudeCodeTodoGuardLegacyClosingMarker, openAICompatClaudeCodeTodoGuardClosingMarker)
		return normalized, normalized != typed
	case []any:
		changed := false
		for i, item := range typed {
			normalized, itemChanged := normalizeOpenAICompatClaudeCodeTodoGuardValue(item)
			if itemChanged {
				typed[i] = normalized
				changed = true
			}
		}
		return typed, changed
	case map[string]any:
		changed := false
		for key, item := range typed {
			normalized, itemChanged := normalizeOpenAICompatClaudeCodeTodoGuardValue(item)
			if itemChanged {
				typed[key] = normalized
				changed = true
			}
		}
		return typed, changed
	default:
		return value, false
	}
}

func responsesInputItemsContainTodoGuard(items []apicompat.ResponsesInputItem) bool {
	return responsesInputItemsContainText(items, openAICompatClaudeCodeTodoGuardMarker) ||
		responsesInputItemsContainText(items, openAICompatClaudeCodeTodoGuardLegacyMarker)
}

func inputContainsTodoGuard(input []any) bool {
	return inputContainsText(input, openAICompatClaudeCodeTodoGuardMarker) ||
		inputContainsText(input, openAICompatClaudeCodeTodoGuardLegacyMarker)
}

func responsesInputItemsContainText(items []apicompat.ResponsesInputItem, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, item := range items {
		if serializedJSONContainsText(string(item.Content), needle) {
			return true
		}
	}
	return false
}

func inputContainsText(input []any, needle string) bool {
	needle = strings.TrimSpace(needle)
	if needle == "" {
		return false
	}
	for _, item := range input {
		b, err := json.Marshal(item)
		if err == nil && serializedJSONContainsText(string(b), needle) {
			return true
		}
	}
	return false
}

func serializedJSONContainsText(serialized, needle string) bool {
	if strings.Contains(serialized, needle) {
		return true
	}
	encoded, err := json.Marshal(needle)
	if err != nil || len(encoded) < 2 {
		return false
	}
	return strings.Contains(serialized, string(encoded[1:len(encoded)-1]))
}
