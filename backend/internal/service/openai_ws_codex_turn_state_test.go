package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type openAIWSTurnStateAuthoritativeRepo struct {
	AccountRepository
	current *Account
}

type legacyOpenAIWSTurnStateStore struct {
	OpenAIWSStateStore
	state  string
	writes int
}

func (s *legacyOpenAIWSTurnStateStore) GetSessionTurnState(int64, int64, string, ...string) (string, bool) {
	return s.state, s.state != ""
}

func (s *legacyOpenAIWSTurnStateStore) BindSessionTurnState(_ int64, _ int64, _ string, state string, _ time.Duration, _ ...string) {
	s.state = state
	s.writes++
}

func (r *openAIWSTurnStateAuthoritativeRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.current, nil
}

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
		svc.noteOpenAICodexTurnStateProvenance(c, account, native, model)

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
		svc.noteOpenAICodexTurnStateProvenance(c, account, native, model)
		svc.SetCodexTurnStateRuntimeSettings(true, false)

		selected := svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, model, native, true)

		require.Equal(t, native, selected)
	})

	t.Run("valid but unproven native state is stripped", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c := newCodexTurnStateGatewayTestContext(t, "ws-native-unproven")
		native := collectorTestToken(t, now.Add(-time.Minute), 2, 7)
		c.Request.Header.Set(openAIWSTurnStateHeader, native)

		selected := svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, model, native, false)

		require.Empty(t, selected)
		require.Empty(t, c.Request.Header.Get(openAIWSTurnStateHeader))
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

func TestResolveOpenAIWSCodexTurnStateRejectsNativeStateFromAnotherModel(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1104)
	c := newCodexTurnStateGatewayTestContext(t, "ws-model-provenance")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 90)
	c.Request.Header.Set(openAIWSTurnStateHeader, state)

	// Simulate a state that was previously committed to this execution scope
	// while the upstream model was gpt-4. The next attempt is for gpt-5.5.
	svc.noteOpenAICodexTurnStateProvenance(c, account, state, "gpt-4")

	selected := svc.resolveOpenAIWSCodexTurnState(
		context.Background(), c, account, "gpt-5.5", state, false,
	)

	require.Empty(t, selected)
	require.Empty(t, c.Request.Header.Get(openAIWSTurnStateHeader))
}

func TestOpenAIWSSessionTurnStateRefreshWindow(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1103)
	now := time.Now().UTC()
	// The test policy is a ten-minute TTL with a two-minute refresh window.
	// A state older than eight minutes must not be reused from the sticky store.
	due := collectorTestToken(t, now.Add(-9*time.Minute), 2, 80)
	fresh := collectorTestToken(t, now.Add(-time.Minute), 2, 81)
	require.True(t, svc.openAIWSSessionTurnStateNeedsRefresh(account, due))
	require.False(t, svc.openAIWSSessionTurnStateNeedsRefresh(account, fresh))
}

func TestOpenAIWSSessionTurnStateLegacyStoreFailsClosed(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1106)
	c := newCodexTurnStateGatewayTestContext(t, "ws-legacy-store")
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 82)
	store := &legacyOpenAIWSTurnStateStore{state: state}

	loaded, ok := svc.getOpenAIWSSessionTurnState(c, store, 1, account, "ws-legacy-store", "gpt-5.5")
	require.False(t, ok)
	require.Empty(t, loaded)
	require.False(t, svc.bindOpenAIWSSessionTurnStateIfRefreshNeeded(c, store, 1, account, "ws-legacy-store", "gpt-5.5", state))
	require.Zero(t, store.writes, "a store without generation fencing must not receive OAuth/Codex Turn-State")
}

func TestOpenAIWSSessionTurnStateCommitGuardRejectsInvalidAndStaleGeneration(t *testing.T) {
	now := time.Now().UTC()
	svc := newCodexTurnStateGatewayTestService(nil)
	defer svc.CloseOpenAIWSPool()
	account := codexTurnStateGatewayTestAccount(1102)
	c := newCodexTurnStateGatewayTestContext(t, "ws-generation-guard")
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 7)
	svc.noteOpenAICodexTurnStateProvenance(c, account, state, "gpt-5.5")

	require.Equal(t, state, svc.resolveOpenAIWSCodexTurnState(context.Background(), c, account, "gpt-5.5", state, false))
	require.True(t, svc.canCommitOpenAIWSSessionTurnState(context.Background(), c, account, "gpt-5.5", state))
	require.False(t, svc.canCommitOpenAIWSSessionTurnState(context.Background(), c, account, "gpt-5.5", "invalid-state"))
	account.Schedulable = false
	require.False(t, svc.canCommitOpenAIWSSessionTurnState(context.Background(), c, account, "gpt-5.5", state), "an account that is no longer schedulable must not promote native state into the session store")
	account.Schedulable = true

	svc.InvalidateOpenAIAccountRuntimeState(account.ID)
	require.False(t, svc.canCommitOpenAIWSSessionTurnState(context.Background(), c, account, "gpt-5.5", state), "an in-flight request from the revoked generation must not republish state")
}

func TestOpenAIWSSessionTurnStateCommitGuardRequiresAuthoritativeIdentity(t *testing.T) {
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 8)
	selected := codexTurnStateGatewayTestAccount(1105)
	selected.Credentials = map[string]any{
		"access_token":       "old-access-token",
		"chatgpt_account_id": "old-chatgpt-account",
	}

	t.Run("same id OAuth identity replacement", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		current := *selected
		current.Credentials = map[string]any{
			"access_token":       "new-access-token",
			"chatgpt_account_id": "new-chatgpt-account",
		}
		svc.accountRepo = &openAIWSTurnStateAuthoritativeRepo{current: &current}
		c := newCodexTurnStateGatewayTestContext(t, "ws-authoritative-identity")

		require.False(t, svc.canCommitOpenAIWSSessionTurnState(context.Background(), c, selected, "gpt-5.5", state))
	})

	t.Run("deleted account", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		svc.accountRepo = &openAIWSTurnStateAuthoritativeRepo{}
		c := newCodexTurnStateGatewayTestContext(t, "ws-authoritative-deleted")

		require.False(t, svc.canCommitOpenAIWSSessionTurnState(context.Background(), c, selected, "gpt-5.5", state))
	})
}
