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

func TestBoundedOpsRequestQueryRedactsSensitiveLongKeyBeforeTruncation(t *testing.T) {
	longKey := strings.Repeat("safe", 600) + "access_token"
	values := url.Values{}
	values.Add(longKey, "query-secret")
	values.Add("trace", "fixture")

	out, truncated := boundedOpsRequestQuery(values)
	require.False(t, truncated)
	require.Equal(t, []string{"[REDACTED]"}, out[truncateString(longKey, opsRequestDetailsMaxValueBytes)])
	require.Equal(t, []string{"fixture"}, out["trace"])
}

func TestBoundedOpsRequestHeadersIncludesOnlySafeOrRedactedValues(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer header-secret")
	headers.Set("Cookie", "session=header-secret")
	headers.Set("User-Agent", "fixture-client/1.0")
	headers.Set("X-Unlisted-Header", "must-not-be-stored")

	out, truncated := boundedOpsRequestHeaders(headers)
	require.False(t, truncated)
	require.Equal(t, []string{"[REDACTED]"}, out["authorization"])
	require.Equal(t, []string{"[REDACTED]"}, out["cookie"])
	require.Equal(t, []string{"fixture-client/1.0"}, out["user-agent"])
	require.NotContains(t, out, "x-unlisted-header")
	require.NotContains(t, out, "header-secret")
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

func TestBuildOpsRequestDetailsJSONPreservesSafeQueryAndRedactsSensitiveMetadata(t *testing.T) {
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
	require.NotContains(t, raw, "query-secret")
	require.NotContains(t, raw, "header-secret")
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
	require.Equal(t, true, details["body_decoded"])
	require.Equal(t, float64(len(payload)), details["body_bytes_decoded"])
	// `body_omitted` is emitted only when the body is unavailable; a complete
	// body is represented by the presence of `body` and a false truncation flag.
	require.NotContains(t, details, "body_omitted")
	body, ok := details["body"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "compressed fixture", body["prompt"])
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

type errorRequestBody struct {
	err error
}

func (b *errorRequestBody) Read([]byte) (int, error) { return 0, b.err }
func (b *errorRequestBody) Close() error             { return nil }
