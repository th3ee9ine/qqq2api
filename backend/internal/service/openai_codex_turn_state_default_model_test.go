package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTurnStateConfiguredModelAcrossLifecycle(t *testing.T) {
	for _, phase := range []string{"initial", "renewal", "recovery"} {
		t.Run(phase, func(t *testing.T) {
			s, repo, account := newTurnStateAutoService(t)
			settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
			settings.values[SettingKeyOpenAICodexTurnStateDefaultModel] = "custom/probe-model"
			if phase != "initial" {
				at := time.Now().Add(-51 * time.Minute)
				account.Extra = map[string]any{CodexTurnStateAutoExtraKey: testGlobalTurnStateToken(at, 10), CodexTurnStateAutoSetAtExtraKey: at.UnixMilli()}
				repo.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
			}
			var sent []string
			s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
				var body struct {
					Model string `json:"model"`
				}
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				sent = append(sent, body.Model)
				return turnStateResponse(testGlobalTurnStateToken(time.Now(), 10)), nil
			}}
			if phase == "recovery" {
				s.collectOpenAICodexTurnState(context.Background(), account, testGlobalTurnStateToken(time.Now(), 11), codexTurnStateAutoToken(account))
			} else {
				// A different routed user model must not override the probe setting.
				s.autoTurnStateForAccount(context.Background(), account, "gpt-5.5", "gpt-5.6-sol")
			}
			waitTurnStateAutoIdle(t, s)
			require.Equal(t, []string{"custom/probe-model"}, sent)
			stored, err := repo.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.NotEmpty(t, codexTurnStateAutoToken(stored))
			require.False(t, codexTurnStateRecoveryFromAccount(stored).Pending)
		})
	}
}

func TestCodexTurnStatePoolRetryReadsChangedDefaultModel(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateDefaultModel] = "custom/first"
	s.proxyRepo = &turnStateProxyRepo{proxies: []Proxy{{Protocol: "http", Host: "pool.test", Port: 8080, Status: StatusActive}}}
	var sent []string
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		var body struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		sent = append(sent, body.Model)
		if len(sent) == 1 {
			settings.values[SettingKeyOpenAICodexTurnStateDefaultModel] = "custom/updated"
			s.settingService.InvalidateOpenAICodexTurnStateCache()
			return turnStateResponse(""), nil
		}
		return turnStateResponse(testGlobalTurnStateToken(time.Now(), 10)), nil
	}}
	s.autoTurnStateForAccount(context.Background(), account, "gpt-5.5")
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, []string{"custom/first", "custom/updated"}, sent)
}
