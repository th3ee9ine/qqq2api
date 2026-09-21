package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
)

// codexTurnStateTransportRecorder keeps the transport assertions local to the
// shared OAuth egress helper. The real collector uses the same helper as the
// account test path, so these checks protect the account/proxy/concurrency
// contract without making a network request.
type codexTurnStateTransportRecorder struct {
	doCalls       int
	doTLSCalls    int
	proxyURL      string
	accountID     int64
	concurrency   int
	profilePassed *tlsfingerprint.Profile
}

func (r *codexTurnStateTransportRecorder) response() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       http.NoBody,
	}
}

func (r *codexTurnStateTransportRecorder) Do(_ *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	r.doCalls++
	r.proxyURL = proxyURL
	r.accountID = accountID
	r.concurrency = concurrency
	return r.response(), nil
}

func (r *codexTurnStateTransportRecorder) DoWithTLS(_ *http.Request, proxyURL string, accountID int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	r.doTLSCalls++
	r.proxyURL = proxyURL
	r.accountID = accountID
	r.concurrency = concurrency
	r.profilePassed = profile
	return r.response(), nil
}

func TestDoOpenAIOAuthTransportUsesAccountRouteAndConcurrency(t *testing.T) {
	recorder := &codexTurnStateTransportRecorder{}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 7}

	response, handled, err := doOpenAIOAuthTransport(
		request,
		"socks5://probe.example:1080",
		account,
		nil,
		recorder,
		&TLSFingerprintProfileService{},
		true,
	)
	require.NoError(t, err)
	require.False(t, handled)
	require.NotNil(t, response)
	defer response.Body.Close()
	require.Equal(t, 0, recorder.doCalls)
	require.Equal(t, 1, recorder.doTLSCalls)
	require.Equal(t, "socks5://probe.example:1080", recorder.proxyURL)
	require.Equal(t, int64(42), recorder.accountID)
	require.Equal(t, 7, recorder.concurrency)
	require.Nil(t, recorder.profilePassed, "OpenAI accounts do not opt into the Anthropic TLS fingerprint profile")
}

func TestDoOpenAIOAuthTransportGivesPluginFirstChance(t *testing.T) {
	recorder := &codexTurnStateTransportRecorder{}
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{rolloutPercent: 100, unavailable: "plugin unavailable"})
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	account := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3}

	response, handled, err := doOpenAIOAuthTransport(request, "", account, manager, recorder, nil, true)
	require.Error(t, err)
	require.True(t, handled)
	require.Nil(t, response)
	require.Equal(t, 0, recorder.doCalls)
	require.Equal(t, 0, recorder.doTLSCalls)
}

func TestDoOpenAIOAuthTransportRoutesPluginByCredentialAccount(t *testing.T) {
	// Pick IDs on opposite sides of a partial rollout. If the helper passed the
	// shadow to PluginManager, the shadow bucket would be handled (and return the
	// unavailable-route error). Passing the parent credential account must leave
	// this request on the TLS fallback instead.
	var parentID, shadowID int64
	for id := int64(1); id < 10000 && (parentID == 0 || shadowID == 0); id++ {
		if stablePluginBucket(id) >= 50 && parentID == 0 {
			parentID = id
		}
		if stablePluginBucket(id) < 50 && shadowID == 0 {
			shadowID = id
		}
	}
	require.NotZero(t, parentID)
	require.NotZero(t, shadowID)

	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{rolloutPercent: 50, unavailable: "plugin unavailable"})
	recorder := &codexTurnStateTransportRecorder{}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	shadow := &Account{ID: shadowID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 9}
	parent := &Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2}

	response, handled, err := doOpenAIOAuthTransportWithCredentialAccount(
		request,
		"socks5://shadow-route.example:1080",
		shadow,
		parent,
		manager,
		recorder,
		nil,
		true,
	)
	require.NoError(t, err)
	require.False(t, handled)
	require.NotNil(t, response)
	defer response.Body.Close()
	require.Equal(t, 0, recorder.doCalls)
	require.Equal(t, 1, recorder.doTLSCalls)
	require.Equal(t, shadowID, recorder.accountID)
	require.Equal(t, shadow.Concurrency, recorder.concurrency)
	require.Equal(t, "socks5://shadow-route.example:1080", recorder.proxyURL)
}

// codexTurnStateCollectorUpstream records the actual collector calls rather
// than only exercising the shared helper. The first response mints a state;
// the second is the required same-route replay.
type codexTurnStateCollectorUpstream struct {
	mu sync.Mutex

	doCalls       int
	doTLSCalls    int
	requests      []*http.Request
	proxies       []string
	accountIDs    []int64
	concurrencies []int
	profiles      []*tlsfingerprint.Profile
}

func (u *codexTurnStateCollectorUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	u.doCalls++
	u.mu.Unlock()
	return nil, errors.New("collector must use DoWithTLS")
}

func (u *codexTurnStateCollectorUpstream) DoWithTLS(
	request *http.Request,
	proxyURL string,
	accountID int64,
	concurrency int,
	profile *tlsfingerprint.Profile,
) (*http.Response, error) {
	u.mu.Lock()
	u.doTLSCalls++
	call := u.doTLSCalls
	u.requests = append(u.requests, request)
	u.proxies = append(u.proxies, proxyURL)
	u.accountIDs = append(u.accountIDs, accountID)
	u.concurrencies = append(u.concurrencies, concurrency)
	u.profiles = append(u.profiles, profile)
	u.mu.Unlock()

	if call == 1 {
		return turnStateModelResponse("parent-state", "gpt-5"), nil
	}
	return turnStateModelResponse("", "gpt-5"), nil
}

func TestCodexTurnStateCollectorUsesCredentialAccountAndTLSFallback(t *testing.T) {
	parentID := int64(101)
	parent := &Account{
		ID:          parentID,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{
			"access_token":       "parent-token",
			"chatgpt_account_id": "parent-chatgpt-account",
		},
	}
	shadow := &Account{
		ID:              202,
		Platform:        PlatformOpenAI,
		Type:            AccountTypeOAuth,
		Status:          StatusActive,
		Schedulable:     true,
		Concurrency:     7,
		ParentAccountID: &parentID,
		// Deliberately keep different values here. A shadow must never use
		// its own credential snapshot for OAuth headers or token lookup.
		Credentials: map[string]any{
			"access_token":       "shadow-token-must-not-leak",
			"chatgpt_account_id": "shadow-account-must-not-leak",
		},
	}
	repo := &turnStateAutoRepo{accounts: map[int64]*Account{
		parent.ID: parent,
		shadow.ID: shadow,
	}}
	settings, _ := turnStateTestSettings("", "gpt-5")
	upstream := &codexTurnStateCollectorUpstream{}
	svc := &OpenAIGatewayService{
		accountRepo:         repo,
		settingService:      settings,
		httpUpstream:        upstream,
		tlsFPProfileService: &TLSFingerprintProfileService{},
	}

	state, err := svc.probeOpenAICodexTurnStateViaProxy(
		context.Background(),
		shadow,
		"gpt-5",
		"socks5://sticky.example:1080",
	)
	require.NoError(t, err)
	require.Equal(t, "parent-state", state)

	upstream.mu.Lock()
	doCalls := upstream.doCalls
	doTLSCalls := upstream.doTLSCalls
	proxies := append([]string(nil), upstream.proxies...)
	accountIDs := append([]int64(nil), upstream.accountIDs...)
	concurrencies := append([]int(nil), upstream.concurrencies...)
	requests := append([]*http.Request(nil), upstream.requests...)
	profiles := append([]*tlsfingerprint.Profile(nil), upstream.profiles...)
	upstream.mu.Unlock()

	require.Equal(t, 0, doCalls)
	require.Equal(t, 2, doTLSCalls)
	require.Equal(t, []string{"socks5://sticky.example:1080", "socks5://sticky.example:1080"}, proxies)
	require.Equal(t, []int64{shadow.ID, shadow.ID}, accountIDs)
	require.Equal(t, []int{shadow.Concurrency, shadow.Concurrency}, concurrencies)
	require.Len(t, requests, 2)
	require.Len(t, profiles, 2)
	for _, profile := range profiles {
		require.Nil(t, profile, "OpenAI accounts do not opt into the Anthropic TLS fingerprint profile")
	}

	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(requests[0].Context()))
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(requests[1].Context()))
	for _, request := range requests {
		require.Equal(t, "Bearer parent-token", request.Header.Get("Authorization"))
		require.Equal(t, "parent-chatgpt-account", request.Header.Get("Chatgpt-Account-Id"))
		require.NotContains(t, request.Header.Get("Authorization"), "shadow-token-must-not-leak")
		require.NotEqual(t, "shadow-account-must-not-leak", request.Header.Get("Chatgpt-Account-Id"))
		require.Equal(t, "chatgpt.com", request.Host)
	}
	require.Empty(t, requests[0].Header.Get(openAICodexTurnStateHeader))
	require.Equal(t, "parent-state", requests[1].Header.Get(openAICodexTurnStateHeader))
}
