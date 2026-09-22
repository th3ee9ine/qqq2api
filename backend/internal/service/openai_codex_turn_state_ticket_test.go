package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
)

type codexTicketCaptureUpstream struct {
	request *http.Request
	proxy   string
	body    []byte
}

func (u *codexTicketCaptureUpstream) Do(req *http.Request, proxyURL string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	u.proxy = proxyURL
	var err error
	u.body, err = io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

func (u *codexTicketCaptureUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, concurrency)
}

func codexTicketTestSnapshot(now time.Time) OpenAICodexTurnStateSnapshot {
	return OpenAICodexTurnStateSnapshot{
		Token:            OpenAICodexTurnStateToken{Value: "ticket-state"},
		Route:            "probe",
		EgressPinned:     true,
		EgressProxyURL:   "http://ticket-proxy.example:8080",
		HarvestSessionID: "ticket-session",
		HarvestCookies: []OpenAICodexTurnStateCookie{
			{Name: "__cf_bm", Value: "ticket-cookie"},
			{Name: "clearance", Value: "ticket-clearance"},
		},
		HarvestCookiesAt: now,
	}
}

func TestCodexTurnStateTicketCookieWindowIsExclusiveAt240Seconds(t *testing.T) {
	now := time.Date(2026, time.September, 22, 12, 0, 0, 0, time.UTC)
	snapshot := codexTicketTestSnapshot(now)
	for _, tc := range []struct {
		name       string
		elapsed    time.Duration
		wantCookie string
	}{
		{name: "239 seconds", elapsed: 239 * time.Second, wantCookie: "__cf_bm=ticket-cookie; clearance=ticket-clearance"},
		{name: "240 seconds", elapsed: 240 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("Cookie", "unrelated=foreign")
			headers.Set("conversation_id", "foreign-conversation")
			headers.Set("session-id", "foreign-session")
			applyOpenAICodexTurnStateTicketHeaders(headers, snapshot, now.Add(tc.elapsed))

			require.Equal(t, "ticket-session", headers.Get("session_id"))
			require.Empty(t, headers.Get("session-id"))
			require.Empty(t, headers.Get("conversation_id"))
			require.Equal(t, tc.wantCookie, headers.Get("Cookie"))
		})
	}
}

func TestCodexTurnStateTicketPinsHTTPIdentityBodyAndEgress(t *testing.T) {
	upstream := &codexTicketCaptureUpstream{}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 1201, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1}
	body := []byte(`{"model":"gpt-5.5","input":"keep this","prompt_cache_key":"foreign-cache","client_metadata":{"session":"foreign"},"device_id":"foreign-device"}`)
	req := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", bytes.NewReader(body))
	req.Header.Set("Cookie", "foreign=stale")
	req.Header.Set("session_id", "foreign-session")
	req.Header.Set("conversation_id", "foreign-conversation")
	req.Header.Set("x-codex-installation-id", "foreign-installation")
	req.Header.Set("x-codex-turn-metadata", "foreign-metadata")
	snapshot := codexTicketTestSnapshot(time.Now())
	req = bindOpenAICodexTurnStateTicketRequest(req, snapshot)
	snapshot.HarvestCookies[0].Value = "mutated-after-binding"

	response, err := svc.doOpenAIUpstream(req, "http://unrelated-proxy.example:8080", account)
	require.NoError(t, err)
	require.NotNil(t, response)
	defer response.Body.Close()
	require.Equal(t, "http://ticket-proxy.example:8080", upstream.proxy)
	require.Equal(t, "ticket-session", upstream.request.Header.Get("session_id"))
	require.Equal(t, "__cf_bm=ticket-cookie; clearance=ticket-clearance", upstream.request.Header.Get("Cookie"))
	for _, header := range []string{"conversation_id", "x-codex-installation-id", "x-codex-turn-metadata"} {
		require.Empty(t, upstream.request.Header.Get(header), header)
	}
	var dispatched map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(upstream.body, &dispatched))
	require.JSONEq(t, `"gpt-5.5"`, string(dispatched["model"]))
	require.JSONEq(t, `"keep this"`, string(dispatched["input"]))
	for _, key := range []string{"prompt_cache_key", "client_metadata", "device_id"} {
		require.NotContains(t, dispatched, key)
	}
	require.Equal(t, int64(len(upstream.body)), upstream.request.ContentLength)
}

func TestCodexTurnStateTicketPinsWSIdentityOnlyForMatchingBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 1202}
	snapshot := codexTicketTestSnapshot(time.Now())
	svc.bindCodexTurnStateRequest(c, OpenAICodexTurnStateKey{AccountID: account.ID, Model: "gpt-5.5"}, "gpt-5.5", snapshot, true)

	newHeaders := func() http.Header {
		headers := make(http.Header)
		headers.Set("Cookie", "foreign=stale")
		headers.Set("session_id", "foreign-session")
		headers.Set("conversation_id", "foreign-conversation")
		return headers
	}
	headers := newHeaders()
	proxy := svc.pinOpenAICodexTurnStateWSIdentity(c, account, "gpt-5.5", headers, "http://default.example:8080")
	require.Equal(t, snapshot.EgressProxyURL, proxy)
	require.Equal(t, snapshot.HarvestSessionID, headers.Get("session_id"))
	require.Equal(t, "__cf_bm=ticket-cookie; clearance=ticket-clearance", headers.Get("Cookie"))
	require.Empty(t, headers.Get("conversation_id"))

	for _, tc := range []struct {
		name    string
		account *Account
		model   string
	}{
		{name: "different account", account: &Account{ID: account.ID + 1}, model: "gpt-5.5"},
		{name: "different model", account: account, model: "gpt-6-astra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			unmodified := newHeaders()
			proxy := svc.pinOpenAICodexTurnStateWSIdentity(c, tc.account, tc.model, unmodified, "http://default.example:8080")
			require.Equal(t, "http://default.example:8080", proxy)
			require.Equal(t, "foreign-session", unmodified.Get("session_id"))
			require.Equal(t, "foreign=stale", unmodified.Get("Cookie"))
		})
	}

	expired := snapshot
	expired.HarvestCookiesAt = time.Now().Add(-openAICodexTurnStateCookieTTL - time.Second)
	svc.bindCodexTurnStateRequest(c, OpenAICodexTurnStateKey{AccountID: account.ID, Model: "gpt-5.5"}, "gpt-5.5", expired, true)
	headers = newHeaders()
	proxy = svc.pinOpenAICodexTurnStateWSIdentity(c, account, "gpt-5.5", headers, "http://default.example:8080")
	require.Equal(t, snapshot.EgressProxyURL, proxy)
	require.Equal(t, snapshot.HarvestSessionID, headers.Get("session_id"))
	require.Empty(t, headers.Get("Cookie"))
}

var _ HTTPUpstream = (*codexTicketCaptureUpstream)(nil)
