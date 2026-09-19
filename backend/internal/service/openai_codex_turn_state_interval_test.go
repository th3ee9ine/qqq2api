package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func seedCodexTurnStateIntervalTestAccount(
	repo *turnStateAutoRepo,
	account *Account,
	model string,
	setAt, verifiedAt, probeAt int64,
	lastError string,
) {
	account.Extra = map[string]any{codexTurnStateModelExtraKey(model): map[string]any{
		CodexTurnStateAutoExtraKey:              "still-valid-state",
		CodexTurnStateAutoSetAtExtraKey:         setAt,
		CodexTurnStateAutoVerifiedAtExtraKey:    verifiedAt,
		CodexTurnStateAutoVerifiedModelExtraKey: model,
		CodexTurnStateAutoProbeAtExtraKey:       probeAt,
		CodexTurnStateAutoLastErrorExtraKey:     lastError,
	}}
	repo.mu.Lock()
	repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
	repo.mu.Unlock()
}

func TestCodexTurnStateOneMinuteRenewalIsNotThrottledByRecentSuccess(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-5"
	settings.values[SettingKeyOpenAICodexTurnStateAutoIntervalMinutes] = "1"
	s.settingService.InvalidateOpenAICodexTurnStateCache()

	now := time.Now()
	setAt := now.Add(-10 * time.Minute).UnixMilli()
	lastSuccessAt := now.Add(-2 * time.Minute).UnixMilli()
	seedCodexTurnStateIntervalTestAccount(repo, account, "gpt-5", setAt, lastSuccessAt, lastSuccessAt, "")

	var calls atomic.Int32
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse("renewed-state", "gpt-5"), nil
		}
		return turnStateModelResponse("", "gpt-5"), nil
	}}

	require.Equal(t, "still-valid-state", s.autoTurnStateForAccount(context.Background(), account, "gpt-5"))
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 2, calls.Load(),
		"a successful probe inside the fixed five-minute failure window must still renew on the configured one-minute interval")
}

func TestCodexTurnStateFailedAutomaticRoundKeepsFiveMinuteBackoff(t *testing.T) {
	for _, tc := range []struct {
		name      string
		probeAge  time.Duration
		wantCalls int32
	}{
		{name: "recent failure is throttled", probeAge: 4 * time.Minute, wantCalls: 0},
		{name: "failure retries after backoff", probeAge: 6 * time.Minute, wantCalls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, account := newTurnStateAutoService(t)
			settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
			settings.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-5"
			settings.values[SettingKeyOpenAICodexTurnStateAutoIntervalMinutes] = "1"
			s.settingService.InvalidateOpenAICodexTurnStateCache()

			now := time.Now()
			setAt := now.Add(-10 * time.Minute).UnixMilli()
			seedCodexTurnStateIntervalTestAccount(
				repo,
				account,
				"gpt-5",
				setAt,
				setAt,
				now.Add(-tc.probeAge).UnixMilli(),
				"transport_failed",
			)

			var calls atomic.Int32
			s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
				calls.Add(1)
				if req.Header.Get(openAICodexTurnStateHeader) == "" {
					return turnStateModelResponse("renewed-state", "gpt-5"), nil
				}
				return turnStateModelResponse("", "gpt-5"), nil
			}}

			require.Equal(t, "still-valid-state", s.autoTurnStateForAccount(context.Background(), account, "gpt-5"))
			waitTurnStateAutoIdle(t, s)
			require.Equal(t, tc.wantCalls, calls.Load())
		})
	}
}

func TestCodexTurnStateRenewalDueUsesLatestSuccessWithoutExtendingHardExpiry(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)

	t.Run("latest of set and verified timestamps is the renewal base", func(t *testing.T) {
		for _, tc := range []struct {
			name              string
			setAt, verifiedAt int64
		}{
			{
				name:       "verification is newer",
				setAt:      now.Add(-10 * time.Minute).UnixMilli(),
				verifiedAt: now.Add(-30 * time.Second).UnixMilli(),
			},
			{
				name:       "collection is newer",
				setAt:      now.Add(-30 * time.Second).UnixMilli(),
				verifiedAt: now.Add(-10 * time.Minute).UnixMilli(),
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				require.False(t, codexTurnStateAutomaticCollectionDue(
					"opaque-valid-state", tc.setAt, tc.verifiedAt, now, 1,
				))
				require.True(t, codexTurnStateAutomaticCollectionDue(
					"opaque-valid-state", tc.setAt, tc.verifiedAt, now.Add(30*time.Second), 1,
				))
			})
		}
	})

	t.Run("fresh verification cannot extend the original state expiry", func(t *testing.T) {
		token := testGlobalTurnStateToken(now.Add(-59*time.Minute), 10)
		setAt := now.Add(-10 * time.Minute).UnixMilli()
		verifiedAt := now.UnixMilli()

		require.False(t, codexTurnStateAutomaticCollectionDue(token, setAt, verifiedAt, now.Add(59*time.Second), 50))
		require.True(t, codexTurnStateAutomaticCollectionDue(token, setAt, verifiedAt, now.Add(time.Minute), 50),
			"the encoded state expiry must cap the configured interval even after a fresh verification")
	})
}
