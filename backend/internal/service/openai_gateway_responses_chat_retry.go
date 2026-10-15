package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
)

// isResponsesShapedRequest identifies the compatibility form where a client
// sent a Responses request to the Chat endpoint. A request containing messages
// remains a Chat request even when it happens to carry an input extension.
func isResponsesShapedRequest(body []byte) bool {
	return !gjson.GetBytes(body, "messages").Exists() && gjson.GetBytes(body, "input").Exists()
}

// shouldRetryResponsesAsChatCompletions is deliberately narrower than a
// generic HTTP retry. It is only an endpoint compatibility retry for an
// unprobed API-key account, and only when the upstream response proves that
// the Responses input shape is unsupported (or the Responses endpoint itself
// is absent). Business errors, transient errors, authentication failures, and
// model errors must remain visible to the normal failover/error path.
func shouldRetryResponsesAsChatCompletions(
	ctx context.Context,
	c *gin.Context,
	status int,
	responseBody []byte,
) bool {
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if c != nil {
		if c.Request != nil && c.Request.Context().Err() != nil {
			return false
		}
		if IsResponseCommitted(c) || (c.Writer != nil && c.Writer.Written()) {
			return false
		}
	}
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		// A model_not_found/resource_not_found response says nothing about
		// endpoint support. Do not turn a model mistake into a second request.
		return !isResponsesProbeModelUnavailable(status, responseBody)
	}
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	return isResponsesStructuredInputTypeRejection(responseBody)
}

// isResponsesStructuredInputTypeRejection recognizes the common compatibility
// failure emitted by Chat-only providers for a Responses input array/object:
// "input: expected a string, but got an array/object". The match is anchored
// to the top-level error parameter when present. Nested paths such as
// input[0].content are ordinary request validation errors and are excluded.
func isResponsesStructuredInputTypeRejection(responseBody []byte) bool {
	param := ""
	message := ""
	for _, path := range []string{"error.param", "response.error.param", "param"} {
		if candidate := strings.TrimSpace(strings.ToLower(gjson.GetBytes(responseBody, path).String())); candidate != "" {
			param = candidate
			break
		}
	}
	for _, path := range []string{"error.message", "response.error.message", "error.detail", "response.error.detail", "message", "detail"} {
		if candidate := strings.TrimSpace(strings.ToLower(gjson.GetBytes(responseBody, path).String())); candidate != "" {
			message = candidate
			break
		}
	}
	if param != "" && param != "input" {
		return false
	}
	if strings.Contains(message, "input[") || strings.Contains(message, "input.") {
		return false
	}
	if !strings.Contains(message, "input") ||
		!strings.Contains(message, "expected") ||
		!strings.Contains(message, "string") ||
		(!strings.Contains(message, "got") && !strings.Contains(message, "received")) ||
		(!strings.Contains(message, "array") && !strings.Contains(message, "object")) {
		return false
	}
	return param == "input" || strings.Contains(message, "expected a string") || strings.Contains(message, "expected string")
}

// responsesBodyToChatCompletionsBody lowers a Responses-shaped request before
// it is sent to a Chat-only upstream. It returns an error when preserving the
// request semantics would require state or fields that Chat Completions cannot
// represent; silently dropping those fields could execute a different request.
func responsesBodyToChatCompletionsBody(body []byte) ([]byte, error) {
	var req apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("parse Responses fallback request: %w", err)
	}
	if err := validateResponsesChatFallbackShape(body, &req); err != nil {
		return nil, err
	}
	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&req)
	if err != nil {
		return nil, fmt.Errorf("convert Responses fallback request to Chat Completions: %w", err)
	}
	chatReq.Model = req.Model
	converted, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal Chat Completions fallback request: %w", err)
	}
	return converted, nil
}

func validateResponsesChatFallbackShape(body []byte, req *apicompat.ResponsesRequest) error {
	if req == nil {
		return fmt.Errorf("Responses fallback request is nil")
	}
	if req.PreviousResponseID != "" || req.PromptCacheKey != "" || len(bytes.TrimSpace(req.PromptCacheOptions)) > 0 {
		return fmt.Errorf("Responses request uses state or prompt-cache fields that Chat fallback cannot preserve")
	}
	if req.Store != nil || len(req.Include) > 0 {
		return fmt.Errorf("Responses request uses state/output fields that Chat fallback cannot preserve")
	}
	if req.Reasoning != nil && strings.TrimSpace(req.Reasoning.Summary) != "" {
		return fmt.Errorf("Responses reasoning summary cannot be preserved by Chat fallback")
	}
	if req.Text != nil && strings.TrimSpace(req.Text.Verbosity) != "" {
		return fmt.Errorf("Responses text verbosity cannot be preserved by Chat fallback")
	}
	tools, err := apicompat.EffectiveResponsesTools(req)
	if err != nil {
		return fmt.Errorf("resolve Responses fallback tools: %w", err)
	}
	if err := validateResponsesToolsForChatFallback(tools); err != nil {
		return err
	}
	if len(bytes.TrimSpace(req.ToolChoice)) > 0 && string(bytes.TrimSpace(req.ToolChoice)) != "null" && len(tools) == 0 {
		return fmt.Errorf("Responses tool_choice cannot be preserved without executable tools")
	}

	// Reject newer stateful or background controls not represented by the local
	// request struct. This check is intentionally based on the raw top-level
	// object so json.Unmarshal cannot silently erase them.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return fmt.Errorf("parse Responses fallback fields: %w", err)
	}
	allowed := map[string]struct{}{
		"model": {}, "instructions": {}, "input": {}, "max_output_tokens": {},
		"temperature": {}, "top_p": {}, "stream": {}, "tools": {}, "include": {},
		"store": {}, "parallel_tool_calls": {}, "reasoning": {}, "text": {},
		"tool_choice": {}, "service_tier": {}, "prompt_cache_key": {},
		"prompt_cache_options": {}, "previous_response_id": {},
	}
	for key, value := range raw {
		if _, ok := allowed[key]; ok {
			continue
		}
		if len(bytes.TrimSpace(value)) > 0 && string(bytes.TrimSpace(value)) != "null" {
			return fmt.Errorf("Responses field %q cannot be preserved by Chat fallback", key)
		}
	}

	input := bytes.TrimSpace(req.Input)
	if len(input) == 0 || string(input) == "null" {
		return fmt.Errorf("Responses input is missing and cannot be preserved by Chat fallback")
	}
	var inputText string
	if json.Unmarshal(input, &inputText) == nil {
		return nil
	}
	if input[0] != '[' {
		return fmt.Errorf("Responses input type cannot be preserved by Chat fallback")
	}
	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return fmt.Errorf("parse Responses fallback input: %w", err)
	}
	for _, item := range items {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(item, &value); err != nil {
			return fmt.Errorf("parse Responses fallback input item: %w", err)
		}
		typeName := strings.Trim(strings.TrimSpace(string(value["type"])), `"`)
		switch typeName {
		case "", "message", "reasoning", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output", "tool_search_call", "tool_search_output":
		default:
			return fmt.Errorf("Responses input item type %q cannot be preserved by Chat fallback", typeName)
		}
		if typeName == "reasoning" {
			if summary, hasSummary := value["summary"]; !hasSummary || len(bytes.TrimSpace(summary)) == 0 || string(bytes.TrimSpace(summary)) == "null" || string(bytes.TrimSpace(summary)) == "[]" {
				return fmt.Errorf("Responses reasoning without a plaintext summary cannot be preserved by Chat fallback")
			}
		}
		content, ok := value["content"]
		if !ok {
			continue
		}
		var parts []map[string]json.RawMessage
		if bytes.HasPrefix(bytes.TrimSpace(content), []byte{'['}) && json.Unmarshal(content, &parts) == nil {
			for _, part := range parts {
				partType := strings.Trim(strings.TrimSpace(string(part["type"])), `"`)
				switch partType {
				case "", "input_text", "output_text", "text", "input_image", "image_url":
				default:
					return fmt.Errorf("Responses content part type %q cannot be preserved by Chat fallback", partType)
				}
			}
		}
	}
	return nil
}

func validateResponsesToolsForChatFallback(tools []apicompat.ResponsesTool) error {
	for _, tool := range tools {
		switch tool.Type {
		case "function", "custom", "tool_search", "x_search":
		case "namespace":
			children := tool.Tools
			if len(children) == 0 {
				children = tool.Children
			}
			if err := validateResponsesToolsForChatFallback(children); err != nil {
				return err
			}
		default:
			return fmt.Errorf("Responses tool type %q cannot be preserved by Chat fallback", tool.Type)
		}
	}
	return nil
}
