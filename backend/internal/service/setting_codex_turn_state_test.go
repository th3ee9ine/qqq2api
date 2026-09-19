package service

import (
	"context"
	"strconv"
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
	require.NotNil(t, settings.OpenAICodexTurnStateProxyIDs)
	require.Empty(t, settings.OpenAICodexTurnStateProxyIDs)
	require.Zero(t, settings.OpenAICodexTurnStateProxyID)
	require.True(t, settings.OpenAICodexTurnStateProxyIDsValid)
}

func preserveCodexTurnStateSettingGlobalCaches(t *testing.T) {
	t.Helper()
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
}

func TestSystemSettingsCodexTurnStateProxyIDsNormalize(t *testing.T) {
	got, err := NormalizeOpenAICodexTurnStateProxyIDs([]int64{9, 3, 9, 5, 3})
	require.NoError(t, err)
	require.Equal(t, []int64{9, 3, 5}, got)

	for _, ids := range [][]int64{{0}, {-1}, {1, 0, 2}} {
		_, err := NormalizeOpenAICodexTurnStateProxyIDs(ids)
		require.ErrorContains(t, err, "positive proxy IDs")
	}

	tooMany := make([]int64, codexTurnStateProxyIDsMaxSize+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	_, err = NormalizeOpenAICodexTurnStateProxyIDs(tooMany)
	require.ErrorContains(t, err, strconv.Itoa(codexTurnStateProxyIDsMaxSize))

	for _, raw := range []string{"null", "{}", `"1"`, "[1", "[1,-2]", " \t "} {
		_, err := ParseOpenAICodexTurnStateProxyIDs(raw)
		require.Error(t, err, raw)
	}
}

func TestSystemSettingsCodexTurnStateProxyIDsParsePrecedence(t *testing.T) {
	svc := NewSettingService(&codexHeaderSettingRepoStub{values: map[string]string{}}, &config.Config{})

	legacy := svc.parseSettings(map[string]string{
		SettingKeyOpenAICodexTurnStateProxyIDs: "[]",
		SettingKeyOpenAICodexTurnStateProxyID:  "17",
	})
	require.Equal(t, []int64{17}, legacy.OpenAICodexTurnStateProxyIDs)
	require.EqualValues(t, 17, legacy.OpenAICodexTurnStateProxyID)
	require.True(t, legacy.OpenAICodexTurnStateProxyIDsValid)

	explicit := svc.parseSettings(map[string]string{
		SettingKeyOpenAICodexTurnStateProxyIDs: "[9,3,9]",
		SettingKeyOpenAICodexTurnStateProxyID:  "17",
	})
	require.Equal(t, []int64{9, 3}, explicit.OpenAICodexTurnStateProxyIDs)
	require.EqualValues(t, 9, explicit.OpenAICodexTurnStateProxyID)
	require.True(t, explicit.OpenAICodexTurnStateProxyIDsValid)

	malformed := svc.parseSettings(map[string]string{
		SettingKeyOpenAICodexTurnStateProxyIDs: "not-json",
		SettingKeyOpenAICodexTurnStateProxyID:  "17",
	})
	require.NotNil(t, malformed.OpenAICodexTurnStateProxyIDs)
	require.Empty(t, malformed.OpenAICodexTurnStateProxyIDs)
	require.Zero(t, malformed.OpenAICodexTurnStateProxyID)
	require.False(t, malformed.OpenAICodexTurnStateProxyIDsValid)

	whitespace := svc.parseSettings(map[string]string{
		SettingKeyOpenAICodexTurnStateProxyIDs: " \t ",
		SettingKeyOpenAICodexTurnStateProxyID:  "17",
	})
	require.Empty(t, whitespace.OpenAICodexTurnStateProxyIDs)
	require.Zero(t, whitespace.OpenAICodexTurnStateProxyID)
	require.False(t, whitespace.OpenAICodexTurnStateProxyIDsValid)

	malformedLegacy := svc.parseSettings(map[string]string{
		SettingKeyOpenAICodexTurnStateProxyIDs: "[]",
		SettingKeyOpenAICodexTurnStateProxyID:  "malformed-private-value",
	})
	require.Empty(t, malformedLegacy.OpenAICodexTurnStateProxyIDs)
	require.Zero(t, malformedLegacy.OpenAICodexTurnStateProxyID)
	require.False(t, malformedLegacy.OpenAICodexTurnStateProxyIDsValid)
}

func TestSystemSettingsCodexTurnStateProxyIDsPersistCanonicalCompatibilityPair(t *testing.T) {
	for _, tc := range []struct {
		name       string
		proxyIDs   []int64
		legacyID   int64
		wantJSON   string
		wantLegacy string
	}{
		{name: "new pool wins and de-duplicates", proxyIDs: []int64{9, 3, 9}, legacyID: 99, wantJSON: "[9,3]", wantLegacy: "9"},
		{name: "new empty pool clears legacy", proxyIDs: []int64{}, legacyID: 99, wantJSON: "[]", wantLegacy: "0"},
		{name: "legacy-only call synchronizes pool", proxyIDs: nil, legacyID: 17, wantJSON: "[17]", wantLegacy: "17"},
		{name: "legacy-only clear synchronizes empty pool", proxyIDs: nil, legacyID: 0, wantJSON: "[]", wantLegacy: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preserveCodexTurnStateSettingGlobalCaches(t)
			repo := &codexHeaderSettingRepoStub{values: map[string]string{}}
			svc := NewSettingService(repo, &config.Config{})
			settings := &SystemSettings{
				OpenAICodexTurnStateProxyIDs: tc.proxyIDs,
				OpenAICodexTurnStateProxyID:  tc.legacyID,
			}
			require.NoError(t, svc.UpdateSettings(context.Background(), settings))
			require.Equal(t, tc.wantJSON, repo.values[SettingKeyOpenAICodexTurnStateProxyIDs])
			require.Equal(t, tc.wantLegacy, repo.values[SettingKeyOpenAICodexTurnStateProxyID])
			require.Equal(t, tc.wantLegacy, strconv.FormatInt(settings.OpenAICodexTurnStateProxyID, 10))
		})
	}
}

func TestSystemSettingsCodexTurnStateProxyIDsRejectInvalidWithoutWrite(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings SystemSettings
		code     string
	}{
		{name: "new pool", settings: SystemSettings{OpenAICodexTurnStateProxyIDs: []int64{1, 0}}, code: "INVALID_OPENAI_CODEX_TURN_STATE_PROXY_IDS"},
		{name: "legacy-only", settings: SystemSettings{OpenAICodexTurnStateProxyID: -1}, code: "INVALID_OPENAI_CODEX_TURN_STATE_PROXY_ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &codexHeaderSettingRepoStub{values: map[string]string{}}
			svc := NewSettingService(repo, &config.Config{})
			err := svc.UpdateSettings(context.Background(), &tc.settings)
			require.ErrorContains(t, err, tc.code)
			require.Nil(t, repo.updates)
		})
	}
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

func TestSystemSettingsCodexTurnStateInvalidStoredScopeRemainsVisible(t *testing.T) {
	svc := NewSettingService(&codexHeaderSettingRepoStub{values: map[string]string{}}, &config.Config{})
	settings := svc.parseSettings(map[string]string{
		SettingKeyOpenAICodexTurnStateModels: " invalid*scope ",
	})

	require.Equal(t, "invalid*scope", settings.OpenAICodexTurnStateModels)
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
