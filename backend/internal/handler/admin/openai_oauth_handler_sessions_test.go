//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

type openAIAccountSessionServiceStub struct {
	list              *service.OpenAIAccountSessionList
	listErr           error
	revokeErr         error
	revokeSessionsErr error
	trustErr          error
	batchResult       *service.OpenAIAccountSessionBatchRevokeResult
	accountID         int64
	sessionID         string
	sessionIDs        []string
	trustedSessionID  string
}

func (s *openAIAccountSessionServiceStub) ListSessions(_ context.Context, accountID int64) (*service.OpenAIAccountSessionList, error) {
	s.accountID = accountID
	return s.list, s.listErr
}

func (s *openAIAccountSessionServiceStub) RevokeSession(_ context.Context, accountID int64, sessionID string) error {
	s.accountID = accountID
	s.sessionID = sessionID
	return s.revokeErr
}

func (s *openAIAccountSessionServiceStub) RevokeSessions(_ context.Context, accountID int64, sessionIDs []string) (*service.OpenAIAccountSessionBatchRevokeResult, error) {
	s.accountID = accountID
	s.sessionIDs = append([]string(nil), sessionIDs...)
	return s.batchResult, s.revokeSessionsErr
}

func (s *openAIAccountSessionServiceStub) TrustSession(_ context.Context, accountID int64, sessionID string) error {
	s.accountID = accountID
	s.trustedSessionID = sessionID
	return s.trustErr
}

func TestOpenAIListSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &openAIAccountSessionServiceStub{list: &service.OpenAIAccountSessionList{
		Sessions: []service.OpenAIAccountSession{{ID: "sess-1", DeviceName: "Mac"}},
	}}
	handler := &OpenAIOAuthHandler{sessionService: stub}
	router := gin.New()
	router.GET("/api/v1/admin/openai/accounts/:id/sessions", handler.ListSessions)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/openai/accounts/42/sessions", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), stub.accountID)
	var envelope struct {
		Data service.OpenAIAccountSessionList `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Sessions, 1)
	require.Equal(t, "sess-1", envelope.Data.Sessions[0].ID)
}

func TestOpenAIRevokeSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &openAIAccountSessionServiceStub{}
	handler := &OpenAIOAuthHandler{sessionService: stub}
	router := gin.New()
	router.DELETE("/api/v1/admin/openai/accounts/:id/sessions/:session_id", handler.RevokeSession)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/v1/admin/openai/accounts/42/sessions/sess-1", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), stub.accountID)
	require.Equal(t, "sess-1", stub.sessionID)
}

func TestOpenAIRevokeSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &openAIAccountSessionServiceStub{batchResult: &service.OpenAIAccountSessionBatchRevokeResult{
		RequestedCount:    2,
		SuccessCount:      2,
		RevokedSessionIDs: []string{"sess-1", "sess-2"},
		Failures:          []service.OpenAIAccountSessionRevokeFailure{},
	}}
	handler := &OpenAIOAuthHandler{sessionService: stub}
	router := gin.New()
	router.POST("/api/v1/admin/openai/accounts/:id/sessions/revoke", handler.RevokeSessions)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/openai/accounts/42/sessions/revoke",
		strings.NewReader(`{"session_ids":["sess-1","sess-2"]}`),
	)
	request.Header.Set("content-type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), stub.accountID)
	require.Equal(t, []string{"sess-1", "sess-2"}, stub.sessionIDs)
	var envelope struct {
		Data service.OpenAIAccountSessionBatchRevokeResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, 2, envelope.Data.SuccessCount)
}

func TestOpenAITrustSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &openAIAccountSessionServiceStub{}
	handler := &OpenAIOAuthHandler{sessionService: stub}
	router := gin.New()
	router.POST("/api/v1/admin/openai/accounts/:id/sessions/trust", handler.TrustSession)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/openai/accounts/42/sessions/trust",
		strings.NewReader(`{"session_id":"sess-current"}`),
	)
	request.Header.Set("content-type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), stub.accountID)
	require.Equal(t, "sess-current", stub.trustedSessionID)
	var envelope struct {
		Data map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Equal(t, "Device session marked as trusted", envelope.Data["message"])
}

func TestOpenAITrustSessionAcceptsChunkedEmptyBodyForPathSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &openAIAccountSessionServiceStub{}
	handler := &OpenAIOAuthHandler{sessionService: stub}
	router := gin.New()
	router.POST("/api/v1/admin/openai/accounts/:id/sessions/:session_id/trust", handler.TrustSession)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/openai/accounts/42/sessions/sess-path/trust",
		strings.NewReader(""),
	)
	// Axios/fetch may use chunked transfer encoding, which reports a negative
	// ContentLength even when the request body is empty.
	request.ContentLength = -1
	request.Header.Set("content-type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, int64(42), stub.accountID)
	require.Equal(t, "sess-path", stub.trustedSessionID)
}

func TestOpenAIListSessionsPropagatesServiceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OpenAIOAuthHandler{sessionService: &openAIAccountSessionServiceStub{listErr: errors.New("upstream failed")}}
	router := gin.New()
	router.GET("/api/v1/admin/openai/accounts/:id/sessions", handler.ListSessions)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/openai/accounts/42/sessions", nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
}
