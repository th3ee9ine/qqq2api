package service

import (
	"context"
	"net/http"
)

type openAIUpstreamTurnStateKey struct{}

// Snapshot before dispatch. Later collection, retries and mutation of a request
// header map must not change a completed attempt's usage record.
func snapshotOpenAIUpstreamTurnState(request *http.Request) *http.Request {
	state := request.Header.Get(openAICodexTurnStateHeader)
	return request.WithContext(context.WithValue(request.Context(), openAIUpstreamTurnStateKey{}, state))
}

func upstreamTurnStateFromResponse(response *http.Response) *string {
	if response == nil || response.Request == nil {
		return nil
	}
	state, ok := response.Request.Context().Value(openAIUpstreamTurnStateKey{}).(string)
	if !ok {
		return nil
	}
	return &state
}
