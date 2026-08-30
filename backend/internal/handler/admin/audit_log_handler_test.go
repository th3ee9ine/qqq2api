package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type auditClearRepositoryStub struct {
	count          int64
	truncateCalled bool
	trace          *service.AuditLog
}

func (r *auditClearRepositoryStub) BatchInsert(context.Context, []*service.AuditLog) (int64, error) {
	return 0, nil
}

func (r *auditClearRepositoryStub) Insert(_ context.Context, log *service.AuditLog) error {
	r.trace = log
	return nil
}

func (r *auditClearRepositoryStub) List(context.Context, *service.AuditLogFilter) (*service.AuditLogList, error) {
	return &service.AuditLogList{}, nil
}

func (r *auditClearRepositoryStub) GetByID(context.Context, int64) (*service.AuditLog, error) {
	return nil, service.ErrAuditLogNotFound
}

func (r *auditClearRepositoryStub) Count(context.Context) (int64, error) {
	return r.count, nil
}

func (r *auditClearRepositoryStub) TruncateAll(context.Context) error {
	r.truncateCalled = true
	return nil
}

func (r *auditClearRepositoryStub) DeleteBefore(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func TestAuditLogHandlerClearDoesNotRequireTOTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &auditClearRepositoryStub{count: 7}
	auditService := service.NewAuditLogService(repo, nil)
	handler := NewAuditLogHandler(auditService, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set(string(middleware.ContextKeyUserRole), "admin")
		c.Set(middleware.ContextKeyAuthEmail, "admin@example.test")
		c.Set("auth_method", service.AuditAuthMethodJWT)
		c.Next()
	})
	router.POST("/api/v1/admin/audit-logs/clear", handler.Clear)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit-logs/clear", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.True(t, repo.truncateCalled)
	require.NotNil(t, repo.trace)
	require.Equal(t, service.AuditActionAuditLogClear, repo.trace.Action)
	require.Equal(t, int64(7), repo.trace.Extra["deleted_rows"])
	require.Contains(t, recorder.Body.String(), `"deleted":7`)
}

func TestAuditLogHandlerClearStillRequiresAnAuthenticatedSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &auditClearRepositoryStub{count: 2}
	auditService := service.NewAuditLogService(repo, nil)
	handler := NewAuditLogHandler(auditService, nil)

	router := gin.New()
	router.POST("/api/v1/admin/audit-logs/clear", handler.Clear)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit-logs/clear", nil))

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.False(t, repo.truncateCalled)
}

func TestAuditLogHandlerClearRejectsAdminAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &auditClearRepositoryStub{count: 3}
	auditService := service.NewAuditLogService(repo, nil)
	handler := NewAuditLogHandler(auditService, nil)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
		c.Set("auth_method", service.AuditAuthMethodAdminAPIKey)
		c.Next()
	})
	router.POST("/api/v1/admin/audit-logs/clear", handler.Clear)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/admin/audit-logs/clear", nil))

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "STEP_UP_ADMIN_API_KEY_FORBIDDEN")
	require.False(t, repo.truncateCalled)
}
