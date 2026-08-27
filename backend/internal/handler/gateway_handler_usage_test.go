package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestUsageUnrestrictedOmitsUserWalletAndSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/usage", nil)

	expiresAt := time.Now().Add(24 * time.Hour)

	handler := &GatewayHandler{}
	handler.usageUnrestricted(
		c,
		&service.APIKey{
			Status:    service.StatusAPIKeyActive,
			ExpiresAt: &expiresAt,
			Group:     &service.Group{Name: "Global group"},
		},
		gin.H{"today": gin.H{"requests": 3}},
		[]gin.H{{"date": "2026-08-23"}},
		[]gin.H{{"model": "test-model"}},
	)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "unrestricted", response["mode"])
	require.Equal(t, "Global group", response["group_name"])
	require.Contains(t, response, "usage")
	require.Contains(t, response, "daily_usage")
	require.Contains(t, response, "model_stats")
	require.NotContains(t, response, "balance")
	require.NotContains(t, response, "remaining")
	require.NotContains(t, response, "subscription")
	require.NotContains(t, response, "planName")
}
