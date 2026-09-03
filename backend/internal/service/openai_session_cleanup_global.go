package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// SettingKeyOpenAISessionCleanupSettings stores the installation-wide policy
// used by the OpenAI device-session cleanup worker.  The setting is deliberately
// separate from accounts.extra: cleanup is one scheduler policy for all
// OpenAI OAuth accounts, not an account-level opt-in anymore.
const SettingKeyOpenAISessionCleanupSettings = "openai_session_cleanup_settings"

// OpenAISessionCleanupGlobalSettings is the public/global cleanup policy.
type OpenAISessionCleanupGlobalSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"interval_minutes"`
}

// OpenAISessionCleanupSettings is kept as a concise alias for API consumers
// that use the same name for account and global cleanup settings.
type OpenAISessionCleanupSettings = OpenAISessionCleanupGlobalSettings

func DefaultOpenAISessionCleanupGlobalSettings() *OpenAISessionCleanupGlobalSettings {
	return &OpenAISessionCleanupGlobalSettings{
		Enabled:         false,
		IntervalMinutes: OpenAISessionCleanupDefaultIntervalMinutes,
	}
}

// GetOpenAISessionCleanupGlobalSettings returns fail-safe defaults when the
// setting is absent or malformed.
func (s *SettingService) GetOpenAISessionCleanupGlobalSettings(ctx context.Context) (*OpenAISessionCleanupGlobalSettings, error) {
	defaults := DefaultOpenAISessionCleanupGlobalSettings()
	if s == nil || s.settingRepo == nil {
		return defaults, nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAISessionCleanupSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return defaults, nil
		}
		return nil, fmt.Errorf("get OpenAI session cleanup settings: %w", err)
	}
	if raw == "" {
		return defaults, nil
	}
	var settings OpenAISessionCleanupGlobalSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return defaults, nil
	}
	if settings.IntervalMinutes < OpenAISessionCleanupMinIntervalMinutes || settings.IntervalMinutes > OpenAISessionCleanupMaxIntervalMinutes {
		settings.IntervalMinutes = defaults.IntervalMinutes
	}
	return &settings, nil
}

func (s *SettingService) SetOpenAISessionCleanupGlobalSettings(ctx context.Context, settings *OpenAISessionCleanupGlobalSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	if settings.IntervalMinutes < OpenAISessionCleanupMinIntervalMinutes || settings.IntervalMinutes > OpenAISessionCleanupMaxIntervalMinutes {
		return fmt.Errorf("interval_minutes must be between %d and %d", OpenAISessionCleanupMinIntervalMinutes, OpenAISessionCleanupMaxIntervalMinutes)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal OpenAI session cleanup settings: %w", err)
	}
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service is not configured")
	}
	return s.settingRepo.Set(ctx, SettingKeyOpenAISessionCleanupSettings, string(data))
}

// Short aliases match the HTTP/API terminology while the Global-suffixed
// methods make the installation-wide scope explicit at call sites.
func (s *SettingService) GetOpenAISessionCleanupSettings(ctx context.Context) (*OpenAISessionCleanupGlobalSettings, error) {
	return s.GetOpenAISessionCleanupGlobalSettings(ctx)
}

func (s *SettingService) SetOpenAISessionCleanupSettings(ctx context.Context, settings *OpenAISessionCleanupGlobalSettings) error {
	return s.SetOpenAISessionCleanupGlobalSettings(ctx, settings)
}
