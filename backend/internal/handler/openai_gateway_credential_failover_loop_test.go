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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// openAICredentialFailoverRepo intentionally models only active OpenAI/Codex
// accounts. The old version of this suite used Grok OAuth accounts, which made
// a retired provider look like a supported OpenAI routing target.
type openAICredentialFailoverRepo struct {
	service.AccountRepository
	mu             sync.Mutex
	accounts       []service.Account
	setErrorIDs    []int64
	selectionCalls int
}

func (r *openAICredentialFailoverRepo) list(platform string) []service.Account {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.selectionCalls++
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform && account.IsSchedulable() {
			out = append(out, account)
		}
	}
	return out
}

func (r *openAICredentialFailoverRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.list(platform), nil
}

func (r *openAICredentialFailoverRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.list(platform), nil
}

func (r *openAICredentialFailoverRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.list(platform), nil
}

func (r *openAICredentialFailoverRepo) ListModelAvailabilityCandidates(_ context.Context, _ *int64, platforms []string, _ bool) ([]service.Account, error) {
	platform := service.PlatformOpenAI
	if len(platforms) > 0 {
		platform = platforms[0]
	}
	return r.list(platform), nil
}

func (r *openAICredentialFailoverRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			copy := r.accounts[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (r *openAICredentialFailoverRepo) SetError(_ context.Context, id int64, message string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setErrorIDs = append(r.setErrorIDs, id)
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			r.accounts[i].Status = service.StatusError
			r.accounts[i].Schedulable = false
			r.accounts[i].ErrorMessage = message
		}
	}
	return nil
}

func (r *openAICredentialFailoverRepo) SetTempUnschedulable(_ context.Context, id int64, until time.Time, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			value := until
			r.accounts[i].TempUnschedulableUntil = &value
		}
	}
	return nil
}

func (r *openAICredentialFailoverRepo) UpdateLastUsed(context.Context, int64) error {
	return nil
}

func (r *openAICredentialFailoverRepo) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}

func (r *openAICredentialFailoverRepo) selectorCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.selectionCalls
}

type openAICredentialFailoverUpstream struct {
	service.HTTPUpstream
	mu      sync.Mutex
	hits    []int64
	failIDs map[int64]bool
}

func (u *openAICredentialFailoverUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	requestBody, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	u.hits = append(u.hits, accountID)
	shouldFail := u.failIDs[accountID]
	u.mu.Unlock()
	if shouldFail {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"type":"authentication_error","code":"invalid_api_key","message":"credential rejected"}}`,
			)),
		}, nil
	}
	if bytes.Contains(requestBody, []byte(`"stream":true`)) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_healthy\",\"model\":\"gpt-5.3-codex\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n",
			)),
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_healthy","object":"response","model":"gpt-5.3-codex","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`,
		)),
	}, nil
}

func (u *openAICredentialFailoverUpstream) accountHits() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.hits...)
}

func newOpenAICredentialFailoverHandler(t *testing.T, failIDs map[int64]bool) (*openAICredentialFailoverRepo, *openAICredentialFailoverUpstream, *gin.Engine, func()) {
	t.Helper()
	groupID := int64(901)
	repo := &openAICredentialFailoverRepo{accounts: []service.Account{
		{
			ID: 801, Name: "rejected", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
			Credentials: map[string]any{"api_key": "rejected-key", "base_url": "https://api.openai.com/v1"},
		},
		{
			ID: 802, Name: "healthy", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
			Status: service.StatusActive, Schedulable: true, Concurrency: 1, Priority: 2,
			Credentials: map[string]any{"api_key": "healthy-key", "base_url": "https://api.openai.com/v1"},
		},
	}}
	upstream := &openAICredentialFailoverUpstream{failIDs: failIDs}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = 3
	cfg.Gateway.OpenAIWS.ForceHTTP = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.IngressModeDefault = service.OpenAIWSIngressModeHTTPBridge
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	rateLimits := service.NewRateLimitService(repo, nil, cfg, nil, nil)
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
		Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.User.ID, Concurrency: 1})
		c.Next()
	})
	router.POST("/openai/v1/responses", h.Responses)
	router.GET("/openai/v1/responses", h.ResponsesWebSocket)
	cleanup := func() { billingCache.Stop() }
	return repo, upstream, router, cleanup
}

func TestResponsesCredentialFailoverLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("rejected OpenAI API key selects healthy account", func(t *testing.T) {
		repo, upstream, router, cleanup := newOpenAICredentialFailoverHandler(t, map[int64]bool{801: true})
		defer cleanup()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.3-codex","input":"hello","stream":false}`))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)

		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "resp_healthy")
		require.Equal(t, []int64{801, 802}, upstream.accountHits())
		require.GreaterOrEqual(t, repo.selectorCalls(), 2)
	})

	t.Run("pre-cancelled request never selects an account", func(t *testing.T) {
		repo, upstream, router, cleanup := newOpenAICredentialFailoverHandler(t, map[int64]bool{801: true})
		defer cleanup()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.3-codex","input":"hello","stream":false}`)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, req)

		require.Zero(t, repo.selectorCalls())
		require.Empty(t, upstream.accountHits())
	})
}

func TestResponsesWebSocketCredentialFailoverLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, upstream, router, cleanup := newOpenAICredentialFailoverHandler(t, map[int64]bool{801: true})
	defer cleanup()
	server := httptest.NewServer(router)
	defer server.Close()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
	conn, _, err := coderws.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http")+"/openai/v1/responses", nil)
	dialCancel()
	require.NoError(t, err)
	defer conn.CloseNow()

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.3-codex","input":"hello","stream":false}`))
	writeCancel()
	require.NoError(t, err)

	readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, payload, err := conn.Read(readCtx)
	readCancel()
	require.NoError(t, err)
	require.Contains(t, string(payload), "resp_healthy")
	require.Equal(t, []int64{801, 802}, upstream.accountHits())
}
