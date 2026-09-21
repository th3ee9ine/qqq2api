package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type reliabilitySettingRepo struct {
	SettingRepository
	values        map[string]string
	err           error
	requestedKeys []string
}

func (r *reliabilitySettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.values[key], nil
}

func (r *reliabilitySettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.requestedKeys = append([]string(nil), keys...)
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func TestAggregateReliabilityAvailabilityDeduplicatesLimitedAccounts(t *testing.T) {
	now := time.Now().UTC()
	tempUntil := now.Add(time.Minute)
	availability := &OpsAccountAvailability{Accounts: map[int64]*AccountAvailability{
		1: {
			Platform: "openai", IsRateLimited: true, IsOverloaded: true,
			TempUnschedulableUntil: &tempUntil,
		},
		2: {Platform: "openai", TempUnschedulableUntil: &tempUntil},
		3: {Platform: "anthropic", IsAvailable: true, HasError: true},
	}}

	got := aggregateReliabilityAvailability(availability, now)
	require.Equal(t, int64(3), got.Total)
	require.Equal(t, int64(1), got.Available)
	require.Equal(t, int64(1), got.RateLimitCount)
	require.Equal(t, int64(1), got.OverloadCount)
	require.Equal(t, int64(2), got.TempUnschedulableCount)
	require.Equal(t, int64(1), got.Limited, "an account in both 429 and 529 states must be counted once")
	require.Equal(t, int64(2), got.CoolingDown, "overlapping cooldown states must be counted once per account")
	require.Equal(t, int64(1), got.Error)
	require.Equal(t, int64(2), got.ByPlatform["openai"].TotalAccounts)
}

func TestAggregateReliabilityConcurrencyUsesPlatformTotalsOnly(t *testing.T) {
	current, queued := aggregateReliabilityConcurrency(map[string]*PlatformConcurrencyInfo{
		"openai":    {CurrentInUse: 3, WaitingInQueue: 2},
		"anthropic": {CurrentInUse: 4, WaitingInQueue: 1},
		"nil":       nil,
	})
	require.Equal(t, int64(7), current)
	require.Equal(t, int64(3), queued)
}

func TestGetReliabilityStatusAggregatesMetricsAndRedactsSensitiveValues(t *testing.T) {
	now := time.Now().UTC()
	tempUntil := now.Add(time.Minute)
	repo := &reliabilitySettingRepo{values: map[string]string{
		SettingKeyOverloadCooldownSettings:     `{"enabled":true,"cooldown_minutes":12}`,
		SettingKeyRateLimit429CooldownSettings: `{"enabled":true,"cooldown_seconds":30}`,
		SettingKeyStreamTimeoutSettings:        `{"enabled":true,"action":"temp_unsched","temp_unsched_minutes":7,"threshold_count":2,"threshold_window_minutes":8}`,
		SettingKeyPanelRateLimitSettings:       `{"enabled":true,"user_rpm":240,"heavy_rpm":60,"exempt_admin":true,"public_ip_rpm":300}`,
	}}
	cfg := &config.Config{Ops: config.OpsConfig{Enabled: true}}
	cfg.Gateway.ResponseHeaderTimeout = 31
	cfg.Gateway.OpenAIResponseHeaderTimeout = 32
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 33
	cfg.Gateway.StreamDataIntervalTimeout = 34
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.OAuthEnabled = true

	svc := &OpsService{settingRepo: repo, cfg: cfg}
	svc.getAccountAvailability = func(context.Context, string, *int64) (*OpsAccountAvailability, error) {
		return &OpsAccountAvailability{Accounts: map[int64]*AccountAvailability{
			1: {
				AccountName: "ACCOUNT_SECRET_TOKEN", Platform: "openai", IsRateLimited: true,
				TempUnschedulableUntil: &tempUntil, ErrorMessage: "RAW_ERROR_PROXY_https://secret.invalid PROMPT_SECRET",
			},
			2: {AccountName: "SECOND_SECRET", Platform: "anthropic", IsAvailable: true},
		}}, nil
	}
	svc.getReliabilityConcurrency = func(context.Context) (map[string]*PlatformConcurrencyInfo, error) {
		return map[string]*PlatformConcurrencyInfo{
			"openai":    {CurrentInUse: 3, WaitingInQueue: 2},
			"anthropic": {CurrentInUse: 4, WaitingInQueue: 1},
		}, nil
	}
	svc.getReliabilityTraffic = func(_ context.Context, filter *OpsDashboardFilter) (*OpsRealtimeTrafficSummary, error) {
		require.WithinDuration(t, now.Add(-reliabilityTrafficWindow), filter.StartTime, 2*time.Second)
		return &OpsRealtimeTrafficSummary{QPS: OpsRateSummary{Current: 1.25}, TPS: OpsRateSummary{Current: 42}}, nil
	}
	svc.getReliabilityOverview = func(context.Context, *OpsDashboardFilter) (*OpsDashboardOverview, error) {
		return &OpsDashboardOverview{
			RequestCountTotal: 100, ErrorCountTotal: 10, ErrorRate: 0.10,
			Upstream429Count: 4, Upstream529Count: 2,
		}, nil
	}

	status, err := svc.GetReliabilityStatus(context.Background())
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.NotNil(t, status.AccountAvailability)
	require.Equal(t, int64(2), status.AccountAvailability.Total)
	require.Equal(t, int64(1), status.AccountAvailability.Limited)
	require.Equal(t, int64(1), status.AccountAvailability.CoolingDown)
	require.Equal(t, int64(7), *status.Traffic.CurrentConcurrency)
	require.Equal(t, int64(3), *status.Traffic.QueuedRequests)
	require.Equal(t, 1.25, *status.Traffic.QPS)
	require.Equal(t, float64(42), *status.Traffic.TPS)
	require.Equal(t, int64(100), *status.Traffic.Requests)
	require.Equal(t, float64(10), *status.Traffic.ErrorRate)
	require.Equal(t, int64(4), *status.Traffic.Upstream429)
	require.Equal(t, int64(2), *status.Traffic.Upstream529)
	require.True(t, status.Runtime.Error.Available)
	require.Equal(t, config.DefaultGatewayUpstreamDialTimeout, time.Duration(status.Connection.DialTimeout)*time.Second)
	require.Equal(t, config.DefaultGatewayUpstreamTLSHandshakeTimeout, time.Duration(status.Connection.TLSHandshakeTimeout)*time.Second)
	require.Equal(t, OpsReliabilityTurnState{
		Supported: true, HTTPEnabled: true, WebSocketEnabled: true,
		CrossAccountProtection: true, HTTPCrossAccountProtection: true, WebSocketCrossAccountProtection: true,
	}, status.TurnState)
	require.NotContains(t, status.Diagnostics.Warnings, "websocket_turn_state_not_account_scoped")

	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	jsonText := string(encoded)
	for _, secret := range []string{"ACCOUNT_SECRET_TOKEN", "SECOND_SECRET", "RAW_ERROR_PROXY", "secret.invalid", "PROMPT_SECRET"} {
		require.NotContains(t, jsonText, secret)
	}
	require.NotContains(t, jsonText, `"fallback"`, "inactive legacy fallback settings must not be advertised")
	require.NotContains(t, jsonText, "turn_state_from_upstream", "opaque turn-state values must never be returned")
}

func TestGetReliabilityStatusOpsDisabledLeavesRuntimeMetricsUnavailable(t *testing.T) {
	repo := &reliabilitySettingRepo{values: map[string]string{
		SettingKeyOverloadCooldownSettings:     "{invalid",
		SettingKeyRateLimit429CooldownSettings: "{invalid",
		SettingKeyStreamTimeoutSettings:        "{invalid",
		SettingKeyPanelRateLimitSettings:       "{invalid",
		SettingKeyEnableModelFallback:          "true",
		SettingKeyFallbackModelOpenAI:          "SHOULD_NOT_BE_READ",
	}}
	svc := &OpsService{settingRepo: repo, cfg: &config.Config{Ops: config.OpsConfig{Enabled: false}}}

	status, err := svc.GetReliabilityStatus(context.Background())
	require.NoError(t, err)
	require.False(t, status.Enabled)
	require.Nil(t, status.AccountAvailability)
	require.Nil(t, status.Traffic.CurrentConcurrency)
	require.Nil(t, status.Traffic.QPS)
	require.False(t, status.Runtime.Error.Available)
	require.Contains(t, status.Diagnostics.Warnings, "ops_monitoring_disabled")
	require.ElementsMatch(t, []string{
		"overload_cooldown_invalid",
		"rate_limit_429_cooldown_invalid",
		"stream_timeout_settings_invalid",
		"panel_rate_limit_settings_invalid",
	}, status.Diagnostics.SourceErrors)
	require.NotContains(t, repo.requestedKeys, SettingKeyEnableModelFallback)
	require.NotContains(t, repo.requestedKeys, SettingKeyFallbackModelOpenAI)
}

func TestGetReliabilityStatusSettingsFailureStillReturnsSafeConfig(t *testing.T) {
	cfg := &config.Config{Ops: config.OpsConfig{Enabled: false}}
	cfg.Gateway.MaxConnsPerHost = 77
	repo := &reliabilitySettingRepo{err: errors.New("DB_SECRET_CONNECTION_STRING")}
	svc := &OpsService{settingRepo: repo, cfg: cfg}

	status, err := svc.GetReliabilityStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, 77, status.Connection.MaxConnsPerHost)
	require.Contains(t, status.Diagnostics.SourceErrors, "settings_unavailable")

	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), "DB_SECRET_CONNECTION_STRING"))
}

func TestGetReliabilityStatusRealtimeDisabledExplainsUnavailableMetrics(t *testing.T) {
	repo := &reliabilitySettingRepo{values: map[string]string{
		SettingKeyOpsRealtimeMonitoringEnabled: "false",
	}}
	svc := &OpsService{settingRepo: repo, cfg: &config.Config{Ops: config.OpsConfig{Enabled: true}}}
	svc.getAccountAvailability = func(context.Context, string, *int64) (*OpsAccountAvailability, error) {
		return &OpsAccountAvailability{Accounts: map[int64]*AccountAvailability{}}, nil
	}
	svc.getReliabilityConcurrency = func(context.Context) (map[string]*PlatformConcurrencyInfo, error) {
		t.Fatal("concurrency query must not run while realtime monitoring is disabled")
		return nil, nil
	}
	svc.getReliabilityTraffic = func(context.Context, *OpsDashboardFilter) (*OpsRealtimeTrafficSummary, error) {
		t.Fatal("traffic query must not run while realtime monitoring is disabled")
		return nil, nil
	}
	svc.getReliabilityOverview = func(context.Context, *OpsDashboardFilter) (*OpsDashboardOverview, error) {
		t.Fatal("overview query must not run while realtime monitoring is disabled")
		return nil, nil
	}

	status, err := svc.GetReliabilityStatus(context.Background())
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.True(t, status.Diagnostics.OpsMonitoringEnabled)
	require.False(t, status.Diagnostics.RealtimeMonitoringEnabled)
	require.Contains(t, status.Diagnostics.Warnings, "realtime_monitoring_disabled")
	require.Nil(t, status.Traffic.CurrentConcurrency)
	require.Nil(t, status.Traffic.QueuedRequests)
	require.Nil(t, status.Traffic.QPS)
	require.Nil(t, status.Traffic.TPS)
	require.False(t, status.Runtime.Error.Available)
}

func TestGetReliabilityStatusNilTrafficSummaryIsUnavailable(t *testing.T) {
	svc := &OpsService{cfg: &config.Config{Ops: config.OpsConfig{Enabled: true}}}
	svc.getAccountAvailability = func(context.Context, string, *int64) (*OpsAccountAvailability, error) {
		return &OpsAccountAvailability{Accounts: map[int64]*AccountAvailability{}}, nil
	}
	svc.getReliabilityConcurrency = func(context.Context) (map[string]*PlatformConcurrencyInfo, error) {
		return map[string]*PlatformConcurrencyInfo{}, nil
	}
	svc.getReliabilityTraffic = func(context.Context, *OpsDashboardFilter) (*OpsRealtimeTrafficSummary, error) {
		return nil, nil
	}
	svc.getReliabilityOverview = func(context.Context, *OpsDashboardFilter) (*OpsDashboardOverview, error) {
		return nil, nil
	}

	status, err := svc.GetReliabilityStatus(context.Background())

	require.NoError(t, err)
	require.Nil(t, status.Traffic.QPS)
	require.Nil(t, status.Traffic.TPS)
	require.Contains(t, status.Diagnostics.SourceErrors, "traffic_unavailable")
}
