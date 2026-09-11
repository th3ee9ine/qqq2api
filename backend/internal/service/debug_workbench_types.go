package service

import "encoding/json"

// DebugWorkbenchRequest is a one-off, administrator-scoped gateway invocation.
type DebugWorkbenchRequest struct {
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
	Body     json.RawMessage   `json:"body"`
	ProxyID  *int64            `json:"proxy_id,omitempty"`
	Session  DebugSessionInput `json:"session"`
}

type DebugSessionInput struct {
	ID     string `json:"id,omitempty"`
	Action string `json:"action,omitempty"`
}

type DebugSessionView struct {
	ID                 string `json:"id"`
	SessionID          string `json:"session_id"`
	ThreadID           string `json:"thread_id"`
	TurnID             string `json:"turn_id"`
	WindowID           string `json:"window_id"`
	TurnIndex          int    `json:"turn_index"`
	TurnStateAvailable bool   `json:"turn_state_available"`
}

// DebugHTTPSnapshot describes application-level HTTP, not a packet capture.
// Body capture is bounded; truncation never changes the forwarded body.
type DebugHTTPSnapshot struct {
	Method        string              `json:"method,omitempty"`
	URL           string              `json:"url,omitempty"`
	StatusCode    int                 `json:"status_code,omitempty"`
	Headers       map[string][]string `json:"headers"`
	Body          json.RawMessage     `json:"body,omitempty"`
	BodyText      string              `json:"body_text,omitempty"`
	BodyEncoding  string              `json:"body_encoding,omitempty"`
	BodyBytes     int64               `json:"body_bytes"`
	CapturedBytes int                 `json:"captured_bytes"`
	Truncated     bool                `json:"truncated"`
	Complete      bool                `json:"complete"`
}

type DebugHeaderChange struct {
	Name         string   `json:"name"`
	Action       string   `json:"action"`
	InputValues  []string `json:"input_values,omitempty"`
	OutputValues []string `json:"output_values,omitempty"`
	Reason       string   `json:"reason"`
}

type DebugUpstreamAttempt struct {
	Index         int                 `json:"index"`
	Transport     string              `json:"transport"`
	AccountID     int64               `json:"account_id"`
	Proxy         string              `json:"proxy"`
	Request       DebugHTTPSnapshot   `json:"request"`
	Response      *DebugHTTPSnapshot  `json:"response,omitempty"`
	HeaderChanges []DebugHeaderChange `json:"header_changes"`
	DurationMS    int64               `json:"duration_ms"`
	TTFTMS        *int64              `json:"ttft_ms,omitempty"`
	Error         string              `json:"error,omitempty"`
}

type DebugWorkbenchResult struct {
	RequestID  string                 `json:"request_id"`
	Success    bool                   `json:"success"`
	Endpoint   string                 `json:"endpoint"`
	Transport  string                 `json:"transport"`
	DurationMS int64                  `json:"duration_ms"`
	Session    DebugSessionView       `json:"session"`
	Inbound    DebugHTTPSnapshot      `json:"inbound"`
	Outbound   DebugHTTPSnapshot      `json:"outbound"`
	Attempts   []DebugUpstreamAttempt `json:"attempts"`
	Warnings   []string               `json:"warnings"`
	Error      string                 `json:"error,omitempty"`
}
