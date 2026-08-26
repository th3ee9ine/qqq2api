package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// startPassthroughHookRecordingServer 与 startPassthroughLifecycleServer 相同，
// 但把一组会记录调用的 hooks 传给 ingress，用于观察透传路径的 turn 回调。
func startPassthroughHookRecordingServer(
	t *testing.T,
	controlCtx context.Context,
	svc *OpenAIGatewayService,
	account *Account,
	hooks *OpenAIWSIngressHooks,
) (*httptest.Server, <-chan error) {
	t.Helper()
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, &coderws.AcceptOptions{CompressionMode: coderws.CompressionContextTakeover})
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		msgType, firstMessage, err := ReadOpenAIWSClientMessage(
			controlCtx,
			conn,
			3*time.Second,
			coderws.StatusPolicyViolation,
			"missing first response.create message",
		)
		if err != nil {
			serverErr <- err
			return
		}
		if msgType != coderws.MessageText {
			serverErr <- errors.New("first message was not text")
			return
		}

		recorder := httptest.NewRecorder()
		ginCtx, _ := gin.CreateTestContext(recorder)
		req := r.Clone(controlCtx)
		req.Header = req.Header.Clone()
		ginCtx.Request = req
		serverErr <- svc.ProxyResponsesWebSocketFromClient(controlCtx, ginCtx, conn, account, "sk-test", firstMessage, hooks)
	}))
	return server, serverErr
}

// TestPassthroughIngressRunsBeforeTurnAndBalancesAfterTurn 钉死 ws_v2
// passthrough 与其他 ingress 模式相同的逐 turn 准入契约：出站前
// BeforeTurn，terminal 后 TurnStarted + AfterTurn，且每轮各一次。
func TestPassthroughIngressRunsBeforeTurnAndBalancesAfterTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)

	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_pricing","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)

	var hooksMu sync.Mutex
	beforeTurnCalls := 0
	expectedTurnStartedAt := time.Date(2026, time.August, 17, 9, 59, 59, 0, time.UTC)
	type hookEvent struct {
		name      string
		turn      int
		startedAt time.Time
	}
	var hookEvents []hookEvent
	hooks := &OpenAIWSIngressHooks{
		InitialTurnStartedAt: expectedTurnStartedAt,
		TurnStarted: func(turn int, startedAt time.Time) {
			hooksMu.Lock()
			hookEvents = append(hookEvents, hookEvent{name: "TurnStarted", turn: turn, startedAt: startedAt})
			hooksMu.Unlock()
		},
		BeforeTurn: func(turn int) error {
			hooksMu.Lock()
			beforeTurnCalls++
			hookEvents = append(hookEvents, hookEvent{name: "BeforeTurn", turn: turn})
			hooksMu.Unlock()
			return nil
		},
		AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
			hooksMu.Lock()
			hookEvents = append(hookEvents, hookEvent{name: "AfterTurn", turn: turn})
			hooksMu.Unlock()
		},
	}

	server, serverErr := startPassthroughHookRecordingServer(
		t,
		controlCtx,
		newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
		passthroughLifecycleAccount(),
		hooks,
	)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	event, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())

	// 等待连接自然结束（inter-turn idle 超时），确保 AfterTurn 已提交。
	_, _ = readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough ingress did not exit")
	}

	hooksMu.Lock()
	gotBefore := beforeTurnCalls
	gotEvents := append([]hookEvent(nil), hookEvents...)
	hooksMu.Unlock()

	require.Equal(t, 1, gotBefore, "首轮也应执行一次 BeforeTurn 以冻结 turn 定价")
	require.Len(t, gotEvents, 3, "透传 ingress 每轮只应报告一组准入/完成回调")
	require.Equal(t, "BeforeTurn", gotEvents[0].name)
	require.Equal(t, "TurnStarted", gotEvents[1].name)
	require.Equal(t, expectedTurnStartedAt, gotEvents[1].startedAt, "TurnStarted 必须携带入口冻结的首轮开始时刻")
	require.Equal(t, "AfterTurn", gotEvents[2].name)
	require.Equal(t, gotEvents[1].turn, gotEvents[2].turn, "TurnStarted 后应提交同一 turn 的 AfterTurn")
}

func TestPassthroughTurnAdmissionFinishesActiveTurnExactlyOnce(t *testing.T) {
	var mu sync.Mutex
	beforeTurns := make([]int, 0, 1)
	afterTurns := make([]int, 0, 1)
	afterErrors := make([]error, 0, 1)
	wantErr := errors.New("relay failed")
	admission := &openAIWSPassthroughTurnAdmission{hooks: &OpenAIWSIngressHooks{
		BeforeTurn: func(turn int) error {
			mu.Lock()
			beforeTurns = append(beforeTurns, turn)
			mu.Unlock()
			return nil
		},
		AfterTurn: func(turn int, _ *OpenAIForwardResult, turnErr error) {
			mu.Lock()
			afterTurns = append(afterTurns, turn)
			afterErrors = append(afterErrors, turnErr)
			mu.Unlock()
		},
	}}

	require.NoError(t, admission.begin(2))
	require.Equal(t, 2, admission.active())
	require.True(t, admission.finish(2, nil, wantErr))
	require.False(t, admission.finish(2, nil, errors.New("duplicate")))
	require.Zero(t, admission.active())

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []int{2}, beforeTurns)
	require.Equal(t, []int{2}, afterTurns)
	require.Equal(t, []error{wantErr}, afterErrors)
}

func TestPassthroughIngressFreezesSubsequentTurnBeforeRequestPolicy(t *testing.T) {
	testPassthroughIngressFreezesSubsequentTurnBeforeRequestPolicy(t, coderws.MessageText)
}

func TestPassthroughIngressFreezesBinarySubsequentTurnBeforeRequestPolicy(t *testing.T) {
	testPassthroughIngressFreezesSubsequentTurnBeforeRequestPolicy(t, coderws.MessageBinary)
}

func testPassthroughIngressFreezesSubsequentTurnBeforeRequestPolicy(t *testing.T, secondMessageType coderws.MessageType) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	controlCtx, cancelControl := context.WithCancelCause(context.Background())
	defer cancelControl(context.Canceled)

	upstream := newStagedPassthroughConn()
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)

	type turnStart struct {
		turn      int
		startedAt time.Time
	}
	turnStarts := make(chan turnStart, 2)
	beforeTurns := make(chan int, 2)
	afterTurns := make(chan int, 2)
	beforeRequestEntered := make(chan time.Time, 1)
	releaseBeforeRequest := make(chan struct{})
	hooks := &OpenAIWSIngressHooks{
		InitialTurnStartedAt: time.Now(),
		TurnStarted: func(turn int, startedAt time.Time) {
			turnStarts <- turnStart{turn: turn, startedAt: startedAt}
		},
		BeforeRequest: func(turn int, _ []byte, _ string) error {
			if turn == 2 {
				beforeRequestEntered <- time.Now()
				<-releaseBeforeRequest
			}
			return nil
		},
		BeforeTurn: func(turn int) error {
			beforeTurns <- turn
			return nil
		},
		AfterTurn: func(turn int, _ *OpenAIForwardResult, _ error) {
			afterTurns <- turn
		},
	}

	server, serverErr := startPassthroughHookRecordingServer(
		t,
		controlCtx,
		newPassthroughLifecycleService(passthroughLifecycleConfig(), upstream),
		passthroughLifecycleAccount(),
		hooks,
	)
	defer server.Close()
	clientConn := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = clientConn.CloseNow() }()

	select {
	case turn := <-beforeTurns:
		require.Equal(t, 1, turn)
	case <-time.After(time.Second):
		t.Fatal("first turn did not acquire passthrough admission")
	}
	require.Equal(t, "response.create", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, 3*time.Second), "type").String())
	firstCompleted, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "resp_first", gjson.GetBytes(firstCompleted, "response.id").String())
	select {
	case first := <-turnStarts:
		require.Equal(t, 1, first.turn)
	case <-time.After(time.Second):
		t.Fatal("first turn start was not reported")
	}
	select {
	case turn := <-afterTurns:
		require.Equal(t, 1, turn)
	case <-time.After(time.Second):
		t.Fatal("first turn did not release passthrough admission")
	}

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = clientConn.Write(writeCtx, secondMessageType, []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_first"}`))
	cancelWrite()
	require.NoError(t, err)

	var policyEnteredAt time.Time
	select {
	case policyEnteredAt = <-beforeRequestEntered:
	case <-time.After(time.Second):
		t.Fatal("second turn did not enter BeforeRequest")
	}
	close(releaseBeforeRequest)
	select {
	case turn := <-beforeTurns:
		require.Equal(t, 2, turn)
	case <-time.After(time.Second):
		t.Fatal("second turn did not acquire passthrough admission")
	}
	require.Equal(t, "response.create", gjson.GetBytes(requirePassthroughUpstreamWrite(t, upstream, 3*time.Second), "type").String())
	upstream.Send(`{"type":"response.completed","response":{"id":"resp_second","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	secondCompleted, err := readPassthroughLifecycleFrame(t, clientConn, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "resp_second", gjson.GetBytes(secondCompleted, "response.id").String())

	select {
	case second := <-turnStarts:
		require.Equal(t, 2, second.turn)
		require.False(t, second.startedAt.After(policyEnteredAt), "第二轮开始时刻必须在 BeforeRequest 策略执行前冻结")
	case <-time.After(time.Second):
		t.Fatal("second turn start was not reported")
	}
	select {
	case turn := <-afterTurns:
		require.Equal(t, 2, turn)
	case <-time.After(time.Second):
		t.Fatal("second turn did not release passthrough admission")
	}

	_ = clientConn.CloseNow()
	cancelControl(context.Canceled)
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("passthrough ingress did not exit")
	}
	select {
	case turn := <-afterTurns:
		t.Fatalf("turn %d released more than once", turn)
	default:
	}
}
