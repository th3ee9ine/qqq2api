package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai"
)

func TestAccountTestCodexHeadersPerRequestIDAndPreview(t *testing.T) {
	seen := make(map[string]bool)
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth, AccountTypeSetupToken} {
		for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
			t.Run(accountType+"/"+endpoint, func(t *testing.T) {
				account := &Account{Platform: PlatformOpenAI, Type: accountType}
				for i := 0; i < 2; i++ {
					req := httptest.NewRequest(http.MethodPost, "https://upstream.example/v1/responses", nil)
					req.Header.Set("X-Client-Request-ID", "stale-inbound-request-id")
					applyOpenAIAccountTestHeaders(req, account, endpoint, []byte(`{"model":"gpt-5.4","stream":true}`))
					value := req.Header.Get("X-Client-Request-ID")
					id, err := uuid.Parse(value)
					require.NoError(t, err)
					require.Equal(t, uuid.Version(4), id.Version())
					require.False(t, seen[value], "request correlation ID must not be reused")
					seen[value] = true
				}
				defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, endpoint, "")
				require.Equal(t, openAITestDynamicHeader, defaults.Headers["X-Client-Request-Id"])
			})
		}
	}
}

func TestAccountTestCodexHeadersAcceptMatchesProtocol(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/"+endpoint, nil)
			applyOpenAIAccountTestHeaders(req, account, endpoint, []byte(`{"model":"gpt-5.4","stream":true}`))
			if endpoint == "images/generations" {
				require.Empty(t, req.Header.Get("Accept"), "ordinary image JSON responses must not advertise SSE")
			} else {
				require.Equal(t, "text/event-stream", req.Header.Get("Accept"))
			}
			require.Empty(t, req.Header.Get("X-Codex-Beta-Features"))
			require.Empty(t, req.Header.Get("X-Codex-Routing-Hint"))
			if endpoint != "responses" {
				for _, name := range []string{"Originator", "Version", "X-Codex-Window-ID"} {
					require.Empty(t, req.Header.Get(name), "do not inject %s on non-Codex endpoints", name)
				}
			}
		})
	}
}

func TestAccountTestCodexHeadersBetaPreservesExplicitFeatures(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		for _, tc := range []struct{ name, initial, want string }{
			{name: "default", want: openAIRemoteCompactionV2Feature},
			{name: "blank", initial: "  ", want: openAIRemoteCompactionV2Feature},
			{name: "explicit-other", initial: "custom_feature", want: "custom_feature"},
			{name: "explicit-list", initial: "remote_compaction_v2,custom_feature", want: "remote_compaction_v2,custom_feature"},
		} {
			t.Run(accountType+"/"+tc.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, chatgptCodexAPIURL, nil)
				if tc.initial != "" {
					req.Header.Set("X-Codex-Beta-Features", tc.initial)
				}
				applyOpenAIAccountTestHeaders(req, &Account{Platform: PlatformOpenAI, Type: accountType}, "responses", []byte(`{"model":"gpt-5.4"}`))
				require.Equal(t, tc.want, req.Header.Get("X-Codex-Beta-Features"))
			})
		}
	}
}

func TestAccountTestCodexHeadersRoutingUsesFinalBody(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	for _, tc := range []struct{ name, body, want string }{
		{name: "model", body: `{"model":"mapped-upstream-model"}`, want: "model=mapped-upstream-model"},
		{name: "priority", body: `{"model":"mapped-upstream-model","service_tier":"priority"}`, want: "model=mapped-upstream-model;tier=priority"},
		{name: "fast-alias", body: `{"model":"mapped-upstream-model","service_tier":"fast"}`, want: "model=mapped-upstream-model;tier=priority"},
		{name: "flex", body: `{"model":"mapped-upstream-model","service_tier":"flex"}`, want: "model=mapped-upstream-model;tier=flex"},
		{name: "ultrafast", body: `{"model":"mapped-upstream-model","service_tier":"ultrafast"}`, want: "model=mapped-upstream-model;tier=ultrafast"},
		{name: "default", body: `{"model":"mapped-upstream-model","service_tier":"default"}`, want: "model=mapped-upstream-model"},
		{name: "auto", body: `{"model":"mapped-upstream-model","service_tier":"auto"}`, want: "model=mapped-upstream-model"},
		{name: "no-model", body: `{}`},
		{name: "model-delimiter", body: `{"model":"model;tier=priority"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, chatgptCodexAPIURL, nil)
			req.Header.Set("X-Codex-Routing-Hint", "model=spoofed;tier=priority")
			applyOpenAIAccountTestHeaders(req, account, "responses", []byte(tc.body))
			require.Equal(t, tc.want, req.Header.Get("X-Codex-Routing-Hint"))
		})
	}
}

func TestAccountTestCodexHeadersAPIKeyRoutingOverrideRemoved(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides: map[string]any{
			"x-codex-routing-hint":  "model=static-spoof",
			"X-Codex-Beta-Features": "explicit-compatible-feature",
		},
	}}
	for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
		t.Run(endpoint, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/"+endpoint, nil)
			req.Header["x-Codex-routing-HINT"] = []string{"model=raw-case-spoof"}
			applyOpenAIAccountTestHeaders(req, account, endpoint, []byte(`{"model":"gpt-5.4"}`))
			for name := range req.Header {
				require.False(t, strings.EqualFold(name, "X-Codex-Routing-Hint"))
			}
			require.Equal(t, []string{"explicit-compatible-feature"}, openAIHeaderValuesEqualFold(req.Header, "X-Codex-Beta-Features"), "API Key explicit compatible headers remain configurable with their wire casing")
			defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, endpoint, "")
			require.NotContains(t, defaults.Headers, "X-Codex-Routing-Hint")
			require.Equal(t, openAITestRedactedHeader, defaults.Headers["X-Codex-Beta-Features"])
		})
	}
}

func TestAccountTestCodexHeadersImageRoutingUsesResponsesCarrier(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, "images/generations", "")
	require.Equal(t, "gpt-image-2", defaults.Body["model"])
	require.NotNil(t, defaults.UpstreamBody)
	carrier, ok := defaults.UpstreamBody["model"].(string)
	require.True(t, ok)
	require.NotEmpty(t, carrier)
	require.NotEqual(t, defaults.Body["model"], carrier)
	require.Equal(t, "model="+carrier, defaults.Headers["X-Codex-Routing-Hint"])
	require.Equal(t, openAIRemoteCompactionV2Feature, defaults.Headers["X-Codex-Beta-Features"])
	encoded, err := json.Marshal(defaults.UpstreamBody)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, chatgptCodexAPIURL, nil)
	applyOpenAIAccountTestHeaders(req, account, "responses", encoded)
	require.Equal(t, defaults.Headers["X-Codex-Routing-Hint"], req.Header.Get("X-Codex-Routing-Hint"))
}

func TestAccountTestCodexHeadersDetailsMatchWireAndRedactCredentials(t *testing.T) {
	for _, accountType := range []string{AccountTypeAPIKey, AccountTypeOAuth} {
		account := &Account{Platform: PlatformOpenAI, Type: accountType, Credentials: map[string]any{
			"access_token": "canary-private-token", "api_key": "canary-private-api-key", "chatgpt_account_id": "canary-private-account",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"X-Partner-Credential": "canary-private-partner",
				"OpenAI-Organization":  "canary-private-org",
				"OpenAI-Project":       "canary-private-project",
			},
		}}
		for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
			t.Run(accountType+"/"+endpoint, func(t *testing.T) {
				defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, endpoint, "")
				included := make(map[string]bool, len(defaults.Headers))
				for name := range defaults.Headers {
					included[strings.ToLower(name)] = true
				}
				described := make(map[string]OpenAITestHeaderDetail)
				for _, detail := range defaults.HeaderDetails {
					name := strings.ToLower(detail.Name)
					_, duplicate := described[name]
					require.False(t, duplicate, "duplicate documentation entry for %s", detail.Name)
					described[name] = detail
					require.Equal(t, included[name], detail.DefaultIncluded, "documentation inclusion mismatch for %s", detail.Name)
					require.NotEmpty(t, detail.Purpose)
					require.NotEmpty(t, detail.Requirement)
					require.NotEmpty(t, detail.Condition)
					require.NotEmpty(t, detail.Source)
				}
				for name := range included {
					require.Contains(t, described, name, "every emitted header must be documented")
				}
				for _, name := range []string{"session_id", "conversation_id", "x-codex-turn-state", "x-codex-turn-metadata", "x-codex-installation-id", "session-id", "thread-id", "turn-id", "openai-beta", "x-openai-internal-codex-residency", "x-responsesapi-include-timing-metrics", "x-openai-subagent", "x-openai-memgen-request", "content-length", "accept-encoding", "connection", "sec-websocket-key"} {
					require.Contains(t, described, name)
					require.False(t, included[name], "conditional/transport header must not become a fixed default: %s", name)
				}
				encoded, err := json.Marshal(defaults)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "canary-private-")
				if accountType == AccountTypeAPIKey {
					for _, name := range []string{"X-Partner-Credential", "Openai-Organization", "Openai-Project"} {
						require.Equal(t, openAITestRedactedHeader, defaults.Headers[name])
					}
				} else {
					require.False(t, included["openai-organization"])
					require.False(t, included["openai-project"])
				}
			})
		}
	}
}

func TestAccountTestCodexHeadersShadowRoutingUsesChildModel(t *testing.T) {
	parent := &Account{ID: 111, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"access_token": "canary-parent-token", "chatgpt_account_id": "canary-parent-account",
		"model_mapping": map[string]any{openai.DefaultTestModel: "parent-only-model"},
	}}
	child := &Account{ID: 222, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parent.ID,
		Credentials: map[string]any{"model_mapping": map[string]any{openai.DefaultTestModel: "child-only-model"}},
	}
	svc := &AccountTestService{accountRepo: newStubCredRepo(parent)}
	for _, endpoint := range []string{"responses", "chat/completions"} {
		t.Run(endpoint, func(t *testing.T) {
			defaults, err := svc.BuildOpenAITestDefaultsForAccount(context.Background(), child, endpoint, "")
			require.NoError(t, err)
			body := defaults.Body
			if defaults.UpstreamBody != nil {
				body = defaults.UpstreamBody
			}
			require.Equal(t, "child-only-model", body["model"])
			require.Equal(t, "model=child-only-model", defaults.Headers["X-Codex-Routing-Hint"])
			require.Equal(t, openAITestRedactedHeader, defaults.Headers["Chatgpt-Account-Id"])
			parentDefaults := svc.BuildOpenAITestDefaults(parent, endpoint, "")
			require.NotEqual(t, parentDefaults.Headers["X-Codex-Routing-Hint"], defaults.Headers["X-Codex-Routing-Hint"])
			encoded, err := json.Marshal(defaults)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "canary-parent-")
		})
	}
}
