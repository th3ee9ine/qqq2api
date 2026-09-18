package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/tidwall/gjson"
)

func observeCodexTurnStateLifecycle(observer *upstreamResponseModelObserver, created, completed string, failed bool) {
	if created != "" {
		observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"`+created+`"}}`), "response.created")
	}
	if completed != "" {
		observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"`+completed+`"}}`), "response.completed")
	}
	if failed {
		observer.ObserveOpenAI([]byte(`{"type":"response.failed","response":{"model":"`+completed+`"}}`), "response.failed")
	}
}

func TestCodexTurnStateAutoResponseEvidenceMatrix(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		requestModel, created, done string
		failed                      bool
		want                        error
	}{
		{name: "astra-created-and-completed", requestModel: "gpt-6-astra", created: "gpt-6-astra", done: "gpt-6-astra"},
		{name: "astra-dated-response", requestModel: "gpt-6-astra", created: "gpt-6-astra-2026-09-18", done: "gpt-6-astra-2026-09-18"},
		{name: "astra-build-response", requestModel: "gpt-6-astra", created: "gpt-6-astra-build-42", done: "gpt-6-astra-build-42"},
		{name: "astra-provider-prefixed-response", requestModel: "gpt-6-astra", created: "openai/gpt-6-astra-2026-09-18", done: "openai/gpt-6-astra-2026-09-18"},
		{name: "astra-public-alias-response", requestModel: "gpt-6-astra", created: "gpt-6", done: "gpt-6"},
		{name: "astra-mixed-lifecycle-variants", requestModel: "gpt-6-astra", created: "openai/gpt-6-astra-2026-09-18", done: "gpt-6"},
		{name: "astra-mixed-lifecycle-variants-reversed", requestModel: "provider/gpt-6-astra-build-42", created: "gpt-6", done: "gpt-6-astra-2026-09-18"},
		{name: "luna-http-200-is-mismatch", requestModel: "gpt-6-astra", created: "gpt-5.6-luna", done: "gpt-5.6-luna", want: errCodexTurnStateResponseModelMismatch},
		{name: "sol-is-not-astra", requestModel: "gpt-6-astra", created: "gpt-5.6-sol", done: "gpt-5.6-sol", want: errCodexTurnStateResponseModelMismatch},
		{name: "terra-is-not-astra", requestModel: "gpt-6-astra", created: "gpt-5.6-terra", done: "gpt-5.6-terra", want: errCodexTurnStateResponseModelMismatch},
		{name: "created-astra-completed-luna", requestModel: "gpt-6-astra", created: "gpt-6-astra", done: "gpt-5.6-luna", want: errCodexTurnStateResponseModelMismatch},
		{name: "missing-created", requestModel: "gpt-6-astra", done: "gpt-6-astra", want: errCodexTurnStateResponseModelMissing},
		{name: "missing-completed", requestModel: "gpt-6-astra", created: "gpt-6-astra", want: errCodexTurnStateResponseModelMissing},
		{name: "failed", requestModel: "gpt-6-astra", created: "gpt-6-astra", done: "gpt-6-astra", failed: true, want: errCodexTurnStateResponseFailed},
		{name: "auto-review-reports-itself", requestModel: "codex-auto-review", created: "codex-auto-review", done: "codex-auto-review"},
		{name: "auto-review-family-provider-prefix", requestModel: "codex-auto-review", created: "provider/codex-auto-review", done: "provider/codex-auto-review"},
		{name: "auto-review-dated-family", requestModel: "codex-auto-review", created: "codex-auto-review-2026-09-18", done: "provider/codex-auto-review-build-42"},
		{name: "auto-review-rejects-astra", requestModel: "codex-auto-review", created: "gpt-6-astra", done: "gpt-6-astra", want: errCodexTurnStateResponseModelMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := &upstreamResponseModelObserver{}
			observeCodexTurnStateLifecycle(observer, tc.created, tc.done, tc.failed)
			err := validateCodexTurnStateResponseEvidence(observer, codexTurnStateExpectedResponseModel(tc.requestModel))
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
		})
	}

	model := "codex-auto-review"
	require.Equal(t, "codex-auto-review", codexTurnStateExpectedResponseModel(model))
	require.Equal(t, "codex-auto-review", model, "validation must not rewrite the requested model")
}

func TestCodexTurnStateResponseDoneDoesNotReplaceCompletedEvidence(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-6-astra"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.done","response":{"model":"gpt-6-astra"}}`), "response.done")

	created, completed, failed := observer.CodexTurnStateEvidence()
	require.Equal(t, "gpt-6-astra", created)
	require.Empty(t, completed)
	require.False(t, failed)
	require.ErrorIs(t, validateCodexTurnStateResponseEvidence(observer, "gpt-6-astra"), errCodexTurnStateResponseModelMissing)
}

func TestCreateOpenAICodexTurnStateProbePayloadUsesExactShortInput(t *testing.T) {
	payload := createOpenAICodexTurnStateProbePayload("gpt-6-astra")
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(encoded, "model").String())
	require.NotEmpty(t, strings.TrimSpace(gjson.GetBytes(encoded, "instructions").String()))
	require.Equal(t, "message", gjson.GetBytes(encoded, "input.0.type").String())
	require.Equal(t, codexTurnStateProbePrompt, gjson.GetBytes(encoded, "input.0.content.0.text").String())
	require.True(t, gjson.GetBytes(encoded, "stream").Bool())
	require.False(t, gjson.GetBytes(encoded, "store").Bool())
}

func TestIsOpenAICodexTurnStateUsageVerificationRequest(t *testing.T) {
	valid := `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with exactly OK."}]}],"stream":true,"store":false}`
	for _, tc := range []struct {
		name        string
		body        string
		nativeState string
		want        bool
	}{
		{name: "exact-short-request", body: valid, want: true},
		{name: "astra-variant-model", body: strings.Replace(valid, `"gpt-6-astra"`, `"gpt-6-astra-2026-09-18"`, 1), want: true},
		{name: "auto-review-model", body: strings.Replace(valid, `"gpt-6-astra"`, `"codex-auto-review"`, 1), want: true},
		{name: "native-state-wins", body: valid, nativeState: "official-client-state"},
		{name: "empty-model", body: strings.Replace(valid, `"gpt-6-astra"`, `"   "`, 1)},
		{name: "string-input", body: `{"model":"gpt-6-astra","input":"Reply with exactly OK."}`},
		{name: "multiple-input-items", body: `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with exactly OK."}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with exactly OK."}]}]}`},
		{name: "wrong-item-type", body: strings.Replace(valid, `"type":"message"`, `"type":"input_text"`, 1)},
		{name: "wrong-role", body: strings.Replace(valid, `"role":"user"`, `"role":"developer"`, 1)},
		{name: "multiple-content-blocks", body: `{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Reply with exactly OK."},{"type":"input_text","text":"Reply with exactly OK."}]}]}`},
		{name: "wrong-content-type", body: strings.Replace(valid, `"type":"input_text"`, `"type":"output_text"`, 1)},
		{name: "wrong-text", body: strings.Replace(valid, `Reply with exactly OK.`, `Reply OK.`, 1)},
		{name: "text-with-extra-space", body: strings.Replace(valid, `Reply with exactly OK.`, `Reply with exactly OK. `, 1)},
		{name: "malformed-json", body: `{"model":"gpt-6-astra"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, IsOpenAICodexTurnStateUsageVerificationRequest([]byte(tc.body), tc.nativeState))
		})
	}
}

func TestCodexTurnStateAutoReviewProbeKeepsRequestModelAndReplaysOnSameRoute(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	const (
		model     = "codex-auto-review"
		state     = "auto-review-candidate"
		stickyURL = "socks5://sticky.test:1080"
	)
	var seenStates []string
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, proxy string, accountID int64) (*http.Response, error) {
		require.Equal(t, stickyURL, proxy)
		require.Equal(t, account.ID, accountID)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
		require.Equal(t, model, payload["model"], "codex-auto-review is a valid request model and must be sent unchanged")
		seenStates = append(seenStates, req.Header.Get(openAICodexTurnStateHeader))
		if len(seenStates) == 1 {
			return turnStateModelResponse(state, "codex-auto-review"), nil
		}
		return turnStateModelResponse("", "codex-auto-review"), nil
	}}

	got, err := s.probeOpenAICodexTurnStateViaProxy(context.Background(), account, model, stickyURL)
	require.NoError(t, err)
	require.Equal(t, state, got)
	require.Equal(t, []string{"", state}, seenStates, "the exact candidate must be replayed on the same sticky route")
}

func TestCodexTurnStateAutoPassthroughEvidenceNeverPublishesWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantState, wantError string
	}{
		{
			name: "astra-created-and-completed-still-needs-replay",
			body: strings.Join([]string{
				`data: {"type":"response.created","response":{"id":"resp_ok","model":"gpt-6-astra"}}`,
				``,
				`data: {"type":"response.completed","response":{"id":"resp_ok","model":"gpt-6-astra","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
			wantState: "old-verified-astra",
		},
		{
			name: "created-astra-completed-luna-preserves-old-astra",
			body: strings.Join([]string{
				`data: {"type":"response.created","response":{"id":"resp_mixed","model":"gpt-6-astra"}}`,
				``,
				`data: {"type":"response.completed","response":{"id":"resp_mixed","model":"gpt-5.6-luna","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
			wantState: "old-verified-astra",
			wantError: errCodexTurnStateResponseModelMismatch.Error(),
		},
		{
			name: "luna-http-200-preserves-old-astra",
			body: strings.Join([]string{
				`data: {"type":"response.created","response":{"id":"resp_luna","model":"gpt-5.6-luna"}}`,
				``,
				`data: {"type":"response.completed","response":{"id":"resp_luna","model":"gpt-5.6-luna","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
			wantState: "old-verified-astra",
			wantError: errCodexTurnStateResponseModelMismatch.Error(),
		},
		{
			name: "missing-completed-preserves-old-astra",
			body: strings.Join([]string{
				`data: {"type":"response.created","response":{"id":"resp_partial","model":"gpt-6-astra"}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
			wantState: "old-verified-astra",
			wantError: errCodexTurnStateResponseModelMissing.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, account := newTurnStateAutoService(t)
			seedAutomaticTurnState(account, "old-verified-astra", "gpt-6-astra")
			repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)

			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			upstreamRequest := httptest.NewRequest(http.MethodPost, chatgptCodexAPIURL, nil)
			upstreamRequest = upstreamRequest.WithContext(withCodexTurnStateModel(upstreamRequest.Context(), "gpt-6-astra"))
			s.stagePendingCodexTurnStateObservation(c, account, "passthrough-candidate", upstreamRequest)

			response := &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type":       []string{"text/event-stream"},
					"X-Codex-Turn-State": []string{"passthrough-candidate"},
				},
				Body: io.NopCloser(strings.NewReader(tc.body)),
			}
			_, err := s.handleNonStreamingResponsePassthrough(context.Background(), response, c, account, "gpt-6-astra", "gpt-6-astra")
			require.NoError(t, err)
			waitTurnStateAutoIdle(t, s)

			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			modelAccount := codexTurnStateModelAccount(stored, "gpt-6-astra")
			require.Equal(t, tc.wantState, codexTurnStateAutoToken(modelAccount))
			require.Equal(t, tc.wantError, modelAccount.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
		})
	}
}

func TestCodexTurnStateAutoUnverifiedPersistedStateIsNeverInjected(t *testing.T) {
	for _, tc := range []struct {
		name          string
		verifiedAt    int64
		verifiedModel string
	}{
		{name: "missing-verification-metadata"},
		{name: "verified-for-another-model", verifiedAt: 1, verifiedModel: "gpt-5.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, account := newTurnStateAutoService(t)
			account.Extra = map[string]any{codexTurnStateModelExtraKey("gpt-5.5"): map[string]any{
				CodexTurnStateAutoExtraKey:              "legacy-unverified-state",
				CodexTurnStateAutoSetAtExtraKey:         int64(1),
				CodexTurnStateAutoVerifiedAtExtraKey:    tc.verifiedAt,
				CodexTurnStateAutoVerifiedModelExtraKey: tc.verifiedModel,
			}}
			repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
			headers := http.Header{}
			require.NoError(t, s.applyOpenAICodexTurnState(context.Background(), account, headers, "gpt-5.5"))
			require.Empty(t, headers.Get(openAICodexTurnStateHeader))
			require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, "gpt-5.5"))
			waitTurnStateAutoIdle(t, s)
		})
	}
}

func TestCodexTurnStateAutoProbeRejectsLunaDespiteHTTP200(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	s.httpUpstream = &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		return turnStateModelResponse("luna-candidate", "gpt-5.6-luna"), nil
	}}

	state, err := s.probeOpenAICodexTurnStateViaProxy(context.Background(), account, "gpt-6-astra", "")
	require.Empty(t, state)
	require.True(t, errors.Is(err, errCodexTurnStateResponseModelMismatch))
}

func TestCodexTurnStateAutoFailedLifecyclePreservesVerifiedCache(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	seedAutomaticTurnState(account, "old-verified-astra", "gpt-6-astra")
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request := httptest.NewRequest(http.MethodPost, chatgptCodexAPIURL, nil)
	request = request.WithContext(withCodexTurnStateModel(request.Context(), "gpt-6-astra"))
	s.stagePendingCodexTurnStateObservation(c, account, "failed-candidate", request)
	observer := beginUpstreamResponseModelObservation(c)
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-6-astra"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.failed","response":{"model":"gpt-6-astra"}}`), "response.failed")
	s.commitPendingCodexTurnStateObservation(c, true)
	waitTurnStateAutoIdle(t, s)

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	modelAccount := codexTurnStateModelAccount(stored, "gpt-6-astra")
	require.Equal(t, "old-verified-astra", codexTurnStateAutoToken(modelAccount))
	require.Equal(t, errCodexTurnStateResponseFailed.Error(), modelAccount.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
}

func prepareCodexTurnStateUsageGateTest(t *testing.T) (*OpenAIGatewayService, *turnStateAutoRepo, *Account, context.Context, *OpenAIRecordUsageInput, *UsageLog, string, string) {
	t.Helper()
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6*"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	const model = "gpt-6-astra"
	now := time.Now()
	oldState := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	candidateState := recoveryTestToken(now, 12, 2)
	seedAutomaticTurnState(account, oldState, model)
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	s.stageCodexTurnStateUsageCandidateLocked(entry, candidateState, entry.recovery.InvalidatedAtMS, now)
	s.openaiTurnStateMu.Unlock()

	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "turn-state-acceptance-1")
	ctx = WithOpenAICodexTurnStateUsageVerification(ctx, 701)
	headers := http.Header{}
	require.NoError(t, s.applyOpenAICodexTurnState(ctx, account, headers, model))
	require.Equal(t, candidateState, headers.Get(openAICodexTurnStateHeader))
	require.Equal(t, oldState, s.autoTurnStateForAccount(context.Background(), account, model), "candidate injection must not replace the old verified token")

	endpoint := codexTurnStateUsageVerificationEndpoint
	completedModel := "gpt-6-astra-2026-09-18"
	usageLog := &UsageLog{
		RequestID:             "local:turn-state-acceptance-1",
		APIKeyID:              701,
		AccountID:             account.ID,
		RequestedModel:        model,
		UpstreamEndpoint:      &endpoint,
		UpstreamTurnState:     &candidateState,
		UpstreamResponseModel: &completedModel,
	}
	input := &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{
		UpstreamResponseModel:                completedModel,
		CodexTurnStateResponseCreatedModel:   "openai/gpt-6-astra-build-42",
		CodexTurnStateResponseCompletedModel: completedModel,
		CodexTurnStateResponseFailed:         false,
		UpstreamResponseModelConflict:        false,
	}}
	return s, repo, account, ctx, input, usageLog, oldState, candidateState
}

func TestCodexTurnStateUsageGatePublishesOnlyAfterNewUsageRow(t *testing.T) {
	s, repo, account, ctx, input, usageLog, oldState, candidateState := prepareCodexTurnStateUsageGateTest(t)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	s.usageLogRepo = usageRepo

	s.writeOpenAIUsageLogWithTurnStateGate(ctx, input, usageLog)
	waitTurnStateAutoIdle(t, s)

	require.Equal(t, 1, usageRepo.calls)
	require.Same(t, usageLog, usageRepo.lastLog)
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, candidateState, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-6-astra")))
	require.NotEqual(t, oldState, candidateState)
	require.Equal(t, "gpt-6-astra-2026-09-18", optionalStringValue(usageRepo.lastLog.UpstreamResponseModel), "usage log keeps the raw response model variant")
}

func TestCodexTurnStateUsageCandidateRequiresDedicatedMarkedRequest(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6*"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	const model = "gpt-6-astra"
	now := time.Now()
	oldState := recoveryTestToken(now.Add(-time.Minute), 10, 11)
	candidateState := recoveryTestToken(now, 12, 12)
	seedAutomaticTurnState(account, oldState, model)
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	s.stageCodexTurnStateUsageCandidateLocked(entry, candidateState, entry.recovery.InvalidatedAtMS, now)
	s.openaiTurnStateMu.Unlock()

	ordinaryCtx := context.WithValue(context.Background(), ctxkey.RequestID, "ordinary-request")
	ordinaryHeaders := http.Header{}
	require.NoError(t, s.applyOpenAICodexTurnState(ordinaryCtx, account, ordinaryHeaders, model))
	require.Equal(t, oldState, ordinaryHeaders.Get(openAICodexTurnStateHeader), "unmarked ordinary traffic must keep using the old verified state")
	s.openaiTurnStateMu.Lock()
	require.Empty(t, entry.candidate.requestID)
	require.Zero(t, entry.candidate.apiKeyID)
	s.openaiTurnStateMu.Unlock()

	wrongModelCtx := context.WithValue(context.Background(), ctxkey.RequestID, "wrong-model-request")
	wrongModelCtx = WithOpenAICodexTurnStateUsageVerification(wrongModelCtx, 701)
	s.openaiTurnStateMu.Lock()
	require.Empty(t, s.codexTurnStateUsageCandidateForRequestLocked(wrongModelCtx, entry, "codex-auto-review", time.Now()))
	require.Empty(t, entry.candidate.requestID, "a different model must not reserve this account/model candidate")
	require.Zero(t, entry.candidate.apiKeyID)
	s.openaiTurnStateMu.Unlock()

	acceptanceCtx := context.WithValue(context.Background(), ctxkey.RequestID, "acceptance-request")
	acceptanceCtx = WithOpenAICodexTurnStateUsageVerification(acceptanceCtx, 701)
	acceptanceHeaders := http.Header{}
	require.NoError(t, s.applyOpenAICodexTurnState(acceptanceCtx, account, acceptanceHeaders, model))
	require.Equal(t, candidateState, acceptanceHeaders.Get(openAICodexTurnStateHeader))
	s.openaiTurnStateMu.Lock()
	require.Equal(t, "local:acceptance-request", entry.candidate.requestID)
	require.Equal(t, int64(701), entry.candidate.apiKeyID)
	require.Equal(t, model, entry.candidate.requestedModel)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateUsageAcceptanceDoesNotLaunchCompetingProbe(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6*"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	const model = "gpt-6-astra"
	now := time.Now()
	candidateState := recoveryTestToken(now, 12, 14)
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	s.stageCodexTurnStateUsageCandidateLocked(entry, candidateState, entry.recovery.InvalidatedAtMS, now)
	s.openaiTurnStateMu.Unlock()

	var upstreamCalls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		upstreamCalls.Add(1)
		return turnStateModelResponse("unexpected-state", model), nil
	}}
	acceptanceCtx := context.WithValue(context.Background(), ctxkey.RequestID, "candidate-without-old-token")
	acceptanceCtx = WithOpenAICodexTurnStateUsageVerification(acceptanceCtx, 701)
	headers := http.Header{}
	require.NoError(t, s.applyOpenAICodexTurnState(acceptanceCtx, account, headers, model))
	require.Equal(t, candidateState, headers.Get(openAICodexTurnStateHeader))
	waitTurnStateAutoIdle(t, s)
	require.Zero(t, upstreamCalls.Load(), "the acceptance request must not race a second maintenance probe")
	repo.mu.Lock()
	require.Zero(t, repo.writes, "a memory-only candidate must remain unpersisted before usage confirmation")
	repo.mu.Unlock()
}

func TestCodexTurnStateUsageCandidateRejectsEmptyRequestModel(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	now := time.Now()
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, "gpt-6-astra")
	s.stageCodexTurnStateUsageCandidateLocked(entry, recoveryTestToken(now, 12, 13), entry.recovery.InvalidatedAtMS, now)
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "empty-model")
	ctx = WithOpenAICodexTurnStateUsageVerification(ctx, 701)
	require.Empty(t, s.codexTurnStateUsageCandidateForRequestLocked(ctx, entry, "   ", now))
	require.Empty(t, entry.candidate.requestID)
	s.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateVerificationErrorCodesAreSafe(t *testing.T) {
	for _, err := range []error{
		errCodexTurnStateResponseModelMismatch,
		errCodexTurnStateResponseModelMissing,
		errCodexTurnStateResponseNotCompleted,
		errCodexTurnStateResponseFailed,
		errCodexTurnStateUsageLogMissing,
		errCodexTurnStateUsageRequestMismatch,
		errCodexTurnStateUsageAPIKeyMismatch,
		errCodexTurnStateUsageAccountMismatch,
		errCodexTurnStateUsageModelMismatch,
		errCodexTurnStateUsageStateMismatch,
		errCodexTurnStateUsageEndpointMismatch,
	} {
		require.Equal(t, err.Error(), safeCodexTurnStateAutoError(err.Error()))
	}
	require.Empty(t, safeCodexTurnStateAutoError("provider body with secret"))
}

func TestCodexTurnStateUsageGateMissingRowPreservesOldState(t *testing.T) {
	s, repo, account, ctx, input, usageLog, oldState, _ := prepareCodexTurnStateUsageGateTest(t)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: false}
	s.usageLogRepo = usageRepo

	s.writeOpenAIUsageLogWithTurnStateGate(ctx, input, usageLog)
	waitTurnStateAutoIdle(t, s)

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	modelAccount := codexTurnStateModelAccount(stored, "gpt-6-astra")
	require.Equal(t, oldState, codexTurnStateAutoToken(modelAccount))
	require.Equal(t, errCodexTurnStateUsageLogMissing.Error(), modelAccount.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
}

func TestCodexTurnStateUsageGateMismatchNeverPublishes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*OpenAIRecordUsageInput, *UsageLog)
	}{
		{name: "request-id", mutate: func(_ *OpenAIRecordUsageInput, log *UsageLog) { log.RequestID = "local:other" }},
		{name: "api-key", mutate: func(_ *OpenAIRecordUsageInput, log *UsageLog) { log.APIKeyID++ }},
		{name: "account", mutate: func(_ *OpenAIRecordUsageInput, log *UsageLog) { log.AccountID++ }},
		{name: "request-model", mutate: func(_ *OpenAIRecordUsageInput, log *UsageLog) { log.RequestedModel = "gpt-5.6-luna" }},
		{name: "sent-state", mutate: func(_ *OpenAIRecordUsageInput, log *UsageLog) {
			other := "different-state"
			log.UpstreamTurnState = &other
		}},
		{name: "endpoint", mutate: func(_ *OpenAIRecordUsageInput, log *UsageLog) {
			other := "/v1/chat/completions"
			log.UpstreamEndpoint = &other
		}},
		{name: "created-model", mutate: func(in *OpenAIRecordUsageInput, _ *UsageLog) {
			in.Result.CodexTurnStateResponseCreatedModel = "gpt-5.6-luna"
		}},
		{name: "completed-model", mutate: func(in *OpenAIRecordUsageInput, _ *UsageLog) {
			in.Result.CodexTurnStateResponseCompletedModel = "gpt-5.6-luna"
		}},
		{name: "stored-response-model", mutate: func(_ *OpenAIRecordUsageInput, log *UsageLog) {
			other := "gpt-6-astra-build-different"
			log.UpstreamResponseModel = &other
		}},
		{name: "missing-created", mutate: func(in *OpenAIRecordUsageInput, _ *UsageLog) { in.Result.CodexTurnStateResponseCreatedModel = "" }},
		{name: "failed-lifecycle", mutate: func(in *OpenAIRecordUsageInput, _ *UsageLog) { in.Result.CodexTurnStateResponseFailed = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, account, _, input, usageLog, oldState, _ := prepareCodexTurnStateUsageGateTest(t)
			tc.mutate(input, usageLog)
			s.confirmCodexTurnStateUsageLog(input, usageLog, true)
			waitTurnStateAutoIdle(t, s)

			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, oldState, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-6-astra")))
		})
	}
}

func TestCodexTurnStateUsageRequestIDCollisionCannotClearAnotherReservation(t *testing.T) {
	s, repo, firstAccount := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6*"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	secondAccount := *firstAccount
	secondAccount.ID = 20
	secondAccount.Extra = nil
	repo.accounts[secondAccount.ID] = &secondAccount
	const model = "gpt-6-astra"
	now := time.Now()
	oldFirst := recoveryTestToken(now.Add(-time.Minute), 10, 31)
	oldSecond := recoveryTestToken(now.Add(-time.Minute), 10, 32)
	candidateFirst := recoveryTestToken(now, 12, 33)
	candidateSecond := recoveryTestToken(now, 12, 34)
	seedAutomaticTurnState(firstAccount, oldFirst, model)
	seedAutomaticTurnState(&secondAccount, oldSecond, model)
	repo.accounts[firstAccount.ID].Extra = mergeMap(nil, firstAccount.Extra)
	repo.accounts[secondAccount.ID].Extra = mergeMap(nil, secondAccount.Extra)

	sharedRequestID := context.WithValue(context.Background(), ctxkey.RequestID, "shared-client-controlled-id")
	firstCtx := WithOpenAICodexTurnStateUsageVerification(sharedRequestID, 701)
	secondCtx := WithOpenAICodexTurnStateUsageVerification(sharedRequestID, 702)
	s.openaiTurnStateMu.Lock()
	firstEntry := s.codexTurnStateEntryLocked(firstAccount, now, model)
	secondEntry := s.codexTurnStateEntryLocked(&secondAccount, now, model)
	s.stageCodexTurnStateUsageCandidateLocked(firstEntry, candidateFirst, firstEntry.recovery.InvalidatedAtMS, now)
	s.stageCodexTurnStateUsageCandidateLocked(secondEntry, candidateSecond, secondEntry.recovery.InvalidatedAtMS, now)
	require.Equal(t, candidateFirst, s.codexTurnStateUsageCandidateForRequestLocked(firstCtx, firstEntry, model, now))
	require.Equal(t, candidateSecond, s.codexTurnStateUsageCandidateForRequestLocked(secondCtx, secondEntry, model, now))
	s.openaiTurnStateMu.Unlock()

	endpoint := codexTurnStateUsageVerificationEndpoint
	completedModel := "gpt-6-astra-2026-09-18"
	sentState := candidateFirst
	usageLog := &UsageLog{
		RequestID:             "local:shared-client-controlled-id",
		APIKeyID:              701,
		AccountID:             firstAccount.ID,
		RequestedModel:        model,
		UpstreamEndpoint:      &endpoint,
		UpstreamTurnState:     &sentState,
		UpstreamResponseModel: &completedModel,
	}
	input := &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{
		UpstreamResponseModel:                completedModel,
		CodexTurnStateResponseCreatedModel:   "gpt-6-astra-build-42",
		CodexTurnStateResponseCompletedModel: completedModel,
	}}
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	s.usageLogRepo = usageRepo
	s.writeOpenAIUsageLogWithTurnStateGate(firstCtx, input, usageLog)
	waitTurnStateAutoIdle(t, s)

	require.Equal(t, 1, usageRepo.calls)
	storedFirst, err := repo.GetByID(context.Background(), firstAccount.ID)
	require.NoError(t, err)
	require.Equal(t, candidateFirst, codexTurnStateAutoToken(codexTurnStateModelAccount(storedFirst, model)))
	storedSecond, err := repo.GetByID(context.Background(), secondAccount.ID)
	require.NoError(t, err)
	require.Equal(t, oldSecond, codexTurnStateAutoToken(codexTurnStateModelAccount(storedSecond, model)))
	s.openaiTurnStateMu.Lock()
	require.Equal(t, candidateSecond, secondEntry.candidate.state)
	require.Equal(t, int64(702), secondEntry.candidate.apiKeyID)
	require.Equal(t, "local:shared-client-controlled-id", secondEntry.candidate.requestID)
	s.openaiTurnStateMu.Unlock()
}
