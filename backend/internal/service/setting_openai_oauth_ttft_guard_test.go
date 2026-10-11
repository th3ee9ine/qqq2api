//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func TestOpenAIOAuthTTFTGuardSettingsDefaults(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetOpenAIOAuthTTFTGuardSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, 30, settings.RecoveryIntervalMinutes)
	require.Equal(t, OpenAIOAuthTTFTGuardDefaultFirstTokenThresholdSeconds, settings.FirstTokenThresholdSeconds)
}

func TestOpenAIOAuthTTFTGuardSettingsReadsAndClamps(t *testing.T) {
	repo := newMockSettingRepo()
	encoded, err := json.Marshal(OpenAIOAuthTTFTGuardSettings{Enabled: false, RecoveryIntervalMinutes: 0, FirstTokenThresholdSeconds: 0})
	require.NoError(t, err)
	repo.data[SettingKeyOpenAIOAuthTTFTGuardSettings] = string(encoded)
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetOpenAIOAuthTTFTGuardSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, OpenAIOAuthTTFTGuardMinRecoveryMinutes, settings.RecoveryIntervalMinutes)
	require.Equal(t, OpenAIOAuthTTFTGuardDefaultFirstTokenThresholdSeconds, settings.FirstTokenThresholdSeconds)

	encoded, err = json.Marshal(OpenAIOAuthTTFTGuardSettings{
		Enabled:                    true,
		RecoveryIntervalMinutes:    30,
		FirstTokenThresholdSeconds: OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds + 1,
	})
	require.NoError(t, err)
	repo.data[SettingKeyOpenAIOAuthTTFTGuardSettings] = string(encoded)
	settings, err = NewSettingService(repo, &config.Config{}).GetOpenAIOAuthTTFTGuardSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.Enabled)
	require.Equal(t, OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds, settings.FirstTokenThresholdSeconds)
}

func TestOpenAIOAuthTTFTGuardSettingsSetterValidatesAndPublishesCache(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	require.Error(t, svc.SetOpenAIOAuthTTFTGuardSettings(context.Background(), &OpenAIOAuthTTFTGuardSettings{
		Enabled: true, RecoveryIntervalMinutes: OpenAIOAuthTTFTGuardMaxRecoveryMinutes + 1,
	}))
	require.Error(t, svc.SetOpenAIOAuthTTFTGuardSettings(context.Background(), &OpenAIOAuthTTFTGuardSettings{
		Enabled: true, RecoveryIntervalMinutes: 30,
		FirstTokenThresholdSeconds: -1,
	}))
	require.Error(t, svc.SetOpenAIOAuthTTFTGuardSettings(context.Background(), &OpenAIOAuthTTFTGuardSettings{
		Enabled: true, RecoveryIntervalMinutes: 30,
		FirstTokenThresholdSeconds: OpenAIOAuthTTFTGuardMaxFirstTokenThresholdSeconds + 1,
	}))
	require.NoError(t, svc.SetOpenAIOAuthTTFTGuardSettings(context.Background(), &OpenAIOAuthTTFTGuardSettings{
		Enabled: false, RecoveryIntervalMinutes: 45, FirstTokenThresholdSeconds: 20,
	}))

	settings, err := svc.GetOpenAIOAuthTTFTGuardSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, 45, settings.RecoveryIntervalMinutes)
	require.Equal(t, 20, settings.FirstTokenThresholdSeconds)
}

func TestOpenAIOAuthTTFTGuardSettingsLegacyAndInvalidStoredValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stored    string
		enabled   bool
		recovery  int
		threshold int
	}{
		{"legacy enabled policy", `{"enabled":true,"recovery_interval_minutes":45}`, true, 45, 15},
		{"empty object", `{}`, false, 1, 15},
		{"malformed JSON", `{broken`, false, 30, 15},
		{"invalid field type", `{"enabled":true,"first_token_threshold_seconds":"20"}`, false, 30, 15},
		{"negative values", `{"enabled":true,"recovery_interval_minutes":-1,"first_token_threshold_seconds":-1}`, true, 1, 1},
		{"upper limits", `{"enabled":true,"recovery_interval_minutes":10080,"first_token_threshold_seconds":300}`, true, 10080, 300},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockSettingRepo()
			repo.data[SettingKeyOpenAIOAuthTTFTGuardSettings] = tc.stored
			got, err := NewSettingService(repo, nil).GetOpenAIOAuthTTFTGuardSettings(context.Background())
			require.NoError(t, err)
			require.Equal(t, &OpenAIOAuthTTFTGuardSettings{Enabled: tc.enabled, RecoveryIntervalMinutes: tc.recovery, FirstTokenThresholdSeconds: tc.threshold}, got)
		})
	}
}

func TestOpenAIOAuthTTFTGuardSettingsPersistenceAndCacheIsolation(t *testing.T) {
	ctx := context.Background()
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, nil)
	initial, err := svc.GetOpenAIOAuthTTFTGuardSettings(ctx)
	require.NoError(t, err)
	initial.Enabled = true
	initial.FirstTokenThresholdSeconds = 99
	cached, err := svc.GetOpenAIOAuthTTFTGuardSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultOpenAIOAuthTTFTGuardSettings(), cached)

	for _, threshold := range []int{1, 300} {
		policy := &OpenAIOAuthTTFTGuardSettings{Enabled: true, RecoveryIntervalMinutes: 10080, FirstTokenThresholdSeconds: threshold}
		require.NoError(t, svc.SetOpenAIOAuthTTFTGuardSettings(ctx, policy))
		persisted, err := NewSettingService(repo, nil).GetOpenAIOAuthTTFTGuardSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, policy, persisted)
	}
	before, err := svc.GetOpenAIOAuthTTFTGuardSettings(ctx)
	require.NoError(t, err)
	for _, invalid := range []*OpenAIOAuthTTFTGuardSettings{
		nil,
		{Enabled: true, RecoveryIntervalMinutes: 0, FirstTokenThresholdSeconds: 15},
		{Enabled: true, RecoveryIntervalMinutes: 10081, FirstTokenThresholdSeconds: 15},
		{Enabled: true, RecoveryIntervalMinutes: 30, FirstTokenThresholdSeconds: -1},
		{Enabled: true, RecoveryIntervalMinutes: 30, FirstTokenThresholdSeconds: 301},
	} {
		require.Error(t, svc.SetOpenAIOAuthTTFTGuardSettings(ctx, invalid))
		after, err := svc.GetOpenAIOAuthTTFTGuardSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, before, after)
		persisted, err := NewSettingService(repo, nil).GetOpenAIOAuthTTFTGuardSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, before, persisted)
	}
}
