package service

import (
	"context"
	"time"
)

// OpenAIOAuthTTFTStreakCache is an optional distributed counter used by the
// first-token latency guard. It is intentionally kept out of GatewayCache so
// existing cache test doubles and alternative cache implementations do not
// need to implement an unrelated operation.
type OpenAIOAuthTTFTStreakCache interface {
	IncrementOpenAIOAuthTTFTStreak(ctx context.Context, accountID int64, ttl time.Duration) (int64, error)
	ResetOpenAIOAuthTTFTStreak(ctx context.Context, accountID int64) error
}
