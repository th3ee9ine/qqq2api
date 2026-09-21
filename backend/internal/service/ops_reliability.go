package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/config"
)

const reliabilityTrafficWindow = 5 * time.Minute

// OpsReliabilityStatus is the deliberately bounded, read-only projection used
// by the admin reliability page. It contains operational counters and config
// metadata only; credentials, proxy URLs, request bodies, and upstream error
// text are intentionally excluded.
type OpsReliabilityStatus struct {
	Enabled             bool                               `json:"enabled"`
	Timestamp           time.Time                          `json:"timestamp"`
	Cooldowns           OpsReliabilityCooldowns            `json:"cooldowns"`
	AccountAvailability *OpsReliabilityAccountAvailability `json:"account_availability,omitempty"`
	Traffic             OpsReliabilityTraffic              `json:"traffic"`
	Connection          OpsReliabilityConnection           `json:"connection"`
	TurnState           OpsReliabilityTurnState            `json:"turn_state"`
	Limits              OpsReliabilityLimits               `json:"limits"`
	Runtime             OpsReliabilityRuntime              `json:"runtime"`
	Diagnostics         OpsReliabilityDiagnostics          `json:"diagnostics"`
	Notes               []string                           `json:"notes"`
}

// OpsReliabilityTurnState reports native gateway capabilities and, when the
// optional collector is installed, its bounded aggregate health. It never
// exposes an opaque state value, session key, account identifier, or heuristic
// quality score.
type OpsReliabilityTurnState struct {
	Supported                       bool                              `json:"supported"`
	HTTPEnabled                     bool                              `json:"http_enabled"`
	WebSocketEnabled                bool                              `json:"websocket_enabled"`
	CrossAccountProtection          bool                              `json:"cross_account_protection"`
	HTTPCrossAccountProtection      bool                              `json:"http_cross_account_protection"`
	WebSocketCrossAccountProtection bool                              `json:"websocket_cross_account_protection"`
	Collector                       *OpsReliabilityTurnStateCollector `json:"collector,omitempty"`
}

type OpsReliabilityCooldown struct {
	Enabled             bool   `json:"enabled"`
	CooldownMinutes     int    `json:"cooldown_minutes,omitempty"`
	CooldownSeconds     int    `json:"cooldown_seconds,omitempty"`
	Action              string `json:"action,omitempty"`
	TempUnschedMinutes  int    `json:"temp_unsched_minutes,omitempty"`
	ThresholdCount      int    `json:"threshold_count,omitempty"`
	ThresholdWindowMins int    `json:"threshold_window_minutes,omitempty"`
}

type OpsReliabilityCooldowns struct {
	Overload529   OpsReliabilityCooldown `json:"overload_529"`
	RateLimit429  OpsReliabilityCooldown `json:"rate_limit_429"`
	StreamTimeout OpsReliabilityCooldown `json:"stream_timeout"`
}

// OpsReliabilityAccountAvailability contains totals rather than account
// records. The existing account-availability API remains the drill-down view;
// this projection avoids returning account names and error messages on a
// page whose purpose is aggregate diagnosis.
type OpsReliabilityAccountAvailability struct {
	TotalAccounts          int64                                         `json:"total_accounts"`
	AvailableCount         int64                                         `json:"available_count"`
	RateLimitCount         int64                                         `json:"rate_limit_count"`
	OverloadCount          int64                                         `json:"overload_count"`
	TempUnschedulableCount int64                                         `json:"temp_unschedulable_count"`
	ErrorCount             int64                                         `json:"error_count"`
	ByPlatform             map[string]OpsReliabilityPlatformAvailability `json:"by_platform"`
	CollectedAt            *time.Time                                    `json:"collected_at,omitempty"`
	// Short aliases keep compact cards compatible with the admin API contract.
	Total       int64 `json:"total"`
	Available   int64 `json:"available"`
	Limited     int64 `json:"limited"`
	CoolingDown int64 `json:"cooling_down"`
	Error       int64 `json:"error"`
}

type OpsReliabilityPlatformAvailability struct {
	TotalAccounts          int64 `json:"total_accounts"`
	AvailableCount         int64 `json:"available_count"`
	RateLimitCount         int64 `json:"rate_limit_count"`
	OverloadCount          int64 `json:"overload_count"`
	TempUnschedulableCount int64 `json:"temp_unschedulable_count"`
	ErrorCount             int64 `json:"error_count"`
}

type OpsReliabilityTraffic struct {
	Window             string    `json:"window"`
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	CurrentConcurrency *int64    `json:"current_concurrency,omitempty"`
	QueuedRequests     *int64    `json:"queued_requests,omitempty"`
	QPS                *float64  `json:"qps,omitempty"`
	TPS                *float64  `json:"tps,omitempty"`
	Requests           *int64    `json:"requests,omitempty"`
	ErrorRate          *float64  `json:"error_rate,omitempty"`
	Upstream429        *int64    `json:"upstream_429,omitempty"`
	Upstream529        *int64    `json:"upstream_529,omitempty"`
}

// OpsReliabilityConnection is a safe summary of the transport controls that
// affect connection establishment and stream lifetime. It intentionally does
// not include endpoints, proxy credentials, or account identifiers.
type OpsReliabilityConnection struct {
	Status     string `json:"status"`
	Reachable  *bool  `json:"reachable,omitempty"`
	Configured bool   `json:"configured"`
	// Canonical values are durations in seconds. These names are kept stable
	// for the reliability menu and are independent of Go implementation names.
	ResponseHeaderTimeout              int                               `json:"response_header_timeout"`
	OpenAIResponseHeaderTimeout        int                               `json:"openai_response_header_timeout"`
	FirstOutputTimeout                 int                               `json:"first_output_timeout"`
	StreamInterval                     int                               `json:"stream_interval"`
	MaxIdleConns                       int                               `json:"max_idle_conns"`
	MaxIdleConnsPerHost                int                               `json:"max_idle_conns_per_host"`
	MaxConnsPerHost                    int                               `json:"max_conns_per_host"`
	IdleConnTimeout                    int                               `json:"idle_conn_timeout"`
	DialTimeout                        int                               `json:"dial_timeout"`
	TLSHandshakeTimeout                int                               `json:"tls_handshake_timeout"`
	PoolIsolation                      string                            `json:"pool_isolation,omitempty"`
	IdleConnTimeoutSeconds             int                               `json:"idle_conn_timeout_seconds,omitempty"`
	MaxUpstreamClients                 int                               `json:"max_upstream_clients,omitempty"`
	ClientIdleTTLSeconds               int                               `json:"client_idle_ttl_seconds,omitempty"`
	ResponseHeaderTimeoutSeconds       int                               `json:"response_header_timeout_seconds,omitempty"`
	OpenAIResponseHeaderTimeoutSeconds int                               `json:"openai_response_header_timeout_seconds,omitempty"`
	OpenAIFirstOutputTimeoutSeconds    int                               `json:"openai_first_output_timeout_seconds,omitempty"`
	StreamDataIntervalTimeoutSeconds   int                               `json:"stream_data_interval_timeout_seconds,omitempty"`
	StreamKeepaliveIntervalSeconds     int                               `json:"stream_keepalive_interval_seconds,omitempty"`
	CodexIdentityEnforcement           bool                              `json:"codex_identity_enforcement"`
	OpenAIWS                           OpsReliabilityWebSocketConnection `json:"openai_ws"`
	OpenAIHTTP2                        OpsReliabilityHTTP2Connection     `json:"openai_http2"`
}

type OpsReliabilityWebSocketConnection struct {
	Enabled                        bool   `json:"enabled"`
	ForceHTTP                      bool   `json:"force_http"`
	OAuthEnabled                   bool   `json:"oauth_enabled"`
	APIKeyEnabled                  bool   `json:"apikey_enabled"`
	ModeRouterV2Enabled            bool   `json:"mode_router_v2_enabled"`
	IngressModeDefault             string `json:"ingress_mode_default,omitempty"`
	ClientFirstMessageTimeoutSec   int    `json:"client_first_message_timeout_seconds,omitempty"`
	IngressInterTurnIdleTimeoutSec int    `json:"ingress_inter_turn_idle_timeout_seconds,omitempty"`
	MaxIngressConnectionsPerAPIKey int    `json:"max_ingress_connections_per_api_key,omitempty"`
	DialTimeoutSeconds             int    `json:"dial_timeout_seconds,omitempty"`
	ReadTimeoutSeconds             int    `json:"read_timeout_seconds,omitempty"`
	WriteTimeoutSeconds            int    `json:"write_timeout_seconds,omitempty"`
	QueueLimitPerConnection        int    `json:"queue_limit_per_connection,omitempty"`
	FallbackCooldownSeconds        int    `json:"fallback_cooldown_seconds,omitempty"`
	RetryTotalBudgetMS             int    `json:"retry_total_budget_ms,omitempty"`
	HTTPBridgeEnabled              bool   `json:"http_bridge_enabled"`
}

type OpsReliabilityHTTP2Connection struct {
	Enabled                   bool `json:"enabled"`
	AllowProxyFallbackToHTTP1 bool `json:"allow_proxy_fallback_to_http1"`
	FallbackErrorThreshold    int  `json:"fallback_error_threshold,omitempty"`
	FallbackWindowSeconds     int  `json:"fallback_window_seconds,omitempty"`
	FallbackTTLSeconds        int  `json:"fallback_ttl_seconds,omitempty"`
}

type OpsReliabilityLimits struct {
	OverloadCooldownMinutes          int    `json:"overload_cooldown_minutes,omitempty"`
	OAuth401CooldownMinutes          int    `json:"oauth_401_cooldown_minutes,omitempty"`
	MaxAccountSwitches               int    `json:"max_account_switches,omitempty"`
	MaxAccountSwitchesGemini         int    `json:"max_account_switches_gemini,omitempty"`
	MaxIngressConnectionsPerAPIKey   int    `json:"max_ingress_connections_per_api_key,omitempty"`
	MaxConnsPerHost                  int    `json:"max_conns_per_host,omitempty"`
	MaxUpstreamClients               int    `json:"max_upstream_clients,omitempty"`
	ConnectionPoolIsolation          string `json:"connection_pool_isolation,omitempty"`
	StreamDataIntervalTimeoutSeconds int    `json:"stream_data_interval_timeout_seconds,omitempty"`
	StreamKeepaliveIntervalSeconds   int    `json:"stream_keepalive_interval_seconds,omitempty"`
	PanelEnabled                     bool   `json:"panel_enabled"`
	PanelUserRPM                     int    `json:"panel_user_rpm,omitempty"`
	PanelHeavyRPM                    int    `json:"panel_heavy_rpm,omitempty"`
	PanelPublicIPRPM                 int    `json:"panel_public_ip_rpm,omitempty"`
	PanelExemptAdmin                 bool   `json:"panel_exempt_admin"`
}

type OpsReliabilityRuntime struct {
	OpsEnabled          bool                               `json:"ops_enabled"`
	AccountAvailability *OpsReliabilityAccountAvailability `json:"account_availability,omitempty"`
	Traffic             OpsReliabilityTraffic              `json:"traffic"`
	Error               OpsReliabilityErrorSummary         `json:"error"`
}

type OpsReliabilityErrorSummary struct {
	Available        bool    `json:"available"`
	Total            int64   `json:"total"`
	RateLimited      int64   `json:"rate_limited"`
	Overloaded       int64   `json:"overloaded"`
	ConnectionFailed *int64  `json:"connection_failed,omitempty"`
	StreamTimeouts   *int64  `json:"stream_timeouts,omitempty"`
	ErrorRate        float64 `json:"error_rate"`
}

// OpsReliabilityDiagnostics deliberately contains no last-error strings.
// Those strings can contain provider response text or deployment details;
// operators can use the existing Ops error-log views for privileged drilldown.
type OpsReliabilityDiagnostics struct {
	OpsMonitoringEnabled      bool                            `json:"ops_monitoring_enabled"`
	RealtimeMonitoringEnabled bool                            `json:"realtime_monitoring_enabled"`
	RuntimeSettingsRefresh    OpsRuntimeSettingsRefreshHealth `json:"runtime_settings_refresh"`
	IngressRejections         OpsReliabilityIngressHealth     `json:"ingress_rejections"`
	AuthCacheInvalidation     OpsReliabilityAuthCacheHealth   `json:"auth_cache_invalidation"`
	SystemLogSink             OpsReliabilitySystemLogHealth   `json:"system_log_sink"`
	SourceErrors              []string                        `json:"source_errors"`
	Warnings                  []string                        `json:"warnings"`
}

type OpsReliabilityIngressHealth struct {
	Cardinality    int64  `json:"cardinality"`
	Capacity       int    `json:"capacity"`
	PendingBatches int    `json:"pending_batches"`
	PendingRows    int    `json:"pending_rows"`
	Overflowed     uint64 `json:"overflowed_count"`
	Dropped        uint64 `json:"dropped_count"`
	Flushed        uint64 `json:"flushed_request_count"`
	FlushFailures  uint64 `json:"flush_failure_count"`
	Accepting      bool   `json:"accepting"`
}

type OpsReliabilityAuthCacheHealth struct {
	OutboxRunning       bool   `json:"outbox_running"`
	OutboxProcessed     uint64 `json:"outbox_processed"`
	OutboxFailures      uint64 `json:"outbox_failures"`
	OutboxPending       int64  `json:"outbox_pending"`
	OutboxOldestLagSec  int64  `json:"outbox_oldest_lag_seconds"`
	SubscriberConnected bool   `json:"subscriber_connected"`
	SubscriberFailures  uint64 `json:"subscriber_failures"`
	LookupTotal         uint64 `json:"lookup_total"`
	LookupRejected      uint64 `json:"lookup_rejected"`
	LookupInFlight      int64  `json:"lookup_in_flight"`
	LookupCapacity      int    `json:"lookup_capacity"`
	InvalidAuthEnabled  bool   `json:"invalid_auth_enabled"`
	InvalidAuthBlocks   uint64 `json:"invalid_auth_blocks"`
}

type OpsReliabilitySystemLogHealth struct {
	QueueDepth      int64  `json:"queue_depth"`
	QueueCapacity   int64  `json:"queue_capacity"`
	DroppedCount    uint64 `json:"dropped_count"`
	WriteFailed     uint64 `json:"write_failed_count"`
	WrittenCount    uint64 `json:"written_count"`
	AvgWriteDelayMs uint64 `json:"avg_write_delay_ms"`
}

// GetReliabilityStatus returns the bounded status projection for the admin
// reliability menu. The method is intentionally best-effort: a missing Ops
// repository or account source is reported as a source error while the
// transport and limit configuration remains available to diagnose the issue.
func (s *OpsService) GetReliabilityStatus(ctx context.Context) (*OpsReliabilityStatus, error) {
	if s == nil {
		return nil, errors.New("ops service is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := time.Now().UTC()
	windowStart := now.Add(-reliabilityTrafficWindow)
	status := &OpsReliabilityStatus{
		Enabled:   s.IsMonitoringEnabled(ctx),
		Timestamp: now,
		Traffic: OpsReliabilityTraffic{
			Window: "5min", StartTime: windowStart, EndTime: now,
		},
		Connection:  reliabilityConnectionFromConfig(s.cfg),
		TurnState:   reliabilityTurnStateFromConfig(s.cfg),
		Limits:      reliabilityLimitsFromConfig(s.cfg),
		Diagnostics: OpsReliabilityDiagnostics{SourceErrors: []string{}, Warnings: []string{}},
		Notes: []string{
			"Account failover keeps the requested model; model replacement follows existing group routing policy and does not verify model quality.",
			"Bounded retry or failover can occur only under existing policy and retry budgets before downstream output is committed; a stream is never spliced to another attempt after output begins.",
			"Authentication, quota, and cooldown signals remain enforced and are not cleared by route or account switching.",
			"Response-header, connection-establishment, first-output, and stream-interval timeouts are separate controls.",
		},
	}
	status.TurnState.Collector = s.reliabilityTurnStateCollectorStatus(ctx)
	var settingsErrors []string
	status.Cooldowns, status.Limits, settingsErrors = s.reliabilitySettings(ctx)
	status.Diagnostics.SourceErrors = append(status.Diagnostics.SourceErrors, settingsErrors...)
	status.Diagnostics.OpsMonitoringEnabled = status.Enabled
	status.Diagnostics.RuntimeSettingsRefresh = s.RuntimeSettingsRefreshHealth()
	status.Diagnostics.IngressRejections = reliabilityIngressHealth(s.GetIngressRejectHealth())
	status.Diagnostics.SystemLogSink = reliabilitySystemLogHealth(s.GetSystemLogSinkHealth())
	status.Diagnostics.AuthCacheInvalidation = reliabilityAuthCacheHealth(s.GetAuthCacheInvalidationHealth(ctx))
	if !status.Enabled {
		status.Runtime.OpsEnabled = false
		status.Runtime.Traffic = status.Traffic
		status.Diagnostics.Warnings = append(status.Diagnostics.Warnings, "ops_monitoring_disabled")
		return status, nil
	}

	status.Diagnostics.RealtimeMonitoringEnabled = s.IsRealtimeMonitoringEnabled(ctx)
	availability, err := s.GetAccountAvailability(ctx, "", nil)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		status.Diagnostics.SourceErrors = appendUniqueReliabilityError(status.Diagnostics.SourceErrors, "account_availability_unavailable")
	} else {
		aggregated := aggregateReliabilityAvailability(availability, now)
		status.AccountAvailability = &aggregated
	}
	status.Runtime.OpsEnabled = status.Enabled
	status.Runtime.AccountAvailability = status.AccountAvailability

	if !status.Diagnostics.RealtimeMonitoringEnabled {
		status.Diagnostics.Warnings = append(status.Diagnostics.Warnings, "realtime_monitoring_disabled")
	} else {
		platformConcurrency, concurrencyErr := s.getReliabilityConcurrencyStats(ctx)
		if concurrencyErr != nil {
			if errors.Is(concurrencyErr, context.Canceled) || errors.Is(concurrencyErr, context.DeadlineExceeded) {
				return nil, concurrencyErr
			}
			status.Diagnostics.SourceErrors = appendUniqueReliabilityError(status.Diagnostics.SourceErrors, "concurrency_unavailable")
		} else {
			current, queued := aggregateReliabilityConcurrency(platformConcurrency)
			status.Traffic.CurrentConcurrency = &current
			status.Traffic.QueuedRequests = &queued
		}

		traffic, trafficErr := s.getReliabilityTrafficSummary(ctx, &OpsDashboardFilter{
			StartTime: windowStart,
			EndTime:   now,
			QueryMode: OpsQueryModeRaw,
		})
		if trafficErr != nil {
			if errors.Is(trafficErr, context.Canceled) || errors.Is(trafficErr, context.DeadlineExceeded) {
				return nil, trafficErr
			}
			status.Diagnostics.SourceErrors = appendUniqueReliabilityError(status.Diagnostics.SourceErrors, "traffic_unavailable")
		} else if traffic == nil {
			status.Diagnostics.SourceErrors = appendUniqueReliabilityError(status.Diagnostics.SourceErrors, "traffic_unavailable")
		} else {
			qps, tps := traffic.QPS.Current, traffic.TPS.Current
			status.Traffic.QPS, status.Traffic.TPS = &qps, &tps
		}

		overview, overviewErr := s.getReliabilityDashboardOverview(ctx, &OpsDashboardFilter{
			StartTime: windowStart,
			EndTime:   now,
			QueryMode: OpsQueryModeRaw,
		})
		if overviewErr != nil {
			if errors.Is(overviewErr, context.Canceled) || errors.Is(overviewErr, context.DeadlineExceeded) {
				return nil, overviewErr
			}
			status.Diagnostics.SourceErrors = appendUniqueReliabilityError(status.Diagnostics.SourceErrors, "error_summary_unavailable")
		} else if overview != nil {
			requests := overview.RequestCountTotal
			errorRate := overview.ErrorRate * 100
			upstream429, upstream529 := overview.Upstream429Count, overview.Upstream529Count
			status.Traffic.Requests = &requests
			status.Traffic.ErrorRate = &errorRate
			status.Traffic.Upstream429 = &upstream429
			status.Traffic.Upstream529 = &upstream529
			status.Runtime.Error = OpsReliabilityErrorSummary{
				Available: true, Total: overview.ErrorCountTotal, RateLimited: upstream429,
				Overloaded: upstream529, ErrorRate: errorRate,
			}
		}
	}
	status.Runtime.Traffic = status.Traffic

	if status.AccountAvailability != nil && status.AccountAvailability.TotalAccounts > 0 && status.AccountAvailability.AvailableCount == 0 {
		status.Diagnostics.Warnings = append(status.Diagnostics.Warnings, "no_schedulable_accounts")
	}
	if status.Diagnostics.RuntimeSettingsRefresh.FailureTotal > 0 {
		status.Diagnostics.Warnings = append(status.Diagnostics.Warnings, "runtime_settings_refresh_failures")
	}
	if status.Diagnostics.IngressRejections.FlushFailures > 0 {
		status.Diagnostics.Warnings = append(status.Diagnostics.Warnings, "ingress_reject_flush_failures")
	}
	if status.Diagnostics.AuthCacheInvalidation.OutboxFailures > 0 || status.Diagnostics.AuthCacheInvalidation.SubscriberFailures > 0 {
		status.Diagnostics.Warnings = append(status.Diagnostics.Warnings, "auth_cache_invalidation_failures")
	}
	if status.Diagnostics.SystemLogSink.WriteFailed > 0 {
		status.Diagnostics.Warnings = append(status.Diagnostics.Warnings, "system_log_sink_write_failures")
	}
	return status, nil
}

func (s *OpsService) getReliabilityConcurrencyStats(ctx context.Context) (map[string]*PlatformConcurrencyInfo, error) {
	if s.getReliabilityConcurrency != nil {
		return s.getReliabilityConcurrency(ctx)
	}
	platforms, _, _, _, err := s.GetConcurrencyStats(ctx, "", nil)
	return platforms, err
}

func (s *OpsService) getReliabilityTrafficSummary(ctx context.Context, filter *OpsDashboardFilter) (*OpsRealtimeTrafficSummary, error) {
	if s.getReliabilityTraffic != nil {
		return s.getReliabilityTraffic(ctx, filter)
	}
	return s.GetRealtimeTrafficSummary(ctx, filter)
}

func (s *OpsService) getReliabilityDashboardOverview(ctx context.Context, filter *OpsDashboardFilter) (*OpsDashboardOverview, error) {
	if s.getReliabilityOverview != nil {
		return s.getReliabilityOverview(ctx, filter)
	}
	return s.GetDashboardOverview(ctx, filter)
}

// reliabilitySettings reads the small set of database-backed controls shown by
// the reliability page. Errors intentionally fall back to the same defaults as
// the runtime setting services; the status endpoint remains useful during a
// transient settings-store failure and never returns raw setting values.
func (s *OpsService) reliabilitySettings(ctx context.Context) (OpsReliabilityCooldowns, OpsReliabilityLimits, []string) {
	overload := DefaultOverloadCooldownSettings()
	rate429 := DefaultRateLimit429CooldownSettings()
	stream := DefaultStreamTimeoutSettings()
	panel := DefaultPanelRateLimitSettings()
	limits := reliabilityLimitsFromConfig(s.cfg)
	sourceErrors := []string{}

	if s == nil || s.settingRepo == nil {
		sourceErrors = append(sourceErrors, "settings_unavailable")
		return reliabilityCooldowns(overload, rate429, stream), reliabilityPanelLimits(limits, panel), sourceErrors
	}

	keys := []string{
		SettingKeyOverloadCooldownSettings,
		SettingKeyRateLimit429CooldownSettings,
		SettingKeyStreamTimeoutSettings,
		SettingKeyPanelRateLimitSettings,
	}
	values, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		sourceErrors = append(sourceErrors, "settings_unavailable")
		return reliabilityCooldowns(overload, rate429, stream), reliabilityPanelLimits(limits, panel), sourceErrors
	}

	var invalid bool
	if overload, invalid = parseReliabilityOverload(values[SettingKeyOverloadCooldownSettings]); invalid {
		sourceErrors = append(sourceErrors, "overload_cooldown_invalid")
	}
	if rate429, invalid = parseReliabilityRate429(values[SettingKeyRateLimit429CooldownSettings]); invalid {
		sourceErrors = append(sourceErrors, "rate_limit_429_cooldown_invalid")
	}
	if stream, invalid = parseReliabilityStreamTimeout(values[SettingKeyStreamTimeoutSettings]); invalid {
		sourceErrors = append(sourceErrors, "stream_timeout_settings_invalid")
	}
	if panel, invalid = parseReliabilityPanelLimit(values[SettingKeyPanelRateLimitSettings]); invalid {
		sourceErrors = append(sourceErrors, "panel_rate_limit_settings_invalid")
	}
	return reliabilityCooldowns(overload, rate429, stream), reliabilityPanelLimits(limits, panel), sourceErrors
}

func parseReliabilityOverload(raw string) (*OverloadCooldownSettings, bool) {
	settings := DefaultOverloadCooldownSettings()
	if strings.TrimSpace(raw) == "" {
		return settings, false
	}
	var parsed OverloadCooldownSettings
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return settings, true
	}
	if parsed.CooldownMinutes < 1 {
		parsed.CooldownMinutes = 1
	} else if parsed.CooldownMinutes > 120 {
		parsed.CooldownMinutes = 120
	}
	return &parsed, false
}

func parseReliabilityRate429(raw string) (*RateLimit429CooldownSettings, bool) {
	settings := DefaultRateLimit429CooldownSettings()
	if strings.TrimSpace(raw) == "" {
		return settings, false
	}
	var parsed RateLimit429CooldownSettings
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return settings, true
	}
	if parsed.CooldownSeconds < 1 {
		parsed.CooldownSeconds = 1
	} else if parsed.CooldownSeconds > 7200 {
		parsed.CooldownSeconds = 7200
	}
	return &parsed, false
}

func parseReliabilityStreamTimeout(raw string) (*StreamTimeoutSettings, bool) {
	settings := DefaultStreamTimeoutSettings()
	if strings.TrimSpace(raw) == "" {
		return settings, false
	}
	var parsed StreamTimeoutSettings
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return settings, true
	}
	parsed.TempUnschedMinutes = clampReliabilityInt(parsed.TempUnschedMinutes, 1, 60)
	parsed.ThresholdCount = clampReliabilityInt(parsed.ThresholdCount, 1, 10)
	parsed.ThresholdWindowMinutes = clampReliabilityInt(parsed.ThresholdWindowMinutes, 1, 60)
	switch parsed.Action {
	case StreamTimeoutActionTempUnsched, StreamTimeoutActionError, StreamTimeoutActionNone:
	default:
		parsed.Action = StreamTimeoutActionTempUnsched
	}
	return &parsed, false
}

func parseReliabilityPanelLimit(raw string) (*PanelRateLimitSettings, bool) {
	settings := DefaultPanelRateLimitSettings()
	if strings.TrimSpace(raw) == "" {
		return settings, false
	}
	var parsed PanelRateLimitSettings
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return settings, true
	}
	normalizePanelRateLimitSettings(&parsed)
	return &parsed, false
}

func clampReliabilityInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func reliabilityCooldowns(overload *OverloadCooldownSettings, rate429 *RateLimit429CooldownSettings, stream *StreamTimeoutSettings) OpsReliabilityCooldowns {
	return OpsReliabilityCooldowns{
		Overload529:  OpsReliabilityCooldown{Enabled: overload.Enabled, CooldownMinutes: overload.CooldownMinutes},
		RateLimit429: OpsReliabilityCooldown{Enabled: rate429.Enabled, CooldownSeconds: rate429.CooldownSeconds},
		StreamTimeout: OpsReliabilityCooldown{
			Enabled: stream.Enabled, Action: stream.Action, TempUnschedMinutes: stream.TempUnschedMinutes,
			ThresholdCount: stream.ThresholdCount, ThresholdWindowMins: stream.ThresholdWindowMinutes,
		},
	}
}

func reliabilityPanelLimits(limits OpsReliabilityLimits, panel *PanelRateLimitSettings) OpsReliabilityLimits {
	limits.PanelEnabled, limits.PanelUserRPM, limits.PanelHeavyRPM = panel.Enabled, panel.UserRPM, panel.HeavyRPM
	limits.PanelPublicIPRPM, limits.PanelExemptAdmin = panel.PublicIPRPM, panel.ExemptAdmin
	return limits
}

func aggregateReliabilityAvailability(availability *OpsAccountAvailability, now time.Time) OpsReliabilityAccountAvailability {
	out := OpsReliabilityAccountAvailability{ByPlatform: map[string]OpsReliabilityPlatformAvailability{}}
	if availability == nil {
		return out
	}
	out.CollectedAt = availability.CollectedAt
	if out.CollectedAt == nil {
		out.CollectedAt = &now
	}
	for _, account := range availability.Accounts {
		if account == nil {
			continue
		}
		out.TotalAccounts++
		platform := account.Platform
		if platform == "" {
			platform = "unknown"
		}
		item := out.ByPlatform[platform]
		item.TotalAccounts++
		if account.IsAvailable {
			out.AvailableCount++
			item.AvailableCount++
		}
		isRateLimited := account.IsRateLimited
		isOverloaded := account.IsOverloaded
		isTempUnschedulable := account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil)
		if isRateLimited {
			out.RateLimitCount++
			item.RateLimitCount++
		}
		if isOverloaded {
			out.OverloadCount++
			item.OverloadCount++
		}
		if isTempUnschedulable {
			out.TempUnschedulableCount++
			item.TempUnschedulableCount++
		}
		if isRateLimited || isOverloaded {
			out.Limited++
		}
		if isRateLimited || isOverloaded || isTempUnschedulable {
			out.CoolingDown++
		}
		if account.HasError {
			out.ErrorCount++
			item.ErrorCount++
		}
		out.ByPlatform[platform] = item
	}
	out.Total = out.TotalAccounts
	out.Available = out.AvailableCount
	out.Error = out.ErrorCount
	return out
}

func aggregateReliabilityConcurrency(platforms map[string]*PlatformConcurrencyInfo) (current, queued int64) {
	for _, item := range platforms {
		if item == nil {
			continue
		}
		current += item.CurrentInUse
		queued += item.WaitingInQueue
	}
	return current, queued
}

func reliabilityConnectionFromConfig(cfg *config.Config) OpsReliabilityConnection {
	if cfg == nil {
		return OpsReliabilityConnection{Status: "unknown"}
	}
	g := cfg.Gateway
	ws := g.OpenAIWS
	h2 := g.OpenAIHTTP2
	return OpsReliabilityConnection{
		Status:                      "unknown",
		Configured:                  true,
		ResponseHeaderTimeout:       g.ResponseHeaderTimeout,
		OpenAIResponseHeaderTimeout: g.OpenAIResponseHeaderTimeout,
		FirstOutputTimeout:          g.OpenAIFirstOutputTimeoutSeconds,
		StreamInterval:              g.StreamDataIntervalTimeout,
		MaxIdleConns:                g.MaxIdleConns,
		MaxIdleConnsPerHost:         g.MaxIdleConnsPerHost,
		MaxConnsPerHost:             g.MaxConnsPerHost,
		IdleConnTimeout:             g.IdleConnTimeoutSeconds,
		// These are the gateway HTTP upstream transport defaults. They are
		// connection-establishment controls, not stream lifetime limits.
		DialTimeout:                        int(config.DefaultGatewayUpstreamDialTimeout / time.Second),
		TLSHandshakeTimeout:                int(config.DefaultGatewayUpstreamTLSHandshakeTimeout / time.Second),
		PoolIsolation:                      g.ConnectionPoolIsolation,
		IdleConnTimeoutSeconds:             g.IdleConnTimeoutSeconds,
		MaxUpstreamClients:                 g.MaxUpstreamClients,
		ClientIdleTTLSeconds:               g.ClientIdleTTLSeconds,
		ResponseHeaderTimeoutSeconds:       g.ResponseHeaderTimeout,
		OpenAIResponseHeaderTimeoutSeconds: g.OpenAIResponseHeaderTimeout,
		OpenAIFirstOutputTimeoutSeconds:    g.OpenAIFirstOutputTimeoutSeconds,
		StreamDataIntervalTimeoutSeconds:   g.StreamDataIntervalTimeout,
		StreamKeepaliveIntervalSeconds:     g.StreamKeepaliveInterval,
		CodexIdentityEnforcement:           !g.DisableCodexIdentityEnforcement && !g.DisableCodexOriginatorNormalization,
		OpenAIWS: OpsReliabilityWebSocketConnection{
			Enabled:                        ws.Enabled,
			ForceHTTP:                      ws.ForceHTTP,
			OAuthEnabled:                   ws.OAuthEnabled,
			APIKeyEnabled:                  ws.APIKeyEnabled,
			ModeRouterV2Enabled:            ws.ModeRouterV2Enabled,
			IngressModeDefault:             ws.IngressModeDefault,
			ClientFirstMessageTimeoutSec:   ws.ClientFirstMessageTimeoutSeconds,
			IngressInterTurnIdleTimeoutSec: ws.IngressInterTurnIdleTimeoutSeconds,
			MaxIngressConnectionsPerAPIKey: ws.MaxIngressConnectionsPerAPIKey,
			DialTimeoutSeconds:             ws.DialTimeoutSeconds,
			ReadTimeoutSeconds:             ws.ReadTimeoutSeconds,
			WriteTimeoutSeconds:            ws.WriteTimeoutSeconds,
			QueueLimitPerConnection:        ws.QueueLimitPerConn,
			FallbackCooldownSeconds:        ws.FallbackCooldownSeconds,
			RetryTotalBudgetMS:             ws.RetryTotalBudgetMS,
			HTTPBridgeEnabled:              ws.HTTPBridgeEnabled,
		},
		OpenAIHTTP2: OpsReliabilityHTTP2Connection{
			Enabled:                   h2.Enabled,
			AllowProxyFallbackToHTTP1: h2.AllowProxyFallbackToHTTP1,
			FallbackErrorThreshold:    h2.FallbackErrorThreshold,
			FallbackWindowSeconds:     h2.FallbackWindowSeconds,
			FallbackTTLSeconds:        h2.FallbackTTLSeconds,
		},
	}
}

func reliabilityTurnStateFromConfig(cfg *config.Config) OpsReliabilityTurnState {
	webSocketEnabled := false
	if cfg != nil {
		ws := cfg.Gateway.OpenAIWS
		webSocketEnabled = ws.Enabled && !ws.ForceHTTP && ws.OAuthEnabled
	}
	return OpsReliabilityTurnState{
		Supported:                       true,
		HTTPEnabled:                     true,
		WebSocketEnabled:                webSocketEnabled,
		CrossAccountProtection:          true,
		HTTPCrossAccountProtection:      true,
		WebSocketCrossAccountProtection: true,
	}
}

func reliabilityLimitsFromConfig(cfg *config.Config) OpsReliabilityLimits {
	if cfg == nil {
		return OpsReliabilityLimits{}
	}
	g := cfg.Gateway
	return OpsReliabilityLimits{
		OverloadCooldownMinutes:          cfg.RateLimit.OverloadCooldownMinutes,
		OAuth401CooldownMinutes:          cfg.RateLimit.OAuth401CooldownMinutes,
		MaxAccountSwitches:               g.MaxAccountSwitches,
		MaxAccountSwitchesGemini:         g.MaxAccountSwitchesGemini,
		MaxIngressConnectionsPerAPIKey:   g.OpenAIWS.MaxIngressConnectionsPerAPIKey,
		MaxConnsPerHost:                  g.MaxConnsPerHost,
		MaxUpstreamClients:               g.MaxUpstreamClients,
		ConnectionPoolIsolation:          g.ConnectionPoolIsolation,
		StreamDataIntervalTimeoutSeconds: g.StreamDataIntervalTimeout,
		StreamKeepaliveIntervalSeconds:   g.StreamKeepaliveInterval,
	}
}

func reliabilityIngressHealth(h OpsIngressRejectHealth) OpsReliabilityIngressHealth {
	return OpsReliabilityIngressHealth{
		Cardinality: h.Cardinality, Capacity: h.Capacity, PendingBatches: h.PendingBatches,
		PendingRows: h.PendingRows, Overflowed: h.Overflowed, Dropped: h.Dropped,
		Flushed: h.Flushed, FlushFailures: h.FlushFailures, Accepting: h.Accepting,
	}
}

func reliabilitySystemLogHealth(h OpsSystemLogSinkHealth) OpsReliabilitySystemLogHealth {
	return OpsReliabilitySystemLogHealth{
		QueueDepth: h.QueueDepth, QueueCapacity: h.QueueCapacity, DroppedCount: h.DroppedCount,
		WriteFailed: h.WriteFailed, WrittenCount: h.WrittenCount, AvgWriteDelayMs: h.AvgWriteDelayMs,
	}
}

func reliabilityAuthCacheHealth(h OpsAuthCacheInvalidationHealth) OpsReliabilityAuthCacheHealth {
	out := OpsReliabilityAuthCacheHealth{
		OutboxRunning: h.Outbox.Running, OutboxProcessed: h.Outbox.Processed, OutboxFailures: h.Outbox.Failures,
		OutboxPending: h.Outbox.Pending, OutboxOldestLagSec: int64(h.Outbox.OldestLag / time.Second),
		SubscriberConnected: h.Subscriber.Connected, SubscriberFailures: h.Subscriber.Failures,
		LookupTotal: h.Lookup.Total, LookupRejected: h.Lookup.Rejected, LookupInFlight: h.Lookup.InFlight,
		LookupCapacity: h.Lookup.Capacity, InvalidAuthEnabled: h.InvalidAbuse.Enabled, InvalidAuthBlocks: h.InvalidAbuse.Blocks,
	}
	return out
}

func appendUniqueReliabilityError(errorsList []string, value string) []string {
	for _, existing := range errorsList {
		if existing == value {
			return errorsList
		}
	}
	return append(errorsList, value)
}
