package service

import (
	"context"
	"strings"
	"time"
)

// OpenAIOAuthTTFTGuardStateExtraKey is the account.extra key used by the
// automatic first-token latency guard.  It is deliberately independent from
// schedulable and the other temporary pause fields so an automatic recovery
// cannot clear an administrator's or another subsystem's pause.
const OpenAIOAuthTTFTGuardStateExtraKey = "openai_oauth_ttft_guard_state"

// OpenAIOAuthTTFTGuardState is persisted in accounts.extra while an OpenAI
// OAuth-like account is paused by the first-token latency guard.  Timestamps
// are RFC3339 strings on the JSON wire. ProbeToken and ProbeLeaseUntil are
// optional and are only populated while a recovery probe is claimed.
type OpenAIOAuthTTFTGuardState struct {
	PausedAt        time.Time
	NextProbeAt     time.Time
	ProbeToken      string
	ProbeLeaseUntil *time.Time
	Model           string
}

// ParseOpenAIOAuthTTFTGuardState validates the persisted JSON shape. Invalid
// or incomplete state is treated as absent so old/corrupt metadata cannot
// accidentally block unrelated account types.
func ParseOpenAIOAuthTTFTGuardState(extra map[string]any) (OpenAIOAuthTTFTGuardState, bool) {
	if len(extra) == 0 {
		return OpenAIOAuthTTFTGuardState{}, false
	}
	raw, ok := extra[OpenAIOAuthTTFTGuardStateExtraKey].(map[string]any)
	if !ok || raw == nil {
		return OpenAIOAuthTTFTGuardState{}, false
	}
	pausedAt, ok := parseTTFTGuardTimestamp(raw["paused_at"])
	if !ok {
		return OpenAIOAuthTTFTGuardState{}, false
	}
	nextProbeAt, ok := parseTTFTGuardTimestamp(raw["next_probe_at"])
	if !ok {
		return OpenAIOAuthTTFTGuardState{}, false
	}
	model := strings.TrimSpace(ttftGuardStringValue(raw["model"]))
	if model == "" {
		return OpenAIOAuthTTFTGuardState{}, false
	}
	state := OpenAIOAuthTTFTGuardState{
		PausedAt:    pausedAt,
		NextProbeAt: nextProbeAt,
		Model:       model,
	}
	state.ProbeToken = strings.TrimSpace(ttftGuardStringValue(raw["probe_token"]))
	if value, exists := raw["probe_lease_until"]; exists && value != nil {
		leaseUntil, valid := parseTTFTGuardTimestamp(value)
		if !valid {
			return OpenAIOAuthTTFTGuardState{}, false
		}
		state.ProbeLeaseUntil = &leaseUntil
	}
	return state, true
}

func parseTTFTGuardTimestamp(value any) (time.Time, bool) {
	valueString, ok := value.(string)
	if !ok || strings.TrimSpace(valueString) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(valueString))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func ttftGuardStringValue(value any) string {
	valueString, _ := value.(string)
	return valueString
}

// IsOpenAIOAuthTTFTPaused reports whether this account carries a valid guard
// state. Setup-token accounts use the same OpenAI inference path and therefore
// participate in the same guard as refreshable OAuth accounts.
func (a *Account) IsOpenAIOAuthTTFTPaused() bool {
	if a == nil || !a.IsOpenAIOAuthLike() || a.ParentAccountID != nil {
		return false
	}
	_, ok := ParseOpenAIOAuthTTFTGuardState(a.Extra)
	return ok
}

// OpenAIOAuthTTFTGuardRepository is the deliberately narrow persistence
// contract used by the latency guard service. Implementations must make the
// three operations atomic and must enqueue/sync scheduler state after a
// successful transition.
type OpenAIOAuthTTFTGuardRepository interface {
	PauseOpenAIOAuthTTFT(ctx context.Context, id int64, model string, now, next time.Time) (bool, error)
	ClaimOpenAIOAuthTTFTProbe(ctx context.Context, now time.Time, token string, leaseUntil time.Time) (*Account, error)
	FinishOpenAIOAuthTTFTProbe(ctx context.Context, id int64, token string, recovered bool, next time.Time) (bool, error)
}
