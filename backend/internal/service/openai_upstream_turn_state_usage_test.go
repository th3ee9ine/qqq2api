package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamTurnStateUsagePreservesActualAttempt(t *testing.T) {
	s := &OpenAIGatewayService{httpUpstream: &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		return turnStateResponse("new-response-state"), nil
	}}}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	req, err := http.NewRequest(http.MethodPost, "https://example.com/responses", nil)
	require.NoError(t, err)
	req.Header.Set(openAICodexTurnStateHeader, "actually-sent")
	first, err := s.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	defer first.Body.Close()
	req.Header.Set(openAICodexTurnStateHeader, "retry-state")
	second, err := s.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	defer second.Body.Close()
	req.Header.Del(openAICodexTurnStateHeader)
	absent, err := s.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	defer absent.Body.Close()
	require.Equal(t, "actually-sent", *upstreamTurnStateFromResponse(first))
	require.Equal(t, "retry-state", *upstreamTurnStateFromResponse(second))
	require.Equal(t, "", *upstreamTurnStateFromResponse(absent))
	require.Nil(t, upstreamTurnStateFromResponse(&http.Response{}))

	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	recorder := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	account.Extra = map[string]any{CodexTurnStateAutoExtraKey: "later-refreshed-cache"}
	err = recorder.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "turn-state-usage", UpstreamTurnState: upstreamTurnStateFromResponse(second), UpstreamHeaders: second.Header, Model: "gpt-5.5", Duration: time.Second},
		APIKey: &APIKey{ID: 1}, User: &User{ID: 2}, Account: account,
	})
	require.NoError(t, err)
	require.Equal(t, "retry-state", *usageRepo.lastLog.UpstreamTurnState)
	state := "blocked-request-state"
	recorder.RecordCyberPolicyUsageLog(context.Background(), CyberPolicyUsageInput{
		APIKey: &APIKey{ID: 1, User: &User{ID: 2}}, Account: account,
		RequestID: "blocked-request", Model: "gpt-5.5", UpstreamTurnState: &state,
	})
	require.Equal(t, state, *usageRepo.lastLog.UpstreamTurnState)

}
