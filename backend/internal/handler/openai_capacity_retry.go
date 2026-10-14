package handler

import (
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// openAICapacityRetryBudget belongs to one HTTP request or one WebSocket turn.
// Once overload is observed, all later retry decisions share the same budget,
// even if the next account returns a different error. Successful WS turns reset
// it; changing accounts or retrying the current turn never does.
type openAICapacityRetryBudget struct {
	maxAttempts int
	retries     int
	active      bool
}

func newOpenAICapacityRetryBudget(cfg *config.Config) openAICapacityRetryBudget {
	maxAttempts := 3
	if cfg != nil && cfg.Gateway.OpenAICapacityRetryMaxAttempts > 0 {
		maxAttempts = cfg.Gateway.OpenAICapacityRetryMaxAttempts
	}
	return openAICapacityRetryBudget{maxAttempts: maxAttempts}
}

func (b *openAICapacityRetryBudget) allowRetry(err *service.UpstreamFailoverError) bool {
	if err == nil || !err.ShouldRetryNextAccount() {
		return false
	}
	if err.IsOpenAICapacityShed() {
		b.active = true
	}
	if !b.active {
		return true
	}
	if b.retries >= b.maxAttempts-1 {
		return false
	}
	b.retries++
	return true
}

func (b *openAICapacityRetryBudget) reset() {
	b.retries = 0
	b.active = false
}
