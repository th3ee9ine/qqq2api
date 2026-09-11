package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDebugWorkbenchTraceCapturesActualBodiesAndHeaderDecisions(t *testing.T) {
	input := http.Header{"Accept-Language": {"zh-CN"}, "User-Agent": {"editor"}, "X-Unknown": {"private-custom"}, "Authorization": {"Bearer editor-secret"}}
	trace := NewDebugWorkbenchTrace(input, nil)
	req, _ := http.NewRequestWithContext(trace.Context(context.Background()), http.MethodPost, "https://example.com/responses?token=private-query", strings.NewReader(`{"model":"custom-model","input":"完整参数","stream":false}`))
	req.Header = http.Header{"Accept-Language": {"zh-CN"}, "User-Agent": {"native"}, "Authorization": {"Bearer actual-secret"}, "X-Client-Request-Id": {"request-uuid"}}
	capture := trace.StartAttempt(req, "http://username:proxy-secret@proxy.example:8080", 42)
	response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"session=secret-cookie"}, "X-Codex-Turn-State": {"opaque-state-secret"}}, Body: io.NopCloser(strings.NewReader(`{"output":"real reply","echo":"actual-secret"}`))}
	capture.Finish(response, nil)
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, string(data), "actual-secret", "capture must not change the upstream body")
	require.NoError(t, response.Body.Close())
	attempts := trace.Attempts()
	require.Len(t, attempts, 1)
	attempt := attempts[0]
	require.JSONEq(t, `{"model":"custom-model","input":"完整参数","stream":false}`, string(attempt.Request.Body))
	require.Contains(t, string(attempt.Response.Body), "real reply")
	require.Contains(t, string(attempt.Response.Body), "[redacted]")
	require.True(t, attempt.Response.Complete)
	require.False(t, attempt.Response.Truncated)
	require.NotNil(t, attempt.TTFTMS)
	actions := map[string]string{}
	for _, h := range attempt.HeaderChanges {
		actions[h.Name] = h.Action
	}
	require.Equal(t, "preserved", actions["Accept-Language"])
	require.Equal(t, "rewritten", actions["User-Agent"])
	require.Equal(t, "generated", actions["X-Client-Request-Id"])
	require.Equal(t, "filtered", actions["X-Unknown"])
	require.Equal(t, "opaque-state-secret", trace.LastTurnState())
	encoded, err := json.Marshal(attempts)
	require.NoError(t, err)
	for _, secret := range []string{"actual-secret", "editor-secret", "private-custom", "private-query", "proxy-secret", "username:", "opaque-state-secret", "secret-cookie"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestDebugWorkbenchTraceBoundedCaptureDoesNotTruncateForwarding(t *testing.T) {
	trace := NewDebugWorkbenchTrace(nil, nil)
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", strings.NewReader(`{}`))
	capture := trace.StartAttempt(req, "", 1)
	body := bytes.Repeat([]byte("x"), DebugWorkbenchCaptureLimit+1024)
	response := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(body))}
	capture.Finish(response, nil)
	forwarded, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Len(t, forwarded, len(body))
	require.NoError(t, response.Body.Close())
	snapshot := trace.Attempts()[0].Response
	require.True(t, snapshot.Truncated)
	require.True(t, snapshot.Complete)
	require.Equal(t, int64(len(body)), snapshot.BodyBytes)
	require.Equal(t, DebugWorkbenchCaptureLimit, snapshot.CapturedBytes)
	require.Contains(t, strings.Join(trace.Warnings(), " "), "8 MiB")
	writer := NewDebugWorkbenchResponseWriter()
	n, err := writer.Write(body)
	require.NoError(t, err)
	require.Equal(t, len(body), n)
	require.Equal(t, int64(len(body)), writer.TotalBytes())
	require.Len(t, writer.Body(), DebugWorkbenchCaptureLimit)
	require.True(t, writer.Snapshot(trace).Truncated)
}

func TestDebugWorkbenchTraceRetriesPartialResponseAndError(t *testing.T) {
	trace := NewDebugWorkbenchTrace(nil, []string{"secret-token"})
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	first := trace.StartAttempt(req, "", 1)
	first.Finish(nil, errors.New("upstream secret-token failed"))
	second := trace.StartAttempt(req, "", 1)
	resp := &http.Response{StatusCode: 502, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("partial response"))}
	second.Finish(resp, nil)
	buf := make([]byte, 7)
	_, err := resp.Body.Read(buf)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	attempts := trace.Attempts()
	require.Len(t, attempts, 2)
	require.Equal(t, "upstream [redacted] failed", attempts[0].Error)
	require.Nil(t, attempts[0].Response)
	require.False(t, attempts[1].Response.Complete)
	require.Equal(t, "partial", attempts[1].Response.BodyText)
	for i := 0; i < 10; i++ {
		a := trace.StartAttempt(req, "", 1)
		a.Finish(nil, nil)
	}
	require.Len(t, trace.Attempts(), 8)
	require.Contains(t, strings.Join(trace.Warnings(), " "), "前 8 次")
}

func TestDebugWorkbenchCancellationDoesNotChangeNormalGatewayDetach(t *testing.T) {
	normal, cancel := context.WithCancel(context.Background())
	normalUpstream, release := detachUpstreamContext(normal)
	defer release()
	trace := NewDebugWorkbenchTrace(nil, nil)
	debugUpstream, releaseDebug := detachUpstreamContext(trace.Context(normal))
	defer releaseDebug()
	debugStream, releaseStream := detachStreamUpstreamContext(trace.Context(normal), true)
	defer releaseStream()
	cancel()
	require.NoError(t, normalUpstream.Err())
	require.ErrorIs(t, debugUpstream.Err(), context.Canceled)
	require.ErrorIs(t, debugStream.Err(), context.Canceled)
}

func TestDebugWorkbenchTraceDisabledHasNoCapture(t *testing.T) {
	var trace *DebugWorkbenchTrace
	capture := trace.StartAttempt(nil, "", 1)
	require.Nil(t, capture)
	capture.Finish(nil, nil)
	capture.MarkPlugin()
}

func TestDebugWorkbenchTraceRedactsEscapedJSONAndSSE(t *testing.T) {
	secret := "credential\\\"escape"
	trace := NewDebugWorkbenchTrace(nil, []string{secret})
	body, err := json.Marshal(map[string]any{"echo": secret, "value": json.Number("1234567890123456789"), "api_key": "hidden"})
	require.NoError(t, err)
	snapshot := trace.ResponseSnapshot(200, http.Header{}, body, int64(len(body)), true)
	require.JSONEq(t, `{"echo":"[redacted]","value":1234567890123456789,"api_key":"[redacted]"}`, string(snapshot.Body))
	sse := append([]byte("data: "), body...)
	snapshot = trace.ResponseSnapshot(200, http.Header{"Content-Type": {"text/event-stream"}}, sse, int64(len(sse)), true)
	require.NotContains(t, snapshot.BodyText, "credential")
	require.Contains(t, snapshot.BodyText, "1234567890123456789")
}

func TestDebugWorkbenchTraceAgentIdentityAndProxyErrors(t *testing.T) {
	trace := NewDebugWorkbenchTrace(nil, nil)
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	req.Header.Set("Authorization", "AgentAssertion encoded-envelope-secret")
	a := trace.StartAttempt(req, "http://proxyuser:x@proxy.example:8080", 1)
	a.Finish(nil, errors.New("encoded-envelope-secret failed at http://proxyuser:x@proxy.example:8080/path?token=other-secret"))
	result, _ := json.Marshal(trace.Attempts())
	for _, secret := range []string{"encoded-envelope-secret", "proxyuser:x", "other-secret"} {
		require.NotContains(t, string(result), secret)
	}
}

func TestDebugWorkbenchTraceOverflowNeverReplaysEarlierTurnState(t *testing.T) {
	trace := NewDebugWorkbenchTrace(nil, nil)
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	for i := 0; i < 9; i++ {
		a := trace.StartAttempt(req, "", 1)
		a.Finish(&http.Response{StatusCode: 500, Header: http.Header{"X-Codex-Turn-State": {"old-state"}}}, nil)
	}
	require.Empty(t, trace.LastTurnState())
}
