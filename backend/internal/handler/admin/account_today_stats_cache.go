package admin

import (
	"context"
	"strconv"
	"strings"
	"time"
)

var accountTodayStatsBatchCache = newSnapshotCache(30 * time.Second)

func buildAccountTodayStatsBatchCacheKey(accountIDs []int64) string {
	return buildAccountTodayStatsBatchCacheKeyWithScope("", accountIDs)
}

func buildAccountTodayStatsBatchCacheKeyForContext(ctx context.Context, accountIDs []int64) string {
	return buildAccountTodayStatsBatchCacheKeyWithScope(accountOwnershipCacheScope(ctx), accountIDs)
}

func buildAccountTodayStatsBatchCacheKeyWithScope(scope string, accountIDs []int64) string {
	if len(accountIDs) == 0 {
		return "accounts_today_stats_empty"
	}
	var b strings.Builder
	b.Grow(len(accountIDs) * 6)
	_, _ = b.WriteString("accounts_today_stats:")
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
