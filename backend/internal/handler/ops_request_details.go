package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

const (
	opsRequestBodyCaptureKey = "ops_request_body_capture"
	// Leave room in the queue budget for request metadata (headers, query and
	// capture flags) in addition to the decoded body.
	opsRequestBodyCaptureLimit         = 96 * 1024
	opsRequestDetailsMaxQueryKeys      = 64
	opsRequestDetailsMaxQueryValues    = 16
	opsRequestDetailsMaxHeaderKeys     = 32
	opsRequestDetailsMaxHeaderValues   = 8
	opsRequestDetailsMaxValueBytes     = 2048
	opsRequestDetailsMaxMediaTypeBytes = 256
)

var opsRequestDetailHeaderAllowlist = map[string]struct{}{
	"accept":              {},
	"anthropic-beta":      {},
	"anthropic-version":   {},
	"authorization":       {},
	"content-type":        {},
	"content-encoding":    {},
	"openai-beta":         {},
	"openai-organization": {},
	"proxy-authorization": {},
	"cookie":              {},
	"user-agent":          {},
	"x-api-key":           {},
	"x-auth-token":        {},
	"x-client-request-id": {},
	"x-goog-api-client":   {},
	"x-request-id":        {},
}

// opsRequestBodyCapture tees the original request body without changing what
// downstream handlers read. It keeps only a bounded prefix and never stores
// the raw body outside that bound.
type opsRequestBodyCapture struct {
	original      io.ReadCloser
	contentLength int64
	limit         int

	mu          sync.Mutex
	buf         []byte
	total       int64
	readStarted bool
	readEOF     bool
	truncated   bool
	readErr     bool
	drained     bool
}

type opsRequestBodySnapshot struct {
	data          []byte
	contentLength int64
	total         int64
	readStarted   bool
	readEOF       bool
	truncated     bool
	readErr       bool
	drained       bool
}

func installOpsRequestBodyCapture(c *gin.Context) *opsRequestBodyCapture {
	if c == nil || c.Request == nil {
		return nil
	}
	original := c.Request.Body
	if original == nil {
		original = http.NoBody
	}
	capture := &opsRequestBodyCapture{
		original:      original,
		contentLength: c.Request.ContentLength,
		limit:         opsRequestBodyCaptureLimit,
	}
	if capture.contentLength == 0 {
		capture.readEOF = true
	}
	if capture.contentLength > int64(capture.limit) {
		capture.truncated = true
	}
	c.Request.Body = capture
	c.Set(opsRequestBodyCaptureKey, capture)
	return capture
}

func (c *opsRequestBodyCapture) Read(p []byte) (int, error) {
	if c == nil || c.original == nil {
		return 0, io.EOF
	}
	n, err := c.original.Read(p)
	c.mu.Lock()
	c.readStarted = true
	if n > 0 {
		c.total += int64(n)
		remaining := c.limit - len(c.buf)
		if remaining > 0 {
			if n < remaining {
				remaining = n
			}
			c.buf = append(c.buf, p[:remaining]...)
		}
		if c.total > int64(c.limit) {
			c.truncated = true
		}
	}
	if errors.Is(err, io.EOF) {
		c.readEOF = true
	} else if err != nil {
		c.readErr = true
		// A MaxBytesReader reports an error immediately after the permitted
		// prefix; treat that as a truncated body even when Content-Length was
		// not supplied.
		if c.total >= int64(c.limit) {
			c.truncated = true
		}
	}
	c.mu.Unlock()
	return n, err
}

func (c *opsRequestBodyCapture) Close() error {
	if c == nil || c.original == nil {
		return nil
	}
	return c.original.Close()
}

func (c *opsRequestBodyCapture) snapshot() opsRequestBodySnapshot {
	if c == nil {
		return opsRequestBodySnapshot{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return opsRequestBodySnapshot{
		data:          append([]byte(nil), c.buf...),
		contentLength: c.contentLength,
		total:         c.total,
		readStarted:   c.readStarted,
		readEOF:       c.readEOF,
		truncated:     c.truncated,
		readErr:       c.readErr,
		drained:       c.drained,
	}
}

// drainIfSafe captures a small, known-length body left unread by an early
// authentication/policy failure. Unknown or large bodies are deliberately not
// drained because doing so could block on a streaming client.
func (c *opsRequestBodyCapture) drainIfSafe() {
	if c == nil || c.contentLength < 0 || c.contentLength > int64(c.limit) {
		return
	}
	c.mu.Lock()
	if c.total >= c.contentLength || c.readErr || c.readEOF {
		c.mu.Unlock()
		return
	}
	c.drained = true
	c.mu.Unlock()
	_, _ = io.Copy(io.Discard, c)
}

func finishOpsRequestBodyCapture(c *gin.Context, capture *opsRequestBodyCapture) {
	if c == nil || c.Request == nil || capture == nil {
		return
	}
	// A composite route may have replaced Request.Body after reading the
	// original stream. In that case the original capture already has all bytes
	// observed by the middleware and must not consume the replacement body.
	if c.Request.Body != capture {
		return
	}
	capture.drainIfSafe()
	snapshot := capture.snapshot()
	if !snapshot.drained || snapshot.readErr || snapshot.truncated ||
		snapshot.contentLength < 0 || snapshot.total < snapshot.contentLength {
		return
	}
	// Put a complete small body back for an outer middleware that may inspect
	// it after this middleware returns. This does not widen the configured body
	// limit because only a body already proven to be within the capture bound is
	// restored.
	c.Request.Body = &restoredOpsRequestBody{
		Reader:  bytes.NewReader(snapshot.data),
		capture: capture,
	}
	c.Request.ContentLength = int64(len(snapshot.data))
}

// restoredOpsRequestBody exposes the captured bytes to an outer middleware
// while still delegating Close to the original request body. Replacing the
// body with io.NopCloser would otherwise leave the network body's resources
// open until the server happened to reclaim the request.
type restoredOpsRequestBody struct {
	io.Reader
	capture *opsRequestBodyCapture
}

func (b *restoredOpsRequestBody) Close() error {
	if b == nil || b.capture == nil {
		return nil
	}
	return b.capture.Close()
}

type opsRequestMetadata struct {
	method          string
	path            string
	query           map[string][]string
	queryTruncated  bool
	contentType     string
	contentEncoding string
	contentLength   int64
	headers         map[string][]string
	headerTruncated bool
}

func snapshotOpsRequestMetadata(c *gin.Context, capture *opsRequestBodyCapture) opsRequestMetadata {
	metadata := opsRequestMetadata{contentLength: -1}
	if c == nil || c.Request == nil {
		return metadata
	}
	req := c.Request
	metadata.method = truncateString(req.Method, 16)
	metadata.contentType = truncateString(req.Header.Get("Content-Type"), opsRequestDetailsMaxMediaTypeBytes)
	metadata.contentEncoding = truncateString(req.Header.Get("Content-Encoding"), opsRequestDetailsMaxMediaTypeBytes)
	if req.URL != nil {
		metadata.path = truncateString(req.URL.Path, 2048)
		metadata.query, metadata.queryTruncated = boundedOpsRequestQuery(req.URL.Query())
	}
	if capture != nil {
		metadata.contentLength = capture.contentLength
	} else {
		metadata.contentLength = req.ContentLength
	}
	metadata.headers, metadata.headerTruncated = boundedOpsRequestHeaders(req.Header)
	return metadata
}

func boundedOpsRequestQuery(values map[string][]string) (map[string][]string, bool) {
	if len(values) == 0 {
		return nil, false
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string][]string)
	truncated := false
	for _, key := range keys {
		if len(out) >= opsRequestDetailsMaxQueryKeys {
			truncated = true
			break
		}
		rawKey := strings.TrimSpace(key)
		// Classify the complete key before truncating it.  Otherwise a very long
		// key whose credential marker appears after the retained prefix could
		// evade redaction while its value is copied into the diagnostic record.
		keyIsSensitive := service.IsSensitiveOpsFieldName(rawKey)
		cleanKey := truncateString(rawKey, opsRequestDetailsMaxValueBytes)
		if cleanKey == "" {
			continue
		}
		items := values[key]
		if len(items) > opsRequestDetailsMaxQueryValues {
			truncated = true
			items = items[:opsRequestDetailsMaxQueryValues]
		}
		cleanItems := make([]string, 0, len(items))
		for _, item := range items {
			if keyIsSensitive {
				cleanItems = append(cleanItems, "[REDACTED]")
			} else {
				cleanItems = append(cleanItems, truncateString(strings.ToValidUTF8(item, ""), opsRequestDetailsMaxValueBytes))
			}
		}
		out[cleanKey] = cleanItems
	}
	return out, truncated
}

func boundedOpsRequestHeaders(headers http.Header) (map[string][]string, bool) {
	if len(headers) == 0 {
		return nil, false
	}
	valuesByKey := make(map[string][]string, len(headers))
	for key := range headers {
		lower := strings.ToLower(strings.TrimSpace(key))
		if _, ok := opsRequestDetailHeaderAllowlist[lower]; ok {
			valuesByKey[lower] = append(valuesByKey[lower], headers[key]...)
		}
	}
	keys := make([]string, 0, len(valuesByKey))
	for key := range valuesByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string][]string)
	truncated := false
	for _, key := range keys {
		if len(out) >= opsRequestDetailsMaxHeaderKeys {
			truncated = true
			break
		}
		source := valuesByKey[key]
		if len(source) > opsRequestDetailsMaxHeaderValues {
			truncated = true
			source = source[:opsRequestDetailsMaxHeaderValues]
		}
		sensitive := service.IsSensitiveOpsFieldName(key)
		clean := make([]string, 0, len(source))
		for _, value := range source {
			if sensitive {
				clean = append(clean, "[REDACTED]")
				continue
			}
			clean = append(clean, truncateString(strings.ToValidUTF8(value, ""), opsRequestDetailsMaxValueBytes))
		}
		out[key] = clean
	}
	return out, truncated
}

func requestBodyCaptureComplete(snapshot opsRequestBodySnapshot) bool {
	if snapshot.readErr || snapshot.truncated {
		return false
	}
	if snapshot.contentLength == 0 {
		return true
	}
	if snapshot.contentLength > 0 {
		return snapshot.total >= snapshot.contentLength
	}
	return snapshot.readEOF
}

func requestBodyContentTypeIsJSON(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func requestBodyCanParseJSON(contentType string, body []byte) bool {
	if requestBodyContentTypeIsJSON(contentType) {
		return true
	}
	// Some compatible clients omit Content-Type. Only infer JSON for an
	// object/array prefix; form, multipart and binary bodies stay omitted.
	trimmed := bytes.TrimSpace(body)
	return len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')
}

func buildOpsRequestDetailsJSON(c *gin.Context, capture *opsRequestBodyCapture) string {
	metadata := snapshotOpsRequestMetadata(c, capture)
	details := map[string]any{
		"method":         metadata.method,
		"path":           metadata.path,
		"content_type":   metadata.contentType,
		"content_length": metadata.contentLength,
	}
	if metadata.contentEncoding != "" && !strings.EqualFold(metadata.contentEncoding, "identity") {
		details["content_encoding"] = metadata.contentEncoding
	}
	if len(metadata.query) > 0 {
		details["query"] = metadata.query
	}
	if metadata.queryTruncated {
		details["query_truncated"] = true
	}
	if len(metadata.headers) > 0 {
		details["headers"] = metadata.headers
	}
	if metadata.headerTruncated {
		details["headers_truncated"] = true
	}

	snapshot := capture.snapshot()
	details["body_read"] = snapshot.readStarted
	details["body_bytes_read"] = snapshot.total
	details["body_truncated"] = snapshot.truncated

	complete := requestBodyCaptureComplete(snapshot)
	switch {
	case snapshot.truncated:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "truncated"
	case snapshot.readErr:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "read_error"
	case !snapshot.readStarted && snapshot.contentLength > 0:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "not_read"
	case !complete:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "incomplete"
	case len(bytes.TrimSpace(snapshot.data)) == 0:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "empty"
	case metadata.contentEncoding != "" && !strings.EqualFold(metadata.contentEncoding, "identity"):
		details["body_omitted"] = true
		details["body_omitted_reason"] = "compressed"
	case !requestBodyCanParseJSON(metadata.contentType, snapshot.data):
		details["body_omitted"] = true
		details["body_omitted_reason"] = "non_json"
	default:
		var body any
		if err := json.Unmarshal(snapshot.data, &body); err != nil {
			details["body_omitted"] = true
			details["body_parse_error"] = true
			details["body_omitted_reason"] = "invalid_json"
		} else {
			details["body"] = body
		}
	}

	encoded, err := json.Marshal(details)
	if err != nil {
		return ""
	}
	cleaned, _ := service.SanitizeOpsRequestDetailsForQueue(string(encoded))
	return cleaned
}

// attachOpsRequestDetails snapshots request metadata synchronously. The
// resulting string is copied into the error-log entry before any asynchronous
// worker is started, so no gin context escapes the request goroutine.
func attachOpsRequestDetails(c *gin.Context, entry *service.OpsInsertErrorLogInput) {
	if c == nil || entry == nil {
		return
	}
	var capture *opsRequestBodyCapture
	if value, ok := c.Get(opsRequestBodyCaptureKey); ok {
		capture, _ = value.(*opsRequestBodyCapture)
	}
	details := buildOpsRequestDetailsJSON(c, capture)
	if details == "" {
		return
	}
	entry.RequestDetailsJSON = &details
}
