package service

// This file contains the in-process turn-state collector used by the gateway.
// Turn state is an opaque, upstream-issued value: the envelope checks below
// only reject values that are malformed or outside the configured lifetime.
// They do not decrypt, verify, or otherwise claim that a value is authentic.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	openAICodexTurnStateCollectorDefaultTTL = time.Hour
	// A healthy state is reusable for 55 minutes of its one-hour local TTL.
	openAICodexTurnStateCollectorDefaultRefreshBefore  = 5 * time.Minute
	openAICodexTurnStateCollectorDefaultClockSkew      = 30 * time.Second
	openAICodexTurnStateCollectorDefaultProbeCooldown  = 3 * time.Minute
	openAICodexTurnStateCollectorDefaultMaxEntries     = 2048
	openAICodexTurnStateCollectorDefaultMaxTokenLength = 2048
	openAICodexTurnStateCollectorDefaultMaxProbeSlots  = 1
	openAICodexTurnStateCookieTTL                      = 240 * time.Second
	openAICodexTurnStateMaxCookies                     = 64
	openAICodexTurnStateCollectorMaxScopeLength        = 512
	openAICodexTurnStateCollectorMaxModelLength        = 128
	openAICodexTurnStateCollectorMaxRouteLength        = 256
	openAICodexTurnStateCollectorMinIssuedUnix         = 1577836800 // 2020-01-01
	openAICodexTurnStateCollectorMaxIssuedUnix         = 4102444800 // 2100-01-01
)

// OpenAICodexTurnStateHeader is the public spelling used by probe and
// transport integrations. The existing relay code keeps its lower-case
// internal alias for backwards compatibility.
const OpenAICodexTurnStateHeader = openAICodexTurnStateHeader

var (
	// The errors are deliberately stable and do not include the opaque state.
	ErrOpenAICodexTurnStateEmpty           = errors.New("codex turn state is empty")
	ErrOpenAICodexTurnStateEncoding        = errors.New("codex turn state has invalid encoding")
	ErrOpenAICodexTurnStateEnvelope        = errors.New("codex turn state envelope is not recognized")
	ErrOpenAICodexTurnStateTimestamp       = errors.New("codex turn state timestamp is outside the supported range")
	ErrOpenAICodexTurnStateExpired         = errors.New("codex turn state is expired or not yet valid")
	ErrOpenAICodexTurnStateShape           = errors.New("codex turn state shape does not match the configured policy")
	ErrOpenAICodexTurnStateCollectorFull   = errors.New("codex turn state collector capacity is full")
	ErrOpenAICodexTurnStateProbeInProgress = errors.New("codex turn state probe is already in progress")
)

// OpenAICodexTurnStatePolicy controls opaque state admission. Blocks is the
// expected encrypted-block count; zero disables that one heuristic. TTL and
// RefreshBefore are local admission windows, not upstream guarantees.
type OpenAICodexTurnStatePolicy struct {
	Blocks        int
	TTL           time.Duration
	RefreshBefore time.Duration
	ClockSkew     time.Duration
	ProbeCooldown time.Duration
	MaxEntries    int
	MaxTokenBytes int
	MaxProbeSlots int
	HoldActive    bool
}

func (p OpenAICodexTurnStatePolicy) normalized() OpenAICodexTurnStatePolicy {
	if p.TTL <= 0 {
		p.TTL = openAICodexTurnStateCollectorDefaultTTL
	}
	if p.ClockSkew <= 0 {
		p.ClockSkew = openAICodexTurnStateCollectorDefaultClockSkew
	}
	if p.RefreshBefore < 0 {
		p.RefreshBefore = 0
	}
	if p.RefreshBefore >= p.TTL {
		p.RefreshBefore = p.TTL / 2
	}
	if p.ProbeCooldown < 0 {
		p.ProbeCooldown = openAICodexTurnStateCollectorDefaultProbeCooldown
	}
	if p.MaxEntries <= 0 {
		p.MaxEntries = openAICodexTurnStateCollectorDefaultMaxEntries
	}
	if p.MaxTokenBytes <= 0 {
		p.MaxTokenBytes = openAICodexTurnStateCollectorDefaultMaxTokenLength
	}
	if p.MaxProbeSlots <= 0 {
		p.MaxProbeSlots = openAICodexTurnStateCollectorDefaultMaxProbeSlots
	}
	// Do not let an accidental configuration turn this bounded cache into an
	// unbounded allocation. The limit is intentionally generous for account,
	// model, and execution-scope combinations.
	if p.MaxEntries > 65536 {
		p.MaxEntries = 65536
	}
	if p.MaxTokenBytes > 8192 {
		p.MaxTokenBytes = 8192
	}
	if p.MaxProbeSlots > 64 {
		p.MaxProbeSlots = 64
	}
	return p
}

// OpenAICodexTurnStateToken is a parsed opaque state value. Value is needed by
// the injection path; status/metrics APIs never return it.
type OpenAICodexTurnStateToken struct {
	Value       string
	Fingerprint string
	IssuedAt    time.Time
	Blocks      int
}

// ParseOpenAICodexTurnState validates the wire envelope without attempting to
// interpret the encrypted payload. Leading/trailing HTTP header whitespace is
// ignored, while embedded whitespace and controls are rejected.
func ParseOpenAICodexTurnState(value string) (OpenAICodexTurnStateToken, error) {
	return parseOpenAICodexTurnState(value, openAICodexTurnStateCollectorDefaultMaxTokenLength)
}

// ValidateOpenAICodexTurnState parses a wire value and applies the supplied
// local policy in one step. It returns no token on failure, so callers cannot
// accidentally inject a value that failed the shape or lifetime checks.
func ValidateOpenAICodexTurnState(value string, policy OpenAICodexTurnStatePolicy, now time.Time) (OpenAICodexTurnStateToken, error) {
	policy = policy.normalized()
	token, err := parseOpenAICodexTurnState(value, policy.MaxTokenBytes)
	if err != nil {
		return OpenAICodexTurnStateToken{}, err
	}
	if policy.Blocks > 0 && token.Blocks != policy.Blocks {
		return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateShape
	}
	if !policy.Accept(token, now) {
		return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateExpired
	}
	return token, nil
}

func parseOpenAICodexTurnState(value string, maxBytes int) (OpenAICodexTurnStateToken, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateEmpty
	}
	if maxBytes <= 0 {
		maxBytes = openAICodexTurnStateCollectorDefaultMaxTokenLength
	}
	if len(value) > maxBytes || strings.ContainsAny(value, " \t\r\n") {
		return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateEncoding
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateEncoding
		}
	}
	core := strings.TrimRight(value, "=")
	if len(value)-len(core) > 2 {
		return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateEncoding
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(core)
	if err != nil || len(raw) < 73 || (len(raw)-57)%16 != 0 || raw[0] != 0x80 {
		return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateEnvelope
	}
	issuedUnix := binary.BigEndian.Uint64(raw[1:9])
	if issuedUnix < openAICodexTurnStateCollectorMinIssuedUnix || issuedUnix >= openAICodexTurnStateCollectorMaxIssuedUnix {
		return OpenAICodexTurnStateToken{}, ErrOpenAICodexTurnStateTimestamp
	}
	digest := sha256.Sum256([]byte(value))
	return OpenAICodexTurnStateToken{
		Value:       value,
		Fingerprint: hex.EncodeToString(digest[:8]),
		IssuedAt:    time.Unix(int64(issuedUnix), 0),
		Blocks:      (len(raw) - 57) / 16,
	}, nil
}

// Accept applies the local policy to a parsed token.
func (p OpenAICodexTurnStatePolicy) Accept(token OpenAICodexTurnStateToken, now time.Time) bool {
	p = p.normalized()
	if token.Value == "" || token.IssuedAt.IsZero() || token.Blocks < 1 {
		return false
	}
	if p.Blocks > 0 && token.Blocks != p.Blocks {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}
	if token.IssuedAt.After(now.Add(p.ClockSkew)) {
		return false
	}
	return now.Before(token.IssuedAt.Add(p.TTL).Add(-p.ClockSkew))
}

// OpenAICodexTurnStateKey isolates state by account, downstream execution
// scope, and model. Scope is intentionally caller-provided and should be
// derived before account-specific request rewriting.
type OpenAICodexTurnStateKey struct {
	AccountID int64
	Scope     string
	Model     string
	// generation is intentionally private: it is a collector lease, not part of
	// the externally visible account/scope/model identity. Gateway-created keys
	// are stamped through BindKey so responses from a pre-invalidation request
	// cannot repopulate state after credentials or connection identity changes.
	generation uint64
}

func (k OpenAICodexTurnStateKey) canonical() OpenAICodexTurnStateKey {
	k.Scope = strings.TrimSpace(k.Scope)
	k.Model = strings.ToLower(strings.TrimSpace(k.Model))
	if len(k.Scope) > openAICodexTurnStateCollectorMaxScopeLength {
		digest := sha256.Sum256([]byte(k.Scope))
		k.Scope = "sha256:" + hex.EncodeToString(digest[:])
	}
	if len(k.Model) > openAICodexTurnStateCollectorMaxModelLength {
		digest := sha256.Sum256([]byte(k.Model))
		k.Model = "sha256:" + hex.EncodeToString(digest[:])
	}
	return k
}

// OpenAICodexTurnStateSnapshot is immutable request state. A caller can keep
// it across a failover attempt; later observations cannot mutate its version.
type OpenAICodexTurnStateSnapshot struct {
	Token            OpenAICodexTurnStateToken
	Route            string
	Version          uint64
	EgressProxyURL   string
	EgressPinned     bool
	HarvestSessionID string
	HarvestCookies   []OpenAICodexTurnStateCookie
	HarvestCookiesAt time.Time
}

// OpenAICodexTurnStateCookie is the minimum cookie material needed for a
// pinned ticket request. Attributes from Set-Cookie are intentionally not
// retained, and status/admin projections expose counts only.
type OpenAICodexTurnStateCookie struct {
	Name  string
	Value string
}

func cloneOpenAICodexTurnStateSnapshot(snapshot OpenAICodexTurnStateSnapshot) OpenAICodexTurnStateSnapshot {
	snapshot.HarvestCookies = append([]OpenAICodexTurnStateCookie(nil), snapshot.HarvestCookies...)
	return snapshot
}

func (s OpenAICodexTurnStateSnapshot) cookiesFresh(now time.Time) bool {
	now = collectorNow(now)
	return len(s.HarvestCookies) > 0 && !s.HarvestCookiesAt.IsZero() &&
		now.Before(s.HarvestCookiesAt.Add(openAICodexTurnStateCookieTTL))
}

func (s OpenAICodexTurnStateSnapshot) usable(policy OpenAICodexTurnStatePolicy, now time.Time) bool {
	return policy.Accept(s.Token, now)
}

// OpenAICodexTurnStateStatus intentionally contains metadata only. In
// particular, it never exposes Token.Value.
type OpenAICodexTurnStateStatus struct {
	Key                    OpenAICodexTurnStateKey `json:"key"`
	Usable                 bool                    `json:"usable"`
	Ready                  bool                    `json:"ready"`
	Version                uint64                  `json:"version"`
	Strikes                int                     `json:"strikes"`
	Candidates             uint64                  `json:"candidates"`
	Observations           uint64                  `json:"observations"`
	ActiveFingerprint      string                  `json:"active_fingerprint,omitempty"`
	ReadyFingerprint       string                  `json:"ready_fingerprint,omitempty"`
	ActiveIssuedAt         time.Time               `json:"active_issued_at,omitempty"`
	ReadyIssuedAt          time.Time               `json:"ready_issued_at,omitempty"`
	RemainingSeconds       int                     `json:"remaining_seconds"`
	CookieCount            int                     `json:"cookie_count"`
	CookieRemainingSeconds int                     `json:"cookie_remaining_seconds"`
	CookieExpired          bool                    `json:"cookie_expired"`
	LastProbeAt            time.Time               `json:"last_probe_at,omitempty"`
	NextProbeAt            time.Time               `json:"next_probe_at,omitempty"`
	ProbeInFlight          bool                    `json:"probe_in_flight"`
}

// OpenAICodexTurnStateMetrics is a bounded aggregate; it contains no key or
// token values and can safely be exported from a health endpoint.
type OpenAICodexTurnStateMetrics struct {
	Entries          int    `json:"entries"`
	Offers           uint64 `json:"offers"`
	AcceptedOffers   uint64 `json:"accepted_offers"`
	RejectedOffers   uint64 `json:"rejected_offers"`
	Observations     uint64 `json:"observations"`
	SuspectResponses uint64 `json:"suspect_responses"`
	Promotions       uint64 `json:"promotions"`
	Evictions        uint64 `json:"evictions"`
	ProbeStarted     uint64 `json:"probe_started"`
	ProbeCoalesced   uint64 `json:"probe_coalesced"`
	ProbeFinished    uint64 `json:"probe_finished"`
}

type openAICodexTurnStateCollectorEntry struct {
	key        OpenAICodexTurnStateKey
	active     OpenAICodexTurnStateSnapshot
	ready      OpenAICodexTurnStateSnapshot
	version    uint64
	strikes    int
	candidates uint64
	observed   uint64
	lastAccess time.Time
	lastProbe  time.Time
	nextProbe  time.Time
	probeDone  chan struct{}
	// probeEpoch fences a response that was already in flight when this key
	// was invalidated. probeInvalidated remains set until the lease is released
	// so a stale probe cannot recreate the entry through Offer.
	probeEpoch       uint64
	probeInvalidated bool
	// forceRefresh bypasses sibling-health throttling after an exact-key
	// invalidation (for example a response model mismatch).
	forceRefresh bool
}

type openAICodexTurnStateAccountGeneration struct {
	value      uint64
	lastAccess time.Time
}

// OpenAICodexTurnStateProbe is an opaque single-flight lease. Call FinishProbe
// or AbortProbe exactly once when the bounded probe attempt ends.
type OpenAICodexTurnStateProbe struct {
	Key       OpenAICodexTurnStateKey
	StartedAt time.Time
	done      chan struct{}
	epoch     uint64
}

// OpenAICodexTurnStateProbeStartResult distinguishes a real single-flight
// conflict from a healthy state that appeared before the probe lease could be
// acquired. Callers can reuse the latter immediately without waiting or
// spending an upstream probe.
type OpenAICodexTurnStateProbeStartResult uint8

const (
	OpenAICodexTurnStateProbeStartUnavailable OpenAICodexTurnStateProbeStartResult = iota
	OpenAICodexTurnStateProbeStarted
	OpenAICodexTurnStateProbeSkippedHealthy
	OpenAICodexTurnStateProbeInFlight
	OpenAICodexTurnStateProbeCooldown
	OpenAICodexTurnStateProbeCapacity
	OpenAICodexTurnStateProbeSkippedAccountModelHealthy
)

// OpenAICodexTurnStateOfferResult describes an automatic collection offer.
// Automatic offers are admitted only when the key has no usable active state
// or its refresh window has opened. The decision and publication happen under
// one collector lock so concurrent responses cannot leave a standby backlog.
type OpenAICodexTurnStateOfferResult uint8

const (
	OpenAICodexTurnStateOfferRejected OpenAICodexTurnStateOfferResult = iota
	OpenAICodexTurnStateOfferPublished
	OpenAICodexTurnStateOfferSkippedHealthy
)

type openAICodexTurnStateCollectorCounters struct {
	offers, acceptedOffers, rejectedOffers atomic.Uint64
	observations, suspectResponses         atomic.Uint64
	promotions, evictions                  atomic.Uint64
	probeStarted, probeCoalesced           atomic.Uint64
	probeFinished                          atomic.Uint64
}

// OpenAICodexTurnStateCollector stores active and standby values in memory.
// The map is bounded and all snapshots are copied by value before callers use
// them, so a response cannot mutate a request already in flight.
type OpenAICodexTurnStateCollector struct {
	mu                 sync.Mutex
	policy             OpenAICodexTurnStatePolicy
	entries            map[OpenAICodexTurnStateKey]*openAICodexTurnStateCollectorEntry
	accountGenerations map[int64]openAICodexTurnStateAccountGeneration
	generationSequence uint64
	counts             openAICodexTurnStateCollectorCounters
	probes             int
}

func NewOpenAICodexTurnStateCollector(policy OpenAICodexTurnStatePolicy) *OpenAICodexTurnStateCollector {
	return &OpenAICodexTurnStateCollector{
		policy:             policy.normalized(),
		entries:            make(map[OpenAICodexTurnStateKey]*openAICodexTurnStateCollectorEntry),
		accountGenerations: make(map[int64]openAICodexTurnStateAccountGeneration),
	}
}

// BindKey stamps a gateway request with the account's current lifecycle
// generation. The stamped key must be retained for the whole request, including
// response observation and probe publication.
func (c *OpenAICodexTurnStateCollector) BindKey(key OpenAICodexTurnStateKey) OpenAICodexTurnStateKey {
	key = key.canonical()
	if c == nil {
		return key
	}
	c.mu.Lock()
	key, _ = c.resolveKeyGenerationLocked(key, time.Now(), true)
	c.mu.Unlock()
	return key
}

// IsCurrentKey reports whether a request-bound key still belongs to the
// account's current lifecycle generation.
func (c *OpenAICodexTurnStateCollector) IsCurrentKey(key OpenAICodexTurnStateKey) bool {
	if c == nil {
		return false
	}
	key = key.canonical()
	c.mu.Lock()
	current, ok := c.accountGenerations[key.AccountID]
	if ok {
		current.lastAccess = time.Now()
		c.accountGenerations[key.AccountID] = current
	}
	c.mu.Unlock()
	return ok && key.generation != 0 && key.generation == current.value
}

func (c *OpenAICodexTurnStateCollector) nextGenerationLocked() uint64 {
	c.generationSequence++
	if c.generationSequence == 0 {
		c.generationSequence = 1
	}
	return c.generationSequence
}

func (c *OpenAICodexTurnStateCollector) removeAccountLocked(accountID int64, countEvictions bool) {
	delete(c.accountGenerations, accountID)
	for key, entry := range c.entries {
		if key.AccountID != accountID {
			continue
		}
		if entry.probeDone != nil {
			close(entry.probeDone)
			entry.probeDone = nil
			if c.probes > 0 {
				c.probes--
			}
			c.counts.probeFinished.Add(1)
		}
		delete(c.entries, key)
		if countEvictions {
			c.counts.evictions.Add(1)
		}
	}
}

func (c *OpenAICodexTurnStateCollector) admitAccountGenerationLocked(accountID int64, now time.Time) openAICodexTurnStateAccountGeneration {
	if current, ok := c.accountGenerations[accountID]; ok {
		current.lastAccess = now
		c.accountGenerations[accountID] = current
		return current
	}
	if len(c.accountGenerations) >= c.policy.MaxEntries {
		var oldestID int64
		var oldest time.Time
		found := false
		for candidateID, candidate := range c.accountGenerations {
			if !found || candidate.lastAccess.Before(oldest) {
				oldestID, oldest, found = candidateID, candidate.lastAccess, true
			}
		}
		if found {
			c.removeAccountLocked(oldestID, true)
		}
	}
	generation := openAICodexTurnStateAccountGeneration{value: c.nextGenerationLocked(), lastAccess: now}
	c.accountGenerations[accountID] = generation
	return generation
}

func (c *OpenAICodexTurnStateCollector) resolveKeyGenerationLocked(key OpenAICodexTurnStateKey, now time.Time, create bool) (OpenAICodexTurnStateKey, bool) {
	key = key.canonical()
	current, ok := c.accountGenerations[key.AccountID]
	if key.generation != 0 {
		if !ok || key.generation != current.value {
			return key, false
		}
		current.lastAccess = now
		c.accountGenerations[key.AccountID] = current
		return key, true
	}
	// Zero-generation keys preserve the standalone collector API. Gateway
	// requests always use BindKey and retain a non-zero generation that can be
	// rejected after lifecycle invalidation.
	if !ok {
		if !create {
			return key, false
		}
		current = c.admitAccountGenerationLocked(key.AccountID, now)
	} else {
		current.lastAccess = now
		c.accountGenerations[key.AccountID] = current
	}
	key.generation = current.value
	return key, true
}

func collectorNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now()
	}
	return now
}

func (c *OpenAICodexTurnStateCollector) entryLocked(key OpenAICodexTurnStateKey, now time.Time, create bool) (*openAICodexTurnStateCollectorEntry, bool) {
	if c == nil {
		return nil, false
	}
	if now.IsZero() {
		now = time.Now()
	}
	var ok bool
	key, ok = c.resolveKeyGenerationLocked(key, now, create)
	if !ok {
		return nil, false
	}
	if entry := c.entries[key]; entry != nil {
		entry.lastAccess = now
		return entry, true
	}
	if !create {
		return nil, false
	}
	c.sweepLocked(now, 32)
	if len(c.entries) >= c.policy.MaxEntries {
		var oldestKey OpenAICodexTurnStateKey
		var oldest time.Time
		found := false
		for candidateKey, candidate := range c.entries {
			if candidate.probeDone != nil {
				continue
			}
			if !found || candidate.lastAccess.Before(oldest) {
				oldestKey, oldest, found = candidateKey, candidate.lastAccess, true
			}
		}
		if !found {
			return nil, false
		}
		delete(c.entries, oldestKey)
		c.counts.evictions.Add(1)
	}
	entry := &openAICodexTurnStateCollectorEntry{key: key, lastAccess: now}
	c.entries[key] = entry
	return entry, true
}

func (c *OpenAICodexTurnStateCollector) sweepLocked(now time.Time, limit int) {
	if limit <= 0 {
		return
	}
	removed := 0
	for key, entry := range c.entries {
		if removed >= limit {
			break
		}
		if entry.probeDone == nil && !entry.lastAccess.IsZero() && now.Sub(entry.lastAccess) > c.policy.TTL {
			delete(c.entries, key)
			c.counts.evictions.Add(1)
			removed++
		}
	}
}

func (c *OpenAICodexTurnStateCollector) promoteLocked(entry *openAICodexTurnStateCollectorEntry, now time.Time) {
	if entry == nil || entry.ready.Token.Value == "" || !c.policy.Accept(entry.ready.Token, now) {
		if entry != nil && entry.ready.Token.Value != "" && !c.policy.Accept(entry.ready.Token, now) {
			entry.ready = OpenAICodexTurnStateSnapshot{}
		}
		return
	}
	if entry.active.Token.Fingerprint == entry.ready.Token.Fingerprint {
		entry.ready = OpenAICodexTurnStateSnapshot{}
		return
	}
	activeUsable := c.policy.Accept(entry.active.Token, now)
	refreshDue := false
	if activeUsable && c.policy.RefreshBefore > 0 {
		refreshDue = now.Add(c.policy.RefreshBefore).After(entry.active.Token.IssuedAt.Add(c.policy.TTL))
	}
	if !activeUsable || entry.strikes >= 2 || (!c.policy.HoldActive && refreshDue && entry.ready.Token.IssuedAt.After(entry.active.Token.IssuedAt)) {
		entry.version++
		entry.ready.Version = entry.version
		entry.active = entry.ready
		entry.ready = OpenAICodexTurnStateSnapshot{}
		entry.strikes = 0
		entry.forceRefresh = false
		c.counts.promotions.Add(1)
	}
}

// Acquire returns an immutable active snapshot. A missing or expired value is
// represented by usable=false; callers may then run a bounded probe.
func (c *OpenAICodexTurnStateCollector) Acquire(key OpenAICodexTurnStateKey, now time.Time) (OpenAICodexTurnStateSnapshot, bool) {
	if c == nil {
		return OpenAICodexTurnStateSnapshot{}, false
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok {
		return OpenAICodexTurnStateSnapshot{}, false
	}
	c.promoteLocked(entry, now)
	snapshot := cloneOpenAICodexTurnStateSnapshot(entry.active)
	return snapshot, snapshot.usable(c.policy, now)
}

// OfferSnapshot publishes a complete server-owned ticket; the ordinary Offer
// API remains token-only for compatibility with existing collector users.
func (c *OpenAICodexTurnStateCollector) OfferSnapshot(key OpenAICodexTurnStateKey, snapshot OpenAICodexTurnStateSnapshot, now time.Time) bool {
	if c == nil {
		return false
	}
	now = collectorNow(now)
	c.counts.offers.Add(1)
	token, ok := c.normalizeOfferToken(snapshot.Token, now)
	if !ok {
		return false
	}
	snapshot.Token = token
	snapshot = normalizeOpenAICodexTurnStateSnapshot(snapshot)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offerSnapshotLocked(key, snapshot, now, nil)
}

// Offer publishes a validated candidate. A healthy active value is never
// overwritten immediately; newer candidates wait in ready until refresh,
// repeated response evidence, expiry, or an explicit rejection promotes them.
func (c *OpenAICodexTurnStateCollector) Offer(key OpenAICodexTurnStateKey, token OpenAICodexTurnStateToken, route string, now time.Time) bool {
	if c == nil {
		return false
	}
	now = collectorNow(now)
	c.counts.offers.Add(1)
	var ok bool
	if token, ok = c.normalizeOfferToken(token, now); !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offerLocked(key, token, route, now, nil)
}

// OfferProbe publishes a candidate only while the exact probe lease that
// produced it is still current. This closes the invalidate-vs-probe race: a
// model-mismatch response can retire a key while its older probe is reading,
// and that probe must not recreate the entry after the invalidation.
func (c *OpenAICodexTurnStateCollector) OfferProbe(probe OpenAICodexTurnStateProbe, token OpenAICodexTurnStateToken, route string, now time.Time) bool {
	if c == nil || probe.done == nil {
		return false
	}
	now = collectorNow(now)
	c.counts.offers.Add(1)
	var ok bool
	if token, ok = c.normalizeOfferToken(token, now); !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offerLocked(probe.Key, token, route, now, &probe)
}

// OfferIfRefreshNeeded atomically checks the current active state and publishes
// token only when collection is due for this account/model. A healthy sibling
// scope suppresses collection without sharing its state; explicit invalidation
// bypasses that gate. Unlike Offer, this never creates a standby candidate
// behind a healthy active state. Gateway response collection uses this method.
func (c *OpenAICodexTurnStateCollector) OfferIfRefreshNeeded(key OpenAICodexTurnStateKey, token OpenAICodexTurnStateToken, route string, now time.Time) OpenAICodexTurnStateOfferResult {
	if c == nil {
		return OpenAICodexTurnStateOfferRejected
	}
	now = collectorNow(now)
	var ok bool
	if token, ok = c.normalizeOfferToken(token, now); !ok {
		c.counts.offers.Add(1)
		return OpenAICodexTurnStateOfferRejected
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offerIfRefreshNeededLocked(key, token, route, now, nil)
}

// OfferProbeIfRefreshNeeded applies the same atomic refresh gate while also
// requiring the exact probe lease to remain current. Lease validation happens
// before the healthy-state shortcut so a fenced probe is rejected, not reported
// as a harmless concurrent skip.
func (c *OpenAICodexTurnStateCollector) OfferProbeIfRefreshNeeded(probe OpenAICodexTurnStateProbe, token OpenAICodexTurnStateToken, route string, now time.Time) OpenAICodexTurnStateOfferResult {
	if c == nil || probe.done == nil {
		return OpenAICodexTurnStateOfferRejected
	}
	now = collectorNow(now)
	var ok bool
	if token, ok = c.normalizeOfferToken(token, now); !ok {
		c.counts.offers.Add(1)
		return OpenAICodexTurnStateOfferRejected
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offerIfRefreshNeededLocked(probe.Key, token, route, now, &probe)
}

// OfferProbeSnapshotIfRefreshNeeded atomically publishes the token together
// with the exact egress/session/cookie identity that minted it.
func (c *OpenAICodexTurnStateCollector) OfferProbeSnapshotIfRefreshNeeded(probe OpenAICodexTurnStateProbe, snapshot OpenAICodexTurnStateSnapshot, now time.Time) OpenAICodexTurnStateOfferResult {
	if c == nil || probe.done == nil {
		return OpenAICodexTurnStateOfferRejected
	}
	now = collectorNow(now)
	token, ok := c.normalizeOfferToken(snapshot.Token, now)
	if !ok {
		c.counts.offers.Add(1)
		return OpenAICodexTurnStateOfferRejected
	}
	snapshot.Token = token
	snapshot = normalizeOpenAICodexTurnStateSnapshot(snapshot)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.offerSnapshotIfRefreshNeededLocked(probe.Key, snapshot, now, &probe)
}

// ProbeCurrent reports whether a probe lease is still allowed to publish.
// Callers use this to distinguish a fenced lease from an ordinary probe
// validation failure, where a still-healthy active snapshot may be retained.
func (c *OpenAICodexTurnStateCollector) ProbeCurrent(probe OpenAICodexTurnStateProbe) bool {
	if c == nil || probe.done == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.resolveKeyGenerationLocked(probe.Key, time.Now(), false)
	if !ok {
		return false
	}
	entry := c.entries[key]
	return entry != nil && entry.probeDone == probe.done &&
		entry.probeEpoch == probe.epoch && !entry.probeInvalidated
}

func (c *OpenAICodexTurnStateCollector) normalizeOfferToken(token OpenAICodexTurnStateToken, now time.Time) (OpenAICodexTurnStateToken, bool) {
	if token.Value == "" || token.Fingerprint == "" {
		c.counts.rejectedOffers.Add(1)
		return OpenAICodexTurnStateToken{}, false
	}
	parsed, err := parseOpenAICodexTurnState(token.Value, c.policy.MaxTokenBytes)
	if err != nil || !c.policy.Accept(parsed, now) {
		c.counts.rejectedOffers.Add(1)
		return OpenAICodexTurnStateToken{}, false
	}
	return parsed, true
}

func (c *OpenAICodexTurnStateCollector) offerLocked(key OpenAICodexTurnStateKey, token OpenAICodexTurnStateToken, route string, now time.Time, expectedProbe *OpenAICodexTurnStateProbe) bool {
	return c.offerSnapshotLocked(key, OpenAICodexTurnStateSnapshot{Token: token, Route: route}, now, expectedProbe)
}

func (c *OpenAICodexTurnStateCollector) offerSnapshotLocked(key OpenAICodexTurnStateKey, snapshot OpenAICodexTurnStateSnapshot, now time.Time, expectedProbe *OpenAICodexTurnStateProbe) bool {
	create := expectedProbe == nil
	entry, ok := c.entryLocked(key, now, create)
	if !ok || (expectedProbe != nil &&
		(entry.probeDone != expectedProbe.done || entry.probeEpoch != expectedProbe.epoch || entry.probeInvalidated)) {
		c.counts.rejectedOffers.Add(1)
		return false
	}
	c.offerSnapshotEntryLocked(entry, snapshot, now)
	return true
}

func (c *OpenAICodexTurnStateCollector) offerIfRefreshNeededLocked(key OpenAICodexTurnStateKey, token OpenAICodexTurnStateToken, route string, now time.Time, expectedProbe *OpenAICodexTurnStateProbe) OpenAICodexTurnStateOfferResult {
	return c.offerSnapshotIfRefreshNeededLocked(key, OpenAICodexTurnStateSnapshot{Token: token, Route: route}, now, expectedProbe)
}

func (c *OpenAICodexTurnStateCollector) offerSnapshotIfRefreshNeededLocked(key OpenAICodexTurnStateKey, snapshot OpenAICodexTurnStateSnapshot, now time.Time, expectedProbe *OpenAICodexTurnStateProbe) OpenAICodexTurnStateOfferResult {
	create := expectedProbe == nil
	canonical, currentGeneration := c.resolveKeyGenerationLocked(key, now, create)
	if !currentGeneration {
		c.counts.offers.Add(1)
		c.counts.rejectedOffers.Add(1)
		return OpenAICodexTurnStateOfferRejected
	}
	entry := c.entries[canonical]
	if entry == nil && create {
		// Check before allocating the per-scope entry so high-cardinality
		// execution scopes cannot evict the healthy account/model sibling.
		if c.hasHealthyAccountModelSiblingLocked(canonical, now) {
			return OpenAICodexTurnStateOfferSkippedHealthy
		}
		var ok bool
		entry, ok = c.entryLocked(canonical, now, true)
		if !ok {
			c.counts.offers.Add(1)
			c.counts.rejectedOffers.Add(1)
			return OpenAICodexTurnStateOfferRejected
		}
	} else if entry != nil {
		entry.lastAccess = now
	}
	if entry == nil || (expectedProbe != nil &&
		(entry.probeDone != expectedProbe.done || entry.probeEpoch != expectedProbe.epoch || entry.probeInvalidated)) {
		c.counts.offers.Add(1)
		c.counts.rejectedOffers.Add(1)
		return OpenAICodexTurnStateOfferRejected
	}
	c.promoteLocked(entry, now)
	readyRefreshQueued := c.policy.Accept(entry.ready.Token, now) &&
		entry.ready.Token.IssuedAt.After(entry.active.Token.IssuedAt)
	if !c.needsRefreshLocked(entry, now) || readyRefreshQueued {
		return OpenAICodexTurnStateOfferSkippedHealthy
	}
	// A response or probe may have allocated this exact scope before another
	// scope published a healthy account/model state. Re-check after validating
	// the probe lease and exact entry, while preserving explicit invalidation's
	// force-refresh bypass.
	if !entry.forceRefresh && c.hasHealthyAccountModelSiblingLocked(entry.key, now) {
		return OpenAICodexTurnStateOfferSkippedHealthy
	}
	c.counts.offers.Add(1)
	c.offerSnapshotEntryLocked(entry, snapshot, now)
	return OpenAICodexTurnStateOfferPublished
}

func (c *OpenAICodexTurnStateCollector) offerEntryLocked(entry *openAICodexTurnStateCollectorEntry, token OpenAICodexTurnStateToken, route string, now time.Time) {
	c.offerSnapshotEntryLocked(entry, OpenAICodexTurnStateSnapshot{Token: token, Route: route}, now)
}

func normalizeOpenAICodexTurnStateSnapshot(snapshot OpenAICodexTurnStateSnapshot) OpenAICodexTurnStateSnapshot {
	snapshot.Route = strings.TrimSpace(snapshot.Route)
	snapshot.EgressProxyURL = strings.TrimSpace(snapshot.EgressProxyURL)
	snapshot.HarvestSessionID = strings.TrimSpace(snapshot.HarvestSessionID)
	if len(snapshot.Route) > openAICodexTurnStateCollectorMaxRouteLength {
		snapshot.Route = snapshot.Route[:openAICodexTurnStateCollectorMaxRouteLength]
	}
	if len(snapshot.EgressProxyURL) > 4096 {
		snapshot.EgressProxyURL = ""
		snapshot.EgressPinned = false
	}
	if len(snapshot.HarvestSessionID) > 256 {
		snapshot.HarvestSessionID = ""
	}
	if len(snapshot.HarvestCookies) > openAICodexTurnStateMaxCookies {
		snapshot.HarvestCookies = snapshot.HarvestCookies[:openAICodexTurnStateMaxCookies]
	}
	snapshot.HarvestCookies = append([]OpenAICodexTurnStateCookie(nil), snapshot.HarvestCookies...)
	return snapshot
}

func (c *OpenAICodexTurnStateCollector) offerSnapshotEntryLocked(entry *openAICodexTurnStateCollectorEntry, snapshot OpenAICodexTurnStateSnapshot, now time.Time) {
	snapshot = normalizeOpenAICodexTurnStateSnapshot(snapshot)
	token, route := snapshot.Token, snapshot.Route
	if len(route) > openAICodexTurnStateCollectorMaxRouteLength {
		route = route[:openAICodexTurnStateCollectorMaxRouteLength]
	}
	snapshot.Route = route
	if entry.active.Token.Fingerprint == token.Fingerprint {
		if snapshot.EgressPinned || snapshot.HarvestSessionID != "" || len(snapshot.HarvestCookies) > 0 {
			entry.version++
			snapshot.Version = entry.version
			entry.active = cloneOpenAICodexTurnStateSnapshot(snapshot)
		}
		entry.forceRefresh = false
		c.counts.acceptedOffers.Add(1)
		return
	}
	if !c.policy.Accept(entry.active.Token, now) {
		entry.version++
		snapshot.Version = entry.version
		entry.active = cloneOpenAICodexTurnStateSnapshot(snapshot)
		entry.ready = OpenAICodexTurnStateSnapshot{}
		entry.strikes = 0
		entry.forceRefresh = false
		c.counts.promotions.Add(1)
		c.counts.acceptedOffers.Add(1)
		return
	}
	if entry.ready.Token.Value == "" || !c.policy.Accept(entry.ready.Token, now) || !token.IssuedAt.Before(entry.ready.Token.IssuedAt) {
		snapshot.Version = 0
		entry.ready = cloneOpenAICodexTurnStateSnapshot(snapshot)
	}
	c.promoteLocked(entry, now)
	entry.candidates++
	c.counts.acceptedOffers.Add(1)
}

// ObserveQualified advances an exact, server-owned ticket after a completed,
// model-matched response. Stale responses fail the version/fingerprint CAS and
// cannot mutate a replacement ticket. Cookie time moves only when the qualified
// response actually supplied one or more cookie pairs.
func (c *OpenAICodexTurnStateCollector) ObserveQualified(
	key OpenAICodexTurnStateKey,
	value string,
	used OpenAICodexTurnStateSnapshot,
	cookies []OpenAICodexTurnStateCookie,
	now time.Time,
) bool {
	if c == nil || strings.TrimSpace(value) == "" || used.Route == "client" {
		return false
	}
	now = collectorNow(now)
	token, err := ValidateOpenAICodexTurnState(value, c.policy, now)
	if err != nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok || used.Version != entry.active.Version || used.Token.Fingerprint != entry.active.Token.Fingerprint {
		return false
	}
	entry.observed++
	c.counts.observations.Add(1)
	next := cloneOpenAICodexTurnStateSnapshot(entry.active)
	next.Token = token
	next.Route = entry.active.Route
	if len(cookies) > 0 {
		currentCookies := entry.active.HarvestCookies
		if !entry.active.cookiesFresh(now) {
			currentCookies = nil
		}
		next.HarvestCookies = mergeOpenAICodexTurnStateCookies(currentCookies, cookies)
		next.HarvestCookiesAt = now
	}
	changed := token.Fingerprint != entry.active.Token.Fingerprint || len(cookies) > 0
	if changed {
		entry.version++
		next.Version = entry.version
		entry.active = cloneOpenAICodexTurnStateSnapshot(next)
		entry.ready = OpenAICodexTurnStateSnapshot{}
		entry.candidates++
		c.counts.promotions.Add(1)
	}
	entry.strikes = 0
	entry.forceRefresh = false
	return true
}

func mergeOpenAICodexTurnStateCookies(current, updates []OpenAICodexTurnStateCookie) []OpenAICodexTurnStateCookie {
	merged := make([]OpenAICodexTurnStateCookie, 0, min(len(current)+len(updates), openAICodexTurnStateMaxCookies))
	positions := make(map[string]int, len(current)+len(updates))
	appendCookie := func(cookie OpenAICodexTurnStateCookie) {
		cookie.Name = strings.TrimSpace(cookie.Name)
		if cookie.Name == "" || len(cookie.Name) > 256 || len(cookie.Value) > 4096 {
			return
		}
		if index, exists := positions[cookie.Name]; exists {
			merged[index] = cookie
			return
		}
		if len(merged) >= openAICodexTurnStateMaxCookies {
			return
		}
		positions[cookie.Name] = len(merged)
		merged = append(merged, cookie)
	}
	for _, cookie := range current {
		appendCookie(cookie)
	}
	for _, cookie := range updates {
		appendCookie(cookie)
	}
	return merged
}

// OfferValue parses and offers a wire header in one operation.
func (c *OpenAICodexTurnStateCollector) OfferValue(key OpenAICodexTurnStateKey, value, route string, now time.Time) (OpenAICodexTurnStateToken, bool, error) {
	maxBytes := openAICodexTurnStateCollectorDefaultMaxTokenLength
	if c != nil {
		maxBytes = c.policy.MaxTokenBytes
	}
	token, err := parseOpenAICodexTurnState(value, maxBytes)
	if err != nil {
		if c != nil {
			c.counts.offers.Add(1)
			c.counts.rejectedOffers.Add(1)
		}
		return OpenAICodexTurnStateToken{}, false, err
	}
	if c == nil {
		return token, false, ErrOpenAICodexTurnStateCollectorFull
	}
	if c.policy.Blocks > 0 && token.Blocks != c.policy.Blocks {
		c.counts.offers.Add(1)
		c.counts.rejectedOffers.Add(1)
		return token, false, ErrOpenAICodexTurnStateShape
	}
	if !c.policy.Accept(token, collectorNow(now)) {
		c.counts.offers.Add(1)
		c.counts.rejectedOffers.Add(1)
		return token, false, ErrOpenAICodexTurnStateExpired
	}
	if !c.Offer(key, token, route, now) {
		return token, false, ErrOpenAICodexTurnStateShape
	}
	return token, true, nil
}

// Observe records shape evidence for a snapshot that was actually used. It
// never publishes the observed value, preventing a stale/failed response from
// replacing a newer active state. Empty headers are left to the caller to
// classify as a protocol-specific missing-state error.
func (c *OpenAICodexTurnStateCollector) Observe(key OpenAICodexTurnStateKey, value string, used OpenAICodexTurnStateSnapshot, now time.Time) bool {
	if c == nil || strings.TrimSpace(value) == "" {
		return false
	}
	now = collectorNow(now)
	c.counts.observations.Add(1)
	token, err := parseOpenAICodexTurnState(value, c.policy.MaxTokenBytes)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok {
		if err != nil {
			c.counts.suspectResponses.Add(1)
		}
		return err != nil
	}
	entry.observed++
	suspect := err != nil || !c.policy.Accept(token, now)
	if used.Version == entry.active.Version && used.Token.Fingerprint == entry.active.Token.Fingerprint {
		if suspect {
			entry.strikes++
		} else {
			entry.strikes = 0
		}
	}
	if suspect {
		c.counts.suspectResponses.Add(1)
	}
	return suspect
}

// ObserveMissing records a completed response that omitted the state header.
// The wire-level Observe method intentionally mirrors the reference store and
// treats an empty value as "no observation"; gateway response paths that know
// a state was required can use this explicit variant to add a strike.
func (c *OpenAICodexTurnStateCollector) ObserveMissing(key OpenAICodexTurnStateKey, used OpenAICodexTurnStateSnapshot, now time.Time) bool {
	if c == nil {
		return false
	}
	now = collectorNow(now)
	c.counts.observations.Add(1)
	c.counts.suspectResponses.Add(1)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok {
		return true
	}
	entry.observed++
	if used.Version == entry.active.Version && used.Token.Fingerprint == entry.active.Token.Fingerprint {
		entry.strikes++
	}
	return true
}

// RejectAndPromote invalidates only the active snapshot represented by used.
// A stale response cannot invalidate a newer active value.
func (c *OpenAICodexTurnStateCollector) RejectAndPromote(key OpenAICodexTurnStateKey, used OpenAICodexTurnStateSnapshot, now time.Time) bool {
	if c == nil {
		return false
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok || used.Version != entry.active.Version || used.Token.Fingerprint != entry.active.Token.Fingerprint {
		return false
	}
	entry.active = OpenAICodexTurnStateSnapshot{}
	entry.strikes = 0
	c.promoteLocked(entry, now)
	return c.policy.Accept(entry.active.Token, now)
}

// Invalidate clears the exact active snapshot represented by used. Unlike
// RejectAndPromote, an invalidation never promotes the standby candidate: a
// model-mismatched response is evidence that the whole state lineage is no
// longer trustworthy and the next request must collect a fresh value.
func (c *OpenAICodexTurnStateCollector) Invalidate(key OpenAICodexTurnStateKey, used OpenAICodexTurnStateSnapshot, now time.Time) bool {
	if c == nil {
		return false
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok {
		return false
	}
	// A model mismatch invalidates the whole key lineage. Fence any probe that
	// is still reading the old upstream response even when the request that
	// observed the mismatch did not borrow the active snapshot.
	if entry.probeDone != nil {
		entry.probeInvalidated = true
		entry.probeEpoch++
	}
	if used.Version != entry.active.Version || used.Token.Fingerprint != entry.active.Token.Fingerprint {
		return false
	}
	entry.active = OpenAICodexTurnStateSnapshot{}
	entry.ready = OpenAICodexTurnStateSnapshot{}
	// A model mismatch is an explicit invalidation signal. Do not carry a
	// cooldown from an earlier probe into the fresh collection round.
	entry.nextProbe = time.Time{}
	entry.strikes = 0
	entry.version++
	entry.forceRefresh = true
	return true
}

// NeedsRefresh reports whether a key should start a bounded collection round.
func (c *OpenAICodexTurnStateCollector) NeedsRefresh(key OpenAICodexTurnStateKey, now time.Time) bool {
	if c == nil {
		return true
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok {
		return true
	}
	c.promoteLocked(entry, now)
	return c.needsRefreshLocked(entry, now)
}

func (c *OpenAICodexTurnStateCollector) needsRefreshLocked(entry *openAICodexTurnStateCollectorEntry, now time.Time) bool {
	if entry == nil {
		return true
	}
	if !c.policy.Accept(entry.active.Token, now) || entry.strikes >= 2 {
		return true
	}
	return c.policy.RefreshBefore > 0 && now.Add(c.policy.RefreshBefore).After(entry.active.Token.IssuedAt.Add(c.policy.TTL))
}

func (c *OpenAICodexTurnStateCollector) hasHealthyAccountModelSiblingLocked(
	key OpenAICodexTurnStateKey,
	now time.Time,
) bool {
	for siblingKey, sibling := range c.entries {
		if sibling == nil || siblingKey == key || siblingKey.AccountID != key.AccountID ||
			siblingKey.Model != key.Model || siblingKey.generation != key.generation || sibling.forceRefresh {
			continue
		}
		c.promoteLocked(sibling, now)
		if !c.needsRefreshLocked(sibling, now) {
			return true
		}
	}
	return false
}

// Status returns bounded metadata for one key.
func (c *OpenAICodexTurnStateCollector) Status(key OpenAICodexTurnStateKey, now time.Time) OpenAICodexTurnStateStatus {
	if c == nil {
		return OpenAICodexTurnStateStatus{Key: key.canonical()}
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, false)
	if !ok {
		return OpenAICodexTurnStateStatus{Key: key.canonical()}
	}
	c.promoteLocked(entry, now)
	status := openAICodexTurnStateStatus(entry, c.policy, now)
	return status
}

// Statuses returns at most limit entries in deterministic key order. A
// non-positive limit uses a conservative default instead of exposing the full
// cache accidentally.
func (c *OpenAICodexTurnStateCollector) Statuses(now time.Time, limit int) []OpenAICodexTurnStateStatus {
	if c == nil {
		return nil
	}
	if limit <= 0 || limit > 65536 {
		limit = 1024
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(now, 64)
	keys := make([]OpenAICodexTurnStateKey, 0, len(c.entries))
	for key := range c.entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].AccountID != keys[j].AccountID {
			return keys[i].AccountID < keys[j].AccountID
		}
		if keys[i].Model != keys[j].Model {
			return keys[i].Model < keys[j].Model
		}
		return keys[i].Scope < keys[j].Scope
	})
	if len(keys) > limit {
		keys = keys[:limit]
	}
	result := make([]OpenAICodexTurnStateStatus, 0, len(keys))
	for _, key := range keys {
		entry := c.entries[key]
		c.promoteLocked(entry, now)
		result = append(result, openAICodexTurnStateStatus(entry, c.policy, now))
	}
	return result
}

func openAICodexTurnStateStatus(entry *openAICodexTurnStateCollectorEntry, policy OpenAICodexTurnStatePolicy, now time.Time) OpenAICodexTurnStateStatus {
	status := OpenAICodexTurnStateStatus{Key: entry.key, Version: entry.version, Strikes: entry.strikes, Candidates: entry.candidates, Observations: entry.observed, ProbeInFlight: entry.probeDone != nil, LastProbeAt: entry.lastProbe, NextProbeAt: entry.nextProbe}
	status.Usable = policy.Accept(entry.active.Token, now)
	status.Ready = policy.Accept(entry.ready.Token, now)
	status.ActiveFingerprint = entry.active.Token.Fingerprint
	status.ReadyFingerprint = entry.ready.Token.Fingerprint
	status.ActiveIssuedAt = entry.active.Token.IssuedAt
	status.ReadyIssuedAt = entry.ready.Token.IssuedAt
	if status.Usable {
		seconds := int(entry.active.Token.IssuedAt.Add(policy.TTL).Sub(now).Seconds())
		if seconds > 0 {
			status.RemainingSeconds = seconds
		}
	}
	status.CookieCount = len(entry.active.HarvestCookies)
	if status.CookieCount > 0 {
		if entry.active.cookiesFresh(now) {
			remaining := entry.active.HarvestCookiesAt.Add(openAICodexTurnStateCookieTTL).Sub(now)
			// Round up so the last fraction of a valid second is not reported as
			// expired while the same cookie is still eligible for injection.
			status.CookieRemainingSeconds = int((remaining + time.Second - 1) / time.Second)
		} else {
			status.CookieExpired = true
		}
	}
	return status
}

// Metrics returns aggregate counters and current bounded cardinality.
func (c *OpenAICodexTurnStateCollector) Metrics() OpenAICodexTurnStateMetrics {
	if c == nil {
		return OpenAICodexTurnStateMetrics{}
	}
	c.mu.Lock()
	entries := len(c.entries)
	c.mu.Unlock()
	return OpenAICodexTurnStateMetrics{
		Entries:          entries,
		Offers:           c.counts.offers.Load(),
		AcceptedOffers:   c.counts.acceptedOffers.Load(),
		RejectedOffers:   c.counts.rejectedOffers.Load(),
		Observations:     c.counts.observations.Load(),
		SuspectResponses: c.counts.suspectResponses.Load(),
		Promotions:       c.counts.promotions.Load(),
		Evictions:        c.counts.evictions.Load(),
		ProbeStarted:     c.counts.probeStarted.Load(),
		ProbeCoalesced:   c.counts.probeCoalesced.Load(),
		ProbeFinished:    c.counts.probeFinished.Load(),
	}
}

// StartProbe reserves one probe slot for a key and enforces the configured
// cooldown. A false result means either another probe is running, cooldown is
// active, or the bounded collector cannot admit another key.
func (c *OpenAICodexTurnStateCollector) StartProbe(key OpenAICodexTurnStateKey, now time.Time) (OpenAICodexTurnStateProbe, bool) {
	if c == nil {
		return OpenAICodexTurnStateProbe{}, false
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, true)
	if !ok {
		return OpenAICodexTurnStateProbe{}, false
	}
	if entry.probeDone != nil {
		c.counts.probeCoalesced.Add(1)
		return OpenAICodexTurnStateProbe{}, false
	}
	if c.probes >= c.policy.MaxProbeSlots {
		c.counts.probeCoalesced.Add(1)
		return OpenAICodexTurnStateProbe{}, false
	}
	if !entry.nextProbe.IsZero() && now.Before(entry.nextProbe) {
		return OpenAICodexTurnStateProbe{}, false
	}
	entry.lastProbe = now
	entry.probeEpoch++
	if entry.probeEpoch == 0 {
		entry.probeEpoch = 1
	}
	entry.probeInvalidated = false
	entry.probeDone = make(chan struct{})
	c.probes++
	c.counts.probeStarted.Add(1)
	return OpenAICodexTurnStateProbe{Key: entry.key, StartedAt: now, done: entry.probeDone, epoch: entry.probeEpoch}, true
}

// StartProbeIfRefreshNeeded atomically promotes any ready state, re-checks the
// refresh window, and acquires the single-flight lease only when collection is
// still necessary. This closes the Acquire/NeedsRefresh-to-StartProbe gap where
// a concurrent response could publish a fresh active state yet the request
// would still spend an unnecessary synthetic probe.
func (c *OpenAICodexTurnStateCollector) StartProbeIfRefreshNeeded(
	key OpenAICodexTurnStateKey,
	now time.Time,
) (OpenAICodexTurnStateProbe, OpenAICodexTurnStateProbeStartResult) {
	if c == nil {
		return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeStartUnavailable
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	canonical, currentGeneration := c.resolveKeyGenerationLocked(key, now, true)
	if !currentGeneration {
		return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeStartUnavailable
	}
	entry := c.entries[canonical]
	if entry == nil {
		// Check the account/model collection gate before allocating a per-scope
		// placeholder. Otherwise a stream of new scopes can fill MaxEntries and
		// evict the healthy entry that should suppress those probes.
		if c.hasHealthyAccountModelSiblingLocked(canonical, now) {
			return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeSkippedAccountModelHealthy
		}
		var ok bool
		entry, ok = c.entryLocked(canonical, now, true)
		if !ok {
			return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeCapacity
		}
	} else {
		entry.lastAccess = now
	}
	c.promoteLocked(entry, now)
	if !c.needsRefreshLocked(entry, now) {
		return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeSkippedHealthy
	}
	if !entry.forceRefresh && c.hasHealthyAccountModelSiblingLocked(entry.key, now) {
		return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeSkippedAccountModelHealthy
	}
	if entry.probeDone != nil {
		c.counts.probeCoalesced.Add(1)
		return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeInFlight
	}
	if c.probes >= c.policy.MaxProbeSlots {
		return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeCapacity
	}
	if !entry.nextProbe.IsZero() && now.Before(entry.nextProbe) {
		return OpenAICodexTurnStateProbe{}, OpenAICodexTurnStateProbeCooldown
	}
	entry.lastProbe = now
	entry.probeEpoch++
	if entry.probeEpoch == 0 {
		entry.probeEpoch = 1
	}
	entry.probeInvalidated = false
	entry.probeDone = make(chan struct{})
	c.probes++
	c.counts.probeStarted.Add(1)
	return OpenAICodexTurnStateProbe{
		Key:       entry.key,
		StartedAt: now,
		done:      entry.probeDone,
		epoch:     entry.probeEpoch,
	}, OpenAICodexTurnStateProbeStarted
}

// FinishProbe releases the single-flight lease and starts the next cooldown.
// It is safe to call more than once; only the active lease is closed.
func (c *OpenAICodexTurnStateCollector) FinishProbe(probe OpenAICodexTurnStateProbe, now time.Time) {
	if c == nil || probe.done == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.resolveKeyGenerationLocked(probe.Key, collectorNow(now), false)
	if !ok {
		return
	}
	entry := c.entries[key]
	if entry == nil || entry.probeDone != probe.done {
		return
	}
	invalidated := entry.probeInvalidated || entry.probeEpoch != probe.epoch
	entry.probeDone = nil
	if c.probes > 0 {
		c.probes--
	}
	close(probe.done)
	c.counts.probeFinished.Add(1)
	if invalidated {
		entry.probeInvalidated = false
		entry.forceRefresh = true
		entry.nextProbe = time.Time{}
		return
	}
	now = collectorNow(now)
	entry.nextProbe = now.Add(c.policy.ProbeCooldown)
	entry.probeInvalidated = false
}

// AbortProbe releases a probe without imposing cooldown and removes its empty
// placeholder atomically. State published concurrently by an authoritative
// response is retained.
func (c *OpenAICodexTurnStateCollector) AbortProbe(probe OpenAICodexTurnStateProbe, now time.Time) {
	if c == nil || probe.done == nil {
		return
	}
	now = collectorNow(now)
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.resolveKeyGenerationLocked(probe.Key, now, false)
	if !ok {
		return
	}
	entry := c.entries[key]
	if entry == nil || entry.probeDone != probe.done {
		return
	}
	invalidated := entry.probeInvalidated || entry.probeEpoch != probe.epoch
	entry.probeDone = nil
	if c.probes > 0 {
		c.probes--
	}
	close(probe.done)
	c.counts.probeFinished.Add(1)
	if invalidated {
		entry.probeInvalidated = false
		entry.forceRefresh = true
		entry.nextProbe = time.Time{}
		return
	}
	if entry.forceRefresh {
		entry.nextProbe = time.Time{}
		return
	}
	if !entry.active.usable(c.policy, now) && !entry.ready.usable(c.policy, now) {
		delete(c.entries, key)
	}
}

// WaitProbe waits for the current key's probe, if any. It returns waited=false
// when no probe is active.
func (c *OpenAICodexTurnStateCollector) WaitProbe(ctx context.Context, key OpenAICodexTurnStateKey) (waited bool, err error) {
	if c == nil {
		return false, nil
	}
	c.mu.Lock()
	key, ok := c.resolveKeyGenerationLocked(key, time.Now(), false)
	var entry *openAICodexTurnStateCollectorEntry
	if ok {
		entry = c.entries[key]
	}
	var done chan struct{}
	if entry != nil {
		done = entry.probeDone
	}
	c.mu.Unlock()
	if done == nil {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return true, nil
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

// Delete removes all state for a key, which is used when failover changes the
// account or when an execution scope is explicitly reset.
func (c *OpenAICodexTurnStateCollector) Delete(key OpenAICodexTurnStateKey) {
	if c == nil {
		return
	}
	c.mu.Lock()
	canonical, ok := c.resolveKeyGenerationLocked(key, time.Now(), false)
	if ok {
		if entry := c.entries[canonical]; entry == nil || entry.probeDone == nil {
			delete(c.entries, canonical)
		} else {
			// Keep the lease alive so waiters and the global probe slot remain
			// well-defined, but fence its eventual result and clear the key when
			// the old probe releases the lease.
			entry.probeInvalidated = true
			entry.probeEpoch++
			entry.active = OpenAICodexTurnStateSnapshot{}
			entry.ready = OpenAICodexTurnStateSnapshot{}
			entry.strikes = 0
			entry.version++
		}
	}
	c.mu.Unlock()
}

// DeleteAndForceRefresh retires an exact scope while leaving a bounded marker
// that bypasses account/model sibling-health suppression. This is used for
// explicit invalidation evidence such as a response-model mismatch, including
// native states that were never borrowed from the collector.
func (c *OpenAICodexTurnStateCollector) DeleteAndForceRefresh(key OpenAICodexTurnStateKey) {
	if c == nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entryLocked(key, now, true)
	if !ok {
		return
	}
	if entry.probeDone != nil {
		entry.probeInvalidated = true
		entry.probeEpoch++
	}
	entry.active = OpenAICodexTurnStateSnapshot{}
	entry.ready = OpenAICodexTurnStateSnapshot{}
	entry.nextProbe = time.Time{}
	entry.strikes = 0
	entry.version++
	entry.forceRefresh = true
}

// DeleteAccount invalidates all keys owned by one upstream account. Removing
// the current non-zero generation before deleting entries prevents already-
// issued requests and probes from recreating state with obsolete credentials;
// the next BindKey allocates a fresh process-wide nonce. No account tombstone is
// retained, so deleted account IDs cannot grow an unbounded side map.
func (c *OpenAICodexTurnStateCollector) DeleteAccount(accountID int64) {
	if c == nil || accountID <= 0 {
		return
	}
	c.mu.Lock()
	c.removeAccountLocked(accountID, false)
	c.mu.Unlock()
}

// DeleteAll invalidates every account generation and removes every cached
// state. generationSequence is deliberately preserved so an in-flight request
// can never become current again after a later BindKey recreates its account.
func (c *OpenAICodexTurnStateCollector) DeleteAll() {
	if c == nil {
		return
	}
	c.mu.Lock()
	for _, entry := range c.entries {
		if entry == nil || entry.probeDone == nil {
			continue
		}
		close(entry.probeDone)
		entry.probeDone = nil
		if c.probes > 0 {
			c.probes--
		}
		c.counts.probeFinished.Add(1)
	}
	c.entries = make(map[OpenAICodexTurnStateKey]*openAICodexTurnStateCollectorEntry)
	c.accountGenerations = make(map[int64]openAICodexTurnStateAccountGeneration)
	c.probes = 0
	c.mu.Unlock()
}
