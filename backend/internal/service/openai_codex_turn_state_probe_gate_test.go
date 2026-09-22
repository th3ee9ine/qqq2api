package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTurnStateProbeGateSingleflightAndDynamicCooldown(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	svc.SetCodexTurnStateRuntimeSettingsWithControls(true, true, nil, CodexTurnStateHarvestControls{
		SpeedPreset: "fast", MaxRequestsPerRound: 12, FailureCooldownSeconds: 15,
	})
	base := time.Now().UTC().Truncate(time.Second)

	lease, failure := svc.beginCodexTurnStateProbe(901, "gpt-6", base)
	require.Nil(t, failure)
	_, failure = svc.beginCodexTurnStateProbe(901, "gpt-6-astra", base)
	require.NotNil(t, failure)
	require.Equal(t, "probe_in_progress", failure.code)

	// The failure policy is runtime-controlled. A probe that started under the
	// old setting must apply the value current when its final failure arrives.
	svc.SetCodexTurnStateRuntimeSettingsWithControls(true, true, nil, CodexTurnStateHarvestControls{
		SpeedPreset: "fast", MaxRequestsPerRound: 12, FailureCooldownSeconds: 60,
	})
	svc.finishCodexTurnStateProbe(lease, &openAICodexTurnStateProbeFailure{
		code: "invalid_state", err: context.DeadlineExceeded, dispatched: true,
	}, base.Add(time.Second))
	_, failure = svc.beginCodexTurnStateProbe(901, "gpt-6-astra", base.Add(59*time.Second))
	require.NotNil(t, failure)
	require.Equal(t, "cooldown", failure.code)

	otherAccount, failure := svc.beginCodexTurnStateProbe(902, "gpt-6-astra", base.Add(59*time.Second))
	require.Nil(t, failure)
	svc.finishCodexTurnStateProbe(otherAccount, nil, base.Add(59*time.Second))
	otherModel, failure := svc.beginCodexTurnStateProbe(901, "gpt-6-astra-high", base.Add(59*time.Second))
	require.Nil(t, failure)
	svc.finishCodexTurnStateProbe(otherModel, nil, base.Add(59*time.Second))

	afterCooldown, failure := svc.beginCodexTurnStateProbe(901, "gpt-6-astra", base.Add(61*time.Second))
	require.Nil(t, failure)
	svc.finishCodexTurnStateProbe(afterCooldown, nil, base.Add(61*time.Second))
}

func TestCodexTurnStateProbeGateSkipsLocalAndCancelledFailures(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	base := time.Now().UTC().Truncate(time.Second)

	lease, failure := svc.beginCodexTurnStateProbe(903, "gpt-5.5", base)
	require.Nil(t, failure)
	svc.finishCodexTurnStateProbe(lease, &openAICodexTurnStateProbeFailure{
		code: "request_budget_exhausted", err: context.DeadlineExceeded,
	}, base)
	lease, failure = svc.beginCodexTurnStateProbe(903, "gpt-5.5", base)
	require.Nil(t, failure)
	svc.finishCodexTurnStateProbe(lease, &openAICodexTurnStateProbeFailure{
		code: "cancelled", err: context.Canceled, dispatched: true,
	}, base)
	lease, failure = svc.beginCodexTurnStateProbe(903, "gpt-5.5", base)
	require.Nil(t, failure)
	svc.finishCodexTurnStateProbe(lease, nil, base)
}

func TestCodexTurnStateProbeGatePoolGenerationFencesOldCompletion(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	controls := CodexTurnStateHarvestControls{SpeedPreset: "fast", MaxRequestsPerRound: 12, FailureCooldownSeconds: 60}
	svc.SetCodexTurnStateRuntimeSettingsWithControls(true, true, []string{"http://proxy-a.example:8080"}, controls)
	base := time.Now().UTC().Truncate(time.Second)
	oldLease, failure := svc.beginCodexTurnStateProbe(904, "gpt-5.5", base)
	require.Nil(t, failure)

	svc.SetCodexTurnStateRuntimeSettingsWithControls(true, true, []string{"http://proxy-b.example:8080"}, controls)
	newLease, failure := svc.beginCodexTurnStateProbe(904, "gpt-5.5", base)
	require.Nil(t, failure)
	svc.finishCodexTurnStateProbe(oldLease, &openAICodexTurnStateProbeFailure{
		code: "invalid_state", err: context.DeadlineExceeded, dispatched: true,
	}, base)
	_, failure = svc.beginCodexTurnStateProbe(904, "gpt-5.5", base)
	require.NotNil(t, failure)
	require.Equal(t, "probe_in_progress", failure.code, "an old generation must not overwrite the new in-flight lease")
	svc.finishCodexTurnStateProbe(newLease, nil, base)
	_, failure = svc.beginCodexTurnStateProbe(904, "gpt-5.5", base)
	require.Nil(t, failure)
}

func TestCodexTurnStateProbeGateBoundsInFlightState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	base := time.Now().UTC().Truncate(time.Second)
	leases := make([]codexTurnStateProbeLease, 0, openAICodexTurnStateMaxProbeGates)
	for accountID := int64(1); accountID <= openAICodexTurnStateMaxProbeGates; accountID++ {
		lease, failure := svc.beginCodexTurnStateProbe(accountID, "gpt-5.5", base)
		require.Nil(t, failure)
		leases = append(leases, lease)
	}
	require.Len(t, svc.codexTurnStateProbeGates, openAICodexTurnStateMaxProbeGates)
	_, failure := svc.beginCodexTurnStateProbe(openAICodexTurnStateMaxProbeGates+1, "gpt-5.5", base)
	require.NotNil(t, failure)
	require.Equal(t, "capacity_full", failure.code)

	svc.finishCodexTurnStateProbe(leases[0], nil, base)
	_, failure = svc.beginCodexTurnStateProbe(openAICodexTurnStateMaxProbeGates+1, "gpt-5.5", base)
	require.Nil(t, failure)
	require.Len(t, svc.codexTurnStateProbeGates, openAICodexTurnStateMaxProbeGates)
}

func TestProbeCodexTurnStateTicketSingleflightsCanonicalModel(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: &codexTurnStateReleasedBody{
			reader:  strings.NewReader(`data: {"type":"response.failed"}` + "\n\n"),
			started: started,
			release: release,
		},
	}}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(905)
	firstDone := make(chan *openAICodexTurnStateProbeFailure, 1)
	go func() {
		_, failure := svc.probeCodexTurnStateTicket(context.Background(), account, "gpt-6")
		firstDone <- failure
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first probe did not reach the upstream response")
	}
	_, failure := svc.probeCodexTurnStateTicket(context.Background(), account, "gpt-6-astra")
	require.NotNil(t, failure)
	require.Equal(t, "probe_in_progress", failure.code)
	close(release)
	select {
	case firstFailure := <-firstDone:
		require.NotNil(t, firstFailure)
		require.True(t, firstFailure.dispatched)
	case <-time.After(2 * time.Second):
		t.Fatal("first probe did not finish")
	}
	require.Len(t, upstream.requests, 1)
}
