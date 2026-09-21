package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// CodexTurnStateRuntimeSettings is the administrator-facing policy for active
// turn-state probes and collector-cache injection. Both switches default to
// enabled for new and upgraded installations.
type CodexTurnStateRuntimeSettings struct {
	ProbeEnabled     bool `json:"probe_enabled"`
	InjectionEnabled bool `json:"injection_enabled"`
}

func defaultCodexTurnStateRuntimeSettings() CodexTurnStateRuntimeSettings {
	return CodexTurnStateRuntimeSettings{
		ProbeEnabled:     true,
		InjectionEnabled: true,
	}
}

func parseCodexTurnStateRuntimeEnabled(raw string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	return value
}

func codexTurnStateRuntimeSettingsFromValues(values map[string]string) CodexTurnStateRuntimeSettings {
	settings := defaultCodexTurnStateRuntimeSettings()
	if raw, ok := values[SettingKeyCodexTurnStateProbeEnabled]; ok {
		settings.ProbeEnabled = parseCodexTurnStateRuntimeEnabled(raw)
	}
	if raw, ok := values[SettingKeyCodexTurnStateCacheInjectionEnabled]; ok {
		settings.InjectionEnabled = parseCodexTurnStateRuntimeEnabled(raw)
	}
	return settings
}

// GetCodexTurnStateRuntimeSettings reads the persisted administrator settings.
// Missing or malformed values independently fall back to true. This is an
// administrative cold path; request forwarding reads the gateway atomic
// snapshot populated at startup, on refresh, and after successful updates.
func (s *OpsService) GetCodexTurnStateRuntimeSettings(ctx context.Context) (*CodexTurnStateRuntimeSettings, error) {
	defaults := defaultCodexTurnStateRuntimeSettings()
	if s == nil || s.settingRepo == nil {
		return &defaults, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyCodexTurnStateProbeEnabled,
		SettingKeyCodexTurnStateCacheInjectionEnabled,
	})
	if err != nil {
		return nil, fmt.Errorf("get Codex turn-state runtime settings: %w", err)
	}
	settings := codexTurnStateRuntimeSettingsFromValues(values)
	return &settings, nil
}

// UpdateCodexTurnStateRuntimeSettings persists both switches as one logical
// update and publishes the new immutable gateway snapshot before returning.
func (s *OpsService) UpdateCodexTurnStateRuntimeSettings(ctx context.Context, settings CodexTurnStateRuntimeSettings) (*CodexTurnStateRuntimeSettings, error) {
	if s == nil || s.settingRepo == nil {
		return nil, fmt.Errorf("setting repository is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.runtimeSettingsMu.Lock()
	defer s.runtimeSettingsMu.Unlock()
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:          strconv.FormatBool(settings.ProbeEnabled),
		SettingKeyCodexTurnStateCacheInjectionEnabled: strconv.FormatBool(settings.InjectionEnabled),
	}); err != nil {
		return nil, fmt.Errorf("update Codex turn-state runtime settings: %w", err)
	}
	s.applyCodexTurnStateRuntimeSettings(settings)
	updated := settings
	return &updated, nil
}

func (s *OpsService) applyCodexTurnStateRuntimeSettings(settings CodexTurnStateRuntimeSettings) {
	if s == nil || s.openAIGatewayService == nil {
		return
	}
	s.openAIGatewayService.SetCodexTurnStateRuntimeSettings(settings.ProbeEnabled, settings.InjectionEnabled)
}
