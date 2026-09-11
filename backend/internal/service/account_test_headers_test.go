package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai_compat"
)

func TestOpenAITestHeaderDefaultsOAuthIdentityAndConditionalHeaders(t *testing.T) {
	previousEnforcement := codexIdentityEnforcement.Load()
	previousLocal := codexAccountLocalDeviceIdentityEnabled.Load()
	SetCodexIdentityEnforcementEnabled(true)
	SetCodexAccountLocalDeviceIdentityEnabled(true)
	t.Cleanup(func() {
		SetCodexIdentityEnforcementEnabled(previousEnforcement)
		SetCodexAccountLocalDeviceIdentityEnabled(previousLocal)
	})
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":               "private-access-token",
			"chatgpt_account_id":         "private-account-id",
			"chatgpt_account_is_fedramp": true,
		},
		Extra: map[string]any{
			OpenAILocalDeviceUserAgentExtraKey:  "codex_vscode/0.125.0 (Mac OS X 14.0; arm64) vscode",
			OpenAILocalDeviceOriginatorExtraKey: "codex_vscode",
			OpenAILocalDeviceVersionExtraKey:    "0.125.0",
		},
	}
	for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
		t.Run(endpoint, func(t *testing.T) {
			defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, endpoint, "")
			h := defaults.Headers
			require.Equal(t, "chatgpt.com", h["Host"])
			require.Equal(t, "application/json", h["Content-Type"])
			require.Equal(t, "text/event-stream", h["Accept"])
			require.Equal(t, "codex_vscode", h["Originator"])
			require.Equal(t, "0.125.0", h["Version"])
			require.Contains(t, h["User-Agent"], "codex_vscode/0.125.0")
			require.Equal(t, "true", h["X-Openai-Fedramp"])
			require.Equal(t, openAITestRedactedHeader, h["Chatgpt-Account-Id"])
			for _, absent := range []string{"X-Accel-Buffering", "Cache-Control", "Connection", "Openai-Beta", "Session_id", "Conversation_id", "X-Codex-Turn-State", "Content-Length"} {
				require.NotContains(t, h, absent)
			}
			encoded, err := json.Marshal(defaults)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private-access-token")
			require.NotContains(t, string(encoded), "private-account-id")
		})
	}
	withoutAccountID := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	h := (*AccountTestService)(nil).BuildOpenAITestDefaults(withoutAccountID, "responses", "").Headers
	require.NotContains(t, h, "Chatgpt-Account-Id")
	require.NotContains(t, h, "X-Openai-Fedramp")
}

func TestOpenAITestHeaderDefaultsAPIKeyOverridesAreFilteredAndRedacted(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url":                   "https://user:private-password@upstream.example:8443/v1?token=private-query",
			"api_key":                    "private-api-key",
			credKeyHeaderOverrideEnabled: true,
			credKeyHeaderOverrides: map[string]any{
				"X-Partner-Access": "private-partner-key",
				"x-sUb2aPi-debug":  "private-internal-value",
				"X-Client-Name":    "qqq2api-debug",
				"Authorization":    "private-forbidden-authorization",
				"Host":             "private-forbidden-host",
				"X-Empty":          "",
				"bad\r\nname":      "private-invalid-value",
			},
		},
	}
	for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
		t.Run(endpoint, func(t *testing.T) {
			defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, endpoint, "")
			h := defaults.Headers
			require.Equal(t, "upstream.example:8443", h["Host"])
			require.Equal(t, "Bearer "+openAITestRedactedHeader, h["Authorization"])
			require.Equal(t, openAITestRedactedHeader, h["X-Partner-Access"])
			require.NotContains(t, h, "X-Client-Name")
			require.NotContains(t, h, "X-Empty")
			if endpoint == "responses" {
				require.NotEmpty(t, h["Originator"])
				require.NotEmpty(t, h["Version"])
				require.Equal(t, openAITestDynamicHeader, h["X-Codex-Window-Id"])
			} else {
				require.NotContains(t, h, "Originator")
				require.NotContains(t, h, "X-Codex-Window-Id")
			}
			if endpoint == "chat/completions" || endpoint == "responses" {
				require.Equal(t, "text/event-stream", h["Accept"])
			} else {
				require.NotContains(t, h, "Accept")
			}
			encoded, err := json.Marshal(defaults)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private-")
		})
	}
}

func TestOpenAITestHeaderDefaultsAgentIdentityDoesNotGenerateAssertion(t *testing.T) {
	// A missing task ID and invalid private key would fail or attempt task
	// registration if the real authentication builder were invoked here.
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
		"auth_mode":         OpenAIAuthModeAgentIdentity,
		"agent_private_key": "private-invalid-key",
		"agent_runtime_id":  "private-runtime-id",
	}}
	for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
		defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, endpoint, "")
		require.Equal(t, "AgentAssertion "+openAITestDynamicHeader, defaults.Headers["Authorization"])
		encoded, err := json.Marshal(defaults)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-")
	}
}

func TestOpenAITestHeaderDefaultsMatchLiveRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
			t.Run(accountType+"/"+endpoint, func(t *testing.T) {
				account := &Account{ID: 5, Platform: PlatformOpenAI, Type: accountType, Credentials: map[string]any{
					"access_token": "private-oauth", "api_key": "private-api-key", "chatgpt_account_id": "private-account",
				}, Extra: map[string]any{openai_compat.ExtraKeyResponsesSupported: true}}
				body := "data: {\"type\":\"response.completed\"}\n\n"
				if endpoint == "images/generations" {
					if accountType == AccountTypeOAuth {
						body = "data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_1\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"output_format\":\"png\"}}\n\ndata: [DONE]\n\n"
					} else {
						body = `{"data":[{"b64_json":"aGVsbG8="}]}`
					}
				} else if endpoint == "chat/completions" && accountType == AccountTypeAPIKey {
					body = "data: [DONE]\n\n"
				}
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}}
				svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/5/test", nil)
				var err error
				switch {
				case endpoint == "images/generations" && accountType == AccountTypeOAuth:
					err = svc.testOpenAIImageOAuth(c, c.Request.Context(), account, "gpt-image-2", "draw a cat")
				case endpoint == "images/generations":
					err = svc.testOpenAIImageAPIKey(c, c.Request.Context(), account, "gpt-image-2", "draw a cat")
				case endpoint == "chat/completions" && accountType == AccountTypeAPIKey:
					err = svc.testOpenAIChatCompletionsConnection(c, account, "gpt-5.4", "hi", "https://api.openai.com", "private-api-key")
				default:
					err = svc.testOpenAIAccountConnection(c, account, "gpt-5.4", "hi", "")
				}
				require.NoError(t, err)
				require.NotNil(t, upstream.lastReq)
				actual := make(map[string]string)
				for name, values := range upstream.lastReq.Header {
					value := strings.Join(values, ", ")
					switch strings.ToLower(name) {
					case "authorization":
						value = "Bearer " + openAITestRedactedHeader
					case "chatgpt-account-id":
						value = openAITestRedactedHeader
					case "x-codex-window-id", "x-client-request-id":
						value = openAITestDynamicHeader
					}
					actual[http.CanonicalHeaderKey(name)] = value
				}
				actual["Host"] = upstream.lastReq.Host
				defaults := svc.BuildOpenAITestDefaults(account, endpoint, "")
				require.Equal(t, actual, defaults.Headers)
			})
		}
	}
}
