package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetiredPlatformsAreNotActive(t *testing.T) {
	for _, platform := range []string{
		PlatformGemini, PlatformAntigravity, PlatformGrok,
		PlatformKimi, PlatformZhipu, PlatformDeepseek,
	} {
		require.True(t, IsRetiredPlatform(platform), platform)
		require.False(t, IsActiveAccountPlatform(platform), platform)
		require.False(t, IsActiveGroupPlatform(platform), platform)
		require.ErrorIs(t, requireActiveAccountPlatform(platform), ErrPlatformRetired)
		require.ErrorIs(t, requireActiveGroupPlatform(platform), ErrPlatformRetired)
	}

	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
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
	for _, platform := range []string{PlatformGemini, PlatformAntigravity, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, "GLM"} {
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
