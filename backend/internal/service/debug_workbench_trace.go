package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
)

const DebugWorkbenchCaptureLimit = 8 << 20
const debugWorkbenchMaxAttempts = 8

type debugWorkbenchTraceKey struct{}

// DebugWorkbenchTrace is opt-in and scoped to a single debug run. No global
// request logs or credentials are persisted by this collector.
type DebugWorkbenchTrace struct {
	mu        sync.Mutex
	input     http.Header
	secrets   []string
	captures  []*DebugWorkbenchAttemptCapture
	dropped   bool
	warnings  []string
	turnState string
}

type DebugWorkbenchAttemptCapture struct {
	trace        *DebugWorkbenchTrace
	attempt      DebugUpstreamAttempt
	started      time.Time
	responseBody debugCaptureBuffer
	ended        bool
}

type debugCaptureBuffer struct {
	data     []byte
	total    int64
	complete bool
}

func (b *debugCaptureBuffer) write(p []byte) {
	b.total += int64(len(p))
	if n := DebugWorkbenchCaptureLimit - len(b.data); n > 0 {
		if n > len(p) {
			n = len(p)
		}
		b.data = append(b.data, p[:n]...)
	}
}

func NewDebugWorkbenchTrace(input http.Header, secrets []string) *DebugWorkbenchTrace {
	t := &DebugWorkbenchTrace{input: input.Clone()}
	t.AddSecrets(secrets)
	for name, values := range input {
		if !debugPublicHeader(name) {
			t.AddSecrets(values)
		}
	}
	return t
}
func (t *DebugWorkbenchTrace) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, debugWorkbenchTraceKey{}, t)
}
func DebugWorkbenchTraceFromContext(ctx context.Context) *DebugWorkbenchTrace {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(debugWorkbenchTraceKey{}).(*DebugWorkbenchTrace)
	return t
}
func (t *DebugWorkbenchTrace) AddSecrets(values []string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.addSecretsLocked(values)
}
func (t *DebugWorkbenchTrace) addSecretsLocked(values []string) {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) < 4 || v == "[redacted]" {
			continue
		}
		t.secrets = append(t.secrets, v)
		if scheme, credential, found := strings.Cut(v, " "); found {
			switch strings.ToLower(scheme) {
			case "bearer", "agentassertion", "basic":
				if credential = strings.TrimSpace(credential); credential != "" {
					t.secrets = append(t.secrets, credential)
				}
			}
		}
		if encoded, err := json.Marshal(v); err == nil && len(encoded) > 2 {
			escaped := string(encoded[1 : len(encoded)-1])
			if escaped != v {
				t.secrets = append(t.secrets, escaped)
			}
		}
	}
	sort.SliceStable(t.secrets, func(i, j int) bool { return len(t.secrets[i]) > len(t.secrets[j]) })
}

var debugURLInText = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s"<>]+`)

func (t *DebugWorkbenchTrace) redactLocked(value string) string {
	value = debugURLInText.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "[redacted URL]"
		}
		parsed.User = nil
		q := parsed.Query()
		for key := range q {
			q.Set(key, "[redacted]")
		}
		parsed.RawQuery = q.Encode()
		return parsed.String()
	})
	for _, secret := range t.secrets {
		value = strings.ReplaceAll(value, secret, "[redacted]")
	}
	return value
}
func (t *DebugWorkbenchTrace) RedactText(value string) string {
	if t == nil {
		return value
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.redactLocked(value)
}

// Unknown headers may contain credentials; only documented non-secret headers
// expose values. Auth, cookies, account IDs, opaque turn state and custom
// header values remain visible as names, with values redacted.
func debugPublicHeader(name string) bool {
	switch strings.ToLower(name) {
	case "accept", "accept-language", "accept-encoding", "content-type", "content-length", "host", "user-agent", "originator", "version", "openai-beta", "session_id", "session-id", "conversation_id", "thread-id", "turn-id", "x-client-request-id", "x-request-id", "x-codex-beta-features", "x-codex-routing-hint", "x-codex-window-id", "x-codex-installation-id", "x-codex-parent-thread-id", "x-codex-turn-metadata", "x-openai-subagent", "x-openai-memgen-request", "x-openai-internal-codex-residency", "x-openai-internal-codex-responses-lite", "x-responsesapi-include-timing-metrics", "cache-control", "date", "server", "connection", "transfer-encoding", "retry-after", "openai-processing-ms":
		return true
	}
	return false
}
func (t *DebugWorkbenchTrace) headersLocked(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for name, values := range headers {
		copied := make([]string, len(values))
		for i, v := range values {
			if !debugPublicHeader(name) {
				copied[i] = "[redacted]"
			} else {
				copied[i] = t.redactLocked(v)
			}
		}
		out[http.CanonicalHeaderKey(name)] = copied
	}
	return out
}
func (t *DebugWorkbenchTrace) urlLocked(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[invalid URL]"
	}
	parsed.User = nil
	q := parsed.Query()
	for key := range q {
		q.Set(key, "[redacted]")
	}
	parsed.RawQuery = q.Encode()
	parsed.Fragment = ""
	return t.redactLocked(parsed.String())
}

// redactBodyLocked preserves JSON semantics even when credentials contain JSON
// escape characters. SSE data JSON is processed independently, without changing
// the response delivered to the gateway.
func (t *DebugWorkbenchTrace) redactBodyLocked(body []byte) []byte {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil && json.Valid(body) {
		var walk func(any) any
		walk = func(v any) any {
			switch x := v.(type) {
			case string:
				return t.redactLocked(x)
			case []any:
				for i := range x {
					x[i] = walk(x[i])
				}
				return x
			case map[string]any:
				for k, item := range x {
					switch strings.ToLower(k) {
					case "authorization", "proxy-authorization", "api_key", "access_token", "refresh_token", "password", "cookie", "set-cookie":
						x[k] = "[redacted]"
					default:
						x[k] = walk(item)
					}
				}
				return x
			default:
				return v
			}
		}
		clean, _ := json.Marshal(walk(value))
		return clean
	}
	lines := bytes.Split(body, []byte("\n"))
	for i, line := range lines {
		if bytes.HasPrefix(line, []byte("data:")) {
			payload := bytes.TrimSpace(line[5:])
			if json.Valid(payload) {
				lines[i] = append([]byte("data: "), t.redactBodyLocked(payload)...)
				continue
			}
		}
		lines[i] = []byte(t.redactLocked(string(line)))
	}
	return bytes.Join(lines, []byte("\n"))
}

func (t *DebugWorkbenchTrace) snapshotLocked(status int, headers http.Header, body []byte, total int64, complete bool) DebugHTTPSnapshot {
	s := DebugHTTPSnapshot{StatusCode: status, Headers: t.headersLocked(headers), BodyBytes: total, CapturedBytes: len(body), Truncated: total > int64(len(body)), Complete: complete}
	if len(body) == 0 {
		return s
	}
	contentType := strings.ToLower(headers.Get("Content-Type"))
	// Ancillary image fetches contain binary bytes, not UTF-8 JSON or SSE.
	if strings.HasPrefix(contentType, "image/") || strings.HasPrefix(contentType, "application/octet-stream") {
		s.BodyEncoding = "base64"
		s.BodyText = base64.StdEncoding.EncodeToString(body)
		return s
	}
	clean := t.redactBodyLocked(body)
	if json.Valid(clean) {
		s.Body = append(json.RawMessage(nil), clean...)
	} else {
		s.BodyText = string(clean)
	}
	return s
}

func (t *DebugWorkbenchTrace) ResponseSnapshot(status int, headers http.Header, body []byte, total int64, complete bool) DebugHTTPSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.snapshotLocked(status, headers, body, total, complete)
}
func (t *DebugWorkbenchTrace) SnapshotRequest(req *http.Request) DebugHTTPSnapshot {
	if req == nil {
		return DebugHTTPSnapshot{Headers: map[string][]string{}}
	}
	var b debugCaptureBuffer
	if req.Body == nil || req.Body == http.NoBody {
		b.complete = true
	} else if req.GetBody != nil {
		if reader, err := req.GetBody(); err == nil {
			_, err = io.Copy(&debugCaptureWriter{buffer: &b}, reader)
			_ = reader.Close()
			b.complete = err == nil
		}
	}
	headers := req.Header.Clone()
	if req.Host != "" {
		headers.Set("Host", req.Host)
	} else if req.URL != nil {
		headers.Set("Host", req.URL.Host)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for name, values := range headers {
		if !debugPublicHeader(name) {
			t.addSecretsLocked(values)
		}
	}
	s := t.snapshotLocked(0, headers, b.data, b.total, b.complete)
	s.Method = req.Method
	if req.URL != nil {
		s.URL = t.urlLocked(req.URL.String())
	}
	return s
}

type debugCaptureWriter struct{ buffer *debugCaptureBuffer }

func (w *debugCaptureWriter) Write(p []byte) (int, error) { w.buffer.write(p); return len(p), nil }

func (t *DebugWorkbenchTrace) StartAttempt(req *http.Request, proxy string, accountID int64) *DebugWorkbenchAttemptCapture {
	if t == nil || req == nil {
		return nil
	}
	t.mu.Lock()
	if len(t.captures) >= debugWorkbenchMaxAttempts {
		t.dropped = true
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()
	if parsed, err := url.Parse(proxy); err == nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		t.AddSecrets([]string{parsed.User.String(), parsed.User.Username(), password})
	}
	snapshot := t.SnapshotRequest(req)
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.captures) >= debugWorkbenchMaxAttempts {
		t.dropped = true
		return nil
	}
	displayProxy := "direct"
	if proxy != "" {
		displayProxy = t.urlLocked(proxy)
	}
	a := &DebugWorkbenchAttemptCapture{trace: t, started: time.Now(), attempt: DebugUpstreamAttempt{Index: len(t.captures) + 1, Transport: "http", AccountID: accountID, Proxy: displayProxy, Request: snapshot, HeaderChanges: t.headerChangesLocked(t.input, req.Header, debugSnapshotHost(snapshot))}}
	t.captures = append(t.captures, a)
	return a
}
func (a *DebugWorkbenchAttemptCapture) Finish(resp *http.Response, err error) {
	if a == nil {
		return
	}
	t := a.trace
	t.mu.Lock()
	defer t.mu.Unlock()
	if err != nil {
		a.attempt.Error = t.redactLocked(err.Error())
	}
	a.attempt.DurationMS = time.Since(a.started).Milliseconds()
	if resp == nil {
		a.ended = true
		return
	}
	// An image download is not a Codex turn and must not overwrite its state.
	if a.attempt.Request.Method == http.MethodPost {
		t.turnState = resp.Header.Get("X-Codex-Turn-State")
	}
	for name, values := range resp.Header {
		if !debugPublicHeader(name) {
			t.addSecretsLocked(values)
		}
	}
	snapshot := t.snapshotLocked(resp.StatusCode, resp.Header, nil, 0, resp.Body == nil || resp.Body == http.NoBody)
	a.attempt.Response = &snapshot
	if resp.Body == nil || resp.Body == http.NoBody {
		a.ended = true
		return
	}
	resp.Body = &debugResponseBody{ReadCloser: resp.Body, capture: a}
}
func (a *DebugWorkbenchAttemptCapture) MarkPlugin() {
	if a == nil {
		return
	}
	a.trace.mu.Lock()
	defer a.trace.mu.Unlock()
	a.attempt.Transport = "plugin_http"
	a.trace.warnings = append(a.trace.warnings, "插件请求记录位于核心网关交接边界；插件内部的再次改写不属于该 HTTP 快照。")
}

type debugResponseBody struct {
	io.ReadCloser
	capture *DebugWorkbenchAttemptCapture
}

func (b *debugResponseBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	a := b.capture
	t := a.trace
	t.mu.Lock()
	defer t.mu.Unlock()
	if n > 0 {
		if a.attempt.TTFTMS == nil {
			elapsed := time.Since(a.started).Milliseconds()
			a.attempt.TTFTMS = &elapsed
		}
		a.responseBody.write(p[:n])
	}
	if err == io.EOF {
		a.responseBody.complete = true
		a.ended = true
	} else if err != nil {
		a.attempt.Error = t.redactLocked(err.Error())
		a.ended = true
	}
	a.attempt.DurationMS = time.Since(a.started).Milliseconds()
	return n, err
}
func (b *debugResponseBody) Close() error {
	err := b.ReadCloser.Close()
	a := b.capture
	a.trace.mu.Lock()
	defer a.trace.mu.Unlock()
	a.ended = true
	a.attempt.DurationMS = time.Since(a.started).Milliseconds()
	return err
}
func (t *DebugWorkbenchTrace) Attempts() []DebugUpstreamAttempt {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]DebugUpstreamAttempt, 0, len(t.captures))
	for _, a := range t.captures {
		v := a.attempt
		// Re-redact after all credentials, including any shadow-account credential,
		// have been discovered at the actual transport boundary.
		raw, _ := json.Marshal(v.Request)
		raw = []byte(t.redactLocked(string(raw)))
		_ = json.Unmarshal(raw, &v.Request)
		if a.attempt.Response != nil {
			h := http.Header(a.attempt.Response.Headers)
			s := t.snapshotLocked(a.attempt.Response.StatusCode, h, a.responseBody.data, a.responseBody.total, a.responseBody.complete || a.attempt.Response.Complete)
			v.Response = &s
		}
		v.Error = t.redactLocked(v.Error)
		result = append(result, v)
	}
	return result
}
func (t *DebugWorkbenchTrace) LastTurnState() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dropped {
		return ""
	}
	return t.turnState
}
func (t *DebugWorkbenchTrace) Warnings() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]string{}, t.warnings...)
	out = append(out, "快照记录交给 HTTP 客户端的最终应用层请求与业务层读取的响应；不虚构 HTTP/2、TLS 或传输层自动生成字段。响应压缩由现有客户端处理。")
	if t.dropped {
		out = append(out, "上游尝试超过 8 次，仅展示前 8 次记录；转发未被截断，本次不缓存回合状态以避免复用旧响应状态。")
	}
	for _, a := range t.captures {
		if a.attempt.Request.Truncated || a.responseBody.total > int64(len(a.responseBody.data)) {
			out = append(out, "单个 Body 捕获上限为 8 MiB，超出部分未展示；实际转发 Body 不受影响。")
			break
		}
	}
	return out
}
func (t *DebugWorkbenchTrace) headerChangesLocked(input, output http.Header, host string) []DebugHeaderChange {
	normalize := func(h http.Header) http.Header {
		out := http.Header{}
		for n, v := range h {
			out[http.CanonicalHeaderKey(n)] = append([]string(nil), v...)
		}
		return out
	}
	in, out := normalize(input), normalize(output)
	if host != "" {
		out.Set("Host", host)
	}
	names := map[string]bool{}
	for n := range in {
		names[n] = true
	}
	for n := range out {
		names[n] = true
	}
	keys := make([]string, 0, len(names))
	for n := range names {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	changes := make([]DebugHeaderChange, 0, len(keys))
	redIn, redOut := t.headersLocked(in), t.headersLocked(out)
	for _, n := range keys {
		a, b := in[n], out[n]
		action, reason := "preserved", "正式网关保留该请求头。"
		switch {
		case len(a) == 0:
			action, reason = "generated", "由账号凭据、调试会话或正式网关请求构造器生成。"
		case len(b) == 0:
			action, reason = "filtered", "此端点的正式网关白名单、条件校验或受保护字段规则未将该请求头传给上游。"
		case !equalDebugValues(a, b):
			action, reason = "rewritten", "正式网关按账号身份、会话隔离、最终 Body 或请求协议重建。"
		}
		changes = append(changes, DebugHeaderChange{Name: n, Action: action, InputValues: redIn[n], OutputValues: redOut[n], Reason: reason})
	}
	return changes
}
func debugSnapshotHost(s DebugHTTPSnapshot) string {
	if values := s.Headers["Host"]; len(values) > 0 {
		return values[0]
	}
	return ""
}

func equalDebugValues(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TraceDebugHTTPUpstream is also usable with in-memory upstreams in tests.
// Normal requests incur only the context lookup and no body copies.
func TraceDebugHTTPUpstream(delegate HTTPUpstream) HTTPUpstream {
	return &debugHTTPUpstream{delegate: delegate}
}

type debugHTTPUpstream struct{ delegate HTTPUpstream }

func (d *debugHTTPUpstream) Do(req *http.Request, p string, id int64, c int) (resp *http.Response, err error) {
	a := DebugWorkbenchTraceFromContext(req.Context()).StartAttempt(req, p, id)
	defer func() { a.Finish(resp, err) }()
	return d.delegate.Do(req, p, id, c)
}
func (d *debugHTTPUpstream) DoWithTLS(req *http.Request, p string, id int64, c int, profile *tlsfingerprint.Profile) (resp *http.Response, err error) {
	a := DebugWorkbenchTraceFromContext(req.Context()).StartAttempt(req, p, id)
	defer func() { a.Finish(resp, err) }()
	return d.delegate.DoWithTLS(req, p, id, c, profile)
}

// DebugWorkbenchResponseWriter captures the downstream body without buffering
// unbounded streams or changing writes into short writes after the limit.
type DebugWorkbenchResponseWriter struct {
	mu     sync.Mutex
	header http.Header
	status int
	buffer debugCaptureBuffer
	closed chan bool
}

func NewDebugWorkbenchResponseWriter() *DebugWorkbenchResponseWriter {
	return &DebugWorkbenchResponseWriter{header: make(http.Header), closed: make(chan bool)}
}
func (w *DebugWorkbenchResponseWriter) Header() http.Header { return w.header }
func (w *DebugWorkbenchResponseWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = status
	}
}
func (w *DebugWorkbenchResponseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.buffer.write(p)
	return len(p), nil
}
func (w *DebugWorkbenchResponseWriter) Flush()                   { w.WriteHeader(http.StatusOK) }
func (w *DebugWorkbenchResponseWriter) CloseNotify() <-chan bool { return w.closed }
func (w *DebugWorkbenchResponseWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
func (w *DebugWorkbenchResponseWriter) Body() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Clone(w.buffer.data)
}
func (w *DebugWorkbenchResponseWriter) TotalBytes() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.total
}
func (w *DebugWorkbenchResponseWriter) Snapshot(t *DebugWorkbenchTrace) DebugHTTPSnapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	return t.ResponseSnapshot(status, w.header, w.buffer.data, w.buffer.total, true)
}
