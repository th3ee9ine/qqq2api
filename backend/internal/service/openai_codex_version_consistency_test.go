//go:build unit

package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestCodexVersionConstants_Consistency(t *testing.T) {
	require.True(t, strings.Contains(codexCLIUserAgent, openai.CodexDefaultOriginator+"/"+codexCLIVersion),
		"codexCLIUserAgent must embed codexCLIVersion")
	require.Contains(t, codexCLIUserAgent, "(Codex Desktop; "+codexDesktopAppVersion+")",
		"Codex Desktop app version must remain an independent UA trailer signal")
	require.NotEqual(t, codexCLIVersion, codexDesktopAppVersion,
		"Desktop app version must not be conflated with the Rust engine version")

	require.True(t, strings.Contains(DefaultOpenAICodexUserAgent, codexCLIVersion),
		"DefaultOpenAICodexUserAgent must embed codexCLIVersion")
	require.Equal(t, "0.150.1", codexResponsesVersionFallback,
		"Responses Version fallback must match the current official stable release")
	require.Equal(t, codexCLIVersion, codexResponsesVersionFallback,
		"User-Agent engine and Responses Version must share one stable release")
}
