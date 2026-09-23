package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemovedProviderHasNoBuiltinCapabilities(t *testing.T) {
	account := &Account{Platform: "deepseek", Type: AccountTypeAPIKey}
	require.False(t, account.IsCNProvider())
	require.False(t, account.SupportsNativeCNResponses())
	require.Empty(t, account.GetOpenAIBaseURL())
	require.Empty(t, account.GetAnthropicProtocolBaseURL())
	require.Empty(t, cnBalanceURL(account))

	billing := NewBillingService(nil, nil)
	for _, model := range []string{"deepseek-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp"} {
		_, detected := DetectModelPlatform(model)
		require.False(t, detected, model)
		require.False(t, isOpenAIOAuthServableModel(model), model)
		require.Equal(t, ThinkingProtocolUnknown, ResolveThinkingProtocol(model), model)
		_, err := billing.GetModelPricing(model)
		require.ErrorIs(t, err, ErrModelPricingUnavailable, model)
	}
}

func TestRetiredPlatformsAreNotActive(t *testing.T) {
	for _, platform := range []string{
		PlatformGemini, PlatformAntigravity,
		PlatformKimi, PlatformZhipu, "deepseek",
	} {
		require.True(t, IsRetiredPlatform(platform), platform)
		require.False(t, IsActiveAccountPlatform(platform), platform)
		require.False(t, IsActiveGroupPlatform(platform), platform)
		require.ErrorIs(t, requireActiveAccountPlatform(platform), ErrPlatformRetired)
		require.ErrorIs(t, requireActiveGroupPlatform(platform), ErrPlatformRetired)
	}

	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGrok, " GROK "} {
		require.False(t, IsRetiredPlatform(platform), platform)
		require.True(t, IsActiveAccountPlatform(platform), platform)
		require.True(t, IsActiveGroupPlatform(platform), platform)
		require.NoError(t, requireActiveAccountPlatform(platform))
	}
	require.True(t, IsActiveGroupPlatform(PlatformComposite))
	require.False(t, IsActiveAccountPlatform(PlatformComposite))

	for _, alias := range []string{"GLM", " glm ", " Gemini ", "DEEPSEEK"} {
		require.True(t, IsRetiredPlatform(alias), alias)
		require.False(t, IsActiveAccountPlatform(alias), alias)
		require.False(t, IsActiveGroupPlatform(alias), alias)
		require.ErrorIs(t, requireActiveAccountPlatform(alias), ErrPlatformRetired)
	}
}

func TestGatewayAccessTokenRejectsRetiredPlatforms(t *testing.T) {
	svc := &GatewayService{}
	for _, platform := range []string{PlatformGemini, PlatformAntigravity, PlatformKimi, PlatformZhipu, "deepseek", "GLM"} {
		token, tokenType, err := svc.GetAccessToken(context.Background(), &Account{
			Platform:    platform,
			Type:        AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "must-not-leak"},
		})
		require.Empty(t, token, "platform=%s", platform)
		require.Empty(t, tokenType, "platform=%s", platform)
		require.ErrorIs(t, err, ErrPlatformRetired, "platform=%s", platform)
	}
}
