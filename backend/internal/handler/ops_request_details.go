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
	pkghttputil "github.com/th3ee9ine/qqq2api/internal/pkg/httputil"
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

// opsRequestBodyCapture tees the original request body without changing what
// downstream handlers read. It keeps only a bounded prefix and never stores
// the raw body outside that bound.
type opsRequestBodyCapture struct {
	original      io.ReadCloser
	contentLength int64
	// contentEncoding/contentLength are snapshotted when the capture is
	// installed.  Gateway body readers may transparently decode compressed
	// requests and mutate Request.Header/ContentLength afterwards; retaining
	// the wire metadata here keeps the diagnostic record truthful.
	contentEncoding string
	limit           int
	// headers/headerTruncated are snapshotted at installation time.  Gateway
	// readers may remove Content-Encoding/Content-Length or otherwise mutate
	// Request.Header while processing the body; diagnostics should describe
	// the headers the client actually sent.
	headers         map[string][]string
	headerTruncated bool

	mu          sync.Mutex
	buf         []byte
	total       int64
	readStarted bool
	readEOF     bool
	truncated   bool
	readErr     bool
	// readLimitExceeded distinguishes an HTTP body-size rejection from an
	// arbitrary I/O failure.  MaxBytesReader reports both a prefix and an
	// error, but diagnostics should identify the actionable truncation reason.
	readLimitExceeded bool
	drained           bool

	// decoded stores a bounded copy of the body produced by the gateway's
	// Content-Encoding decoder (gzip/zstd/deflate).  The raw capture above is
	// the wire representation, so parsing it as JSON would incorrectly fail
	// after the gateway has already decoded it for normal processing.
	decoded          []byte
	decodedTotal     int64
	decodedSet       bool
	decodedTruncated bool
	decodeErr        bool
	// normalized is the representation produced by the lenient JSON reader
	// after BOM/control-byte normalization. Keep it separate from decoded so
	// byte counters continue to describe the wire-decoded payload.
	normalized              []byte
	normalizedTotal         int64
	normalizedSet           bool
	normalizedTruncated     bool
	normalizedLimitExceeded bool
	lenientJSON             bool
}

type opsRequestBodySnapshot struct {
	data                    []byte
	contentLength           int64
	contentEncoding         string
	total                   int64
	readStarted             bool
	readEOF                 bool
	truncated               bool
	readErr                 bool
	readLimitExceeded       bool
	drained                 bool
	headers                 map[string][]string
	headerTruncated         bool
	decoded                 []byte
	decodedTotal            int64
	decodedSet              bool
	decodedTruncated        bool
	decodeErr               bool
	normalized              []byte
	normalizedTotal         int64
	normalizedSet           bool
	normalizedTruncated     bool
	normalizedLimitExceeded bool
	lenientJSON             bool
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
		original:        original,
		contentLength:   c.Request.ContentLength,
		contentEncoding: truncateString(strings.TrimSpace(c.Request.Header.Get("Content-Encoding")), opsRequestDetailsMaxMediaTypeBytes),
		limit:           opsRequestBodyCaptureLimit,
	}
	capture.headers, capture.headerTruncated = boundedOpsRequestHeaders(c.Request.Header)
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

// SetDecodedRequestBody is an optional observer hook used by
// httputil.ReadRequestBodyWithPrealloc.  That helper transparently decodes
// gzip/zstd/deflate and then removes Content-Encoding from the request; keep a
// bounded copy of the decoded bytes so diagnostics show the same JSON that the
// gateway actually processed rather than trying to parse compressed wire data.
func (c *opsRequestBodyCapture) SetDecodedRequestBody(body []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.decodedSet = true
	c.decodeErr = false
	c.decodedTotal = int64(len(body))
	c.decodedTruncated = len(body) > c.limit
	c.decoded = c.decoded[:0]
	if c.limit > 0 && len(body) > c.limit {
		body = body[:c.limit]
	}
	c.decoded = append(c.decoded, body...)
}

// SetNormalizedRequestBody records the representation returned by the
// lenient JSON reader. It is intentionally separate from SetDecodedRequestBody
// so diagnostics can report both decompressed and post-normalization sizes.
func (c *opsRequestBodyCapture) SetNormalizedRequestBody(body []byte) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.normalizedLimitExceeded = false
	c.normalizedSet = true
	c.normalizedTotal = int64(len(body))
	c.normalizedTruncated = len(body) > c.limit
	c.normalized = c.normalized[:0]
	if c.limit > 0 && len(body) > c.limit {
		body = body[:c.limit]
	}
	c.normalized = append(c.normalized, body...)
}

// MarkNormalizedRequestBodyLimit records a failed compatibility parse whose
// normalized representation would exceed the diagnostic bound. The parser
// returns no usable bytes in this case, so retain an explicit truncation
// marker rather than letting the raw control-byte payload be mislabeled as
// invalid JSON.
func (c *opsRequestBodyCapture) MarkNormalizedRequestBodyLimit() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.normalizedLimitExceeded = true
	c.normalizedSet = false
	c.normalized = nil
	c.normalizedTotal = 0
	c.normalizedTruncated = true
	c.mu.Unlock()
}

// MarkLenientJSONRequestBody identifies requests whose gateway parser applies
// BOM/control-byte normalization. Strict readers must keep the original wire
// representation in diagnostics rather than silently repairing it.
func (c *opsRequestBodyCapture) MarkLenientJSONRequestBody() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.lenientJSON = true
	c.mu.Unlock()
}

// MarkRequestBodyDecodeError records a failed Content-Encoding decode.  The
// raw bytes are intentionally not parsed as JSON; buildOpsRequestDetailsJSON
// will retain the original encoding and an explicit omission reason instead.
func (c *opsRequestBodyCapture) MarkRequestBodyDecodeError() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.decodeErr = true
	c.decodedSet = false
	c.decoded = nil
	c.decodedTotal = 0
	c.decodedTruncated = false
	c.normalizedSet = false
	c.normalized = nil
	c.normalizedTotal = 0
	c.normalizedTruncated = false
	c.normalizedLimitExceeded = false
	c.mu.Unlock()
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
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			c.readLimitExceeded = true
		}
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
		data:                    append([]byte(nil), c.buf...),
		contentLength:           c.contentLength,
		contentEncoding:         c.contentEncoding,
		total:                   c.total,
		readStarted:             c.readStarted,
		readEOF:                 c.readEOF,
		truncated:               c.truncated,
		readErr:                 c.readErr,
		readLimitExceeded:       c.readLimitExceeded,
		drained:                 c.drained,
		headers:                 cloneOpsRequestHeaderValues(c.headers),
		headerTruncated:         c.headerTruncated,
		decoded:                 append([]byte(nil), c.decoded...),
		decodedTotal:            c.decodedTotal,
		decodedSet:              c.decodedSet,
		decodedTruncated:        c.decodedTruncated,
		decodeErr:               c.decodeErr,
		normalized:              append([]byte(nil), c.normalized...),
		normalizedTotal:         c.normalizedTotal,
		normalizedSet:           c.normalizedSet,
		normalizedTruncated:     c.normalizedTruncated,
		normalizedLimitExceeded: c.normalizedLimitExceeded,
		lenientJSON:             c.lenientJSON,
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

// Keep the optional request-body observer hooks available if an outer
// middleware rereads a body restored after an early rejection.
func (b *restoredOpsRequestBody) SetDecodedRequestBody(body []byte) {
	if b == nil || b.capture == nil {
		return
	}
	b.capture.SetDecodedRequestBody(body)
}

func (b *restoredOpsRequestBody) MarkRequestBodyDecodeError() {
	if b == nil || b.capture == nil {
		return
	}
	b.capture.MarkRequestBodyDecodeError()
}

func (b *restoredOpsRequestBody) MarkLenientJSONRequestBody() {
	if b == nil || b.capture == nil {
		return
	}
	b.capture.MarkLenientJSONRequestBody()
}

func (b *restoredOpsRequestBody) MarkNormalizedRequestBodyLimit() {
	if b == nil || b.capture == nil {
		return
	}
	b.capture.MarkNormalizedRequestBodyLimit()
}

func (b *restoredOpsRequestBody) SetNormalizedRequestBody(body []byte) {
	if b == nil || b.capture == nil {
		return
	}
	b.capture.SetNormalizedRequestBody(body)
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
		metadata.contentEncoding = capture.contentEncoding
		metadata.headers = cloneOpsRequestHeaderValues(capture.headers)
		metadata.headerTruncated = capture.headerTruncated
	} else {
		metadata.contentLength = req.ContentLength
		metadata.headers, metadata.headerTruncated = boundedOpsRequestHeaders(req.Header)
	}
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
			// Preserve the complete client-provided value (subject only to the
			// diagnostic size bound).  Request details are explicitly intended for
			// troubleshooting and therefore must not redact credential-shaped keys.
			cleanItems = append(cleanItems, truncateString(strings.ToValidUTF8(item, ""), opsRequestDetailsMaxValueBytes))
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
		if lower == "" {
			continue
		}
		// Keep every client header rather than an allowlist. Values are still
		// bounded below so arbitrary headers cannot monopolize the diagnostic
		// record, but no header is hidden or replaced.
		valuesByKey[lower] = append(valuesByKey[lower], headers[key]...)
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
		clean := make([]string, 0, len(source))
		for _, value := range source {
			// Preserve raw header values for error diagnosis; only UTF-8 repair and
			// the fixed per-value byte bound are applied.
			clean = append(clean, truncateString(strings.ToValidUTF8(value, ""), opsRequestDetailsMaxValueBytes))
		}
		out[key] = clean
	}
	return out, truncated
}

func cloneOpsRequestHeaderValues(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func requestBodyCaptureComplete(snapshot opsRequestBodySnapshot) bool {
	if snapshot.readErr || snapshot.truncated {
		return false
	}
	return rawBodyReadComplete(snapshot)
}

// rawBodyReadComplete reports whether the wire reader reached the end of the
// request independently of the bounded-prefix flag.  A compressed wire body
// may legitimately exceed opsRequestBodyCaptureLimit while the gateway still
// reads and decodes it in full; in that case the decoded diagnostic snapshot
// can be complete even though the raw prefix is marked truncated.
func rawBodyReadComplete(snapshot opsRequestBodySnapshot) bool {
	if snapshot.readErr {
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
	// Match the lenient JSON reader's BOM handling so a content-type-less
	// request still gets its complete object/array snapshot.
	trimmed = bytes.TrimPrefix(trimmed, []byte{0xef, 0xbb, 0xbf})
	trimmed = bytes.TrimSpace(trimmed)
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
	// Prefer the decoded body supplied by the gateway's request reader when it
	// exists.  The raw capture remains useful for byte accounting and for
	// preserving the original wire metadata, but compressed bytes are not JSON.
	bodyData := snapshot.data
	bodyTotal := snapshot.total
	bodyTruncated := snapshot.truncated
	bodyComplete := requestBodyCaptureComplete(snapshot)
	decodedBody := false
	if snapshot.decodedSet {
		bodyData = snapshot.decoded
		bodyTotal = snapshot.decodedTotal
		bodyTruncated = snapshot.decodedTruncated
		// A successful decoder callback is emitted only after the complete raw
		// body has been read.  Keep the raw completeness check as an additional
		// guard for malformed/truncated wire streams.
		bodyComplete = rawBodyReadComplete(snapshot) && !snapshot.decodedTruncated
		decodedBody = true
	}
	if snapshot.normalizedLimitExceeded {
		// No normalized bytes were returned, but the route did consume the
		// complete request and rejected its compatibility representation at the
		// configured size boundary. Mark the diagnostic as truncated so the raw
		// prefix is never mistaken for the payload the handler parsed.
		bodyTruncated = true
		bodyComplete = false
	}
	if snapshot.readLimitExceeded {
		// MaxBytesReader can return a prefix together with its limit error
		// before the capture's own 96 KiB bound is reached.
		bodyTruncated = true
		bodyComplete = false
	}
	// Prefer the post-normalization representation recorded by the lenient
	// gateway reader.  It is separate from the decompressed snapshot so both
	// byte counts remain meaningful for compressed requests.
	normalizedBody := false
	if snapshot.normalizedSet {
		bodyData = snapshot.normalized
		bodyTotal = snapshot.normalizedTotal
		bodyTruncated = snapshot.normalizedTruncated
		bodyComplete = rawBodyReadComplete(snapshot) && !snapshot.normalizedTruncated
		normalizedBody = true
	} else if snapshot.lenientJSON && !bodyTruncated && requestBodyCanParseJSON(metadata.contentType, bodyData) {
		// Older/compatible readers may mark the route as lenient without
		// providing the optional normalized observer.  Reproduce the parser's
		// normalization only in that explicitly marked path; strict readers must
		// retain the original bytes and report invalid JSON as-is.
		if normalized, err := pkghttputil.NormalizeLenientJSONRequestBody(bodyData, int64(opsRequestBodyCaptureLimit)); err == nil {
			if !bytes.Equal(normalized, bodyData) {
				bodyData = normalized
				bodyTotal = int64(len(normalized))
				bodyTruncated = len(normalized) > opsRequestBodyCaptureLimit
				normalizedBody = true
			}
		} else {
			// Normalization can expand a bounded prefix (for example, many raw
			// control bytes become six-byte escapes). Preserve the actionable
			// size reason when the compatibility parser rejects that expansion.
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				bodyTruncated = true
				bodyComplete = false
			}
		}
	}
	details["body_read"] = snapshot.readStarted
	details["body_bytes_read"] = snapshot.total
	details["body_truncated"] = bodyTruncated
	if decodedBody {
		details["body_decoded"] = true
		details["body_bytes_decoded"] = snapshot.decodedTotal
	}
	if normalizedBody {
		details["body_normalized"] = true
		details["body_bytes_normalized"] = bodyTotal
	}

	complete := bodyComplete
	processedBody := decodedBody || normalizedBody
	switch {
	case snapshot.decodeErr:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "decompression_failed"
	case snapshot.normalizedLimitExceeded:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "truncated"
	case snapshot.readLimitExceeded:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "truncated"
	case bodyTruncated && !snapshot.readErr:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "truncated"
	case snapshot.readErr:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "read_error"
	case !snapshot.readStarted && snapshot.contentLength > 0:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "not_read"
	case processedBody && !rawBodyReadComplete(snapshot):
		details["body_omitted"] = true
		details["body_omitted_reason"] = "incomplete"
	case !complete:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "incomplete"
	case len(bytes.TrimSpace(bodyData)) == 0:
		details["body_omitted"] = true
		details["body_omitted_reason"] = "empty"
	case !processedBody && metadata.contentEncoding != "" && !strings.EqualFold(metadata.contentEncoding, "identity"):
		details["body_omitted"] = true
		details["body_omitted_reason"] = "compressed"
	case !requestBodyCanParseJSON(metadata.contentType, bodyData):
		details["body_omitted"] = true
		details["body_omitted_reason"] = "non_json"
	default:
		var body any
		if err := json.Unmarshal(bodyData, &body); err != nil {
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
	// The snapshot is deliberately not redacted.  Queue/storage layers may
	// compact an oversized JSON document, but they must preserve all values that
	// fit within their technical bounds.
	return string(encoded)
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
