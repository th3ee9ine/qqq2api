package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type paidCreditsRecoveryRepo struct {
	AccountRepository
	account       *Account
	updateErr     error
	clearCalls    int
	clearedID     int64
	clearedUntil  time.Time
	clearedReason string
	updates       map[string]any
}

func (r *paidCreditsRecoveryRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *paidCreditsRecoveryRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return r.updateErr
}

func (r *paidCreditsRecoveryRepo) ClearTempUnschedulableIfReason(_ context.Context, id int64, until time.Time, reason string) (bool, error) {
	r.clearCalls++
	r.clearedID, r.clearedUntil, r.clearedReason = id, until, reason
	return true, nil
}

func TestPaidCreditsThresholdRecoveryUsesNestedSnapshot(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Hour)
	for _, tc := range []struct {
		name      string
		credits   string
		fetchedAt int64
		configure func(*Account)
		wantClear bool
	}{
		{name: "positive balance", credits: `{"has_credits":true,"balance":"25125"}`, fetchedAt: now.Unix(), wantClear: true},
		{name: "balance without flag", credits: `{"balance":"0.5"}`, fetchedAt: now.Unix(), wantClear: true},
		{name: "unlimited", credits: `{"unlimited":true}`, fetchedAt: now.Unix(), wantClear: true},
		{name: "explicit no credits", credits: `{"has_credits":false,"balance":"25125"}`, fetchedAt: now.Unix()},
		{name: "empty balance", credits: `{"has_credits":true,"balance":"0"}`, fetchedAt: now.Unix()},
		{name: "overage reached", credits: `{"has_credits":true,"balance":"25125","overage_limit_reached":true}`, fetchedAt: now.Unix()},
		{name: "stale", credits: `{"has_credits":true,"balance":"1"}`, fetchedAt: now.Add(-3 * time.Hour).Unix()},
		{name: "future", credits: `{"has_credits":true,"balance":"1"}`, fetchedAt: now.Add(time.Minute).Unix()},
		{name: "missing timestamp", credits: `{"has_credits":true,"balance":"1"}`},
		{name: "transport block", credits: `{"has_credits":true,"balance":"1"}`, fetchedAt: now.Unix(), configure: func(a *Account) {
			a.TempUnschedulableReason = BuildTempUnschedReasonPayload("transport_error", "upstream unavailable")
		}},
		{name: "shadow", credits: `{"has_credits":true,"balance":"1"}`, fetchedAt: now.Unix(), configure: func(a *Account) { parent := int64(1); a.ParentAccountID = &parent }},
		{name: "api key", credits: `{"has_credits":true,"balance":"1"}`, fetchedAt: now.Unix(), configure: func(a *Account) { a.Type = AccountTypeAPIKey }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, TempUnschedulableUntil: &until, TempUnschedulableReason: BuildAccountSchedulingThresholdReason("quota threshold")}
			if tc.configure != nil {
				tc.configure(a)
			}
			r := &paidCreditsRecoveryRepo{account: a}
			var credits OpenAIPaidCredits
			require.NoError(t, json.Unmarshal([]byte(tc.credits), &credits))
			(&OpenAIQuotaService{accountRepo: r}).clearPaidCreditsThresholdPause(context.Background(), a.ID, &credits, tc.fetchedAt)
			if !tc.wantClear {
				require.Zero(t, r.clearCalls)
				return
			}
			require.Equal(t, 1, r.clearCalls)
			require.Equal(t, a.ID, r.clearedID)
			require.Equal(t, until, r.clearedUntil)
			require.Equal(t, a.TempUnschedulableReason, r.clearedReason)
		})
	}
}

func TestCachePaidCreditsSnapshotRecoversOnlyAfterWrite(t *testing.T) {
	until := time.Now().Add(time.Hour)
	for _, writeFailed := range []bool{false, true} {
		t.Run(map[bool]string{false: "persisted", true: "write failed"}[writeFailed], func(t *testing.T) {
			r := &paidCreditsRecoveryRepo{account: &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, TempUnschedulableUntil: &until, TempUnschedulableReason: BuildAccountSchedulingThresholdReason("quota threshold")}}
			if writeFailed {
				r.updateErr = errors.New("write failed")
			}
			err := (&OpenAIQuotaService{accountRepo: r}).CachePaidCreditsSnapshot(context.Background(), 42, &OpenAICredits{HasCredits: true, Balance: "25125"})
			if writeFailed {
				require.Error(t, err)
				require.Zero(t, r.clearCalls)
				return
			}
			require.NoError(t, err)
			require.True(t, openAIPaidCreditsSnapshotActive(r.updates, time.Now()))
			require.Equal(t, 1, r.clearCalls)
		})
	}
}
