package service

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	// Diagnostics are an operational view, not a historical event log. Keep
	// enough entries for a useful pool overview while bounding memory when the
	// configured pool or upstream egress addresses rotate frequently.
	openAICodexTurnStateDiagnosticsMaxEntries = 1024
	openAICodexTurnStateDiagnosticsTTL        = 24 * time.Hour
	openAICodexTurnStateProxyProbeWorkers     = 8
	openAICodexTurnStateProxyProbeCooldown    = 30 * time.Second
)

// OpenAICodexTurnStateSuccessfulIP aggregates egress observations triggered by
// successful state probes. Successes is the legacy field name for observation
// count, not the number of state probes: diagnostics are sampled, and a rotating
// proxy can assign the separate diagnostic connection a different exit IP.
// It contains only public exit metadata, never proxy credentials.
type OpenAICodexTurnStateSuccessfulIP struct {
	IP            string
	Region        string
	Country       string
	CountryCode   string
	Successes     uint64
	LastSuccessAt time.Time
}

type OpenAICodexTurnStateIPRegion struct {
	Region        string
	Country       string
	CountryCode   string
	Successes     uint64 // Legacy name for sampled egress observation count.
	lastSuccessAt time.Time
}

type OpenAICodexTurnStateCandidateBreakdown struct {
	Reason string
	Count  uint64
}

func (s *OpenAIGatewayService) SetProxyExitInfoProber(prober ProxyExitInfoProber) {
	if s != nil {
		s.proxyProber = prober
	}
}

func (s *OpenAIGatewayService) recordCodexTurnStateCandidateReason(reason string) {
	if s == nil {
		return
	}
	reason = strings.TrimSpace(strings.ToLower(reason))
	if reason == "" {
		return
	}
	s.codexTurnStateProxyStatsMu.Lock()
	if s.codexTurnStateCandidateReasons == nil {
		s.codexTurnStateCandidateReasons = make(map[string]uint64)
	}
	if len(s.codexTurnStateCandidateReasons) < 32 || s.codexTurnStateCandidateReasons[reason] > 0 {
		s.codexTurnStateCandidateReasons[reason]++
	}
	s.codexTurnStateProxyStatsMu.Unlock()
}

// recordCodexTurnStateProxySuccess performs the optional exit-IP lookup after
// a probe has already passed the authoritative SSE/state checks. A failed
// geolocation lookup never invalidates the state; it only leaves diagnostics
// incomplete. The lookup is bounded so a telemetry provider cannot hold the
// request indefinitely.
func (s *OpenAIGatewayService) recordCodexTurnStateProxySuccess(ctx context.Context, proxyURL string) {
	if s == nil {
		return
	}
	s.recordCodexTurnStateProxyObservation(ctx, proxyURL, s.codexTurnStateDiagnosticsGeneration())
}

func (s *OpenAIGatewayService) codexTurnStateDiagnosticsGeneration() uint64 {
	if settings := s.codexTurnStateRuntime.Load(); settings != nil {
		return settings.proxyPoolGeneration
	}
	return 0
}

func (s *OpenAIGatewayService) recordCodexTurnStateProxyObservation(ctx context.Context, proxyURL string, generation uint64) {
	if s == nil || s.proxyProber == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	info, _, err := s.proxyProber.ProbeProxy(probeCtx, proxyURL)
	if err != nil || info == nil || strings.TrimSpace(info.IP) == "" {
		return
	}
	now := time.Now().UTC()
	ip := strings.TrimSpace(info.IP)
	region := strings.TrimSpace(info.Region)
	country := strings.TrimSpace(info.Country)
	countryCode := strings.ToUpper(strings.TrimSpace(info.CountryCode))
	regionKey := strings.Join([]string{countryCode, country, region}, "\x00")
	s.codexTurnStateProxyStatsMu.Lock()
	defer s.codexTurnStateProxyStatsMu.Unlock()
	if generation != s.codexTurnStateDiagnosticsGeneration() {
		return
	}
	s.pruneCodexTurnStateDiagnosticsLocked(now)
	if s.codexTurnStateSuccessfulIPs == nil {
		s.codexTurnStateSuccessfulIPs = make(map[string]*OpenAICodexTurnStateSuccessfulIP)
	}
	if s.codexTurnStateIPRegions == nil {
		s.codexTurnStateIPRegions = make(map[string]*OpenAICodexTurnStateIPRegion)
	}
	entry := s.codexTurnStateSuccessfulIPs[ip]
	if entry == nil {
		evictOldestCodexTurnStateIPLocked(s.codexTurnStateSuccessfulIPs)
		entry = &OpenAICodexTurnStateSuccessfulIP{IP: ip}
		s.codexTurnStateSuccessfulIPs[ip] = entry
	}
	entry.Region, entry.Country, entry.CountryCode = region, country, countryCode
	entry.Successes++
	entry.LastSuccessAt = now
	regionEntry := s.codexTurnStateIPRegions[regionKey]
	if regionEntry == nil {
		evictOldestCodexTurnStateRegionLocked(s.codexTurnStateIPRegions)
		regionEntry = &OpenAICodexTurnStateIPRegion{Region: region, Country: country, CountryCode: countryCode}
		s.codexTurnStateIPRegions[regionKey] = regionEntry
	}
	regionEntry.Successes++
	regionEntry.lastSuccessAt = now
}

// recordCodexTurnStateProxySuccessAsync keeps exit-IP telemetry off the probe
// critical path. The authoritative state response has already been validated;
// a slow geo endpoint must not consume the caller's probe timeout or delay the
// upstream request. The worker has its own bounded context and stores only the
// sanitized result from ProxyExitInfoProber.
func (s *OpenAIGatewayService) recordCodexTurnStateProxySuccessAsync(proxyURL string, generation uint64) {
	if s == nil || s.proxyProber == nil {
		return
	}
	proxyURL = strings.TrimSpace(proxyURL)
	now := time.Now()
	s.codexTurnStateProxyStatsMu.Lock()
	if generation != s.codexTurnStateDiagnosticsGeneration() {
		s.codexTurnStateProxyStatsMu.Unlock()
		return
	}
	if s.codexTurnStateProxyProbeSem == nil {
		s.codexTurnStateProxyProbeSem = make(chan struct{}, openAICodexTurnStateProxyProbeWorkers)
	}
	if s.codexTurnStateProxyProbeLast == nil {
		s.codexTurnStateProxyProbeLast = make(map[string]time.Time)
	}
	for key, last := range s.codexTurnStateProxyProbeLast {
		if now.Sub(last) >= openAICodexTurnStateProxyProbeCooldown*2 {
			delete(s.codexTurnStateProxyProbeLast, key)
		}
	}
	if len(s.codexTurnStateProxyProbeLast) >= openAICodexTurnStateDiagnosticsMaxEntries {
		oldestKey := ""
		var oldest time.Time
		found := false
		for key, last := range s.codexTurnStateProxyProbeLast {
			if !found || last.Before(oldest) {
				oldestKey, oldest = key, last
				found = true
			}
		}
		if found {
			delete(s.codexTurnStateProxyProbeLast, oldestKey)
		}
	}
	if last, ok := s.codexTurnStateProxyProbeLast[proxyURL]; ok && now.Sub(last) < openAICodexTurnStateProxyProbeCooldown {
		s.codexTurnStateProxyStatsMu.Unlock()
		return
	}
	select {
	case s.codexTurnStateProxyProbeSem <- struct{}{}:
		s.codexTurnStateProxyProbeLast[proxyURL] = now
	default:
		// Telemetry must never create an unbounded goroutine backlog. A later
		// successful probe will retry the best-effort lookup.
		s.codexTurnStateProxyStatsMu.Unlock()
		return
	}
	s.codexTurnStateProxyStatsMu.Unlock()
	go func() {
		defer func() { <-s.codexTurnStateProxyProbeSem }()
		s.recordCodexTurnStateProxyObservation(context.Background(), proxyURL, generation)
	}()
}

func (s *OpenAIGatewayService) pruneCodexTurnStateDiagnosticsLocked(now time.Time) {
	cutoff := now.Add(-openAICodexTurnStateDiagnosticsTTL)
	for ip, entry := range s.codexTurnStateSuccessfulIPs {
		if entry == nil || entry.LastSuccessAt.Before(cutoff) {
			delete(s.codexTurnStateSuccessfulIPs, ip)
		}
	}
	for key, entry := range s.codexTurnStateIPRegions {
		if entry == nil || (!entry.lastSuccessAt.IsZero() && entry.lastSuccessAt.Before(cutoff)) {
			delete(s.codexTurnStateIPRegions, key)
		}
	}
}

func evictOldestCodexTurnStateIPLocked(entries map[string]*OpenAICodexTurnStateSuccessfulIP) {
	if len(entries) < openAICodexTurnStateDiagnosticsMaxEntries {
		return
	}
	oldestKey := ""
	var oldest time.Time
	for key, entry := range entries {
		if entry == nil {
			oldestKey = key
			break
		}
		if oldestKey == "" || entry.LastSuccessAt.Before(oldest) {
			oldestKey, oldest = key, entry.LastSuccessAt
		}
	}
	if oldestKey != "" {
		delete(entries, oldestKey)
	}
}

func evictOldestCodexTurnStateRegionLocked(entries map[string]*OpenAICodexTurnStateIPRegion) {
	if len(entries) < openAICodexTurnStateDiagnosticsMaxEntries {
		return
	}
	// Region entries have no per-entry timestamp, so evict a deterministic
	// key. The aggregate is informational and will be recreated on a future
	// successful probe.
	oldestKey := ""
	for key := range entries {
		if oldestKey == "" || key < oldestKey {
			oldestKey = key
		}
	}
	if oldestKey != "" {
		delete(entries, oldestKey)
	}
}

func (s *OpenAIGatewayService) codexTurnStateDiagnostics() (ips []OpenAICodexTurnStateSuccessfulIP, regions []OpenAICodexTurnStateIPRegion, candidates []OpenAICodexTurnStateCandidateBreakdown) {
	if s == nil {
		return nil, nil, nil
	}
	s.codexTurnStateProxyStatsMu.Lock()
	defer s.codexTurnStateProxyStatsMu.Unlock()
	s.pruneCodexTurnStateDiagnosticsLocked(time.Now().UTC())
	for _, entry := range s.codexTurnStateSuccessfulIPs {
		if entry != nil {
			ips = append(ips, *entry)
		}
	}
	for _, entry := range s.codexTurnStateIPRegions {
		if entry != nil {
			regions = append(regions, *entry)
		}
	}
	for reason, count := range s.codexTurnStateCandidateReasons {
		candidates = append(candidates, OpenAICodexTurnStateCandidateBreakdown{Reason: reason, Count: count})
	}
	sort.Slice(ips, func(i, j int) bool {
		if ips[i].Successes != ips[j].Successes {
			return ips[i].Successes > ips[j].Successes
		}
		return ips[i].IP < ips[j].IP
	})
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].Successes != regions[j].Successes {
			return regions[i].Successes > regions[j].Successes
		}
		return regions[i].Region < regions[j].Region
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Count != candidates[j].Count {
			return candidates[i].Count > candidates[j].Count
		}
		return candidates[i].Reason < candidates[j].Reason
	})
	if len(ips) > openAICodexTurnStateProxyPoolMaxEntries {
		ips = ips[:openAICodexTurnStateProxyPoolMaxEntries]
	}
	if len(regions) > openAICodexTurnStateProxyPoolMaxEntries {
		regions = regions[:openAICodexTurnStateProxyPoolMaxEntries]
	}
	if len(candidates) > 32 {
		candidates = candidates[:32]
	}
	return ips, regions, candidates
}
