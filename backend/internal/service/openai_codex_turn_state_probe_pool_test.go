package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type turnStateProxyRepo struct {
	ProxyRepository
	proxies  []Proxy
	calls    atomic.Int32
	getCalls atomic.Int32
}

func (r *turnStateProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	r.getCalls.Add(1)
	for i := range r.proxies {
		if r.proxies[i].ID != id {
			continue
		}
		proxy := r.proxies[i]
		return &proxy, nil
	}
	return nil, ErrProxyNotFound
}

func (r *turnStateProxyRepo) ListActive(context.Context) ([]Proxy, error) {
	r.calls.Add(1)
	return r.proxies, nil
}

func (r *turnStateProxyRepo) ListAllForFallback(context.Context) ([]Proxy, error) {
	r.calls.Add(1)
	return append([]Proxy(nil), r.proxies...), nil
}

type turnStateProbeSequenceUpstream struct {
	HTTPUpstream
	call func(*http.Request, string, int64) (*http.Response, error)
}

type turnStateConcurrentProxyRepo struct {
	ProxyRepository
	proxies []Proxy
}

func (r *turnStateConcurrentProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	for i := range r.proxies {
		if r.proxies[i].ID != id {
			continue
		}
		proxy := r.proxies[i]
		return &proxy, nil
	}
	return nil, ErrProxyNotFound
}

type turnStateDurableBoundaryRepo struct {
	*turnStateAutoRepo
	statsMu       sync.Mutex
	boundaryErr   error
	boundaryCalls int
	sourceCalls   int
}

func (r *turnStateDurableBoundaryRepo) AdvanceCodexTurnStateProbeNotBefore(_ context.Context, accountID, notBefore int64) error {
	r.statsMu.Lock()
	r.boundaryCalls++
	err := r.boundaryErr
	r.statsMu.Unlock()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.accounts[accountID]
	if account == nil {
		return ErrAccountNotFound
	}
	if codexTurnStateAutoInt64(account, CodexTurnStateAutoProbeNotBeforeExtraKey) < notBefore {
		account.Extra = mergeMap(account.Extra, map[string]any{CodexTurnStateAutoProbeNotBeforeExtraKey: notBefore})
	}
	return nil
}

func (r *turnStateDurableBoundaryRepo) GetCodexTurnStateSource(ctx context.Context, accountID int64) (*Account, error) {
	r.statsMu.Lock()
	r.sourceCalls++
	r.statsMu.Unlock()
	return r.turnStateAutoRepo.GetByID(ctx, accountID)
}

func (r *turnStateDurableBoundaryRepo) setBoundaryError(err error) {
	r.statsMu.Lock()
	r.boundaryErr = err
	r.statsMu.Unlock()
}

func (r *turnStateDurableBoundaryRepo) boundaryCallCount() int {
	r.statsMu.Lock()
	defer r.statsMu.Unlock()
	return r.boundaryCalls
}

func (r *turnStateConcurrentProxyRepo) ListActive(context.Context) ([]Proxy, error) {
	return append([]Proxy(nil), r.proxies...), nil
}

func (r *turnStateConcurrentProxyRepo) ListAllForFallback(context.Context) ([]Proxy, error) {
	return append([]Proxy(nil), r.proxies...), nil
}

func (u *turnStateProbeSequenceUpstream) Do(req *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
	return u.call(req, proxy, id)
}

func turnStateDedicatedTemplateProxy() Proxy {
	return Proxy{
		Name:     "test-dynamic-proxy",
		Protocol: "socks5", Host: codexTurnState1024ProxyHost, Port: codexTurnState1024ProxyPort,
		Username: "testacct-region-US", Password: "test-secret", Status: StatusActive,
	}
}

func turnStateDedicatedSessionProxy(sessionID string) Proxy {
	proxy := turnStateDedicatedTemplateProxy()
	proxy.Name = "test-sticky-" + sessionID
	proxy.Username = "testacct-region-US-sid-" + sessionID + "-t-5"
	return proxy
}

func requireDedicatedTurnStateRoute(t *testing.T, route string) string {
	t.Helper()
	parsed, err := url.Parse(route)
	require.NoError(t, err)
	require.Equal(t, "socks5", parsed.Scheme)
	require.Equal(t, codexTurnState1024ProxyHost, parsed.Hostname())
	require.Equal(t, "3000", parsed.Port())
	require.NotNil(t, parsed.User)
	password, ok := parsed.User.Password()
	require.True(t, ok)
	require.Equal(t, "test-secret", password)
	return parsed.User.Username()
}

func TestCodexTurnStateProbePoolRetriesModelMismatchWithUniqueRoutes(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	fresh := testGlobalTurnStateToken(time.Now(), 10)
	pool := &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
	s.proxyRepo = pool
	var routes []string
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, route string, _ int64) (*http.Response, error) {
		routes = append(routes, route)
		require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), codexTurnStateProbeSessionTimeout)
		if len(routes) == 1 {
			return turnStateModelResponse("mismatched-candidate", "gpt-5.6-luna"), nil
		}
		return turnStateResponse(fresh), nil
	}}
	s.autoTurnStateForAccount(context.Background(), account, "gpt-5.5")
	waitTurnStateAutoIdle(t, s)
	require.Len(t, routes, 2)
	require.NotEqual(t, routes[0], routes[1], "a fresh pool route may be attempted only once")
	for _, route := range routes {
		require.Regexp(t, `^testacct-region-US-sid-[A-Za-z0-9]{8}-t-5$`, requireDedicatedTurnStateRoute(t, route))
	}
	require.EqualValues(t, 1, pool.calls.Load())
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5.5")), "maintenance evidence alone must not publish")
	s.openaiTurnStateMu.Lock()
	staged := s.openaiTurnStates[codexTurnStateKey{account.ID, "gpt-5.5"}].candidate.state
	s.openaiTurnStateMu.Unlock()
	require.Equal(t, fresh, staged)
	require.Nil(t, stored.ProxyID, "maintenance must not change the account route")
}

func TestCodexTurnStateProbePoolBoundsAndStops(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		state                    string
		status, calls, poolCalls int
	}{
		{"fresh", testGlobalTurnStateToken(time.Now(), 10), 200, 1, 1},
		{"team-length-is-diagnostic", testGlobalTurnStateToken(time.Now(), 12), 200, 1, 1},
		{"opaque-with-model-evidence", "opaque", 200, 1, 1},
		{"312-length-is-diagnostic", testGlobalTurnStateToken(time.Now(), 11), 200, 1, 1},
		{"356-length-is-diagnostic", testGlobalTurnStateToken(time.Now(), 13), 200, 1, 1},
		{"invalid-retries-three-unique-routes", "bad\nstate", 200, 3, 1},
		{"auth-stops", "", 401, 1, 1},
		{"access-denied-stops", "", 403, 1, 1},
		{"quota-stops", "", 429, 1, 1},
		{"quota-with-state-stops", testGlobalTurnStateToken(time.Now(), 11), 429, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, account := newTurnStateAutoService(t)
			pool := &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
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
			require.EqualValues(t, tc.poolCalls, pool.calls.Load())
		})
	}
}

func TestCodexTurnStateProbePoolValidatesAddressesAndCapsRoutes(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	proxies := []Proxy{
		// Names do not gate whether a syntactically valid 1024Proxy record can
		// participate in dynamic maintenance routing.
		{Protocol: "socks5", Host: codexTurnState1024ProxyHost, Port: codexTurnState1024ProxyPort, Username: "testacct-region-US-sid-NORMAL01-t-5", Password: "test-secret", Status: StatusActive},
		{Name: "expired-without-address", Status: StatusActive, ExpiresAt: &expired},
		{Name: "disabled-without-address", Status: "disabled"},
	}
	for _, sessionID := range []string{"SESSION01", "SESSION02", "SESSION03", "SESSION04", "SESSION01"} {
		proxies = append(proxies, turnStateDedicatedSessionProxy(sessionID))
	}
	s := &OpenAIGatewayService{proxyRepo: &turnStateProxyRepo{proxies: proxies}}
	primaryTemplate, err := parseCodexTurnStateProbeProxyTemplate("socks5://testacct-region-US-sid-SESSION04-t-5:test-secret@us.1024proxy.io:3000")
	require.NoError(t, err)
	routes := s.codexTurnStatePoolRoutes(context.Background(), primaryTemplate.route("SESSION04"))
	require.Len(t, routes, 3)
	sessions := map[string]bool{}
	for _, route := range routes {
		username := requireDedicatedTurnStateRoute(t, route)
		require.Contains(t, []string{
			"testacct-region-US-sid-NORMAL01-t-5",
			"testacct-region-US-sid-SESSION01-t-5",
			"testacct-region-US-sid-SESSION02-t-5",
			"testacct-region-US-sid-SESSION03-t-5",
		}, username)
		sessions[username] = true
	}
	require.Len(t, sessions, 3)
}

func TestCodexTurnStateProbePoolUsesInactiveAndExpiredStaticRecordsWhenDynamicIsAbsent(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	primary := Proxy{ID: 1, Protocol: "socks5", Host: "primary.example", Port: 1080, Username: "u", Password: "p", Status: StatusActive}
	proxies := []Proxy{
		primary,
		{ID: 2, Protocol: "http", Host: "pool-a.example", Port: 8080, Status: StatusActive},
		{ID: 3, Protocol: "socks5", Host: "pool-b.example", Port: 1081, Status: StatusActive},
		{ID: 4, Protocol: "https", Host: "pool-c.example", Port: 8443, Status: StatusActive},
		{ID: 5, Protocol: "socks5h", Host: "pool-d.example", Port: 1082, Status: StatusActive},
		{ID: 6, Protocol: "socks5", Host: "expired.example", Port: 1080, Status: StatusActive, ExpiresAt: &expired},
		{ID: 7, Protocol: "socks5", Host: "disabled.example", Port: 1080, Status: "disabled"},
		{ID: 8, Protocol: "ftp", Host: "invalid.example", Port: 21, Status: StatusActive},
		// This is an ordinary pool record, not a dedicated Turn State record;
		// it must not opt the whole pool into the dynamic branch.
		{ID: 9, Protocol: "socks5", Host: "pool-a.example", Port: 8080, Status: StatusActive},
	}
	repo := &turnStateProxyRepo{proxies: proxies}
	s := &OpenAIGatewayService{proxyRepo: repo}

	routes := s.codexTurnStatePoolRoutes(context.Background(), primary.URL())
	require.Len(t, routes, codexTurnStateProbePoolSize)
	require.EqualValues(t, 1, repo.calls.Load())
	allowedHosts := map[string]bool{
		"pool-a.example": true, "pool-b.example": true, "pool-c.example": true,
		"pool-d.example": true, "expired.example": true, "disabled.example": true,
	}
	seen := make(map[string]bool, len(routes))
	for _, route := range routes {
		parsed, err := url.Parse(route)
		require.NoError(t, err)
		require.NotEqual(t, primary.Host, parsed.Host)
		require.True(t, allowedHosts[parsed.Hostname()], "unexpected fallback route host %q", parsed.Hostname())
		seen[route] = true
	}
	require.Len(t, seen, codexTurnStateProbePoolSize)
}

func TestCodexTurnStateProbePoolPrefersDynamicRecordsOverOrdinaryIPPool(t *testing.T) {
	dynamic := turnStateDedicatedSessionProxy("SESSION01")
	ordinary := Proxy{ID: 77, Protocol: "http", Host: "ordinary.example", Port: 8080, Status: StatusActive}
	repo := &turnStateProxyRepo{proxies: []Proxy{dynamic, ordinary}}
	s := &OpenAIGatewayService{proxyRepo: repo}

	routes := s.codexTurnStatePoolRoutes(context.Background(), "")
	require.Len(t, routes, 1)
	require.EqualValues(t, 1, repo.calls.Load())
	require.Equal(t, "testacct-region-US-sid-SESSION01-t-5", requireDedicatedTurnStateRoute(t, routes[0]))
}

func TestCodexTurnStateProbePoolIgnoresNameStatusAndExpiryWithoutExplicitID(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	proxy := turnStateDedicatedTemplateProxy()
	proxy.Name = "ordinary proxy from any vendor catalog"
	proxy.Status = "disabled"
	proxy.ExpiresAt = &expired
	s := &OpenAIGatewayService{proxyRepo: &turnStateProxyRepo{proxies: []Proxy{proxy}}}

	routes := s.codexTurnStatePoolRoutes(context.Background(), "")
	require.Len(t, routes, codexTurnStateProbePoolSize)
	for _, route := range routes {
		require.Regexp(t, `^testacct-region-US-sid-[A-Za-z0-9]{8}-t-5$`, requireDedicatedTurnStateRoute(t, route))
	}
}

func TestParseCodexTurnStateProbeProxyTemplatePreservesAuthenticatedRegionAndSession(t *testing.T) {
	template, err := parseCodexTurnStateProbeProxyTemplate("us.1024proxy.io:3000:testacct-region-US-sid-Ab12Cd34-t-5:test-secret")
	require.NoError(t, err)
	require.False(t, template.dynamic)
	require.Equal(t, "Ab12Cd34", template.sessionID)
	require.Equal(t, "testacct-region-US-sid-Ab12Cd34-t-5", requireDedicatedTurnStateRoute(t, template.route(template.sessionID)))

	template, err = parseCodexTurnStateProbeProxyTemplate("socks5://testacct-region-US:test-secret@us.1024proxy.io:3000")
	require.NoError(t, err)
	require.True(t, template.dynamic)
	routes := buildCodexTurnStateProbeRoutes([]codexTurnStateProbeProxyTemplate{template}, "", bytes.NewReader([]byte{
		0, 1, 2, 3, 4, 5, 6, 7,
		8, 9, 10, 11, 12, 13, 14, 15,
		16, 17, 18, 19, 20, 21, 22, 23,
	}))
	require.Len(t, routes, codexTurnStateProbePoolSize)
	seen := map[string]bool{}
	for _, route := range routes {
		username := requireDedicatedTurnStateRoute(t, route)
		require.Regexp(t, `^testacct-region-US-sid-[A-Za-z0-9]{8}-t-5$`, username)
		seen[username] = true
	}
	require.Len(t, seen, codexTurnStateProbePoolSize)

	fixedCountry, err := parseCodexTurnStateProbeProxyTemplate("socks5://testacct-region-US-sid-Zx98Yw76-t-5:test-secret@us.1024proxy.io:3000")
	require.NoError(t, err)
	require.Equal(t, "US", fixedCountry.region)
	require.Equal(t, "testacct-region-US-sid-Zx98Yw76-t-5", requireDedicatedTurnStateRoute(t, fixedCountry.route("Zx98Yw76")))
}

func TestCodexTurnStateProbeProxyTemplateRejectsUnsafeOrWrongScope(t *testing.T) {
	for _, raw := range []string{
		"us.1024proxy.io:3000:testacct-region-Rand-sid-Ab12Cd34-t-5:test-secret",
		"us.1024proxy.io:3000:testacct-region-Rand:test-secret",
		"other.example:3000:testacct-region-Rand:test-secret",
		"us.1024proxy.io:3001:testacct-region-Rand:test-secret",
		"us.1024proxy.io:3000:testacct-region-USA-sid-Ab12Cd34-t-5:test-secret",
		"us.1024proxy.io:3000:testacct-region-us-sid-Ab12Cd34-t-5:test-secret",
		"us.1024proxy.io:3000:testacct-region-Rand-sid-short-t-5:test-secret",
		"us.1024proxy.io:3000:testacct-region-Rand-sid-Ab12Cd34-t-10:test-secret",
		"us.1024proxy.io:3000:" + strings.Repeat("a", 65) + "-region-Rand:test-secret",
		"socks5://testacct-region-Rand:test-secret@us.1024proxy.io:3000/path",
		"us.1024proxy.io:3000:testacct-region-Rand:test-secret\nleak",
	} {
		_, err := parseCodexTurnStateProbeProxyTemplate(raw)
		require.ErrorIs(t, err, errInvalidCodexTurnStateProbeProxy)
		require.NotContains(t, err.Error(), "test-secret")
	}
}

func TestCodexTurnStateProbePoolSkipsInvalidDedicatedAndFallsBackBestEffort(t *testing.T) {
	t.Run("invalid companion does not discard valid dynamic template", func(t *testing.T) {
		invalid := turnStateDedicatedTemplateProxy()
		invalid.Host = "other.example"
		s := &OpenAIGatewayService{proxyRepo: &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy(), invalid}}}

		routes := s.codexTurnStatePoolRoutes(context.Background(), "")
		require.Len(t, routes, codexTurnStateProbePoolSize)
		for _, route := range routes {
			require.Regexp(t, `^testacct-region-US-sid-[A-Za-z0-9]{8}-t-5$`, requireDedicatedTurnStateRoute(t, route))
		}
	})

	t.Run("invalid dedicated record falls back to ordinary proxy", func(t *testing.T) {
		invalid := turnStateDedicatedTemplateProxy()
		invalid.Protocol = "ftp"
		fallback := Proxy{Protocol: "http", Host: "fallback.example", Port: 8080, Status: StatusActive}
		s := &OpenAIGatewayService{proxyRepo: &turnStateProxyRepo{proxies: []Proxy{invalid, fallback}}}

		require.Equal(t, []string{fallback.URL()}, s.codexTurnStatePoolRoutes(context.Background(), ""))
	})

	t.Run("mixed credentials remain usable as static pool routes", func(t *testing.T) {
		first := turnStateDedicatedSessionProxy("SESSION08")
		second := turnStateDedicatedSessionProxy("SESSION09")
		second.Password = "different-test-secret"
		s := &OpenAIGatewayService{proxyRepo: &turnStateProxyRepo{proxies: []Proxy{first, second}}}

		require.ElementsMatch(t, []string{first.URL(), second.URL()}, s.codexTurnStatePoolRoutes(context.Background(), ""))
	})

	t.Run("mixed regions remain usable as static pool routes", func(t *testing.T) {
		first := turnStateDedicatedSessionProxy("SESSION08")
		second := turnStateDedicatedSessionProxy("SESSION09")
		second.Username = "testacct-region-CA-sid-SESSION09-t-5"
		s := &OpenAIGatewayService{proxyRepo: &turnStateProxyRepo{proxies: []Proxy{first, second}}}

		require.ElementsMatch(t, []string{first.URL(), second.URL()}, s.codexTurnStatePoolRoutes(context.Background(), ""))
	})
}

func TestCodexTurnStateConfiguredProxyIgnoresStatusExpiryVendorAndName(t *testing.T) {
	expired := time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name        string
		proxy       Proxy
		wantDynamic bool
	}{
		{
			name: "inactive dynamic template",
			proxy: func() Proxy {
				proxy := turnStateDedicatedTemplateProxy()
				proxy.Status = "disabled"
				return proxy
			}(),
			wantDynamic: true,
		},
		{
			name: "expired dynamic template",
			proxy: func() Proxy {
				proxy := turnStateDedicatedTemplateProxy()
				proxy.ExpiresAt = &expired
				return proxy
			}(),
			wantDynamic: true,
		},
		{
			name: "generic proxy record",
			proxy: Proxy{
				Name: "ordinary-vendor-proxy", Protocol: "http", Host: "generic-vendor.example", Port: 8080,
				Status: StatusActive,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const configuredProxyID int64 = 41
			tc.proxy.ID = configuredProxyID
			settings, settingRepo := turnStateTestSettings("", "")
			settingRepo.values[SettingKeyOpenAICodexTurnStateProxyID] = "41"
			proxyRepo := &turnStateProxyRepo{proxies: []Proxy{tc.proxy}}
			s := &OpenAIGatewayService{settingService: settings, proxyRepo: proxyRepo}

			routes := s.codexTurnStatePoolRoutes(context.Background(), "")
			if tc.wantDynamic {
				require.Len(t, routes, codexTurnStateProbePoolSize)
				for _, route := range routes {
					require.Regexp(t, `^testacct-region-US-sid-[A-Za-z0-9]{8}-t-5$`, requireDedicatedTurnStateRoute(t, route))
				}
			} else {
				require.Equal(t, []string{tc.proxy.URL()}, routes)
			}
			require.EqualValues(t, 1, proxyRepo.getCalls.Load())
			require.Zero(t, proxyRepo.calls.Load(), "configured proxy lookup must not depend on the active proxy list")
		})
	}
}

func TestCodexTurnStateLiveProxyParserPreservesFixedCountryStickySessionsAndCapsAtThree(t *testing.T) {
	raw := strings.Join([]string{
		"us.1024proxy.io:3000:testacct-region-US-sid-SESSION01-t-5:test-secret",
		"socks5://testacct-region-US-sid-SESSION02-t-5:test-secret@us.1024proxy.io:3000",
		"us.1024proxy.io:3000:testacct-region-US-sid-SESSION02-t-5:test-secret",
		"us.1024proxy.io:3000:testacct-region-US-sid-SESSION03-t-5:test-secret",
		"us.1024proxy.io:3000:testacct-region-US-sid-SESSION04-t-5:test-secret",
	}, "\n")
	daily := "socks5://daily:daily-secret@daily.example:443"
	normalizedDaily, dynamic := codexTurnStateLiveLoadProxies(t, daily, raw)
	parsedDaily, err := url.Parse(normalizedDaily)
	require.NoError(t, err)
	require.Equal(t, "daily.example", parsedDaily.Hostname())
	require.Equal(t, "443", parsedDaily.Port())
	require.Len(t, dynamic, codexTurnStateProbePoolSize)

	seen := map[string]bool{}
	for _, route := range dynamic {
		username := requireDedicatedTurnStateRoute(t, route)
		require.Contains(t, []string{
			"testacct-region-US-sid-SESSION01-t-5",
			"testacct-region-US-sid-SESSION02-t-5",
			"testacct-region-US-sid-SESSION03-t-5",
		}, username)
		seen[username] = true
	}
	require.Len(t, seen, codexTurnStateProbePoolSize)
}

func TestCodexTurnStateLiveProxyParserRejectsInvalidStickyScope(t *testing.T) {
	for _, raw := range []string{
		"socks5://testacct-region-USA-sid-Ab12Cd34-t-5:test-secret@us.1024proxy.io:3000",
		"socks5://testacct-region-Rand-sid-Ab12Cd34-t-5:test-secret@other.example:3000",
		"socks5://testacct-region-Rand-sid-Ab12Cd34-t-10:test-secret@us.1024proxy.io:3000",
		"socks5://testacct-region-Rand:test-secret@us.1024proxy.io:3000",
	} {
		_, _, _, err := codexTurnStateLiveNormalizeSOCKS5(raw, true)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "test-secret")
	}
}

func TestCodexTurnStateProbeRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	future := now.Add(37 * time.Second).UTC().Format(http.TimeFormat)
	for _, tc := range []struct {
		name   string
		header string
		want   time.Duration
		known  bool
	}{
		{name: "delta", header: "17", want: 17 * time.Second, known: true},
		{name: "http-date", header: future, want: 37 * time.Second, known: true},
		{name: "zero", header: "0"},
		{name: "negative", header: "-1"},
		{name: "past-date", header: now.Add(-time.Second).UTC().Format(http.TimeFormat)},
		{name: "invalid", header: "later"},
		{name: "overflow", header: "9223372036854775807"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delay, known := codexTurnStateProbeRetryAfter(http.Header{"Retry-After": []string{tc.header}}, now)
			require.Equal(t, tc.known, known)
			if tc.known {
				require.Equal(t, tc.want, delay)
			} else {
				require.Zero(t, delay)
			}
		})
	}

	delay, known := codexTurnStateProbeRetryAfter(nil, now)
	require.False(t, known)
	require.Zero(t, delay)
}

func TestCodexTurnStateProbe429DiagnosticStopsRotation(t *testing.T) {
	now := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	code, delay := codexTurnStateProbe429Diagnostic(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"12"}}, now)
	require.Equal(t, codexTurnStateProbe429RetryAfterCode, code)
	require.Equal(t, 12*time.Second, delay)

	code, delay = codexTurnStateProbe429Diagnostic(http.StatusTooManyRequests, http.Header{}, now)
	require.Equal(t, codexTurnStateProbe429NoRetryAfterCode, code)
	require.Equal(t, codexTurnStateProbe429Fallback, delay)

	code, delay = codexTurnStateProbe429Diagnostic(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"later"}}, now)
	require.Equal(t, codexTurnStateProbe429NoRetryAfterCode, code)
	require.Equal(t, codexTurnStateProbe429Fallback, delay)

	code, delay = codexTurnStateProbe429Diagnostic(http.StatusBadGateway, http.Header{"Retry-After": []string{"12"}}, now)
	require.Empty(t, code)
	require.Zero(t, delay)
}

func TestCodexTurnStateProbe429StopsWorkerAndSetsRetryBoundary(t *testing.T) {
	for _, tc := range []struct {
		name          string
		rateLimitCall int
		retryAfter    string
		wantCode      string
		wantDelay     time.Duration
		wantRoutes    int
	}{
		{
			name: "collection_obeys_delta", rateLimitCall: 1, retryAfter: "3600",
			wantCode: codexTurnStateProbe429RetryAfterCode, wantDelay: time.Hour, wantRoutes: 1,
		},
		{
			name: "same_route_replay_uses_missing_header_fallback", rateLimitCall: 2,
			wantCode: codexTurnStateProbe429NoRetryAfterCode, wantDelay: codexTurnStateProbe429Fallback, wantRoutes: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, account := newTurnStateAutoService(t)
			model := "gpt-5.5"
			old := testGlobalTurnStateToken(time.Now().Add(-51*time.Minute), 10)
			seedAutomaticTurnState(account, old, model)
			oldAt := time.Now().Add(-51 * time.Minute).UnixMilli()
			scoped := codexTurnStateModelAccount(account, model)
			scoped.Extra[CodexTurnStateAutoSetAtExtraKey] = oldAt
			scoped.Extra[CodexTurnStateAutoVerifiedAtExtraKey] = oldAt
			repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)

			pool := &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
			s.proxyRepo = pool
			candidate := "same-account-same-model-candidate"
			calls := 0
			routes := make([]string, 0, tc.rateLimitCall)
			s.httpUpstream = &turnStateProbeSequenceUpstream{call: func(req *http.Request, route string, _ int64) (*http.Response, error) {
				calls++
				routes = append(routes, route)
				if calls == tc.rateLimitCall {
					// Simulate a concurrent invalidation queuing forced work while this
					// request is in flight. The worker must not issue it after the 429.
					s.openaiTurnStateMu.Lock()
					entry := s.openaiTurnStates[codexTurnStateKey{account.ID, model}]
					entry.probe, entry.forceProbe = true, true
					s.openaiTurnStateMu.Unlock()

					resp := turnStateModelResponse("", model)
					resp.StatusCode = http.StatusTooManyRequests
					if tc.retryAfter != "" {
						resp.Header.Set("Retry-After", tc.retryAfter)
					}
					return resp, nil
				}
				if req.Header.Get(openAICodexTurnStateHeader) == "" {
					return turnStateModelResponse(candidate, model), nil
				}
				return turnStateModelResponse("", model), nil
			}}

			started := time.Now()
			require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), account, model))
			waitTurnStateAutoIdle(t, s)
			require.Equal(t, tc.rateLimitCall, calls, "429 must stop the current route round and queued forced work")
			require.EqualValues(t, 1, pool.calls.Load())
			uniqueRoutes := make(map[string]struct{}, len(routes))
			for _, route := range routes {
				uniqueRoutes[route] = struct{}{}
			}
			require.Len(t, uniqueRoutes, tc.wantRoutes)

			s.openaiTurnStateMu.Lock()
			entry := s.openaiTurnStates[codexTurnStateKey{account.ID, model}]
			retryAt := entry.probeRetryAfter
			forceProbe := entry.forceProbe
			s.openaiTurnStateMu.Unlock()
			require.True(t, forceProbe, "blocked forced work must remain pending for a later request")
			require.GreaterOrEqual(t, retryAt.Sub(started), tc.wantDelay-time.Second)
			require.Less(t, retryAt.Sub(started), tc.wantDelay+5*time.Second)

			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			storedScope := codexTurnStateModelAccount(stored, model)
			require.Equal(t, old, codexTurnStateAutoToken(storedScope), "429 must not replace the last verified state")
			require.Equal(t, tc.wantCode, storedScope.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
			persistedNotBefore := codexTurnStateAutoInt64(storedScope, CodexTurnStateAutoProbeNotBeforeExtraKey)
			require.GreaterOrEqual(t, persistedNotBefore, started.Add(tc.wantDelay-time.Second).UnixMilli())

			// A process restart must reload the upstream boundary rather than fall
			// back to the shorter five-minute probe cadence.
			restartScope := mergeMap(nil, storedScope.Extra)
			restartScope[CodexTurnStateAutoProbeAtExtraKey] = started.Add(-6 * time.Minute).UnixMilli()
			persistCtx, cancelPersist := context.WithTimeout(context.Background(), time.Second)
			require.NoError(t, repo.UpdateExtra(persistCtx, account.ID, map[string]any{codexTurnStateModelExtraKey(model): restartScope}))
			cancelPersist()
			stored, err = repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			restarted := &OpenAIGatewayService{
				settingService: s.settingService,
				accountRepo:    repo,
				httpUpstream:   s.httpUpstream,
				proxyRepo:      pool,
			}
			require.Equal(t, old, restarted.autoTurnStateForAccount(context.Background(), stored, model))
			waitTurnStateAutoIdle(t, restarted)
			require.Equal(t, tc.rateLimitCall, calls, "persisted Retry-After must survive a process restart")

			for range 10 {
				s.autoTurnStateForAccount(context.Background(), account, model)
			}
			waitTurnStateAutoIdle(t, s)
			require.Equal(t, tc.rateLimitCall, calls, "requests before retry boundary must not reschedule the probe")

			// The upstream probe boundary must not block persistence after the
			// durable acceptance gate has independently verified a candidate.
			native := "verified-native-during-probe-backoff"
			publishVerifiedTurnStateForTest(s, account, model, native)
			waitTurnStateAutoIdle(t, s)
			stored, err = repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, native, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, model)))
			require.Equal(t, tc.rateLimitCall, calls, "state persistence must not bypass the probe boundary")
		})
	}
}

func TestCodexTurnStateProbeDoesNotReplayOnAccountDailyRoute(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	model := "gpt-5.5"
	pool := &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
	s.proxyRepo = pool
	proxyID := int64(99)
	daily := &Proxy{ID: proxyID, Protocol: "http", Host: "daily.test", Port: 8080, Status: StatusActive}
	account.ProxyID, account.Proxy = &proxyID, daily
	repo.accounts[account.ID].ProxyID, repo.accounts[account.ID].Proxy = &proxyID, daily

	const candidate = "same-route-only-candidate"
	var routes []string
	s.httpUpstream = &turnStateProbeSequenceUpstream{call: func(req *http.Request, route string, _ int64) (*http.Response, error) {
		routes = append(routes, route)
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(candidate, model), nil
		}
		return turnStateModelResponse("", model), nil
	}}

	s.autoTurnStateForAccount(context.Background(), account, model)
	waitTurnStateAutoIdle(t, s)
	require.Len(t, routes, 2)
	require.Equal(t, routes[0], routes[1], "collection and replay must stay on the selected maintenance route")
	require.NotEqual(t, daily.URL(), routes[0], "the account daily route is not a publication prerequisite")
}

func TestCodexTurnStateProbe429CooldownIsSharedAcrossAccountModels(t *testing.T) {
	for _, tc := range []struct {
		name, heldModel, limitedModel string
	}{
		{name: "astra_429_stops_auto_review", heldModel: "codex-auto-review", limitedModel: "gpt-6-astra"},
		{name: "auto_review_429_stops_astra", heldModel: "gpt-6-astra", limitedModel: "codex-auto-review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, account := newTurnStateAutoService(t)
			settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
			settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6-astra*,codex-auto-review*"
			settings.values[SettingKeyOpenAICodexTurnStateDefaultModel] = "gpt-6-astra"
			s.settingService.InvalidateOpenAICodexTurnStateCache()
			s.proxyRepo = &turnStateConcurrentProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}

			heldStarted := make(chan struct{})
			releaseHeld := make(chan struct{})
			var releaseOnce sync.Once
			t.Cleanup(func() { releaseOnce.Do(func() { close(releaseHeld) }) })
			calls := make(map[string]int)
			var callsMu sync.Mutex
			s.httpUpstream = &turnStateProbeSequenceUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
				var payload struct {
					Model string `json:"model"`
				}
				require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
				callsMu.Lock()
				calls[payload.Model]++
				modelCall := calls[payload.Model]
				callsMu.Unlock()

				switch payload.Model {
				case tc.heldModel:
					if modelCall == 1 {
						close(heldStarted)
						<-releaseHeld
					}
					return turnStateModelResponse("held-model-candidate", payload.Model), nil
				case tc.limitedModel:
					resp := turnStateModelResponse("", payload.Model)
					resp.StatusCode = http.StatusTooManyRequests
					resp.Header.Set("Retry-After", "3600")
					return resp, nil
				default:
					t.Fatalf("unexpected probe model %q", payload.Model)
					return nil, nil
				}
			}}

			s.autoTurnStateForAccount(context.Background(), account, tc.heldModel)
			select {
			case <-heldStarted:
			case <-time.After(time.Second):
				t.Fatal("the held model probe did not start")
			}
			s.autoTurnStateForAccount(context.Background(), account, tc.limitedModel)
			require.Eventually(t, func() bool {
				s.openaiTurnStateMu.Lock()
				defer s.openaiTurnStateMu.Unlock()
				return s.codexTurnStateAccountProbeNotBeforeLocked(account.ID) > time.Now().UnixMilli()
			}, time.Second, time.Millisecond, "the 429 must install the account-wide boundary before releasing the other model")
			releaseOnce.Do(func() { close(releaseHeld) })
			waitTurnStateAutoIdle(t, s)

			callsMu.Lock()
			require.Equal(t, 1, calls[tc.limitedModel], "the 429 route must not rotate")
			require.Equal(t, 1, calls[tc.heldModel], "the concurrent model must stop before same-route replay")
			callsMu.Unlock()

			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			persistedBoundary := codexTurnStateAutoInt64(stored, CodexTurnStateAutoProbeNotBeforeExtraKey)
			require.Greater(t, persistedBoundary, time.Now().UnixMilli(), "the account-wide boundary must survive restart")

			publishVerifiedTurnStateForTest(s, account, tc.limitedModel, "verified-during-account-cooldown")
			waitTurnStateAutoIdle(t, s)
			s.openaiTurnStateMu.Lock()
			require.Greater(t, s.codexTurnStateAccountProbeNotBeforeLocked(account.ID), time.Now().UnixMilli(),
				"successful state acceptance must not release another model from the account-wide 429 boundary")
			s.openaiTurnStateMu.Unlock()

			s.autoTurnStateForAccount(context.Background(), account, tc.heldModel)
			waitTurnStateAutoIdle(t, s)
			restarted := &OpenAIGatewayService{
				settingService: s.settingService,
				accountRepo:    repo,
				httpUpstream:   s.httpUpstream,
				proxyRepo:      s.proxyRepo,
			}
			restarted.autoTurnStateForAccount(context.Background(), stored, tc.heldModel)
			waitTurnStateAutoIdle(t, restarted)
			callsMu.Lock()
			require.Equal(t, 1, calls[tc.heldModel], "neither later traffic nor a restarted process may bypass the other model's 429")
			callsMu.Unlock()
		})
	}
}

func TestCodexTurnStateProbe429BoundaryPersistenceRetriesWithoutReprobing(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	repo := &turnStateDurableBoundaryRepo{turnStateAutoRepo: baseRepo, boundaryErr: errors.New("temporary database failure")}
	s.accountRepo = repo
	s.proxyRepo = &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}

	model := "gpt-5.5"
	var upstreamCalls int
	s.httpUpstream = &turnStateProbeSequenceUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		upstreamCalls++
		resp := turnStateModelResponse("", model)
		resp.StatusCode = http.StatusTooManyRequests
		resp.Header.Set("Retry-After", "3600")
		return resp, nil
	}}

	s.autoTurnStateForAccount(context.Background(), account, model)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, 1, upstreamCalls, "database retries must never issue another upstream probe")
	require.Equal(t, codexTurnStateProbePersistAttempts, repo.boundaryCallCount())

	s.openaiTurnStateMu.Lock()
	entry := s.openaiTurnStates[codexTurnStateKey{account.ID, model}]
	require.NotNil(t, entry)
	require.True(t, entry.dirty, "exhausted boundary writes must remain pending")
	require.False(t, entry.probe, "persistence retry work must not carry an upstream probe")
	require.Greater(t, entry.probeNotBefore, time.Now().UnixMilli())
	repo.setBoundaryError(nil)
	entry.retryAfter = time.Time{}
	s.startCodexTurnStateWorkerLocked(account.ID, entry)
	s.openaiTurnStateMu.Unlock()
	waitTurnStateAutoIdle(t, s)

	require.Equal(t, 1, upstreamCalls, "a later successful database retry must still be persistence-only")
	require.Equal(t, codexTurnStateProbePersistAttempts+1, repo.boundaryCallCount())
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Greater(t, codexTurnStateAutoInt64(stored, CodexTurnStateAutoProbeNotBeforeExtraKey), time.Now().UnixMilli())
}

func TestCodexTurnStateProbeRefreshesDurableBoundaryBeforeEveryStage(t *testing.T) {
	for _, tc := range []struct {
		name             string
		installAfterCall int
		transportFailure bool
		wantCalls        int
	}{
		{name: "next_exit", installAfterCall: 1, transportFailure: true, wantCalls: 1},
		{name: "same_exit_replay", installAfterCall: 1, wantCalls: 1},
		{name: "candidate_stage", installAfterCall: 2, wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, baseRepo, account := newTurnStateAutoService(t)
			repo := &turnStateDurableBoundaryRepo{turnStateAutoRepo: baseRepo}
			b.accountRepo = repo
			b.proxyRepo = &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
			model := "gpt-5.5"

			// A represents another process sharing only the durable account row.
			a := &OpenAIGatewayService{settingService: b.settingService, accountRepo: repo}
			installBoundary := func() {
				notBefore := time.Now().Add(time.Hour).UnixMilli()
				a.openaiTurnStateMu.Lock()
				entry := a.codexTurnStateEntryLocked(account, time.Now(), model)
				entry.probeNotBefore = notBefore
				entry.probeRetryAfter = time.UnixMilli(notBefore)
				a.openaiTurnStateMu.Unlock()
				require.NoError(t, a.persistCodexTurnState(account.ID, entry))
			}

			candidate := "same-account-same-model-candidate"
			calls := 0
			b.httpUpstream = &turnStateProbeSequenceUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls++
				if calls == tc.installAfterCall {
					installBoundary()
				}
				if tc.transportFailure && calls == 1 {
					return nil, errors.New("temporary transport failure")
				}
				if calls == 1 {
					return turnStateModelResponse(candidate, model), nil
				}
				return turnStateModelResponse("", model), nil
			}}

			b.openaiTurnStateMu.Lock()
			entry := b.codexTurnStateEntryLocked(account, time.Now(), model)
			entry.forceProbe = true
			b.openaiTurnStateMu.Unlock()
			b.runCodexTurnStateProbe(account.ID, entry)

			require.Equal(t, tc.wantCalls, calls, "instance B must stop before the next upstream stage")
			b.openaiTurnStateMu.Lock()
			require.Greater(t, b.codexTurnStateAccountProbeNotBeforeLocked(account.ID), time.Now().UnixMilli())
			b.openaiTurnStateMu.Unlock()
			repo.statsMu.Lock()
			sourceCalls := repo.sourceCalls
			repo.statsMu.Unlock()
			require.GreaterOrEqual(t, sourceCalls, tc.wantCalls+1, "each attempted stage must refresh the shared durable boundary")
		})
	}
}
