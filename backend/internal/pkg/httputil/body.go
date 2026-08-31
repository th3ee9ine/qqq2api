package httputil

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const (
	requestBodyReadInitCap    = 512
	requestBodyReadMaxInitCap = 1 << 20
	jsonUTF8BOMLen            = 3
	// maxDecompressedBodySize limits the decompressed request body to 64 MB
	// to prevent decompression bomb attacks.
	maxDecompressedBodySize = 64 << 20
)

// ReadRequestBodyWithPrealloc reads request body with preallocated buffer based
// on content length, transparently decoding any Content-Encoding the upstream
// client used to compress the body (zstd, gzip, deflate).
func ReadRequestBodyWithPrealloc(req *http.Request) ([]byte, error) {
	if req == nil || req.Body == nil {
		return nil, nil
	}
	// Keep an optional observer before consuming req.Body. The Ops request
	// capture uses this hook to retain the decoded representation for
	// diagnostics; callers that do not implement it are unaffected.
	observer := req.Body

	capHint := requestBodyReadInitCap
	if req.ContentLength > 0 {
		switch {
		case req.ContentLength < int64(requestBodyReadInitCap):
			capHint = requestBodyReadInitCap
		case req.ContentLength > int64(requestBodyReadMaxInitCap):
			capHint = requestBodyReadMaxInitCap
		default:
			capHint = int(req.ContentLength)
		}
	}

	buf := bytes.NewBuffer(make([]byte, 0, capHint))
	if _, err := io.Copy(buf, req.Body); err != nil {
		return nil, err
	}
	raw := buf.Bytes()

	enc := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
	if enc == "" || enc == "identity" {
		return raw, nil
	}

	decoded, err := decompressRequestBody(enc, raw)
	if err != nil {
		markRequestBodyDecodeError(observer)
		return nil, fmt.Errorf("decode Content-Encoding %q: %w", enc, err)
	}
	recordDecodedRequestBody(observer, decoded)

	req.Header.Del("Content-Encoding")
	req.Header.Del("Content-Length")
	req.ContentLength = int64(len(decoded))

	return decoded, nil
}

// recordDecodedRequestBody notifies an optional request-body observer without
// coupling this utility package to any particular middleware implementation.
// The structural interface keeps the hook source-compatible with existing
// callers and silently does nothing for ordinary io.ReadClosers.
func recordDecodedRequestBody(body io.ReadCloser, decoded []byte) {
	if observer, ok := body.(interface{ SetDecodedRequestBody([]byte) }); ok {
		observer.SetDecodedRequestBody(decoded)
	}
}

// recordNormalizedRequestBody notifies an optional observer about the exact
// representation returned by the lenient JSON reader.  This is kept separate
// from the decoded hook: compressed requests have two useful sizes (the
// decompressed wire payload and the post-normalization JSON payload), and
// conflating them makes diagnostics inaccurate.
func recordNormalizedRequestBody(body io.ReadCloser, normalized []byte) {
	if observer, ok := body.(interface{ SetNormalizedRequestBody([]byte) }); ok {
		observer.SetNormalizedRequestBody(normalized)
	}
}

// markLenientJSONRequestBody identifies observers attached to requests that
// are parsed through the compatibility reader.  Diagnostics use this marker
// to reproduce BOM/control-byte normalization only for routes that actually
// apply the lenient parser; strict binary/JSON readers keep the original wire
// bytes intact.
func markLenientJSONRequestBody(body io.ReadCloser) {
	if observer, ok := body.(interface{ MarkLenientJSONRequestBody() }); ok {
		observer.MarkLenientJSONRequestBody()
	}
}

// markNormalizedRequestBodyLimit reports that the lenient parser rejected a
// payload because its normalized representation exceeded the configured
// limit. Observers can then preserve a truthful "truncated" diagnostic
// instead of attempting to parse the unnormalized prefix.
func markNormalizedRequestBodyLimit(body io.ReadCloser) {
	if observer, ok := body.(interface{ MarkNormalizedRequestBodyLimit() }); ok {
		observer.MarkNormalizedRequestBodyLimit()
	}
}

func markRequestBodyDecodeError(body io.ReadCloser) {
	if observer, ok := body.(interface{ MarkRequestBodyDecodeError() }); ok {
		observer.MarkRequestBodyDecodeError()
	}
}

// ReadLenientJSONRequestBodyWithPrealloc reads a request body and normalizes
// JSON string control bytes before strict validation.
func ReadLenientJSONRequestBodyWithPrealloc(req *http.Request, maxNormalizedBytes int64) ([]byte, error) {
	// Keep the wire encoding before ReadRequestBodyWithPrealloc clears it.  An
	// Ops request capture already observes the raw bytes, but compressed
	// requests need the post-normalization representation for diagnostics.
	var observer io.ReadCloser
	compressed := false
	if req != nil {
		observer = req.Body
		markLenientJSONRequestBody(observer)
		enc := strings.ToLower(strings.TrimSpace(req.Header.Get("Content-Encoding")))
		compressed = enc != "" && enc != "identity"
	}
	body, err := ReadRequestBodyWithPrealloc(req)
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizeLenientJSONRequestBody(body, maxNormalizedBytes)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			markNormalizedRequestBodyLimit(observer)
		}
		return nil, err
	}
	// For compressed requests, always retain the normalized representation even
	// when it is byte-for-byte identical to the decoded payload.  For identity
	// requests, only retain it when normalization changed the wire bytes; the
	// raw capture is already the exact representation otherwise.
	if compressed || !bytes.Equal(normalized, body) {
		recordNormalizedRequestBody(observer, normalized)
	}
	return normalized, nil
}

func decompressRequestBody(encoding string, raw []byte) ([]byte, error) {
	switch encoding {
	case "zstd":
		dec, err := zstd.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer dec.Close()
		return io.ReadAll(io.LimitReader(dec, maxDecompressedBodySize))
	case "gzip", "x-gzip":
		gr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		return io.ReadAll(io.LimitReader(gr, maxDecompressedBodySize))
	case "deflate":
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer func() { _ = zr.Close() }()
		return io.ReadAll(io.LimitReader(zr, maxDecompressedBodySize))
	default:
		return nil, errors.New("unsupported Content-Encoding")
	}
}

// NormalizeLenientJSONRequestBody escapes raw control bytes that broken
// OpenAI-compatible clients sometimes place inside JSON strings.
func NormalizeLenientJSONRequestBody(body []byte, maxNormalizedBytes int64) ([]byte, error) {
	if maxNormalizedBytes <= 0 {
		maxNormalizedBytes = maxDecompressedBodySize
	}

	body = trimUTF8BOM(body)
	if len(body) == 0 {
		return body, nil
	}
	if int64(len(body)) > maxNormalizedBytes {
		return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
	}

	var out []byte
	inString := false
	escaped := false
	for i, b := range body {
		if inString && isJSONControlByte(b) {
			if out == nil {
				capHint := len(body) + 6
				if int64(capHint) > maxNormalizedBytes {
					capHint = int(maxNormalizedBytes)
				}
				out = make([]byte, 0, capHint)
				out = append(out, body[:i]...)
			}
			if int64(len(out)+6) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = appendJSONUnicodeEscape(out, b)
			escaped = false
			continue
		}

		switch {
		case escaped:
			escaped = false
		case inString && b == '\\':
			escaped = true
		case b == '"':
			inString = !inString
		}

		if out != nil {
			if int64(len(out)+1) > maxNormalizedBytes {
				return nil, &http.MaxBytesError{Limit: maxNormalizedBytes}
			}
			out = append(out, b)
		}
	}
	if out != nil {
		return out, nil
	}
	return body, nil
}

func trimUTF8BOM(body []byte) []byte {
	if len(body) >= jsonUTF8BOMLen && body[0] == 0xef && body[1] == 0xbb && body[2] == 0xbf {
		return body[jsonUTF8BOMLen:]
	}
	return body
}

func isJSONControlByte(b byte) bool {
	return b < 0x20 || b == 0x7f
}

func appendJSONUnicodeEscape(dst []byte, b byte) []byte {
	const hex = "0123456789abcdef"
	return append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0x0f])
}
