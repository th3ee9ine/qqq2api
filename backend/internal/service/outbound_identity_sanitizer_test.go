package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
)

func TestSanitizeOutboundGatewayIdentity(t *testing.T) {
	headers := make(http.Header)
	headers.Set("X-Sub2API-Trace", "trace-1")
	headers.Set("X-QQQ2API-Source", "gateway")
	headers.Set("User-Agent", "sub2api-openai/1")
	headers.Set("X-Origin", "qqq2api-gateway")
	headers.Set("X-User-Agent", "sub2api-client/1")
	headers.Add("X-Client-Name", "keep")
	headers.Add("X-Client-Name", "via QQQ2API")
	headers.Set("X-Unrelated", "via sub2api but not identity metadata")
	headers.Set("Authorization", "Bearer token-with-sub2api-text")
	headers.Set("X-API-Key", "opaque-sub2api-api-key")
	headers.Set("X-Goog-API-Key", "opaque-qqq2api-api-key")
	headers.Set("Cookie", "session=qqq2api-opaque")
	headers.Set("OpenAI-Organization", "org-example")

	SanitizeOutboundGatewayIdentity(headers)

	require.Empty(t, headers.Values("X-Sub2API-Trace"))
	require.Empty(t, headers.Values("X-QQQ2API-Source"))
	require.Empty(t, headers.Values("User-Agent"))
	require.Empty(t, headers.Values("X-Origin"))
	require.Empty(t, headers.Values("X-User-Agent"))
	require.Equal(t, []string{"keep"}, headers.Values("X-Client-Name"))
	require.Equal(t, "via sub2api but not identity metadata", headers.Get("X-Unrelated"))
	require.Equal(t, "Bearer token-with-sub2api-text", headers.Get("Authorization"))
	require.Equal(t, "opaque-sub2api-api-key", headers.Get("X-API-Key"))
	require.Equal(t, "opaque-qqq2api-api-key", headers.Get("X-Goog-API-Key"))
	require.Equal(t, "session=qqq2api-opaque", headers.Get("Cookie"))
	require.Equal(t, "org-example", headers.Get("OpenAI-Organization"))
}

type identitySanitizerHTTPUpstream struct {
	request *http.Request
}

func (u *identitySanitizerHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("{}")),
	}, nil
}

func (u *identitySanitizerHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func TestDoOpenAIUpstreamSanitizesHeadersAtFinalEgress(t *testing.T) {
	upstream := &identitySanitizerHTTPUpstream{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	req, err := http.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", strings.NewReader(`{"input":"<sub2api-claude-code-todo-guard>x</sub2api-claude-code-todo-guard>"}`))
	require.NoError(t, err)
	req.Header.Set("X-Sub2API-Trace", "trace")
	req.Header.Set("User-Agent", "qqq2api-test")
	req.Header.Set("Authorization", "Bearer token")

	resp, err := svc.doOpenAIUpstream(req, "", &Account{ID: 1, Concurrency: 1})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NoError(t, resp.Body.Close())
	require.NotNil(t, upstream.request)
	require.Empty(t, upstream.request.Header.Get("X-Sub2API-Trace"))
	require.Empty(t, upstream.request.Header.Get("User-Agent"))
	require.Equal(t, "Bearer token", upstream.request.Header.Get("Authorization"))
	body, err := io.ReadAll(upstream.request.Body)
	require.NoError(t, err)
	require.NotContains(t, strings.ToLower(string(body)), "sub2api")
	require.Contains(t, string(body), openAICompatClaudeCodeTodoGuardMarker)
}
