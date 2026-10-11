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
	require.True(t, settings.Enabled)
	require.Equal(t, 30, settings.RecoveryIntervalMinutes)
}

func TestOpenAIOAuthTTFTGuardSettingsReadsAndClamps(t *testing.T) {
	repo := newMockSettingRepo()
	encoded, err := json.Marshal(OpenAIOAuthTTFTGuardSettings{Enabled: false, RecoveryIntervalMinutes: 0})
	require.NoError(t, err)
	repo.data[SettingKeyOpenAIOAuthTTFTGuardSettings] = string(encoded)
	svc := NewSettingService(repo, &config.Config{})

	settings, err := svc.GetOpenAIOAuthTTFTGuardSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, OpenAIOAuthTTFTGuardMinRecoveryMinutes, settings.RecoveryIntervalMinutes)
}

func TestOpenAIOAuthTTFTGuardSettingsSetterValidatesAndPublishesCache(t *testing.T) {
	repo := newMockSettingRepo()
	svc := NewSettingService(repo, &config.Config{})

	require.Error(t, svc.SetOpenAIOAuthTTFTGuardSettings(context.Background(), &OpenAIOAuthTTFTGuardSettings{
		Enabled: true, RecoveryIntervalMinutes: OpenAIOAuthTTFTGuardMaxRecoveryMinutes + 1,
	}))
	require.NoError(t, svc.SetOpenAIOAuthTTFTGuardSettings(context.Background(), &OpenAIOAuthTTFTGuardSettings{
		Enabled: false, RecoveryIntervalMinutes: 45,
	}))

	settings, err := svc.GetOpenAIOAuthTTFTGuardSettings(context.Background())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Equal(t, 45, settings.RecoveryIntervalMinutes)
}
