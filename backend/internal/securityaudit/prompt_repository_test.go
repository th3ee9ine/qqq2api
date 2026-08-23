package securityaudit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldStorePromptAuditEvent(t *testing.T) {
	tests := []struct {
		name            string
		storePassEvents bool
		decision        EventDecision
		want            bool
	}{
		{name: "pass disabled", storePassEvents: false, decision: EventPass, want: false},
		{name: "flag disabled", storePassEvents: false, decision: EventFlag, want: true},
		{name: "critical disabled", storePassEvents: false, decision: EventCritical, want: true},
		{name: "pass enabled", storePassEvents: true, decision: EventPass, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStorePromptAuditEvent(tt.decision, tt.storePassEvents); got != tt.want {
				t.Fatalf("shouldStorePromptAuditEvent(%q, %t) = %t, want %t", tt.decision, tt.storePassEvents, got, tt.want)
			}
		})
	}
}

func TestBuildEventWhereSearchUsesGlobalKeyAndGroupIdentityOnly(t *testing.T) {
	where, args := buildEventWhere(EventFilter{Keyword: " global identity "}, 1)

	require.Len(t, args, 1)
	require.Equal(t, "%global identity%", args[0])
	require.Contains(t, where, "e.api_key_name_snapshot ILIKE $1")
	require.Contains(t, where, "e.group_name ILIKE $1")
	for _, legacyColumn := range []string{"e.user_id", "e.username_snapshot", "e.user_email_snapshot"} {
		require.False(t, strings.Contains(where, legacyColumn), "search must not use legacy user identity column %s", legacyColumn)
	}
}
