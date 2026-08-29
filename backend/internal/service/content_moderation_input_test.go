package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 当数组末尾不是用户消息时（典型场景：Agent 工具循环结束于 tool/assistant），
// 应直接跳过审计——不再回溯查找历史中的某条用户消息。

func TestExtractContentModerationInput_AnthropicAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"调用一下天气工具"},
			{"role":"assistant","content":[{"type":"tool_use","id":"tool_1","name":"weather","input":{}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":"晴 25 度"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_AnthropicFirstTurnExtractsUser(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "Q1", input.Text)
}

func TestExtractContentModerationInput_AnthropicMultiTurnExtractsLatestUser(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"},
			{"role":"assistant","content":"A1"},
			{"role":"user","content":"Q2"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "Q2", input.Text)
}

func TestExtractContentModerationInput_AnthropicStreamResendExtractsResend(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"原问题"},
			{"role":"assistant","content":"部分回答……"},
			{"role":"user","content":"重发"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolAnthropicMessages, body)

	require.Equal(t, "重发", input.Text)
}

func TestExtractContentModerationInput_OpenAIChatAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"system","content":"sys"},
			{"role":"user","content":"列出我的订单"},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"orders","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"[]"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_OpenAIChatMultiTurnExtractsLatestUser(t *testing.T) {
	body := []byte(`{
		"messages": [
			{"role":"user","content":"Q1"},
			{"role":"assistant","content":"A1"},
			{"role":"user","content":"Q2"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body)

	require.Equal(t, "Q2", input.Text)
}

func TestExtractContentModerationInput_OpenAICompletionsSupportsPromptArray(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5",
		"prompt":["first prompt", "latest prompt"]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAICompletions, body)

	// Legacy Completions has no role metadata; preserve all textual prompts in
	// their deterministic request order so a batched request is fully audited.
	require.Equal(t, "first prompt latest prompt", input.Text)
}

func TestExtractContentModerationInput_OpenAIEmbeddingsSupportsStringAndTokenInputs(t *testing.T) {
	stringInput := ExtractContentModerationInput(ContentModerationProtocolOpenAIEmbeddings,
		[]byte(`{"model":"text-embedding-3-small","input":["first", "second"]}`))
	require.Equal(t, "first second", stringInput.Text)

	// Numeric token IDs are valid Embeddings input but are not auditable text;
	// they should be ignored rather than stringified into the audit payload.
	tokenInput := ExtractContentModerationInput(ContentModerationProtocolOpenAIEmbeddings,
		[]byte(`{"model":"text-embedding-3-small","input":[[123,456],[789]]}`))
	require.Empty(t, tokenInput.Text)
	require.Empty(t, tokenInput.Images)
}

func TestExtractContentModerationInput_OpenAIModerationsSupportsMultimodalInput(t *testing.T) {
	body := []byte(`{
		"model":"omni-moderation-latest",
		"input":[
			{"type":"text","text":"inspect this"},
			{"type":"image_url","image_url":{"url":"https://example.com/image.png"}}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIModerations, body)

	require.Equal(t, "inspect this", input.Text)
	require.Equal(t, []string{"https://example.com/image.png"}, input.Images)
}

func TestExtractContentModerationInput_OpenAIAlphaSearchUsesResponsesInput(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.5",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"search latest release"}]}
		],
		"commands":{"search_query":[{"q":"OpenAI news"}]}
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIAlphaSearch, body)
	require.Equal(t, "search latest release OpenAI news", input.Text)
}

func TestExtractContentModerationInput_OpenAILiveIncludesInstructions(t *testing.T) {
	body := []byte(`{
		"type":"realtime",
		"model":"gpt-4o-realtime-preview",
		"instructions":"answer briefly and safely",
		"input":[{"type":"input_text","text":"hello"}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAILive, body)
	require.Equal(t, "answer briefly and safely hello", input.Text)
}

func TestExtractContentModerationInput_OpenAILiveDoesNotTreatAudioDataAsImage(t *testing.T) {
	body := []byte(`{
		"type":"realtime",
		"input":[{"type":"input_audio","audio":"BASE64_AUDIO_CANARY","data":"BASE64_AUDIO_CANARY"}]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAILive, body)
	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_GeminiAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"查询天气"}]},
			{"role":"model","parts":[{"functionCall":{"name":"weather","args":{}}}]},
			{"role":"user","parts":[{"functionResponse":{"name":"weather","response":{"temp":25}}}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_GeminiFirstTurnExtractsUser(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"你好"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Equal(t, "你好", input.Text)
}

func TestExtractContentModerationInput_GeminiMultiTurnExtractsLatestUser(t *testing.T) {
	body := []byte(`{
		"contents": [
			{"role":"user","parts":[{"text":"Q1"}]},
			{"role":"model","parts":[{"text":"A1"}]},
			{"role":"user","parts":[{"text":"Q2"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolGemini, body)

	require.Equal(t, "Q2", input.Text)
}

func TestExtractContentModerationInput_ResponsesAgentToolLoopSkipsAudit(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"运行测试"}]},
			{"type":"function_call","call_id":"call_1","name":"run_tests","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"all passed"}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}

func TestExtractContentModerationInput_ResponsesLastUserMessageExtracted(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"latest"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Equal(t, "latest", input.Text)
}

func TestExtractContentModerationInput_ResponsesLastIsAssistantSkipped(t *testing.T) {
	body := []byte(`{
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"q1"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a1"}]}
		]
	}`)

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, body)

	require.Empty(t, input.Text)
	require.Empty(t, input.Images)
}
