package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestAccountCodexTurnStateAutoRedactedAndDiagnosticsSafe(t *testing.T) {
	a := &service.Account{ID: 10, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: map[string]any{
		service.CodexTurnStateAutoExtraKey: "private-token", service.CodexTurnStateAutoSetAtExtraKey: time.Now().UnixMilli(),
		service.CodexTurnStateAutoLastErrorExtraKey: "private-provider-error", "note": "public",
		service.CodexTurnStateAutoRecoveryExtraKey: map[string]any{"invalidated_at_ms": time.Now().UnixMilli(), "pending": true, "rejected": []string{"private-digest"}, "legacy_token": "private-token"},
	}}
	dto := AccountFromService(a)
	for _, projection := range []any{dto, AccountListItemFromAccount(dto)} {
		data, err := json.Marshal(projection)
		require.NoError(t, err)
		require.NotContains(t, string(data), "private")
		require.Contains(t, string(data), `"configured":true`)
		require.Contains(t, string(data), `"recovery_pending":true`)
		require.Contains(t, string(data), `"note":"public"`)
	}
	require.Equal(t, "private-token", a.Extra[service.CodexTurnStateAutoExtraKey])
}
