package openai

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultModelsIncludeBareGPT56Alias(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-5.6")
}

func TestDefaultModelsIncludeGPT6Astra(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-6-astra")
	require.Contains(t, DefaultModelIDs(), "gpt-6")
	var displayName string
	for _, model := range DefaultModels {
		if model.ID == "gpt-6-astra" {
			displayName = model.DisplayName
			break
		}
	}
	require.Equal(t, "GPT-6 Astra", displayName)
}

func TestDefaultModelsPreferConcreteGPT56SolForAccountTests(t *testing.T) {
	require.NotEmpty(t, DefaultModels)
	require.Equal(t, "gpt-5.6-sol", DefaultModels[0].ID)
}

func TestDefaultModelsIncludeGPTImage25(t *testing.T) {
	require.Contains(t, DefaultModelIDs(), "gpt-image-2.5-flare")
	require.Contains(t, DefaultModelIDs(), "gpt-image-2.5-sunburst")
}

func TestGPT6SolLunaModelIdentity(t *testing.T) {
	for _, model := range []string{"gpt-6.1-sol", "gpt-6-sol", "gpt-6-luna"} {
		require.Contains(t, DefaultModelIDs(), model)
		for _, suffix := range []string{"", "-none", "-low", "-medium", "-high", "-xhigh", "-max", "-openai-compact"} {
			spelling := "openai/" + model + suffix
			require.True(t, IsGPT6SolOrLunaModelSpelling(spelling), spelling)
			require.Equal(t, model, CanonicalGPT6SolOrLunaModel(spelling), spelling)
		}
	}
	for _, model := range []string{"gpt-6-astra", "gpt-6-solitude", "gpt-6-luna-preview", "gpt-6.1", "gpt-6.1-luna", "gpt-6.1-sol-preview", "gpt-6.1-solitude", "gpt-6.10-sol"} {
		require.False(t, IsGPT6SolOrLunaModelSpelling(model), model)
		require.Empty(t, CanonicalGPT6SolOrLunaModel(model), model)
	}
	require.Equal(t, "gpt-6.1-sol", CanonicalGPT6SolOrLunaModel(" OPENAI/GPT-6.1_SOL_HIGH "))
}

func TestGPT6NoneReasoningSupportExcludesGPT61Sol(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "openai/gpt-6-luna-none"} {
		require.True(t, SupportsGPT6NoneReasoningEffort(model), model)
	}
	for _, model := range []string{"gpt-6.1-sol", "openai/gpt-6.1-sol-none", "gpt-6.1-sol-openai-compact", "gpt-6-astra", "unknown"} {
		require.False(t, SupportsGPT6NoneReasoningEffort(model), model)
	}
}
