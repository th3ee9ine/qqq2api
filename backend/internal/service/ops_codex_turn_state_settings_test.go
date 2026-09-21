package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type codexTurnStateSettingsRepo struct {
	SettingRepository
	mu       sync.Mutex
	values   map[string]string
	readErr  error
	writeErr error
}

func newCodexTurnStateSettingsRepo(values map[string]string) *codexTurnStateSettingsRepo {
	if values == nil {
		values = map[string]string{}
	}
	return &codexTurnStateSettingsRepo{values: values}
}

func (r *codexTurnStateSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readErr != nil {
		return nil, r.readErr
	}
	result := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

func (r *codexTurnStateSettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeErr != nil {
		return r.writeErr
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestCodexTurnStateRuntimeSettingsDefaultsMissingAndMalformedValuesToEnabled(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          "not-a-boolean",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "false",
	})
	svc := &OpsService{settingRepo: repo}

	settings, err := svc.GetCodexTurnStateRuntimeSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.ProbeEnabled)
	require.False(t, settings.InjectionEnabled)

	delete(repo.values, SettingKeyCodexTurnStateCacheInjectionEnabled)
	settings, err = svc.GetCodexTurnStateRuntimeSettings(context.Background())
	require.NoError(t, err)
	require.True(t, settings.ProbeEnabled)
	require.True(t, settings.InjectionEnabled)

	settings, err = (*OpsService)(nil).GetCodexTurnStateRuntimeSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, &CodexTurnStateRuntimeSettings{ProbeEnabled: true, InjectionEnabled: true}, settings)
}

func TestCodexTurnStateRuntimeSettingsLoadAndUpdateGatewaySnapshot(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          "false",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "true",
	})
	gateway := &OpenAIGatewayService{}
	svc := &OpsService{settingRepo: repo, openAIGatewayService: gateway}
	svc.initRuntimeSettings(context.Background())

	probeEnabled, injectionEnabled := gateway.CodexTurnStateRuntimeSettings()
	require.False(t, probeEnabled)
	require.True(t, injectionEnabled)

	updated, err := svc.UpdateCodexTurnStateRuntimeSettings(context.Background(), CodexTurnStateRuntimeSettings{
		ProbeEnabled:     true,
		InjectionEnabled: false,
	})
	require.NoError(t, err)
	require.Equal(t, &CodexTurnStateRuntimeSettings{ProbeEnabled: true, InjectionEnabled: false}, updated)
	require.Equal(t, "true", repo.values[SettingKeyCodexTurnStateProbeEnabled])
	require.Equal(t, "false", repo.values[SettingKeyCodexTurnStateCacheInjectionEnabled])
	probeEnabled, injectionEnabled = gateway.CodexTurnStateRuntimeSettings()
	require.True(t, probeEnabled)
	require.False(t, injectionEnabled)
}

func TestCodexTurnStateRuntimeSettingsFailedRefreshAndWriteKeepLastSnapshot(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          "false",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "false",
	})
	gateway := &OpenAIGatewayService{}
	svc := &OpsService{settingRepo: repo, openAIGatewayService: gateway}
	svc.initRuntimeSettings(context.Background())

	repo.readErr = errors.New("read failed")
	require.Error(t, svc.RefreshRuntimeSettings(context.Background()))
	probeEnabled, injectionEnabled := gateway.CodexTurnStateRuntimeSettings()
	require.False(t, probeEnabled)
	require.False(t, injectionEnabled)

	repo.readErr = nil
	repo.writeErr = errors.New("write failed")
	_, err := svc.UpdateCodexTurnStateRuntimeSettings(context.Background(), CodexTurnStateRuntimeSettings{
		ProbeEnabled:     true,
		InjectionEnabled: true,
	})
	require.Error(t, err)
	probeEnabled, injectionEnabled = gateway.CodexTurnStateRuntimeSettings()
	require.False(t, probeEnabled)
	require.False(t, injectionEnabled)
}
