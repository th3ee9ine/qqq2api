//go:build unit

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"github.com/tidwall/gjson"
)

type capacityRetryLoopUpstream struct {
	service.HTTPUpstream
	mu   sync.Mutex
	hits []int64
	// -1 keeps the upstream overloaded; nonnegative values allow recovery.
	failures int
	statuses []int
	sseError bool
}

func (u *capacityRetryLoopUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.hits = append(u.hits, accountID)
	fail := u.failures != 0
	status := http.StatusServiceUnavailable
	if len(u.statuses) > 0 {
		status = u.statuses[0]
		u.statuses = u.statuses[1:]
		fail = status >= 400
	}
	if u.failures > 0 {
		u.failures--
	}
	u.mu.Unlock()
	if fail {
		if u.sseError {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}, "Retry-After": {"2"}},
				Body: io.NopCloser(strings.NewReader("event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_capacity\"}}\n\n" +
					"event: error\ndata: {\"type\":\"error\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}\n\n")),
			}, nil
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": {"application/json"}, "Retry-After": {"2"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"type":"server_error","code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`)),
		}, nil
	}
	return (&openAICredentialFailoverUpstream{}).Do(req, "", accountID, 0)
}

func (u *capacityRetryLoopUpstream) accountHits() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.hits...)
}

func newCapacityRetryLoopHandler(t *testing.T, attempts, failures int, disableSameAccount bool) (*capacityRetryLoopUpstream, *gin.Engine) {
	t.Helper()
	groupID := int64(901)
	repo := &openAICredentialFailoverRepo{}
	for i := 1; i <= 12; i++ {
		credentials := map[string]any{"api_key": "test-key", "base_url": "https://api.openai.com/v1"}
		if disableSameAccount {
			credentials["pool_mode"] = true
			credentials["pool_mode_retry_count"] = 0
		}
		repo.accounts = append(repo.accounts, service.Account{
			ID: int64(i), Name: "capacity-test", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: i,
			Credentials: credentials,
		})
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 10
	cfg.Gateway.OpenAICapacityRetryMaxAttempts = attempts
	cfg.Gateway.OpenAIWS.ForceHTTP = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = service.OpenAIWSIngressModeHTTPBridge
	upstream := &capacityRetryLoopUpstream{failures: failures}
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	rateLimits := service.NewRateLimitService(repo, cfg, nil)
	gateway := service.NewOpenAIGatewayService(
		repo, nil, nil, nil, nil, nil, nil, cfg, nil, nil,
		service.NewBillingService(cfg, nil), rateLimits, billingCache, upstream,
		&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil,
	)
	cache := &concurrencyCacheMock{
		acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
	}
	h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billingCache, &service.APIKeyService{}, nil, nil, nil, nil, cfg)
	apiKey := &service.APIKey{
		ID: 902, GroupID: &groupID,
		User:  &service.User{ID: 903, Status: service.StatusActive},
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive, AllowMessagesDispatch: true, AllowImageGeneration: true},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.POST("/v1/responses", h.Responses)
	router.GET("/v1/responses", h.ResponsesWebSocket)
	router.POST("/v1/chat/completions", h.ChatCompletions)
	router.POST("/v1/messages", h.Messages)
	router.POST("/v1/embeddings", h.Embeddings)
	router.POST("/v1/images/generations", h.Images)
	router.POST("/v1/responses/alpha_search", h.AlphaSearch)
	return upstream, router
}

func TestOpenAICapacityRetryHTTPBoundsUpstreamAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name               string
		path               string
		body               string
		attempts           int
		failures           int
		disableSameAccount bool
		wantHits           int
		wantStatus         int
	}{
		{"responses default", "/v1/responses", `{"model":"gpt-5.3-codex","input":"hello","stream":false}`, 0, -1, false, 3, 503},
		{"responses streaming", "/v1/responses", `{"model":"gpt-5.3-codex","input":"hello","stream":true}`, 0, -1, false, 3, 503},
		{"chat completions", "/v1/chat/completions", `{"model":"gpt-5.3-codex","messages":[{"role":"user","content":"hello"}]}`, 0, -1, false, 3, 503},
		{"messages", "/v1/messages", `{"model":"gpt-5.3-codex","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`, 0, -1, false, 3, 503},
		{"embeddings", "/v1/embeddings", `{"model":"text-embedding-3-small","input":"hello"}`, 0, -1, false, 3, 503},
		{"images", "/v1/images/generations", `{"model":"gpt-image-1","prompt":"a blue square"}`, 0, -1, false, 3, 503},
		{"alpha search", "/v1/responses/alpha_search", `{"model":"gpt-5.3-codex","query":"hello"}`, 0, -1, false, 3, 503},
		{"single attempt", "/v1/responses", `{"model":"gpt-5.3-codex","input":"hello"}`, 1, -1, false, 1, 503},
		{"account switching shares budget", "/v1/responses", `{"model":"gpt-5.3-codex","input":"hello"}`, 2, -1, true, 2, 503},
		{"recovery succeeds", "/v1/responses", `{"model":"gpt-5.3-codex","input":"hello"}`, 3, 2, false, 3, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream, router := newCapacityRetryLoopHandler(t, tc.attempts, tc.failures, tc.disableSameAccount)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, tc.wantStatus, recorder.Code, recorder.Body.String())
			require.Len(t, upstream.accountHits(), tc.wantHits)
			if tc.wantStatus == 503 {
				require.Contains(t, recorder.Body.String(), "overloaded")
				require.Equal(t, "2", recorder.Header().Get("Retry-After"))
			}
			if tc.disableSameAccount {
				require.Equal(t, []int64{1, 2}, upstream.accountHits())
			}
		})
	}
}

func TestOpenAICapacityRetryHTTP200StreamErrorBoundsUpstreamAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []string{"false", "true"} {
		t.Run("stream="+stream, func(t *testing.T) {
			upstream, router := newCapacityRetryLoopHandler(t, 3, -1, false)
			upstream.sseError = true
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.3-codex","input":"hello","stream":`+stream+`}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
			require.Len(t, upstream.accountHits(), 3)
			require.Contains(t, recorder.Body.String(), "overloaded")
			require.NotContains(t, recorder.Body.String(), "resp_capacity", "metadata must stay buffered before failover")
		})
	}
}

func TestOpenAICapacityRetryWebSocketBoundsUpstreamAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream, router := newCapacityRetryLoopHandler(t, 3, -1, false)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.3-codex","input":"hello"}`)))
	for {
		_, _, err = conn.Read(ctx)
		if err != nil {
			break
		}
	}
	require.NotEqual(t, context.DeadlineExceeded, ctx.Err())
	require.Len(t, upstream.accountHits(), 3)
}

func TestOpenAICapacityRetryWebSocketSuccessfulTurnResetsAccountAllowance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream, router := newCapacityRetryLoopHandler(t, 3, 0, false)
	upstream.statuses = []int{429, 200, 429, 200}
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	for turn := 0; turn < 2; turn++ {
		require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.3-codex","input":"hello"}`)))
		for {
			_, payload, readErr := conn.Read(ctx)
			require.NoError(t, readErr)
			if gjson.GetBytes(payload, "type").String() == "response.completed" {
				break
			}
		}
	}
	require.Equal(t, []int64{1, 1, 1, 1}, upstream.accountHits())
}
