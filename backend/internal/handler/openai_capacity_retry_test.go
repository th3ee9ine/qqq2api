package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func capacityRetryTestError() *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		StatusCode:             http.StatusServiceUnavailable,
		RequestScopedTransient: true,
		ResponseBody:           []byte(`{"error":{"code":"server_is_overloaded"}}`),
	}
}

func TestOpenAICapacityRetryBudget(t *testing.T) {
	t.Run("unrelated errors retain their own retry policy", func(t *testing.T) {
		budget := newOpenAICapacityRetryBudget(nil)
		for i := 0; i < 20; i++ {
			require.True(t, budget.allowRetry(&service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
		}
		require.False(t, budget.allowRetry(nil))
		require.False(t, budget.allowRetry(&service.UpstreamFailoverError{NextAccountAction: service.NextAccountStop}))
	})
	t.Run("overload and later errors share the same retry allowance", func(t *testing.T) {
		budget := newOpenAICapacityRetryBudget(nil)
		require.True(t, budget.allowRetry(capacityRetryTestError()))
		require.True(t, budget.allowRetry(&service.UpstreamFailoverError{StatusCode: http.StatusBadGateway}))
		for i := 0; i < 10; i++ {
			require.False(t, budget.allowRetry(capacityRetryTestError()), "another account must not restart the allowance")
		}
	})
	t.Run("successful websocket turn starts a fresh budget", func(t *testing.T) {
		budget := newOpenAICapacityRetryBudget(nil)
		for turn := 0; turn < 3; turn++ {
			require.True(t, budget.allowRetry(capacityRetryTestError()))
			require.True(t, budget.allowRetry(capacityRetryTestError()))
			require.False(t, budget.allowRetry(capacityRetryTestError()))
			budget.reset()
		}
	})
	for _, maxAttempts := range []int{1, 2, 5, 10} {
		cfg := &config.Config{}
		cfg.Gateway.OpenAICapacityRetryMaxAttempts = maxAttempts
		budget := newOpenAICapacityRetryBudget(cfg)
		for attempt := 1; attempt < maxAttempts; attempt++ {
			require.True(t, budget.allowRetry(capacityRetryTestError()))
		}
		require.False(t, budget.allowRetry(capacityRetryTestError()))
	}
}
