package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/xai"
	"github.com/tidwall/gjson"
)

func TestGrok47RetainsReasoningAcrossCompatibleProtocols(t *testing.T) {
	for _, model := range []string{"grok-4.7", "grok-4.7-latest", "xai/grok-4.7"} {
		t.Run(model, func(t *testing.T) {
			responses, err := normalizeGrokResponsesReasoningEffort([]byte(`{"reasoning":{"effort":"xhigh"}}`), model)
			require.NoError(t, err)
			require.Equal(t, "xhigh", gjson.GetBytes(responses, "reasoning.effort").String())
			chat, err := normalizeGrokChatReasoningEffort([]byte(`{"reasoning_effort":"xhigh"}`), model)
			require.NoError(t, err)
			require.Equal(t, "xhigh", gjson.GetBytes(chat, "reasoning_effort").String())
			require.True(t, grokChatResponsesBridgeModel(model))
		})
	}
}

func TestGrokQuotaDiagnosticsRemainAvailableDuringSchedulingCooldown(t *testing.T) {
	until := time.Now().Add(time.Hour)
	account := &Account{
		ID: 9001, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive,
		Schedulable: true, TempUnschedulableUntil: &until,
		TempUnschedulableReason: "grok upstream spending limit",
		Credentials: map[string]any{
			"access_token": "diagnostic-access-token", "refresh_token": "diagnostic-refresh-token",
			"expires_at": time.Now().Add(2 * time.Hour).Unix(),
		},
	}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	provider := NewGrokTokenProvider(repo, nil)
	_, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err, "normal model requests must still respect scheduling cooldowns")
	svc := NewGrokQuotaService(repo, nil, provider, &httpUpstreamRecorder{}, nil)
	loaded, token, _, err := svc.prepareProbe(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, account.ID, loaded.ID)
	require.Equal(t, "diagnostic-access-token", token)
	require.Equal(t, &until, account.TempUnschedulableUntil)
	require.False(t, account.IsSchedulable(), "diagnostics must not unpause model scheduling")
}

type grokMappingSettingsRepo struct {
	SettingRepository
	values map[string]string
	err    error
}

func (r *grokMappingSettingsRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return r.values, r.err
}

func TestLoadGrokModelMappingSettingsPrimesStartupAndPreservesExplicitOptOut(t *testing.T) {
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })
	for _, tc := range []struct {
		name            string
		values          map[string]string
		wantModel       string
		wantCrossClient bool
	}{
		{name: "unset settings use defaults", wantModel: xai.DefaultTextModel, wantCrossClient: true},
		{name: "operator settings", values: map[string]string{
			SettingKeyGrokDefaultTextModel:           " grok-4.7 ",
			SettingKeyGrokCrossClientModelMapEnabled: "false",
		}, wantModel: "grok-4.7", wantCrossClient: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{})
			svc := &SettingService{settingRepo: &grokMappingSettingsRepo{values: tc.values}}
			require.NoError(t, svc.LoadGrokModelMappingSettings(context.Background()))
			account := &Account{Platform: PlatformGrok, Credentials: map[string]any{}}
			mappings := account.GetModelMapping()
			require.Equal(t, tc.wantModel, mappings["grok"])
			_, crossClient := mappings["claude-*"]
			require.Equal(t, tc.wantCrossClient, crossClient)
		})
	}
	before := xai.RuntimeModelMappingOptions()
	svc := &SettingService{settingRepo: &grokMappingSettingsRepo{err: errors.New("database unavailable")}}
	require.Error(t, svc.LoadGrokModelMappingSettings(context.Background()))
	require.Equal(t, before, xai.RuntimeModelMappingOptions(), "read failures preserve the last usable configuration")
}

func TestRefreshCachedSettingsUpdatesGrokAccountModelMappings(t *testing.T) {
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })
	xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{DefaultText: "grok-4.6", EnableCrossClientMap: true})
	account := &Account{Platform: PlatformGrok, Credentials: map[string]any{}}
	require.Equal(t, "grok-4.6", account.GetModelMapping()["claude-*"])
	(&SettingService{}).refreshCachedSettings(&SystemSettings{
		GrokDefaultTextModel: "grok-4.7", GrokCrossClientModelMapEnabled: false,
	})
	mappings := account.GetModelMapping()
	require.Equal(t, "grok-4.7", mappings["grok"])
	require.NotContains(t, mappings, "claude-*", "existing account caches must observe the updated mapping policy")
}
