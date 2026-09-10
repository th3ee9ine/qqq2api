package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

// NormalizeOpenAICodexClientVersionMode keeps legacy empty settings in automatic
// mode. Invalid nonempty values are rejected at the settings write boundary.
func NormalizeOpenAICodexClientVersionMode(raw string) string {
	switch strings.TrimSpace(raw) {
	case "", OpenAICodexClientVersionModeAuto:
		return OpenAICodexClientVersionModeAuto
	case OpenAICodexClientVersionModePinned:
		return OpenAICodexClientVersionModePinned
	default:
		return ""
	}
}

// validateOpenAICodexVersionUpdates runs with settingsUpdateMu held. It reads
// omitted keys from storage so an earlier HTTP snapshot cannot validate a pair
// that a concurrent update has since changed. Nothing is written on failure.
func (s *SettingService) validateOpenAICodexVersionUpdates(ctx context.Context, updates map[string]string) error {
	_, updatesMode := updates[SettingKeyOpenAICodexClientVersionMode]
	_, updatesVersion := updates[SettingKeyOpenAICodexClientVersion]
	if !updatesMode && !updatesVersion {
		return nil
	}
	readMerged := func(key string) (string, error) {
		if value, ok := updates[key]; ok {
			return value, nil
		}
		value, err := s.settingRepo.GetValue(ctx, key)
		if errors.Is(err, ErrSettingNotFound) {
			return "", nil
		}
		if err != nil {
			return "", fmt.Errorf("read current %s before settings update: %w", key, err)
		}
		return value, nil
	}
	rawMode, err := readMerged(SettingKeyOpenAICodexClientVersionMode)
	if err != nil {
		return err
	}
	mode := NormalizeOpenAICodexClientVersionMode(rawMode)
	if mode == "" {
		return infraerrors.BadRequest("INVALID_OPENAI_CODEX_CLIENT_VERSION_MODE", "openai_codex_client_version_mode must be auto or pinned")
	}
	if mode != OpenAICodexClientVersionModePinned {
		return nil
	}
	version, err := readMerged(SettingKeyOpenAICodexClientVersion)
	if err != nil {
		return err
	}
	if normalizeStableCodexClientVersion(version) == "" {
		return infraerrors.BadRequest("INVALID_OPENAI_CODEX_PINNED_VERSION", "openai_codex_client_version must be a nonempty stable X.Y.Z version when openai_codex_client_version_mode is pinned")
	}
	return nil
}
