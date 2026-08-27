package service

import (
	"strings"

	"github.com/th3ee9ine/qqq2api/internal/domain"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

var (
	ErrPlatformRetired     = infraerrors.BadRequest("PLATFORM_RETIRED", "the requested platform is no longer supported")
	ErrPlatformUnsupported = infraerrors.BadRequest("PLATFORM_UNSUPPORTED", "the requested platform is not supported")
)

func IsRetiredPlatform(platform string) bool {
	return domain.IsRetiredPlatform(strings.ToLower(strings.TrimSpace(platform)))
}

func IsActiveAccountPlatform(platform string) bool {
	return domain.IsActiveAccountPlatform(strings.ToLower(strings.TrimSpace(platform)))
}

func IsActiveGroupPlatform(platform string) bool {
	return domain.IsActiveGroupPlatform(strings.ToLower(strings.TrimSpace(platform)))
}

func requireActiveAccountPlatform(platform string) error {
	if IsActiveAccountPlatform(platform) {
		return nil
	}
	if IsRetiredPlatform(platform) {
		return ErrPlatformRetired
	}
	return ErrPlatformUnsupported
}

func requireActiveGroupPlatform(platform string) error {
	if IsActiveGroupPlatform(platform) {
		return nil
	}
	if IsRetiredPlatform(platform) {
		return ErrPlatformRetired
	}
	return ErrPlatformUnsupported
}
