package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func newOpenAICompatTurnStateTestService() *OpenAIGatewayService {
	svc := &OpenAIGatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				CodexTurnState: config.GatewayCodexTurnStateConfig{
					TTLSeconds:           3600,
					RefreshBeforeSeconds: 300,
					MaxTokenBytes:        2048,
				},
			},
		},
	}
	svc.initCodexTurnStateCollector()
	return svc
}

func TestOpenAICompatSessionTurnStateIsolatedByAccountAndModel(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := codexTurnStateGatewayTestAccount(101)
	otherAccount := codexTurnStateGatewayTestAccount(202)
	c, _ := newTurnStateTestContext(t, 77, "compat-session")
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 41)

	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, "gpt-5.4")
	require.Equal(t, state, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.4"))

	// A model switch must not reuse or leave the old state available.
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.5"))
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.4"))

	// Account identity remains part of the key even when the prompt cache key and
	// API-key/session context are shared.
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, "gpt-5.4")
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, otherAccount, "cache-key", "gpt-5.4"))
}

func TestOpenAICompatSessionTurnStateReusesAcrossContextsForSameAccountAndModel(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := codexTurnStateGatewayTestAccount(303)
	first, _ := newTurnStateTestContext(t, 88, "compat-ip-a")
	second, _ := newTurnStateTestContext(t, 88, "compat-ip-b")
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 42)

	svc.bindOpenAICompatSessionTurnState(context.Background(), first, account, "cache-key", state, "gpt-5.4")
	require.Equal(t, state, svc.getOpenAICompatSessionTurnState(context.Background(), second, account, "cache-key", "gpt-5.4"),
		"the compatibility key intentionally has no source-IP component")
}

func TestOpenAICompatSessionTurnStateTrustedInjectionIsExactlyScoped(t *testing.T) {
	setup := func(t *testing.T) (*OpenAIGatewayService, *gin.Context, *Account, string) {
		t.Helper()
		svc := newOpenAICompatTurnStateTestService()
		svc.codexTurnStateCollector = NewOpenAICodexTurnStateCollector(collectorTestPolicy())
		account := codexTurnStateGatewayTestAccount(305)
		c, _ := newTurnStateTestContext(t, 90, "compat-trusted")
		state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 57)
		svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, "gpt-5.4")
		require.Equal(t, state, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.4"))
		return svc, c, account, state
	}

	t.Run("same account model hash and generation is admitted", func(t *testing.T) {
		svc, c, account, state := setup(t)
		header := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state}}

		svc.guardOpenAICodexTurnStateEcho(c, account, header, "gpt-5.4")

		require.Equal(t, state, header.Get(openAICodexTurnStateHeader))
	})

	t.Run("different account is rejected", func(t *testing.T) {
		svc, c, _, state := setup(t)
		header := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state}}

		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateGatewayTestAccount(306), header, "gpt-5.4")

		require.Empty(t, header.Get(openAICodexTurnStateHeader))
	})

	t.Run("different model is rejected", func(t *testing.T) {
		svc, c, account, state := setup(t)
		header := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state}}

		svc.guardOpenAICodexTurnStateEcho(c, account, header, "gpt-5.5")

		require.Empty(t, header.Get(openAICodexTurnStateHeader))
	})

	t.Run("different state hash is rejected", func(t *testing.T) {
		svc, c, account, _ := setup(t)
		otherState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 58)
		header := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{otherState}}

		svc.guardOpenAICodexTurnStateEcho(c, account, header, "gpt-5.4")

		require.Empty(t, header.Get(openAICodexTurnStateHeader))
	})

	t.Run("stale generation is rejected", func(t *testing.T) {
		svc, c, account, state := setup(t)
		header := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state}}
		svc.InvalidateOpenAIAccountRuntimeState(account.ID)

		svc.guardOpenAICodexTurnStateEcho(c, account, header, "gpt-5.4")

		require.Empty(t, header.Get(openAICodexTurnStateHeader))
	})
}

func TestOpenAICompatSessionTurnStateRejectsStaleGenerationWriteAfterInvalidation(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	svc.codexTurnStateCollector = NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	account := codexTurnStateGatewayTestAccount(304)
	staleCtx, _ := newTurnStateTestContext(t, 89, "compat-stale")
	freshCtx, _ := newTurnStateTestContext(t, 89, "compat-fresh")
	model := "gpt-5.4"
	logicalKey := OpenAICodexTurnStateKey{AccountID: account.ID, Scope: "compat", Model: model}
	staleKey := svc.codexTurnStateCollector.BindKey(logicalKey)
	svc.bindCodexTurnStateRequest(staleCtx, staleKey, model, OpenAICodexTurnStateSnapshot{}, false)

	svc.InvalidateOpenAIAccountRuntimeState(account.ID)
	freshKey := svc.codexTurnStateCollector.BindKey(logicalKey)
	require.NotEqual(t, staleKey.generation, freshKey.generation)
	svc.bindCodexTurnStateRequest(freshCtx, freshKey, model, OpenAICodexTurnStateSnapshot{}, false)

	freshState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 55)
	staleState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 56)
	svc.bindOpenAICompatSessionTurnState(context.Background(), freshCtx, account, "cache-key", freshState, model)
	svc.bindOpenAICompatSessionTurnState(context.Background(), staleCtx, account, "cache-key", staleState, model)
	require.Equal(t, freshState, svc.getOpenAICompatSessionTurnState(context.Background(), freshCtx, account, "cache-key", model),
		"an old in-flight response must not overwrite the new runtime generation")

	// Reads also fail closed if a stale value somehow survives in the map, while
	// leaving the independently valid response-ID continuation intact.
	staleCacheKey := openAICompatSessionResponseKey(freshCtx, account, "stale-cache-key")
	svc.openaiCompatSessionResponses.Store(staleCacheKey, openAICompatSessionResponseBinding{
		ResponseID:          "resp-preserved",
		ResponseModel:       model,
		TurnState:           staleState,
		Model:               model,
		TurnStateGeneration: staleKey.generation,
		ExpiresAt:           time.Now().Add(time.Hour),
	})
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), freshCtx, account, "stale-cache-key", model))
	raw, ok := svc.openaiCompatSessionResponses.Load(staleCacheKey)
	require.True(t, ok)
	binding, ok := raw.(openAICompatSessionResponseBinding)
	require.True(t, ok)
	require.Equal(t, "resp-preserved", binding.ResponseID)
	require.Empty(t, binding.TurnState)
	require.Zero(t, binding.TurnStateGeneration)
}

func TestOpenAICompatSessionTurnStateKeepsHealthyBindingAcrossNewResponseTokens(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(390)
	c := newCodexTurnStateGatewayTestContext(t, "compat-stable-state")
	model := "gpt-5.5"
	first := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 61)
	second := collectorTestToken(t, time.Now().UTC(), 2, 62)

	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", first, model)
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", second, model)

	require.Equal(t, first, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", model),
		"a newly issued token must not slide a healthy binding's 55-minute window")
}

func TestOpenAICompatSessionTurnStateStaleWriterCannotClearReplacementGeneration(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(391)
	model := "gpt-5.5"
	staleCtx := newCodexTurnStateGatewayTestContext(t, "compat-generation-barrier")
	freshCtx := newCodexTurnStateGatewayTestContext(t, "compat-generation-barrier")
	logicalKey := OpenAICodexTurnStateKey{AccountID: account.ID, Scope: "compat-generation-barrier", Model: model}
	staleKey := svc.codexTurnStateCollector.BindKey(logicalKey)
	svc.bindCodexTurnStateRequest(staleCtx, staleKey, model, OpenAICodexTurnStateSnapshot{}, false)
	svc.InvalidateOpenAIAccountRuntimeState(account.ID)
	freshKey := svc.codexTurnStateCollector.BindKey(logicalKey)
	svc.bindCodexTurnStateRequest(freshCtx, freshKey, model, OpenAICodexTurnStateSnapshot{}, false)
	require.NotEqual(t, staleKey.generation, freshKey.generation)

	freshState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 63)
	svc.bindOpenAICompatSessionTurnState(context.Background(), freshCtx, account, "cache-key", freshState, model)
	svc.bindOpenAICompatSessionTurnState(context.Background(), staleCtx, account, "cache-key", "invalid-state", model)

	require.Equal(t, freshState, svc.getOpenAICompatSessionTurnState(context.Background(), freshCtx, account, "cache-key", model),
		"an invalid late response from G1 must not clear the G2 binding")
}

func TestOpenAICompatSessionTurnStateRejectsInvalidAndRefreshDueValues(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := codexTurnStateGatewayTestAccount(404)
	c, _ := newTurnStateTestContext(t, 99, "compat-session")

	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "invalid", "not-a-turn-state", "gpt-5.4")
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "invalid", "gpt-5.4"))

	refreshDue := collectorTestToken(t, time.Now().UTC().Add(-56*time.Minute), 2, 43)
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "refresh", refreshDue, "gpt-5.4")
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "refresh", "gpt-5.4"))

	// A state from a legacy binding with no model metadata is not returned to a
	// model-aware caller; this prevents an old cache entry crossing model lines.
	legacyKey := openAICompatSessionResponseKey(c, account, "legacy")
	svc.openaiCompatSessionResponses.Store(legacyKey, openAICompatSessionResponseBinding{
		TurnState: collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 44),
		ExpiresAt: time.Now().Add(time.Hour),
	})
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "legacy", "gpt-5.4"))
}

func TestOpenAICompatSessionTurnStateModelMismatchInvalidatesBinding(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := codexTurnStateGatewayTestAccount(505)
	c, _ := newTurnStateTestContext(t, 100, "compat-session")
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 45)
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, "gpt-5.4")

	beginUpstreamResponseModelObservation(c).Observe("gpt-5.5", true)
	require.True(t, openAICompatTurnStateResponseModelMismatch(c, "gpt-5.4"))
	svc.invalidateOpenAICompatSessionTurnStateOnModelMismatch(c, account, "cache-key")
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.4"))
}

func TestOpenAICompatSessionTurnStateModelObserverConflictInvalidatesBinding(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := codexTurnStateGatewayTestAccount(606)
	c, _ := newTurnStateTestContext(t, 101, "compat-session")
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 46)
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, "gpt-5.4")

	observer := beginUpstreamResponseModelObservation(c)
	observer.Observe("gpt-5.4", false)
	observer.Observe("gpt-5.5", true)
	require.True(t, openAICompatTurnStateResponseModelMismatch(c, "gpt-5.4"))
	svc.invalidateOpenAICompatSessionTurnStateOnModelMismatch(c, account, "cache-key")
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.4"))
}

func TestOpenAICompatResponseModelMismatchFromHTTPErrorBody(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 103, "compat-session")
	beginUpstreamResponseModelObservation(c)
	require.True(t, observeOpenAICompatResponseModelPayload(c, "gpt-5.4", []byte(`{"error":{"model":"gpt-5.5","message":"wrong model"}}`)))
}

func TestOpenAICompatSessionTurnStateInvalidResponseHeaderIsNotBound(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := codexTurnStateGatewayTestAccount(707)
	c, _ := newTurnStateTestContext(t, 102, "compat-session")
	resp := http.Header{openAICodexTurnStateHeader: []string{"unexpected"}}
	// The bind helper is the last line of defence for a response header that did
	// not pass the generic upstream guard.
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", resp.Get(openAICodexTurnStateHeader), "gpt-5.4")
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.4"))
}

func TestStageOpenAICompatSessionTurnStatePreservesNativeHeaderAndRestoresContext(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 104, "compat-session")
	c.Request.Header["x-codex-turn-state"] = []string{"native-state"}
	restore := stageOpenAICompatSessionTurnState(c, "cached-state")
	restore()
	require.Equal(t, []string{"native-state"}, c.Request.Header["x-codex-turn-state"], "native header must win")

	delete(c.Request.Header, "x-codex-turn-state")
	c.Request.Header["x-codex-turn-state"] = []string{""}
	restore = stageOpenAICompatSessionTurnState(c, "cached-state")
	require.Equal(t, "cached-state", c.Request.Header.Get(openAICodexTurnStateHeader))
	restore()
	require.Equal(t, []string{""}, c.Request.Header["x-codex-turn-state"], "temporary bridge must restore the original key/value")
}

func TestOpenAICompatSessionResponseIDIsolatedByModel(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := &Account{ID: 808, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	c, _ := newTurnStateTestContext(t, 105, "compat-response-id")
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 52)

	// A response ID and its state are retained for the same final model.
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, "gpt-5.4")
	svc.bindOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "resp-54", "gpt-5.4")
	require.Equal(t, "resp-54", svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.4"))

	// A model switch must not replay either previous_response_id or the
	// model-scoped Turn-State. The stale binding is retired atomically.
	require.Empty(t, svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.5"))
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.5"))
	require.Empty(t, svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.4"))

	// The new model can establish an independent continuation after the old
	// binding was retired.
	svc.bindOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "resp-55", "gpt-5.5")
	require.Equal(t, "resp-55", svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.5"))
}

func TestOpenAICompatSessionResponseIDLegacyBindingRemainsReadable(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := &Account{ID: 809, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	c, _ := newTurnStateTestContext(t, 106, "compat-response-id-legacy")

	// Older in-memory bindings have no response-model metadata. Keep the
	// response-ID continuation usable for those entries, while Turn-State
	// remains independently fail-closed when its model metadata is absent.
	svc.bindOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "resp-legacy")
	require.Equal(t, "resp-legacy", svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.4"))

	legacyState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 53)
	svc.openaiCompatSessionResponses.Store(openAICompatSessionResponseKey(c, account, "state-key"), openAICompatSessionResponseBinding{
		ResponseID: "resp-legacy-state",
		TurnState:  legacyState,
		ExpiresAt:  time.Now().Add(time.Hour),
	})
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "state-key", "gpt-5.4"))
}

func TestOpenAICompatSessionResponseIDReadsLegacyModelFieldAsScoped(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := &Account{ID: 812, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	c, _ := newTurnStateTestContext(t, 109, "compat-response-id-old-layout")
	key := openAICompatSessionResponseKey(c, account, "cache-key")
	svc.openaiCompatSessionResponses.Store(key, openAICompatSessionResponseBinding{
		ResponseID: "resp-old-layout",
		Model:      "gpt-5.4",
		ExpiresAt:  time.Now().Add(time.Hour),
	})
	require.Equal(t, "resp-old-layout", svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.4"))
	require.Empty(t, svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.5"))
}

func TestOpenAICompatSessionDisabledLegacyModelFieldIsScoped(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := &Account{ID: 813, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	c, _ := newTurnStateTestContext(t, 110, "compat-response-disabled-old-layout")
	key := openAICompatSessionResponseKey(c, account, "cache-key")
	svc.openaiCompatSessionResponses.Store(key, openAICompatSessionResponseBinding{
		Model:                "gpt-5.4",
		ContinuationDisabled: true,
		ExpiresAt:            time.Now().Add(time.Hour),
	})
	require.True(t, svc.isOpenAICompatSessionContinuationDisabled(context.Background(), c, account, "cache-key", "gpt-5.4"))
	require.False(t, svc.isOpenAICompatSessionContinuationDisabled(context.Background(), c, account, "cache-key", "gpt-5.5"))
}

func TestOpenAICompatSessionResponseIDDeleteKeepsOtherModelBinding(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := &Account{ID: 810, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	c, _ := newTurnStateTestContext(t, 107, "compat-response-id-delete")

	svc.bindOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "resp-55", "gpt-5.5")
	svc.deleteOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.4")
	require.Equal(t, "resp-55", svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.5"))

	svc.deleteOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.5")
	require.Empty(t, svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.5"))
}

func TestOpenAICompatSessionResponseIDReadDoesNotDropTurnState(t *testing.T) {
	svc := newOpenAICompatTurnStateTestService()
	account := &Account{ID: 811, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	c, _ := newTurnStateTestContext(t, 108, "compat-response-id-state")
	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 54)

	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, "gpt-5.4")
	// No response ID exists, but reading that empty continuation must not erase
	// the independently valid Turn-State binding.
	require.Empty(t, svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "gpt-5.4"))
	require.Equal(t, state, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", "gpt-5.4"))
}

func TestOpenAICompatSessionResponseMutationCannotOverwriteConcurrentTurnState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(8120)
	c := newCodexTurnStateGatewayTestContext(t, "compat-response-turn-state-cas")
	model := "gpt-5.5"
	key := openAICompatSessionResponseKey(c, account, "cache-key")

	svc.bindOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "resp-old", model)
	raw, loaded := svc.openaiCompatSessionResponses.Load(key)
	require.True(t, loaded)
	oldBinding, ok := raw.(openAICompatSessionResponseBinding)
	require.True(t, ok)

	state := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 64)
	svc.bindOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", state, model)
	staleMutation := oldBinding
	staleMutation.ResponseID = "resp-stale-writer"
	require.False(t, svc.openaiCompatSessionResponses.CompareAndSwap(key, oldBinding, staleMutation),
		"a ResponseID mutation based on the old snapshot must retry instead of overwriting Turn-State")

	svc.bindOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", "resp-new", model)
	require.Equal(t, "resp-new", svc.getOpenAICompatSessionResponseID(context.Background(), c, account, "cache-key", model))
	require.Equal(t, state, svc.getOpenAICompatSessionTurnState(context.Background(), c, account, "cache-key", model))
}

func TestForwardAsAnthropicRefreshesTurnStateAfterResponseModelMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	firstState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 47)
	secondState := collectorTestToken(t, time.Now().UTC().Add(-time.Minute), 2, 48)
	first := openAICompatSSECompletedResponse("resp_mismatch", "gpt-5.5")
	first.Header.Set(openAICodexTurnStateHeader, firstState)
	second := openAICompatSSECompletedResponse("resp_recovered", "gpt-5.4")
	second.Header.Set(openAICodexTurnStateHeader, secondState)
	third := openAICompatSSECompletedResponse("resp_reused", "gpt-5.4")
	upstream := &httpUpstreamRecorder{responses: []*http.Response{first, second, third}}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
	svc.initCodexTurnStateCollector()
	svc.SetCodexTurnStateRuntimeSettings(false, true)
	account := openAISetupTokenCompatAccount(808)
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)

	forward := func() {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "stable-cache-key", "gpt-5.4")
		require.NoError(t, err)
		require.NotNil(t, result)
	}

	forward()
	// The mismatched response must not populate the compatibility cache.
	require.Empty(t, svc.getOpenAICompatSessionTurnState(context.Background(), nil, account, "stable-cache-key", "gpt-5.4"))
	require.Empty(t, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))

	forward()
	// No stale state is sent while the previous lineage is being refreshed.
	require.Empty(t, upstream.requests[1].Header.Get(openAICodexTurnStateHeader))
	require.Equal(t, secondState, svc.getOpenAICompatSessionTurnState(context.Background(), nil, account, "stable-cache-key", "gpt-5.4"))

	forward()
	require.Equal(t, secondState, upstream.requests[2].Header.Get(openAICodexTurnStateHeader))
}
