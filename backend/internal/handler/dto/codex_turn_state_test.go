package dto

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestAccountCodexTurnStateAutoRedactedAndDiagnosticsSafe(t *testing.T) {
	now := time.Now().UnixMilli()
	a := &service.Account{ID: 10, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Extra: map[string]any{
		service.CodexTurnStateAutoExtraKey: "private-token", service.CodexTurnStateAutoSetAtExtraKey: now,
		service.CodexTurnStateAutoLastErrorExtraKey: "private-provider-error", "note": "public",
		service.CodexTurnStateAutoRecoveryExtraKey: map[string]any{"invalidated_at_ms": now, "pending": true, "rejected": []string{"private-digest"}, "legacy_token": "private-token"},
	}}
	a.Extra[service.CodexTurnStateModelExtraPrefix+base64.RawURLEncoding.EncodeToString([]byte("gpt-5.5"))] = map[string]any{
		service.CodexTurnStateAutoExtraKey: "private-model-token", service.CodexTurnStateAutoSetAtExtraKey: now,
		service.CodexTurnStateAutoVerifiedAtExtraKey: now, service.CodexTurnStateAutoVerifiedModelExtraKey: "gpt-5.5",
		service.CodexTurnStateAutoRecoveryExtraKey: map[string]any{"invalidated_at_ms": now, "pending": true},
	}
	a.Extra[service.CodexTurnStateProbeBurstBudgetExtraKey("gpt-5.5")] = service.CodexTurnStateProbeBurstBudget{
		Version: 1, Model: "gpt-5.5", StartedAtMS: now, Attempts: 1,
	}
	dto := AccountFromService(a)
	for _, projection := range []any{dto, AccountListItemFromAccount(dto)} {
		data, err := json.Marshal(projection)
		require.NoError(t, err)
		require.NotContains(t, string(data), "private")
		require.NotContains(t, string(data), service.CodexTurnStateProbeBurstBudgetExtraPrefix)
		require.Contains(t, string(data), `"configured":true`)
		// Legacy length-derived recovery metadata is intentionally ignored; state
		// length is diagnostic only and cannot revoke an otherwise verified value.
		require.Contains(t, string(data), `"recovery_pending":false`)
		require.Contains(t, string(data), `"note":"public"`)
	}
	require.Equal(t, "private-token", a.Extra[service.CodexTurnStateAutoExtraKey])
}

func TestIneligibleAccountCodexTurnStateAutoSerializesAsNull(t *testing.T) {
	account := &service.Account{ID: 11, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusDisabled}
	detail := AccountFromService(account)
	for _, projection := range []any{detail, AccountListItemFromAccount(detail)} {
		data, err := json.Marshal(projection)
		require.NoError(t, err)
		require.Contains(t, string(data), `"codex_turn_state_auto":null`, "refresh responses must explicitly invalidate stale frontend eligibility")
	}
}
