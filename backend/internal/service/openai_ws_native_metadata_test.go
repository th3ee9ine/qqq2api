package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStampOpenAIWSStreamRequestStartJSONMatchesNativeSendShape(t *testing.T) {
	now := time.Unix(1_700_000_000, 123_000_000)
	payload := []byte(`{"type":"response.create","client_metadata":{"session_id":"session-1","x-codex-ws-stream-request-start-ms":"stale"},"sequence":900719925474099312345}`)

	stamped, changed := stampOpenAIWSStreamRequestStartJSON(payload, now)
	require.True(t, changed)
	require.Equal(t, "1700000000123", gjson.GetBytes(stamped, "client_metadata.x-codex-ws-stream-request-start-ms").String())
	require.Equal(t, "session-1", gjson.GetBytes(stamped, "client_metadata.session_id").String())
	require.Contains(t, string(stamped), `"sequence":900719925474099312345`, "raw JSON numbers must not be rounded by a full decode")
	require.Equal(t, "stale", gjson.GetBytes(payload, "client_metadata.x-codex-ws-stream-request-start-ms").String(), "input bytes remain reusable")
}

func TestStampOpenAIWSStreamRequestStartJSONRebuildsInvalidMetadataOnly(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_456)
	for _, payload := range [][]byte{
		[]byte(`{"type":"response.create"}`),
		[]byte(`{"type":"response.create","client_metadata":null}`),
		[]byte(`{"type":"response.create","client_metadata":"not-an-object"}`),
	} {
		stamped, changed := stampOpenAIWSStreamRequestStartJSON(payload, now)
		require.True(t, changed)
		require.Equal(t, "1700000000456", gjson.GetBytes(stamped, "client_metadata.x-codex-ws-stream-request-start-ms").String())
	}

	for _, payload := range [][]byte{
		[]byte(`{"type":"session.update","client_metadata":{"keep":"me"}}`),
		[]byte(`{"type":"response.create"`),
		[]byte(`[]`),
	} {
		stamped, changed := stampOpenAIWSStreamRequestStartJSON(payload, now)
		require.False(t, changed)
		require.Equal(t, payload, stamped)
	}
}

func TestStampOpenAIWSStreamRequestStartValueCopiesRequestTemplate(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_789)
	originalMetadata := map[string]any{"session_id": "session-1"}
	original := map[string]any{
		"type":            "response.create",
		"client_metadata": originalMetadata,
		"input":           []any{"hello"},
	}

	stampedValue, changed := stampOpenAIWSStreamRequestStartValue(original, now)
	require.True(t, changed)
	stamped, ok := stampedValue.(map[string]any)
	require.True(t, ok)
	metadata, ok := stamped["client_metadata"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "1700000000789", metadata[openAIWSStreamRequestStartMSMetadataKey])
	require.Equal(t, "session-1", metadata["session_id"])
	require.NotContains(t, originalMetadata, openAIWSStreamRequestStartMSMetadataKey)
	metadata["write-only"] = true
	require.NotContains(t, originalMetadata, "write-only")

	raw, changed := stampOpenAIWSStreamRequestStartValue(json.RawMessage(`{"type":"response.create"}`), now)
	require.True(t, changed)
	rawJSON, ok := raw.(json.RawMessage)
	require.True(t, ok)
	require.Equal(t, "1700000000789", gjson.GetBytes(rawJSON, "client_metadata.x-codex-ws-stream-request-start-ms").String())
}
