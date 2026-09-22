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

func TestCodexTurnStateManualTicketOneTimeAccountAndModelIsolation(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	base := time.Now().UTC().Truncate(time.Second)
	makeTicket := func(marker byte) OpenAICodexTurnStateSnapshot {
		t.Helper()
		token, err := ParseOpenAICodexTurnState(collectorTestToken(t, base, 2, marker))
		require.NoError(t, err)
		return OpenAICodexTurnStateSnapshot{Token: token, Route: "manual"}
	}

	accountAModelA := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 801, Scope: "manual", Model: "gpt-5"})
	accountAModelB := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 801, Scope: "manual", Model: "gpt-6"})
	accountBModelA := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 802, Scope: "manual", Model: "gpt-5"})
	first := makeTicket(101)
	second := makeTicket(102)
	third := makeTicket(103)
	svc.storeManualCodexTurnStateTicket(accountAModelA, first, base)
	svc.storeManualCodexTurnStateTicket(accountAModelB, second, base)
	svc.storeManualCodexTurnStateTicket(accountBModelA, third, base)

	wrongAccount := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 803, Scope: "execution", Model: "gpt-5"})
	svc.consumeManualCodexTurnStateTicket(wrongAccount, base)
	_, usable := svc.codexTurnStateCollector.Acquire(wrongAccount, base)
	require.False(t, usable)

	firstScope := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 801, Scope: "execution-a", Model: "gpt-5"})
	svc.consumeManualCodexTurnStateTicket(firstScope, base)
	snapshot, usable := svc.codexTurnStateCollector.Acquire(firstScope, base)
	require.True(t, usable)
	require.Equal(t, first.Token.Value, snapshot.Token.Value)

	secondScope := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 801, Scope: "execution-b", Model: "gpt-5"})
	svc.consumeManualCodexTurnStateTicket(secondScope, base)
	_, usable = svc.codexTurnStateCollector.Acquire(secondScope, base)
	require.False(t, usable, "one manual ticket may only seed a single execution scope")

	otherModel := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 801, Scope: "execution-a", Model: "gpt-6"})
	svc.consumeManualCodexTurnStateTicket(otherModel, base)
	snapshot, usable = svc.codexTurnStateCollector.Acquire(otherModel, base)
	require.True(t, usable)
	require.Equal(t, second.Token.Value, snapshot.Token.Value)

	otherAccount := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 802, Scope: "execution-a", Model: "gpt-5"})
	svc.consumeManualCodexTurnStateTicket(otherAccount, base)
	snapshot, usable = svc.codexTurnStateCollector.Acquire(otherAccount, base)
	require.True(t, usable)
	require.Equal(t, third.Token.Value, snapshot.Token.Value)
}

func TestCodexTurnStateManualTicketRejectsInvalidatedAccountGeneration(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	base := time.Now().UTC().Truncate(time.Second)
	token, err := ParseOpenAICodexTurnState(collectorTestToken(t, base, 2, 104))
	require.NoError(t, err)
	manual := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 804, Scope: "manual", Model: "gpt-5"})
	svc.storeManualCodexTurnStateTicket(manual, OpenAICodexTurnStateSnapshot{Token: token, Route: "manual"}, base)
	svc.codexTurnStateCollector.DeleteAccount(804)
	require.False(t, svc.codexTurnStateCollector.IsCurrentKey(manual))

	execution := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 804, Scope: "execution", Model: "gpt-5"})
	svc.consumeManualCodexTurnStateTicket(execution, base)
	_, usable := svc.codexTurnStateCollector.Acquire(execution, base)
	require.False(t, usable, "stale manual data must not survive account credential rotation")
}

func TestCodexTurnStateManualTicketReplacesHealthyActiveAndFencesOldResponse(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	base := time.Now().UTC().Truncate(time.Second)
	key := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 805, Scope: "execution", Model: "gpt-5"})
	oldValue := collectorTestToken(t, base, 2, 105)
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, oldValue, "probe", base))
	old, usable := svc.codexTurnStateCollector.Acquire(key, base)
	require.True(t, usable)

	manualKey := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 805, Scope: "manual", Model: "gpt-5"})
	newValue := collectorTestToken(t, base.Add(time.Second), 2, 106)
	newToken, err := ParseOpenAICodexTurnState(newValue)
	require.NoError(t, err)
	svc.storeManualCodexTurnStateTicket(manualKey, OpenAICodexTurnStateSnapshot{Token: newToken, Route: "manual"}, base)
	svc.consumeManualCodexTurnStateTicket(key, base.Add(time.Second))
	current, usable := svc.codexTurnStateCollector.Acquire(key, base.Add(time.Second))
	require.True(t, usable)
	require.Equal(t, newValue, current.Token.Value)
	require.Greater(t, current.Version, old.Version)

	staleValue := collectorTestToken(t, base.Add(2*time.Second), 2, 107)
	require.False(t, svc.codexTurnStateCollector.ObserveQualified(key, staleValue, old, nil, base.Add(2*time.Second)))
	current, usable = svc.codexTurnStateCollector.Acquire(key, base.Add(2*time.Second))
	require.True(t, usable)
	require.Equal(t, newValue, current.Token.Value)
}

func TestCodexTurnStateManualTicketExpiresAtCookieBoundary(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	for _, test := range []struct {
		name     string
		elapsed  time.Duration
		accepted bool
	}{
		{name: "239_seconds", elapsed: 239 * time.Second, accepted: true},
		{name: "240_seconds", elapsed: 240 * time.Second, accepted: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := newCodexTurnStateGatewayTestService(nil)
			manual := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 806, Scope: "manual", Model: "gpt-5"})
			value := collectorTestToken(t, base, 2, 108)
			token, err := ParseOpenAICodexTurnState(value)
			require.NoError(t, err)
			svc.storeManualCodexTurnStateTicket(manual, OpenAICodexTurnStateSnapshot{
				Token: token, Route: "manual", HarvestSessionID: "manual-session",
				HarvestCookies:   []OpenAICodexTurnStateCookie{{Name: "session", Value: "opaque"}},
				HarvestCookiesAt: base,
			}, base)
			execution := svc.codexTurnStateCollector.BindKey(OpenAICodexTurnStateKey{AccountID: 806, Scope: "execution", Model: "gpt-5"})
			now := base.Add(test.elapsed)
			svc.consumeManualCodexTurnStateTicket(execution, now)
			snapshot, usable := svc.codexTurnStateCollector.Acquire(execution, now)
			require.Equal(t, test.accepted, usable)
			if usable {
				require.Equal(t, value, snapshot.Token.Value)
				require.True(t, snapshot.cookiesFresh(now))
			}
		})
	}
}
