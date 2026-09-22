package service

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"
	"unicode"
)

// OpenAICodexTurnStateReliabilitySnapshot is the bounded, aggregate view of
// the turn-state collector used by the Ops reliability projection. It is
// deliberately different from a request/session snapshot: opaque state,
// account IDs, scope keys, routes, and proxy details must never cross this
// boundary.
//
// The gateway collector should return this value from a lock-protected,
// non-blocking status read. Timestamps are optional and are copied by the
// projection before being exposed to the administrator UI.
type OpenAICodexTurnStateReliabilitySnapshot struct {
	Enabled             bool
	ProbeEnabled        bool
	InjectionEnabled    bool
	Status              string
	Ready               bool
	Collecting          bool
	ActiveEntries       int
	ReadyCandidates     int
	Observations        uint64
	Successes           uint64
	Failures            uint64
	LastSuccessAt       *time.Time
	LastFailureAt       *time.Time
	LastErrorCode       string
	ProxyPool           []OpenAICodexTurnStateProxySummary
	SuccessfulIPRegions []OpenAICodexTurnStateIPRegion
	SuccessfulIPs       []OpenAICodexTurnStateSuccessfulIP
	CandidateBreakdown  []OpenAICodexTurnStateCandidateBreakdown
}

// OpenAICodexTurnStateReliabilityProvider is implemented by the OpenAI
// gateway when the collector is installed. Keeping this as a small optional
// interface lets older/test gateway instances continue serving reliability
// status without making the collector a hard dependency of OpsService.
type OpenAICodexTurnStateReliabilityProvider interface {
	CodexTurnStateReliabilitySnapshot(context.Context) OpenAICodexTurnStateReliabilitySnapshot
}

// OpsReliabilityTurnStateCollector is the safe JSON projection of the
// collector snapshot. It contains no opaque token or identity-bearing key.
type OpsReliabilityTurnStateCollector struct {
	Enabled             bool                                        `json:"enabled"`
	ProbeEnabled        bool                                        `json:"probe_enabled"`
	InjectionEnabled    bool                                        `json:"injection_enabled"`
	Status              string                                      `json:"status,omitempty"`
	Ready               bool                                        `json:"ready"`
	Collecting          bool                                        `json:"collecting"`
	ActiveEntries       int                                         `json:"active_entries"`
	ReadyCandidates     int                                         `json:"ready_candidates"`
	Observations        uint64                                      `json:"observations"`
	Successes           uint64                                      `json:"successes"`
	Failures            uint64                                      `json:"failures"`
	LastSuccessAt       *time.Time                                  `json:"last_success_at,omitempty"`
	LastFailureAt       *time.Time                                  `json:"last_failure_at,omitempty"`
	LastErrorCode       string                                      `json:"last_error_code,omitempty"`
	ProxyPool           []OpsReliabilityTurnStateProxy              `json:"proxy_pool,omitempty"`
	SuccessfulIPRegions []OpsReliabilityTurnStateIPRegion           `json:"successful_ip_regions,omitempty"`
	SuccessfulIPs       []OpsReliabilityTurnStateSuccessfulIP       `json:"successful_ips,omitempty"`
	CandidateBreakdown  []OpsReliabilityTurnStateCandidateBreakdown `json:"candidate_breakdown,omitempty"`
}

// These are deliberately separate from the gateway diagnostics structs: the
// Ops response is an allow-listed, bounded projection rather than a wire copy.
type OpsReliabilityTurnStateProxy struct {
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
}

type OpsReliabilityTurnStateIPRegion struct {
	Region      string `json:"region,omitempty"`
	Country     string `json:"country,omitempty"`
	CountryCode string `json:"country_code,omitempty"`
	Successes   uint64 `json:"successes"`
}

type OpsReliabilityTurnStateSuccessfulIP struct {
	IP            string     `json:"ip"`
	Region        string     `json:"region,omitempty"`
	Country       string     `json:"country,omitempty"`
	CountryCode   string     `json:"country_code,omitempty"`
	Successes     uint64     `json:"successes"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
}

type OpsReliabilityTurnStateCandidateBreakdown struct {
	Reason string `json:"reason"`
	Count  uint64 `json:"count"`
}

const (
	maxReliabilityTurnStateDiagnosticsEntries = 256
	maxReliabilityTurnStateCandidateReasons   = 32
)

var reliabilityTurnStateCandidateReasonAllowlist = map[string]struct{}{
	"account_unavailable":         {},
	"account_unschedulable":       {},
	"account_disabled":            {},
	"account_expired":             {},
	"account_identity_changed":    {},
	"capability_mismatch":         {},
	"channel_upstream_restricted": {},
	"active_healthy_skipped":      {},
	"already_ready":               {},
	"capacity_full":               {},
	"cooldown":                    {},
	"duplicate":                   {},
	"initial_missing":             {},
	"invalid_model":               {},
	"invalid_state":               {},
	"missing_response_state":      {},
	"model_mismatch":              {},
	"model_not_supported":         {},
	"group_mismatch":              {},
	"not_eligible":                {},
	"probe_disabled":              {},
	"probe_failed":                {},
	"probe_in_progress":           {},
	"probe_timeout":               {},
	"proxy_failed":                {},
	"proxy_stream_quarantined":    {},
	"proxy_timeout":               {},
	"proxy_unavailable":           {},
	"privacy_not_set":             {},
	"quota_auto_pause":            {},
	"refresh_due":                 {},
	"response_model_mismatch":     {},
	"transport_error":             {},
	"upstream_401":                {},
	"upstream_403":                {},
	"upstream_429":                {},
	"upstream_5xx":                {},
	"model_capacity":              {},
	"upstream_rate_limited":       {},
	"response_failed":             {},
	"incomplete_stream":           {},
	"cancelled":                   {},
	"unavailable":                 {},
	"runtime_blocked":             {},
	"scheduling_threshold":        {},
	"same_account_retry_mismatch": {},
	"shadow_parent_unhealthy":     {},
	"state_time_rejected":         {},
	"unreliable_key":              {},
	"unknown":                     {},
	"other":                       {},
}

// reliabilityTurnStateCollectorFromSnapshot maps an internal collector
// snapshot to a bounded status object. Status and error code are allow-listed
// because they can otherwise become an accidental channel for upstream error
// text or deployment details.
func reliabilityTurnStateCollectorFromSnapshot(snapshot OpenAICodexTurnStateReliabilitySnapshot) *OpsReliabilityTurnStateCollector {
	projected := &OpsReliabilityTurnStateCollector{
		Enabled:          snapshot.Enabled,
		ProbeEnabled:     snapshot.ProbeEnabled,
		InjectionEnabled: snapshot.InjectionEnabled,
		Status:           normalizeTurnStateCollectorStatus(snapshot.Status),
		Ready:            snapshot.Ready,
		Collecting:       snapshot.Collecting,
		ActiveEntries:    maxNonNegative(snapshot.ActiveEntries),
		ReadyCandidates:  maxNonNegative(snapshot.ReadyCandidates),
		Observations:     snapshot.Observations,
		Successes:        snapshot.Successes,
		Failures:         snapshot.Failures,
		LastSuccessAt:    copyReliabilityTime(snapshot.LastSuccessAt),
		LastFailureAt:    copyReliabilityTime(snapshot.LastFailureAt),
		LastErrorCode:    normalizeTurnStateCollectorErrorCode(snapshot.LastErrorCode),
	}
	projected.ProxyPool = projectReliabilityTurnStateProxyPool(snapshot.ProxyPool)
	projected.SuccessfulIPRegions = projectReliabilityTurnStateIPRegions(snapshot.SuccessfulIPRegions)
	projected.SuccessfulIPs = projectReliabilityTurnStateSuccessfulIPs(snapshot.SuccessfulIPs)
	projected.CandidateBreakdown = projectReliabilityTurnStateCandidateBreakdown(snapshot.CandidateBreakdown)
	return projected
}

func boundedReliabilityLabel(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxRunes <= 0 {
		return ""
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return ""
		}
	}
	runes := []rune(value)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	return string(runes)
}

func projectReliabilityTurnStateProxyPool(values []OpenAICodexTurnStateProxySummary) []OpsReliabilityTurnStateProxy {
	if len(values) == 0 {
		return nil
	}
	result := make([]OpsReliabilityTurnStateProxy, 0, reliabilityMinInt(len(values), maxReliabilityTurnStateDiagnosticsEntries))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		protocol := strings.ToLower(strings.TrimSpace(value.Protocol))
		if protocol != "http" && protocol != "https" && protocol != "socks5" {
			continue
		}
		host := boundedReliabilityLabel(value.Host, 255)
		if host == "" || strings.ContainsAny(host, "@/\\") || value.Port < 1 || value.Port > 65535 {
			continue
		}
		if parsed := net.ParseIP(host); parsed != nil {
			host = parsed.String()
		}
		key := protocol + "\x00" + host + "\x00" + formatReliabilityPort(value.Port)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, OpsReliabilityTurnStateProxy{Protocol: protocol, Host: host, Port: value.Port})
		if len(result) >= maxReliabilityTurnStateDiagnosticsEntries {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Protocol != result[j].Protocol {
			return result[i].Protocol < result[j].Protocol
		}
		if result[i].Host != result[j].Host {
			return result[i].Host < result[j].Host
		}
		return result[i].Port < result[j].Port
	})
	return result
}

func formatReliabilityPort(port int) string {
	// Avoid importing fmt just for a deduplication key.
	if port == 0 {
		return "0"
	}
	var digits [6]byte
	i := len(digits)
	for port > 0 {
		i--
		digits[i] = byte('0' + port%10)
		port /= 10
	}
	return string(digits[i:])
}

func projectReliabilityTurnStateIPRegions(values []OpenAICodexTurnStateIPRegion) []OpsReliabilityTurnStateIPRegion {
	if len(values) == 0 {
		return nil
	}
	result := make([]OpsReliabilityTurnStateIPRegion, 0, reliabilityMinInt(len(values), maxReliabilityTurnStateDiagnosticsEntries))
	for _, value := range values {
		region := boundedReliabilityLabel(value.Region, 128)
		country := boundedReliabilityLabel(value.Country, 128)
		countryCode := strings.ToUpper(boundedReliabilityLabel(value.CountryCode, 8))
		if region == "" && country == "" && countryCode == "" {
			continue
		}
		result = append(result, OpsReliabilityTurnStateIPRegion{Region: region, Country: country, CountryCode: countryCode, Successes: value.Successes})
		if len(result) >= maxReliabilityTurnStateDiagnosticsEntries {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Successes != result[j].Successes {
			return result[i].Successes > result[j].Successes
		}
		return result[i].Region < result[j].Region
	})
	return result
}

func projectReliabilityTurnStateSuccessfulIPs(values []OpenAICodexTurnStateSuccessfulIP) []OpsReliabilityTurnStateSuccessfulIP {
	if len(values) == 0 {
		return nil
	}
	result := make([]OpsReliabilityTurnStateSuccessfulIP, 0, reliabilityMinInt(len(values), maxReliabilityTurnStateDiagnosticsEntries))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		parsed := net.ParseIP(strings.TrimSpace(value.IP))
		if parsed == nil {
			continue
		}
		ip := parsed.String()
		if _, exists := seen[ip]; exists {
			continue
		}
		seen[ip] = struct{}{}
		result = append(result, OpsReliabilityTurnStateSuccessfulIP{
			IP:            ip,
			Region:        boundedReliabilityLabel(value.Region, 128),
			Country:       boundedReliabilityLabel(value.Country, 128),
			CountryCode:   strings.ToUpper(boundedReliabilityLabel(value.CountryCode, 8)),
			Successes:     value.Successes,
			LastSuccessAt: copyReliabilityValueTime(value.LastSuccessAt),
		})
		if len(result) >= maxReliabilityTurnStateDiagnosticsEntries {
			break
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Successes != result[j].Successes {
			return result[i].Successes > result[j].Successes
		}
		return result[i].IP < result[j].IP
	})
	return result
}

func projectReliabilityTurnStateCandidateBreakdown(values []OpenAICodexTurnStateCandidateBreakdown) []OpsReliabilityTurnStateCandidateBreakdown {
	if len(values) == 0 {
		return nil
	}
	counts := make(map[string]uint64, reliabilityMinInt(len(values), maxReliabilityTurnStateCandidateReasons))
	for _, value := range values {
		reason := strings.ToLower(strings.TrimSpace(value.Reason))
		if _, allowed := reliabilityTurnStateCandidateReasonAllowlist[reason]; !allowed {
			// Preserve the count without exposing arbitrary upstream/error text.
			reason = "other"
		}
		if len(counts) >= maxReliabilityTurnStateCandidateReasons {
			if _, exists := counts[reason]; !exists {
				continue
			}
		}
		counts[reason] += value.Count
	}
	result := make([]OpsReliabilityTurnStateCandidateBreakdown, 0, len(counts))
	for reason, count := range counts {
		result = append(result, OpsReliabilityTurnStateCandidateBreakdown{Reason: reason, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count != result[j].Count {
			return result[i].Count > result[j].Count
		}
		return result[i].Reason < result[j].Reason
	})
	return result
}

func reliabilityMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func copyReliabilityValueTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func maxNonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func copyReliabilityTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func normalizeTurnStateCollectorStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "disabled", "unavailable", "idle", "collecting", "warming", "ready", "stale", "cooldown", "degraded", "error":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		if strings.TrimSpace(value) == "" {
			return "unknown"
		}
		return "unknown"
	}
}

func normalizeTurnStateCollectorErrorCode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "probe_timeout", "transport_error", "upstream_401", "upstream_403", "upstream_429", "upstream_5xx",
		"model_capacity", "upstream_rate_limited", "response_failed", "response_model_mismatch",
		"invalid_state", "invalid_model", "incomplete_stream", "state_time_rejected", "cooldown", "cancelled", "disabled", "unavailable", "other":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

// reliabilityTurnStateCollectorStatus reads the optional collector without
// making the reliability endpoint depend on its implementation. The provider
// method is expected to be a fast snapshot read; request cancellation is
// still honored before invoking it.
func (s *OpsService) reliabilityTurnStateCollectorStatus(ctx context.Context) *OpsReliabilityTurnStateCollector {
	if s == nil || s.openAIGatewayService == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	provider, ok := any(s.openAIGatewayService).(OpenAICodexTurnStateReliabilityProvider)
	if !ok {
		return nil
	}
	return reliabilityTurnStateCollectorFromSnapshot(provider.CodexTurnStateReliabilitySnapshot(ctx))
}
