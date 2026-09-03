//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// cleanupHandlerAdminStub overrides only the account methods used by the
// session-cleanup endpoints. Embedding the complete interface keeps this test
// seam resilient when unrelated admin operations gain new methods.
type cleanupHandlerAdminStub struct {
	service.AdminService

	mu                 sync.Mutex
	account            *service.Account
	getErr             error
	updateExtraErr     error
	getCalls           int
	updateExtraCalls   int
	lastUpdateExtra    map[string]any
	updatedAccountCopy bool
}

var _ service.AdminService = (*cleanupHandlerAdminStub)(nil)

func (s *cleanupHandlerAdminStub) GetAccount(_ context.Context, _ int64) (*service.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.account == nil {
		return nil, nil
	}
	if !s.updatedAccountCopy {
		return s.account, nil
	}
	copy := *s.account
	copy.Extra = cloneCleanupHandlerExtra(s.account.Extra)
	return &copy, nil
}

func (s *cleanupHandlerAdminStub) UpdateAccountExtra(_ context.Context, _ int64, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateExtraCalls++
	s.lastUpdateExtra = cloneCleanupHandlerExtra(updates)
	if s.updateExtraErr != nil {
		return s.updateExtraErr
	}
	if s.account != nil {
		if s.account.Extra == nil {
			s.account.Extra = make(map[string]any)
		}
		for key, value := range updates {
			s.account.Extra[key] = value
		}
	}
	return nil
}

func (s *cleanupHandlerAdminStub) counts() (get, update int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getCalls, s.updateExtraCalls
}

func cloneCleanupHandlerExtra(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	copy := make(map[string]any, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}

type cleanupHandlerSessionRepo struct {
	service.AccountRepository
	account *service.Account
	mu      sync.Mutex
	states  []map[string]any
}

var _ service.AccountRepository = (*cleanupHandlerSessionRepo)(nil)

func (r *cleanupHandlerSessionRepo) GetByID(ctx context.Context, _ int64) (*service.Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.account == nil {
		return nil, nil
	}
	copy := *r.account
	copy.Extra = cloneCleanupHandlerExtra(r.account.Extra)
	return &copy, nil
}

func (r *cleanupHandlerSessionRepo) UpdateExtra(ctx context.Context, _ int64, updates map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, cloneCleanupHandlerExtra(updates))
	return nil
}

func (r *cleanupHandlerSessionRepo) lastState() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.states) == 0 {
		return nil
	}
	return cloneCleanupHandlerExtra(r.states[len(r.states)-1])
}

type cleanupHandlerSessionClient struct {
	list      *service.OpenAIAccountSessionList
	listErr   error
	mu        sync.Mutex
	revoked   []string
	listCalls int
	revokeErr error
}

var _ service.OpenAISessionCleanupClient = (*cleanupHandlerSessionClient)(nil)

func (c *cleanupHandlerSessionClient) ListSessions(ctx context.Context, _ int64) (*service.OpenAIAccountSessionList, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.listCalls++
	c.mu.Unlock()
	return c.list, c.listErr
}

func (c *cleanupHandlerSessionClient) RevokeSession(ctx context.Context, _ int64, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.revoked = append(c.revoked, id)
	return c.revokeErr
}

func (c *cleanupHandlerSessionClient) revokedIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.revoked...)
}

func openAICleanupHandlerAccount(enabled bool) *service.Account {
	return &service.Account{
		ID:       42,
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeOAuth,
		Status:   service.StatusActive,
		Extra: map[string]any{
			service.OpenAISessionCleanupEnabledExtraKey:         enabled,
			service.OpenAISessionCleanupIntervalMinutesExtraKey: 15,
		},
	}
}

type cleanupHandlerEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Reason  string          `json:"reason"`
	Data    json.RawMessage `json:"data"`
}

func performOpenAISessionCleanupHandlerRequest(
	t *testing.T,
	handler *OpenAIOAuthHandler,
	method, path, body string,
) (int, cleanupHandlerEnvelope) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/admin/openai/accounts/:id/sessions/cleanup", handler.GetSessionCleanup)
	router.PUT("/api/v1/admin/openai/accounts/:id/sessions/cleanup", handler.UpdateSessionCleanup)
	router.POST("/api/v1/admin/openai/accounts/:id/sessions/cleanup/run", handler.RunSessionCleanup)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, request)
	var envelope cleanupHandlerEnvelope
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	return recorder.Code, envelope
}

func TestOpenAISessionCleanupHandlerNilAdminServiceReturnsServerError(t *testing.T) {
	handler := &OpenAIOAuthHandler{}
	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "get", method: http.MethodGet, path: "/api/v1/admin/openai/accounts/42/sessions/cleanup"},
		{name: "update", method: http.MethodPut, path: "/api/v1/admin/openai/accounts/42/sessions/cleanup", body: `{}`},
		{name: "run", method: http.MethodPost, path: "/api/v1/admin/openai/accounts/42/sessions/cleanup/run"},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, envelope := performOpenAISessionCleanupHandlerRequest(t, handler, test.method, test.path, test.body)
			require.Equal(t, http.StatusInternalServerError, status)
			require.Equal(t, http.StatusInternalServerError, envelope.Code)
		})
	}
}

func TestOpenAISessionCleanupHandlerGetReturnsPolicyAndRedactedState(t *testing.T) {
	account := openAICleanupHandlerAccount(true)
	account.Extra[service.OpenAISessionCleanupStateExtraKey] = service.OpenAISessionCleanupState{
		Status:              service.OpenAISessionCleanupStatusSuccess,
		LastRunAt:           "2026-09-02T12:00:00Z",
		RevokedCount:        2,
		CurrentSessionKnown: true,
		Message:             "device ids are not exposed",
	}
	admin := &cleanupHandlerAdminStub{account: account}
	handler := &OpenAIOAuthHandler{adminService: admin}

	status, envelope := performOpenAISessionCleanupHandlerRequest(
		t,
		handler,
		http.MethodGet,
		"/api/v1/admin/openai/accounts/42/sessions/cleanup",
		"",
	)

	require.Equal(t, http.StatusOK, status)
	var data openAISessionCleanupResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.True(t, data.Enabled)
	require.Equal(t, 15, data.IntervalMinutes)
	require.NotNil(t, data.State)
	require.Equal(t, 2, data.State.RevokedCount)
	// Runtime messages are normalized to a fixed, non-sensitive value rather
	// than reflecting arbitrary legacy JSONB content back to the browser.
	require.Empty(t, data.State.Message)
	getCalls, updateCalls := admin.counts()
	require.Equal(t, 1, getCalls)
	require.Zero(t, updateCalls)
}

func TestOpenAISessionCleanupHandlerGetFailsClosedForMalformedPolicy(t *testing.T) {
	account := openAICleanupHandlerAccount(true)
	account.Extra[service.OpenAISessionCleanupIntervalMinutesExtraKey] = "invalid"
	admin := &cleanupHandlerAdminStub{account: account}
	handler := &OpenAIOAuthHandler{adminService: admin}

	status, envelope := performOpenAISessionCleanupHandlerRequest(
		t,
		handler,
		http.MethodGet,
		"/api/v1/admin/openai/accounts/42/sessions/cleanup",
		"",
	)

	require.Equal(t, http.StatusOK, status)
	var data openAISessionCleanupResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.False(t, data.Enabled)
	require.Equal(t, service.OpenAISessionCleanupDefaultIntervalMinutes, data.IntervalMinutes)
}

func TestOpenAISessionCleanupHandlerRejectsInvalidAccountIDsAndTypes(t *testing.T) {
	for _, test := range []struct {
		name    string
		path    string
		account *service.Account
	}{
		{name: "non numeric id", path: "/api/v1/admin/openai/accounts/nope/sessions/cleanup"},
		{name: "zero id", path: "/api/v1/admin/openai/accounts/0/sessions/cleanup"},
		{name: "wrong platform", path: "/api/v1/admin/openai/accounts/42/sessions/cleanup", account: func() *service.Account {
			a := openAICleanupHandlerAccount(true)
			a.Platform = service.PlatformAnthropic
			return a
		}()},
		{name: "wrong type", path: "/api/v1/admin/openai/accounts/42/sessions/cleanup", account: func() *service.Account {
			a := openAICleanupHandlerAccount(true)
			a.Type = service.AccountTypeAPIKey
			return a
		}()},
		{name: "shadow", path: "/api/v1/admin/openai/accounts/42/sessions/cleanup", account: func() *service.Account {
			a := openAICleanupHandlerAccount(true)
			parentID := int64(7)
			a.ParentAccountID = &parentID
			return a
		}()},
	} {
		t.Run(test.name, func(t *testing.T) {
			admin := &cleanupHandlerAdminStub{account: test.account}
			handler := &OpenAIOAuthHandler{adminService: admin}
			status, _ := performOpenAISessionCleanupHandlerRequest(t, handler, http.MethodGet, test.path, "")
			require.Equal(t, http.StatusBadRequest, status)
			_, updates := admin.counts()
			require.Zero(t, updates)
		})
	}
}

func TestOpenAISessionCleanupHandlerUpdateUsesAtomicExtraPatch(t *testing.T) {
	account := openAICleanupHandlerAccount(false)
	account.Extra["unrelated"] = "preserved"
	admin := &cleanupHandlerAdminStub{account: account, updatedAccountCopy: true}
	handler := &OpenAIOAuthHandler{adminService: admin}

	status, envelope := performOpenAISessionCleanupHandlerRequest(
		t,
		handler,
		http.MethodPut,
		"/api/v1/admin/openai/accounts/42/sessions/cleanup",
		`{"enabled":true,"interval_minutes":30}`,
	)

	require.Equal(t, http.StatusOK, status)
	var data openAISessionCleanupResponse
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.True(t, data.Enabled)
	require.Equal(t, 30, data.IntervalMinutes)
	getCalls, updateCalls := admin.counts()
	require.Equal(t, 2, getCalls, "one read validates the target and one reads the merged response")
	require.Equal(t, 1, updateCalls)
	admin.mu.Lock()
	defer admin.mu.Unlock()
	require.Equal(t, map[string]any{
		service.OpenAISessionCleanupEnabledExtraKey:         true,
		service.OpenAISessionCleanupIntervalMinutesExtraKey: 30,
	}, admin.lastUpdateExtra)
	require.Equal(t, "preserved", admin.account.Extra["unrelated"])
}

func TestOpenAISessionCleanupHandlerUpdatePropagatesValidationReason(t *testing.T) {
	account := openAICleanupHandlerAccount(false)
	admin := &cleanupHandlerAdminStub{
		account: account,
		updateExtraErr: infraerrors.BadRequest(
			"OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_INTERVAL_INVALID",
			"interval is outside the supported range",
		),
	}
	handler := &OpenAIOAuthHandler{adminService: admin}

	status, envelope := performOpenAISessionCleanupHandlerRequest(
		t,
		handler,
		http.MethodPut,
		"/api/v1/admin/openai/accounts/42/sessions/cleanup",
		`{"enabled":true,"interval_minutes":1}`,
	)

	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_INTERVAL_INVALID", envelope.Reason)
	_, updateCalls := admin.counts()
	require.Equal(t, 1, updateCalls)
}

func TestOpenAISessionCleanupHandlerUpdateRejectsMalformedJSON(t *testing.T) {
	account := openAICleanupHandlerAccount(false)
	admin := &cleanupHandlerAdminStub{account: account}
	handler := &OpenAIOAuthHandler{adminService: admin}

	status, _ := performOpenAISessionCleanupHandlerRequest(
		t,
		handler,
		http.MethodPut,
		"/api/v1/admin/openai/accounts/42/sessions/cleanup",
		`{"enabled":`,
	)

	require.Equal(t, http.StatusBadRequest, status)
	_, updateCalls := admin.counts()
	require.Zero(t, updateCalls)
}

func TestOpenAISessionCleanupHandlerRunNowReturnsStatusAndPreservesCurrent(t *testing.T) {
	account := openAICleanupHandlerAccount(true)
	admin := &cleanupHandlerAdminStub{account: account}
	repo := &cleanupHandlerSessionRepo{account: account}
	client := &cleanupHandlerSessionClient{list: &service.OpenAIAccountSessionList{
		CurrentKnown: true,
		Sessions: []service.OpenAIAccountSession{
			{ID: "current", Current: true, CanRevoke: true},
			{ID: "old", CanRevoke: true},
		},
	}}
	cleanup := service.NewOpenAISessionCleanupService(repo, client, nil)
	handler := &OpenAIOAuthHandler{adminService: admin, sessionCleanup: cleanup}

	status, envelope := performOpenAISessionCleanupHandlerRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/openai/accounts/42/sessions/cleanup/run",
		"",
	)

	require.Equal(t, http.StatusOK, status)
	var data map[string]string
	require.NoError(t, json.Unmarshal(envelope.Data, &data))
	require.Equal(t, "OpenAI session cleanup completed", data["message"])
	require.Equal(t, []string{"old"}, client.revokedIDs())
	state := repo.lastState()
	require.NotNil(t, state)
	decoded := service.DecodeOpenAISessionCleanupState(state[service.OpenAISessionCleanupStateExtraKey])
	require.NotNil(t, decoded)
	require.Equal(t, service.OpenAISessionCleanupStatusSuccess, decoded.Status)
	require.Equal(t, 1, decoded.RevokedCount)
	getCalls, _ := admin.counts()
	require.Equal(t, 1, getCalls)
}

func TestOpenAISessionCleanupHandlerRunNowPropagatesDisabledAndUpstreamErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		enabled    bool
		clientErr  error
		wantStatus int
		wantReason string
	}{
		{name: "disabled policy", enabled: false, wantStatus: http.StatusBadRequest, wantReason: "OPENAI_SESSION_CLEANUP_DISABLED"},
		{name: "upstream error", enabled: true, clientErr: errors.New("upstream unavailable"), wantStatus: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := openAICleanupHandlerAccount(test.enabled)
			admin := &cleanupHandlerAdminStub{account: account}
			repo := &cleanupHandlerSessionRepo{account: account}
			client := &cleanupHandlerSessionClient{
				list:    &service.OpenAIAccountSessionList{CurrentKnown: true, Sessions: []service.OpenAIAccountSession{{ID: "current", Current: true}}},
				listErr: test.clientErr,
			}
			handler := &OpenAIOAuthHandler{
				adminService:   admin,
				sessionCleanup: service.NewOpenAISessionCleanupService(repo, client, nil),
			}

			status, envelope := performOpenAISessionCleanupHandlerRequest(t, handler, http.MethodPost, "/api/v1/admin/openai/accounts/42/sessions/cleanup/run", "")
			require.Equal(t, test.wantStatus, status)
			require.Equal(t, test.wantReason, envelope.Reason)
		})
	}
}

func TestOpenAISessionCleanupHandlerRunNowMissingWorkerReturnsBadRequest(t *testing.T) {
	admin := &cleanupHandlerAdminStub{account: openAICleanupHandlerAccount(true)}
	handler := &OpenAIOAuthHandler{adminService: admin}

	status, envelope := performOpenAISessionCleanupHandlerRequest(
		t,
		handler,
		http.MethodPost,
		"/api/v1/admin/openai/accounts/42/sessions/cleanup/run",
		"",
	)

	require.Equal(t, http.StatusBadRequest, status)
	require.Contains(t, envelope.Message, "service is not enabled")
}
