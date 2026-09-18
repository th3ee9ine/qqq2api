package service

import "encoding/json"

const (
	DebugVerificationStageBaseline  = "baseline"
	DebugVerificationStageCapture   = "capture"
	DebugVerificationStageReplay    = "replay"
	DebugVerificationStageAutomatic = "automatic"
)

// DebugWorkbenchRequest is a one-off, administrator-scoped gateway invocation.
type DebugWorkbenchRequest struct {
	Endpoint          string            `json:"endpoint"`
	Headers           map[string]string `json:"headers"`
	Body              json.RawMessage   `json:"body"`
	ProxyID           *int64            `json:"proxy_id,omitempty"`
	APIKeyID          int64             `json:"api_key_id,omitempty"`
	VerificationStage string            `json:"verification_stage,omitempty"`
	Session           DebugSessionInput `json:"session"`
}

type DebugSessionInput struct {
	ID     string `json:"id,omitempty"`
	Action string `json:"action,omitempty"`
	// model is populated from the submitted Body by the server. It is kept out
	// of the JSON API so callers cannot provide a scope different from the
	// model that is actually sent upstream.
	model             string
	apiKeyID          int64
	verificationStage string
	proxyURL          string
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

// DebugStateVerification contains only non-secret evidence observed during a
// Responses debug run. UsageLogAccountID and UpstreamResponseModel remain empty
// unless a durable usage_logs row is actually consulted; request traces are not
// mislabeled as database evidence.
type DebugStateVerification struct {
	RequestedModel         string `json:"requested_model,omitempty"`
	ResponseCreatedModel   string `json:"response_created_model,omitempty"`
	ResponseCompletedModel string `json:"response_completed_model,omitempty"`
	ResponseModel          string `json:"response_model,omitempty"`
	StateSent              bool   `json:"state_sent"`
	StateReceived          bool   `json:"state_received"`
	StateLength            int    `json:"state_length,omitempty"`
	StateSource            string `json:"state_source,omitempty"`
	ActualAccountID        int64  `json:"actual_account_id,omitempty"`
	UsageLogAccountID      int64  `json:"usage_log_account_id,omitempty"`
	UsageLogAPIKeyID       int64  `json:"usage_log_api_key_id,omitempty"`
	UsageLogRequestedModel string `json:"usage_log_requested_model,omitempty"`
	UpstreamResponseModel  string `json:"upstream_response_model,omitempty"`
	UsageLogStateSent      bool   `json:"usage_log_state_sent"`
	UsageLogVerified       bool   `json:"usage_log_verified"`
	StateMatchesCapture    bool   `json:"state_matches_capture"`
	StatePublished         bool   `json:"state_published"`
	DailyRouteVerified     bool   `json:"daily_route_verified"`
}

type DebugWorkbenchResult struct {
	RequestID         string                  `json:"request_id"`
	Success           bool                    `json:"success"`
	Endpoint          string                  `json:"endpoint"`
	Transport         string                  `json:"transport"`
	DurationMS        int64                   `json:"duration_ms"`
	Session           DebugSessionView        `json:"session"`
	Inbound           DebugHTTPSnapshot       `json:"inbound"`
	Outbound          DebugHTTPSnapshot       `json:"outbound"`
	Attempts          []DebugUpstreamAttempt  `json:"attempts"`
	Warnings          []string                `json:"warnings"`
	Error             string                  `json:"error,omitempty"`
	StateVerification *DebugStateVerification `json:"state_verification,omitempty"`
	DailyReplay       *DebugWorkbenchResult   `json:"daily_replay,omitempty"`
}
