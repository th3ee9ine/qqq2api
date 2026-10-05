package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const passthroughCacheOptionsRequest = `{"type":"response.create","model":"gpt-5.5","prompt_cache_options":{"enabled":true},"prompt_cache_key":"keep-key","input":"hello"}`
const passthroughCacheOptionsRejection = `{"type":"error","error":{"code":"invalid_parameter","message":"prompt_cache_options is not supported on this model","param":"prompt_cache_options"}}`

func TestPassthroughCacheRetryPreservesSupportedRequestAndBoundsRejectedRetry(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprintf("rejected=%t", rejected), func(t *testing.T) {
			upstream := newStagedPassthroughConn()
			conn := &openAIWSPassthroughCacheRetryFrameConn{inner: upstream, writeTimeout: time.Second}
			require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(passthroughCacheOptionsRequest)))
			require.JSONEq(t, passthroughCacheOptionsRequest, string(requirePassthroughUpstreamWrite(t, upstream, time.Second)))
			if rejected {
				upstream.Send(passthroughCacheOptionsRejection)
				upstream.Send(passthroughCacheOptionsRejection)
			} else {
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_supported"}}`)
			}
			_, payload, err := conn.ReadFrame(context.Background())
			require.NoError(t, err)
			if rejected {
				require.JSONEq(t, passthroughCacheOptionsRejection, string(payload), "a repeated rejection must reach the client")
				retry := requirePassthroughUpstreamWrite(t, upstream, time.Second)
				require.JSONEq(t, `{"type":"response.create","model":"gpt-5.5","prompt_cache_key":"keep-key","input":"hello"}`, string(retry))
			} else {
				require.Equal(t, "response.completed", gjson.GetBytes(payload, "type").String())
			}
			require.Empty(t, upstream.writes, "only an explicit rejection may send one extra request")
		})
	}
}

func TestPassthroughCacheRetryDoesNotReplayAmbiguousOrStartedTurns(t *testing.T) {
	tests := []struct {
		name         string
		beforeOutput string
		clientFrame  string
		errorFrame   string
	}{
		{name: "created already visible", beforeOutput: `{"type":"response.created","response":{"id":"resp_started"}}`},
		{name: "text already visible", beforeOutput: `{"type":"response.output_text.delta","delta":"hello"}`},
		{name: "client cancelled", clientFrame: `{"type":"response.cancel"}`},
		{name: "overlapping create cannot bind rejection", clientFrame: passthroughCacheOptionsRequest},
		{name: "value validation", errorFrame: `{"type":"error","status_code":400,"error":{"code":"invalid_parameter","param":"prompt_cache_options","message":"prompt_cache_options.enabled must be a boolean"}}`},
		{name: "explicit non400", errorFrame: `{"type":"error","status_code":403,"error":{"code":"invalid_parameter","param":"prompt_cache_options","message":"prompt_cache_options is not supported on this model"}}`},
		{name: "failed response is not request validation", errorFrame: `{"type":"response.failed","response":{"error":{"status_code":400,"code":"invalid_parameter","param":"prompt_cache_options","message":"prompt_cache_options is not supported on this model"}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := newStagedPassthroughConn()
			conn := &openAIWSPassthroughCacheRetryFrameConn{inner: upstream, writeTimeout: time.Second}
			require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(passthroughCacheOptionsRequest)))
			requirePassthroughUpstreamWrite(t, upstream, time.Second)
			if tt.beforeOutput != "" {
				upstream.Send(tt.beforeOutput)
				_, payload, err := conn.ReadFrame(context.Background())
				require.NoError(t, err)
				require.JSONEq(t, tt.beforeOutput, string(payload))
			}
			if tt.clientFrame != "" {
				require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(tt.clientFrame)))
				requirePassthroughUpstreamWrite(t, upstream, time.Second)
			}
			if tt.errorFrame == "" {
				tt.errorFrame = passthroughCacheOptionsRejection
			}
			upstream.Send(tt.errorFrame)
			_, payload, err := conn.ReadFrame(context.Background())
			require.NoError(t, err)
			require.JSONEq(t, tt.errorFrame, string(payload))
			require.Empty(t, upstream.writes)
		})
	}
}

func TestPassthroughCacheRetrySerializesConcurrentCancellation(t *testing.T) {
	for iteration := 0; iteration < 25; iteration++ {
		upstream := newStagedPassthroughConn()
		conn := &openAIWSPassthroughCacheRetryFrameConn{inner: upstream, writeTimeout: time.Second}
		require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(passthroughCacheOptionsRequest)))
		requirePassthroughUpstreamWrite(t, upstream, time.Second)
		upstream.Send(passthroughCacheOptionsRejection)
		upstream.Send(`{"type":"response.completed","response":{"id":"resp_race"}}`)
		var wg sync.WaitGroup
		wg.Add(2)
		errors := make(chan error, 2)
		go func() {
			defer wg.Done()
			_, _, err := conn.ReadFrame(context.Background())
			errors <- err
		}()
		go func() {
			defer wg.Done()
			errors <- conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.cancel"}`))
		}()
		wg.Wait()
		require.NoError(t, <-errors)
		require.NoError(t, <-errors)
		cancelSeen := false
		for len(upstream.writes) > 0 {
			frame := <-upstream.writes
			if gjson.GetBytes(frame, "type").String() == "response.cancel" {
				cancelSeen = true
			} else {
				require.False(t, cancelSeen, "a request must never be retried after cancellation")
				require.False(t, gjson.GetBytes(frame, "prompt_cache_options").Exists())
			}
		}
		require.True(t, cancelSeen)
	}
}

func TestPassthroughCacheRetryKeepsOriginalFirstOutputDeadline(t *testing.T) {
	upstream := newStagedPassthroughConn()
	retryConn := &openAIWSPassthroughCacheRetryFrameConn{inner: upstream, writeTimeout: time.Second}
	deadlineArms := 0
	conn := &openAIWSPassthroughFirstOutputFrameConn{
		inner: retryConn,
		resolveDeadline: func([]byte) openAIWSPassthroughFirstOutputDeadline {
			deadlineArms++
			return openAIWSPassthroughFirstOutputDeadline{startedAt: time.Now(), timeout: time.Second}
		},
	}
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(passthroughCacheOptionsRequest)))
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	upstream.Send(passthroughCacheOptionsRejection)
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_retry"}}`)
	_, payload, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(payload, "type").String())
	requirePassthroughUpstreamWrite(t, upstream, time.Second)
	require.Equal(t, 1, deadlineArms, "compatibility retry must not extend the turn deadline")
}

func TestPassthroughLifecycle_CacheOptionsRetryFirstAndLaterTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)
	upstream := newStagedPassthroughConn()
	svc := newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream)
	var completedTurns, failedTurns atomic.Int32
	server, serverErr := startPassthroughLifecycleServerWithHooks(t, controlCtx, svc, passthroughLifecycleAccount(), func(*gin.Context) *OpenAIWSIngressHooks {
		return &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
			completedTurns.Add(1)
			if err != nil || result == nil || result.UpstreamTerminalEvent != "response.completed" {
				failedTurns.Add(1)
			}
		}}
	})
	defer server.Close()
	clientConn := dialPassthroughLifecycleClientWithPayload(t, server, passthroughCacheOptionsRequest)
	defer func() { _ = clientConn.CloseNow() }()
	for turn := 1; turn <= 2; turn++ {
		if turn > 1 {
			writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
			err := clientConn.Write(writeCtx, coderws.MessageText, []byte(passthroughCacheOptionsRequest))
			cancel()
			require.NoError(t, err)
		}
		request := requirePassthroughUpstreamWrite(t, upstream, time.Second)
		require.True(t, gjson.GetBytes(request, "prompt_cache_options").Exists(), "every new turn must preserve the client's options first")
		upstream.Send(passthroughCacheOptionsRejection)
		retry := requirePassthroughUpstreamWrite(t, upstream, time.Second)
		require.False(t, gjson.GetBytes(retry, "prompt_cache_options").Exists())
		require.Equal(t, "keep-key", gjson.GetBytes(retry, "prompt_cache_key").String())
		upstream.Send(fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_retry_%d","model":"gpt-5.5","usage":{"input_tokens":1,"output_tokens":1}}}`, turn))
		completed, err := readPassthroughLifecycleFrame(t, clientConn, time.Second)
		require.NoError(t, err)
		require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String(), "rejected first attempt must not reach the client")
		require.Equal(t, fmt.Sprintf("resp_retry_%d", turn), gjson.GetBytes(completed, "response.id").String())
	}
	require.NoError(t, clientConn.Close(coderws.StatusNormalClosure, "done"))
	select {
	case err := <-serverErr:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough relay did not finish")
	}
	require.Equal(t, int32(2), completedTurns.Load(), "compatibility attempts must not create extra billable turns")
	require.Zero(t, failedTurns.Load())
	select {
	case extra := <-upstream.writes:
		t.Fatalf("unexpected extra upstream request: %s", extra)
	default:
	}
}
