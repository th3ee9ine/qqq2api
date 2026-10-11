package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

const openAIOAuthTTFTHistoryPrefix = "openai_oauth_ttft_history:account:"
const openAIOAuthTTFTHistoryTimeLayout = "2006-01-02T15:04:05.000000000Z"

var openAIOAuthTTFTHistoryAppendScript = redis.NewScript(`
redis.call('ZADD', KEYS[1], 0, ARGV[1])
redis.call('ZREMRANGEBYRANK', KEYS[1], 0, -tonumber(ARGV[2])-1)
return 1
`)

func openAIOAuthTTFTHistoryKey(accountID int64) string {
	return fmt.Sprintf("%s%d", openAIOAuthTTFTHistoryPrefix, accountID)
}

func (c *gatewayCache) AppendOpenAIOAuthTTFTObservation(ctx context.Context, accountID int64, observation service.OpenAIOAuthTTFTObservation) error {
	if c == nil || c.rdb == nil {
		return errors.New("gateway cache unavailable")
	}
	if accountID <= 0 {
		return errors.New("invalid OpenAI OAuth TTFT account id")
	}
	if observation.FirstTokenMs != nil && *observation.FirstTokenMs < 0 {
		observation.FirstTokenMs = nil
	}
	payload, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return err
	}
	// All scores are equal so Redis orders by the fixed-width UTC nanosecond
	// prefix without floating-point timestamp precision loss. A unique member
	// is generated once per invocation, outside Run's automatic Redis retries.
	// Replaying a trimmed old member cannot evict any newer observation.
	member := observation.ObservedAt.UTC().Format(openAIOAuthTTFTHistoryTimeLayout) + "\n" + id.String() + "\n" + string(payload)
	return openAIOAuthTTFTHistoryAppendScript.Run(ctx, c.rdb, []string{openAIOAuthTTFTHistoryKey(accountID)}, member, service.OpenAIOAuthTTFTHistoryLimit).Err()
}

func (c *gatewayCache) GetOpenAIOAuthTTFTObservations(ctx context.Context, accountIDs []int64) (map[int64][]service.OpenAIOAuthTTFTObservation, error) {
	result := make(map[int64][]service.OpenAIOAuthTTFTObservation, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}
	if c == nil || c.rdb == nil {
		return nil, errors.New("gateway cache unavailable")
	}
	commands := make(map[int64]*redis.StringSliceCmd, len(accountIDs))
	_, err := c.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, id := range accountIDs {
			if id <= 0 {
				continue
			}
			if _, exists := commands[id]; !exists {
				commands[id] = pipe.ZRevRange(ctx, openAIOAuthTTFTHistoryKey(id), 0, service.OpenAIOAuthTTFTHistoryLimit-1)
			}
		}
		return nil
	})
	readSucceeded := false
	for id, command := range commands {
		entries, readErr := command.Result()
		if readErr != nil {
			slog.Warn("openai_oauth_ttft_history_account_read_failed", "account_id", id, "error", readErr)
			continue
		}
		readSucceeded = true
		observations := make([]service.OpenAIOAuthTTFTObservation, 0, len(entries))
		for _, entry := range entries {
			parts := strings.SplitN(entry, "\n", 3)
			if len(parts) != 3 || len(parts[0]) != len(openAIOAuthTTFTHistoryTimeLayout) {
				slog.Warn("openai_oauth_ttft_history_entry_invalid_member", "account_id", id)
				continue
			}
			observedAt, timeErr := time.Parse(openAIOAuthTTFTHistoryTimeLayout, parts[0])
			_, idErr := uuid.Parse(parts[1])
			if timeErr != nil || idErr != nil {
				slog.Warn("openai_oauth_ttft_history_entry_invalid_member", "account_id", id)
				continue
			}
			var observation service.OpenAIOAuthTTFTObservation
			if err := json.Unmarshal([]byte(parts[2]), &observation); err != nil {
				slog.Warn("openai_oauth_ttft_history_entry_decode_failed", "account_id", id, "error", err)
				continue
			}
			if observation.ObservedAt.IsZero() || !observation.ObservedAt.Equal(observedAt) {
				slog.Warn("openai_oauth_ttft_history_entry_invalid_timestamp", "account_id", id)
				continue
			}
			if observation.FirstTokenMs != nil && *observation.FirstTokenMs < 0 {
				observation.FirstTokenMs = nil
			}
			observations = append(observations, observation)
		}
		result[id] = observations
	}
	if err != nil && !readSucceeded {
		return nil, err
	}
	return result, nil
}

var _ service.OpenAIOAuthTTFTHistoryCache = (*gatewayCache)(nil)
