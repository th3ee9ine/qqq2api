package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"time"
)

const openAICodexTurnStateMaxHarvestNodes = 1024
const openAICodexTurnStateMaxProbeGates = 2048

type codexTurnStateHarvestNode struct {
	id                  string
	label               string
	successes           uint64
	failures            uint64
	consecutiveFailures uint64
	lastLatencyMS       int64
	lastResult          string
	lastAttempt         time.Time
	cooldownUntil       time.Time
}

type OpenAICodexTurnStateHarvestNodeSummary struct {
	NodeID                   string
	Label                    string
	Successes                uint64
	Failures                 uint64
	ConsecutiveFailures      uint64
	LastLatencyMS            int64
	LastResult               string
	CooldownRemainingSeconds int
}

type codexTurnStateProbeGateKey struct {
	accountID int64
	model     string
}

type codexTurnStateProbeGateState struct {
	generation    uint64
	inFlight      bool
	cooldownUntil time.Time
}

type codexTurnStateProbeLease struct {
	key        codexTurnStateProbeGateKey
	generation uint64
}

func codexTurnStateHarvestNodeID(proxyURL string) string {
	sum := sha256.Sum256([]byte(proxyURL))
	return hex.EncodeToString(sum[:8])
}

func codexTurnStateHarvestNodeLabel(proxyURL string) string {
	if proxyURL == "" {
		return "direct"
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || parsed.Hostname() == "" {
		return "proxy"
	}
	// Never include Userinfo, path, query, fragment, or the raw proxy URL.
	return parsed.Scheme + "://" + parsed.Host
}

func (s *OpenAIGatewayService) codexTurnStateHarvestSettings() (time.Duration, int, time.Duration) {
	defaults := defaultCodexTurnStateHarvestControls()
	preset := codexTurnStateHarvestPresets[defaults.SpeedPreset]
	if s != nil {
		if current := s.codexTurnStateRuntime.Load(); current != nil {
			return current.harvestRoundInterval, current.harvestRequestBudget, current.harvestFailureCooldown
		}
	}
	return time.Duration(preset.RoundIntervalSeconds) * time.Second, defaults.MaxRequestsPerRound,
		time.Duration(defaults.FailureCooldownSeconds) * time.Second
}

func (s *OpenAIGatewayService) codexTurnStateHarvestRouteAvailable(proxyURL string, now time.Time) bool {
	s.codexTurnStateProxyStatsMu.RLock()
	node := s.codexTurnStateHarvestNodes[codexTurnStateHarvestNodeID(proxyURL)]
	available := node == nil || !now.Before(node.cooldownUntil)
	s.codexTurnStateProxyStatsMu.RUnlock()
	return available
}

// beginCodexTurnStateProbe coordinates automatic collection for one account
// and concrete model. A lease covers the entire multi-route probe, not an
// individual egress attempt.
func (s *OpenAIGatewayService) beginCodexTurnStateProbe(accountID int64, model string, now time.Time) (codexTurnStateProbeLease, *openAICodexTurnStateProbeFailure) {
	if s == nil {
		return codexTurnStateProbeLease{}, &openAICodexTurnStateProbeFailure{code: "unavailable", err: errors.New("turn-state probe is unavailable")}
	}
	key := codexTurnStateProbeGateKey{accountID: accountID, model: codexTurnStateModelIdentity(model)}
	if key.model == "" {
		return codexTurnStateProbeLease{}, &openAICodexTurnStateProbeFailure{code: "invalid_model", err: errors.New("final upstream model is unavailable")}
	}
	s.codexTurnStateProxyStatsMu.Lock()
	defer s.codexTurnStateProxyStatsMu.Unlock()
	generation := s.codexTurnStateDiagnosticsGeneration()
	if s.codexTurnStateProbeGates == nil {
		s.codexTurnStateProbeGates = make(map[codexTurnStateProbeGateKey]codexTurnStateProbeGateState)
	}
	for candidate, state := range s.codexTurnStateProbeGates {
		if state.generation != generation || (!state.inFlight && !now.Before(state.cooldownUntil)) {
			delete(s.codexTurnStateProbeGates, candidate)
		}
	}
	if state, ok := s.codexTurnStateProbeGates[key]; ok {
		code := "cooldown"
		if state.inFlight {
			code = "probe_in_progress"
		}
		return codexTurnStateProbeLease{}, &openAICodexTurnStateProbeFailure{code: code, err: errors.New(code)}
	}
	if len(s.codexTurnStateProbeGates) >= openAICodexTurnStateMaxProbeGates {
		return codexTurnStateProbeLease{}, &openAICodexTurnStateProbeFailure{code: "capacity_full", err: errors.New("capacity_full")}
	}
	s.codexTurnStateProbeGates[key] = codexTurnStateProbeGateState{generation: generation, inFlight: true}
	return codexTurnStateProbeLease{key: key, generation: generation}, nil
}

func (s *OpenAIGatewayService) finishCodexTurnStateProbe(lease codexTurnStateProbeLease, failure *openAICodexTurnStateProbeFailure, now time.Time) {
	if s == nil {
		return
	}
	installCooldown := failure != nil && failure.dispatched && failure.code != "cancelled" && !errors.Is(failure.err, context.Canceled)

	s.codexTurnStateProxyStatsMu.Lock()
	defer s.codexTurnStateProxyStatsMu.Unlock()
	state, ok := s.codexTurnStateProbeGates[lease.key]
	if !ok || !state.inFlight || state.generation != lease.generation || lease.generation != s.codexTurnStateDiagnosticsGeneration() {
		return
	}
	_, _, cooldown := s.codexTurnStateHarvestSettings()
	if !installCooldown || cooldown <= 0 {
		delete(s.codexTurnStateProbeGates, lease.key)
		return
	}
	state.inFlight = false
	state.cooldownUntil = now.Add(cooldown)
	s.codexTurnStateProbeGates[lease.key] = state
}

// The shared budget is reserved only after credentials have been prepared and
// immediately before the outbound transport.
func (s *OpenAIGatewayService) reserveCodexTurnStateHarvestRequest(now time.Time) bool {
	s.codexTurnStateProxyStatsMu.Lock()
	defer s.codexTurnStateProxyStatsMu.Unlock()
	interval, limit, _ := s.codexTurnStateHarvestSettings()
	if interval <= 0 || limit <= 0 {
		return false
	}
	if s.codexTurnStateHarvestRoundStart.IsZero() || !now.Before(s.codexTurnStateHarvestRoundStart.Add(interval)) {
		s.codexTurnStateHarvestRoundStart = now
		s.codexTurnStateHarvestRoundUsed = 0
	}
	if s.codexTurnStateHarvestRoundUsed >= limit {
		return false
	}
	s.codexTurnStateHarvestRoundUsed++
	return true
}

func (s *OpenAIGatewayService) recordCodexTurnStateHarvestNode(proxyURL string, failure *openAICodexTurnStateProbeFailure, elapsed time.Duration, now time.Time, generation uint64) {
	if failure != nil && (failure.code == "cancelled" || errors.Is(failure.err, context.Canceled)) {
		return
	}
	s.codexTurnStateProxyStatsMu.Lock()
	defer s.codexTurnStateProxyStatsMu.Unlock()
	_, _, cooldown := s.codexTurnStateHarvestSettings()
	if generation != s.codexTurnStateDiagnosticsGeneration() {
		return
	}
	if s.codexTurnStateHarvestNodes == nil {
		s.codexTurnStateHarvestNodes = make(map[string]*codexTurnStateHarvestNode)
	}
	id := codexTurnStateHarvestNodeID(proxyURL)
	node := s.codexTurnStateHarvestNodes[id]
	if node == nil {
		if len(s.codexTurnStateHarvestNodes) >= openAICodexTurnStateMaxHarvestNodes {
			var oldest string
			for key, candidate := range s.codexTurnStateHarvestNodes {
				if oldest == "" || candidate.lastAttempt.Before(s.codexTurnStateHarvestNodes[oldest].lastAttempt) {
					oldest = key
				}
			}
			delete(s.codexTurnStateHarvestNodes, oldest)
		}
		node = &codexTurnStateHarvestNode{id: id, label: codexTurnStateHarvestNodeLabel(proxyURL)}
		s.codexTurnStateHarvestNodes[id] = node
	}
	node.lastAttempt = now
	node.lastLatencyMS = elapsed.Milliseconds()
	if node.lastLatencyMS < 0 {
		node.lastLatencyMS = 0
	}
	if failure == nil {
		node.successes++
		node.consecutiveFailures = 0
		node.cooldownUntil = time.Time{}
		node.lastResult = "success"
		return
	}
	node.failures++
	node.consecutiveFailures++
	// Account/model rejection and request cancellation say nothing about the
	// shared egress node's health; do not quarantine it for other accounts.
	switch failure.code {
	case "upstream_401", "upstream_403", "upstream_429", "upstream_rate_limited", "response_model_mismatch", "invalid_state", "response_failed":
	default:
		node.cooldownUntil = now.Add(cooldown)
	}
	node.lastResult = normalizeTurnStateCollectorErrorCode(failure.code)
	if node.lastResult == "" {
		node.lastResult = "other"
	}
}

func (s *OpenAIGatewayService) codexTurnStateHarvestSummary(now time.Time) ([]OpenAICodexTurnStateHarvestNodeSummary, int, int, *time.Time) {
	s.codexTurnStateProxyStatsMu.RLock()
	interval, limit, _ := s.codexTurnStateHarvestSettings()
	used := s.codexTurnStateHarvestRoundUsed
	reset := s.codexTurnStateHarvestRoundStart.Add(interval)
	if s.codexTurnStateHarvestRoundStart.IsZero() || !now.Before(reset) {
		used = 0
		reset = now.Add(interval)
	}
	nodes := make([]OpenAICodexTurnStateHarvestNodeSummary, 0, len(s.codexTurnStateHarvestNodes))
	for _, node := range s.codexTurnStateHarvestNodes {
		remaining := int(node.cooldownUntil.Sub(now).Seconds())
		nodes = append(nodes, OpenAICodexTurnStateHarvestNodeSummary{
			NodeID: node.id, Label: node.label, Successes: node.successes, Failures: node.failures,
			ConsecutiveFailures: node.consecutiveFailures, LastLatencyMS: node.lastLatencyMS,
			LastResult: node.lastResult, CooldownRemainingSeconds: max(0, remaining),
		})
	}
	s.codexTurnStateProxyStatsMu.RUnlock()
	return nodes, used, limit, &reset
}
