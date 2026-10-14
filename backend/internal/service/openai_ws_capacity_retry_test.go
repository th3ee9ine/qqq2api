package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

const wsCapacityErrorEvent = `{"type":"error","error":{"type":"server_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`
const wsCapacityFailedEvent = `{"type":"response.failed","response":{"id":"resp_capacity","status":"failed","error":{"type":"server_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`

type wsCapacityRecordingDialer struct {
	mu        sync.Mutex
	events    [][]byte
	handshake bool
	dials     int
	conns     []*openAIWSCaptureConn
}

func (d *wsCapacityRecordingDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dials++
	headers := http.Header{"Retry-After": {"2"}, "X-Request-Id": {"capacity-test"}}
	if d.handshake {
		return nil, http.StatusServiceUnavailable, headers, &openAIWSHandshakeError{
			Body: []byte(wsCapacityErrorEvent), Err: errors.New("handshake rejected"),
		}
	}
	conn := &openAIWSCaptureConn{events: append([][]byte(nil), d.events...)}
	d.conns = append(d.conns, conn)
	return conn, http.StatusSwitchingProtocols, headers, nil
}

func (d *wsCapacityRecordingDialer) counts() (dials, creates int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, conn := range d.conns {
		conn.mu.Lock()
		for _, payload := range conn.writes {
			if payload["type"] == "response.create" {
				creates++
			}
		}
		conn.mu.Unlock()
	}
	return d.dials, creates
}

func newWSCapacityTestService(t *testing.T, cfg *config.Config, dialer openAIWSClientDialer) (*OpenAIGatewayService, *Account) {
	t.Helper()
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.RetryBackoffInitialMS = 1
	cfg.Gateway.OpenAIWS.RetryBackoffMaxMS = 1
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	t.Cleanup(pool.Close)
	svc := &OpenAIGatewayService{
		cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool,
	}
	account := &Account{
		ID: 5884, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "test-key"}, Extra: map[string]any{"responses_websockets_v2_enabled": true},
	}
	return svc, account
}

// Exercise the full HTTP Forward path with a recording WS dialer. Each caller-
// authorized attempt must make one upstream attempt, without multiplying the
// handler's 1/3-attempt allowance by the internal WS reconnect limit.
func TestOpenAIWSV2CapacityDoesNotAmplifyCallerRetryBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, limit := range []int{1, 3} {
		for _, tc := range []struct {
			name      string
			event     string
			prewarm   bool
			handshake bool
		}{
			{name: "error", event: wsCapacityErrorEvent},
			{name: "response_failed", event: wsCapacityFailedEvent},
			{name: "response_incomplete", event: strings.ReplaceAll(wsCapacityFailedEvent, "failed", "incomplete")},
			{name: "response_cancelled", event: strings.ReplaceAll(wsCapacityFailedEvent, "failed", "cancelled")},
			{name: "response_canceled", event: strings.ReplaceAll(wsCapacityFailedEvent, "failed", "canceled")},
			{name: "prewarm_error", event: wsCapacityErrorEvent, prewarm: true},
			{name: "prewarm_failed", event: wsCapacityFailedEvent, prewarm: true},
			{name: "prewarm_incomplete", event: strings.ReplaceAll(wsCapacityFailedEvent, "failed", "incomplete"), prewarm: true},
			{name: "handshake_503", handshake: true},
		} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("attempts_%d/%s/stream_%t", limit, tc.name, stream), func(t *testing.T) {
					cfg := newOpenAIWSV2TestConfig()
					cfg.Gateway.OpenAICapacityRetryMaxAttempts = limit
					cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = tc.prewarm
					dialer := &wsCapacityRecordingDialer{events: [][]byte{[]byte(tc.event)}, handshake: tc.handshake}
					svc, account := newWSCapacityTestService(t, cfg, dialer)
					for attempt := 0; attempt < limit; attempt++ {
						recorder := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(recorder)
						c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
						body := []byte(fmt.Sprintf(`{"model":"gpt-5.5","input":"hello","stream":%t}`, stream))
						result, err := svc.Forward(context.Background(), c, account, body)
						require.Nil(t, result)
						var failoverErr *UpstreamFailoverError
						require.ErrorAs(t, err, &failoverErr)
						require.True(t, failoverErr.IsOpenAICapacityShed())
						require.Equal(t, http.StatusServiceUnavailable, failoverErr.ClientStatusCode)
						require.Equal(t, "2", failoverErr.ResponseHeaders.Get("Retry-After"))
						require.False(t, c.Writer.Written(), "the handler must receive the error before any response is committed")
						require.Empty(t, recorder.Body.String())
						require.False(t, svc.writeOpenAIWSFallbackErrorResponse(c, account, err))
					}
					dials, creates := dialer.counts()
					require.Equal(t, limit, dials)
					if tc.handshake {
						require.Zero(t, creates)
					} else {
						require.Equal(t, limit, creates)
					}
					require.Zero(t, svc.SnapshotOpenAIWSRetryMetrics().RetryAttemptsTotal)
				})
			}
		}
	}
}

func TestOpenAIWSV2CapacityAfterOutputDoesNotReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, terminal := range []string{wsCapacityErrorEvent, wsCapacityFailedEvent, strings.ReplaceAll(wsCapacityFailedEvent, "failed", "incomplete")} {
		dialer := &wsCapacityRecordingDialer{events: [][]byte{
			[]byte(`{"type":"response.output_text.delta","response_id":"resp_partial","delta":"already visible"}`),
			[]byte(terminal),
		}}
		svc, account := newWSCapacityTestService(t, newOpenAIWSV2TestConfig(), dialer)
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		_, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","input":"hello","stream":true}`))
		var failoverErr *UpstreamFailoverError
		require.False(t, errors.As(err, &failoverErr))
		require.True(t, c.Writer.Written())
		require.Contains(t, recorder.Body.String(), "already visible")
		dials, creates := dialer.counts()
		require.Equal(t, 1, dials)
		require.Equal(t, 1, creates)
	}
}

func TestOpenAIWSV2NetworkReadFailureStillReconnects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dialer := &openAIWSQueueDialer{conns: []openAIWSClientConn{
		&openAIWSCaptureConn{}, // EOF before output: an ordinary transport failure.
		&openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"id":"resp_retry","model":"gpt-5.5","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`)}},
	}}
	svc, account := newWSCapacityTestService(t, newOpenAIWSV2TestConfig(), dialer)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	result, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","input":"hello","stream":false}`))
	require.NoError(t, err)
	require.Equal(t, "resp_retry", result.RequestID)
	require.Equal(t, 2, dialer.DialCount())
	require.EqualValues(t, 1, svc.SnapshotOpenAIWSRetryMetrics().RetryAttemptsTotal)
}
