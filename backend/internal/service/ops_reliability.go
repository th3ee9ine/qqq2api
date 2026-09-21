package service

import (
	"context"
	"errors"

	"github.com/th3ee9ine/qqq2api/internal/config"
)

// OpsReliabilityStatus is the bounded, read-only Turn State projection used
// by the admin reliability page.
type OpsReliabilityStatus struct {
	TurnState OpsReliabilityTurnState `json:"turn_state"`
}

// OpsReliabilityTurnState reports native gateway capabilities and, when the
// optional collector is installed, its bounded aggregate health. It never
// exposes an opaque state value, session key, account identifier, or heuristic
// quality score.
type OpsReliabilityTurnState struct {
	Supported                       bool                              `json:"supported"`
	HTTPEnabled                     bool                              `json:"http_enabled"`
	WebSocketEnabled                bool                              `json:"websocket_enabled"`
	CrossAccountProtection          bool                              `json:"cross_account_protection"`
	HTTPCrossAccountProtection      bool                              `json:"http_cross_account_protection"`
	WebSocketCrossAccountProtection bool                              `json:"websocket_cross_account_protection"`
	Collector                       *OpsReliabilityTurnStateCollector `json:"collector,omitempty"`
}

// GetReliabilityStatus returns only Turn State capability and bounded
// collector health. Account availability and traffic metrics belong to the
// Ops dashboard and are deliberately not queried by this endpoint.
func (s *OpsService) GetReliabilityStatus(ctx context.Context) (*OpsReliabilityStatus, error) {
	if s == nil {
		return nil, errors.New("ops service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	status := &OpsReliabilityStatus{TurnState: reliabilityTurnStateFromConfig(s.cfg)}
	status.TurnState.Collector = s.reliabilityTurnStateCollectorStatus(ctx)
	return status, nil
}

func reliabilityTurnStateFromConfig(cfg *config.Config) OpsReliabilityTurnState {
	webSocketEnabled := false
	if cfg != nil {
		ws := cfg.Gateway.OpenAIWS
		webSocketEnabled = ws.Enabled && !ws.ForceHTTP && ws.OAuthEnabled
	}
	return OpsReliabilityTurnState{
		Supported:                       true,
		HTTPEnabled:                     true,
		WebSocketEnabled:                webSocketEnabled,
		CrossAccountProtection:          true,
		HTTPCrossAccountProtection:      true,
		WebSocketCrossAccountProtection: true,
	}
}
