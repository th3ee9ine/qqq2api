package admin

import (
	"context"
	"strconv"
	"strings"
	"time"
)

var accountLifetimeStatsBatchCache = newSnapshotCache(30 * time.Second)

func buildAccountLifetimeStatsBatchCacheKey(accountIDs []int64) string {
	return buildAccountLifetimeStatsBatchCacheKeyWithScope("", accountIDs)
}

func buildAccountLifetimeStatsBatchCacheKeyForContext(ctx context.Context, accountIDs []int64) string {
	return buildAccountLifetimeStatsBatchCacheKeyWithScope(accountOwnershipCacheScope(ctx), accountIDs)
}

func buildAccountLifetimeStatsBatchCacheKeyWithScope(scope string, accountIDs []int64) string {
	if len(accountIDs) == 0 {
		return "accounts_lifetime_stats_empty"
	}
	var b strings.Builder
	b.Grow(len(accountIDs) * 6)
	_, _ = b.WriteString("accounts_lifetime_stats:")
	if scope != "" {
		_, _ = b.WriteString(scope)
		_ = b.WriteByte(':')
	}
	for i, id := range accountIDs {
		if i > 0 {
			_ = b.WriteByte(',')
		}
		_, _ = b.WriteString(strconv.FormatInt(id, 10))
	}
	return b.String()
}
