package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
	"github.com/tidwall/gjson"
)

type ttftProbeUpstreamFunc func(*http.Request, string, int64, int) (*http.Response, error)

func (f ttftProbeUpstreamFunc) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	return f(req, proxy, accountID, concurrency)
}

func (f ttftProbeUpstreamFunc) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return f(req, proxy, accountID, concurrency)
}

func newTTFTProbeAccount() *Account {
	return &Account{
		ID: 101, Name: "local-ttft-probe", Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"access_token": "test-oauth-token", "chatgpt_account_id": "test-account"},
		Extra:       map[string]any{"openai_ws_force_http": true},
	}
}

func ttftProbeStreamResponse(events string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(events))}
}

const ttftProbeSuccessfulEvents = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n" +
	"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_probe\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"OK\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"

func TestProbeOpenAIOAuthTTFTForwardsRealResponsesRequest(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		name := "managed"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			account := newTTFTProbeAccount()
			account.Extra["openai_passthrough"] = passthrough
			calls := 0
			upstream := ttftProbeUpstreamFunc(func(req *http.Request, _ string, accountID int64, concurrency int) (*http.Response, error) {
				calls++
				require.Equal(t, account.ID, accountID)
				require.Equal(t, account.Concurrency, concurrency)
				require.Equal(t, "/backend-api/codex/responses", req.URL.Path)
				require.Equal(t, "Bearer test-oauth-token", req.Header.Get("Authorization"))
				require.NotEmpty(t, req.Header.Get("User-Agent"))
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.True(t, gjson.GetBytes(body, "stream").Bool())
				require.False(t, gjson.GetBytes(body, "store").Bool())
				require.Contains(t, string(body), "Reply with OK.")
				deadline, ok := req.Context().Deadline()
				require.True(t, ok)
				require.Greater(t, time.Until(deadline), 309*time.Second)
				require.LessOrEqual(t, time.Until(deadline), 310*time.Second)
				return ttftProbeStreamResponse(ttftProbeSuccessfulEvents), nil
			})
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			result, err := svc.ProbeOpenAIOAuthTTFT(context.Background(), account, "gpt-5.4", 300)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, result.FirstTokenMs)
			require.Equal(t, 1, calls)
		})
	}
}

func TestProbeOpenAIOAuthTTFTRejectsIncompleteOrFailedStreams(t *testing.T) {
	for _, tt := range []struct{ name, events string }{
		{"missing terminal", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"OK\"}\n\n"},
		{"failed terminal", "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_error\",\"message\":\"local failure\"}}}\n\n"},
		{"missing first token", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: ttftProbeUpstreamFunc(func(*http.Request, string, int64, int) (*http.Response, error) {
				return ttftProbeStreamResponse(tt.events), nil
			})}
			_, err := svc.ProbeOpenAIOAuthTTFT(context.Background(), newTTFTProbeAccount(), "gpt-5.4", 15)
			require.Error(t, err)
		})
	}
}

func TestProbeOpenAIOAuthTTFTRejectsInvalidAccountsBeforeUpstream(t *testing.T) {
	parent := int64(1)
	for _, account := range []*Account{nil, {Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, {Platform: PlatformAnthropic, Type: AccountTypeOAuth}, {Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent}} {
		svc := &OpenAIGatewayService{}
		_, err := svc.ProbeOpenAIOAuthTTFT(context.Background(), account, "gpt-5.4")
		require.ErrorIs(t, err, errOpenAIOAuthTTFTProbeInvalidAccount)
	}
}

func TestOpenAIOAuthTTFTProbeDeadlineAndLeaseScaleTogether(t *testing.T) {
	for _, tt := range []struct {
		threshold      int
		timeout, lease time.Duration
	}{
		{1, 30 * time.Second, 2 * time.Minute},
		{15, 30 * time.Second, 2 * time.Minute},
		{120, 130 * time.Second, 160 * time.Second},
		{300, 310 * time.Second, 340 * time.Second},
		{301, 310 * time.Second, 340 * time.Second},
	} {
		require.Equal(t, tt.timeout, openAIOAuthTTFTProbeTimeoutForThreshold(tt.threshold))
		require.Equal(t, tt.lease, openAIOAuthTTFTProbeLeaseDurationForThreshold(tt.threshold))
		require.Greater(t, tt.lease, tt.timeout+openAIOAuthTTFTRecoveryOuterGrace)
	}
}

func TestProbeOpenAIOAuthTTFTUsesMeasuredLatencyForRecovery(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: ttftProbeUpstreamFunc(func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		timer := time.NewTimer(1100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return ttftProbeStreamResponse(ttftProbeSuccessfulEvents), nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	})}
	result, err := svc.ProbeOpenAIOAuthTTFT(context.Background(), newTTFTProbeAccount(), "gpt-5.4", 1)
	require.ErrorIs(t, err, errOpenAIOAuthTTFTProbeTooSlow)
	require.NotNil(t, result)
	require.NotNil(t, result.FirstTokenMs)
	require.Greater(t, *result.FirstTokenMs, 1000)
}

func TestOpenAIOAuthTTFTProbeContextPreservesOnlyProbeCancellation(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	normal, releaseNormal := detachUpstreamContext(parent)
	defer releaseNormal()
	probe, releaseProbe := detachUpstreamContext(context.WithValue(parent, openAIOAuthTTFTProbeContextKey{}, true))
	defer releaseProbe()
	_, normalDeadline := normal.Deadline()
	_, probeDeadline := probe.Deadline()
	require.False(t, normalDeadline)
	require.True(t, probeDeadline)
	cancel()
	require.NoError(t, normal.Err())
	require.ErrorIs(t, probe.Err(), context.Canceled)
}

func TestProbeOpenAIOAuthTTFTHonorsCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: ttftProbeUpstreamFunc(func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		parentDeadline, _ := ctx.Deadline()
		require.Equal(t, parentDeadline, deadline)
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	_, err := svc.ProbeOpenAIOAuthTTFT(ctx, newTTFTProbeAccount(), "gpt-5.4", 300)
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

type ttftProbeCancelWSConn struct {
	*openAIWSCaptureConn
	cancel context.CancelFunc
	reads  int
}

func (c *ttftProbeCancelWSConn) ReadMessage(ctx context.Context) ([]byte, error) {
	c.reads++
	if c.reads == 2 {
		c.cancel()
		return nil, context.Canceled
	}
	return c.openAIWSCaptureConn.ReadMessage(ctx)
}

func TestProbeOpenAIOAuthTTFTWebSocketFacadeSuccessAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "cancellation does not drain"}[canceled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			capture := &openAIWSCaptureConn{events: [][]byte{
				[]byte(`{"type":"response.output_text.delta","delta":"OK"}`),
				[]byte(`{"type":"response.completed","response":{"id":"resp_probe_ws","status":"completed","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`),
			}}
			var conn openAIWSClientConn = capture
			var cancelConn *ttftProbeCancelWSConn
			if canceled {
				cancelConn = &ttftProbeCancelWSConn{openAIWSCaptureConn: capture, cancel: cancel}
				conn = cancelConn
			}
			cfg := newOpenAIWSTurnStateLifecycleConfig()
			svc, store := newOpenAIWSTurnStateLifecycleService(t, cfg, &openAIWSClientConnCancelDialer{conn: conn})
			svc.httpUpstream = ttftProbeUpstreamFunc(func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				t.Error("WS facade probe unexpectedly fell back to HTTP")
				return nil, errors.New("unexpected HTTP fallback")
			})
			account := newTTFTProbeAccount()
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true}
			result, err := svc.ProbeOpenAIOAuthTTFT(ctx, account, "gpt-5.4", 300)
			if canceled {
				require.Error(t, err)
				require.Equal(t, 2, cancelConn.reads, "a canceled health probe must not drain another terminal event")
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.OpenAIWSMode)
				require.Equal(t, "response.completed", result.UpstreamTerminalEvent)
				require.NotNil(t, result.FirstTokenMs)
				_, found := store.GetResponseConn("resp_probe_ws")
				require.False(t, found, "probe must not create public continuation affinity")
			}
		})
	}
}

func TestValidateOpenAIOAuthTTFTProbeResult(t *testing.T) {
	fast := 14999
	boundary := openAIOAuthTTFTProbeMaxMillis
	slow := openAIOAuthTTFTProbeMaxMillis + 1
	tests := []struct {
		name    string
		result  *OpenAIForwardResult
		wantErr error
	}{
		{name: "nil result", wantErr: errOpenAIOAuthTTFTProbeNoResult},
		{name: "missing first token", result: &OpenAIForwardResult{}, wantErr: errOpenAIOAuthTTFTProbeNoFirstToken},
		{name: "negative first token", result: &OpenAIForwardResult{FirstTokenMs: func() *int { v := -1; return &v }()}, wantErr: errOpenAIOAuthTTFTProbeNoFirstToken},
		{name: "failed websocket terminal", result: &OpenAIForwardResult{OpenAIWSMode: true, UpstreamTerminalEvent: "response.failed", FirstTokenMs: &fast}, wantErr: errOpenAIOAuthTTFTProbeNotTerminal},
		{name: "too slow", result: &OpenAIForwardResult{FirstTokenMs: &slow}, wantErr: errOpenAIOAuthTTFTProbeTooSlow},
		{name: "threshold boundary", result: &OpenAIForwardResult{FirstTokenMs: &boundary}},
		{name: "fast HTTP", result: &OpenAIForwardResult{FirstTokenMs: &fast}},
		{name: "successful websocket terminal", result: &OpenAIForwardResult{OpenAIWSMode: true, UpstreamTerminalEvent: "response.completed", FirstTokenMs: &fast}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpenAIOAuthTTFTProbeResult(tt.result)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validate result: unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate result error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOpenAIOAuthTTFTProbeResultUsesConfiguredThreshold(t *testing.T) {
	latency := 20_001
	result := &OpenAIForwardResult{FirstTokenMs: &latency}
	if err := validateOpenAIOAuthTTFTProbeResult(result, 20_000); !errors.Is(err, errOpenAIOAuthTTFTProbeTooSlow) {
		t.Fatalf("validate configured threshold error = %v, want %v", err, errOpenAIOAuthTTFTProbeTooSlow)
	}
	if err := validateOpenAIOAuthTTFTProbeResult(result, 21_000); err != nil {
		t.Fatalf("validate configured threshold: unexpected error: %v", err)
	}
}
