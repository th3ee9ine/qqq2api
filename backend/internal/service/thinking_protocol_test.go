package service

import "testing"

func TestResolveThinkingProtocol(t *testing.T) {
	tests := []struct {
		name, modelID string
		want          ThinkingProtocol
	}{
		{"claude-sonnet", "claude-sonnet-4-5", ThinkingProtocolAnthropicStrict},
		{"claude-opus", "claude-opus-4-5-20251101", ThinkingProtocolAnthropicStrict},
		{"short opus", "opus-4-5", ThinkingProtocolAnthropicStrict},
		{"short sonnet", "sonnet-4-5", ThinkingProtocolAnthropicStrict},
		{"short haiku", "haiku-4-5", ThinkingProtocolAnthropicStrict},
		{"uppercase Claude", "Claude-Sonnet-4-5", ThinkingProtocolAnthropicStrict},
		{"qwen thinking", "qwen-2-72b-thinking", ThinkingProtocolPassbackRequired},
		{"qwen3 thinking", "qwen3-235b-a22b-thinking-2507", ThinkingProtocolPassbackRequired},
		{"qwen3 next thinking", "qwen3-next-80b-a3b-thinking", ThinkingProtocolPassbackRequired},
		{"qwen without thinking", "qwen3-32b", ThinkingProtocolUnknown},
		{"retired kimi", "kimi-k3", ThinkingProtocolUnknown},
		{"retired zhipu", "glm-5.2", ThinkingProtocolUnknown},
		{"retired minimax", "MiniMax-M2", ThinkingProtocolUnknown},
		{"gpt", "gpt-5.1", ThinkingProtocolUnknown},
		{"empty", "", ThinkingProtocolUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveThinkingProtocol(tt.modelID); got != tt.want {
				t.Errorf("ResolveThinkingProtocol(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestThinkingProtocolFiltersOnlyAnthropicStrict(t *testing.T) {
	for _, model := range []string{"claude-sonnet-4-5", "opus-4-5", "haiku-4-5"} {
		if !ShouldPreFilterThinkingBlocks(model) || !ShouldRectifyThinkingSignatureError(model) || !ShouldApplyRetryFilters(model) {
			t.Fatalf("strict model %q should enable all thinking filters", model)
		}
	}
	for _, model := range []string{"kimi-k3", "glm-5.2", "MiniMax-M2", "qwen3-235b-thinking", "gpt-5.1", ""} {
		if ShouldPreFilterThinkingBlocks(model) || ShouldRectifyThinkingSignatureError(model) || ShouldApplyRetryFilters(model) {
			t.Fatalf("non-Anthropic model %q should not enable thinking filters", model)
		}
	}
}
