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

type explicitAccountOwnershipAdminService struct {
	*stubAdminService
	accountsByID map[int64]*service.Account
	lookupCalls  int
}

func (s *explicitAccountOwnershipAdminService) GetAccountsByIDs(_ context.Context, ids []int64) ([]*service.Account, error) {
	s.lookupCalls++
	out := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		if account, ok := s.accountsByID[id]; ok {
			out = append(out, account)
		}
	}
	return out, nil
}

func scopedAccountRequest(ownerID int64) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/admin/accounts/today-stats/batch", nil)
	return req.WithContext(context.WithValue(req.Context(), ctxkey.AccountAdminID, ownerID))
}

func TestRequireExplicitAccountOwnershipRejectsMixedIDsAtomically(t *testing.T) {
	ownerID := int64(41)
	owned := &service.Account{ID: 11, AccountAdminID: &ownerID}
	svc := &explicitAccountOwnershipAdminService{
		stubAdminService: newStubAdminService(),
		accountsByID:     map[int64]*service.Account{owned.ID: owned},
	}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/admin/accounts/today-stats/batch", func(c *gin.Context) {
		if !h.requireExplicitAccountOwnership(c, []int64{owned.ID, 99}) {
			return
		}
		t.Fatalf("handler continued after a foreign ID")
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, scopedAccountRequest(ownerID))

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ACCOUNT_NOT_FOUND")
	require.Equal(t, 1, svc.lookupCalls)
}

func TestRequireExplicitAccountOwnershipAcceptsOwnedIDsAndNamespacesStatsCache(t *testing.T) {
	ownerID := int64(41)
	owned := &service.Account{ID: 11, AccountAdminID: &ownerID}
	svc := &explicitAccountOwnershipAdminService{
		stubAdminService: newStubAdminService(),
		accountsByID:     map[int64]*service.Account{owned.ID: owned},
	}
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	continued := false
	router.POST("/admin/accounts/today-stats/batch", func(c *gin.Context) {
		if h.requireExplicitAccountOwnership(c, []int64{owned.ID}) {
			continued = true
			c.Status(http.StatusNoContent)
		}
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, scopedAccountRequest(ownerID))
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.True(t, continued)

	ownerContext := context.WithValue(context.Background(), ctxkey.AccountAdminID, ownerID)
	otherContext := context.WithValue(context.Background(), ctxkey.AccountAdminID, int64(42))
	ownerKey := buildAccountTodayStatsBatchCacheKeyForContext(ownerContext, []int64{owned.ID})
	otherKey := buildAccountTodayStatsBatchCacheKeyForContext(otherContext, []int64{owned.ID})
	globalKey := buildAccountTodayStatsBatchCacheKey([]int64{owned.ID})
	require.NotEqual(t, ownerKey, otherKey)
	require.NotEqual(t, ownerKey, globalKey)
	require.Contains(t, ownerKey, "owner:41")
}
