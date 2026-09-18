package service

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTurnStateProbeBurstBudgetFromAccount(t *testing.T) {
	model := "gpt-6-astra"
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	require.Equal(t, CodexTurnStateProbeBurstBudgetExtraPrefix+base64.RawURLEncoding.EncodeToString([]byte(model)), slot)

	missing, err := codexTurnStateProbeBurstBudgetFromAccount(&Account{Extra: map[string]any{}}, slot)
	require.NoError(t, err)
	require.Zero(t, missing)

	want := CodexTurnStateProbeBurstBudget{Version: 3, Generation: 1700, Model: model, StartedAtMS: 2000, Attempts: 3, CandidatePendingUntilMS: 3000}
	account := &Account{Extra: map[string]any{slot: map[string]any{
		"version": float64(want.Version), "generation": float64(want.Generation), "model": want.Model,
		"started_at_ms": float64(want.StartedAtMS), "attempts": float64(want.Attempts),
		"candidate_pending_until_ms": float64(want.CandidatePendingUntilMS),
	}}}
	got, err := codexTurnStateProbeBurstBudgetFromAccount(account, slot)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestCodexTurnStateProbeBurstBudgetFromAccountRejectsCorruptOrCrossModelPayload(t *testing.T) {
	model := "gpt-6-astra"
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	valid := map[string]any{"version": 1, "generation": 0, "model": model, "started_at_ms": 1000, "attempts": 1}
	tests := []struct {
		name  string
		value any
	}{
		{name: "not object", value: "invalid"},
		{name: "version zero", value: copyBurstBudgetMap(valid, "version", 0)},
		{name: "negative generation", value: copyBurstBudgetMap(valid, "generation", -1)},
		{name: "missing start", value: copyBurstBudgetMap(valid, "started_at_ms", 0)},
		{name: "attempt zero", value: copyBurstBudgetMap(valid, "attempts", 0)},
		{name: "attempt four", value: copyBurstBudgetMap(valid, "attempts", 4)},
		{name: "negative in flight", value: copyBurstBudgetMap(valid, "in_flight_until_ms", -1)},
		{name: "in flight not after start", value: copyBurstBudgetMap(valid, "in_flight_until_ms", 1000)},
		{name: "in flight beyond window", value: copyBurstBudgetMap(valid, "in_flight_until_ms", 61_001)},
		{name: "in flight and candidate pending", value: func() map[string]any {
			value := copyBurstBudgetMap(valid, "in_flight_until_ms", 2_000)
			return copyBurstBudgetMap(value, "candidate_pending_until_ms", 3_000)
		}()},
		{name: "negative pending", value: copyBurstBudgetMap(valid, "candidate_pending_until_ms", -1)},
		{name: "pending not after start", value: copyBurstBudgetMap(valid, "candidate_pending_until_ms", 1000)},
		{name: "different model", value: copyBurstBudgetMap(valid, "model", "codex-auto-review")},
		{name: "invalid model", value: copyBurstBudgetMap(valid, "model", "gpt-6-astra *")},
		{name: "unknown proxy field", value: copyBurstBudgetMap(valid, "proxy_url", "socks5://secret")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := codexTurnStateProbeBurstBudgetFromAccount(&Account{Extra: map[string]any{slot: tc.value}}, slot)
			require.ErrorIs(t, err, errCodexTurnStateProbeBurstBudgetCorrupt)
		})
	}
}

func copyBurstBudgetMap(source map[string]any, key string, value any) map[string]any {
	result := make(map[string]any, len(source))
	for sourceKey, sourceValue := range source {
		result[sourceKey] = sourceValue
	}
	result[key] = value
	return result
}
