package service

import (
	"context"
	"strings"
	"time"
)

// OpenAICodexTurnStateReliabilitySnapshot is the bounded, aggregate view of
// the turn-state collector used by the Ops reliability projection. It is
// deliberately different from a request/session snapshot: opaque state,
// account IDs, scope keys, routes, and proxy details must never cross this
// boundary.
//
// The gateway collector should return this value from a lock-protected,
// non-blocking status read. Timestamps are optional and are copied by the
// projection before being exposed to the administrator UI.
type OpenAICodexTurnStateReliabilitySnapshot struct {
	Enabled          bool
	ProbeEnabled     bool
	InjectionEnabled bool
	Status           string
	Ready            bool
	Collecting       bool
	ActiveEntries    int
	ReadyCandidates  int
	Observations     uint64
	Successes        uint64
	Failures         uint64
	LastSuccessAt    *time.Time
	LastFailureAt    *time.Time
	LastErrorCode    string
}

// OpenAICodexTurnStateReliabilityProvider is implemented by the OpenAI
// gateway when the collector is installed. Keeping this as a small optional
// interface lets older/test gateway instances continue serving reliability
// status without making the collector a hard dependency of OpsService.
type OpenAICodexTurnStateReliabilityProvider interface {
	CodexTurnStateReliabilitySnapshot(context.Context) OpenAICodexTurnStateReliabilitySnapshot
}

// OpsReliabilityTurnStateCollector is the safe JSON projection of the
// collector snapshot. It contains no opaque token or identity-bearing key.
type OpsReliabilityTurnStateCollector struct {
	Enabled          bool       `json:"enabled"`
	ProbeEnabled     bool       `json:"probe_enabled"`
	InjectionEnabled bool       `json:"injection_enabled"`
	Status           string     `json:"status,omitempty"`
	Ready            bool       `json:"ready"`
	Collecting       bool       `json:"collecting"`
	ActiveEntries    int        `json:"active_entries"`
	ReadyCandidates  int        `json:"ready_candidates"`
	Observations     uint64     `json:"observations"`
	Successes        uint64     `json:"successes"`
	Failures         uint64     `json:"failures"`
	LastSuccessAt    *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt    *time.Time `json:"last_failure_at,omitempty"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
}

// reliabilityTurnStateCollectorFromSnapshot maps an internal collector
// snapshot to a bounded status object. Status and error code are allow-listed
// because they can otherwise become an accidental channel for upstream error
// text or deployment details.
func reliabilityTurnStateCollectorFromSnapshot(snapshot OpenAICodexTurnStateReliabilitySnapshot) *OpsReliabilityTurnStateCollector {
	return &OpsReliabilityTurnStateCollector{
		Enabled:          snapshot.Enabled,
		ProbeEnabled:     snapshot.ProbeEnabled,
		InjectionEnabled: snapshot.InjectionEnabled,
		Status:           normalizeTurnStateCollectorStatus(snapshot.Status),
		Ready:            snapshot.Ready,
		Collecting:       snapshot.Collecting,
		ActiveEntries:    maxNonNegative(snapshot.ActiveEntries),
		ReadyCandidates:  maxNonNegative(snapshot.ReadyCandidates),
		Observations:     snapshot.Observations,
		Successes:        snapshot.Successes,
		Failures:         snapshot.Failures,
		LastSuccessAt:    copyReliabilityTime(snapshot.LastSuccessAt),
		LastFailureAt:    copyReliabilityTime(snapshot.LastFailureAt),
		LastErrorCode:    normalizeTurnStateCollectorErrorCode(snapshot.LastErrorCode),
	}
}

func maxNonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func copyReliabilityTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func normalizeTurnStateCollectorStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "disabled", "unavailable", "idle", "collecting", "warming", "ready", "stale", "cooldown", "degraded", "error":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		if strings.TrimSpace(value) == "" {
			return "unknown"
		}
		return "unknown"
	}
}

func normalizeTurnStateCollectorErrorCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "probe_timeout", "transport_error", "upstream_401", "upstream_403", "upstream_429", "upstream_5xx",
		"model_capacity", "upstream_rate_limited", "response_failed", "response_model_mismatch",
		"invalid_state", "invalid_model", "incomplete_stream", "state_time_rejected", "cooldown", "cancelled", "disabled", "unavailable", "other":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

// reliabilityTurnStateCollectorStatus reads the optional collector without
// making the reliability endpoint depend on its implementation. The provider
// method is expected to be a fast snapshot read; request cancellation is
// still honored before invoking it.
func (s *OpsService) reliabilityTurnStateCollectorStatus(ctx context.Context) *OpsReliabilityTurnStateCollector {
	if s == nil || s.openAIGatewayService == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	provider, ok := any(s.openAIGatewayService).(OpenAICodexTurnStateReliabilityProvider)
	if !ok {
		return nil
	}
	return reliabilityTurnStateCollectorFromSnapshot(provider.CodexTurnStateReliabilitySnapshot(ctx))
}
