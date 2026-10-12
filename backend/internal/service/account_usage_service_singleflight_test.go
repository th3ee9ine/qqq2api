package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetOpenAIUsageConcurrentCallersShareProbeAndKeepAccountImmutable(t *testing.T) {
	account := &Account{
		ID: 771, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Credentials: map[string]any{"chatgpt_account_id": "quota-concurrent-account"},
		Extra:       map[string]any{"unrelated": "preserved"},
	}
	repo := &sparkShadowUsageTestRepo{
		accounts:      map[int64]*Account{account.ID: account},
		updateExtraCh: make(chan sparkShadowExtraUpdate, 64),
	}
	tokenProvider := NewOpenAITokenProvider(repo, &stubQuotaTokenCache{tokens: map[string]string{
		OpenAITokenCacheKey(account): "fake-access-token",
	}}, nil)
	var requests atomic.Int32
	var enteredOnce sync.Once
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		enteredOnce.Do(func() { close(entered) })
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(OpenAIQuotaUsage{RateLimit: &OpenAIRateLimit{
			PrimaryWindow: &OpenAIRateLimitWindow{
				UsedPercent: 42.5, ResetAfterSeconds: 3600, LimitWindowSeconds: 18000,
			},
			SecondaryWindow: &OpenAIRateLimitWindow{
				UsedPercent: 10, ResetAfterSeconds: 86400, LimitWindowSeconds: 604800,
			},
		}})
	}))
	defer srv.Close()
	svc := &AccountUsageService{
		accountRepo: repo,
		cache:       NewUsageCache(),
		openAIQuotaService: NewOpenAIQuotaService(repo, nil, tokenProvider,
			newQuotaRedirectingFactory(srv)),
	}

	const callers = 32
	start := make(chan struct{})
	results := make([]*UsageInfo, callers)
	errors := make([]error, callers)
	var ready, done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	for i := range callers {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			results[i], errors[i] = svc.getOpenAIUsage(context.Background(), account, true)
		}()
	}
	ready.Wait()
	close(start)
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		unblock()
		t.Fatal("timed out waiting for quota probe")
	}
	// Hold the network response long enough for all released goroutines to
	// join the flight. The test exercises the actual shared account/map with
	// the race detector rather than a test-only production synchronization hook.
	time.Sleep(50 * time.Millisecond)
	unblock()
	done.Wait()

	require.Equal(t, int32(1), requests.Load(), "same-account probes must be collapsed")
	for i, result := range results {
		require.NoError(t, errors[i])
		require.NotNil(t, result)
		require.NotNil(t, result.FiveHour, "follower %d must receive the shared result", i)
		require.NotNil(t, result.SevenDay)
		require.Equal(t, 42.5, result.FiveHour.Utilization)
		require.Equal(t, 10.0, result.SevenDay.Utilization)
	}
	require.Equal(t, map[string]any{"unrelated": "preserved"}, account.Extra,
		"a probe must not mutate a shared account snapshot")
	results[0].FiveHour.Utilization = 99
	require.Equal(t, 42.5, results[1].FiveHour.Utilization,
		"callers must own independent UsageInfo windows")
	select {
	case <-repo.updateExtraCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for one snapshot write")
	}
	require.Empty(t, repo.updateExtraCh, "only the singleflight leader should persist")
}

func TestCodexSnapshotUpdatedAtPreservesSubsecondOrdering(t *testing.T) {
	first := time.Date(2026, 10, 13, 10, 0, 0, 123456789, time.UTC)
	second := first.Add(time.Nanosecond)
	used := 42.0
	for _, now := range []time.Time{first, second} {
		headerUpdates := buildCodexUsageExtraUpdates(&OpenAICodexUsageSnapshot{
			PrimaryUsedPercent: &used,
			UpdatedAt:          now.Format(time.RFC3339Nano),
		}, now)
		quotaUpdates := buildCodexPrimaryWindowExtraUpdates(&OpenAIQuotaUsage{RateLimit: &OpenAIRateLimit{
			PrimaryWindow: &OpenAIRateLimitWindow{UsedPercent: used, LimitWindowSeconds: 18000},
		}}, now)
		for _, updates := range []map[string]any{headerUpdates, quotaUpdates} {
			parsed, ok := parseCodexSnapshotUpdatedAt(updates)
			require.True(t, ok)
			require.True(t, parsed.Equal(now), "snapshot ordering must retain nanoseconds")
		}
	}
}
