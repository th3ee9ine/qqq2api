package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReliabilityTurnStateCollectorProjectionRedactsAndBounds(t *testing.T) {
	success := time.Date(2026, 9, 21, 10, 11, 12, 0, time.FixedZone("CST", 8*60*60))
	failure := success.Add(time.Minute)
	projected := reliabilityTurnStateCollectorFromSnapshot(OpenAICodexTurnStateReliabilitySnapshot{
		Enabled:          true,
		InjectionEnabled: true,
		Status:           "READY",
		Ready:            true,
		Collecting:       false,
		ActiveEntries:    -4,
		ReadyCandidates:  -2,
		Observations:     9,
		Successes:        4,
		Failures:         1,
		LastSuccessAt:    &success,
		LastFailureAt:    &failure,
		LastErrorCode:    "Bearer secret-token must not appear",
	})

	require.Equal(t, "ready", projected.Status)
	require.True(t, projected.Enabled)
	require.True(t, projected.InjectionEnabled)
	require.True(t, projected.Ready)
	require.Zero(t, projected.ActiveEntries)
	require.Zero(t, projected.ReadyCandidates)
	require.Equal(t, uint64(9), projected.Observations)
	require.Equal(t, uint64(4), projected.Successes)
	require.Equal(t, uint64(1), projected.Failures)
	require.Equal(t, success.UTC(), *projected.LastSuccessAt)
	require.Equal(t, failure.UTC(), *projected.LastFailureAt)
	require.Empty(t, projected.LastErrorCode)

	// The projection owns its timestamp values and must not alias the collector
	// snapshot, which may be reused by a lock-free status reader.
	success = success.Add(time.Hour)
	require.NotEqual(t, success.UTC(), *projected.LastSuccessAt)
}

func TestReliabilityTurnStateCollectorProjectionUsesAllowLists(t *testing.T) {
	for _, test := range []struct {
		status string
		code   string
		wantS  string
		wantC  string
	}{
		{status: "collecting", code: "probe_timeout", wantS: "collecting", wantC: "probe_timeout"},
		{status: " cooldown ", code: "UPSTREAM_429", wantS: "cooldown", wantC: "upstream_429"},
		{status: "provider leaked text", code: "raw upstream body", wantS: "unknown", wantC: ""},
		{status: "", code: "", wantS: "unknown", wantC: ""},
	} {
		t.Run(test.status, func(t *testing.T) {
			got := reliabilityTurnStateCollectorFromSnapshot(OpenAICodexTurnStateReliabilitySnapshot{
				Status:        test.status,
				LastErrorCode: test.code,
			})
			require.Equal(t, test.wantS, got.Status)
			require.Equal(t, test.wantC, got.LastErrorCode)
		})
	}
}

func TestReliabilityTurnStateCollectorStatusUnavailableWithoutProvider(t *testing.T) {
	svc := &OpsService{}
	require.Nil(t, svc.reliabilityTurnStateCollectorStatus(nil))
}
