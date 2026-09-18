package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDebugWorkbenchRunRejectsCrossModelContinuationBeforeUpstream(t *testing.T) {
	var requests []*http.Request
	upstream := &debugWorkbenchHTTPStub{fn: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		response := debugWorkbenchJSONResponse(http.StatusOK, `{"id":"resp_debug","object":"response","status":"completed","model":"gpt-6-astra","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
		response.Header.Set(openAICodexTurnStateHeader, "astra-state")
		return response, nil
	}}

	svc, _ := debugWorkbenchTestService(debugWorkbenchTestAccount(), upstream)
	first, err := svc.Run(context.Background(), 1, 7, DebugWorkbenchRequest{
		Endpoint: "responses",
		Body:     json.RawMessage(`{"model":"gpt-6-astra","input":"Reply with exactly OK.","stream":false}`),
		Session:  DebugSessionInput{Action: "new_session"},
	})
	require.NoError(t, err)
	require.True(t, first.Success, first.Error)
	require.Len(t, requests, 1)

	_, err = svc.Run(context.Background(), 1, 7, DebugWorkbenchRequest{
		Endpoint: "responses",
		Body:     json.RawMessage(`{"model":"codex-auto-review","input":"Reply with exactly OK.","stream":false}`),
		Session:  DebugSessionInput{ID: first.Session.ID, Action: "continue_turn"},
	})
	var inputErr *DebugWorkbenchInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, http.StatusConflict, inputErr.StatusCode)
	require.Len(t, requests, 1, "model mismatch must be rejected before an upstream request")

	continued, err := svc.Run(context.Background(), 1, 7, DebugWorkbenchRequest{
		Endpoint: "responses",
		Body:     json.RawMessage(`{"model":"gpt-6-astra","input":"Reply with exactly OK.","stream":false}`),
		Session:  DebugSessionInput{ID: first.Session.ID, Action: "continue_turn"},
	})
	require.NoError(t, err)
	require.True(t, continued.Success, continued.Error)
	require.Len(t, requests, 2)
	require.Equal(t, "astra-state", requests[1].Header.Get(openAICodexTurnStateHeader))
}
