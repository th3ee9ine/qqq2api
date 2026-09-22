package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
)

// codexTurnStateRouteBudgetUpstream makes the first route consume its child
// deadline and returns a valid probe response from the second route. It keeps
// this regression test at the HTTPUpstream boundary, where the route-specific
// context is observable without starting a real proxy listener.
type codexTurnStateRouteBudgetUpstream struct {
	mu          sync.Mutex
	calls       []string
	firstDone   chan struct{}
	firstCalled sync.Once
	state       string
}

type codexTurnStateLargePoolBudgetUpstream struct {
	mu      sync.Mutex
	calls   []string
	budgets []time.Duration
}

func (u *codexTurnStateLargePoolBudgetUpstream) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	budget := time.Duration(0)
	if deadline, ok := req.Context().Deadline(); ok {
		budget = time.Until(deadline)
	}
	u.mu.Lock()
	u.calls = append(u.calls, proxyURL)
	u.budgets = append(u.budgets, budget)
	u.mu.Unlock()
	return nil, errors.New("synthetic proxy failure")
}

func (u *codexTurnStateLargePoolBudgetUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *codexTurnStateLargePoolBudgetUpstream) snapshot() ([]string, []time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...), append([]time.Duration(nil), u.budgets...)
}

func (u *codexTurnStateRouteBudgetUpstream) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.calls = append(u.calls, proxyURL)
	u.mu.Unlock()
	if proxyURL == "http://slow.example:8080" {
		u.firstCalled.Do(func() { close(u.firstDone) })
		<-req.Context().Done()
		return nil, req.Context().Err()
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{u.state},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"model":"gpt-5.5"}}`,
			``,
			`data: {"type":"response.completed","response":{"model":"gpt-5.5"}}`,
			``,
		}, "\n"))),
	}, nil
}

func (u *codexTurnStateRouteBudgetUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *codexTurnStateRouteBudgetUpstream) routes() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...)
}

func TestProbeCodexTurnStateContinuesAfterRouteBudgetExpires(t *testing.T) {
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 191)
	upstream := &codexTurnStateRouteBudgetUpstream{
		firstDone: make(chan struct{}),
		state:     state,
	}
	svc := newCodexTurnStateGatewayTestService(upstream)
	// A fresh cursor starts at the first configured route.
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{
		"http://slow.example:8080",
		"https://healthy.example:8443",
	})

	account := codexTurnStateGatewayTestAccount(992)
	started := time.Now()
	token, failure := svc.probeCodexTurnState(context.Background(), account, "gpt-5.5")

	require.Nil(t, failure)
	require.Equal(t, state, token.Value)
	require.Less(t, time.Since(started), 2*time.Second, "the second route should run within the one-second total probe budget")
	select {
	case <-upstream.firstDone:
	default:
		t.Fatal("the slow first route was not attempted")
	}
	require.Equal(t, []string{"http://slow.example:8080", "https://healthy.example:8443"}, upstream.routes())
}

func TestProbeCodexTurnStateLargePoolBoundsAttemptsWithoutDilutingRouteBudget(t *testing.T) {
	upstream := &codexTurnStateLargePoolBudgetUpstream{}
	svc := newCodexTurnStateGatewayTestService(upstream)
	svc.cfg.Gateway.CodexTurnState.ProbeTimeoutSeconds = 15
	pool := make([]string, openAICodexTurnStateProxyPoolMaxEntries)
	for index := range pool {
		pool[index] = fmt.Sprintf("http://proxy-%03d.example:8080", index)
	}
	svc.SetCodexTurnStateRuntimeSettingsWithControls(true, true, pool, CodexTurnStateHarvestControls{
		SpeedPreset: "burst", MaxRequestsPerRound: 20, FailureCooldownSeconds: 1,
	})

	for round := 0; round < 2; round++ {
		token, failure := svc.probeCodexTurnState(context.Background(), codexTurnStateGatewayTestAccount(993+int64(round)), "gpt-5.5")
		require.Empty(t, token.Value)
		require.NotNil(t, failure)
	}
	routes, budgets := upstream.snapshot()
	require.Len(t, routes, 2*openAICodexTurnStateProbeMaxRouteAttempts)
	require.Len(t, budgets, 2*openAICodexTurnStateProbeMaxRouteAttempts)
	for index, route := range routes {
		require.Equal(t, fmt.Sprintf("http://proxy-%03d.example:8080", index), route)
	}
	for _, budget := range budgets {
		require.Greater(t, budget, 2*time.Second, "a 256-route pool must not dilute a 15-second round to millisecond deadlines")
	}
}

func TestCodexTurnStateHarvestBudgetResetsAfterRound(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	svc.SetCodexTurnStateRuntimeSettingsWithControls(true, true, nil, CodexTurnStateHarvestControls{
		SpeedPreset: "fast", MaxRequestsPerRound: 2, FailureCooldownSeconds: 60,
	})
	base := time.Now().UTC().Truncate(time.Second)
	require.True(t, svc.reserveCodexTurnStateHarvestRequest(base))
	require.True(t, svc.reserveCodexTurnStateHarvestRequest(base))
	require.False(t, svc.reserveCodexTurnStateHarvestRequest(base))

	_, used, limit, resetAt := svc.codexTurnStateHarvestSummary(base.Add(59 * time.Second))
	require.Equal(t, 2, used)
	require.Equal(t, 2, limit)
	require.NotNil(t, resetAt)
	require.Equal(t, base.Add(60*time.Second), *resetAt)

	require.True(t, svc.reserveCodexTurnStateHarvestRequest(base.Add(60*time.Second)))
	_, used, limit, resetAt = svc.codexTurnStateHarvestSummary(base.Add(60 * time.Second))
	require.Equal(t, 1, used)
	require.Equal(t, 2, limit)
	require.Equal(t, base.Add(120*time.Second), *resetAt)
}

func TestCodexTurnStateHarvestNodeCooldownAndSafeProjection(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	proxyURL := "http://private-user:private-password@proxy.example:8080"
	svc.SetCodexTurnStateRuntimeSettingsWithControls(true, true, []string{proxyURL}, CodexTurnStateHarvestControls{
		SpeedPreset: "fast", MaxRequestsPerRound: 12, FailureCooldownSeconds: 60,
	})
	base := time.Now().UTC().Truncate(time.Second)
	generation := svc.codexTurnStateDiagnosticsGeneration()
	svc.recordCodexTurnStateHarvestNode(proxyURL, &openAICodexTurnStateProbeFailure{code: "transport_error"}, 25*time.Millisecond, base, generation)
	require.False(t, svc.codexTurnStateHarvestRouteAvailable(proxyURL, base.Add(59*time.Second)))
	nodes, _, _, _ := svc.codexTurnStateHarvestSummary(base.Add(59 * time.Second))
	require.Len(t, nodes, 1)
	require.Equal(t, codexTurnStateHarvestNodeID(proxyURL), nodes[0].NodeID)
	require.Equal(t, "http://proxy.example:8080", nodes[0].Label)
	require.Equal(t, uint64(1), nodes[0].Failures)
	require.Equal(t, uint64(1), nodes[0].ConsecutiveFailures)
	require.Equal(t, 1, nodes[0].CooldownRemainingSeconds)
	require.Equal(t, "transport_error", nodes[0].LastResult)
	require.NotContains(t, fmt.Sprintf("%+v", nodes[0]), "private-password")
	require.True(t, svc.codexTurnStateHarvestRouteAvailable(proxyURL, base.Add(60*time.Second)))

	svc.recordCodexTurnStateHarvestNode(proxyURL, nil, 10*time.Millisecond, base.Add(61*time.Second), generation)
	nodes, _, _, _ = svc.codexTurnStateHarvestSummary(base.Add(61 * time.Second))
	require.Equal(t, uint64(1), nodes[0].Successes)
	require.Zero(t, nodes[0].ConsecutiveFailures)
	require.Zero(t, nodes[0].CooldownRemainingSeconds)
	require.Equal(t, "success", nodes[0].LastResult)

	svc.recordCodexTurnStateHarvestNode(proxyURL, &openAICodexTurnStateProbeFailure{code: "upstream_401"}, 10*time.Millisecond, base.Add(62*time.Second), generation)
	require.True(t, svc.codexTurnStateHarvestRouteAvailable(proxyURL, base.Add(62*time.Second)),
		"an account-specific upstream rejection must not isolate the shared proxy")
	nodes, _, _, _ = svc.codexTurnStateHarvestSummary(base.Add(62 * time.Second))
	require.Equal(t, uint64(2), nodes[0].Failures)
	require.Equal(t, uint64(1), nodes[0].ConsecutiveFailures)
	require.Zero(t, nodes[0].CooldownRemainingSeconds)
	require.Equal(t, "upstream_401", nodes[0].LastResult)
}

func TestCodexTurnStatePoolChangeInvalidatesPinnedTicketButNoopResavePreservesIt(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	poolA := []string{"http://route-a.example:8080"}
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, poolA)
	base := time.Now().UTC().Truncate(time.Second)
	logicalKey := OpenAICodexTurnStateKey{AccountID: 994, Scope: "execution", Model: "gpt-5.5"}
	key := svc.codexTurnStateCollector.BindKey(logicalKey)
	value := collectorTestToken(t, base, 2, 200)
	token, err := ParseOpenAICodexTurnState(value)
	require.NoError(t, err)
	require.True(t, svc.codexTurnStateCollector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
		Token: token, Route: "probe", EgressProxyURL: poolA[0], EgressPinned: true,
	}, base))

	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{poolA[0]})
	active, usable := svc.codexTurnStateCollector.Acquire(key, base)
	require.True(t, usable, "resaving the identical pool should preserve active collection")
	require.Equal(t, value, active.Token.Value)

	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, []string{"http://route-b.example:8080"})
	_, usable = svc.codexTurnStateCollector.Acquire(key, base)
	require.False(t, usable, "a request already bound to a removed route must not replay the old ticket")
	current := svc.codexTurnStateCollector.BindKey(logicalKey)
	_, usable = svc.codexTurnStateCollector.Acquire(current, base)
	require.False(t, usable, "new execution scopes must not find a ticket pinned to the old pool")
}
