package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	// CodexTurnStateProbeBurstBudgetExtraPrefix keeps independent budgets for
	// Astra and codex-auto-review while retaining account ownership in
	// accounts.extra. The payload never contains a proxy URL or credential.
	CodexTurnStateProbeBurstBudgetExtraPrefix = "codex_turn_state_auto_probe_burst:"
	CodexTurnStateProbeBurstMaxAttempts       = 3
	// CodexTurnStateProbeBurstMaxLeaseMS is shared with repository adapters so
	// each per-route lease is validated identically before and after persistence.
	// A complete pool round renews this short lease without consuming another
	// attempt, so a crashed worker cannot lock the slot for the whole pool size.
	CodexTurnStateProbeBurstMaxLeaseMS  = int64(time.Minute / time.Millisecond)
	codexTurnStateProbeBurstMaxAttempts = CodexTurnStateProbeBurstMaxAttempts
)

// CodexTurnStateProbeBurstBudget is a model-scoped, account-owned reservation
// counter. A missing/expired state may consume at most three full-pool round
// reservations for one state generation. StartedAtMS is the start of the
// current short lease (or the most recent reservation after release), not a
// whole-round deadline; only a strictly newer generation resets Attempts.
type CodexTurnStateProbeBurstBudget struct {
	Version                 int64  `json:"version"`
	Generation              int64  `json:"generation"`
	Model                   string `json:"model"`
	StartedAtMS             int64  `json:"started_at_ms"`
	Attempts                int    `json:"attempts"`
	InFlightUntilMS         int64  `json:"in_flight_until_ms,omitempty"`
	CandidatePendingUntilMS int64  `json:"candidate_pending_until_ms,omitempty"`
}

// CodexTurnStateProbeBurstBudgetRepository persists reservations before an
// upstream collection request. A false result is a lost CAS and must be
// treated as no reservation until the winner has been reloaded.
type CodexTurnStateProbeBurstBudgetRepository interface {
	CompareAndSwapCodexTurnStateProbeBurstBudget(context.Context, int64, string, int64, CodexTurnStateProbeBurstBudget) (bool, error)
}

var errCodexTurnStateProbeBurstBudgetCorrupt = errors.New("codex turn state probe burst budget is invalid")

func codexTurnStateProbeBurstBudgetExtraKey(model string) string {
	return CodexTurnStateProbeBurstBudgetExtraPrefix + base64.RawURLEncoding.EncodeToString([]byte(codexTurnStateOwnerModel(model)))
}

// CodexTurnStateProbeBurstBudgetExtraKey is exported for repository adapters;
// service call sites should use the package-local helper above.
func CodexTurnStateProbeBurstBudgetExtraKey(model string) string {
	return codexTurnStateProbeBurstBudgetExtraKey(model)
}

func codexTurnStateProbeBurstBudgetFromAccount(account *Account, slot string) (CodexTurnStateProbeBurstBudget, error) {
	var result CodexTurnStateProbeBurstBudget
	if account == nil || account.Extra == nil {
		return result, nil
	}
	raw, exists := account.Extra[slot]
	if !exists || raw == nil {
		return result, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return CodexTurnStateProbeBurstBudget{}, errCodexTurnStateProbeBurstBudgetCorrupt
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return CodexTurnStateProbeBurstBudget{}, errCodexTurnStateProbeBurstBudgetCorrupt
	}
	model := strings.TrimSpace(result.Model)
	if result.Version <= 0 || result.Generation < 0 || result.Model != model || model == "" ||
		result.StartedAtMS <= 0 || result.Attempts < 1 || result.Attempts > codexTurnStateProbeBurstMaxAttempts ||
		result.InFlightUntilMS < 0 || result.InFlightUntilMS > 0 &&
		(result.InFlightUntilMS <= result.StartedAtMS || result.InFlightUntilMS-result.StartedAtMS > CodexTurnStateProbeBurstMaxLeaseMS) ||
		result.CandidatePendingUntilMS < 0 || result.CandidatePendingUntilMS > 0 && result.CandidatePendingUntilMS <= result.StartedAtMS ||
		result.InFlightUntilMS > 0 && result.CandidatePendingUntilMS > 0 ||
		slot != codexTurnStateProbeBurstBudgetExtraKey(model) {
		return CodexTurnStateProbeBurstBudget{}, errCodexTurnStateProbeBurstBudgetCorrupt
	}
	if normalized, normalizeErr := NormalizeOpenAICodexTurnStateDefaultModel(model); normalizeErr != nil || normalized != model || codexTurnStateOwnerModel(model) != model {
		return CodexTurnStateProbeBurstBudget{}, errCodexTurnStateProbeBurstBudgetCorrupt
	}
	return result, nil
}
