package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildOpenAITestDefaultsUsesAccountTestPayloads(t *testing.T) {
	svc := (*AccountTestService)(nil) // builder is deliberately side-effect free

	responses := svc.BuildOpenAITestDefaults(nil, "responses", "")
	require.Equal(t, "responses", responses.Endpoint)
	require.Equal(t, "gpt-5.4", responses.Body["model"])
	require.Equal(t, true, responses.Body["stream"])
	require.NotEmpty(t, responses.Body["instructions"])
	require.Equal(t, "Bearer ••••••••", responses.Headers["Authorization"])

	chat := svc.BuildOpenAITestDefaults(nil, "chat/completions", "hello")
	require.Equal(t, "hello", chat.Body["messages"].([]map[string]any)[0]["content"])
	require.Equal(t, true, chat.Body["stream"])

	images := svc.BuildOpenAITestDefaults(nil, "images/generations", "")
	require.Equal(t, "gpt-image-2", images.Body["model"])
	require.Equal(t, 1, images.Body["n"])
	require.Equal(t, "b64_json", images.Body["response_format"])

	oauthImages := svc.BuildOpenAITestDefaults(&Account{Type: AccountTypeOAuth}, "images/generations", "")
	require.Equal(t, "gpt-image-2", oauthImages.UpstreamBody["model"])
	require.Equal(t, "https://chatgpt.com/backend-api/codex/images/generations", oauthImages.URL)
	require.Equal(t, "application/json", oauthImages.Headers["Accept"])
}

func TestBuildOpenAITestDefaultsOAuthAndAPIKeyRouting(t *testing.T) {
	svc := (*AccountTestService)(nil)
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"base_url": "https://ignored.example/?token=secret"}}
	oauthDefaults := svc.BuildOpenAITestDefaults(oauth, "responses", "")
	require.Equal(t, chatgptCodexAPIURL, oauthDefaults.URL)
	require.NotContains(t, oauthDefaults.URL, "secret")

	apiKey := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://upstream.example/v1?token=secret"}}
	apiDefaults := svc.BuildOpenAITestDefaults(apiKey, "responses", "")
	require.Equal(t, "https://upstream.example/v1/responses", apiDefaults.URL)
	require.NotContains(t, apiDefaults.URL, "secret")
}

func TestBuildOpenAITestDefaultsOAuthImageMappingUsesResponsesFallback(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-image-2": "gpt-image-1"},
		},
	}
	defaults := (*AccountTestService)(nil).BuildOpenAITestDefaults(account, "images/generations", "")
	require.Equal(t, "gpt-image-1", defaults.Body["model"])
	require.Equal(t, chatgptCodexAPIURL, defaults.URL)
	require.Equal(t, "text/event-stream", defaults.Headers["Accept"])
	require.Equal(t, true, defaults.UpstreamBody["stream"])
	tools := defaults.UpstreamBody["tools"].([]any)
	require.Len(t, tools, 1)
	require.Equal(t, "image_generation", tools[0].(map[string]any)["type"])
	require.Equal(t, "gpt-image-1", tools[0].(map[string]any)["model"])
}
