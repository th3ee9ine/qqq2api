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
	require.Equal(t, &CodexTurnStateRuntimeSettings{
		ProbeEnabled: true, InjectionEnabled: true,
		Harvest: defaultCodexTurnStateHarvestControls(),
	}, settings)
}

func TestCodexTurnStateRuntimeSettingsMalformedPersistedPoolReturnsError(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          "true",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "true",
		SettingKeyCodexTurnStateProxyPool:             `["http://proxy.example:8080"`,
	})
	svc := &OpsService{settingRepo: repo}

	settings, err := svc.GetCodexTurnStateRuntimeSettings(context.Background())
	require.ErrorIs(t, err, ErrInvalidOpenAICodexTurnStateProxyPool)
	require.Nil(t, settings)
}

func TestCodexTurnStateRuntimeSettingsMalformedPoolColdStartDisablesGateway(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          "true",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "true",
		SettingKeyCodexTurnStateProxyPool:             `["http://proxy.example:8080"`,
	})
	gateway := &OpenAIGatewayService{}
	svc := &OpsService{settingRepo: repo, openAIGatewayService: gateway}

	svc.initRuntimeSettings(context.Background())
	probeEnabled, injectionEnabled := gateway.CodexTurnStateRuntimeSettings()
	require.False(t, probeEnabled)
	require.False(t, injectionEnabled)
	require.Empty(t, gateway.CodexTurnStateProxyPool())
}

func TestCodexTurnStateRuntimeSettingsColdStartDatabaseErrorKeepsDefaultOn(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(nil)
	repo.readErr = errors.New("database unavailable")
	gateway := &OpenAIGatewayService{}
	svc := &OpsService{settingRepo: repo, openAIGatewayService: gateway}

	svc.initRuntimeSettings(context.Background())
	probeEnabled, injectionEnabled := gateway.CodexTurnStateRuntimeSettings()
	require.True(t, probeEnabled)
	require.True(t, injectionEnabled)
	require.Empty(t, gateway.CodexTurnStateProxyPool())
}

func TestCodexTurnStateRuntimeSettingsLoadAndUpdateGatewaySnapshot(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          "false",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "true",
		SettingKeyCodexTurnStateProxyPool:             `["http://proxy.example:8080"]`,
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
	require.Equal(t, &CodexTurnStateRuntimeSettings{
		ProbeEnabled:     true,
		InjectionEnabled: false,
		ProxyPoolURLs:    []string{"http://proxy.example:8080"},
		Harvest:          defaultCodexTurnStateHarvestControls(),
	}, updated)
	require.Equal(t, "true", repo.values[SettingKeyCodexTurnStateProbeEnabled])
	require.Equal(t, "false", repo.values[SettingKeyCodexTurnStateCacheInjectionEnabled])
	require.Equal(t, `["http://proxy.example:8080"]`, repo.values[SettingKeyCodexTurnStateProxyPool])
	probeEnabled, injectionEnabled = gateway.CodexTurnStateRuntimeSettings()
	require.True(t, probeEnabled)
	require.False(t, injectionEnabled)
	require.Equal(t, []string{"http://proxy.example:8080"}, gateway.CodexTurnStateProxyPool())
}

func TestCodexTurnStateRuntimeSettingsOmittedPoolReadFailureDoesNotClearExistingPool(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          "false",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "true",
		SettingKeyCodexTurnStateProxyPool:             `["http://proxy.example:8080"]`,
	})
	gateway := &OpenAIGatewayService{}
	svc := &OpsService{settingRepo: repo, openAIGatewayService: gateway}
	svc.initRuntimeSettings(context.Background())
	repo.readErr = errors.New("read failed")
	_, err := svc.UpdateCodexTurnStateRuntimeSettings(context.Background(), CodexTurnStateRuntimeSettings{
		ProbeEnabled:     true,
		InjectionEnabled: true,
		// nil means the caller omitted the new field and must preserve it.
		ProxyPoolURLs: nil,
	})
	require.Error(t, err)
	require.Equal(t, `["http://proxy.example:8080"]`, repo.values[SettingKeyCodexTurnStateProxyPool])
	require.Equal(t, []string{"http://proxy.example:8080"}, gateway.CodexTurnStateProxyPool())
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

func TestCodexTurnStateRuntimeSettingsMalformedPoolRefreshKeepsLastSnapshot(t *testing.T) {
	repo := newCodexTurnStateSettingsRepo(map[string]string{
		SettingKeyOpsMonitoringEnabled:                "false",
		SettingKeyCodexTurnStateProbeEnabled:          "false",
		SettingKeyCodexTurnStateCacheInjectionEnabled: "false",
		SettingKeyCodexTurnStateProxyPool:             `["http://proxy.example:8080"]`,
	})
	gateway := &OpenAIGatewayService{}
	svc := &OpsService{settingRepo: repo, openAIGatewayService: gateway}
	svc.initRuntimeSettings(context.Background())

	// Corrupt the persisted value after a valid snapshot has been published.
	repo.mu.Lock()
	repo.values[SettingKeyCodexTurnStateProxyPool] = `["http://proxy.example:8080"`
	repo.mu.Unlock()

	err := svc.RefreshRuntimeSettings(context.Background())
	require.ErrorIs(t, err, ErrInvalidOpenAICodexTurnStateProxyPool)
	probeEnabled, injectionEnabled := gateway.CodexTurnStateRuntimeSettings()
	require.False(t, probeEnabled)
	require.False(t, injectionEnabled)
	require.Equal(t, []string{"http://proxy.example:8080"}, gateway.CodexTurnStateProxyPool())
	require.False(t, svc.IsMonitoringEnabled(context.Background()))
}
