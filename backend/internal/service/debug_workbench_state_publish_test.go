package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDebugWorkbenchVerificationBudgetAnd429(t *testing.T) {
	s := &DebugWorkbenchService{}
	daily := &Account{ID: 3}
	input := DebugWorkbenchRequest{VerificationStage: DebugVerificationStageBaseline, Body: json.RawMessage(`{"model":"gpt-6-astra"}`)}
	require.NoError(t, s.reserveDebugWorkbenchVerification(9, daily, daily, input))
	baseline := DebugStateVerification{RequestedModel: "gpt-6-astra", ResponseCreatedModel: "gpt-5.6-luna", ResponseCompletedModel: "gpt-5.6-luna", UpstreamResponseModel: "gpt-5.6-luna", UsageLogVerified: true, ActualAccountID: 3, UsageLogAccountID: 3, UsageLogAPIKeyID: 9, UsageLogRequestedModel: "gpt-6-astra"}
	s.noteDebugWorkbenchVerificationBaseline(9, 3, &baseline)
	account := *daily
	proxyID := int64(2)
	account.ProxyID = &proxyID
	input.VerificationStage = DebugVerificationStageCapture
	for i := 0; i < 3; i++ {
		account.Proxy = &Proxy{Protocol: "socks5", Host: "us.1024proxy.io", Port: 3000, Username: fmt.Sprintf("test-region-US-sid-session%d-t-5", i), Password: "test-password"}
		require.NoError(t, s.reserveDebugWorkbenchVerification(9, &account, daily, input))
		if i == 0 {
			require.Error(t, s.reserveDebugWorkbenchVerification(9, &account, daily, input), "one sticky session may be captured only once")
		}
	}
	account.Proxy = &Proxy{Protocol: "socks5", Host: "us.1024proxy.io", Port: 3000, Username: "test-region-US-sid-session4-t-5", Password: "test-password"}
	require.Error(t, s.reserveDebugWorkbenchVerification(9, &account, daily, input))
	input.VerificationStage = DebugVerificationStageBaseline
	require.NoError(t, s.reserveDebugWorkbenchVerification(9, daily, daily, input))
	s.noteDebugWorkbenchVerificationBaseline(9, 3, &DebugStateVerification{RequestedModel: "gpt-6-astra", ResponseCreatedModel: "gpt-5.6-luna", ResponseCompletedModel: "gpt-5.6-luna", UpstreamResponseModel: "gpt-5.6-luna", UsageLogVerified: true, ActualAccountID: 3, UsageLogAccountID: 3, UsageLogAPIKeyID: 9, UsageLogRequestedModel: "gpt-6-astra"})
	input.VerificationStage = DebugVerificationStageCapture
	require.Error(t, s.reserveDebugWorkbenchVerification(9, &account, daily, input), "baseline cannot reset the budget")
	s.noteDebugWorkbenchVerificationLimit(9, 3, []DebugUpstreamAttempt{{Response: &DebugHTTPSnapshot{StatusCode: 429, Headers: map[string][]string{"Retry-After": {"60"}}}}})
	input.VerificationStage = DebugVerificationStageBaseline
	require.Error(t, s.reserveDebugWorkbenchVerification(9, daily, daily, input), "429 blocks even baseline without rotating")
	budget := s.verificationBudgets[debugWorkbenchVerificationBudgetKey{3, 9}]
	require.True(t, budget.notBefore.After(time.Now().Add(55*time.Second)))
}

func TestDebugWorkbenchBaselineRequiresDurableLifecycleEvidence(t *testing.T) {
	valid := DebugStateVerification{RequestedModel: "gpt-6-astra", ResponseCreatedModel: "gpt-5.6-luna", ResponseCompletedModel: "gpt-5.6-luna", UpstreamResponseModel: "gpt-5.6-luna", UsageLogVerified: true, ActualAccountID: 3, UsageLogAccountID: 3, UsageLogAPIKeyID: 9, UsageLogRequestedModel: "gpt-6-astra"}
	require.True(t, debugWorkbenchVerifiedBaseline(&valid, 3, 9), "a real Luna baseline is diagnostic evidence")
	for _, mutate := range []func(*DebugStateVerification){
		func(e *DebugStateVerification) { e.ResponseCreatedModel = "" },
		func(e *DebugStateVerification) { e.ResponseCompletedModel = "gpt-6-astra" },
		func(e *DebugStateVerification) { e.UpstreamResponseModel = "gpt-6-astra" },
		func(e *DebugStateVerification) { e.UsageLogVerified = false },
		func(e *DebugStateVerification) { e.UsageLogAPIKeyID = 10 },
		func(e *DebugStateVerification) { e.StateSent = true },
	} {
		copy := valid
		mutate(&copy)
		require.False(t, debugWorkbenchVerifiedBaseline(&copy, 3, 9))
	}
}

func TestDebugWorkbenchRejectsRandomCountryAndWrongRoutes(t *testing.T) {
	s := &DebugWorkbenchService{}
	daily := &Account{ID: 3}
	account := *daily
	proxyID := int64(2)
	account.ProxyID = &proxyID
	input := DebugWorkbenchRequest{VerificationStage: DebugVerificationStageCapture, Body: json.RawMessage(`{"model":"gpt-6-astra"}`)}
	require.Error(t, s.reserveDebugWorkbenchVerification(9, &account, daily, input), "baseline required")
	input.VerificationStage = DebugVerificationStageBaseline
	require.NoError(t, s.reserveDebugWorkbenchVerification(9, daily, daily, input))
	account.Proxy = &Proxy{Protocol: "socks5", Host: "us.1024proxy.io", Port: 3000, Username: "test-region-Rand-sid-session-t-5"}
	input.VerificationStage = DebugVerificationStageCapture
	require.Error(t, s.reserveDebugWorkbenchVerification(9, &account, daily, input))
	input.VerificationStage = DebugVerificationStageAutomatic
	require.Error(t, s.reserveDebugWorkbenchVerification(9, &account, daily, input))
}

func TestDebugWorkbenchReplayEvidenceRequiresCompleteScopeAndRawVariants(t *testing.T) {
	base := DebugStateVerification{RequestedModel: "gpt-6-astra", ResponseCreatedModel: "provider/gpt-6-astra-2026-09-18", ResponseCompletedModel: "gpt-6-astra-build-42", UpstreamResponseModel: "gpt-6-astra-build-42", StateSent: true, StateMatchesCapture: true, UsageLogStateSent: true, UsageLogVerified: true, ActualAccountID: 3, UsageLogAccountID: 3, UsageLogAPIKeyID: 9, UsageLogRequestedModel: "gpt-6-astra"}
	require.True(t, debugWorkbenchVerifiedReplay(&base, 3, 9))
	for _, mutate := range []func(*DebugStateVerification){
		func(e *DebugStateVerification) { e.ResponseCompletedModel = "gpt-5.6-luna" },
		func(e *DebugStateVerification) { e.UsageLogAccountID = 4 },
		func(e *DebugStateVerification) { e.UsageLogAPIKeyID = 10 },
		func(e *DebugStateVerification) { e.StateMatchesCapture = false },
		func(e *DebugStateVerification) { e.UsageLogVerified = false },
		func(e *DebugStateVerification) { e.UpstreamResponseModel = "gpt-6-astra" },
	} {
		copy := base
		mutate(&copy)
		require.False(t, debugWorkbenchVerifiedReplay(&copy, 3, 9))
	}
}

type debugAtomicStateRepo struct {
	AccountRepository
	updates map[string]any
	updated bool
	err     error
}

func (r *debugAtomicStateRepo) UpdateCodexTurnState(_ context.Context, _ int64, _ string, value map[string]any) (bool, error) {
	r.updates = value
	return r.updated, r.err
}

func TestDebugWorkbenchStatePublicationIsDurableAndPreservesCollectionAge(t *testing.T) {
	gateway, _, account := newTurnStateAutoService(t)
	repo := &debugAtomicStateRepo{updated: true}
	gateway.accountRepo = repo
	s := &DebugWorkbenchService{gateway: gateway}
	collected := time.Now().Add(-time.Minute)
	evidence := validDebugWorkbenchStatePublicationEvidence(account.ID, 9, "gpt-6-astra", "verified-candidate", collected)
	require.NoError(t, s.publishDebugWorkbenchState(context.Background(), account, evidence))
	entry := gateway.openaiTurnStates[codexTurnStateKey{account.ID, "gpt-6-astra"}]
	require.Equal(t, "verified-candidate", entry.token)
	require.Equal(t, collected.UnixMilli(), entry.setAt)
	require.Equal(t, collected.UnixMilli(), repo.updates[CodexTurnStateAutoSetAtExtraKey])
	repo.err = errors.New("storage failed")
	require.Error(t, s.publishDebugWorkbenchState(context.Background(), account, validDebugWorkbenchStatePublicationEvidence(account.ID, 9, "gpt-6-astra", "failed-candidate", time.Now())))
	require.Equal(t, "verified-candidate", entry.token)
	repo.err, repo.updated = nil, false
	require.Error(t, s.publishDebugWorkbenchState(context.Background(), account, validDebugWorkbenchStatePublicationEvidence(account.ID, 9, "gpt-6-astra", "racing-candidate", time.Now())))
	require.Equal(t, "verified-candidate", entry.token)
	luna := validDebugWorkbenchStatePublicationEvidence(account.ID, 9, "gpt-6-astra", "luna-candidate", time.Now())
	setDebugWorkbenchPublicationModels(&luna.replay, "gpt-5.6-luna", "gpt-5.6-luna", "gpt-5.6-luna")
	require.Error(t, s.publishDebugWorkbenchState(context.Background(), account, luna))
	require.Equal(t, "verified-candidate", entry.token)
}

func TestDebugWorkbenchStatePublicationRequiresCompleteReplayEvidence(t *testing.T) {
	accountID, apiKeyID := int64(3), int64(9)
	base := validDebugWorkbenchStatePublicationEvidence(accountID, apiKeyID, "gpt-6-astra", "candidate-state", time.Now())
	tests := []struct {
		name   string
		mutate func(*debugWorkbenchStatePublicationEvidence)
	}{
		{name: "missing created", mutate: func(e *debugWorkbenchStatePublicationEvidence) {
			e.replay.result.CodexTurnStateResponseCreatedModel = ""
		}},
		{name: "missing completed", mutate: func(e *debugWorkbenchStatePublicationEvidence) {
			e.replay.result.CodexTurnStateResponseCompletedModel = ""
		}},
		{name: "missing usage model", mutate: func(e *debugWorkbenchStatePublicationEvidence) { e.replay.verification.UpstreamResponseModel = "" }},
		{name: "response failed", mutate: func(e *debugWorkbenchStatePublicationEvidence) { e.replay.result.CodexTurnStateResponseFailed = true }},
		{name: "response conflict", mutate: func(e *debugWorkbenchStatePublicationEvidence) { e.replay.result.UpstreamResponseModelConflict = true }},
		{name: "luna terminal", mutate: func(e *debugWorkbenchStatePublicationEvidence) {
			setDebugWorkbenchPublicationModels(&e.replay, "gpt-5.6-luna", "gpt-5.6-luna", "gpt-5.6-luna")
		}},
		{name: "wrong account", mutate: func(e *debugWorkbenchStatePublicationEvidence) {
			e.replay.verification.UsageLogAccountID = accountID + 1
		}},
		{name: "wrong api key", mutate: func(e *debugWorkbenchStatePublicationEvidence) { e.replay.verification.UsageLogAPIKeyID = apiKeyID + 1 }},
		{name: "wrong state", mutate: func(e *debugWorkbenchStatePublicationEvidence) { *e.replay.result.UpstreamTurnState = "other-state" }},
		{name: "daily route not verified", mutate: func(e *debugWorkbenchStatePublicationEvidence) { e.dailyRouteVerified = false }},
		{name: "missing daily evidence", mutate: func(e *debugWorkbenchStatePublicationEvidence) {
			e.dailyReplay = debugWorkbenchStateReplayPublicationEvidence{}
		}},
		{name: "daily replay failed", mutate: func(e *debugWorkbenchStatePublicationEvidence) {
			e.dailyReplay.result.CodexTurnStateResponseFailed = true
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := cloneDebugWorkbenchStatePublicationEvidence(base)
			tt.mutate(&evidence)
			gateway, _, account := newTurnStateAutoService(t)
			repo := &debugAtomicStateRepo{updated: true}
			gateway.accountRepo = repo
			s := &DebugWorkbenchService{gateway: gateway}

			require.Error(t, s.publishDebugWorkbenchState(context.Background(), account, evidence))
			require.Nil(t, repo.updates)
		})
	}
}

func TestDebugWorkbenchStatePublicationAcceptsFamilyVariantsWithoutRewriting(t *testing.T) {
	tests := []struct {
		name           string
		requested      string
		replayCreated  string
		replayTerminal string
		dailyCreated   string
		dailyTerminal  string
	}{
		{name: "astra", requested: "gpt-6-astra", replayCreated: "provider/gpt-6-astra-2026-09-18", replayTerminal: "gpt-6-astra-build-42", dailyCreated: "gpt-6-astra-2026-09-19", dailyTerminal: "provider/gpt-6-astra-release-7"},
		{name: "auto review", requested: "codex-auto-review", replayCreated: "provider/codex-auto-review-2026-09-18", replayTerminal: "codex-auto-review-build-42", dailyCreated: "codex-auto-review-2026-09-19", dailyTerminal: "provider/codex-auto-review-release-7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway, _, account := newTurnStateAutoService(t)
			repo := &debugAtomicStateRepo{updated: true}
			gateway.accountRepo = repo
			s := &DebugWorkbenchService{gateway: gateway}
			evidence := validDebugWorkbenchStatePublicationEvidence(account.ID, 9, tt.requested, "variant-candidate", time.Now())
			setDebugWorkbenchPublicationModels(&evidence.replay, tt.replayCreated, tt.replayTerminal, tt.replayTerminal)
			setDebugWorkbenchPublicationModels(&evidence.dailyReplay, tt.dailyCreated, tt.dailyTerminal, tt.dailyTerminal)
			replayUsageModel := evidence.replay.verification.UpstreamResponseModel
			dailyUsageModel := evidence.dailyReplay.verification.UpstreamResponseModel

			require.NoError(t, s.publishDebugWorkbenchState(context.Background(), account, evidence))
			require.Equal(t, replayUsageModel, evidence.replay.verification.UpstreamResponseModel)
			require.Equal(t, dailyUsageModel, evidence.dailyReplay.verification.UpstreamResponseModel)
		})
	}
}

func validDebugWorkbenchStatePublicationEvidence(accountID, apiKeyID int64, requestedModel, state string, collectedAt time.Time) debugWorkbenchStatePublicationEvidence {
	newReplay := func() debugWorkbenchStateReplayPublicationEvidence {
		result := &OpenAIForwardResult{
			Model:                                requestedModel,
			UpstreamTurnState:                    debugStringPtr(state),
			UpstreamResponseModel:                requestedModel,
			CodexTurnStateResponseCreatedModel:   requestedModel,
			CodexTurnStateResponseCompletedModel: requestedModel,
		}
		verification := &DebugStateVerification{
			RequestedModel:         requestedModel,
			ResponseCreatedModel:   requestedModel,
			ResponseCompletedModel: requestedModel,
			StateSent:              true,
			ActualAccountID:        accountID,
			UsageLogAccountID:      accountID,
			UsageLogAPIKeyID:       apiKeyID,
			UsageLogRequestedModel: requestedModel,
			UpstreamResponseModel:  requestedModel,
			UsageLogStateSent:      true,
			UsageLogVerified:       true,
			StateMatchesCapture:    true,
		}
		return debugWorkbenchStateReplayPublicationEvidence{result: result, verification: verification}
	}
	return debugWorkbenchStatePublicationEvidence{
		apiKeyID:           apiKeyID,
		state:              state,
		collectedAt:        collectedAt,
		dailyRouteVerified: true,
		replay:             newReplay(),
		dailyReplay:        newReplay(),
	}
}

func cloneDebugWorkbenchStatePublicationEvidence(source debugWorkbenchStatePublicationEvidence) debugWorkbenchStatePublicationEvidence {
	cloneReplay := func(source debugWorkbenchStateReplayPublicationEvidence) debugWorkbenchStateReplayPublicationEvidence {
		result := *source.result
		if source.result.UpstreamTurnState != nil {
			state := *source.result.UpstreamTurnState
			result.UpstreamTurnState = &state
		}
		verification := *source.verification
		return debugWorkbenchStateReplayPublicationEvidence{result: &result, verification: &verification}
	}
	clone := source
	clone.replay = cloneReplay(source.replay)
	clone.dailyReplay = cloneReplay(source.dailyReplay)
	return clone
}

func setDebugWorkbenchPublicationModels(evidence *debugWorkbenchStateReplayPublicationEvidence, created, completed, terminal string) {
	evidence.result.CodexTurnStateResponseCreatedModel = created
	evidence.result.CodexTurnStateResponseCompletedModel = completed
	evidence.result.UpstreamResponseModel = terminal
	evidence.verification.ResponseCreatedModel = created
	evidence.verification.ResponseCompletedModel = completed
	evidence.verification.UpstreamResponseModel = terminal
}

func TestDebugWorkbenchAutomaticNeverStartsMaintenanceProbe(t *testing.T) {
	gateway, _, account := newTurnStateAutoService(t)
	ctx := context.WithValue(context.Background(), codexTurnStateManualVerificationContextKey{}, true)
	require.Empty(t, gateway.autoTurnStateForAccount(ctx, account, "gpt-5"))
	entry := gateway.openaiTurnStates[codexTurnStateKey{account.ID, "gpt-5"}]
	require.False(t, entry.probe)
	require.False(t, entry.running)
	gateway.noteCodexTurnStateVerificationError(ctx, account, "gpt-5", errCodexTurnStateResponseModelMismatch)
	require.False(t, entry.running)
}

func TestAcceptanceModelsAreNeverRewrittenInSSEOrJSON(t *testing.T) {
	s := &OpenAIGatewayService{}
	for _, requested := range []string{"gpt-6-astra", "gpt-6-astra-2026-09-18", "codex-auto-review", "codex-auto-review-build-42"} {
		line := `data: {"type":"response.completed","response":{"model":"gpt-5.6-luna"}}`
		require.Equal(t, line, s.replaceModelInSSELine(line, "gpt-5.6-luna", requested))
		body := []byte(`{"model":"gpt-5.6-luna"}`)
		require.Equal(t, body, s.replaceModelInResponseBody(body, "gpt-5.6-luna", requested))
	}
}
