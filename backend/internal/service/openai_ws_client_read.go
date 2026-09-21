package service

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	coderws "github.com/coder/websocket"
)

type openAIWSClientReadResult struct {
	messageType coderws.MessageType
	payload     []byte
	err         error
}

// openAIWSClientReadAhead keeps a client reader active while a native ingress
// turn is relaying upstream events. Besides buffering the next request frame,
// this makes a peer close observable before handshake-derived turn state is
// committed. The inter-turn timeout is armed only after the current turn has
// reached a terminal event.
type openAIWSClientReadAhead struct {
	resultCh      chan openAIWSClientReadResult
	timeoutStart  chan struct{}
	timeoutActive atomic.Bool
	cached        *openAIWSClientReadResult
	controlCtx    context.Context
	conn          *coderws.Conn
}

func startOpenAIWSClientReadAhead(
	controlCtx context.Context,
	conn *coderws.Conn,
	timeout time.Duration,
) *openAIWSClientReadAhead {
	if controlCtx == nil {
		controlCtx = context.Background()
	}
	readAhead := &openAIWSClientReadAhead{
		resultCh:     make(chan openAIWSClientReadResult, 1),
		timeoutStart: make(chan struct{}, 1),
		controlCtx:   controlCtx,
		conn:         conn,
	}
	go func() {
		messageType, payload, err := readOpenAIWSClientMessageWithTimeoutStart(
			controlCtx,
			conn,
			timeout,
			coderws.StatusNormalClosure,
			"websocket idle timeout",
			readAhead.timeoutStart,
			readAhead.timeoutActive.Load,
		)
		readAhead.resultCh <- openAIWSClientReadResult{
			messageType: messageType,
			payload:     payload,
			err:         err,
		}
	}()
	return readAhead
}

func (r *openAIWSClientReadAhead) MarkTurnCompleted() {
	if r == nil || !r.timeoutActive.CompareAndSwap(false, true) {
		return
	}
	select {
	case r.timeoutStart <- struct{}{}:
	default:
	}
}

func (r *openAIWSClientReadAhead) Poll() (openAIWSClientReadResult, bool) {
	if r == nil {
		return openAIWSClientReadResult{}, false
	}
	if r.cached != nil {
		return *r.cached, true
	}
	select {
	case result := <-r.resultCh:
		r.cached = &result
		return result, true
	default:
		return openAIWSClientReadResult{}, false
	}
}

func (r *openAIWSClientReadAhead) Wait() openAIWSClientReadResult {
	if r == nil {
		return openAIWSClientReadResult{err: errors.New("openai websocket client read-ahead is nil")}
	}
	var result openAIWSClientReadResult
	if r.cached != nil {
		result = *r.cached
	} else {
		result = <-r.resultCh
		r.cached = &result
	}
	if result.err == nil && r.timeoutActive.Load() {
		if cause := context.Cause(r.controlCtx); errors.Is(cause, ErrOpenAIWSIngressLeaseLost) {
			const reason = "websocket ingress capacity lease lost; please reconnect"
			if r.conn != nil {
				_ = r.conn.Close(coderws.StatusTryAgainLater, reason)
				_ = r.conn.CloseNow()
			}
			result = openAIWSClientReadResult{
				err: NewOpenAIWSClientCloseError(coderws.StatusTryAgainLater, reason, cause),
			}
			r.cached = &result
		}
	}
	return result
}

// ReadOpenAIWSClientMessage keeps one reader alive while control events send
// their close frame, then closes the transport and joins that reader.
func ReadOpenAIWSClientMessage(
	controlCtx context.Context,
	conn *coderws.Conn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
) (coderws.MessageType, []byte, error) {
	return readOpenAIWSClientMessageWithTimeoutStart(
		controlCtx,
		conn,
		timeout,
		timeoutStatus,
		timeoutReason,
		nil,
		nil,
	)
}

// readOpenAIWSClientMessageWithTimeoutStart supports readers whose timeout
// starts after a state transition, such as a completed passthrough turn. When
// timeoutActive is nil, a positive timeout starts immediately.
func readOpenAIWSClientMessageWithTimeoutStart(
	controlCtx context.Context,
	conn *coderws.Conn,
	timeout time.Duration,
	timeoutStatus coderws.StatusCode,
	timeoutReason string,
	timeoutStart <-chan struct{},
	timeoutActive func() bool,
) (coderws.MessageType, []byte, error) {
	if conn == nil {
		return 0, nil, errors.New("openai websocket client connection is nil")
	}
	if controlCtx == nil {
		controlCtx = context.Background()
	}

	readDone := make(chan openAIWSClientReadResult, 1)
	go func() {
		messageType, payload, err := conn.Read(context.Background())
		readDone <- openAIWSClientReadResult{messageType: messageType, payload: payload, err: err}
	}()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	startTimeout := func() {
		if timeout <= 0 || (timeoutActive != nil && !timeoutActive()) {
			return
		}
		if timer == nil {
			timer = time.NewTimer(timeout)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
		}
		timeoutCh = timer.C
	}
	if timeoutActive == nil || timeoutActive() {
		startTimeout()
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	closeAndJoin := func(status coderws.StatusCode, reason string, cause error) (coderws.MessageType, []byte, error) {
		_ = conn.Close(status, reason)
		_ = conn.CloseNow()
		<-readDone
		return 0, nil, NewOpenAIWSClientCloseError(status, reason, cause)
	}
	controlDone := controlCtx.Done()
	var deferredLeaseLoss error

	for {
		select {
		case result := <-readDone:
			return result.messageType, result.payload, result.err
		case <-timeoutStart:
			startTimeout()
			if deferredLeaseLoss != nil {
				return closeAndJoin(
					coderws.StatusTryAgainLater,
					"websocket ingress capacity lease lost; please reconnect",
					deferredLeaseLoss,
				)
			}
		case <-timeoutCh:
			return closeAndJoin(timeoutStatus, timeoutReason, context.DeadlineExceeded)
		case <-controlDone:
			cause := context.Cause(controlCtx)
			if errors.Is(cause, ErrOpenAIWSIngressLeaseLost) {
				// Read-ahead starts while the current turn is still writing. Keep the
				// downstream socket open until MarkTurnCompleted confirms that the
				// terminal event has been delivered, then send the retryable close.
				if timeoutActive != nil && !timeoutActive() {
					deferredLeaseLoss = cause
					controlDone = nil
					continue
				}
				return closeAndJoin(
					coderws.StatusTryAgainLater,
					"websocket ingress capacity lease lost; please reconnect",
					cause,
				)
			}
			return closeAndJoin(coderws.StatusGoingAway, "websocket request canceled", cause)
		}
	}
}
