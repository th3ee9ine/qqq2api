package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

func TestGetReliabilityStatusReturnsOnlyTurnState(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc := &OpsService{cfg: cfg}

	status, err := svc.GetReliabilityStatus(context.Background())

	require.NoError(t, err)
	require.Equal(t, OpsReliabilityTurnState{
		Supported:                       true,
		HTTPEnabled:                     true,
		WebSocketEnabled:                true,
		CrossAccountProtection:          true,
		HTTPCrossAccountProtection:      true,
		WebSocketCrossAccountProtection: true,
	}, status.TurnState)

	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	var projection map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &projection))
	require.Len(t, projection, 1)
	require.Contains(t, projection, "turn_state")
	require.NotContains(t, string(encoded), "x-codex-turn-state")
}

func TestGetReliabilityStatusWithoutConfigStillReportsHTTPProtection(t *testing.T) {
	status, err := (&OpsService{}).GetReliabilityStatus(nil)

	require.NoError(t, err)
	require.True(t, status.TurnState.Supported)
	require.True(t, status.TurnState.HTTPEnabled)
	require.True(t, status.TurnState.HTTPCrossAccountProtection)
	require.False(t, status.TurnState.WebSocketEnabled)
}

func TestGetReliabilityStatusRejectsNilService(t *testing.T) {
	var svc *OpsService

	status, err := svc.GetReliabilityStatus(context.Background())

	require.Nil(t, status)
	require.EqualError(t, err, "ops service is nil")
}
