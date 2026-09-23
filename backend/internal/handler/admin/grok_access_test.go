package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type grokScopedAdminService struct {
	service.AdminService
	ownerID   int64
	accountID int64
}

func (s *grokScopedAdminService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	s.ownerID, _ = ctxkey.AccountAdminIDFromContext(ctx)
	s.accountID = id
	return nil, service.ErrAccountNotFound
}

// An inaccessible account must be rejected before the quota service is reached,
// including when its shared cache already contains a result for that account.
func TestGrokQuotaEndpointsCheckScopedAccountAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			adminSvc := &grokScopedAdminService{}
			h := NewGrokOAuthHandler(nil, adminSvc, nil, nil)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.AccountAdminID, int64(7)))
				c.Next()
			})
			router.GET("/accounts/:id/quota", h.QueryQuota)
			router.POST("/accounts/:id/quota", h.ResetQuota)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(method, "/accounts/42/quota", nil))
			require.Equal(t, http.StatusNotFound, rec.Code)
			require.Equal(t, int64(7), adminSvc.ownerID)
			require.Equal(t, int64(42), adminSvc.accountID)
		})
	}
}
