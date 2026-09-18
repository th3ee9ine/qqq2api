package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func TestSystemSettingsCodexTurnStateDefaults(t *testing.T) {
	svc := NewSettingService(&codexHeaderSettingRepoStub{values: map[string]string{}}, &config.Config{})
	settings, err := svc.GetAllSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.OpenAICodexTurnStateAutoEnabled)
	require.Empty(t, settings.OpenAICodexTurnStateModels)
	require.Equal(t, "gpt-5.5", settings.OpenAICodexTurnStateDefaultModel)
}

func TestSystemSettingsCodexTurnStateHeaderValidation(t *testing.T) {
	for _, value := range []string{"", "opaque-not-fernet", " state ", strings.Repeat("a", 4096)} {
		require.NoError(t, ValidateOpenAICodexTurnState(value))
	}
	for _, value := range []string{"token\x00suffix", "token\tpart", "token\r\nInjected: yes", "token令牌", strings.Repeat("a", 4097)} {
		require.Error(t, ValidateOpenAICodexTurnState(value))
	}
}

func TestSystemSettingsCodexTurnStateModelsNormalize(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"", ""},
		{"GPT-5.4,gpt-5.4\n GPT-5.*", "gpt-5.4,gpt-5.*"},
		{"*", "*"},
		{" gpt-5.4 , , gpt-5.5\r\n", "gpt-5.4,gpt-5.5"},
	} {
		got, err := NormalizeOpenAICodexTurnStateModels(tc.raw)
		require.NoError(t, err)
		require.Equal(t, tc.want, got)
	}
	for _, value := range []string{"gpt *", "gpt*bad", "gpt**", "gpt\x00bad", "gpt\tbad", "gpt-令牌", strings.Repeat("a", 1025)} {
		_, err := NormalizeOpenAICodexTurnStateModels(value)
		require.Error(t, err, value)
	}
}

func TestSystemSettingsCodexTurnStateOmittedPreservesScopeAndIgnoresLegacy(t *testing.T) {
	// UpdateSettings refreshes unrelated package-wide gateway caches too. Keep
	// this persistence test from disabling local Codex identities (or changing
	// scheduler defaults) for tests that execute afterwards.
	identityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	versionBounds, _ := versionBoundsCache.Load().(*cachedVersionBounds)
	backendMode, _ := backendModeCache.Load().(*cachedBackendMode)
	forwarding, _ := gatewayForwardingCache.Load().(*cachedGatewayForwardingSettings)
	scheduler, _ := openAIAdvancedSchedulerSettingCache.Load().(*cachedOpenAIAdvancedSchedulerSetting)
	thresholds, _ := accountSchedulingThresholdsCache.Load().(*cachedAccountSchedulingThresholds)
	t.Cleanup(func() {
		versionBoundsCache.Store(versionBounds)
		backendModeCache.Store(backendMode)
		gatewayForwardingCache.Store(forwarding)
		openAIAdvancedSchedulerSettingCache.Store(scheduler)
		accountSchedulingThresholdsCache.Store(thresholds)
		SetCodexAccountLocalDeviceIdentityEnabled(identityEnabled)
	})

	repo := &codexHeaderSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTurnState:        "current-private-state",
		SettingKeyOpenAICodexTurnStateEnabled: "true",
		SettingKeyOpenAICodexTurnStateModels:  "gpt-5.*",
		SettingKeyOpenAICodexTurnStateSetAtMS: "12345",
	}}
	svc := NewSettingService(repo, &config.Config{})
	err := svc.UpdateSettingsOmitting(context.Background(), &SystemSettings{}, OmittedSettingKeys{
		SettingKeyOpenAICodexTurnState: {}, SettingKeyOpenAICodexTurnStateEnabled: {}, SettingKeyOpenAICodexTurnStateModels: {},
	})
	require.NoError(t, err)
	require.Equal(t, "current-private-state", repo.values[SettingKeyOpenAICodexTurnState])
	require.Equal(t, "true", repo.values[SettingKeyOpenAICodexTurnStateEnabled])
	require.Equal(t, "gpt-5.*", repo.values[SettingKeyOpenAICodexTurnStateModels])
	require.Equal(t, "12345", repo.values[SettingKeyOpenAICodexTurnStateSetAtMS])
}
