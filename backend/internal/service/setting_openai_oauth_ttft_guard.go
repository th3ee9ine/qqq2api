package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	openAIOAuthTTFTGuardSettingsCacheTTL   = 30 * time.Second
	OpenAIOAuthTTFTGuardMinRecoveryMinutes = 1
	OpenAIOAuthTTFTGuardMaxRecoveryMinutes = 10080
)

type cachedOpenAIOAuthTTFTGuardSettings struct {
	settings  OpenAIOAuthTTFTGuardSettings
	expiresAt time.Time
}

func normalizeOpenAIOAuthTTFTGuardSettings(settings *OpenAIOAuthTTFTGuardSettings) *OpenAIOAuthTTFTGuardSettings {
	if settings == nil {
		return DefaultOpenAIOAuthTTFTGuardSettings()
	}
	result := *settings
	if result.RecoveryIntervalMinutes < OpenAIOAuthTTFTGuardMinRecoveryMinutes {
		result.RecoveryIntervalMinutes = OpenAIOAuthTTFTGuardMinRecoveryMinutes
	} else if result.RecoveryIntervalMinutes > OpenAIOAuthTTFTGuardMaxRecoveryMinutes {
		result.RecoveryIntervalMinutes = OpenAIOAuthTTFTGuardMaxRecoveryMinutes
	}
	return &result
}

// GetOpenAIOAuthTTFTGuardSettings returns the cached OpenAI OAuth TTFT guard
// policy. Missing or malformed stored values use the safe defaults.
func (s *SettingService) GetOpenAIOAuthTTFTGuardSettings(ctx context.Context) (*OpenAIOAuthTTFTGuardSettings, error) {
	if s == nil || s.settingRepo == nil {
		return DefaultOpenAIOAuthTTFTGuardSettings(), nil
	}
	if cached, ok := s.openAIOAuthTTFTGuardCache.Load().(*cachedOpenAIOAuthTTFTGuardSettings); ok && cached != nil && time.Now().Before(cached.expiresAt) {
		result := cached.settings
		return &result, nil
	}

	settings := DefaultOpenAIOAuthTTFTGuardSettings()
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIOAuthTTFTGuardSettings)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, fmt.Errorf("get OpenAI OAuth TTFT guard settings: %w", err)
	}
	if err == nil && strings.TrimSpace(value) != "" {
		var stored OpenAIOAuthTTFTGuardSettings
		if json.Unmarshal([]byte(value), &stored) == nil {
			settings = normalizeOpenAIOAuthTTFTGuardSettings(&stored)
		}
	}
	s.openAIOAuthTTFTGuardCache.Store(&cachedOpenAIOAuthTTFTGuardSettings{
		settings:  *settings,
		expiresAt: time.Now().Add(openAIOAuthTTFTGuardSettingsCacheTTL),
	})
	result := *settings
	return &result, nil
}

// SetOpenAIOAuthTTFTGuardSettings validates and persists the OpenAI OAuth TTFT
// guard policy, then publishes it to this process's cache immediately.
func (s *SettingService) SetOpenAIOAuthTTFTGuardSettings(ctx context.Context, settings *OpenAIOAuthTTFTGuardSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("setting service is unavailable")
	}
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	if settings.RecoveryIntervalMinutes < OpenAIOAuthTTFTGuardMinRecoveryMinutes || settings.RecoveryIntervalMinutes > OpenAIOAuthTTFTGuardMaxRecoveryMinutes {
		return fmt.Errorf("recovery_interval_minutes must be between %d-%d", OpenAIOAuthTTFTGuardMinRecoveryMinutes, OpenAIOAuthTTFTGuardMaxRecoveryMinutes)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal OpenAI OAuth TTFT guard settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOpenAIOAuthTTFTGuardSettings, string(data)); err != nil {
		return fmt.Errorf("set OpenAI OAuth TTFT guard settings: %w", err)
	}
	s.openAIOAuthTTFTGuardCache.Store(&cachedOpenAIOAuthTTFTGuardSettings{
		settings:  *settings,
		expiresAt: time.Now().Add(openAIOAuthTTFTGuardSettingsCacheTTL),
	})
	return nil
}
