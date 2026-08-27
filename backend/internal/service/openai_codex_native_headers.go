package service

import (
	"net/http"
	"strings"
)

const (
	// openAICodexResidencyHeader is emitted by the native Codex HTTP client
	// when a residency requirement is configured. The current client exposes
	// only the "us" requirement, so the gateway keeps that exact wire shape
	// instead of forwarding arbitrary internal-header values.
	openAICodexResidencyHeader = "x-openai-internal-codex-residency"

	// openAIResponsesTimingMetricsHeader is an optional native Codex WebSocket
	// handshake flag. It is intentionally limited to the WS path, matching the
	// upstream client rather than adding a non-native HTTP header.
	openAIResponsesTimingMetricsHeader = "x-responsesapi-include-timing-metrics"

	// openAIMemgenRequestHeader marks the native Codex memory-consolidation
	// subagent. It is meaningful only together with the matching subagent
	// header, so the copier below validates the pair as one native signal.
	openAIMemgenRequestHeader = "x-openai-memgen-request"
)

// copyOpenAICodexResidencyHeader preserves the native Codex residency
// requirement across the gateway. Only the value currently produced by the
// official client is accepted; duplicates and unknown values are omitted so a
// caller cannot create a header shape the native client would never send.
func copyOpenAICodexResidencyHeader(dst, src http.Header) {
	copyOpenAICodexNativeSingletonHeader(dst, src, openAICodexResidencyHeader, "us")
}

// copyOpenAIResponsesTimingMetricsHeader preserves the optional native Codex
// WebSocket timing-metrics flag without leaking it onto HTTP Responses calls.
func copyOpenAIResponsesTimingMetricsHeader(dst, src http.Header) {
	copyOpenAICodexNativeSingletonHeader(dst, src, openAIResponsesTimingMetricsHeader, "true")
}

// copyOpenAIMemgenRequestHeader preserves the request-kind signal used by the
// native memory-consolidation subagent. Requiring the matching subagent value
// avoids forwarding a standalone internal flag that the native client would
// not produce for an ordinary turn.
func copyOpenAIMemgenRequestHeader(dst, src http.Header) {
	deleteOpenAIHeaderEqualFold(dst, openAIMemgenRequestHeader)
	subagentValues := openAIHeaderValuesEqualFold(src, openAISubagentHeader)
	if len(subagentValues) != 1 || strings.TrimSpace(subagentValues[0]) != "memory_consolidation" {
		return
	}
	copyOpenAICodexNativeSingletonHeader(dst, src, openAIMemgenRequestHeader, "true")
}

func copyOpenAICodexNativeSingletonHeader(dst, src http.Header, name, allowedValue string) {
	if dst == nil || src == nil {
		return
	}
	deleteOpenAIHeaderEqualFold(dst, name)
	values := openAIHeaderValuesEqualFold(src, name)
	if len(values) != 1 || !strings.EqualFold(strings.TrimSpace(values[0]), allowedValue) {
		return
	}
	dst.Set(name, allowedValue)
}

// openAIHeaderValuesEqualFold also handles headers assembled directly as a
// map (rather than through Header.Set), where key canonicalization is not
// guaranteed. Multiple differently-cased keys remain duplicates and are
// rejected by the singleton guard above.
func openAIHeaderValuesEqualFold(headers http.Header, name string) []string {
	var values []string
	for key, candidates := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			values = append(values, candidates...)
		}
	}
	return values
}
