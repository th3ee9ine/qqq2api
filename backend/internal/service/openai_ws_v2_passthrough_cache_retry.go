package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	coderws "github.com/coder/websocket"
	openaiwsv2 "github.com/th3ee9ine/qqq2api/internal/service/openai_ws_v2"
	"github.com/tidwall/gjson"
)

// openAIWSPassthroughCacheRetryFrameConn keeps unsupported cache options from
// failing a turn before the upstream has accepted it. The original request is
// always sent intact. Only an explicit parameter rejection in the very first
// upstream frame permits one retry on the same connection, before the relay can
// expose that rejection or account for a failed turn.
//
// This wrapper sits inside the first-output deadline wrapper: retrying does not
// reset the turn's deadline. Any upstream frame, client control frame, or I/O
// error closes the retry window. In particular, response.created is already
// enough to forbid a retry, even though it contains no generated text.
type openAIWSPassthroughCacheRetryFrameConn struct {
	inner        openaiwsv2.FrameConn
	writeTimeout time.Duration
	onRetry      func(reason string)

	// Serializes all writes and the retry decision. A concurrent cancel either
	// wins before the decision (disabling it), or is sent after the retry.
	mu          sync.Mutex
	active      bool
	pendingBody []byte
	pendingType coderws.MessageType
	retryState  *openAIResponsesRejectedFieldRetryState
}

func (c *openAIWSPassthroughCacheRetryFrameConn) WriteFrame(ctx context.Context, msgType coderws.MessageType, payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pendingBody = nil
	c.retryState = nil
	if (msgType == coderws.MessageText || msgType == coderws.MessageBinary) &&
		strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "response.create" {
		// The adapter already rejects overlapping turns. Remain conservative if
		// the wrapper is used elsewhere: a pipelined create cannot safely bind
		// the next upstream rejection to either request.
		if !c.active && gjson.GetBytes(payload, "prompt_cache_options").Exists() {
			c.pendingBody = append([]byte(nil), payload...)
			c.pendingType = msgType
			c.retryState = newOpenAIResponsesRejectedFieldRetryState(payload)
		}
		c.active = true
	}
	if err := c.inner.WriteFrame(ctx, msgType, payload); err != nil {
		c.pendingBody = nil
		c.retryState = nil
		return err
	}
	return nil
}

func (c *openAIWSPassthroughCacheRetryFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	for {
		msgType, payload, err := c.inner.ReadFrame(ctx)
		c.mu.Lock()
		pendingBody, pendingType, retryState := c.pendingBody, c.pendingType, c.retryState
		// Consuming the window before retrying also bounds repeated rejections:
		// neither a second error nor a failed retry write can replay the turn.
		c.pendingBody, c.retryState = nil, nil
		if err == nil && (msgType == coderws.MessageText || msgType == coderws.MessageBinary) &&
			strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "error" &&
			len(pendingBody) > 0 && retryState != nil && openAIWSRejectedFieldRetryHTTPStatus(payload) == http.StatusBadRequest {
			retryBody, reason, changed, normalizeErr := normalizeOpenAIResponsesRejectedFieldRetryBody(http.StatusBadRequest, pendingBody, payload)
			if normalizeErr != nil {
				c.mu.Unlock()
				return msgType, nil, fmt.Errorf("normalize passthrough cache rejection: %w", normalizeErr)
			}
			if changed && reason == "prompt_cache_options parameter rejection" && retryState.Allow(retryBody) {
				writeCtx, cancel := context.WithTimeout(ctx, c.writeTimeout)
				writeErr := c.inner.WriteFrame(writeCtx, pendingType, retryBody)
				cancel()
				c.mu.Unlock()
				if writeErr != nil {
					return msgType, nil, fmt.Errorf("retry passthrough cache rejection: %w", writeErr)
				}
				if c.onRetry != nil {
					c.onRetry(reason)
				}
				continue
			}
		}
		if err == nil && (strings.TrimSpace(gjson.GetBytes(payload, "type").String()) == "error" || openAIWSPassthroughIsTerminalOutput(payload)) {
			c.active = false
		}
		c.mu.Unlock()
		return msgType, payload, err
	}
}

func (c *openAIWSPassthroughCacheRetryFrameConn) Close() error { return c.inner.Close() }

var _ openaiwsv2.FrameConn = (*openAIWSPassthroughCacheRetryFrameConn)(nil)
