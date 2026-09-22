package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A Turn-State is scoped by the final upstream model, not by the broad model
// family used by billing/model-alias helpers.  Reasoning/dated variants that
// happen to share a family must not make an echoed state reusable for one
// another.
func TestCodexTurnStateProvenanceDoesNotCrossKnownModelVariants(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(12001)
	c, _ := newTurnStateTestContext(t, 91, "model-variant-isolation")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 211)

	svc.noteOpenAICodexTurnStateProvenance(c, account, state, "gpt-5.6-sol-high")
	echo := make(http.Header)
	echo.Set(openAICodexTurnStateHeader, state)
	svc.guardOpenAICodexTurnStateEcho(c, account, echo, "gpt-5.6-sol-low")

	require.Empty(t, echo.Get(openAICodexTurnStateHeader))
}

func TestCodexTurnStateProvenanceKeepsExplicitVariantsInSeparateShards(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(12002)
	c, _ := newTurnStateTestContext(t, 92, "model-variant-shards")
	high := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 212)
	low := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 213)

	svc.noteOpenAICodexTurnStateProvenance(c, account, high, "gpt-5.6-sol-high")
	svc.noteOpenAICodexTurnStateProvenance(c, account, low, "gpt-5.6-sol-low")

	seed := openAICodexTurnStateSeed(c)
	highKey := openAICodexTurnStateOriginModelKey(seed, "gpt-5.6-sol-high")
	lowKey := openAICodexTurnStateOriginModelKey(seed, "gpt-5.6-sol-low")
	require.NotEqual(t, highKey, lowKey)
	_, highOK := svc.openaiCodexTurnStateOrigins.Load(highKey)
	_, lowOK := svc.openaiCodexTurnStateOrigins.Load(lowKey)
	require.True(t, highOK)
	require.True(t, lowOK)

	for _, tc := range []struct {
		model string
		state string
	}{
		{model: "gpt-5.6-sol-high", state: high},
		{model: "gpt-5.6-sol-low", state: low},
	} {
		echo := http.Header{}
		echo.Set(openAICodexTurnStateHeader, tc.state)
		svc.guardOpenAICodexTurnStateEcho(c, account, echo, tc.model)
		require.Equal(t, tc.state, echo.Get(openAICodexTurnStateHeader), tc.model)
	}

	cross := http.Header{}
	cross.Set(openAICodexTurnStateHeader, high)
	svc.guardOpenAICodexTurnStateEcho(c, account, cross, "gpt-5.6-sol-low")
	require.Empty(t, cross.Get(openAICodexTurnStateHeader))
}

func TestCodexTurnStateResponseModelMismatchRejectsExplicitVariants(t *testing.T) {
	require.True(t, codexTurnStateResponseModelMismatch("gpt-5.6-sol-high", "gpt-5.6-sol-low", false))
	require.True(t, codexTurnStateResponseModelMismatch("gpt-5.6-sol-2026-09-18", "gpt-5.6-sol-2026-09-19", false))
	require.False(t, codexTurnStateResponseModelMismatch("gpt-6", "gpt-6-astra", false))
	require.False(t, codexTurnStateResponseModelMismatch("gpt-5.6", "gpt-5.6-sol", false))
}

func TestCodexTurnStateProbeRejectsExplicitModelVariantMismatch(t *testing.T) {
	require.False(t, codexTurnStateProbeModelsMatch("gpt-5.6-sol-high", []string{"gpt-5.6-sol-low"}))
	require.False(t, codexTurnStateProbeModelsMatch("gpt-5.6-sol-2026-09-18", []string{"gpt-5.6-sol-2026-09-19"}))
	require.True(t, codexTurnStateProbeModelsMatch("gpt-5.6-sol-high", []string{"gpt-5.6-sol-high"}))
	require.True(t, codexTurnStateProbeModelsMatch("gpt-6", []string{"gpt-6-astra"}))
}
