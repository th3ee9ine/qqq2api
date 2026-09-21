package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotDispatchedUpstreamRequestCapturesFinalHeaders(t *testing.T) {
	const secretTurnState = "secret-turn-state-must-not-be-captured"
	ctx, capture := withUpstreamIdentityCapture(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	request.Header.Set(openAICodexTurnStateHeader, secretTurnState)
	request.Header.Set("Originator", "origin-before-transport")
	request.Header.Set("User-Agent", "ua-before-transport")
	request.Header.Set("Version", "version-before-transport")

	request = snapshotUpstreamRequestIdentity(request)
	beforeTransport := upstreamIdentityFromRequest(request)

	request.Header.Set(openAICodexTurnStateHeader, secretTurnState)
	request.Header.Set("Originator", "origin-final")
	request.Header.Set("User-Agent", "ua-final")
	request.Header.Set("Version", "version-final")
	response := &http.Response{Request: request}
	request = snapshotDispatchedUpstreamRequest(request, response)

	require.Equal(t, "origin-before-transport", *beforeTransport.originator)
	require.Equal(t, "ua-before-transport", *beforeTransport.userAgent)
	require.Equal(t, "version-before-transport", *beforeTransport.version)

	final := capture.load()
	require.NotNil(t, final)
	require.Equal(t, "origin-final", *final.originator)
	require.Equal(t, "ua-final", *final.userAgent)
	require.Equal(t, "version-final", *final.version)
	require.Same(t, request, response.Request)
	require.Same(t, final, upstreamIdentityFromResponse(response))
	result := &OpenAIForwardResult{}
	final.applyToOpenAIResult(result)
	require.Nil(t, result.UpstreamTurnState)
}

func TestSnapshotUpstreamIdentityDistinguishesObservedMissingHeaders(t *testing.T) {
	snapshot := snapshotUpstreamIdentity(nil)
	require.NotNil(t, snapshot.originator)
	require.NotNil(t, snapshot.userAgent)
	require.NotNil(t, snapshot.version)
	require.Empty(t, *snapshot.originator)
	require.Empty(t, *snapshot.userAgent)
	require.Empty(t, *snapshot.version)
}

func TestSnapshotDispatchedUpstreamRequestPrefersFinalResponseRequest(t *testing.T) {
	ctx, capture := withUpstreamIdentityCapture(context.Background())
	original, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	original.Header.Set("User-Agent", "original-user-agent")
	original = snapshotUpstreamRequestIdentity(original)

	finalRequest, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://fallback.example.com/v1/responses", nil)
	require.NoError(t, err)
	finalRequest.Header.Set("Originator", "fallback-originator")
	response := &http.Response{Request: finalRequest}

	dispatched := snapshotDispatchedUpstreamRequest(original, response)
	final := upstreamIdentityFromResponse(response)
	require.Same(t, dispatched, response.Request)
	require.Same(t, final, capture.load(), "the original request context must observe the final transport request")
	require.Equal(t, "fallback-originator", *final.originator)
	require.Empty(t, *final.userAgent, "a header removed by the fallback request must stay absent")
	require.Equal(t, "original-user-agent", *upstreamIdentityFromRequest(original).userAgent,
		"the immutable pre-dispatch snapshot must not be mutated")
}

func TestApplyCapturedUpstreamIdentityDoesNotOverrideExplicitHandshake(t *testing.T) {
	ctx, capture := withUpstreamIdentityCapture(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/v1/responses", nil)
	require.NoError(t, err)
	request.Header.Set("User-Agent", "http-fallback")
	snapshotUpstreamRequestIdentity(request)

	wsUserAgent := "ws-handshake"
	result := &OpenAIForwardResult{UpstreamUserAgent: &wsUserAgent}
	applyCapturedUpstreamIdentityToOpenAIResult(capture, result, nil)

	require.Equal(t, "ws-handshake", *result.UpstreamUserAgent)
	require.Nil(t, result.UpstreamTurnState)
	require.Nil(t, result.UpstreamOriginator)
	require.Nil(t, result.UpstreamVersion)
}
