//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/th3ee9ine/qqq2api/internal/service"
)

type openAIAccountSessionServiceStub struct {
	list      *service.OpenAIAccountSessionList
	listErr   error
	revokeErr error
	accountID int64
	sessionID string
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

func TestOpenAIListSessionsPropagatesServiceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &OpenAIOAuthHandler{sessionService: &openAIAccountSessionServiceStub{listErr: errors.New("upstream failed")}}
	router := gin.New()
	router.GET("/api/v1/admin/openai/accounts/:id/sessions", handler.ListSessions)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/admin/openai/accounts/42/sessions", nil))

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
}
