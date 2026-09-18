package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
	"github.com/th3ee9ine/qqq2api/internal/pkg/usagestats"
)

type debugWorkbenchUsageLogRepoStub struct {
	UsageLogRepository

	mu    sync.Mutex
	calls []usagestats.UsageLogFilters
	find  func(call int, filters usagestats.UsageLogFilters) ([]UsageLog, error)
}

func (s *debugWorkbenchUsageLogRepoStub) ListWithFilters(_ context.Context, _ pagination.PaginationParams, filters usagestats.UsageLogFilters) ([]UsageLog, *pagination.PaginationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, filters)
	if s.find == nil {
		return nil, nil, nil
	}
	logs, err := s.find(len(s.calls), filters)
	return logs, nil, err
}

func TestDebugWorkbenchUsageEvidenceKeepsRawUpstreamModel(t *testing.T) {
	const rawUpstreamModel = "openai/gpt-6-astra-2026-09-01"
	result := debugWorkbenchUsageResult(rawUpstreamModel, "")
	repo := &debugWorkbenchUsageLogRepoStub{find: func(_ int, filters usagestats.UsageLogFilters) ([]UsageLog, error) {
		if filters.RequestID != "client:debug-request-id" {
			return nil, nil
		}
		return []UsageLog{{
			APIKeyID:              11,
			AccountID:             7,
			RequestID:             filters.RequestID,
			Model:                 "gpt-6-astra",
			RequestedModel:        "gpt-6-astra",
			InboundEndpoint:       debugStringPtr(codexTurnStateUsageVerificationEndpoint),
			UpstreamEndpoint:      debugStringPtr(codexTurnStateUsageVerificationEndpoint),
			UpstreamResponseModel: debugStringPtr(rawUpstreamModel),
		}}, nil
	}}
	svc := &DebugWorkbenchService{gateway: &OpenAIGatewayService{usageLogRepo: repo}}
	evidence := debugWorkbenchUsageEvidence("gpt-6-astra", rawUpstreamModel, 7)

	require.True(t, svc.attachDebugWorkbenchUsageEvidence(context.Background(), "debug-request-id", 7, 11, evidence, result))
	require.Equal(t, int64(7), evidence.UsageLogAccountID)
	require.Equal(t, int64(11), evidence.UsageLogAPIKeyID)
	require.Equal(t, "gpt-6-astra", evidence.UsageLogRequestedModel)
	require.Equal(t, rawUpstreamModel, evidence.UpstreamResponseModel)
	require.True(t, evidence.UsageLogVerified)
	require.Len(t, repo.calls, 1)
	require.Equal(t, "client:debug-request-id", repo.calls[0].RequestID)
	require.True(t, repo.calls[0].SkipCount)
}

func TestDebugWorkbenchUsageEvidenceRejectsWrongScope(t *testing.T) {
	result := debugWorkbenchUsageResult("gpt-6-astra", "")
	for _, tt := range []struct {
		name     string
		row      UsageLog
		actualID int64
	}{
		{
			name:     "different selected account",
			row:      debugWorkbenchUsageRow(8, 11, "gpt-6-astra", "gpt-6-astra", ""),
			actualID: 7,
		},
		{
			name:     "different actual attempt account",
			row:      debugWorkbenchUsageRow(7, 11, "gpt-6-astra", "gpt-6-astra", ""),
			actualID: 8,
		},
		{
			name:     "different request model",
			row:      debugWorkbenchUsageRow(7, 11, "codex-auto-review", "gpt-6-astra", ""),
			actualID: 7,
		},
		{
			name:     "different API key",
			row:      debugWorkbenchUsageRow(7, 12, "gpt-6-astra", "gpt-6-astra", ""),
			actualID: 7,
		},
		{
			name:     "missing upstream model evidence",
			row:      debugWorkbenchUsageRow(7, 11, "gpt-6-astra", "", ""),
			actualID: 7,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := &debugWorkbenchUsageLogRepoStub{find: func(_ int, filters usagestats.UsageLogFilters) ([]UsageLog, error) {
				row := tt.row
				row.RequestID = filters.RequestID
				return []UsageLog{row}, nil
			}}
			svc := &DebugWorkbenchService{gateway: &OpenAIGatewayService{usageLogRepo: repo}}
			evidence := debugWorkbenchUsageEvidence("gpt-6-astra", "gpt-6-astra", tt.actualID)

			require.False(t, svc.attachDebugWorkbenchUsageEvidence(context.Background(), "debug-request-id", 7, 11, evidence, result))
			require.Zero(t, evidence.UsageLogAccountID)
			require.Empty(t, evidence.UpstreamResponseModel)
			require.LessOrEqual(t, len(repo.calls), debugWorkbenchUsageLookupTries*2)
		})
	}
}

func TestDebugWorkbenchUsageEvidenceWaitsForAsyncWrite(t *testing.T) {
	raw := "gpt-6-astra-build-42"
	result := debugWorkbenchUsageResult(raw, "captured-state")
	repo := &debugWorkbenchUsageLogRepoStub{find: func(call int, filters usagestats.UsageLogFilters) ([]UsageLog, error) {
		// One poll checks the client-prefixed ID and the legacy unprefixed ID.
		// Make the durable row visible on the second poll.
		if call < 3 || filters.RequestID != "client:debug-request-id" {
			return nil, nil
		}
		row := debugWorkbenchUsageRow(7, 11, "gpt-6-astra", raw, "captured-state")
		row.RequestID = filters.RequestID
		return []UsageLog{row}, nil
	}}
	svc := &DebugWorkbenchService{gateway: &OpenAIGatewayService{usageLogRepo: repo}}
	evidence := debugWorkbenchUsageEvidence("gpt-6-astra", raw, 7)

	require.True(t, svc.attachDebugWorkbenchUsageEvidence(context.Background(), "debug-request-id", 7, 11, evidence, result))
	require.Equal(t, raw, evidence.UpstreamResponseModel)
	require.True(t, evidence.UsageLogStateSent)
	require.True(t, evidence.UsageLogVerified)
	require.GreaterOrEqual(t, len(repo.calls), 3)
}

func TestDebugWorkbenchInjectsRequestIDIntoForwardContext(t *testing.T) {
	var forwardedContextID string
	upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		forwardedContextID, _ = req.Context().Value(ctxkey.ClientRequestID).(string)
		return debugWorkbenchJSONResponse(http.StatusOK, `{"id":"resp_debug","object":"response","status":"completed","model":"gpt-6-astra","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`), nil
	}}
	svc, _ := debugWorkbenchTestService(debugWorkbenchTestAccount(), upstream)

	result, err := svc.Run(context.Background(), 23, 7, DebugWorkbenchRequest{
		Endpoint: "responses",
		Body:     json.RawMessage(`{"model":"gpt-6-astra","input":"Reply with exactly OK.","stream":false}`),
		Session:  DebugSessionInput{Action: "new_session"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.RequestID)
	require.Equal(t, result.RequestID, forwardedContextID)
	require.NotNil(t, result.StateVerification)
}

func debugStringPtr(value string) *string { return &value }

func debugWorkbenchUsageResult(rawModel, sentState string) *OpenAIForwardResult {
	return &OpenAIForwardResult{
		UpstreamTurnState:                    debugStringPtr(sentState),
		UpstreamResponseModel:                rawModel,
		CodexTurnStateResponseCreatedModel:   rawModel,
		CodexTurnStateResponseCompletedModel: rawModel,
	}
}

func debugWorkbenchUsageEvidence(requestedModel, rawModel string, accountID int64) *DebugStateVerification {
	return &DebugStateVerification{
		RequestedModel:         requestedModel,
		ResponseCreatedModel:   rawModel,
		ResponseCompletedModel: rawModel,
		ActualAccountID:        accountID,
	}
}

func debugWorkbenchUsageRow(accountID, apiKeyID int64, requestedModel, rawModel, sentState string) UsageLog {
	row := UsageLog{
		AccountID:         accountID,
		APIKeyID:          apiKeyID,
		Model:             requestedModel,
		RequestedModel:    requestedModel,
		InboundEndpoint:   debugStringPtr(codexTurnStateUsageVerificationEndpoint),
		UpstreamEndpoint:  debugStringPtr(codexTurnStateUsageVerificationEndpoint),
		UpstreamTurnState: debugStringPtr(sentState),
	}
	if rawModel != "" {
		row.UpstreamResponseModel = debugStringPtr(rawModel)
	}
	return row
}
