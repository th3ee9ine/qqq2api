package service

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func TestNewModelPricingCatalogFallbackAndContext(t *testing.T) {
	data, err := os.ReadFile("../../resources/model-pricing/model_prices_and_context_window.json")
	require.NoError(t, err)
	catalog := &PricingService{}
	catalog.pricingData, err = catalog.parsePricingData(data)
	require.NoError(t, err)
	sources := map[string]*BillingService{
		"billing fallback": NewBillingService(&config.Config{}, nil),
		"pricing fallback": NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
			"gpt-6":         {InputCostPerToken: 10e-6, OutputCostPerToken: 50e-6},
			"claude-opus-5": {InputCostPerToken: 5e-6, OutputCostPerToken: 25e-6},
		}}),
		"catalog": NewBillingService(&config.Config{}, catalog),
	}
	for source, svc := range sources {
		for _, tc := range []struct {
			model                      string
			input, output, write, read float64
		}{
			{"gpt-6-sol", 2e-6, 10e-6, 2.5e-6, 0.2e-6},
			{"gpt-6-luna", 0.1e-6, 0.5e-6, 0.125e-6, 0.01e-6},
		} {
			t.Run(source+"/"+tc.model, func(t *testing.T) {
				for _, n := range []int{271999, 272000, 272001} {
					for tier, mult := range map[string]float64{"": 1, "priority": 2, "fast": 2, "flex": 0.5} {
						tokens := UsageTokens{InputTokens: n - 3000, CacheReadTokens: 2000, CacheCreationTokens: 1000, OutputTokens: 500}
						cost, err := svc.CalculateCostWithServiceTier(tc.model, tokens, 1, tier)
						require.NoError(t, err)
						im, om := 1.0, 1.0
						if n > 272000 {
							im = 2
							om = 1.5
						}
						require.InDelta(t, float64(tokens.InputTokens)*tc.input*im*mult, cost.InputCost, 1e-10)
						require.InDelta(t, 1000*tc.write*im*mult, cost.CacheCreationCost, 1e-10)
						require.InDelta(t, 2000*tc.read*im*mult, cost.CacheReadCost, 1e-10)
						require.InDelta(t, 500*tc.output*om*mult, cost.OutputCost, 1e-10)
						require.InDelta(t, cost.InputCost+cost.OutputCost+cost.CacheCreationCost+cost.CacheReadCost, cost.TotalCost, 1e-10)
						require.Equal(t, n > 272000, cost.LongContextBillingApplied)
					}
				}
			})
		}
		t.Run(source+"/opus", func(t *testing.T) {
			tokens := UsageTokens{InputTokens: 300000, OutputTokens: 500, CacheReadTokens: 1000, CacheCreationTokens: 1000, CacheCreation5mTokens: 400, CacheCreation1hTokens: 600}
			for tier, mult := range map[string]float64{"": 1, "fast": 2} {
				cost, err := svc.CalculateCostWithServiceTier("claude-opus-5-5", tokens, 1, tier)
				require.NoError(t, err)
				require.InDelta(t, 1.2*mult, cost.InputCost, 1e-10)
				require.InDelta(t, (400*5e-6+600*8e-6)*mult, cost.CacheCreationCost, 1e-10)
				require.InDelta(t, 1000*0.2e-6*mult, cost.CacheReadCost, 1e-10)
				require.InDelta(t, 500*20e-6*mult, cost.OutputCost, 1e-10)
				require.False(t, cost.LongContextBillingApplied)
			}
		})
	}
}

func TestNewModelPricingChannelOverridesAndFamilyIsolation(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	for _, model := range []string{"gpt-6-sol", "gpt-6-luna", "claude-opus-5-5"} {
		t.Run(model, func(t *testing.T) {
			zero := 0.0
			prices, err := svc.GetModelPricingWithChannel(model, &ChannelModelPricing{InputPrice: &zero, OutputPrice: &zero, CacheWritePrice: &zero, CacheReadPrice: &zero})
			require.NoError(t, err)
			cost := svc.computeTokenBreakdown(prices, UsageTokens{InputTokens: 300000, CacheReadTokens: 1000, CacheCreationTokens: 1000, OutputTokens: 1000}, 1, "priority", true)
			require.Zero(t, cost.TotalCost)
			fresh, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.Positive(t, fresh.InputPricePerToken)
		})
	}
	prices, err := svc.GetModelPricing("claude-opus-5")
	require.NoError(t, err)
	require.Equal(t, 5e-6, prices.InputPricePerToken)
	prices, err = svc.GetModelPricing("gpt-6")
	require.NoError(t, err)
	require.Equal(t, 10e-6, prices.InputPricePerToken)
	require.Equal(t, "gpt-6-sol", normalizeKnownOpenAICodexModel("openai/gpt-6-sol-max"))
	require.Equal(t, "gpt-6-luna", normalizeKnownOpenAICodexModel("gpt-6-luna-openai-compact"))
}

func TestNewModelPricingExplicitZeroCacheWrite(t *testing.T) {
	svc := &PricingService{}
	var err error
	svc.pricingData, err = svc.parsePricingData([]byte(`{"gpt-6-sol":{"litellm_provider":"openai","input_cost_per_token":0.000002,"output_cost_per_token":0.00001,"input_cost_per_token_flex":0.000001,"cache_creation_input_token_cost":0}}`))
	require.NoError(t, err)
	billing := NewBillingService(&config.Config{}, svc)
	for _, tier := range []string{"", "priority", "flex"} {
		cost, err := billing.CalculateCostWithServiceTier("gpt-6-sol", UsageTokens{CacheCreationTokens: 1000}, 1, tier)
		require.NoError(t, err)
		require.Zero(t, cost.CacheCreationCost)
	}
}

func TestNewModelPricingAliasesRetainExplicitOverrides(t *testing.T) {
	zero := &LiteLLMModelPricing{}
	svc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-6-sol": zero, "gpt-6-luna": zero, "claude-opus-5-5": zero,
	}}
	for _, model := range []string{"gpt-6-sol-max", "openai/gpt-6-luna-openai-compact", "claude-opus-5-5-thinking"} {
		require.Same(t, zero, svc.GetModelPricing(model))
	}
}
