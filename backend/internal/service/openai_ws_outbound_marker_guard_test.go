//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeLegacyOpenAIOutboundWSResponseCreateJSONPreservesPayloadAndIsIdempotent(t *testing.T) {
	instructions := "keep-prefix " + codexImageGenerationBridgeLegacyMarker + "bridge" + codexImageGenerationBridgeLegacyClosingMarker + " keep-suffix"
	wantInstructions := "keep-prefix " + codexImageGenerationBridgeMarker + "bridge" + codexImageGenerationBridgeClosingMarker + " keep-suffix"
	payload := []byte(`{"type":"response.create","instructions":` + strconv.Quote(instructions) + `,"unknown":{"sequence":900719925474099312345,"literal":"sub2api-not-a-retired-marker"},"tools":[{"name":"` + codexPythonToolLegacyAlias + `"}]}`)
	want := []byte(`{"type":"response.create","instructions":` + strconv.Quote(wantInstructions) + `,"unknown":{"sequence":900719925474099312345,"literal":"sub2api-not-a-retired-marker"},"tools":[{"name":"` + codexPythonToolAlias + `"}]}`)

	normalized, changed := normalizeLegacyOpenAIOutboundWSResponseCreateJSON(payload)

	require.True(t, changed)
	require.Equal(t, want, normalized)

	normalizedAgain, changedAgain := normalizeLegacyOpenAIOutboundWSResponseCreateJSON(normalized)
	require.False(t, changedAgain)
	require.Equal(t, normalized, normalizedAgain)
}

func TestNormalizeLegacyOpenAIOutboundWSResponseCreateJSONSkipsInvalidUnrelatedAndNearMarkers(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "invalid json",
			payload: []byte(`{"type":"response.create","instructions":"` + codexImageGenerationBridgeLegacyMarker + `"`),
		},
		{
			name:    "unrelated event",
			payload: []byte(`{"type":"session.update","instructions":"` + codexImageGenerationBridgeLegacyMarker + `"}`),
		},
		{
			name:    "near marker only",
			payload: []byte(`{"type":"response.create","instructions":"<sub2api-codex-image-generation-extra>","tools":[{"name":"` + codexPythonToolLegacyAlias + `_extra"}]}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, changed := normalizeLegacyOpenAIOutboundWSResponseCreateJSON(tt.payload)
			require.False(t, changed)
			require.Equal(t, tt.payload, normalized)
		})
	}
}

func TestOpenAIWSPooledWriteAppliesFinalResponseCreateMarkerGuard(t *testing.T) {
	upstream := &openAIWSOutboundMarkerCaptureConn{}
	conn := newOpenAIWSConn("marker-guard", 0, upstream, nil)
	payload := map[string]any{
		"type":         "response.create",
		"instructions": "keep " + openAICompatClaudeCodeTodoGuardLegacyMarker + "todo" + openAICompatClaudeCodeTodoGuardLegacyClosingMarker,
		"unknown": map[string]any{
			"literal": "preserve-me",
		},
	}

	require.NoError(t, conn.writeJSON(payload, context.Background()))
	require.Len(t, upstream.jsonWrites, 1)
	var written map[string]any
	require.NoError(t, json.Unmarshal(upstream.jsonWrites[0], &written))
	require.Equal(t, "keep "+openAICompatClaudeCodeTodoGuardMarker+"todo"+openAICompatClaudeCodeTodoGuardClosingMarker, written["instructions"])
	require.Equal(t, "preserve-me", written["unknown"].(map[string]any)["literal"])
	require.NotEmpty(t, written["client_metadata"].(map[string]any)[openAIWSStreamRequestStartMSMetadataKey])
}

func TestOpenAIWSPassthroughWriteAppliesFinalResponseCreateMarkerGuardOnly(t *testing.T) {
	inner := &openAIWSOutboundMarkerCaptureConn{}
	guard := &openAIWSOutboundMarkerGuardFrameConn{inner: inner}
	responseCreate := []byte(`{"type":"response.create","instructions":"` + codexSparkImageUnsupportedLegacyMarker + `keep` + codexSparkImageUnsupportedLegacyClosingMarker + `","unknown":"preserve-me"}`)
	unrelated := []byte(`{"type":"session.update","instructions":"` + codexSparkImageUnsupportedLegacyMarker + `"}`)
	invalid := []byte(`{"type":"response.create","instructions":"` + codexSparkImageUnsupportedLegacyMarker + `"`)

	require.NoError(t, guard.WriteFrame(context.Background(), coderws.MessageText, responseCreate))
	require.NoError(t, guard.WriteFrame(context.Background(), coderws.MessageText, unrelated))
	require.NoError(t, guard.WriteFrame(context.Background(), coderws.MessageBinary, invalid))

	require.Len(t, inner.frameWrites, 3)
	require.Equal(t, "response.create", gjson.GetBytes(inner.frameWrites[0], "type").String())
	require.Equal(t, codexSparkImageUnsupportedMarker+"keep"+codexSparkImageUnsupportedClosingMarker, gjson.GetBytes(inner.frameWrites[0], "instructions").String())
	require.Equal(t, "preserve-me", gjson.GetBytes(inner.frameWrites[0], "unknown").String())
	require.NotEmpty(t, gjson.GetBytes(inner.frameWrites[0], "client_metadata."+openAIWSStreamRequestStartMSMetadataKey).String())
	require.Equal(t, unrelated, inner.frameWrites[1])
	require.Equal(t, invalid, inner.frameWrites[2])
}

type openAIWSOutboundMarkerCaptureConn struct {
	jsonWrites  [][]byte
	frameWrites [][]byte
}

func (c *openAIWSOutboundMarkerCaptureConn) WriteJSON(_ context.Context, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.jsonWrites = append(c.jsonWrites, append([]byte(nil), encoded...))
	return nil
}

func (c *openAIWSOutboundMarkerCaptureConn) ReadMessage(context.Context) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (c *openAIWSOutboundMarkerCaptureConn) Ping(context.Context) error { return nil }

func (c *openAIWSOutboundMarkerCaptureConn) ReadFrame(context.Context) (coderws.MessageType, []byte, error) {
	return coderws.MessageText, nil, errors.New("not implemented")
}

func (c *openAIWSOutboundMarkerCaptureConn) WriteFrame(_ context.Context, _ coderws.MessageType, payload []byte) error {
	c.frameWrites = append(c.frameWrites, append([]byte(nil), payload...))
	return nil
}

func (c *openAIWSOutboundMarkerCaptureConn) Close() error { return nil }
