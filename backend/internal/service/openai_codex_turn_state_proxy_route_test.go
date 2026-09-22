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
	svc.SetCodexTurnStateRuntimeSettingsWithProxyPool(true, true, pool)

	for round := 0; round < 2; round++ {
		token, failure := svc.probeCodexTurnState(context.Background(), codexTurnStateGatewayTestAccount(993), "gpt-5.5")
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
