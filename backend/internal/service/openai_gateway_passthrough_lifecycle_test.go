package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

// passthroughLifecycleBody can keep an upstream read blocked until the handler
// closes the response body. This models a response whose headers arrived but
// whose SSE stream stopped producing bytes.
type passthroughLifecycleBody struct {
	first     []byte
	firstPos  int
	closed    chan struct{}
	closeOnce sync.Once
}

func newPassthroughLifecycleBody(first string) *passthroughLifecycleBody {
	return &passthroughLifecycleBody{
		first:  []byte(first),
		closed: make(chan struct{}),
	}
}

func (b *passthroughLifecycleBody) Read(p []byte) (int, error) {
	if b.firstPos < len(b.first) {
		n := copy(p, b.first[b.firstPos:])
		b.firstPos += n
		return n, nil
	}
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *passthroughLifecycleBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

type passthroughLifecycleWriter struct {
	gin.ResponseWriter
	failWrites bool
}

type passthroughBufferedLifecycleBody struct {
	started   chan struct{}
	release   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
	reader    *strings.Reader
}

func (b *passthroughBufferedLifecycleBody) Read(p []byte) (int, error) {
	b.startOnce.Do(func() { close(b.started) })
	select {
	case <-b.release:
		return b.reader.Read(p)
	case <-b.closed:
		return 0, io.ErrClosedPipe
	}
}

func (b *passthroughBufferedLifecycleBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func TestOpenAIPassthroughBufferedDisconnectBoundsSilentBodyRead(t *testing.T) {
	for _, status := range []int{http.StatusBadGateway, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c, _ := newPassthroughLifecycleContext()
			ctx, cancel := context.WithCancel(c.Request.Context())
			c.Request = c.Request.WithContext(ctx)
			body := &passthroughBufferedLifecycleBody{
				started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}),
				reader: strings.NewReader(""),
			}
			t.Cleanup(func() { cancel(); _ = body.Close() })
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       body,
			}}
			svc := &OpenAIGatewayService{
				cfg:          &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}},
				httpUpstream: upstream,
			}
			account := &Account{ID: 81, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-key"},
				Extra:       map[string]any{"openai_passthrough": true},
			}
			done := make(chan error, 1)
			go func() {
				_, err := svc.Forward(ctx, c, account, []byte(`{"model":"gpt-5.2","stream":false,"input":"hello"}`))
				done <- err
			}()
			select {
			case <-body.started:
			case <-time.After(3 * time.Second):
				t.Fatal("forward never started reading the response body")
			}
			// Cancel only after headers have arrived and the buffered read is
			// blocked, so the header-wait gate cannot be responsible for release.
			cancel()
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.Canceled)
				var failoverErr *UpstreamFailoverError
				require.NotErrorAs(t, err, &failoverErr)
			case <-time.After(3 * time.Second):
				t.Fatal("disconnected buffered request kept the silent upstream body open")
			}
			require.Len(t, upstream.requests, 1)
			select {
			case <-body.closed:
			default:
				t.Fatal("disconnect watchdog did not close the response body")
			}
		})
	}
}

func TestOpenAIPassthroughBufferedDisconnectStillDrainsUsage(t *testing.T) {
	c, _ := newPassthroughLifecycleContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	body := &passthroughBufferedLifecycleBody{
		started: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{}),
		reader: strings.NewReader(`{"id":"resp_buffered","status":"completed","usage":{"input_tokens":11,"output_tokens":4}}`),
	}
	t.Cleanup(func() { cancel(); _ = body.Close() })
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       body,
	}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 1}},
		httpUpstream: upstream,
	}
	account := &Account{ID: 82, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-key"},
		Extra:       map[string]any{"openai_passthrough": true},
	}
	type outcome struct {
		result *OpenAIForwardResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := svc.Forward(ctx, c, account, []byte(`{"model":"gpt-5.2","stream":false,"input":"hello"}`))
		done <- outcome{result: result, err: err}
	}()
	select {
	case <-body.started:
	case <-time.After(3 * time.Second):
		t.Fatal("forward never started reading the response body")
	}
	cancel()
	select {
	case <-body.closed:
		t.Fatal("successful buffered response was canceled before usage could drain")
	case <-time.After(25 * time.Millisecond):
	}
	close(body.release)
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.NotNil(t, got.result)
		require.True(t, got.result.ClientDisconnect)
		require.Equal(t, 11, got.result.Usage.InputTokens)
		require.Equal(t, 4, got.result.Usage.OutputTokens)
	case <-time.After(3 * time.Second):
		t.Fatal("buffered response did not finish after usage arrived")
	}
}

func (w *passthroughLifecycleWriter) Write(data []byte) (int, error) {
	if w.failWrites {
		return 0, errors.New("client disconnected")
	}
	return w.ResponseWriter.Write(data)
}

func (w *passthroughLifecycleWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func newPassthroughLifecycleContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, recorder
}

func requirePassthroughLifecycleFailoverMessage(t *testing.T, err error, want string) {
	t.Helper()
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Contains(t, string(failoverErr.ResponseBody), want)
}

func TestOpenAIPassthroughHeaderWaitGateCancelsBeforeHeadersOnly(t *testing.T) {
	clientCtx, cancel := context.WithCancel(context.Background())
	upstreamCtx, gate := newOpenAIPassthroughHeaderWaitContext(clientCtx, context.WithoutCancel(clientCtx))
	cancel()
	select {
	case <-upstreamCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("client cancellation did not cancel an upstream request waiting for headers")
	}
	gate.close()

	clientCtx, cancel = context.WithCancel(context.Background())
	upstreamCtx, gate = newOpenAIPassthroughHeaderWaitContext(clientCtx, context.WithoutCancel(clientCtx))
	gate.headersReceived()
	cancel()
	select {
	case <-upstreamCtx.Done():
		t.Fatal("client cancellation after headers must preserve the detached response body context")
	case <-time.After(25 * time.Millisecond):
	}
	gate.close()
}

func TestOpenAIPassthroughStreamingIdleClosesBlockedUpstreamBody(t *testing.T) {
	c, recorder := newPassthroughLifecycleContext()
	body := newPassthroughLifecycleBody("")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		StreamDataIntervalTimeout: 1,
		MaxLineSize:               defaultMaxLineSize,
	}}}

	started := time.Now()
	_, err := svc.handleStreamingResponsePassthrough(
		c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, started, "model", "model",
	)

	require.Error(t, err)
	requirePassthroughLifecycleFailoverMessage(t, err, "stream data interval timeout")
	require.Less(t, time.Since(started), 3*time.Second)
	select {
	case <-body.closed:
	default:
		t.Fatal("passthrough did not close the blocked upstream body")
	}
	// The timeout is handled before any semantic output, so a failover error is
	// returned while the downstream recorder remains untouched.
	require.Empty(t, recorder.Body.String())
}

func TestOpenAIPassthroughStreamingDisconnectHasBoundedDrain(t *testing.T) {
	c, _ := newPassthroughLifecycleContext()
	c.Writer = &passthroughLifecycleWriter{ResponseWriter: c.Writer, failWrites: true}
	body := newPassthroughLifecycleBody("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		StreamDataIntervalTimeout: 1,
		MaxLineSize:               defaultMaxLineSize,
	}}}

	started := time.Now()
	_, err := svc.handleStreamingResponsePassthrough(
		c.Request.Context(), resp, c, &Account{ID: 2, Platform: PlatformOpenAI}, started, "model", "model",
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "stream usage incomplete after disconnect")
	require.Less(t, time.Since(started), 3*time.Second)
	select {
	case <-body.closed:
	default:
		t.Fatal("passthrough did not close the upstream body after disconnect")
	}
}

func TestOpenAIPassthroughStreamingRequestCancellationDoesNotFailover(t *testing.T) {
	c, _ := newPassthroughLifecycleContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()
	body := newPassthroughLifecycleBody("")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		StreamDataIntervalTimeout: 1,
		MaxLineSize:               defaultMaxLineSize,
	}}}

	_, err := svc.handleStreamingResponsePassthrough(
		c.Request.Context(), resp, c, &Account{ID: 5, Platform: PlatformOpenAI}, time.Now(), "model", "model",
	)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
	require.Contains(t, err.Error(), "stream usage incomplete after disconnect")
	select {
	case <-body.closed:
	default:
		t.Fatal("passthrough did not close the body after request cancellation")
	}
}

func TestOpenAIPassthroughStreamingDisconnectBeforeFailedEventSkipsFallbackAndKeepsUsage(t *testing.T) {
	c, _ := newPassthroughLifecycleContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	c.Request.URL.Path = "/v1/responses/compact"
	cancel()
	body := newPassthroughLifecycleBody(strings.Join([]string{
		`data: {"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"code":"model_not_found","message":"compact model unavailable"},"usage":{"input_tokens":13,"output_tokens":2}}}`,
		"",
	}, "\n"))
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:               defaultMaxLineSize,
		StreamDataIntervalTimeout: 1,
	}}}

	result, err := svc.handleStreamingResponsePassthrough(
		c.Request.Context(), resp, c, &Account{ID: 6, Platform: PlatformOpenAI}, time.Now(), "gpt-5.2", "gpt-5.2",
	)

	require.Error(t, err)
	require.NotNil(t, result)
	require.True(t, result.clientDisconnected)
	require.Equal(t, 13, result.usage.InputTokens)
	require.Equal(t, 2, result.usage.OutputTokens)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
	var compactSignal *openAICompactFallbackSignal
	require.NotErrorAs(t, err, &compactSignal)
}

func TestOpenAIPassthroughStreamingDisconnectBeforeEmptyCompletedSkipsFailover(t *testing.T) {
	c, _ := newPassthroughLifecycleContext()
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()
	body := newPassthroughLifecycleBody("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_empty\"}}\n\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize:               defaultMaxLineSize,
		StreamDataIntervalTimeout: 1,
	}}}

	result, err := svc.handleStreamingResponsePassthrough(
		c.Request.Context(), resp, c, &Account{ID: 7, Platform: PlatformOpenAI}, time.Now(), "gpt-5.2", "gpt-5.2",
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.clientDisconnected)
	require.Zero(t, result.usage.InputTokens)
	require.Zero(t, result.usage.OutputTokens)
}

func TestOpenAIPassthroughStreamingPreOutputStagingLimitClosesBody(t *testing.T) {
	c, _ := newPassthroughLifecycleContext()
	// A comment line is valid SSE but cannot start semantic output. Its size
	// exceeds the aggregate pre-output staging budget while staying below the
	// scanner's configured per-line limit.
	payload := strings.Repeat(":", openAIFirstOutputStageMaxBytes+1) + "\n"
	body := newPassthroughLifecycleBody(payload)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: 2 * openAIFirstOutputStageMaxBytes,
	}}}

	_, err := svc.handleStreamingResponsePassthrough(
		context.Background(), resp, c, &Account{ID: 3, Platform: PlatformOpenAI}, time.Now(), "model", "model",
	)

	require.Error(t, err)
	requirePassthroughLifecycleFailoverMessage(t, err, "pre-output staging limit exceeded")
	select {
	case <-body.closed:
	default:
		t.Fatal("passthrough did not close the body after staging limit")
	}
}

func TestOpenAIPassthroughStreamingLineLimitClosesBody(t *testing.T) {
	c, _ := newPassthroughLifecycleContext()
	body := newPassthroughLifecycleBody("data: " + strings.Repeat("a", 128*1024) + "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{
		MaxLineSize: 64 * 1024,
	}}}

	_, err := svc.handleStreamingResponsePassthrough(
		context.Background(), resp, c, &Account{ID: 4, Platform: PlatformOpenAI}, time.Now(), "model", "model",
	)

	require.Error(t, err)
	require.ErrorIs(t, err, bufio.ErrTooLong)
	select {
	case <-body.closed:
	default:
		t.Fatal("passthrough did not close the body after line limit")
	}
}
