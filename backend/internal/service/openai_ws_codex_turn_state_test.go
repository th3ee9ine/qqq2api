package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveOpenAIWSCodexTurnStatePriority(t *testing.T) {
	now := time.Now().UTC()
	account := codexTurnStateGatewayTestAccount(1101)
	model := "gpt-5.5"

	t.Run("valid native state wins over cached state", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c := newCodexTurnStateGatewayTestContext(t, "ws-native-priority")
		cached := collectorTestToken(t, now.Add(-2*time.Minute), 2, 1)
		native := collectorTestToken(t, now.Add(-time.Minute), 2, 2)
		require.True(t, svc.codexTurnStateCollector.OfferValueMust(svc.codexTurnStateKey(c, account, model), cached, "cached", now))

		selected := svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, model, native, true)

		require.Equal(t, native, selected)
		binding, ok := svc.codexTurnStateBinding(c, account, model)
		require.True(t, ok)
		require.False(t, binding.used)
		require.Equal(t, "client", binding.snapshot.Route)
	})

	t.Run("valid native state is preserved when injection is disabled", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c := newCodexTurnStateGatewayTestContext(t, "ws-store-priority")
		cached := collectorTestToken(t, now.Add(-2*time.Minute), 2, 3)
		native := collectorTestToken(t, now.Add(-time.Minute), 2, 4)
		require.True(t, svc.codexTurnStateCollector.OfferValueMust(svc.codexTurnStateKey(c, account, model), cached, "cached", now))
		svc.SetCodexTurnStateRuntimeSettings(true, false)

		selected := svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, model, native, true)

		require.Equal(t, native, selected)
	})

	t.Run("invalid native state is stripped without cached fallback", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c := newCodexTurnStateGatewayTestContext(t, "ws-invalid-native")
		cached := collectorTestToken(t, now.Add(-time.Minute), 2, 5)
		require.True(t, svc.codexTurnStateCollector.OfferValueMust(svc.codexTurnStateKey(c, account, model), cached, "cached", now))
		c.Request.Header.Set(openAICodexTurnStateHeader, "invalid-state")

		selected := svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, model, "invalid-state", true)

		require.Empty(t, selected)
		require.Empty(t, c.Request.Header.Get(openAICodexTurnStateHeader))
		binding, ok := svc.codexTurnStateBinding(c, account, model)
		require.True(t, ok)
		require.False(t, binding.used)
		require.Empty(t, binding.snapshot.Token.Value)
	})

	t.Run("empty state only injects when injection is enabled", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c := newCodexTurnStateGatewayTestContext(t, "ws-empty-injection-gate")
		cached := collectorTestToken(t, now.Add(-time.Minute), 2, 6)
		require.True(t, svc.codexTurnStateCollector.OfferValueMust(svc.codexTurnStateKey(c, account, model), cached, "cached", now))

		svc.SetCodexTurnStateRuntimeSettings(true, false)
		require.Empty(t, svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, model, "", true))

		svc.SetCodexTurnStateRuntimeSettings(true, true)
		require.Equal(t, cached, svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, model, "", true))
	})
}

func TestOpenAIWSSessionTurnStateCommitGuardRejectsInvalidAndStaleGeneration(t *testing.T) {
	now := time.Now().UTC()
	svc := newCodexTurnStateGatewayTestService(nil)
	defer svc.CloseOpenAIWSPool()
	account := codexTurnStateGatewayTestAccount(1102)
	c := newCodexTurnStateGatewayTestContext(t, "ws-generation-guard")
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 7)

	require.Equal(t, state, svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, "gpt-5.5", state, false))
	require.True(t, svc.canCommitOpenAIWSSessionTurnState(c, account, state))
	require.False(t, svc.canCommitOpenAIWSSessionTurnState(c, account, "invalid-state"))

	svc.InvalidateOpenAIAccountRuntimeState(account.ID)
	require.False(t, svc.canCommitOpenAIWSSessionTurnState(c, account, state), "an in-flight request from the revoked generation must not republish state")
}
