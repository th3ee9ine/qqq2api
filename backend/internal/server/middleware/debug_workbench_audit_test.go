package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestDebugWorkbenchAuditOmitsBodyWithoutDroppingOperation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &auditCaptureRepository{}
	audit := service.NewAuditLogService(repository, nil)
	audit.Start()
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(ContextKeyUser), AuthSubject{UserID: 77}); c.Next() })
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
	original := `{"headers":{"X-Custom":"private-credential"},"body":{"input":"private-prompt"}}`
	router.POST("/api/v1/admin/accounts/:id/debug", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, original, string(body))
		c.Status(http.StatusOK)
	})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/admin/accounts/7/debug", bytes.NewBufferString(original))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	audit.Stop()
	require.Equal(t, http.StatusOK, recorder.Code)
	repository.mu.Lock()
	defer repository.mu.Unlock()
	require.Len(t, repository.logs, 1)
	require.Equal(t, "<credential-bearing body omitted>", repository.logs[0].RequestBody)
	require.Equal(t, http.StatusOK, repository.logs[0].StatusCode)
}
