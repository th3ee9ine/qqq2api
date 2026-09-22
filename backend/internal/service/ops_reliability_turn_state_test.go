package service

import (
	"encoding/json"
	"strings"
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

func TestReliabilityTurnStateCollectorProjectionIncludesBoundedDiagnostics(t *testing.T) {
	lastSuccess := time.Date(2026, 9, 21, 10, 11, 12, 0, time.FixedZone("CST", 8*60*60))
	projected := reliabilityTurnStateCollectorFromSnapshot(OpenAICodexTurnStateReliabilitySnapshot{
		ProxyPool: []OpenAICodexTurnStateProxySummary{
			{Protocol: "HTTP", Host: "proxy.example.com", Port: 8080},
			{Protocol: "http", Host: "user:password@proxy.example.com", Port: 8080},
			{Protocol: "ftp", Host: "ignored.example.com", Port: 21},
		},
		SuccessfulIPRegions: []OpenAICodexTurnStateIPRegion{{Region: "North America", Country: "United States", CountryCode: "us", Successes: 4}},
		SuccessfulIPs: []OpenAICodexTurnStateSuccessfulIP{
			{IP: "198.51.100.8", Region: "North America", Country: "United States", CountryCode: "us", Successes: 3, LastSuccessAt: lastSuccess},
			{IP: "not-an-ip", Successes: 99},
		},
		CandidateBreakdown: []OpenAICodexTurnStateCandidateBreakdown{
			{Reason: "active_healthy_skipped", Count: 4},
			{Reason: "active_healthy_skipped", Count: 2},
			{Reason: "capability_mismatch", Count: 3},
			{Reason: "group_mismatch", Count: 2},
			{Reason: "privacy_not_set", Count: 1},
			{Reason: "channel_upstream_restricted", Count: 1},
			{Reason: "same_account_retry_mismatch", Count: 1},
			{Reason: "upstream raw error text", Count: 7},
		},
	})

	require.Len(t, projected.ProxyPool, 1)
	require.Equal(t, "http", projected.ProxyPool[0].Protocol)
	require.Equal(t, "proxy.example.com", projected.ProxyPool[0].Host)
	require.Len(t, projected.SuccessfulIPRegions, 1)
	require.Equal(t, uint64(4), projected.SuccessfulIPRegions[0].Successes)
	require.Len(t, projected.SuccessfulIPs, 1)
	require.Equal(t, "198.51.100.8", projected.SuccessfulIPs[0].IP)
	require.Equal(t, lastSuccess.UTC(), *projected.SuccessfulIPs[0].LastSuccessAt)
	require.Len(t, projected.CandidateBreakdown, 7)
	counts := make(map[string]uint64, len(projected.CandidateBreakdown))
	for _, entry := range projected.CandidateBreakdown {
		counts[entry.Reason] = entry.Count
	}
	require.Equal(t, uint64(7), counts["other"])
	require.Equal(t, uint64(6), counts["active_healthy_skipped"])
	require.Equal(t, uint64(3), counts["capability_mismatch"])
	require.Equal(t, uint64(2), counts["group_mismatch"])
	require.Equal(t, uint64(1), counts["privacy_not_set"])
	require.Equal(t, uint64(1), counts["channel_upstream_restricted"])
	require.Equal(t, uint64(1), counts["same_account_retry_mismatch"])

	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "password")
	require.NotContains(t, string(encoded), "raw error")
	require.NotContains(t, string(encoded), "not-an-ip")
	require.True(t, strings.Contains(string(encoded), "198.51.100.8"))
}

func TestReliabilityTurnStateCollectorProjectionRedactsHarvestNodesAndCookieValues(t *testing.T) {
	projected := reliabilityTurnStateCollectorFromSnapshot(OpenAICodexTurnStateReliabilitySnapshot{
		CookieCount: 3, CookieActiveCount: 1, CookieExpiredCount: 1, CookieRemainingSeconds: 239,
		BudgetUsed: 2, BudgetLimit: 6,
		HarvestNodes: []OpenAICodexTurnStateHarvestNodeSummary{
			{NodeID: "0123456789abcdef", Label: "http://proxy.example:8080", LastResult: "transport_error", Failures: 1},
			{NodeID: "fedcba9876543210", Label: "http://secret:password@proxy.example:8080", LastResult: "raw-session-cookie"},
			{NodeID: "not-a-node-id", Label: "http://proxy.example:8080", LastResult: "success"},
		},
	})
	require.Equal(t, 3, projected.CookieCount)
	require.Equal(t, 1, projected.CookieExpiredCount)
	require.Equal(t, 239, projected.CookieRemainingSeconds)
	require.Equal(t, 2, projected.BudgetUsed)
	require.Len(t, projected.Nodes, 1)
	require.Equal(t, "transport_error", projected.Nodes[0].LastResult)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "password")
	require.NotContains(t, string(encoded), "raw-session-cookie")
}

func TestReliabilityTurnStateCollectorStatusUnavailableWithoutProvider(t *testing.T) {
	svc := &OpsService{}
	require.Nil(t, svc.reliabilityTurnStateCollectorStatus(nil))
}
