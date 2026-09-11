package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/net/http/httpguts"
)

const (
	DebugWorkbenchMaxBodyBytes     = 8 << 20
	DebugWorkbenchMaxEnvelopeBytes = 9 << 20
	debugWorkbenchTimeout          = 120 * time.Second
)

// DebugWorkbenchAccountSource keeps the execution path independent of the admin
// transport. Accounts and proxy credentials never come from the submitted JSON.
type DebugWorkbenchAccountSource interface {
	GetAccount(context.Context, int64) (*Account, error)
	GetProxy(context.Context, int64) (*Proxy, error)
}

type DebugWorkbenchInputError struct {
	StatusCode int
	Message    string
}

func (e *DebugWorkbenchInputError) Error() string { return e.Message }
func debugInputError(status int, message string) error {
	return &DebugWorkbenchInputError{StatusCode: status, Message: message}
}

type DebugWorkbenchService struct {
	gateway  *OpenAIGatewayService
	accounts DebugWorkbenchAccountSource
	sessions *DebugWorkbenchSessionStore
	slots    chan struct{}
}

func NewDebugWorkbenchService(gateway *OpenAIGatewayService, accounts DebugWorkbenchAccountSource) *DebugWorkbenchService {
	return &DebugWorkbenchService{gateway: gateway, accounts: accounts, sessions: NewDebugWorkbenchSessionStore(), slots: make(chan struct{}, 4)}
}

// ValidateDebugWorkbenchRequest validates the complete edited request, rather
// than reconstructing a smaller connectivity-test payload from model/prompt.
func ValidateDebugWorkbenchRequest(input DebugWorkbenchRequest) error {
	switch input.Endpoint {
	case "responses", "chat/completions", "images/generations":
	default:
		return debugInputError(http.StatusBadRequest, "unsupported debug endpoint")
	}
	if len(input.Body) > DebugWorkbenchMaxBodyBytes {
		return debugInputError(http.StatusRequestEntityTooLarge, "debug body exceeds 8 MiB")
	}
	body := bytes.TrimSpace(input.Body)
	if len(body) == 0 || body[0] != '{' || !json.Valid(body) {
		return debugInputError(http.StatusBadRequest, "body must be a valid JSON object")
	}
	if input.ProxyID != nil && *input.ProxyID < 0 {
		return debugInputError(http.StatusBadRequest, "proxy_id must be zero (direct) or a positive proxy ID")
	}
	if len(input.Headers) > 64 {
		return debugInputError(http.StatusBadRequest, "at most 64 request headers are supported")
	}
	seen := make(map[string]bool, len(input.Headers))
	total := 0
	for name, value := range input.Headers {
		lower := strings.ToLower(name)
		if len(name) > 200 || !httpguts.ValidHeaderFieldName(name) {
			return debugInputError(http.StatusBadRequest, "invalid request header name")
		}
		if seen[lower] {
			return debugInputError(http.StatusBadRequest, "duplicate request header names (case insensitive)")
		}
		seen[lower] = true
		if len(value) > 8192 || !httpguts.ValidHeaderFieldValue(value) {
			return debugInputError(http.StatusBadRequest, "invalid or oversized request header value")
		}
		total += len(name) + len(value)
	}
	if total > 64<<10 {
		return debugInputError(http.StatusBadRequest, "request headers exceed 64 KiB")
	}
	if len(input.Session.ID) > 128 {
		return debugInputError(http.StatusBadRequest, "invalid debug session ID")
	}
	switch input.Session.Action {
	case "", "new_session", "new_turn", "continue_turn":
	default:
		return debugInputError(http.StatusBadRequest, "invalid debug session action")
	}
	return nil
}

func (s *DebugWorkbenchService) Run(ctx context.Context, ownerID, accountID int64, input DebugWorkbenchRequest) (*DebugWorkbenchResult, error) {
	if err := ValidateDebugWorkbenchRequest(input); err != nil {
		return nil, err
	}
	if ownerID <= 0 {
		return nil, debugInputError(http.StatusUnauthorized, "authenticated administrator required")
	}
	if accountID <= 0 {
		return nil, debugInputError(http.StatusBadRequest, "invalid account ID")
	}
	if s == nil || s.gateway == nil || s.accounts == nil || s.sessions == nil {
		return nil, debugInputError(http.StatusServiceUnavailable, "debug workbench service is not configured")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return nil, debugInputError(http.StatusTooManyRequests, "four debug requests are already running; retry after one completes")
	}
	ctx, cancel := context.WithTimeout(ctx, debugWorkbenchTimeout)
	defer cancel()
	selected, err := s.accounts.GetAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, debugInputError(http.StatusNotFound, "account not found")
	}
	if !selected.IsOpenAI() {
		return nil, debugInputError(http.StatusBadRequest, "debug workbench requires an OpenAI account")
	}
	// Never change the repository account or a shared service containing mutexes.
	account := *selected
	account.Extra = maps.Clone(selected.Extra)
	if account.Extra == nil {
		account.Extra = map[string]any{}
	}
	account.Extra["openai_ws_force_http"] = true
	account.Credentials = maps.Clone(selected.Credentials)
	if input.ProxyID != nil {
		if *input.ProxyID == 0 {
			account.ProxyID, account.Proxy = nil, nil
		} else {
			proxy, loadErr := s.accounts.GetProxy(ctx, *input.ProxyID)
			if loadErr != nil {
				return nil, loadErr
			}
			if proxy == nil {
				return nil, debugInputError(http.StatusNotFound, "proxy not found")
			}
			if !proxy.IsActive() || proxy.IsExpired(time.Now()) {
				return nil, debugInputError(http.StatusBadRequest, "selected proxy is inactive or expired")
			}
			id := proxy.ID
			account.ProxyID, account.Proxy = &id, proxy
		}
	} else if account.ProxyID != nil && account.Proxy == nil {
		proxy, loadErr := s.accounts.GetProxy(ctx, *account.ProxyID)
		if loadErr != nil {
			return nil, loadErr
		}
		if proxy == nil {
			return nil, debugInputError(http.StatusNotFound, "account proxy not found")
		}
		account.Proxy = proxy
	}
	lease, err := s.sessions.Acquire(ownerID, accountID, input.Session)
	if err != nil {
		return nil, debugWorkbenchSessionError(err)
	}
	finished := false
	defer func() {
		if !finished {
			lease.Finish("", false)
		}
	}()

	submitted := make(http.Header, len(input.Headers))
	for name, value := range input.Headers {
		submitted.Set(name, value)
	}
	secrets := debugWorkbenchCredentialSecrets(account.Credentials)
	if account.Proxy != nil {
		if account.Proxy.Password != "" {
			secrets = append(secrets, account.Proxy.Password)
		}
		if account.Proxy.Username != "" {
			secrets = append(secrets, account.Proxy.Username)
		}
	}
	trace := NewDebugWorkbenchTrace(submitted, secrets)
	ctx = trace.Context(ctx)
	// Snapshot exactly what the editor submitted, before account and session rules.
	inbound, _ := http.NewRequestWithContext(ctx, http.MethodPost, "/v1/"+input.Endpoint, bytes.NewReader(input.Body))
	inbound.Header = submitted.Clone()
	requestID := uuid.NewString()
	result := &DebugWorkbenchResult{RequestID: requestID, Endpoint: input.Endpoint, Transport: "http", Inbound: trace.SnapshotRequest(inbound)}

	req := inbound.Clone(ctx)
	req.Header = debugWorkbenchExecutableHeaders(submitted)
	req.Header.Set("Content-Type", "application/json")
	if v := strings.TrimSpace(req.Header.Get("X-Client-Request-Id")); v == "" || strings.Contains(v, "<generated-") || strings.Contains(v, "••") {
		req.Header.Set("X-Client-Request-Id", requestID)
	}
	for name, values := range lease.Headers() {
		req.Header[name] = append([]string(nil), values...)
	}
	executionBody, metadataChanged, metadataErr := debugWorkbenchAlignSessionMetadata(req.Header, input.Body, lease.View())
	if metadataErr != nil {
		return nil, debugInputError(http.StatusBadRequest, metadataErr.Error())
	}
	executionBody, promptCacheKey, cacheNote := debugWorkbenchSessionPromptCache(&account, input.Endpoint, executionBody, lease.View().SessionID)
	req.Body = io.NopCloser(bytes.NewReader(executionBody))
	req.ContentLength = int64(len(executionBody))
	req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(executionBody)), nil }
	recorder := NewDebugWorkbenchResponseWriter()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	// A separate negative namespace isolates administrator sessions from API-key
	// traffic; this synthetic key is not used for authentication or billing.
	c.Set("api_key", &APIKey{ID: -ownerID, UserID: ownerID})
	SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
	start := time.Now()
	switch input.Endpoint {
	case "responses":
		_, err = s.gateway.Forward(ctx, c, &account, executionBody)
	case "chat/completions":
		_, err = s.gateway.ForwardAsChatCompletions(ctx, c, &account, executionBody, promptCacheKey, "")
	case "images/generations":
		var parsed *OpenAIImagesRequest
		parsed, err = s.gateway.ParseOpenAIImagesRequest(c, executionBody)
		if err == nil {
			_, err = s.gateway.ForwardImages(ctx, c, &account, executionBody, parsed, "")
		}
	}
	trace.AddSecrets(debugWorkbenchCredentialSecrets(codexAccountIdentitySource(c, &account).Credentials))
	result.Inbound = trace.SnapshotRequest(inbound)
	result.DurationMS = time.Since(start).Milliseconds()
	if err != nil && !c.Writer.Written() {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		c.JSON(status, gin.H{"error": gin.H{"type": "debug_execution_error", "message": trace.RedactText(err.Error())}})
	}
	result.Success = err == nil && recorder.Status() >= 200 && recorder.Status() < 300
	result.Outbound = recorder.Snapshot(trace)
	result.Attempts = trace.Attempts()
	result.Warnings = append([]string{"调试执行复用正式网关的 HTTP 请求构造与响应转换；本次固定使用 HTTP，不进行 WebSocket 握手。", "会话上下文关联请求标识与上游回合状态，不自动补写历史消息；完整历史或 previous_response_id 由 Body 显式提供。"}, trace.Warnings()...)
	if cacheNote != "" {
		result.Warnings = append(result.Warnings, cacheNote)
	}
	if metadataChanged {
		result.Warnings = append(result.Warnings, "已将 X-Codex-Turn-Metadata 与 Body.client_metadata 中现有的当前会话、线程、回合和窗口标识对齐至托管会话；父级与子代理上下文保留，入站快照保留原值。")
	}
	if len(result.Attempts) == 0 {
		result.Warnings = append(result.Warnings, "本次没有可记录的 HTTP 客户端调用；请检查本地校验错误或账号插件传输。")
	}
	if err != nil {
		result.Error = trace.RedactText(err.Error())
	}
	result.Session = lease.Finish(trace.LastTurnState(), result.Success)
	finished = true
	return result, nil
}

func debugWorkbenchSessionError(err error) error {
	switch {
	case errors.Is(err, ErrDebugSessionBusy):
		return debugInputError(http.StatusConflict, "debug session already has an active request")
	case errors.Is(err, ErrDebugSessionCapacity):
		return debugInputError(http.StatusServiceUnavailable, "debug session capacity reached")
	case errors.Is(err, ErrDebugSessionNotFound):
		return debugInputError(http.StatusNotFound, "debug session not found or expired for this account")
	default:
		return debugInputError(http.StatusBadRequest, "invalid debug session input")
	}
}

// Gateway allowlists and account overrides still decide final forwarding. This
// initial pass excludes transport, credentials and managed session values, so
// editor placeholders or an administrator bearer can never become credentials.
func debugWorkbenchExecutableHeaders(submitted http.Header) http.Header {
	out := make(http.Header, len(submitted))
	for name, values := range submitted {
		lower := strings.ToLower(name)
		if debugWorkbenchManagedHeader(lower) {
			continue
		}
		if len(values) != 1 || strings.Contains(values[0], "••") || strings.Contains(values[0], "<generated-") {
			continue
		}
		out[name] = append([]string(nil), values...)
	}
	return out
}

func debugWorkbenchManagedHeader(name string) bool {
	switch name {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-goog-api-key", "chatgpt-account-id", "chatgpt-account-id-fallback",
		"host", "content-length", "transfer-encoding", "connection", "keep-alive", "proxy-authenticate", "proxy-connection", "te", "trailer", "upgrade", "accept-encoding", "content-type",
		"session-id", "session_id", "conversation_id", "thread-id", "thread_id", "turn-id", "turn_id", "x-codex-window-id", "x-codex-turn-state":
		return true
	}
	return strings.HasPrefix(name, "sec-websocket-")
}

// OAuth's formal HTTP builder derives session_id from prompt_cache_key. Fill
// that native body field only when absent, rather than bypassing its account
// isolation by overwriting the final request headers. Explicit keys are retained.
func debugWorkbenchSessionPromptCache(account *Account, endpoint string, body []byte, sessionID string) ([]byte, string, string) {
	if !account.UsesOpenAICodexProtocol() {
		return body, "", ""
	}
	if endpoint == "images/generations" {
		return body, "", "OAuth 生图使用正式 Images→Responses 承载转换；最终 Session 头与缓存键遵循该转换，不承诺所有协议具有相同的会话头。"
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return body, "", ""
	}
	if raw, exists := payload["prompt_cache_key"]; exists {
		var key string
		_ = json.Unmarshal(raw, &key)
		return body, key, "已保留显式 prompt_cache_key；上游 session_id 根据该值及所选账号的原生隔离规则重建，空值或类型错误交由正式网关处理。"
	}
	payload["prompt_cache_key"], _ = json.Marshal(sessionID)
	rebuilt, err := json.Marshal(payload)
	if err != nil {
		return body, "", ""
	}
	return rebuilt, sessionID, "Body 未指定 prompt_cache_key，已使用本次托管会话的稳定 Session ID 补齐；上游 session_id 继续由正式网关进行账号隔离。"
}

// Align only existing current identity fields. It does not invent metadata,
// ancestry, installation IDs or subagent roles, and does not change other body
// parameters. RawMessage keeps large numeric ordinals lossless on re-encoding.
func debugWorkbenchAlignSessionMetadata(headers http.Header, body []byte, view DebugSessionView) ([]byte, bool, error) {
	changed := false
	if raw := headers.Get("X-Codex-Turn-Metadata"); raw != "" {
		normalized, edited, err := debugWorkbenchAlignMetadataObject([]byte(raw), view)
		if err != nil {
			return nil, false, fmt.Errorf("X-Codex-Turn-Metadata must be a JSON object")
		}
		if edited {
			headers.Set("X-Codex-Turn-Metadata", string(normalized))
			changed = true
		}
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, fmt.Errorf("body must be a JSON object")
	}
	raw, exists := payload["client_metadata"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return body, changed, nil
	}
	normalized, edited, err := debugWorkbenchAlignMetadataObject(raw, view)
	if err != nil {
		return nil, false, fmt.Errorf("body.client_metadata must be a JSON object")
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(normalized, &metadata); err != nil {
		return nil, false, fmt.Errorf("body.client_metadata must be a JSON object")
	}
	if embedded, ok := metadata["x-codex-turn-metadata"]; ok {
		var encoded string
		if err := json.Unmarshal(embedded, &encoded); err != nil {
			return nil, false, fmt.Errorf("body.client_metadata.x-codex-turn-metadata must be a JSON object encoded as a string")
		}
		embeddedBody, embeddedChanged, err := debugWorkbenchAlignMetadataObject([]byte(encoded), view)
		if err != nil {
			return nil, false, fmt.Errorf("body.client_metadata.x-codex-turn-metadata must be a JSON object encoded as a string")
		}
		if embeddedChanged {
			metadata["x-codex-turn-metadata"], _ = json.Marshal(string(embeddedBody))
			edited = true
		}
	}
	if !edited {
		return body, changed, nil
	}
	payload["client_metadata"], _ = json.Marshal(metadata)
	rebuilt, err := json.Marshal(payload)
	return rebuilt, true, err
}

func debugWorkbenchAlignMetadataObject(raw []byte, view DebugSessionView) ([]byte, bool, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil {
		return nil, false, fmt.Errorf("metadata must be a JSON object")
	}
	replacements := map[string]string{
		"session_id": view.SessionID, "session-id": view.SessionID, "conversation_id": view.SessionID,
		"thread_id": view.ThreadID, "thread-id": view.ThreadID,
		"turn_id": view.TurnID, "turn-id": view.TurnID,
		"window_id": view.WindowID, "x-codex-window-id": view.WindowID,
	}
	changed := false
	for key, value := range replacements {
		if previous, exists := metadata[key]; exists {
			var old string
			_ = json.Unmarshal(previous, &old)
			if old != value {
				metadata[key], _ = json.Marshal(value)
				changed = true
			}
		}
	}
	if !changed {
		return raw, false, nil
	}
	rebuilt, err := json.Marshal(metadata)
	return rebuilt, true, err
}

func debugWorkbenchCredentialSecrets(credentials map[string]any) []string {
	var secrets []string
	var collect func(any)
	collect = func(value any) {
		switch v := value.(type) {
		case string:
			if len(v) >= 4 {
				secrets = append(secrets, v)
			}
		case map[string]any:
			for _, nested := range v {
				collect(nested)
			}
		case map[string]string:
			for _, nested := range v {
				collect(nested)
			}
		case []any:
			for _, nested := range v {
				collect(nested)
			}
		}
	}
	// Non-secret configuration (base URLs, model mappings, routing enums) must
	// remain visible. Custom header values are opaque and always treated private.
	for key, value := range credentials {
		name := strings.ToLower(key)
		if name == "header_overrides" || strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "password") || strings.Contains(name, "cookie") || strings.Contains(name, "api_key") || strings.Contains(name, "private_key") || strings.HasPrefix(name, "chatgpt_") {
			collect(value)
		}
	}
	return secrets
}

// Ensure extra JSON after the envelope is rejected by the handler's decoder.
func DecodeDebugWorkbenchRequest(r io.Reader) (DebugWorkbenchRequest, error) {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	var input DebugWorkbenchRequest
	if err := decoder.Decode(&input); err != nil {
		return input, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return input, fmt.Errorf("debug request must contain exactly one JSON object")
	}
	return input, nil
}
