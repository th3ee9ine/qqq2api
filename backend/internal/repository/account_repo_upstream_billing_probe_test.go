package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamBillingProbeExtraIsSchedulerNeutral(t *testing.T) {
	require.True(t, isSchedulerNeutralExtraKey("upstream_billing_probe"))
	require.True(t, isSchedulerNeutralExtraKey("upstream_billing_probe_enabled"))
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"upstream_billing_probe":         map[string]any{"status": "ok"},
		"upstream_billing_probe_enabled": true,
	}))
}

func TestOpenAISessionCleanupExtraIsSchedulerNeutral(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"openai_session_cleanup_enabled",
		"openai_session_cleanup_interval_minutes",
		"openai_session_cleanup_state",
		"auto_revoke_non_current_sessions_enabled",
		"auto_revoke_non_current_sessions_interval_minutes",
	} {
		require.Truef(t, isSchedulerNeutralExtraKey(key), "expected %q to be scheduler-neutral", key)
	}
	require.False(t, shouldEnqueueSchedulerOutboxForExtraUpdates(map[string]any{
		"openai_session_cleanup_state": map[string]any{"status": "success"},
	}))
}
