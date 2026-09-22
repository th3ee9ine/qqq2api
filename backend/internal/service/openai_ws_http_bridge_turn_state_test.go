package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func bridgeTurnStateResponse(t *testing.T, model string, state string) *http.Response {
	t.Helper()
	body := "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_bridge_turn_state\",\"model\":\"" + model + "\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"
	headers := http.Header{"Content-Type": []string{"text/event-stream"}}
	if state != "" {
		headers.Set(openAIWSTurnStateHeader, state)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     headers,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestProxyOpenAIWSHTTPBridgeTurnRetiresTurnStateOnResponseModelMismatch(t *testing.T) {
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 241)
	upstream := &httpUpstreamRecorder{resp: bridgeTurnStateResponse(t, "gpt-6-astra", state)}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(1201)
	c := newCodexTurnStateGatewayTestContext(t, "bridge-mismatch-scope")
	c.Request.Header.Set(openAIWSTurnStateHeader, state)
	payload := []byte(`{"type":"response.create","model":"gpt-5.5","stream":true,"input":"hello"}`)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, state, "seed", time.Now()))

	result, err := svc.proxyOpenAIWSHTTPBridgeTurn(
		context.Background(), c, account, "test-access-token", payload, len(payload),
		"gpt-5.5", "", "", "", "", 1,
		func([]byte) error { return nil },
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.ResponseHeaders.Get(openAIWSTurnStateHeader))
	require.Empty(t, c.Request.Header.Get(openAIWSTurnStateHeader))
	require.True(t, openAIWSTurnStateModelMismatchMarked(c))
	_, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.False(t, usable, "a bridge response model mismatch must retire the active state")
}

func TestProxyOpenAIWSHTTPBridgeTurnPublishesOnlySuccessfulResponseState(t *testing.T) {
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 242)
	upstream := &httpUpstreamRecorder{resp: bridgeTurnStateResponse(t, "gpt-5.5", state)}
	svc := newCodexTurnStateGatewayTestService(upstream)
	svc.SetCodexTurnStateRuntimeSettings(false, true)
	account := codexTurnStateGatewayTestAccount(1202)
	c := newCodexTurnStateGatewayTestContext(t, "bridge-success-scope")
	payload := []byte(`{"type":"response.create","model":"gpt-5.5","stream":true,"input":"hello"}`)

	result, err := svc.proxyOpenAIWSHTTPBridgeTurn(
		context.Background(), c, account, "test-access-token", payload, len(payload),
		"gpt-5.5", "", "", "", "", 1,
		func([]byte) error { return nil },
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, state, result.ResponseHeaders.Get(openAIWSTurnStateHeader))
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	snapshot, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.True(t, usable)
	require.Equal(t, state, snapshot.Token.Value)
	require.False(t, svc.codexTurnStateCollector.Status(key, time.Now()).Ready, "a successful first observation should become active, not a standby backlog")
}

func TestProxyOpenAIWSHTTPBridgeTurnDropsInvalidResponseState(t *testing.T) {
	active := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 243)
	upstream := &httpUpstreamRecorder{resp: bridgeTurnStateResponse(t, "gpt-5.5", "malformed-turn-state")}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(1203)
	c := newCodexTurnStateGatewayTestContext(t, "bridge-invalid-state-scope")
	c.Request.Header.Set(openAIWSTurnStateHeader, active)
	payload := []byte(`{"type":"response.create","model":"gpt-5.5","stream":true,"input":"hello"}`)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, active, "seed", time.Now()))

	result, err := svc.proxyOpenAIWSHTTPBridgeTurn(
		context.Background(), c, account, "test-access-token", payload, len(payload),
		"gpt-5.5", "", "", "", "", 1,
		func([]byte) error { return nil },
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Empty(t, result.ResponseHeaders.Get(openAIWSTurnStateHeader))
	snapshot, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.True(t, usable, "an invalid replacement response must not retire a still-valid active state")
	require.Equal(t, active, snapshot.Token.Value)
	require.Equal(t, "invalid_state", svc.codexTurnStateLastError.Load())
}
