package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

const codexTurnStateCollectionLeaseKeyPrefix = "openai:codex_turn_state:collection_lease:account:"

var refreshCodexTurnStateCollectionLeaseScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == false or current ~= ARGV[1] then
  return 0
end
redis.call('PEXPIRE', KEYS[1], ARGV[2])
return 1
`)

var releaseCodexTurnStateCollectionLeaseScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if current == false or current ~= ARGV[1] then
  return 0
end
redis.call('DEL', KEYS[1])
return 1
`)

func codexTurnStateCollectionLeaseKey(accountID int64) string {
	return fmt.Sprintf("%s%d", codexTurnStateCollectionLeaseKeyPrefix, accountID)
}

func validateCodexTurnStateCollectionLease(accountID int64, owner string, ttl time.Duration) error {
	if accountID <= 0 || strings.TrimSpace(owner) == "" || ttl <= 0 {
		return errors.New("invalid Codex Turn State collection lease")
	}
	return nil
}

func (c *gatewayCache) TryAcquireCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if err := validateCodexTurnStateCollectionLease(accountID, owner, ttl); err != nil {
		return false, err
	}
	return c.rdb.SetNX(ctx, codexTurnStateCollectionLeaseKey(accountID), owner, ttl).Result()
}

func (c *gatewayCache) RefreshCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if err := validateCodexTurnStateCollectionLease(accountID, owner, ttl); err != nil {
		return false, err
	}
	result, err := refreshCodexTurnStateCollectionLeaseScript.Run(
		ctx,
		c.rdb,
		[]string{codexTurnStateCollectionLeaseKey(accountID)},
		owner,
		ttl.Milliseconds(),
	).Int()
	return result == 1, err
}

func (c *gatewayCache) ReleaseCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("gateway cache unavailable")
	}
	if accountID <= 0 || strings.TrimSpace(owner) == "" {
		return false, errors.New("invalid Codex Turn State collection lease")
	}
	result, err := releaseCodexTurnStateCollectionLeaseScript.Run(
		ctx,
		c.rdb,
		[]string{codexTurnStateCollectionLeaseKey(accountID)},
		owner,
	).Int()
	return result == 1, err
}

var _ service.CodexTurnStateCollectionLeaseStore = (*gatewayCache)(nil)
