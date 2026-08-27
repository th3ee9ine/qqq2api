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

// normalizeLegacyOpenAIOutboundWSResponseCreateJSON narrows the final WS
// guard to client response.create frames. Keeping the event check beside the
// JSON validity check prevents unrelated WS protocol messages from being
// rewritten while preserving the original payload bytes except for retired
// exact markers.
func normalizeLegacyOpenAIOutboundWSResponseCreateJSON(body []byte) ([]byte, bool) {
	if len(body) == 0 || !json.Valid(body) {
		return body, false
	}
	var event struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &event); err != nil || event.Type != "response.create" {
		return body, false
	}
	return normalizeLegacyOpenAIOutboundJSON(body)
}

// normalizeLegacyOpenAIOutboundWSResponseCreateValue adapts the byte-level
// guard to the pooled WS JSON writer. json.RawMessage is inspected directly so
// unknown fields, number spellings, and instructions survive unchanged; map
// payloads are marshaled only for the compatibility check and retain their
// existing WriteJSON behavior when no retired marker is present.
func normalizeLegacyOpenAIOutboundWSResponseCreateValue(value any) (any, bool) {
	var body []byte
	if raw, ok := value.(json.RawMessage); ok {
		body = raw
	} else {
		var encoded bytes.Buffer
		encoder := json.NewEncoder(&encoded)
		// The retired XML-like markers contain '<' and '>'. Disable HTML
		// escaping for the inspection copy so the existing exact-byte normalizer
		// can see them. A changed value is then returned as RawMessage so the WS
		// writer does not hide the normalized marker behind a second escaping pass.
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			return value, false
		}
		body = bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'})
	}
	normalized, changed := normalizeLegacyOpenAIOutboundWSResponseCreateJSON(body)
	if !changed {
		return value, false
	}
	return json.RawMessage(normalized), true
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
