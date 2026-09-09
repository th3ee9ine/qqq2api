//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func paidCreditsRecoveryAdmissionPolicy() openAIOAuthAdmissionPolicy {
	return openAIOAuthAdmissionPolicy{baseInterval: time.Millisecond, highQuotaInterval: 2 * time.Millisecond, criticalQuotaInterval: 3 * time.Millisecond, severeQuotaInterval: 4 * time.Millisecond, maxQueueWait: 20 * time.Millisecond, stateTTL: time.Minute, maxAccounts: 128}
}

func paidCreditsThresholdRecoveryAccount(now time.Time) *Account {
	until := now.Add(time.Hour)
	return &Account{
		ID:                      7101,
		Platform:                PlatformOpenAI,
		Type:                    AccountTypeOAuth,
		Status:                  StatusActive,
		Schedulable:             true,
		TempUnschedulableUntil:  &until,
		TempUnschedulableReason: BuildAccountSchedulingThresholdReason("quota exhausted"),
		Extra: map[string]any{
			openaiQuotaPaidCreditsKey: map[string]any{
				"has_credits": true,
				"balance":     "25125",
				"fetched_at":  now.Unix(),
			},
		},
	}
}

func TestPaidCreditsTemporaryPauseEligibilityConsistent(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		mutate func(*Account)
		paused bool
	}{
		{name: "OAuth parent with fresh paid credits"},
		{name: "shadow", mutate: func(a *Account) { parent := int64(5); a.ParentAccountID = &parent }, paused: true},
		{name: "API key", mutate: func(a *Account) { a.Type = AccountTypeAPIKey }, paused: true},
		{name: "other platform", mutate: func(a *Account) { a.Platform = PlatformGrok }, paused: true},
		{name: "authentication", mutate: func(a *Account) {
			a.TempUnschedulableReason = BuildTempUnschedReasonPayload("ratelimit", "authentication failed")
		}, paused: true},
		{name: "transport", mutate: func(a *Account) {
			a.TempUnschedulableReason = BuildTempUnschedReasonPayload("upstream_transport", "connection failed")
		}, paused: true},
		{name: "custom", mutate: func(a *Account) {
			a.TempUnschedulableReason = BuildTempUnschedReasonPayload("custom_rule", "temporary pause")
		}, paused: true},
		{name: "stale snapshot", mutate: func(a *Account) {
			a.Extra[openaiQuotaPaidCreditsKey].(map[string]any)["fetched_at"] = now.Add(-3 * time.Hour).Unix()
		}, paused: true},
		{name: "empty credits", mutate: func(a *Account) {
			a.Extra[openaiQuotaPaidCreditsKey].(map[string]any)["balance"] = "0"
		}, paused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := paidCreditsThresholdRecoveryAccount(now)
			if tc.mutate != nil {
				tc.mutate(a)
			}
			require.Equal(t, tc.paused, a.hasActiveTemporarySchedulingPause(now))
			require.Equal(t, !tc.paused, a.IsSchedulable())
			require.Equal(t, tc.paused, accountPersistedSchedulingCooldownActive(a))
			deadline := (&OpenAIGatewayService{}).openAIOAuthAdmissionNotBefore(a, now)
			require.Equal(t, tc.paused, deadline.After(now))
		})
	}
}

func TestPaidCreditsRuntimeRecoveryAllowsRequestAdmission(t *testing.T) {
	now := time.Now()
	a := paidCreditsThresholdRecoveryAccount(now)
	svc := &OpenAIGatewayService{
		openAIOAuthAdmission: newOpenAIOAuthAdmissionController(paidCreditsRecoveryAdmissionPolicy()),
	}
	svc.BlockAccountScheduling(a, *a.TempUnschedulableUntil, "quota threshold")
	require.True(t, svc.isOpenAIAccountRuntimeBlocked(a))
	require.True(t, svc.openAIOAuthAdmission.accountNotBefore(a.ID).After(now))

	require.True(t, a.IsSchedulable())
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(a, "gpt-5.6-sol"))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(a))
	require.True(t, svc.openAIOAuthAdmission.accountNotBefore(a.ID).IsZero())
	require.NoError(t, svc.WaitForOpenAIOAuthAccountAdmission(context.Background(), a))
}

func TestPaidCreditsRuntimeRecoveryKeepsHardAndModelCooldowns(t *testing.T) {
	now := time.Now()
	for _, kind := range []string{"authentication", "rate limit", "overload", "model"} {
		t.Run(kind, func(t *testing.T) {
			a := paidCreditsThresholdRecoveryAccount(now)
			svc := &OpenAIGatewayService{}
			svc.BlockAccountScheduling(a, *a.TempUnschedulableUntil, "quota threshold")
			switch kind {
			case "authentication":
				a.TempUnschedulableReason = BuildTempUnschedReasonPayload("ratelimit", "authentication failed")
			case "rate limit":
				a.RateLimitResetAt = a.TempUnschedulableUntil
			case "overload":
				a.OverloadUntil = a.TempUnschedulableUntil
			case "model":
				svc.recordOpenAIAccountModelTransientFailure(a, "gpt-5.6-sol", now)
				svc.recordOpenAIAccountModelTransientFailure(a, "gpt-5.6-sol", now)
			}
			require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(a, "gpt-5.6-sol"))
			if kind == "model" {
				require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(a, "gpt-5.6-terra"))
			} else {
				require.True(t, svc.isOpenAIAccountRuntimeBlocked(a))
			}
		})
	}
}

func TestRuntimeRecoveryKeepsNewerAdmissionDeferAndSpacing(t *testing.T) {
	now := time.Now()
	a := paidCreditsThresholdRecoveryAccount(now)
	svc := &OpenAIGatewayService{}
	svc.BlockAccountScheduling(a, *a.TempUnschedulableUntil, "quota threshold")
	controller := svc.getOpenAIOAuthAdmissionController()
	newerDeadline := a.TempUnschedulableUntil.Add(time.Minute)
	controller.deferUntil(a.ID, newerDeadline)
	reservedStart := now.Add(time.Second)
	controller.mu.Lock()
	controller.accounts[a.ID].nextStart = reservedStart
	controller.mu.Unlock()

	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(a, "gpt-5.6-sol"))
	require.Equal(t, newerDeadline, controller.accountNotBefore(a.ID))
	controller.mu.Lock()
	defer controller.mu.Unlock()
	require.Equal(t, reservedStart, controller.accounts[a.ID].nextStart)
}
