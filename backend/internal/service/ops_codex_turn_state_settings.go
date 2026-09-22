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
	// ProxyPoolURLs is the dedicated collector egress pool. A nil slice means
	// the caller did not request a change; an explicit empty slice clears it.
	//
	// The pool can contain proxy credentials. Keep the value available to the
	// internal settings/update path, but never serialize it through an
	// administrator response. The HTTP handler exposes only credential-free
	// metadata (configured/count) instead.
	ProxyPoolURLs []string                      `json:"-"`
	Harvest       CodexTurnStateHarvestControls `json:"harvest"`
}

type CodexTurnStateHarvestControls struct {
	SpeedPreset            string `json:"speed_preset"`
	MaxRequestsPerRound    int    `json:"max_requests_per_round"`
	FailureCooldownSeconds int    `json:"failure_cooldown_seconds"`
}

type CodexTurnStateHarvestPreset struct {
	RoundIntervalSeconds   int `json:"round_interval_seconds"`
	MaxRequestsPerRound    int `json:"max_requests_per_round"`
	FailureCooldownSeconds int `json:"failure_cooldown_seconds"`
}

var codexTurnStateHarvestPresets = map[string]CodexTurnStateHarvestPreset{
	"slow":     {RoundIntervalSeconds: 300, MaxRequestsPerRound: 3, FailureCooldownSeconds: 300},
	"standard": {RoundIntervalSeconds: 180, MaxRequestsPerRound: 6, FailureCooldownSeconds: 180},
	"fast":     {RoundIntervalSeconds: 60, MaxRequestsPerRound: 12, FailureCooldownSeconds: 60},
	"burst":    {RoundIntervalSeconds: 1, MaxRequestsPerRound: 20, FailureCooldownSeconds: 1},
}

func CodexTurnStateHarvestPresetNames() []string {
	return []string{"slow", "standard", "fast", "burst"}
}

func defaultCodexTurnStateHarvestControls() CodexTurnStateHarvestControls {
	preset := codexTurnStateHarvestPresets["standard"]
	return CodexTurnStateHarvestControls{SpeedPreset: "standard", MaxRequestsPerRound: preset.MaxRequestsPerRound, FailureCooldownSeconds: preset.FailureCooldownSeconds}
}

func normalizeCodexTurnStateHarvestControls(controls CodexTurnStateHarvestControls) CodexTurnStateHarvestControls {
	controls.SpeedPreset = strings.ToLower(strings.TrimSpace(controls.SpeedPreset))
	preset, ok := codexTurnStateHarvestPresets[controls.SpeedPreset]
	if !ok {
		controls.SpeedPreset = "standard"
		preset = codexTurnStateHarvestPresets[controls.SpeedPreset]
	}
	if controls.MaxRequestsPerRound <= 0 {
		controls.MaxRequestsPerRound = preset.MaxRequestsPerRound
	}
	if controls.FailureCooldownSeconds <= 0 {
		controls.FailureCooldownSeconds = preset.FailureCooldownSeconds
	}
	return controls
}

func ValidateCodexTurnStateHarvestControls(controls CodexTurnStateHarvestControls) error {
	if _, ok := codexTurnStateHarvestPresets[strings.ToLower(strings.TrimSpace(controls.SpeedPreset))]; !ok {
		return fmt.Errorf("invalid harvest speed preset")
	}
	if controls.MaxRequestsPerRound < 1 || controls.MaxRequestsPerRound > 100 {
		return fmt.Errorf("max_requests_per_round must be between 1 and 100")
	}
	if controls.FailureCooldownSeconds < 1 || controls.FailureCooldownSeconds > 3600 {
		return fmt.Errorf("failure_cooldown_seconds must be between 1 and 3600")
	}
	return nil
}

func defaultCodexTurnStateRuntimeSettings() CodexTurnStateRuntimeSettings {
	return CodexTurnStateRuntimeSettings{
		ProbeEnabled:     true,
		InjectionEnabled: true,
		Harvest:          defaultCodexTurnStateHarvestControls(),
	}
}

func parseCodexTurnStateRuntimeEnabled(raw string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	return value
}

func codexTurnStateRuntimeSettingsFromValues(values map[string]string) (CodexTurnStateRuntimeSettings, error) {
	settings := defaultCodexTurnStateRuntimeSettings()
	if raw, ok := values[SettingKeyCodexTurnStateProbeEnabled]; ok {
		settings.ProbeEnabled = parseCodexTurnStateRuntimeEnabled(raw)
	}
	if raw, ok := values[SettingKeyCodexTurnStateCacheInjectionEnabled]; ok {
		settings.InjectionEnabled = parseCodexTurnStateRuntimeEnabled(raw)
	}
	if raw, ok := values[SettingKeyCodexTurnStateProxyPool]; ok {
		pool, err := parseOpenAICodexTurnStateProxyPool(raw)
		if err != nil {
			return settings, fmt.Errorf("parse persisted Codex turn-state proxy pool: %w", err)
		}
		settings.ProxyPoolURLs = pool
	}
	if raw, ok := values[SettingKeyCodexTurnStateHarvestSpeedPreset]; ok {
		settings.Harvest.SpeedPreset = strings.TrimSpace(raw)
	}
	if raw, ok := values[SettingKeyCodexTurnStateHarvestRequestBudget]; ok {
		if value, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			settings.Harvest.MaxRequestsPerRound = value
		}
	}
	if raw, ok := values[SettingKeyCodexTurnStateHarvestFailureCooldown]; ok {
		if value, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
			settings.Harvest.FailureCooldownSeconds = value
		}
	}
	settings.Harvest = normalizeCodexTurnStateHarvestControls(settings.Harvest)
	if err := ValidateCodexTurnStateHarvestControls(settings.Harvest); err != nil {
		return settings, err
	}
	return settings, nil
}

// GetCodexTurnStateRuntimeSettings reads the persisted administrator settings.
// Missing boolean values fall back to true. A malformed persisted proxy pool is
// an explicit configuration error: return it to the administrator instead of
// silently publishing an empty pool that would route probes through an account
// proxy. This is an administrative cold path; request forwarding reads the
// gateway atomic snapshot populated at startup, on refresh, and after
// successful updates.
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
		SettingKeyCodexTurnStateProxyPool,
		SettingKeyCodexTurnStateHarvestSpeedPreset,
		SettingKeyCodexTurnStateHarvestRequestBudget,
		SettingKeyCodexTurnStateHarvestFailureCooldown,
	})
	if err != nil {
		return nil, fmt.Errorf("get Codex turn-state runtime settings: %w", err)
	}
	settings, err := codexTurnStateRuntimeSettingsFromValues(values)
	if err != nil {
		return nil, err
	}
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
	pool := settings.ProxyPoolURLs
	if strings.TrimSpace(settings.Harvest.SpeedPreset) == "" {
		current, readErr := s.settingRepo.GetMultiple(ctx, []string{
			SettingKeyCodexTurnStateHarvestSpeedPreset,
			SettingKeyCodexTurnStateHarvestRequestBudget,
			SettingKeyCodexTurnStateHarvestFailureCooldown,
		})
		if readErr != nil {
			return nil, fmt.Errorf("read existing Codex turn-state harvest controls: %w", readErr)
		}
		existing, parseErr := codexTurnStateRuntimeSettingsFromValues(current)
		if parseErr != nil {
			return nil, parseErr
		}
		settings.Harvest = existing.Harvest
	}
	settings.Harvest = normalizeCodexTurnStateHarvestControls(settings.Harvest)
	if err := ValidateCodexTurnStateHarvestControls(settings.Harvest); err != nil {
		return nil, err
	}
	if pool == nil {
		// Older callers only know about the two switches. Preserve the existing
		// pool when they omit the new field.
		current, readErr := s.settingRepo.GetMultiple(ctx, []string{SettingKeyCodexTurnStateProxyPool})
		if readErr != nil {
			return nil, fmt.Errorf("read existing Codex turn-state proxy pool: %w", readErr)
		}
		var parseErr error
		pool, parseErr = parseOpenAICodexTurnStateProxyPool(current[SettingKeyCodexTurnStateProxyPool])
		if parseErr != nil {
			return nil, fmt.Errorf("read existing Codex turn-state proxy pool: %w", parseErr)
		}
	}
	var normalizedPool []string
	if pool != nil {
		var err error
		normalizedPool, err = NormalizeOpenAICodexTurnStateProxyPool(pool)
		if err != nil {
			return nil, fmt.Errorf("update Codex turn-state proxy pool: %w", err)
		}
	}
	poolJSON, err := marshalOpenAICodexTurnStateProxyPool(normalizedPool)
	if err != nil {
		return nil, fmt.Errorf("encode Codex turn-state proxy pool: %w", err)
	}
	if err := s.settingRepo.SetMultiple(ctx, map[string]string{
		SettingKeyCodexTurnStateProbeEnabled:           strconv.FormatBool(settings.ProbeEnabled),
		SettingKeyCodexTurnStateCacheInjectionEnabled:  strconv.FormatBool(settings.InjectionEnabled),
		SettingKeyCodexTurnStateProxyPool:              poolJSON,
		SettingKeyCodexTurnStateHarvestSpeedPreset:     settings.Harvest.SpeedPreset,
		SettingKeyCodexTurnStateHarvestRequestBudget:   strconv.Itoa(settings.Harvest.MaxRequestsPerRound),
		SettingKeyCodexTurnStateHarvestFailureCooldown: strconv.Itoa(settings.Harvest.FailureCooldownSeconds),
	}); err != nil {
		return nil, fmt.Errorf("update Codex turn-state runtime settings: %w", err)
	}
	updated := settings
	updated.ProxyPoolURLs = normalizedPool
	s.applyCodexTurnStateRuntimeSettings(updated)
	return &updated, nil
}

func (s *OpsService) applyCodexTurnStateRuntimeSettings(settings CodexTurnStateRuntimeSettings) {
	if s == nil || s.openAIGatewayService == nil {
		return
	}
	s.openAIGatewayService.SetCodexTurnStateRuntimeSettingsWithControls(settings.ProbeEnabled, settings.InjectionEnabled, settings.ProxyPoolURLs, settings.Harvest)
}
