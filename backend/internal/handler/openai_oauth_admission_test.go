//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAcquireOpenAIAccountSlotAppliesOAuthAdmissionCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &profitCountingConcurrencyCache{}
	gateway := &service.OpenAIGatewayService{}
	handler := &OpenAIGatewayHandler{
		gatewayService:    gateway,
		concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatClaude, 0),
	}
	account := &service.Account{
		ID:          991,
		Platform:    service.PlatformOpenAI,
		Type:        service.AccountTypeOAuth,
		Status:      service.StatusActive,
		Schedulable: true,
		Concurrency: 2,
		Extra:       map[string]any{},
	}
	gateway.BlockAccountScheduling(account, time.Now().Add(time.Minute), "test_shared_429")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	selection := &service.AccountSelectionResult{
		Account: account,
		WaitPlan: &service.AccountWaitPlan{
			AccountID:      account.ID,
			MaxConcurrency: account.Concurrency,
			MaxWaiting:     1,
			Timeout:        time.Second,
		},
	}
	streamStarted := false

	release, result := handler.acquireResponsesAccountSlot(c, nil, "", selection, false, &streamStarted, zap.NewNop())
	require.Equal(t, openAISlotAcquireFailed, result)
	require.Nil(t, release)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	retryAfterSeconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, retryAfterSeconds, 59)
	require.Equal(t, int64(1), cache.accountReleases.Load(), "local admission rejection must release the acquired account slot")
}
