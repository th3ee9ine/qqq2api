package handler

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	pkghttputil "github.com/th3ee9ine/qqq2api/internal/pkg/httputil"
)

type trackingRequestBody struct {
	*bytes.Reader
	closeErr   error
	closeCalls int
}

func (b *trackingRequestBody) Close() error {
	b.closeCalls++
	return b.closeErr
}

func TestBoundedOpsRequestQueryPreservesSensitiveLongKeyBeforeTruncation(t *testing.T) {
	longKey := strings.Repeat("safe", 600) + "access_token"
	values := url.Values{}
	values.Add(longKey, "query-secret")
	values.Add("trace", "fixture")

	out, truncated := boundedOpsRequestQuery(values)
	require.True(t, truncated, "the long key is bounded and must be marked truncated")
	require.Equal(t, []string{"query-secret"}, out[truncateString(longKey, opsRequestDetailsMaxValueBytes)])
	require.Equal(t, []string{"fixture"}, out["trace"])
}

func TestBoundedOpsRequestHeadersPreservesAllValues(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer header-secret")
	headers.Set("Cookie", "session=header-secret")
	headers.Set("User-Agent", "fixture-client/1.0")
	headers.Set("X-Unlisted-Header", "must-not-be-stored")

	out, truncated := boundedOpsRequestHeaders(headers)
	require.False(t, truncated)
	require.Equal(t, []string{"Bearer header-secret"}, out["authorization"])
	require.Equal(t, []string{"session=header-secret"}, out["cookie"])
	require.Equal(t, []string{"fixture-client/1.0"}, out["user-agent"])
	require.Equal(t, []string{"must-not-be-stored"}, out["x-unlisted-header"])
}

func TestFinishOpsRequestBodyCaptureRestoresBodyAndDelegatesClose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	sentinel := errors.New("close sentinel")
	original := &trackingRequestBody{
		Reader:   bytes.NewReader([]byte(`{"model":"gpt-test"}`)),
		closeErr: sentinel,
	}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Body = original
	c.Request.ContentLength = int64(len(`{"model":"gpt-test"}`))

	capture := installOpsRequestBodyCapture(c)
	finishOpsRequestBodyCapture(c, capture)

	restored, ok := c.Request.Body.(*restoredOpsRequestBody)
	require.True(t, ok)
	data, err := io.ReadAll(restored)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-test"}`, string(data))
	require.ErrorIs(t, restored.Close(), sentinel)
	require.Equal(t, 1, original.closeCalls)
}

func TestBuildOpsRequestDetailsJSONPreservesRawQueryAndMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/responses?trace=fixture&token=query-secret",
		strings.NewReader(`{"model":"gpt-test","prompt":"hello"}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer header-secret")
	c.Request.Header.Set("User-Agent", "fixture-client/1.0")
	capture := installOpsRequestBodyCapture(c)
	_, err := io.ReadAll(c.Request.Body)
	require.NoError(t, err)

	raw := buildOpsRequestDetailsJSON(c, capture)
	require.NotEmpty(t, raw)
	require.Contains(t, raw, "query-secret")
	require.Contains(t, raw, "header-secret")
	require.Contains(t, raw, "fixture")
	require.Contains(t, raw, "fixture-client/1.0")
}

func TestBuildOpsRequestDetailsJSONUsesDecodedCompressedBodyAndPreservesWireMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	payload := []byte(`{"model":"gpt-test","prompt":"compressed fixture"}`)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses?trace=fixture", bytes.NewReader(compressed.Bytes()))
	c.Request.ContentLength = int64(compressed.Len())
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Content-Encoding", "gzip")
	capture := installOpsRequestBodyCapture(c)
	decoded, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	require.NoError(t, err)
	require.Equal(t, payload, decoded)

	raw := buildOpsRequestDetailsJSON(c, capture)
	require.NotEmpty(t, raw)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, "gzip", details["content_encoding"])
	require.Equal(t, float64(compressed.Len()), details["content_length"])
	headers, ok := details["headers"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []any{"gzip"}, headers["content-encoding"])
	require.Equal(t, true, details["body_decoded"])
	require.Equal(t, float64(len(payload)), details["body_bytes_decoded"])
	// `body_omitted` is emitted only when the body is unavailable; a complete
	// body is represented by the presence of `body` and a false truncation flag.
	require.NotContains(t, details, "body_omitted")
	body, ok := details["body"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "compressed fixture", body["prompt"])
}

func TestBuildOpsRequestDetailsJSONKeepsCompleteDecodedBodyWhenWirePrefixIsLarge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	payload := []byte(`{"model":"gpt-test","prompt":"many gzip members"}`)
	var compressed bytes.Buffer
	// A gzip stream may contain multiple members.  Add enough empty members to
	// make the wire representation exceed the diagnostic prefix while keeping
	// the decoded request small and complete.
	for i := 0; i < 5000; i++ {
		writer := gzip.NewWriter(&compressed)
		require.NoError(t, writer.Close())
	}
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(payload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	require.Greater(t, compressed.Len(), opsRequestBodyCaptureLimit)

	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressed.Bytes()))
	c.Request.ContentLength = int64(compressed.Len())
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Content-Encoding", "gzip")
	capture := installOpsRequestBodyCapture(c)
	decoded, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	require.NoError(t, err)
	require.Equal(t, payload, decoded)

	raw := buildOpsRequestDetailsJSON(c, capture)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, false, details["body_truncated"], "decoded body should not be marked truncated")
	require.Equal(t, float64(compressed.Len()), details["body_bytes_read"])
	require.NotContains(t, details, "body_omitted", "complete decoded payload should remain visible")
	body, ok := details["body"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "many gzip members", body["prompt"])
}

func TestBuildOpsRequestDetailsJSONUsesLenientNormalizedIdentityBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	// The gateway's lenient reader accepts a BOM and raw control bytes inside
	// JSON strings after normalizing them. The request capture itself retains
	// the original wire bytes, so diagnostics must apply the same normalization
	// before attempting to decode the body.
	payload := []byte("\xef\xbb\xbf{\"model\":\"gpt-test\",\"prompt\":\"hello\x00world\"}")
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	c.Request.ContentLength = int64(len(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	capture := installOpsRequestBodyCapture(c)
	normalized, err := pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(c.Request, int64(opsRequestBodyCaptureLimit))
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-test","prompt":"hello\u0000world"}`, string(normalized))

	raw := buildOpsRequestDetailsJSON(c, capture)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, true, details["body_normalized"])
	body, ok := details["body"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello\x00world", body["prompt"])
}

func TestBuildOpsRequestDetailsJSONDoesNotNormalizeStrictRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	// Binary/image-style handlers use the strict reader.  Even if a payload
	// happens to look like JSON, diagnostics must not silently apply the
	// compatibility reader's BOM/control-byte repairs on their behalf.
	payload := []byte("\xef\xbb\xbf{\"model\":\"gpt-test\",\"prompt\":\"hello\x00world\"}")
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images", bytes.NewReader(payload))
	c.Request.ContentLength = int64(len(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	capture := installOpsRequestBodyCapture(c)
	_, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	require.NoError(t, err)

	raw := buildOpsRequestDetailsJSON(c, capture)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, true, details["body_omitted"])
	require.Equal(t, "invalid_json", details["body_omitted_reason"])
	require.NotContains(t, details, "body")
}

func TestBuildOpsRequestDetailsJSONPreservesDecodedAndNormalizedSizes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	decodedPayload := []byte("\xef\xbb\xbf{\"model\":\"gpt-test\",\"prompt\":\"hello\x00world\"}")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write(decodedPayload)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(compressed.Bytes()))
	c.Request.ContentLength = int64(compressed.Len())
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Content-Encoding", "gzip")
	capture := installOpsRequestBodyCapture(c)
	normalized, err := pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(c.Request, int64(opsRequestBodyCaptureLimit))
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-test","prompt":"hello\u0000world"}`, string(normalized))

	raw := buildOpsRequestDetailsJSON(c, capture)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, true, details["body_decoded"])
	require.Equal(t, float64(len(decodedPayload)), details["body_bytes_decoded"])
	require.Equal(t, true, details["body_normalized"])
	require.Equal(t, float64(len(normalized)), details["body_bytes_normalized"])
	require.NotContains(t, details, "body_omitted")
	body, ok := details["body"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello\x00world", body["prompt"])
}

func TestBuildOpsRequestDetailsJSONMarksNormalizedLimitAsTruncated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	// The compatibility reader expands raw control bytes to six-byte unicode
	// escapes. A small normalization limit can therefore reject an otherwise
	// completely read request; diagnostics should report the size boundary,
	// not attempt to parse the unnormalized bytes as invalid JSON.
	payload := []byte(`{"model":"gpt-test","prompt":"` + strings.Repeat("\x00", 8) + `"}`)
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	c.Request.ContentLength = int64(len(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	capture := installOpsRequestBodyCapture(c)
	_, err := pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(c.Request, int64(len(payload)+1))
	require.Error(t, err)
	var maxErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxErr)

	raw := buildOpsRequestDetailsJSON(c, capture)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, true, details["body_truncated"])
	require.Equal(t, "truncated", details["body_omitted_reason"])
	require.NotContains(t, details, "body")
}

func TestBuildOpsRequestDetailsJSONMarksCompressedDecodeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	rawCompressed := []byte("not-a-gzip-stream")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(rawCompressed))
	c.Request.ContentLength = int64(len(rawCompressed))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Content-Encoding", "gzip")
	capture := installOpsRequestBodyCapture(c)
	_, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	require.Error(t, err)

	raw := buildOpsRequestDetailsJSON(c, capture)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, "gzip", details["content_encoding"])
	require.Equal(t, true, details["body_omitted"])
	require.Equal(t, "decompression_failed", details["body_omitted_reason"])
}

func TestRequestBodyCaptureMarksReadErrorsAsOmitted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	readErr := errors.New("body read failed")
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Body = &errorRequestBody{err: readErr}
	c.Request.ContentLength = -1
	capture := installOpsRequestBodyCapture(c)
	_, err := io.ReadAll(c.Request.Body)
	require.ErrorIs(t, err, readErr)

	raw := buildOpsRequestDetailsJSON(c, capture)
	require.NotEmpty(t, raw)
	require.Contains(t, raw, `"body_omitted":true`)
	require.Contains(t, raw, `"body_omitted_reason":"read_error"`)
}

func TestRequestBodyCaptureMarksMaxBytesErrorsAsTruncated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	payload := []byte(`{"model":"gpt-test","prompt":"oversized"}`)
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(payload))
	request.ContentLength = int64(len(payload))
	request.Body = http.MaxBytesReader(recorder, request.Body, int64(len(payload)-2))
	c.Request = request
	capture := installOpsRequestBodyCapture(c)
	_, err := io.ReadAll(c.Request.Body)
	require.Error(t, err)
	var maxErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxErr)

	raw := buildOpsRequestDetailsJSON(c, capture)
	var details map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &details))
	require.Equal(t, true, details["body_omitted"])
	require.Equal(t, "truncated", details["body_omitted_reason"])
}

type errorRequestBody struct {
	err error
}

func (b *errorRequestBody) Read([]byte) (int, error) { return 0, b.err }
func (b *errorRequestBody) Close() error             { return nil }
