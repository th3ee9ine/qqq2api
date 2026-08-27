package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type openAIHTTPAffinityWriteProbe struct {
	*httptest.ResponseRecorder
	once        sync.Once
	beforeWrite func()
}

func (r *openAIHTTPAffinityWriteProbe) Write(payload []byte) (int, error) {
	r.once.Do(func() {
		if r.beforeWrite != nil {
			r.beforeWrite()
		}
	})
	return r.ResponseRecorder.Write(payload)
}

func newOpenAIHTTPAffinityProbeContext(
	t *testing.T,
	svc *OpenAIGatewayService,
	account *Account,
	groupID int64,
	responseID string,
) (*gin.Context, *openAIHTTPAffinityWriteProbe, *bool) {
	t.Helper()
	probeCalled := false
	probe := &openAIHTTPAffinityWriteProbe{ResponseRecorder: httptest.NewRecorder()}
	probe.beforeWrite = func() {
		probeCalled = true
		store := svc.getOpenAIWSStateStore()
		boundAccountID, err := store.GetResponseAccount(context.Background(), groupID, responseID)
		require.NoError(t, err)
		require.Equal(t, account.ID, boundAccountID, "account affinity must exist before response bytes are visible")
		ownerUserID, ownerAPIKeyID, found, err := store.GetHTTPResponseOwner(context.Background(), groupID, responseID)
		require.NoError(t, err)
		require.True(t, found, "owner affinity must exist before response bytes are visible")
		require.Equal(t, int64(601), ownerUserID)
		require.Equal(t, int64(501), ownerAPIKeyID)
	}
	c, _ := gin.CreateTestContext(probe)
	c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
	c.Set("api_key", &APIKey{ID: 501, GroupID: &groupID})
	SetOpenAIHTTPResponseOwner(c, 601, 501)
	return c, probe, &probeCalled
}

func TestOpenAIHTTPResponseAffinityBoundBeforeNativeStreamingWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{}, toolCorrector: NewCodexToolCorrector()}
	account := &Account{ID: 37011, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Name: "native-api-key"}
	groupID := int64(4201)
	responseID := "resp_native_stream_prewrite"
	c, probe, probeCalled := newOpenAIHTTPAffinityProbeContext(t, svc, account, groupID, responseID)
	upstream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_native_stream_prewrite","status":"in_progress"}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","response_id":"resp_native_stream_prewrite","delta":"ready"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_native_stream_prewrite","status":"completed","output":[{"type":"message"}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		"",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}

	result, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "gpt-5", "gpt-5")
	require.NoError(t, err)
	require.Equal(t, responseID, result.responseID)
	require.True(t, *probeCalled)
	require.Contains(t, probe.Body.String(), `"type":"response.created"`)
}

func TestOpenAIHTTPResponseAffinityBoundBeforePassthroughStreamingWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	account := &Account{ID: 37012, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Name: "passthrough-api-key"}
	groupID := int64(4202)
	responseID := "resp_passthrough_stream_prewrite"
	c, probe, probeCalled := newOpenAIHTTPAffinityProbeContext(t, svc, account, groupID, responseID)
	upstream := strings.Join([]string{
		"event: response.created",
		`data: {"type":"response.created","response":{"id":"resp_passthrough_stream_prewrite","status":"in_progress"}}`,
		"",
		"event: response.output_text.delta",
		`data: {"type":"response.output_text.delta","response_id":"resp_passthrough_stream_prewrite","delta":"ready"}`,
		"",
		"event: response.completed",
		`data: {"type":"response.completed","response":{"id":"resp_passthrough_stream_prewrite","status":"completed","output":[{"type":"message"}],"usage":{"input_tokens":1,"output_tokens":1}}}`,
		"",
		"",
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(upstream)),
	}

	result, err := svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, time.Now(), "gpt-5", "gpt-5")
	require.NoError(t, err)
	require.Equal(t, responseID, result.responseID)
	require.True(t, *probeCalled)
	require.Contains(t, probe.Body.String(), `"type":"response.created"`)
}

func TestOpenAIHTTPResponseAffinityBoundBeforeNonStreamingWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		passthrough bool
		responseID  string
	}{
		{name: "native", responseID: "resp_native_json_prewrite"},
		{name: "passthrough", passthrough: true, responseID: "resp_passthrough_json_prewrite"},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{}, toolCorrector: NewCodexToolCorrector()}
			account := &Account{ID: int64(37020 + i), Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Name: tc.name + "-api-key"}
			groupID := int64(4210 + i)
			c, probe, probeCalled := newOpenAIHTTPAffinityProbeContext(t, svc, account, groupID, tc.responseID)
			body := `{"id":"` + tc.responseID + `","object":"response","model":"gpt-5","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}

			if tc.passthrough {
				result, err := svc.handleNonStreamingResponsePassthrough(context.Background(), resp, c, account, "gpt-5", "gpt-5")
				require.NoError(t, err)
				require.Equal(t, tc.responseID, result.responseID)
			} else {
				result, err := svc.handleNonStreamingResponse(context.Background(), resp, c, account, "gpt-5", "gpt-5")
				require.NoError(t, err)
				require.Equal(t, tc.responseID, result.responseID)
			}
			require.True(t, *probeCalled)
			require.Contains(t, probe.Body.String(), tc.responseID)
		})
	}
}
