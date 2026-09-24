package service

import "strings"

// ThinkingProtocol describes the thinking-block contract used when forwarding
// Anthropic messages. Anthropic requires signed blocks, while the retained Qwen
// thinking integration requires passback. Unknown upstreams are left untouched so protocol
// details supplied by the client survive the gateway.
type ThinkingProtocol int

const (
	ThinkingProtocolUnknown ThinkingProtocol = iota
	ThinkingProtocolAnthropicStrict
	ThinkingProtocolPassbackRequired
)

// ResolveThinkingProtocol recognizes Anthropic model IDs. Retired vendor model
// names intentionally resolve to Unknown; the gateway no longer applies their
// provider-specific thinking or passback adaptations.
func ResolveThinkingProtocol(modelID string) ThinkingProtocol {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if (strings.HasPrefix(id, "qwen-") || strings.HasPrefix(id, "qwen2-") ||
		strings.HasPrefix(id, "qwen3-") || strings.HasPrefix(id, "qwen4-")) && strings.Contains(id, "-thinking") {
		return ThinkingProtocolPassbackRequired
	}
	switch {
	case strings.HasPrefix(id, "claude-"),
		strings.HasPrefix(id, "opus-"),
		strings.HasPrefix(id, "sonnet-"),
		strings.HasPrefix(id, "haiku-"):
		return ThinkingProtocolAnthropicStrict
	default:
		return ThinkingProtocolUnknown
	}
}

func ShouldPreFilterThinkingBlocks(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}

func ShouldRectifyThinkingSignatureError(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}

func ShouldApplyRetryFilters(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}
