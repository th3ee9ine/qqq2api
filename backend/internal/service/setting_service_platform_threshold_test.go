//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func newSettingServiceForPlatformThresholdTest(seed map[string]string) *SettingService {
	accountSchedulingThresholdsSF.Forget(SettingKeyAccountSchedulingThresholds)
	accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})
	repo := newMockSettingRepo()
	for k, v := range seed {
		repo.data[k] = v
	}
	return NewSettingService(repo, &config.Config{})
}

func TestPlatformSchedulingThresholds_RoundTrip_DefaultsAndStoredValues(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	got := svc.parseSettings(map[string]string{})
	require.Equal(t, map[string]int{
		PlatformOpenAI:    100,
		PlatformAnthropic: 100,
	}, got.AccountSchedulingThresholds)

	got = svc.parseSettings(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":91,"grok":77,"gemini":85,"kiro":99}`,
	})
	require.Equal(t, 91, got.AccountSchedulingThresholds[PlatformOpenAI])
	require.Equal(t, 100, got.AccountSchedulingThresholds[PlatformAnthropic])
	require.NotContains(t, got.AccountSchedulingThresholds, PlatformGrok)
	require.NotContains(t, got.AccountSchedulingThresholds, PlatformGemini)
	require.NotContains(t, got.AccountSchedulingThresholds, "kiro")
}

func TestBuildSystemSettingsUpdates_PersistsAccountSchedulingThresholds(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	updates, err := svc.buildSystemSettingsUpdates(context.Background(), &SystemSettings{
		AccountSchedulingThresholds: map[string]int{
			PlatformOpenAI:    91,
			PlatformAnthropic: 88,
		},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"openai":91,"anthropic":88}`, updates[SettingKeyAccountSchedulingThresholds])
}

func TestValidateAndNormalizeAccountSchedulingThresholds_FillsMissingPlatforms(t *testing.T) {
	normalized, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{
		PlatformOpenAI: 91,
	})
	require.NoError(t, err)
	require.Equal(t, 91, normalized[PlatformOpenAI])
	require.Equal(t, 100, normalized[PlatformAnthropic])
	require.NotContains(t, normalized, PlatformGrok)
	require.NotContains(t, normalized, PlatformGemini)
	require.NotContains(t, normalized, "kiro")
	require.NotContains(t, normalized, PlatformAntigravity)
}

func TestValidateAndNormalizeAccountSchedulingThresholds_RejectsUnsupportedPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformGemini, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek} {
		_, err := validateAndNormalizeAccountSchedulingThresholds(map[string]int{platform: 85})
		require.Error(t, err, "platform=%s", platform)
	}
}

func TestUpdateSettings_StoresAccountSchedulingThresholds(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		AccountSchedulingThresholds: map[string]int{
			PlatformOpenAI:    92,
			PlatformAnthropic: 89,
		},
	})
	require.NoError(t, err)

	got := svc.parseSettings(map[string]string{
		SettingKeyAccountSchedulingThresholds: svc.settingRepo.(*mockSettingRepo).data[SettingKeyAccountSchedulingThresholds],
	})
	require.Equal(t, 92, got.AccountSchedulingThresholds[PlatformOpenAI])
	require.Equal(t, 89, got.AccountSchedulingThresholds[PlatformAnthropic])
	require.NotContains(t, got.AccountSchedulingThresholds, PlatformGrok)
	require.NotContains(t, got.AccountSchedulingThresholds, "kiro")
}

func TestGetAccountSchedulingThresholds_ReadsStoredValue(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":93,"grok":88,"kiro":87}`,
	})

	got := svc.GetAccountSchedulingThresholds(context.Background())

	require.Equal(t, 93, got[PlatformOpenAI])
	require.Equal(t, 100, got[PlatformAnthropic])
	require.NotContains(t, got, PlatformGrok)
	require.NotContains(t, got, "kiro")
}

func TestGetAccountSchedulingThresholds_MissingSettingUsesDefaultsAndNormalCacheTTL(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(nil)
	repo := svc.settingRepo.(*mockSettingRepo)
	repo.getValueErr = ErrSettingNotFound

	got := svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, defaultAccountSchedulingThresholds(), got)
	require.Equal(t, 1, repo.getValueCalls)

	repo.data[SettingKeyAccountSchedulingThresholds] = `{"openai":91}`
	got = svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, 100, got[PlatformOpenAI], "missing-setting defaults should remain cached for the normal TTL")
	require.Equal(t, 1, repo.getValueCalls)

	cached, ok := accountSchedulingThresholdsCache.Load().(*cachedAccountSchedulingThresholds)
	require.True(t, ok)
	require.Greater(t, cached.expiresAt, time.Now().Add(accountSchedulingThresholdsCacheTTL-time.Second).UnixNano())
}

func TestUpdateSettings_OmittedAccountSchedulingThresholdsDoesNotCacheDefaults(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":85,"grok":88,"kiro":87}`,
	})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		FrontendURL: "https://example.test",
	})
	require.NoError(t, err)

	got := svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, 85, got[PlatformOpenAI])
	require.NotContains(t, got, PlatformGrok)
	require.NotContains(t, got, "kiro")
}

func TestAccountSchedulingThresholds_InvalidStoredValueUsesSameDefaultsInSettingsAndCache(t *testing.T) {
	svc := newSettingServiceForPlatformThresholdTest(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":0,"grok":88,"kiro":87}`,
	})

	settings := svc.parseSettings(map[string]string{
		SettingKeyAccountSchedulingThresholds: `{"openai":0,"grok":88,"kiro":87}`,
	})
	cached := svc.GetAccountSchedulingThresholds(context.Background())

	require.Equal(t, settings.AccountSchedulingThresholds, cached)
	require.Equal(t, 100, cached[PlatformOpenAI])
	require.NotContains(t, cached, PlatformGrok)
	require.NotContains(t, cached, "kiro")
}

func TestGetAccountSchedulingThresholds_NilRepoReturnsDefaults(t *testing.T) {
	svc := &SettingService{}
	got := svc.GetAccountSchedulingThresholds(context.Background())
	require.Equal(t, map[string]int{
		PlatformOpenAI:    100,
		PlatformAnthropic: 100,
	}, got)
}
