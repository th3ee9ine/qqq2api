package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeKnownOpenAICodexModelGPT6Astra(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "openai/gpt-6-astra", "OPENAI/GPT-6_ASTRA", "gpt-6", "openai/gpt-6"} {
		require.Equal(t, "gpt-6-astra", normalizeKnownOpenAICodexModel(model))
	}
}

func TestNormalizeKnownOpenAICodexModelGPT6SolLuna(t *testing.T) {
	tests := map[string]string{
		"gpt-6.1-sol":                       "gpt-6.1-sol",
		"openai/gpt-6.1-sol-max":            "gpt-6.1-sol",
		"OPENAI/GPT-6.1_SOL_OPENAI_COMPACT": "gpt-6.1-sol",
		"gpt-6-sol":                         "gpt-6-sol",
		"openai/gpt-6-sol-max":              "gpt-6-sol",
		"gpt-6-luna-high":                   "gpt-6-luna",
		"OPENAI/GPT-6_LUNA_OPENAI_COMPACT":  "gpt-6-luna",
	}
	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, expected, normalizeKnownOpenAICodexModel(input))
		})
	}
}

func TestUsageBillingModelCandidatesGPT61SolKeepsVersionIdentity(t *testing.T) {
	require.Equal(t,
		[]string{"openai/gpt-6.1-sol-max", "gpt-6.1-sol-max", "gpt-6.1-sol"},
		usageBillingModelCandidates("openai/gpt-6.1-sol-max"),
	)
	require.True(t, isOpenAIGPT6Model("gpt-6.1-sol"))
	require.False(t, isOpenAIGPT6AstraModel("gpt-6.1-sol"))
	for _, unknown := range []string{"gpt-6.1", "gpt-6.1-luna", "gpt-6.1-sol-preview", "gpt-6.1-solitude"} {
		require.Empty(t, normalizeKnownOpenAICodexModel(unknown), unknown)
	}
}

func TestNormalizeKnownOpenAICodexModel_BareGPT56RoutesToSol(t *testing.T) {
	tests := map[string]string{
		"gpt-5.6":            "gpt-5.6-sol",
		"openai/gpt-5.6":     "gpt-5.6-sol",
		"gpt5.6":             "gpt-5.6-sol",
		"gpt-5.6-high":       "gpt-5.6-sol",
		"gpt-5.6-max":        "gpt-5.6-sol",
		"gpt-5.6-2026-07-09": "gpt-5.6-sol",
		"openai/gpt-5.6-max": "gpt-5.6-sol",
	}

	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, expected, normalizeKnownOpenAICodexModel(input))
		})
	}
}

func TestUsageBillingModelCandidates_BareGPT56IncludesSol(t *testing.T) {
	require.Equal(t,
		[]string{"gpt-5.6", "gpt-5.6-sol"},
		usageBillingModelCandidates("gpt-5.6"),
	)
	require.Equal(t,
		[]string{"openai/gpt-5.6", "gpt-5.6", "gpt-5.6-sol"},
		usageBillingModelCandidates("openai/gpt-5.6"),
	)
}
