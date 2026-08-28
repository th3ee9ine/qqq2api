package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// openAIWSStreamRequestStartMSMetadataKey is transport-only metadata emitted by
// the native Codex client immediately before every response.create write. It is
// deliberately refreshed for retries and connection reuse instead of being
// copied from the inbound request, where it may describe an earlier hop.
const openAIWSStreamRequestStartMSMetadataKey = "x-codex-ws-stream-request-start-ms"

// stampOpenAIWSStreamRequestStartJSON applies the native Codex WebSocket send
// timestamp while preserving the rest of the raw JSON byte-for-byte. Invalid
// JSON and non-response.create protocol frames are returned unchanged.
func stampOpenAIWSStreamRequestStartJSON(payload []byte, now time.Time) ([]byte, bool) {
	if len(payload) == 0 || !json.Valid(payload) ||
		strings.TrimSpace(gjson.GetBytes(payload, "type").String()) != "response.create" {
		return payload, false
	}

	stamp := strconv.FormatInt(now.UnixMilli(), 10)
	clientMetadata := gjson.GetBytes(payload, "client_metadata")
	if clientMetadata.IsObject() {
		updated, err := sjson.SetBytes(payload, "client_metadata."+openAIWSStreamRequestStartMSMetadataKey, stamp)
		if err != nil {
			return payload, false
		}
		return updated, true
	}

	metadata, err := json.Marshal(map[string]string{
		openAIWSStreamRequestStartMSMetadataKey: stamp,
	})
	if err != nil {
		return payload, false
	}
	updated, err := sjson.SetRawBytes(payload, "client_metadata", metadata)
	if err != nil {
		return payload, false
	}
	return updated, true
}

// stampOpenAIWSStreamRequestStartValue adapts the raw JSON implementation to
// the pooled JSON writer. Map payloads are copied before stamping so callers
// can safely retain an unstamped request template for retries or diagnostics.
func stampOpenAIWSStreamRequestStartValue(value any, now time.Time) (any, bool) {
	switch payload := value.(type) {
	case json.RawMessage:
		updated, changed := stampOpenAIWSStreamRequestStartJSON(payload, now)
		if !changed {
			return value, false
		}
		return json.RawMessage(updated), true
	case []byte:
		updated, changed := stampOpenAIWSStreamRequestStartJSON(payload, now)
		if !changed {
			return value, false
		}
		return updated, true
	case map[string]any:
		eventType, _ := payload["type"].(string)
		if strings.TrimSpace(eventType) != "response.create" {
			return value, false
		}
		updated := make(map[string]any, len(payload)+1)
		for key, item := range payload {
			updated[key] = item
		}
		stamp := strconv.FormatInt(now.UnixMilli(), 10)
		switch metadata := payload["client_metadata"].(type) {
		case map[string]any:
			cloned := make(map[string]any, len(metadata)+1)
			for key, item := range metadata {
				cloned[key] = item
			}
			cloned[openAIWSStreamRequestStartMSMetadataKey] = stamp
			updated["client_metadata"] = cloned
		case map[string]string:
			cloned := make(map[string]string, len(metadata)+1)
			for key, item := range metadata {
				cloned[key] = item
			}
			cloned[openAIWSStreamRequestStartMSMetadataKey] = stamp
			updated["client_metadata"] = cloned
		default:
			updated["client_metadata"] = map[string]string{
				openAIWSStreamRequestStartMSMetadataKey: stamp,
			}
		}
		return updated, true
	default:
		return value, false
	}
}
