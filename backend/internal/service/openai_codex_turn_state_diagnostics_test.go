package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type turnStateDiagnosticsProberFunc func(context.Context, string) (*ProxyExitInfo, int64, error)

func (f turnStateDiagnosticsProberFunc) ProbeProxy(ctx context.Context, proxyURL string) (*ProxyExitInfo, int64, error) {
	return f(ctx, proxyURL)
}

func TestRecordCodexTurnStateProxySuccessIncludesDirectEgress(t *testing.T) {
	var observedRoute string
	svc := &OpenAIGatewayService{}
	svc.SetProxyExitInfoProber(turnStateDiagnosticsProberFunc(func(_ context.Context, proxyURL string) (*ProxyExitInfo, int64, error) {
		observedRoute = proxyURL
		return &ProxyExitInfo{
			IP:          "198.51.100.44",
			Region:      "East China",
			Country:     "China",
			CountryCode: "cn",
		}, 1, nil
	}))

	// An empty route means the probe used direct egress. It must still appear in
	// the successful exit-IP diagnostics just like a dedicated proxy route.
	svc.recordCodexTurnStateProxySuccess(context.Background(), "")

	require.Empty(t, observedRoute)
	ips, regions, _ := svc.codexTurnStateDiagnostics()
	require.Len(t, ips, 1)
	require.Equal(t, "198.51.100.44", ips[0].IP)
	require.Equal(t, "CN", ips[0].CountryCode)
	require.Len(t, regions, 1)
	require.Equal(t, uint64(1), regions[0].Successes)
}

func TestCodexTurnStateDiagnosticsCountsObservedExitsWithoutAttributingSkippedProbes(t *testing.T) {
	const route = "http://rotating.example:8080"
	var calls atomic.Int64
	svc := &OpenAIGatewayService{}
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{route})
	svc.SetProxyExitInfoProber(turnStateDiagnosticsProberFunc(func(_ context.Context, _ string) (*ProxyExitInfo, int64, error) {
		ip := "198.51.100.1"
		if calls.Add(1) > 1 {
			ip = "198.51.100.2"
		}
		return &ProxyExitInfo{IP: ip, Country: "Example", CountryCode: "EX"}, 1, nil
	}))
	generation := svc.codexTurnStateDiagnosticsGeneration()
	for index := 0; index < 20; index++ {
		svc.recordCodexTurnStateProxySuccessAsync(route, generation)
	}
	require.Eventually(t, func() bool {
		ips, _, _ := svc.codexTurnStateDiagnostics()
		return len(ips) == 1
	}, time.Second, time.Millisecond)
	ips, regions, _ := svc.codexTurnStateDiagnostics()
	require.Equal(t, int64(1), calls.Load())
	require.Equal(t, uint64(1), ips[0].Successes, "unobserved successes must not be assigned to a cached exit IP")
	require.Equal(t, uint64(1), regions[0].Successes)

	// A later connection through the same rotating route can expose a different
	// IP. Retain one observation for each concrete result, without estimating how
	// many state probes used either exit during the cooldown.
	svc.codexTurnStateProxyStatsMu.Lock()
	svc.codexTurnStateProxyProbeLast[route] = time.Now().Add(-openAICodexTurnStateProxyProbeCooldown)
	svc.codexTurnStateProxyStatsMu.Unlock()
	svc.recordCodexTurnStateProxySuccessAsync(route, generation)
	require.Eventually(t, func() bool {
		ips, _, _ := svc.codexTurnStateDiagnostics()
		return len(ips) == 2
	}, time.Second, time.Millisecond)
	ips, regions, _ = svc.codexTurnStateDiagnostics()
	require.Equal(t, int64(2), calls.Load())
	require.Equal(t, uint64(1), ips[0].Successes)
	require.Equal(t, uint64(1), ips[1].Successes)
	require.Equal(t, uint64(2), regions[0].Successes)
}

func TestCodexTurnStateDiagnosticsPoolChangesRetireOnlyPoolObservations(t *testing.T) {
	const route = "http://first.example:8080"
	svc := &OpenAIGatewayService{}
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{route})
	svc.SetProxyExitInfoProber(turnStateDiagnosticsProberFunc(func(context.Context, string) (*ProxyExitInfo, int64, error) {
		return &ProxyExitInfo{IP: "198.51.100.3", Region: "Example"}, 1, nil
	}))
	svc.recordCodexTurnStateProxySuccess(context.Background(), route)
	svc.recordCodexTurnStateCandidateReason("published")
	svc.codexTurnStateProxyStatsMu.Lock()
	svc.codexTurnStateProxyProbeLast = map[string]time.Time{route: time.Now()}
	svc.codexTurnStateProxyStatsMu.Unlock()
	generation := svc.codexTurnStateDiagnosticsGeneration()

	// Editing only the switches, or resaving the same normalized pool, retains
	// its observations and does not invalidate its asynchronous workers.
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(false, true, []string{"HTTP://first.example:8080"})
	require.Equal(t, generation, svc.codexTurnStateDiagnosticsGeneration())
	ips, regions, candidates := svc.codexTurnStateDiagnostics()
	require.Len(t, ips, 1)
	require.Len(t, regions, 1)
	require.Len(t, candidates, 1)

	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{"socks5://second.example:1080"})
	require.NotEqual(t, generation, svc.codexTurnStateDiagnosticsGeneration())
	ips, regions, candidates = svc.codexTurnStateDiagnostics()
	require.Empty(t, ips)
	require.Empty(t, regions)
	require.Len(t, candidates, 1, "candidate decisions are not egress pool statistics")
	svc.codexTurnStateProxyStatsMu.Lock()
	require.Empty(t, svc.codexTurnStateProxyProbeLast)
	svc.codexTurnStateProxyStatsMu.Unlock()
}

func TestCodexTurnStateDiagnosticsRejectsRetiredPoolAsyncWritesAndCompletedProbes(t *testing.T) {
	const route = "http://first.example:8080"
	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseOld := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseOld()
	svc := &OpenAIGatewayService{}
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{route})
	svc.SetProxyExitInfoProber(turnStateDiagnosticsProberFunc(func(ctx context.Context, _ string) (*ProxyExitInfo, int64, error) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			}
			return &ProxyExitInfo{IP: "198.51.100.10", Region: "Old"}, 1, nil
		}
		return &ProxyExitInfo{IP: "198.51.100.11", Region: "Current"}, 1, nil
	}))
	oldGeneration := svc.codexTurnStateDiagnosticsGeneration()
	svc.recordCodexTurnStateProxySuccessAsync(route, oldGeneration)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("old pool diagnostic did not start")
	}
	// Returning to the original URLs must not make the original worker current.
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{"http://second.example:8080"})
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{route})
	svc.recordCodexTurnStateProxySuccessAsync(route, oldGeneration)
	require.Equal(t, int64(1), calls.Load(), "a state probe completed under a retired pool must not start a lookup")
	svc.recordCodexTurnStateProxySuccessAsync(route, svc.codexTurnStateDiagnosticsGeneration())
	require.Eventually(t, func() bool {
		ips, _, _ := svc.codexTurnStateDiagnostics()
		return len(ips) == 1 && ips[0].IP == "198.51.100.11"
	}, time.Second, time.Millisecond)
	releaseOld()
	require.Eventually(t, func() bool {
		return len(svc.codexTurnStateProxyProbeSem) == 0
	}, time.Second, time.Millisecond)
	ips, regions, _ := svc.codexTurnStateDiagnostics()
	require.Len(t, ips, 1)
	require.Equal(t, "198.51.100.11", ips[0].IP)
	require.Len(t, regions, 1)
	require.Equal(t, "Current", regions[0].Region)
	require.Equal(t, int64(2), calls.Load())
}
