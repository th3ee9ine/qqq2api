package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type turnStateProxyRepo struct {
	ProxyRepository
	proxies []Proxy
	calls   int
}

func (r *turnStateProxyRepo) ListActive(context.Context) ([]Proxy, error) {
	r.calls++
	return r.proxies, nil
}

func TestCodexTurnStateProbePoolRecovers312InSecondRound(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	old := testGlobalTurnStateToken(time.Now().Add(-51*time.Minute), 10)
	fresh := testGlobalTurnStateToken(time.Now(), 10)
	signal := testGlobalTurnStateToken(time.Now(), 11)
	account.Extra = map[string]any{codexTurnStateModelExtraKey("gpt-5.5"): map[string]any{CodexTurnStateAutoExtraKey: old, CodexTurnStateAutoSetAtExtraKey: time.Now().Add(-51 * time.Minute).UnixMilli()}}
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	pool := &turnStateProxyRepo{proxies: []Proxy{{ID: 7, Protocol: "http", Host: "pool.test", Port: 8080, Status: StatusActive}}}
	s.proxyRepo = pool
	var routes []string
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, route string, _ int64) (*http.Response, error) {
		routes = append(routes, route)
		require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), codexTurnStateProbeAttemptTimeout)
		switch len(routes) {
		case 1:
			return turnStateResponse(signal), nil
		case 2:
			s.openaiTurnStateMu.Lock()
			revoked := s.openaiTurnStates[codexTurnStateKey{account.ID, "gpt-5.5"}].token == "" && s.openaiTurnStates[codexTurnStateKey{account.ID, "gpt-5.5"}].recovery.Pending
			s.openaiTurnStateMu.Unlock()
			require.True(t, revoked, "312 must revoke before the next pool request")
			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.True(t, codexTurnStateRecoveryFromAccount(codexTurnStateModelAccount(stored, "gpt-5.5")).Pending)
			require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.5")))
			return turnStateResponse(old), nil // A valid but revoked 292 must not stop the rounds.
		default:
			return turnStateResponse(fresh), nil
		}
	}}
	s.autoTurnStateForAccount(context.Background(), account, "gpt-5.5")
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, []string{"", "http://pool.test:8080", "http://pool.test:8080"}, routes)
	require.Equal(t, 1, pool.calls)
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, fresh, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.5")))
	require.False(t, codexTurnStateRecoveryFromAccount(codexTurnStateModelAccount(stored, "gpt-5.5")).Pending)
	require.Nil(t, stored.ProxyID, "maintenance must not change the account route")
}

func TestCodexTurnStateProbePoolBoundsAndStops(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		state                    string
		status, calls, poolCalls int
	}{
		{"fresh-primary", testGlobalTurnStateToken(time.Now(), 10), 200, 1, 0},
		{"opaque-never-accepted", "opaque", 200, 4, 1},
		{"312-bounded", testGlobalTurnStateToken(time.Now(), 11), 200, 4, 1},
		{"356-bounded", testGlobalTurnStateToken(time.Now(), 13), 200, 4, 1},
		{"auth-stops", "", 401, 1, 0},
		{"access-denied-stops", "", 403, 1, 0},
		{"quota-stops", "", 429, 1, 0},
		{"quota-with-312", testGlobalTurnStateToken(time.Now(), 11), 429, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, account := newTurnStateAutoService(t)
			pool := &turnStateProxyRepo{proxies: []Proxy{{Protocol: "socks5", Host: "pool.test", Port: 1080, Status: StatusActive}}}
			s.proxyRepo = pool
			calls := 0
			s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls++
				r := turnStateResponse(tc.state)
				r.StatusCode = tc.status
				return r, nil
			}}
			s.autoTurnStateForAccount(context.Background(), account, "gpt-5.5")
			waitTurnStateAutoIdle(t, s)
			s.autoTurnStateForAccount(context.Background(), account, "gpt-5.5")
			waitTurnStateAutoIdle(t, s)
			require.Equal(t, tc.calls, calls)
			require.Equal(t, tc.poolCalls, pool.calls)
			if codexTurnStateIsRecoverySignal(tc.state) {
				s.openaiTurnStateMu.Lock()
				pending := s.openaiTurnStates[codexTurnStateKey{account.ID, "gpt-5.5"}].recovery.Pending
				s.openaiTurnStateMu.Unlock()
				require.True(t, pending)
			}
		})
	}
}

func TestCodexTurnStateProbePoolFiltersAndCapsRoutes(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	proxies := []Proxy{
		{Protocol: "http", Host: "primary", Port: 80, Status: StatusActive},
		{Protocol: "http", Host: "expired", Port: 80, Status: StatusActive, ExpiresAt: &expired},
		{Protocol: "http", Host: "disabled", Port: 80, Status: "disabled"},
		{Protocol: "unknown", Host: "unsupported", Port: 80, Status: StatusActive},
	}
	for _, host := range []string{"a", "b", "c", "d", "a"} {
		proxies = append(proxies, Proxy{Protocol: "http", Host: host, Port: 80, Status: StatusActive})
	}
	s := &OpenAIGatewayService{proxyRepo: &turnStateProxyRepo{proxies: proxies}}
	routes := s.codexTurnStatePoolRoutes(context.Background(), "http://primary:80")
	require.Len(t, routes, 9)
	counts := map[string]int{}
	for _, route := range routes {
		require.Contains(t, []string{"http://a:80", "http://b:80", "http://c:80", "http://d:80"}, route)
		counts[route]++
	}
	require.Len(t, counts, 3)
	for _, count := range counts {
		require.Equal(t, 3, count)
	}
}
