package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

var legacyOpenAIOutboundReplacements = [...][2][]byte{
	{[]byte(openAICompatClaudeCodeTodoGuardLegacyMarker), []byte(openAICompatClaudeCodeTodoGuardMarker)},
	{[]byte(openAICompatClaudeCodeTodoGuardLegacyClosingMarker), []byte(openAICompatClaudeCodeTodoGuardClosingMarker)},
	{[]byte(codexImageGenerationBridgeLegacyMarker), []byte(codexImageGenerationBridgeMarker)},
	{[]byte(codexImageGenerationBridgeLegacyClosingMarker), []byte(codexImageGenerationBridgeClosingMarker)},
	{[]byte(codexSparkImageUnsupportedLegacyMarker), []byte(codexSparkImageUnsupportedMarker)},
	{[]byte(codexSparkImageUnsupportedLegacyClosingMarker), []byte(codexSparkImageUnsupportedClosingMarker)},
	{[]byte(`"` + codexPythonToolLegacyAlias + `"`), []byte(`"` + codexPythonToolAlias + `"`)},
}

// normalizeLegacyOpenAIOutboundJSON is the final wire-bound compatibility
// guard. Older stored/replayed requests may bypass the feature-specific
// transform that originally introduced these tokens, so every JSON transport
// replaces only the retired exact markers before dispatch.
func normalizeLegacyOpenAIOutboundJSON(body []byte) ([]byte, bool) {
	if len(body) == 0 || !json.Valid(body) {
		return body, false
	}
	normalized := body
	changed := false
	for _, replacement := range legacyOpenAIOutboundReplacements {
		if !bytes.Contains(normalized, replacement[0]) {
			continue
		}
		normalized = bytes.ReplaceAll(normalized, replacement[0], replacement[1])
		changed = true
	}
	return normalized, changed
}

// normalizeLegacyOpenAIOutboundRequestBody applies the JSON guard without
// consuming a one-shot body. All in-tree OpenAI JSON builders use replayable
// bytes/string readers; unknown streaming bodies are intentionally untouched.
func normalizeLegacyOpenAIOutboundRequestBody(req *http.Request) {
	if req == nil || req.Body == nil || req.GetBody == nil {
		return
	}
	bodyReader, err := req.GetBody()
	if err != nil {
		return
	}
	body, err := io.ReadAll(bodyReader)
	_ = bodyReader.Close()
	if err != nil {
		return
	}
	normalized, changed := normalizeLegacyOpenAIOutboundJSON(body)
	if !changed {
		return
	}
	stable := bytes.Clone(normalized)
	req.Body = io.NopCloser(bytes.NewReader(stable))
	req.ContentLength = int64(len(stable))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(stable)), nil
	}
}
