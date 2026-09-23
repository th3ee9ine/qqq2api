package apicompat

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpus55ResponsesAdaptiveThinkingAndToolChoice(t *testing.T) {
	for _, effort := range []string{"", "low", "medium", "high", "xhigh", "max"} {
		req := &ResponsesRequest{Model: "claude-opus-5-5", Input: json.RawMessage(`"hello"`), Reasoning: &ResponsesReasoning{Effort: effort}}
		out, err := ResponsesToAnthropicRequest(req)
		require.NoError(t, err)
		require.Equal(t, "adaptive", out.Thinking.Type)
		require.Zero(t, out.Thinking.BudgetTokens)
		if effort == "" {
			effort = "medium"
		}
		require.Equal(t, effort, out.OutputConfig.Effort)
	}
	for _, choice := range []string{`"required"`, `{"type":"function","name":"lookup"}`} {
		_, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: "claude-opus-5-5", Input: json.RawMessage(`"hello"`), ToolChoice: json.RawMessage(choice)})
		require.ErrorContains(t, err, "forced tool_choice")
	}
	_, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: "claude-opus-5-5", Input: json.RawMessage(`"hello"`), Reasoning: &ResponsesReasoning{Effort: "none"}})
	require.ErrorContains(t, err, "reasoning effort")
	old, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: "claude-opus-5", Input: json.RawMessage(`"hello"`), Reasoning: &ResponsesReasoning{Effort: "xhigh"}})
	require.NoError(t, err)
	require.Equal(t, "max", old.OutputConfig.Effort)
	require.Equal(t, "enabled", old.Thinking.Type)
}

func TestOpus55SignedThinkingResponsesRoundTrip(t *testing.T) {
	block := AnthropicContentBlock{Type: "thinking", Thinking: "", Signature: "upstream-signed-block"}
	response := AnthropicToResponsesResponse(&AnthropicResponse{Model: "claude-opus-5-5", Content: []AnthropicContentBlock{block, {Type: "tool_use", ID: "toolu_1", Name: "lookup", Input: json.RawMessage(`{}`)}}})
	require.Len(t, response.Output, 2)
	require.NotEmpty(t, response.Output[0].EncryptedContent)
	raw, err := json.Marshal(response.Output)
	require.NoError(t, err)
	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(raw, &items))
	items = append(items, ResponsesInputItem{Type: "function_call_output", CallID: response.Output[1].CallID, Output: "ok"})
	raw, err = json.Marshal(items)
	require.NoError(t, err)
	converted, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: "claude-opus-5-5", Input: raw})
	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(converted.Messages[0].Content, &blocks))
	require.Equal(t, block, blocks[0])
	require.Equal(t, "tool_use", blocks[1].Type)
	// An explicitly marked but malformed bridge envelope must be rejected.
	_, _, err = convertResponsesInputToAnthropic("", json.RawMessage(`[{"type":"reasoning","encrypted_content":"anthropic-thinking-v1:!"}]`), true)
	require.Error(t, err)
}

func TestOpus55StreamingThinkingSignatureRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name, upstreamModel, replayModel string
		preserve, wantSignature          bool
	}{
		{name: "upstream model enables signature preservation", upstreamModel: "claude-opus-5-5", replayModel: "claude-opus-5-5", wantSignature: true},
		{name: "mapped account alias enables signature preservation", upstreamModel: "account-alias", replayModel: "claude-opus-5-5", preserve: true, wantSignature: true},
		{name: "older model keeps existing summary-only behavior", upstreamModel: "claude-opus-5", replayModel: "claude-opus-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := NewAnthropicEventToResponsesState()
			state.Model = "client-alias"
			state.PreserveThinkingSignatures = tc.preserve
			var done []ResponsesOutput
			var completed *ResponsesResponse
			for _, event := range []AnthropicStreamEvent{
				{Type: "message_start", Message: &AnthropicResponse{ID: "msg_signed", Model: tc.upstreamModel}},
				{Type: "content_block_start", ContentBlock: &AnthropicContentBlock{Type: "thinking", Thinking: "Plan ", Signature: "sig-"}},
				{Type: "content_block_delta", Delta: &AnthropicDelta{Type: "thinking_delta", Thinking: "carefully."}},
				{Type: "content_block_delta", Delta: &AnthropicDelta{Type: "signature_delta", Signature: "part-one"}},
				{Type: "content_block_delta", Delta: &AnthropicDelta{Type: "signature_delta", Signature: ":part-two"}},
				{Type: "content_block_stop"},
				{Type: "content_block_start", ContentBlock: &AnthropicContentBlock{Type: "text"}},
				{Type: "content_block_delta", Delta: &AnthropicDelta{Type: "text_delta", Text: "Answer"}},
				{Type: "content_block_stop"},
				{Type: "message_stop"},
			} {
				for _, converted := range AnthropicEventToResponsesEvents(&event, state) {
					switch converted.Type {
					case "response.output_item.done":
						require.NotNil(t, converted.Item)
						done = append(done, *converted.Item)
					case "response.completed":
						completed = converted.Response
					}
				}
			}
			require.NotNil(t, completed)
			require.Len(t, done, 2)
			require.Equal(t, done, completed.Output, "item.done and response.completed must carry the same signed block")
			require.Equal(t, []ResponsesSummary{{Type: "summary_text", Text: "Plan carefully."}}, done[0].Summary)
			blocks := replayThinkingOutputs(t, tc.replayModel, completed.Output)
			if tc.wantSignature {
				require.NotEmpty(t, done[0].EncryptedContent)
				require.Equal(t, []AnthropicContentBlock{
					{Type: "thinking", Thinking: "Plan carefully.", Signature: "sig-part-one:part-two"},
					{Type: "text", Text: "Answer"},
				}, blocks)
			} else {
				require.Empty(t, done[0].EncryptedContent)
				require.Equal(t, []AnthropicContentBlock{{Type: "text", Text: "Answer"}}, blocks)
			}
		})
	}
}

func TestOpus55RedactedThinkingRoundTrip(t *testing.T) {
	block := AnthropicContentBlock{Type: "redacted_thinking", Data: "opaque+redacted/payload=="}
	for _, streaming := range []bool{false, true} {
		name := "buffered"
		if streaming {
			name = "streaming"
		}
		t.Run(name, func(t *testing.T) {
			var outputs []ResponsesOutput
			if streaming {
				state := NewAnthropicEventToResponsesState()
				for _, event := range []AnthropicStreamEvent{
					{Type: "message_start", Message: &AnthropicResponse{ID: "msg_redacted", Model: "claude-opus-5-5"}},
					{Type: "content_block_start", ContentBlock: &block},
					{Type: "content_block_stop"},
					{Type: "message_stop"},
				} {
					for _, converted := range AnthropicEventToResponsesEvents(&event, state) {
						if converted.Type == "response.completed" {
							outputs = converted.Response.Output
						}
					}
				}
			} else {
				outputs = AnthropicToResponsesResponse(&AnthropicResponse{Model: "claude-opus-5-5", Content: []AnthropicContentBlock{block}}).Output
			}
			require.Len(t, outputs, 1)
			require.Equal(t, "reasoning", outputs[0].Type)
			require.Empty(t, outputs[0].Summary, "redacted content must never appear as visible reasoning")
			require.NotEmpty(t, outputs[0].EncryptedContent)
			require.Equal(t, []AnthropicContentBlock{block}, replayThinkingOutputs(t, "claude-opus-5-5", outputs))
		})
	}
}

func TestOpus55ThinkingBridgeIgnoresUnmarkedCiphertext(t *testing.T) {
	for _, ciphertext := range []string{
		"gAAAAAB_OpenAI_opaque_ciphertext",
		base64.RawStdEncoding.EncodeToString([]byte(`{"type":"thinking","thinking":"untrusted","signature":"not-an-anthropic-envelope"}`)),
	} {
		input, err := json.Marshal([]ResponsesInputItem{
			{Type: "reasoning", EncryptedContent: ciphertext},
			{Type: "message", Role: "user", Content: json.RawMessage(`"Continue"`)},
		})
		require.NoError(t, err)
		converted, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: "claude-opus-5-5", Input: input})
		require.NoError(t, err)
		require.Len(t, converted.Messages, 1, "ordinary OpenAI ciphertext must not become an assistant thinking block")
		require.Equal(t, "user", converted.Messages[0].Role)
		require.JSONEq(t, `"Continue"`, string(converted.Messages[0].Content))
	}
}

func replayThinkingOutputs(t *testing.T, model string, outputs []ResponsesOutput) []AnthropicContentBlock {
	t.Helper()
	raw, err := json.Marshal(outputs)
	require.NoError(t, err)
	var items []ResponsesInputItem
	require.NoError(t, json.Unmarshal(raw, &items))
	items = append(items, ResponsesInputItem{Type: "message", Role: "user", Content: json.RawMessage(`"Continue"`)})
	raw, err = json.Marshal(items)
	require.NoError(t, err)
	converted, err := ResponsesToAnthropicRequest(&ResponsesRequest{Model: model, Input: raw})
	require.NoError(t, err)
	require.Len(t, converted.Messages, 2)
	require.Equal(t, "assistant", converted.Messages[0].Role)
	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(converted.Messages[0].Content, &blocks))
	return blocks
}

func TestGPT6ChatSamplingAndCacheFields(t *testing.T) {
	temperature := 0.7
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna"} {
		for _, effort := range []string{"", "none", "medium", "max"} {
			out, err := ChatCompletionsToResponses(&ChatCompletionsRequest{Model: model, ReasoningEffort: effort, Temperature: &temperature, TopP: &temperature, PromptCacheOptions: json.RawMessage(`{"mode":"explicit","ttl":"30m"}`), Messages: []ChatMessage{{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"hello","prompt_cache_breakpoint":{"mode":"explicit"}}]`)}}})
			require.NoError(t, err)
			if effort == "none" {
				require.NotNil(t, out.Temperature)
			} else {
				require.Nil(t, out.Temperature)
				require.Nil(t, out.TopP)
			}
			require.JSONEq(t, `{"mode":"explicit","ttl":"30m"}`, string(out.PromptCacheOptions))
			require.Contains(t, string(out.Input), "prompt_cache_breakpoint")
		}
	}
}

func TestGPT6AnthropicBridgePreservesModelAndReasoningRules(t *testing.T) {
	temperature := 0.4
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna"} {
		out, err := AnthropicToResponses(&AnthropicRequest{
			Model:       model,
			MaxTokens:   100,
			Temperature: &temperature,
			Messages:    []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
		})
		require.NoError(t, err)
		require.Equal(t, model, out.Model)
		require.Nil(t, out.Temperature, "GPT-6 Sol/Luna are reasoning models on Responses")
		require.Nil(t, out.TopP)
		require.NotNil(t, out.Reasoning)
		chat, err := AnthropicToChatCompletionsRequest(&AnthropicRequest{
			Model:       model + "-high",
			Temperature: &temperature,
			Messages:    []AnthropicMessage{{Role: "user", Content: json.RawMessage(`"hello"`)}},
		})
		require.NoError(t, err)
		require.Equal(t, model+"-high", chat.Model)
		require.Nil(t, chat.Temperature)
	}
}

func TestOpus55AnthropicResponseBridgeKeepsSignedThinking(t *testing.T) {
	resp := AnthropicToResponsesResponse(&AnthropicResponse{
		Model: "claude-opus-5-5",
		Content: []AnthropicContentBlock{
			{Type: "thinking", Signature: "sig-55"},
			{Type: "text", Text: "answer"},
		},
	})
	require.Len(t, resp.Output, 2)
	require.Equal(t, "reasoning", resp.Output[0].Type)
	require.Contains(t, resp.Output[0].EncryptedContent, anthropicThinkingEnvelopePrefix)
	require.Equal(t, "message", resp.Output[1].Type)
}
