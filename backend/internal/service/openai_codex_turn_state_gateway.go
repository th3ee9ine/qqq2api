package service

// This file connects the opaque turn-state collector to the OpenAI gateway.
// The collector deliberately knows nothing about accounts, HTTP transports, or
// SSE. Keeping those concerns here makes it possible to test the state machine
// independently and ensures API-key/relay routes never accidentally opt in.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
)

const (
	openAICodexTurnStateContextKey      = "openai_codex_turn_state_request_binding"
	openAICodexTurnStateProbeMaxDefault = int64(256 * 1024)
	openAICodexTurnStateErrorMaxDefault = int64(64 * 1024)

	// A configured pool may contain hundreds of routes, but one collection
	// round must preserve a useful connection budget for each route it tries.
	openAICodexTurnStateProbeMaxRouteAttempts = 6
	openAICodexTurnStateProbeMinRouteBudget   = time.Second
)

var openAICodexTurnStateProbeIdentityHeaders = [...]string{
	"Authorization",
	"ChatGPT-Account-Id",
	"User-Agent",
	"Version",
	"Originator",
	"OpenAI-Beta",
}

func copyCodexTurnStateProbeIdentity(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for _, name := range openAICodexTurnStateProbeIdentityHeaders {
		for _, value := range src.Values(name) {
			if value = strings.TrimSpace(value); value != "" {
				dst.Add(name, value)
			}
		}
	}
}

func copyCodexTurnStateHeaderValues(dst, src http.Header) {
	if dst == nil || src == nil {
		return
	}
	for name, values := range src {
		for _, value := range values {
			dst.Add(name, value)
		}
	}
}

type openAICodexTurnStateRequestBinding struct {
	key      OpenAICodexTurnStateKey
	model    string
	snapshot OpenAICodexTurnStateSnapshot
	used     bool
}

type openAICodexTurnStateRuntimeSettings struct {
	probeEnabled           bool
	cacheInjectionEnabled  bool
	proxyPoolURLs          []string
	proxyPoolGeneration    uint64
	harvestSpeedPreset     string
	harvestRoundInterval   time.Duration
	harvestRequestBudget   int
	harvestFailureCooldown time.Duration
}

// codexTurnStateGroupIDContextKey carries the concrete scheduling group into
// the bounded probe. The probe runs with the request context (rather than the
// Gin context), so without this small bridge its second eligibility check
// could lose group-scoped scheduler gates after the upstream call returns.
type codexTurnStateGroupIDContextKey struct{}

func withCodexTurnStateGroupID(ctx context.Context, groupID *int64) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if groupID == nil || *groupID <= 0 {
		return ctx
	}
	return context.WithValue(ctx, codexTurnStateGroupIDContextKey{}, *groupID)
}

func codexTurnStateGroupIDFromContext(ctx context.Context) *int64 {
	if ctx == nil {
		return nil
	}
	if value, ok := ctx.Value(codexTurnStateGroupIDContextKey{}).(int64); ok && value > 0 {
		return &value
	}
	// A request that already passed through the scheduler may carry the
	// immutable profit gate even when the Gin context is unavailable (for
	// example, a detached probe context). Reuse its group identity as a
	// fallback for the remaining group-scoped checks.
	if gate, ok := ctx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate); ok && gate != nil && gate.groupID > 0 {
		value := gate.groupID
		return &value
	}
	return nil
}

func codexTurnStateGroupIDForRequest(c *gin.Context, ctx context.Context) *int64 {
	if groupID := codexTurnStateGroupIDFromContext(ctx); groupID != nil {
		return groupID
	}
	if c != nil {
		if id := getOpenAIGroupIDFromContext(c); id > 0 {
			return &id
		}
	}
	if ctx != nil {
		if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && IsGroupContextValid(group) && group.ID > 0 {
			id := group.ID
			return &id
		}
	}
	return nil
}

// SetCodexTurnStateRuntimeSettings publishes the administrator-controlled
// switches as one immutable snapshot. A nil snapshot means the persisted
// settings have not been loaded yet, so both capabilities use their default-on
// behavior.
func (s *OpenAIGatewayService) SetCodexTurnStateRuntimeSettings(probeEnabled, cacheInjectionEnabled bool) {
	var proxyPool []string
	if s != nil {
		// Keep the compatibility setter from silently erasing a pool that was
		// loaded by the newer Ops settings path.
		proxyPool = s.CodexTurnStateProxyPool()
	}
	s.SetCodexTurnStateRuntimeSettingsWithProxyPool(probeEnabled, cacheInjectionEnabled, proxyPool)
}

// SetCodexTurnStateRuntimeSettingsWithProxyPool publishes the administrator
// switches and a defensive copy of the dedicated collector egress pool as one
// immutable runtime snapshot. The pool is deliberately independent of an
// account's normal proxy binding.
func (s *OpenAIGatewayService) SetCodexTurnStateRuntimeSettingsWithProxyPool(probeEnabled, cacheInjectionEnabled bool, proxyPoolURLs []string) {
	controls := defaultCodexTurnStateHarvestControls()
	if s != nil {
		if current := s.codexTurnStateRuntime.Load(); current != nil {
			controls.SpeedPreset = current.harvestSpeedPreset
			controls.MaxRequestsPerRound = current.harvestRequestBudget
			controls.FailureCooldownSeconds = int(current.harvestFailureCooldown / time.Second)
		}
	}
	s.SetCodexTurnStateRuntimeSettingsWithControls(probeEnabled, cacheInjectionEnabled, proxyPoolURLs, controls)
}

func (s *OpenAIGatewayService) SetCodexTurnStateRuntimeSettingsWithControls(
	probeEnabled, cacheInjectionEnabled bool,
	proxyPoolURLs []string,
	controls CodexTurnStateHarvestControls,
) {
	if s == nil {
		return
	}
	controls = normalizeCodexTurnStateHarvestControls(controls)
	preset := codexTurnStateHarvestPresets[controls.SpeedPreset]
	pool, err := normalizeOpenAICodexTurnStateProxyPool(proxyPoolURLs)
	if err != nil {
		pool = nil
	}
	pool = append([]string(nil), pool...)
	// Publish the pool and retire its diagnostic aggregates under the same
	// lock used by asynchronous observation writers. A generation also prevents
	// an old worker from repopulating the view after A -> B -> A pool changes.
	s.codexTurnStateProxyStatsMu.Lock()
	defer s.codexTurnStateProxyStatsMu.Unlock()
	previous := s.codexTurnStateRuntime.Load()
	var previousPool []string
	var poolGeneration uint64
	if previous != nil {
		previousPool = previous.proxyPoolURLs
		poolGeneration = previous.proxyPoolGeneration
	}
	if !slices.Equal(previousPool, pool) {
		poolGeneration++
		s.codexTurnStateSuccessfulIPs = nil
		s.codexTurnStateIPRegions = nil
		s.codexTurnStateProxyProbeLast = nil
		s.codexTurnStateHarvestNodes = nil
		s.codexTurnStateManualTickets = nil
		s.codexTurnStateProbeGates = nil
		if previous != nil && s.codexTurnStateCollector != nil {
			// A ticket pins its collection egress. Retire old generations before
			// the new pool becomes visible so removed routes cannot be reused.
			s.codexTurnStateCollector.DeleteAll()
		}
	}
	if previous == nil || previous.harvestRoundInterval != time.Duration(preset.RoundIntervalSeconds)*time.Second || previous.harvestRequestBudget != controls.MaxRequestsPerRound {
		s.codexTurnStateHarvestRoundStart = time.Time{}
		s.codexTurnStateHarvestRoundUsed = 0
	}
	s.codexTurnStateRuntime.Store(&openAICodexTurnStateRuntimeSettings{
		probeEnabled:           probeEnabled,
		cacheInjectionEnabled:  cacheInjectionEnabled,
		proxyPoolURLs:          pool,
		proxyPoolGeneration:    poolGeneration,
		harvestSpeedPreset:     controls.SpeedPreset,
		harvestRoundInterval:   time.Duration(preset.RoundIntervalSeconds) * time.Second,
		harvestRequestBudget:   controls.MaxRequestsPerRound,
		harvestFailureCooldown: time.Duration(controls.FailureCooldownSeconds) * time.Second,
	})
}

func (s *OpenAIGatewayService) CodexTurnStateHarvestControls() CodexTurnStateHarvestControls {
	if s == nil {
		return defaultCodexTurnStateHarvestControls()
	}
	settings := s.codexTurnStateRuntime.Load()
	if settings == nil {
		return defaultCodexTurnStateHarvestControls()
	}
	return normalizeCodexTurnStateHarvestControls(CodexTurnStateHarvestControls{
		SpeedPreset:            settings.harvestSpeedPreset,
		MaxRequestsPerRound:    settings.harvestRequestBudget,
		FailureCooldownSeconds: int(settings.harvestFailureCooldown / time.Second),
	})
}

// CodexTurnStateRuntimeSettings returns a lock-free runtime snapshot. Missing
// settings intentionally default to enabled so new and upgraded installations
// get the UI defaults without requiring a database migration.
func (s *OpenAIGatewayService) CodexTurnStateRuntimeSettings() (probeEnabled, cacheInjectionEnabled bool) {
	if s == nil {
		return true, true
	}
	settings := s.codexTurnStateRuntime.Load()
	if settings == nil {
		return true, true
	}
	return settings.probeEnabled, settings.cacheInjectionEnabled
}

// CodexTurnStateProxyPool returns a copy so request paths cannot mutate the
// published administrator snapshot.
func (s *OpenAIGatewayService) CodexTurnStateProxyPool() []string {
	if s == nil {
		return nil
	}
	settings := s.codexTurnStateRuntime.Load()
	if settings == nil || len(settings.proxyPoolURLs) == 0 {
		return nil
	}
	return append([]string(nil), settings.proxyPoolURLs...)
}

type openAICodexTurnStateProbeFailure struct {
	code       string
	statusCode int
	err        error
	dispatched bool
}

func (e *openAICodexTurnStateProbeFailure) Error() string {
	if e == nil {
		return ""
	}
	if e.err == nil {
		return e.code
	}
	return e.code + ": " + e.err.Error()
}

func (e *openAICodexTurnStateProbeFailure) Unwrap() error { return e.err }

func (s *OpenAIGatewayService) initCodexTurnStateCollector() {
	if s == nil || s.cfg == nil {
		return
	}
	s.codexTurnStateCollector = NewOpenAICodexTurnStateCollector(s.codexTurnStatePolicy())
	// Store an initial value so atomic.Value.Load is always well-defined even
	// before the first probe failure.
	s.codexTurnStateLastError.Store("")
}

func (s *OpenAIGatewayService) codexTurnStatePolicy() OpenAICodexTurnStatePolicy {
	cfg := s.codexTurnStateConfig()
	return OpenAICodexTurnStatePolicy{
		Blocks:        cfg.ExpectedBlocks,
		TTL:           time.Duration(cfg.TTLSeconds) * time.Second,
		RefreshBefore: time.Duration(cfg.RefreshBeforeSeconds) * time.Second,
		ProbeCooldown: time.Duration(cfg.CooldownSeconds) * time.Second,
		MaxEntries:    cfg.MaxEntries,
		MaxTokenBytes: cfg.MaxTokenBytes,
		MaxProbeSlots: 1,
		HoldActive:    cfg.HoldActive,
	}
}

func (s *OpenAIGatewayService) codexTurnStateConfig() config.GatewayCodexTurnStateConfig {
	if s == nil || s.cfg == nil {
		return config.GatewayCodexTurnStateConfig{}
	}
	return s.cfg.Gateway.CodexTurnState
}

func codexTurnStateEligibleAccount(account *Account) bool {
	return account != nil && account.IsOpenAIOAuthLike() && account.UsesOpenAICodexProtocol()
}

// codexTurnStateCollectionAccount reports whether a credential is currently a
// real scheduling candidate for automatic collection. Collection can run after
// a retry/failover boundary, so the scheduler's eligibility decision is
// re-checked immediately before probing or publishing a reusable candidate.
func codexTurnStateCollectionAccount(account *Account, model string) bool {
	if !codexTurnStateEligibleAccount(account) {
		return false
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	// IsSchedulableForModel includes the same account-level gates plus any
	// model-scoped rate-limit state used by the scheduler. Keep this helper
	// model-aware even though the request path below uses the context-aware
	// variant for hot-path probes.
	if !account.IsSchedulableForModel(model) {
		return false
	}
	if account.IsModelSupported(model) {
		return true
	}
	// The gateway normally passes the final mapped model. An explicit mapping
	// can instead be keyed by a public alias, so accept a configured target.
	for _, mapped := range account.GetModelMapping() {
		if strings.EqualFold(strings.TrimSpace(mapped), model) {
			return true
		}
	}
	return false
}

// codexTurnStateCollectionEligibility mirrors the scheduler's request-time
// availability gates that are relevant to an automatic probe. It intentionally
// runs after native client state has been admitted: a client-owned valid value
// is a passthrough concern, while a reusable cache/probe value must only be
// minted for an account that could be selected for the same model now.
func (s *OpenAIGatewayService) codexTurnStateCollectionEligibility(ctx context.Context, account *Account, model string) (bool, string) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !codexTurnStateEligibleAccount(account) {
		return false, "not_eligible"
	}
	if retryTarget := openAISameAccountRetryTarget(ctx); retryTarget > 0 && account.ID != retryTarget {
		return false, "same_account_retry_mismatch"
	}
	model = codexTurnStateModel(model)
	if model == "" {
		return false, "invalid_model"
	}
	if !account.IsSchedulable() || !account.IsSchedulableForModelWithContext(ctx, model) {
		return false, "account_unschedulable"
	}
	if !account.IsModelSupported(model) {
		// A mapped target can be the final model even when the public alias is
		// what the account's allowlist stores.
		mapped := false
		for _, value := range account.GetModelMapping() {
			if strings.EqualFold(strings.TrimSpace(value), model) {
				mapped = true
				break
			}
		}
		if !mapped {
			return false, "model_not_supported"
		}
	}
	// The ordinary scheduler asks for the Responses capability explicitly. An
	// OAuth/Codex account normally supports it, but an operator can mark a
	// credential as incapable; a probe must not mint reusable state for such an
	// account because the actual request would never be dispatched there.
	if !account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses) {
		return false, "capability_mismatch"
	}
	if s == nil {
		return true, ""
	}
	groupID := codexTurnStateGroupIDFromContext(ctx)
	if groupID != nil {
		// The scheduler treats an account without group metadata as globally
		// eligible. Enforce the group boundary only when the account carries an
		// explicit group binding; otherwise a synthetic/internal request context
		// would incorrectly suppress an otherwise schedulable Codex account.
		hasGroupMetadata := len(account.GroupIDs) > 0 || len(account.AccountGroups) > 0
		if hasGroupMetadata && !s.openAIAccountMatchesSchedulingGroup(account, groupID) {
			return false, "group_mismatch"
		}
		if s.openAIGroupRequiresPrivacySet(ctx, groupID) && !account.IsPrivacySet() {
			return false, "privacy_not_set"
		}
		if s.needsUpstreamChannelRestrictionCheck(ctx, groupID) &&
			s.isUpstreamModelRestrictedByChannel(ctx, *groupID, account, model, false) {
			return false, "channel_upstream_restricted"
		}
		// The request path normally installs this gate before forwarding. Install
		// it defensively for internal callers so collection cannot become a side
		// door around the same profit admission used by scheduling.
		ctx = s.withOpenAIProfitControlGate(ctx, groupID)
	}
	if vetoed, reason := openAIProfitControlVetoReason(ctx, account); vetoed {
		return false, reason
	}
	if s.isOpenAIAccountRequestRuntimeBlocked(account, model) {
		return false, "runtime_blocked"
	}
	if s.isOpenAIProxyStreamQuarantined(ctx, account) {
		return false, "proxy_stream_quarantined"
	}
	if s.isOpenAIAccountBlockedBySchedulingThreshold(ctx, account) {
		return false, "scheduling_threshold"
	}
	if paused, _ := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
		// The scheduler keeps detailed quota-reset reasons for its own
		// selection diagnostics. Turn-State exposes a bounded, stable reason
		// vocabulary and must classify every quota veto as ineligible; otherwise
		// a detailed `quota_auto_reset_*` value falls through the probe-failure
		// path and can be mistaken for a transient collection error.
		return false, "quota_auto_pause"
	}
	if !parentHealthyForShadow(account, s.parentAccountLookup(ctx)) {
		return false, "shadow_parent_unhealthy"
	}
	return true, ""
}

// authoritativeCodexTurnStateCollectionAccount closes the scheduler-snapshot
// race at the two cold collection boundaries (synthetic probe and response
// publication). A request may select an account just before an administrator
// disables it, then bind its collector key just after the lifecycle generation
// was rotated. Generation fencing alone cannot identify that late bind, so
// collection re-reads the persisted account before spending quota or publishing
// reusable state.
//
// Healthy cache reuse remains on the in-memory path; callers use this only when
// a key is missing/refresh-due or a probe is already collecting.
func (s *OpenAIGatewayService) authoritativeCodexTurnStateCollectionAccount(ctx context.Context, account *Account, model string) (*Account, string) {
	if eligible, reason := s.codexTurnStateCollectionEligibility(ctx, account, model); !eligible {
		return nil, reason
	}
	if s == nil || s.accountRepo == nil {
		// Narrow unit-test services may omit a repository. Production wiring always
		// installs one; retain the prior in-memory behavior for hand-built services.
		return account, ""
	}
	current, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || current == nil || current.ID != account.ID {
		return nil, "account_unavailable"
	}
	if eligible, reason := s.codexTurnStateCollectionEligibility(ctx, current, model); !eligible {
		return nil, reason
	}
	return current, ""
}

// codexTurnStateCollectionIdentityCurrent verifies that an ordinary response
// was produced by the same upstream credential identity that the repository
// currently assigns to the selected account. This is intentionally independent
// of proxy identity: Turn-State is reusable across IPs for the same account and
// model. Credential shadows compare their resolved parent source, which is the
// identity that was actually used to construct the upstream request.
func (s *OpenAIGatewayService) codexTurnStateCollectionIdentityCurrent(ctx context.Context, c *gin.Context, selected, current *Account) bool {
	if selected == nil || current == nil || selected.ID != current.ID {
		return false
	}
	requestSource := codexAccountIdentitySource(c, selected)
	currentSource := current
	if current.IsShadow() {
		if s == nil || s.accountRepo == nil {
			return false
		}
		if ctx == nil {
			ctx = context.Background()
		}
		resolved, err := resolveCredentialAccount(ctx, s.accountRepo, current)
		if err != nil || resolved == nil {
			return false
		}
		currentSource = resolved
	}
	return sameCodexTurnStateCredentialIdentity(requestSource, currentSource)
}

// canCommitOpenAICodexTurnState is the shared cold-publication boundary for
// every reusable or client-visible OAuth/Codex Turn-State. Generation fencing
// alone cannot detect a late bind after an account lifecycle event, so each
// commit also re-reads the authoritative account row and verifies that the
// credential identity which produced the response is still current.
func (s *OpenAIGatewayService) canCommitOpenAICodexTurnState(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	model, state string,
) bool {
	if s == nil || !s.codexTurnStateEligible(account) {
		return false
	}
	model = codexTurnStateModel(model)
	state = strings.TrimSpace(state)
	if model == "" || state == "" {
		return false
	}
	if _, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), time.Now()); err != nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if groupID := codexTurnStateGroupIDForRequest(c, ctx); groupID != nil {
		ctx = withCodexTurnStateGroupID(ctx, groupID)
	}
	generation, current := s.codexTurnStateRequestGenerationCurrent(c, account.ID)
	if !current || generation == 0 {
		return false
	}
	currentAccount, _ := s.authoritativeCodexTurnStateCollectionAccount(ctx, account, model)
	if currentAccount == nil {
		return false
	}
	return s.codexTurnStateCollectionIdentityCurrent(ctx, c, account, currentAccount)
}

func sameCodexTurnStateCredentialIdentity(left, right *Account) bool {
	if left == nil || right == nil || left.Platform != right.Platform || left.Type != right.Type {
		return false
	}
	leftNamespace := codexAccountIdentityNamespace(left)
	rightNamespace := codexAccountIdentityNamespace(right)
	if leftNamespace != "" || rightNamespace != "" {
		return leftNamespace != "" && leftNamespace == rightNamespace
	}
	// Legacy OAuth records may predate every stable namespace field. Fall back to
	// a credential-document digest so an account replacement cannot publish a
	// state minted by the prior identity. Token refreshes on such records may
	// recollect once, which is safer than cross-account reuse.
	return hashOpenAIAccountRuntimeDocument(left.Credentials) == hashOpenAIAccountRuntimeDocument(right.Credentials)
}

func isCodexTurnStateCollectionEligibilityReason(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "not_eligible", "same_account_retry_mismatch", "invalid_model", "account_unavailable", "account_unschedulable", "model_not_supported",
		"capability_mismatch", "group_mismatch", "privacy_not_set", "channel_upstream_restricted",
		"runtime_blocked", "proxy_stream_quarantined", "scheduling_threshold",
		"quota_auto_pause", "shadow_parent_unhealthy":
		return true
	default:
		return false
	}
}

func (s *OpenAIGatewayService) codexTurnStateEligible(account *Account) bool {
	return s != nil && s.codexTurnStateCollector != nil && codexTurnStateEligibleAccount(account)
}

func codexTurnStateModel(model string) string {
	return strings.TrimSpace(model)
}

func (s *OpenAIGatewayService) codexTurnStateKey(c *gin.Context, account *Account, model string) OpenAICodexTurnStateKey {
	var scope string
	if c != nil {
		scope, _ = boundOpenAICodexTurnStateExecutionScope(c)
		if scope == "" {
			scope = openAICodexTurnStateSeed(c)
		}
	}
	key := OpenAICodexTurnStateKey{AccountID: account.ID, Scope: scope, Model: codexTurnStateModel(model)}
	if s != nil && s.codexTurnStateCollector != nil {
		key = s.codexTurnStateCollector.BindKey(key)
	}
	return key
}

func (s *OpenAIGatewayService) bindCodexTurnStateRequest(c *gin.Context, key OpenAICodexTurnStateKey, model string, snapshot OpenAICodexTurnStateSnapshot, used bool) {
	if c == nil {
		return
	}
	c.Set(openAICodexTurnStateContextKey, openAICodexTurnStateRequestBinding{
		key: key, model: codexTurnStateModel(model), snapshot: snapshot, used: used,
	})
}

func (s *OpenAIGatewayService) codexTurnStateBinding(c *gin.Context, account *Account, model string) (openAICodexTurnStateRequestBinding, bool) {
	if c != nil {
		if raw, ok := c.Get(openAICodexTurnStateContextKey); ok {
			if binding, ok := raw.(openAICodexTurnStateRequestBinding); ok &&
				binding.key.AccountID == account.ID && strings.EqualFold(binding.model, codexTurnStateModel(model)) {
				return binding, true
			}
		}
	}
	return openAICodexTurnStateRequestBinding{key: s.codexTurnStateKey(c, account, model), model: codexTurnStateModel(model)}, false
}

// prepareCodexTurnState admits an OAuth/Codex client echo only with exact local
// account/model/generation provenance, then reuses or obtains a cached snapshot.
// Probing is synchronous only on a miss and is bounded by the configured
// timeout; a probe failure never prevents the original request from proceeding
// without injection.
func (s *OpenAIGatewayService) prepareCodexTurnState(ctx context.Context, c *gin.Context, account *Account, model string, incoming http.Header, allowProbe bool, probeIdentity ...http.Header) (OpenAICodexTurnStateSnapshot, bool) {
	// Sanitize before the eligibility fast path. An API-key or temporarily
	// unavailable account must not bypass the malformed-state guard merely
	// because it does not participate in the collector.
	model = codexTurnStateModel(model)
	if incoming != nil {
		if value := strings.TrimSpace(incoming.Get(openAICodexTurnStateHeader)); value != "" {
			if _, err := ValidateOpenAICodexTurnState(value, s.codexTurnStatePolicy(), time.Now()); err != nil {
				incoming.Del(openAICodexTurnStateHeader)
			}
		}
	}
	collectorEligible := s.codexTurnStateEligible(account)
	requestGroupID := codexTurnStateGroupIDForRequest(c, ctx)
	var key OpenAICodexTurnStateKey
	if collectorEligible {
		key = s.codexTurnStateKey(c, account, model)
	}
	now := time.Now()
	if incoming != nil {
		if value := strings.TrimSpace(incoming.Get(openAICodexTurnStateHeader)); value != "" {
			// A model-mismatch response may have already retired the collector
			// entry and provenance while the downstream client still echoes the
			// old state. Treat the tombstone as authoritative: clear the echo and
			// continue through this model's cache/probe path instead of taking the
			// anonymous native-state fast path.
			if s.openAICodexTurnStateInvalidated(c, model, value) {
				incoming.Del(openAICodexTurnStateHeader)
				if c != nil && c.Request != nil {
					c.Request.Header.Del(openAICodexTurnStateHeader)
				}
			}
			if incoming.Get(openAICodexTurnStateHeader) == "" {
				// The old value was fenced above. Fall through to the current
				// model's reusable cache/probe selection below.
			} else if codexTurnStateEligibleAccount(account) &&
				!s.openAICodexTurnStateEchoExpected(c, account, model, value) {
				// A valid envelope is not proof of its account/model lineage. OAuth/Codex
				// client input fails closed unless exact provenance (or a server-owned
				// compatibility binding) authorizes this state for the current generation.
				incoming.Del(openAICodexTurnStateHeader)
				if c != nil && c.Request != nil {
					c.Request.Header.Del(openAICodexTurnStateHeader)
				}
				s.recordCodexTurnStateCandidateReason("invalid_state")
			} else if token, err := ValidateOpenAICodexTurnState(value, s.codexTurnStatePolicy(), now); err == nil {
				// A client can echo the state we just returned from a harvested ticket.
				// Keep its server-owned identity through the ordinary cache path below;
				// treating that exact echo as native would drop its egress and cookies.
				serverOwnedEcho := false
				if collectorEligible {
					_, injectionEnabled := s.CodexTurnStateRuntimeSettings()
					active, usable := s.codexTurnStateCollector.Acquire(key, now)
					serverOwnedEcho = injectionEnabled && usable && active.Token.Value == value &&
						active.Route != "client" && (active.EgressPinned || active.HarvestSessionID != "")
				}
				if !serverOwnedEcho {
					// Proven native state remains authoritative without becoming a
					// reusable collector candidate based on client input alone.
					if collectorEligible {
						s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{Token: token, Route: "client"}, false)
					}
					return OpenAICodexTurnStateSnapshot{Token: token, Route: "client"}, true
				}
			}
			// Never forward a malformed/expired value when a usable cached value
			// or a bounded probe can be selected below.
			incoming.Del(openAICodexTurnStateHeader)
		}
	}
	if !collectorEligible {
		// Automatic collection is restricted to eligible OAuth/Codex credentials.
		// Non-collector accounts may retain their validated opaque-header behavior,
		// while an unproven OAuth/Codex echo was already stripped above.
		return OpenAICodexTurnStateSnapshot{}, false
	}
	reliableKey := strings.TrimSpace(key.Scope) != "" && strings.TrimSpace(key.Model) != ""
	// A missing final model cannot prove the model half of provenance, so no
	// OAuth/Codex state may be forwarded or selected under an unscoped key.
	if model == "" {
		s.recordCodexTurnStateCandidateReason("invalid_model")
		if incoming != nil {
			incoming.Del(openAICodexTurnStateHeader)
		}
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	// A provenance-validated client echo remains authoritative for that attempt.
	// Only automatic cache reuse/probing is gated by current scheduling state.
	collectionCtx := ctx
	if c != nil && c.Request != nil {
		collectionCtx = c.Request.Context()
	}
	if collectionCtx == nil {
		collectionCtx = context.Background()
	}
	if requestGroupID != nil {
		collectionCtx = withCodexTurnStateGroupID(collectionCtx, requestGroupID)
	}
	if eligible, reason := s.codexTurnStateCollectionEligibility(collectionCtx, account, model); !eligible {
		s.recordCodexTurnStateCandidateReason(reason)
		if incoming != nil {
			incoming.Del(openAICodexTurnStateHeader)
		}
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	// Cache reuse and probing require the complete isolation key. An unknown
	// final upstream model must never be folded into another model's cache entry.
	if !reliableKey {
		s.recordCodexTurnStateCandidateReason("unreliable_key")
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	s.consumeManualCodexTurnStateTicket(key, now)
	probeEnabled, cacheInjectionEnabled := s.CodexTurnStateRuntimeSettings()
	// Always inspect the active entry, even when injection is disabled. The
	// injection switch controls whether a usable value is attached to this
	// request; it must not turn the 55-minute collection throttle into a
	// per-request probe loop.
	active, activeUsable := s.codexTurnStateCollector.Acquire(key, now)
	refreshDue := s.codexTurnStateCollector.NeedsRefresh(key, now)
	if activeUsable && !refreshDue {
		s.recordCodexTurnStateCandidateReason("active_healthy_skipped")
		if cacheInjectionEnabled {
			s.bindCodexTurnStateRequest(c, key, model, active, true)
			return active, true
		}
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	if !allowProbe || !probeEnabled {
		if activeUsable && cacheInjectionEnabled {
			s.recordCodexTurnStateCandidateReason("active_healthy_skipped")
			s.bindCodexTurnStateRequest(c, key, model, active, true)
			return active, true
		}
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		s.recordCodexTurnStateCandidateReason("probe_disabled")
		return OpenAICodexTurnStateSnapshot{}, false
	}
	// Generation requests deliberately detach their upstream context so billing
	// can finish after the downstream disappears. A synthetic probe is different:
	// it exists only for this client attempt and must stop with that client.
	probeCtx := ctx
	if c != nil && c.Request != nil {
		probeCtx = c.Request.Context()
	}
	if probeCtx == nil {
		probeCtx = context.Background()
	}
	if requestGroupID != nil {
		probeCtx = withCodexTurnStateGroupID(probeCtx, requestGroupID)
	}
	if probeCtx.Err() != nil {
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	probe, probeStart := s.codexTurnStateCollector.StartProbeIfRefreshNeeded(key, now)
	if probeStart == OpenAICodexTurnStateProbeSkippedHealthy {
		s.recordCodexTurnStateCandidateReason("active_healthy_skipped")
		if cacheInjectionEnabled {
			if snapshot, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
				s.bindCodexTurnStateRequest(c, key, model, snapshot, true)
				return snapshot, true
			}
		}
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	if probeStart != OpenAICodexTurnStateProbeStarted {
		reason := openAICodexTurnStateProbeStartReason(probeStart)
		s.recordCodexTurnStateCandidateReason(reason)
		// A still-usable snapshot is safe while another request performs the
		// refresh. Avoid making every concurrent request wait behind one probe.
		if activeUsable && cacheInjectionEnabled {
			s.bindCodexTurnStateRequest(c, key, model, active, true)
			return active, true
		}
		if probeStart != OpenAICodexTurnStateProbeInFlight {
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		// A concurrent request may be collecting the value. Wait only for that
		// bounded flight; if it completes, the next acquire can inject it.
		_, _ = s.codexTurnStateCollector.WaitProbe(probeCtx, key)
		if probeCtx.Err() != nil {
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		if cacheInjectionEnabled {
			if snapshot, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
				s.bindCodexTurnStateRequest(c, key, model, snapshot, true)
				return snapshot, true
			}
		}
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	discardProbeEntry := false
	defer func() {
		if discardProbeEntry {
			s.codexTurnStateCollector.AbortProbe(probe, time.Now())
			return
		}
		s.codexTurnStateCollector.FinishProbe(probe, time.Now())
	}()
	// The selected account is a request snapshot. A same-ID OAuth replacement
	// can occur after scheduler selection but before the synthetic round starts;
	// do not let probeCodexTurnState silently switch credentials and publish the
	// replacement identity under the old request.
	if currentAccount, reason := s.authoritativeCodexTurnStateCollectionAccount(probeCtx, account, key.Model); currentAccount == nil {
		discardProbeEntry = true
		s.recordCodexTurnStateCandidateReason(reason)
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	} else if !s.codexTurnStateCollectionIdentityCurrent(probeCtx, c, account, currentAccount) {
		discardProbeEntry = true
		s.recordCodexTurnStateCandidateReason("account_identity_changed")
		s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
		return OpenAICodexTurnStateSnapshot{}, false
	}
	identity := incoming
	if len(probeIdentity) > 0 && probeIdentity[0] != nil {
		identity = probeIdentity[0]
	}
	if ticket, failure := s.probeCodexTurnStateTicket(probeCtx, account, key.Model, identity); failure == nil {
		if err := probeCtx.Err(); err != nil {
			discardProbeEntry = true
			s.recordCodexTurnStateProbeFailure(codexTurnStateProbeContextFailure(probeCtx, err).code)
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		// The account may have become unavailable while the bounded upstream
		// request was in flight. Never publish a reusable state for a credential
		// that the scheduler would reject now.
		if currentAccount, reason := s.authoritativeCodexTurnStateCollectionAccount(probeCtx, account, key.Model); currentAccount == nil {
			discardProbeEntry = true
			s.recordCodexTurnStateCandidateReason(reason)
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		} else if !s.codexTurnStateCollectionIdentityCurrent(probeCtx, c, account, currentAccount) {
			// The probe may have observed a replacement credential while its
			// request was in flight. Neither that response nor the pre-probe active
			// state belongs to the selected request identity.
			discardProbeEntry = true
			s.recordCodexTurnStateCandidateReason("account_identity_changed")
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		offerResult := s.codexTurnStateCollector.OfferProbeSnapshotIfRefreshNeeded(probe, ticket, time.Now())
		if offerResult == OpenAICodexTurnStateOfferPublished {
			// A successful synthetic probe is an authoritative fresh state even
			// when the normal response relay is not involved. Clear the prior
			// model-scoped tombstone only after the candidate is accepted; a probe
			// discarded by an eligibility/generation fence must not resurrect the
			// old lineage.
			s.clearOpenAICodexTurnStateInvalidationForModel(c, model, ticket.Token.Value)
			if activeUsable {
				s.recordCodexTurnStateCandidateReason("refresh_due")
			} else {
				s.recordCodexTurnStateCandidateReason("initial_missing")
			}
			s.recordCodexTurnStateProbeSuccess()
			if cacheInjectionEnabled {
				if snapshot, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
					s.bindCodexTurnStateRequest(c, key, model, snapshot, true)
					return snapshot, true
				}
			}
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		if offerResult == OpenAICodexTurnStateOfferSkippedHealthy {
			// A normal response may have published while this probe was in flight.
			// Reuse that winner without retaining this probe result as a standby
			// candidate or misclassifying the race as an invalid-state failure.
			s.recordCodexTurnStateCandidateReason("active_healthy_skipped")
			s.recordCodexTurnStateProbeSuccess()
			if cacheInjectionEnabled {
				if snapshot, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
					s.bindCodexTurnStateRequest(c, key, model, snapshot, true)
					return snapshot, true
				}
			}
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return OpenAICodexTurnStateSnapshot{}, false
		}
		s.recordCodexTurnStateCandidateReason("invalid_state")
		s.recordCodexTurnStateProbeFailure("invalid_state")
		// OfferProbe can be rejected because a concurrent model-mismatch or
		// account switch fenced this lease while the upstream response was in
		// flight. In that case the active snapshot captured before StartProbe is
		// stale and must not be injected as a refresh-failure fallback. For an
		// ordinary validation failure, refresh the snapshot under the collector
		// lock so a concurrent invalidation cannot leave us with a stale copy.
		if !s.codexTurnStateCollector.ProbeCurrent(probe) {
			discardProbeEntry = true
			activeUsable = false
		} else if activeUsable {
			if current, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
				active = current
			} else {
				activeUsable = false
			}
		}
	} else {
		if probeCtx.Err() != nil {
			discardProbeEntry = true
		}
		if isCodexTurnStateCollectionEligibilityReason(failure.code) || failure.code == "response_model_mismatch" {
			discardProbeEntry = true
			s.recordCodexTurnStateCandidateReason(failure.code)
			if failure.code == "response_model_mismatch" {
				// A probe response that declares another model proves that the
				// existing lineage cannot be trusted for this model either. Do not
				// fall through to the generic refresh-failure path below, which is
				// allowed to keep an otherwise healthy active value. The current
				// request must go upstream without Turn-State and a later request
				// will perform a fresh probe.
				if !s.codexTurnStateCollector.Invalidate(key, active, time.Now()) {
					s.codexTurnStateCollector.DeleteAndForceRefresh(key)
				}
				activeUsable = false
			} else if activeUsable {
				// Eligibility can change while the probe is in flight. Refresh
				// the fallback through the collector so a concurrent invalidation
				// or expiry cannot reintroduce the pre-probe snapshot.
				if current, usable := s.codexTurnStateCollector.Acquire(key, time.Now()); usable {
					active = current
				} else {
					activeUsable = false
				}
			}
		} else {
			// Keep the stable probe error code in the candidate breakdown as well
			// as the aggregate failure counter. This makes the reliability view
			// useful for distinguishing auth/rate-limit/transport failures from a
			// generic probe failure without exposing upstream error text.
			s.recordCodexTurnStateCandidateReason(failure.code)
			s.recordCodexTurnStateProbeFailure(failure.code)
		}
	}
	if activeUsable && cacheInjectionEnabled {
		// Refresh is best effort; a failed collection must not discard a state
		// that is still inside its local validity window.
		s.bindCodexTurnStateRequest(c, key, model, active, true)
		return active, true
	}
	s.recordCodexTurnStateCandidateReason("probe_failed")
	s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
	return OpenAICodexTurnStateSnapshot{}, false
}

func openAICodexTurnStateProbeStartReason(result OpenAICodexTurnStateProbeStartResult) string {
	switch result {
	case OpenAICodexTurnStateProbeInFlight:
		return "probe_in_progress"
	case OpenAICodexTurnStateProbeCooldown:
		return "cooldown"
	case OpenAICodexTurnStateProbeCapacity:
		return "capacity_full"
	case OpenAICodexTurnStateProbeSkippedAccountModelHealthy:
		return "active_healthy_skipped"
	case OpenAICodexTurnStateProbeSkippedHealthy:
		return "active_healthy_skipped"
	default:
		return "account_unavailable"
	}
}

func openAICodexTurnStateProbeAttemptCount(routeCount int, timeout time.Duration) int {
	if routeCount <= 0 {
		return 0
	}
	attempts := routeCount
	if attempts > openAICodexTurnStateProbeMaxRouteAttempts {
		attempts = openAICodexTurnStateProbeMaxRouteAttempts
	}
	budgetAttempts := int(timeout / openAICodexTurnStateProbeMinRouteBudget)
	if budgetAttempts < 1 {
		budgetAttempts = 1
	}
	if attempts > budgetAttempts {
		attempts = budgetAttempts
	}
	return attempts
}

// probeCodexTurnState selects a bounded rotating subset of dedicated collector
// routes per round. A configured pool is authoritative: account-level proxy
// settings are used only when the dedicated pool is empty. The cursor advances
// by the number of routes actually attempted, so immediate successes rotate one
// at a time while failed batches cover the pool without repeatedly scanning the
// same prefix. The bounded subset prevents a large pool from shrinking every
// route deadline to an unusable duration.
func (s *OpenAIGatewayService) probeCodexTurnState(parent context.Context, account *Account, model string, identityHeaders ...http.Header) (OpenAICodexTurnStateToken, *openAICodexTurnStateProbeFailure) {
	ticket, failure := s.probeCodexTurnStateTicket(parent, account, model, identityHeaders...)
	return ticket.Token, failure
}

func (s *OpenAIGatewayService) probeCodexTurnStateTicket(parent context.Context, account *Account, model string, identityHeaders ...http.Header) (ticket OpenAICodexTurnStateSnapshot, failure *openAICodexTurnStateProbeFailure) {
	if s == nil || s.httpUpstream == nil || !codexTurnStateEligibleAccount(account) {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "unavailable", err: errors.New("turn-state probe is unavailable")}
	}
	if parent == nil {
		parent = context.Background()
	}
	cfg := s.codexTurnStateConfig()
	timeout := time.Duration(cfg.ProbeTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	attemptBudget := timeout
	if parentDeadline, ok := parent.Deadline(); ok {
		if remaining := time.Until(parentDeadline); remaining < attemptBudget {
			attemptBudget = remaining
		}
	}
	probeCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	collectionAccount, reason := s.authoritativeCodexTurnStateCollectionAccount(probeCtx, account, model)
	if collectionAccount == nil {
		if reason == "" {
			reason = "account_unschedulable"
		}
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{
			code: reason,
			err:  errors.New("turn-state collection account is not currently schedulable"),
		}
	}
	account = collectionAccount
	lease, gateFailure := s.beginCodexTurnStateProbe(account.ID, model, time.Now())
	if gateFailure != nil {
		return OpenAICodexTurnStateSnapshot{}, gateFailure
	}
	defer func() {
		s.finishCodexTurnStateProbe(lease, failure, time.Now())
	}()
	diagnosticsGeneration := s.codexTurnStateDiagnosticsGeneration()
	routes := s.codexTurnStateProbeRoutes(account)
	if len(routes) == 0 {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "unavailable", err: errors.New("turn-state probe has no egress route")}
	}
	// Add returns the post-increment value. Subtract one so a fresh gateway
	// starts at route zero. After the round, advance by any additional routes
	// actually attempted: a first-route success moves by one, while an all-failed
	// six-route batch lets the next round start at the seventh route.
	start := int((s.codexTurnStateProxyCursor.Add(1) - 1) % uint64(len(routes)))
	attemptCount := openAICodexTurnStateProbeAttemptCount(len(routes), attemptBudget)
	attempted := 0
	defer func() {
		if attempted > 1 {
			s.codexTurnStateProxyCursor.Add(uint64(attempted - 1))
		}
	}()
	// Keep one total probe budget, but partition the currently remaining budget
	// across only this round's bounded attempts. Dividing by the full configured
	// pool would give a 256-route pool only milliseconds per connection. A route
	// that hangs can consume only its fair share, while later rounds rotate the
	// starting route for pool-wide fairness and diagnostics coverage.
	probeDeadline, hasProbeDeadline := probeCtx.Deadline()
	var lastFailure *openAICodexTurnStateProbeFailure
	for offset := 0; offset < len(routes) && attempted < attemptCount; offset++ {
		proxyURL := routes[(start+offset)%len(routes)]
		if !s.codexTurnStateHarvestRouteAvailable(proxyURL, time.Now()) {
			continue
		}
		routeCtx := probeCtx
		var routeCancel context.CancelFunc
		if hasProbeDeadline {
			remainingRoutes := attemptCount - attempted
			remainingBudget := time.Until(probeDeadline)
			if remainingBudget <= 0 {
				break
			}
			routeBudget := remainingBudget / time.Duration(remainingRoutes)
			if routeBudget <= 0 {
				routeBudget = remainingBudget
			}
			routeCtx, routeCancel = context.WithTimeout(probeCtx, routeBudget)
		}
		attempted++
		attemptStart := time.Now()
		ticket, failure := s.probeCodexTurnStateTicketViaProxy(routeCtx, account, model, proxyURL, identityHeaders...)
		if routeCancel != nil {
			routeCancel()
		}
		if failure != nil && failure.code == "request_budget_exhausted" {
			return OpenAICodexTurnStateSnapshot{}, failure
		}
		if failure == nil || failure.dispatched {
			s.recordCodexTurnStateHarvestNode(proxyURL, failure, time.Since(attemptStart), time.Now(), diagnosticsGeneration)
		}
		if failure == nil {
			// Exit-IP diagnostics are best effort and deliberately do not affect
			// state validity or route selection. An empty route is meaningful: it
			// represents direct egress when no account or dedicated proxy is set.
			s.recordCodexTurnStateProxySuccessAsync(proxyURL, diagnosticsGeneration)
			return ticket, nil
		}
		lastFailure = failure
		if probeCtx.Err() != nil {
			break
		}
	}
	if lastFailure != nil {
		return OpenAICodexTurnStateSnapshot{}, lastFailure
	}
	if attempted == 0 && probeCtx.Err() == nil {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "routes_cooling_down", err: errors.New("routes_cooling_down")}
	}
	return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "unavailable", err: errors.New("turn-state probe failed without a route result")}
}

func (s *OpenAIGatewayService) codexTurnStateProbeRoutes(account *Account) []string {
	if s == nil {
		return nil
	}
	if pool := s.CodexTurnStateProxyPool(); len(pool) > 0 {
		return pool
	}
	return []string{resolveAccountProxyURL(account)}
}

func (s *OpenAIGatewayService) probeCodexTurnStateTicketViaProxy(probeCtx context.Context, account *Account, model, proxyURL string, identityHeaders ...http.Header) (ticket OpenAICodexTurnStateSnapshot, failure *openAICodexTurnStateProbeFailure) {
	if s == nil || s.httpUpstream == nil || !codexTurnStateEligibleAccount(account) {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "unavailable", err: errors.New("turn-state probe is unavailable")}
	}
	if probeCtx == nil {
		probeCtx = context.Background()
	}
	cfg := s.codexTurnStateConfig()

	model = codexTurnStateModel(model)
	if model == "" {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "invalid_model", err: errors.New("final upstream model is unavailable")}
	}
	payload, err := json.Marshal(map[string]any{
		"model":               model,
		"instructions":        "Reply with OK.",
		"input":               []map[string]any{{"type": "message", "role": "user", "content": []map[string]any{{"type": "input_text", "text": "Reply with OK."}}}},
		"parallel_tool_calls": true,
		"include":             []string{"reasoning.encrypted_content"},
		"stream":              true,
		"store":               false,
	})
	if err != nil {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: err}
	}
	req, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(probeCtx, HTTPUpstreamProfileOpenAI)), http.MethodPost, chatgptCodexURL, bytes.NewReader(payload))
	if err != nil {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: err}
	}
	if len(identityHeaders) > 0 {
		copyCodexTurnStateProbeIdentity(req.Header, identityHeaders[0])
	}
	if strings.TrimSpace(req.Header.Get("Authorization")) == "" {
		token, _, tokenErr := s.GetAccessToken(probeCtx, account)
		if tokenErr != nil {
			return OpenAICodexTurnStateSnapshot{}, codexTurnStateProbeContextFailure(probeCtx, tokenErr)
		}
		authHeaders, authErr := s.buildOpenAIAuthenticationHeaders(probeCtx, account, token)
		if authErr != nil {
			return OpenAICodexTurnStateSnapshot{}, codexTurnStateProbeContextFailure(probeCtx, authErr)
		}
		copyCodexTurnStateHeaderValues(req.Header, authHeaders)
	}
	req.Host = "chatgpt.com"
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(probeCtx, s.accountRepo, req.Header, account); err != nil {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: err}
	}
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cache-control", "no-cache")
	if strings.TrimSpace(req.Header.Get("originator")) == "" {
		req.Header.Set("originator", resolveCodexOutboundIdentityForAccount(account).originator)
	}
	if strings.TrimSpace(req.Header.Get("OpenAI-Beta")) == "" {
		applyOpenAICodexBetaFeatures(nil, account, req.Header)
	}
	enforceCodexIdentityHeadersWithAccount(req.Header, account)
	account.ApplyHeaderOverrides(req.Header)
	harvestSessionID := uuid.NewString()
	applyOpenAICodexTurnStateTicketHeaders(req.Header, OpenAICodexTurnStateSnapshot{HarvestSessionID: harvestSessionID}, time.Now())
	req.Header.Del(openAICodexTurnStateHeader)

	credentialAccount, credentialErr := resolveCredentialAccount(probeCtx, s.accountRepo, account)
	if credentialErr != nil {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: credentialErr}
	}
	if !s.reserveCodexTurnStateHarvestRequest(time.Now()) {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "request_budget_exhausted", err: errors.New("request_budget_exhausted")}
	}
	defer func() {
		if failure != nil {
			failure.dispatched = true
		}
	}()
	response, _, err := doOpenAIOAuthTransportWithCredentialAccount(
		req,
		proxyURL,
		account,
		credentialAccount,
		s.pluginManager,
		s.httpUpstream,
		s.tlsFPProfileService,
		true,
	)
	if err != nil {
		return OpenAICodexTurnStateSnapshot{}, codexTurnStateProbeContextFailure(probeCtx, err)
	}
	if response == nil {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "transport_error", err: errors.New("nil probe response")}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		responseBody := readCodexTurnStateProbeErrorBody(response.Body, cfg.MaxProbeResponseBytes)
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			s.handleCodexTurnStateProbeUpstreamError(probeCtx, account, response.StatusCode, response.Header, responseBody)
		}
		code := "transport_error"
		switch {
		case response.StatusCode == http.StatusUnauthorized:
			code = "upstream_401"
		case response.StatusCode == http.StatusForbidden:
			code = "upstream_403"
		case response.StatusCode == http.StatusTooManyRequests:
			code = "upstream_429"
		case response.StatusCode >= http.StatusInternalServerError:
			code = "upstream_5xx"
		}
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: code, statusCode: response.StatusCode, err: errors.New(code)}
	}
	maxBytes := cfg.MaxProbeResponseBytes
	if maxBytes <= 0 {
		maxBytes = openAICodexTurnStateProbeMaxDefault
	}
	outcome, readErr := inspectCodexTurnStateProbeSSE(response.Body, maxBytes)
	if readErr != nil {
		code := "incomplete_stream"
		if errors.Is(readErr, context.Canceled) || errors.Is(probeCtx.Err(), context.Canceled) {
			code = "cancelled"
		} else if errors.Is(readErr, context.DeadlineExceeded) || errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			code = "probe_timeout"
		}
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: code, err: readErr}
	}
	if outcome.failureEvent != "" {
		code := classifyCodexTurnStateProbeStreamFailure(outcome.failureEvent, outcome.failureCode)
		if code == "upstream_rate_limited" {
			s.handleCodexTurnStateProbeUpstreamError(probeCtx, account, http.StatusTooManyRequests, response.Header, nil)
		}
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: code, err: errors.New(code)}
	}
	if !outcome.completed {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "incomplete_stream", err: errors.New("probe did not receive response.completed")}
	}
	if !codexTurnStateProbeModelsMatch(model, outcome.models) {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "response_model_mismatch", err: errors.New("response_model_mismatch")}
	}
	state := strings.TrimSpace(response.Header.Get(openAICodexTurnStateHeader))
	if state == "" {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "invalid_state", err: errors.New("probe response omitted turn state")}
	}
	tokenValue, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), time.Now())
	if err != nil {
		return OpenAICodexTurnStateSnapshot{}, &openAICodexTurnStateProbeFailure{code: "invalid_state", err: err}
	}
	if err := probeCtx.Err(); err != nil {
		return OpenAICodexTurnStateSnapshot{}, codexTurnStateProbeContextFailure(probeCtx, err)
	}
	now := time.Now()
	cookies := responseOpenAICodexTurnStateCookies(response.Header)
	return OpenAICodexTurnStateSnapshot{
		Token:            tokenValue,
		Route:            "probe",
		EgressProxyURL:   strings.TrimSpace(proxyURL),
		EgressPinned:     true,
		HarvestSessionID: harvestSessionID,
		HarvestCookies:   cookies,
		HarvestCookiesAt: func() time.Time {
			if len(cookies) > 0 {
				return now
			}
			return time.Time{}
		}(),
	}, nil
}

type openAICodexTurnStateProbeSSEOutcome struct {
	completed    bool
	failureEvent string
	failureCode  string
	models       []string
}

func codexTurnStateProbeContextFailure(ctx context.Context, err error) *openAICodexTurnStateProbeFailure {
	code := "transport_error"
	if errors.Is(err, context.Canceled) || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)) {
		code = "cancelled"
	} else if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		code = "probe_timeout"
	}
	return &openAICodexTurnStateProbeFailure{code: code, err: err}
}

func readCodexTurnStateProbeErrorBody(body io.Reader, configuredLimit int64) []byte {
	if body == nil {
		return nil
	}
	limit := configuredLimit
	if limit <= 0 || limit > openAICodexTurnStateErrorMaxDefault {
		limit = openAICodexTurnStateErrorMaxDefault
	}
	data, _ := io.ReadAll(io.LimitReader(body, limit))
	return data
}

func (s *OpenAIGatewayService) handleCodexTurnStateProbeUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte) {
	if s == nil || account == nil {
		return
	}
	s.handleOpenAIAuxiliaryUpstreamError(ctx, account, statusCode, headers, responseBody)
	if statusCode != http.StatusTooManyRequests || !isOpenAIOAuthAccount(account) || account.IsShadow() {
		return
	}
	// A probe has no generation retry path. Park this credential immediately so
	// Retry-After is shared with normal scheduling instead of starting the
	// request-local same-account retry window used by generation requests.
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	disposition, resetAt := classifyOpenAIOAuth429(headers, responseBody)
	now := time.Now()
	cooldownUntil := s.openAIOAuth429CooldownUntil(stateCtx, account, headers, resetAt, now)
	reason := "turn_state_probe_429"
	if disposition != openAIOAuth429Transient {
		reason = "turn_state_probe_quota"
	}
	s.BlockAccountScheduling(account, cooldownUntil, reason)
	s.openaiOAuth429RetryStartedAt.Delete(account.ID)
}

func classifyCodexTurnStateProbeStreamFailure(eventType, code string) string {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "server_is_overloaded", "slow_down":
		return "model_capacity"
	case "rate_limit_exceeded", "insufficient_quota":
		return "upstream_rate_limited"
	}
	switch eventType {
	case "slow_down":
		return "model_capacity"
	case "response.incomplete":
		return "incomplete_stream"
	default:
		return "response_failed"
	}
}

// codexTurnStateModelIdentity is the narrow model identity used for reusable
// Turn-State provenance and response validation.  Billing/model-routing helpers
// intentionally collapse a broad family (for example gpt-5.6-sol-high and
// gpt-5.6-sol-low) onto one pricing/runtime base.  That collapse is unsafe for
// Turn-State: the upstream can mint a state for one concrete model variant and
// reject it for another.  Keep only the unambiguous public aliases collapsed;
// preserve every explicit suffix (reasoning level, date, build, etc.) so
// different model strings cannot reuse one another's state.
func codexTurnStateModelIdentity(model string) string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return ""
	}
	canonical := canonicalizeOpenAIModelAliasSpelling(trimmed)
	if canonical == "" {
		// Unknown/provider-specific names still need deterministic, case-
		// insensitive isolation even when the generic OpenAI canonicalizer does
		// not recognize them as a GPT/Codex alias.
		return strings.ToLower(trimmed)
	}
	switch canonical {
	case "gpt-6":
		// The bare public alias is an explicit route to Astra, not a distinct
		// upstream model.
		return "gpt-6-astra"
	case "gpt-5.6":
		// Keep the one documented bare GPT-5.6 alias equivalent to Sol.  Do
		// not apply normalizeKnownOpenAICodexModel here: it would also fold
		// reasoning/date variants that must remain isolated.
		return "gpt-5.6-sol"
	default:
		return canonical
	}
}

func codexTurnStateModelIdentitiesMatch(left, right string) bool {
	leftIdentity := codexTurnStateModelIdentity(left)
	rightIdentity := codexTurnStateModelIdentity(right)
	return leftIdentity != "" && leftIdentity == rightIdentity
}

func codexTurnStateProbeModelsMatch(requested string, observed []string) bool {
	// Probe acceptance must use the same concrete identity as cache/provenance
	// isolation. Family normalization intentionally collapses reasoning/date
	// variants for billing, but doing that here would mint a state for one
	// model and make it reusable by another model.
	want := codexTurnStateModelIdentity(requested)
	if want == "" || len(observed) == 0 {
		return false
	}
	for _, model := range observed {
		if got := codexTurnStateModelIdentity(model); got == "" || got != want {
			return false
		}
	}
	return true
}

func inspectCodexTurnStateProbeSSE(body io.Reader, maxBytes int64) (openAICodexTurnStateProbeSSEOutcome, error) {
	var outcome openAICodexTurnStateProbeSSEOutcome
	if body == nil {
		return outcome, errors.New("nil probe body")
	}
	if maxBytes <= 0 {
		maxBytes = openAICodexTurnStateProbeMaxDefault
	}
	reader := bufio.NewReader(io.LimitReader(body, maxBytes+1))
	var data bytes.Buffer
	var consumed int64
	var eventName string
	flush := func() {
		if data.Len() == 0 {
			eventName = ""
			return
		}
		payload := bytes.TrimSpace(data.Bytes())
		data.Reset()
		defer func() { eventName = "" }()
		if bytes.Equal(payload, []byte("[DONE]")) {
			return
		}
		if !json.Valid(payload) {
			return
		}
		var envelope struct {
			Type  string `json:"type"`
			Model string `json:"model"`
			Code  string `json:"code"`
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
			Response struct {
				Model string `json:"model"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal(payload, &envelope) != nil {
			return
		}
		typ := strings.TrimSpace(envelope.Type)
		if typ == "" {
			typ = strings.TrimSpace(eventName)
		}
		switch typ {
		case "response.created", "response.completed":
			model := strings.TrimSpace(envelope.Response.Model)
			if model == "" {
				// Some compatible SSE relays flatten the model declaration onto
				// the event envelope instead of retaining response.model. Treat
				// both forms as authoritative for probe/model isolation.
				model = strings.TrimSpace(envelope.Model)
			}
			if model != "" {
				outcome.models = append(outcome.models, model)
			}
			if typ == "response.completed" {
				outcome.completed = true
			}
		case "response.failed", "response.incomplete", "error", "slow_down":
			code := strings.TrimSpace(envelope.Response.Error.Code)
			if code == "" {
				code = strings.TrimSpace(envelope.Error.Code)
			}
			if code == "" {
				code = strings.TrimSpace(envelope.Code)
			}
			currentClass := classifyCodexTurnStateProbeStreamFailure(typ, code)
			priorClass := classifyCodexTurnStateProbeStreamFailure(outcome.failureEvent, outcome.failureCode)
			if outcome.failureEvent == "" || currentClass == "upstream_rate_limited" || priorClass == "response_failed" {
				outcome.failureEvent = typ
				outcome.failureCode = code
			}
		}
	}
	for {
		line, readErr := reader.ReadString('\n')
		consumed += int64(len(line))
		if consumed > maxBytes {
			return openAICodexTurnStateProbeSSEOutcome{}, errors.New("probe response exceeded bounded body")
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		} else if strings.HasPrefix(trimmed, "data:") {
			part := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(part)
		} else if trimmed == "" {
			flush()
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				flush()
				break
			}
			return openAICodexTurnStateProbeSSEOutcome{}, readErr
		}
	}
	return outcome, nil
}

// readCodexTurnStateProbeSSE is kept as the narrow test-facing shape used by
// the original collector tests. The probe itself consumes the richer outcome
// above so model and stable error-code checks cannot be skipped.
func readCodexTurnStateProbeSSE(body io.Reader, maxBytes int64) (model string, completed bool, terminal string, err error) {
	outcome, err := inspectCodexTurnStateProbeSSE(body, maxBytes)
	if len(outcome.models) > 0 {
		model = outcome.models[len(outcome.models)-1]
	}
	return model, outcome.completed, outcome.failureEvent, err
}

// observeCodexTurnStateResponse uses the request-scoped model observer used by
// HTTP/SSE forwarding. WS forwarding keeps its observer local, so it calls the
// detailed variant below and passes both the observed model and conflict bit.
func (s *OpenAIGatewayService) observeCodexTurnStateResponse(c *gin.Context, account *Account, model string, upstream http.Header, observedModels ...string) {
	observedModel := ""
	if len(observedModels) > 0 {
		observedModel = strings.TrimSpace(observedModels[0])
	}
	if observedModel == "" {
		observedModel = strings.TrimSpace(observedUpstreamResponseModel(c))
	}
	s.observeCodexTurnStateResponseDetails(c, account, model, upstream, observedModel, observedUpstreamResponseModelConflict(c))
}

func codexTurnStateResponseModelMismatch(requested, observed string, conflict bool) bool {
	if conflict {
		return true
	}
	observed = strings.TrimSpace(observed)
	return observed != "" && !codexTurnStateModelIdentitiesMatch(requested, observed)
}

func (s *OpenAIGatewayService) observeCodexTurnStateResponseDetails(c *gin.Context, account *Account, model string, upstream http.Header, observedModel string, responseModelConflict bool) {
	if s == nil || !codexTurnStateEligibleAccount(account) || upstream == nil {
		return
	}
	binding, bound := s.codexTurnStateBinding(c, account, model)
	if !bound {
		binding.key = s.codexTurnStateKey(c, account, model)
	}
	// A response model mismatch means the state was minted for a different
	// model and must not remain reusable. HTTP gets the conflict bit from the
	// request-scoped observer; WS passes its local observer explicitly.
	if codexTurnStateResponseModelMismatch(model, observedModel, responseModelConflict) {
		s.recordCodexTurnStateCandidateReason("response_model_mismatch")
		now := time.Now()
		// Preserve a bounded fingerprint before retiring collector/provenance
		// state. The downstream client may echo the already-issued value on its
		// next request; without this tombstone it would be mistaken for an
		// untracked native state and sent upstream again.
		// The binding snapshot records the state that was actually sent for both
		// collector-backed and native/client-owned requests. Native state is not
		// marked as `used` because it must never participate in collector
		// observation/promotion, but a model mismatch still invalidates that exact
		// request lineage rather than an unrelated state returned in the response.
		invalidatedState := strings.TrimSpace(binding.snapshot.Token.Value)
		if invalidatedState == "" {
			invalidatedState = strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
		}
		s.invalidateOpenAICodexTurnStateForModel(c, model, invalidatedState)
		// The caller may still stage this response header after observation
		// (notably for non-streaming/compact responses). Remove the invalid
		// value from every header copy before that staging point.
		deleteOpenAIHeaderEqualFold(upstream, openAICodexTurnStateHeader)
		if binding.used {
			if !s.codexTurnStateCollector.Invalidate(binding.key, binding.snapshot, now) {
				s.codexTurnStateCollector.DeleteAndForceRefresh(binding.key)
			}
		} else {
			s.codexTurnStateCollector.DeleteAndForceRefresh(binding.key)
		}
		s.clearOpenAIWSSessionTurnStateForModel(c, account, model)
		markOpenAIWSTurnStateModelMismatch(c)
		s.clearOpenAICodexTurnStateProvenanceForModel(c, model)
		if c != nil && c.Request != nil {
			c.Request.Header.Del(openAICodexTurnStateHeader)
		}
		if c != nil && c.Writer != nil && !c.Writer.Written() {
			c.Writer.Header().Del(openAICodexTurnStateHeader)
		}
		s.recordCodexTurnStateProbeFailure("response_model_mismatch")
		return
	}
	// The collector may have been disabled or the account may have become
	// temporarily unavailable by the time the response is observed. A model
	// mismatch remains authoritative evidence that the already-issued state is
	// invalid, so the branch above must run for OAuth/Codex accounts even when
	// no collector instance is currently available.
	if !s.codexTurnStateEligible(account) {
		return
	}
	// A normal response observation is deliberately ignored after cancellation:
	// the downstream may never have received its state.  A model mismatch is
	// different: it is evidence that the state already used by this attempt is
	// invalid, so the branch above must run even while the request is draining.
	if c != nil && c.Request != nil {
		select {
		case <-c.Request.Context().Done():
			return
		default:
		}
	}
	state := strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
	if state == "" {
		if binding.used {
			s.codexTurnStateCollector.ObserveMissing(binding.key, binding.snapshot, time.Now())
			s.recordCodexTurnStateCandidateReason("missing_response_state")
		}
		return
	}
	// Validate the wire value before consulting the healthy-active shortcut. A
	// response can carry a malformed, expired, or wrong-shape state even while
	// the request's previously injected active value is still healthy. Leaving
	// this check below the shortcut lets that unexpected value escape through
	// bridge/HTTP response staging and makes the next turn inherit bad state.
	now := time.Now()
	token, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), now)
	if err != nil {
		if binding.used {
			// Keep suspect-response accounting for the state that was actually
			// used, but never publish the invalid wire value as a candidate.
			s.codexTurnStateCollector.Observe(binding.key, state, binding.snapshot, now)
		}
		deleteOpenAIHeaderEqualFold(upstream, openAICodexTurnStateHeader)
		if c != nil && c.Writer != nil && !c.Writer.Written() {
			c.Writer.Header().Del(openAICodexTurnStateHeader)
		}
		s.recordCodexTurnStateCandidateReason("invalid_state")
		s.recordCodexTurnStateProbeFailure("invalid_state")
		return
	}
	// A successful response is an authoritative observation of a new lineage,
	// including probe/streaming paths that do not write provenance before this
	// observer runs. Let it retire the prior model-scoped tombstone; an exact
	// re-observation of the invalidated value is intentionally left fenced.
	s.clearOpenAICodexTurnStateInvalidationForModel(c, model, state)
	if s.openAICodexTurnStateInvalidated(c, model, state) {
		if binding.used {
			if !s.codexTurnStateCollector.Invalidate(binding.key, binding.snapshot, now) {
				s.codexTurnStateCollector.DeleteAndForceRefresh(binding.key)
			}
		} else {
			s.codexTurnStateCollector.DeleteAndForceRefresh(binding.key)
		}
		// Do not let a client or a retry re-introduce the exact lineage that a
		// model-mismatched response retired. It is invalid for both downstream
		// relay and collector promotion until a genuinely different state is
		// observed.
		deleteOpenAIHeaderEqualFold(upstream, openAICodexTurnStateHeader)
		if c != nil && c.Writer != nil && !c.Writer.Written() {
			c.Writer.Header().Del(openAICodexTurnStateHeader)
		}
		s.recordCodexTurnStateCandidateReason("invalid_state")
		s.recordCodexTurnStateProbeFailure("invalid_state")
		return
	}
	if binding.used {
		// A completed, model-matched response advances the exact ticket that this
		// request consumed. Re-check eligibility and authoritative identity before
		// the CAS so a disabled/replaced account cannot refresh either state or
		// cookies while the request is draining.
		collectionCtx := context.Background()
		if c != nil && c.Request != nil {
			collectionCtx = c.Request.Context()
		}
		if groupID := codexTurnStateGroupIDForRequest(c, collectionCtx); groupID != nil {
			collectionCtx = withCodexTurnStateGroupID(collectionCtx, groupID)
		}
		if eligible, reason := s.codexTurnStateCollectionEligibility(collectionCtx, account, model); !eligible {
			s.recordCodexTurnStateCandidateReason(reason)
			return
		}
		currentAccount, reason := s.authoritativeCodexTurnStateCollectionAccount(collectionCtx, account, model)
		if currentAccount == nil {
			s.recordCodexTurnStateCandidateReason(reason)
			return
		}
		if !s.codexTurnStateCollectionIdentityCurrent(collectionCtx, c, account, currentAccount) {
			s.recordCodexTurnStateCandidateReason("account_identity_changed")
			return
		}
		// ObserveQualified owns both state rotation and the independent cookie
		// timestamp. A failed CAS means another request already replaced this
		// ticket; never feed the stale response into the ordinary offer path.
		s.codexTurnStateCollector.ObserveQualified(
			binding.key,
			state,
			binding.snapshot,
			responseOpenAICodexTurnStateCookies(upstream),
			now,
		)
		return
	}
	// A response can still be relayed to a native client without a reliable
	// execution scope, but it must never create a reusable collector entry.
	if strings.TrimSpace(binding.key.Scope) == "" || strings.TrimSpace(binding.key.Model) == "" {
		s.recordCodexTurnStateCandidateReason("unreliable_key")
		return
	}
	// Do not retain a standby candidate while the active state is healthy. This
	// was the source of the large "待发布候选" backlog: every successful turn
	// generated a new opaque value even though refresh was not due. A response
	// can publish only on an empty/invalid key or once the configured refresh
	// window (55 minutes by default) has opened.
	collectionCtx := context.Background()
	if c != nil && c.Request != nil {
		collectionCtx = c.Request.Context()
	}
	// The response observer runs after the scheduler has selected the account,
	// but the Gin context still carries the concrete group used for that
	// selection. Preserve it on the detached eligibility context so a group,
	// privacy, channel, or profit veto that appears while the request is in
	// flight prevents publishing a reusable candidate.
	if groupID := codexTurnStateGroupIDForRequest(c, collectionCtx); groupID != nil {
		collectionCtx = withCodexTurnStateGroupID(collectionCtx, groupID)
	}
	if eligible, reason := s.codexTurnStateCollectionEligibility(collectionCtx, account, model); !eligible {
		s.recordCodexTurnStateCandidateReason(reason)
		return
	}
	_, activeUsable := s.codexTurnStateCollector.Acquire(binding.key, now)
	refreshDue := s.codexTurnStateCollector.NeedsRefresh(binding.key, now)
	if activeUsable && !refreshDue {
		// This is the dominant per-turn path. Return before the authoritative DB
		// lookup; if a concurrent invalidation wins after this snapshot, skipping a
		// candidate is safe and the next request will collect under the new state.
		s.recordCodexTurnStateCandidateReason("active_healthy_skipped")
		return
	}
	currentAccount, reason := s.authoritativeCodexTurnStateCollectionAccount(collectionCtx, account, model)
	if currentAccount == nil {
		s.recordCodexTurnStateCandidateReason(reason)
		return
	}
	if !s.codexTurnStateCollectionIdentityCurrent(collectionCtx, c, account, currentAccount) {
		s.recordCodexTurnStateCandidateReason("account_identity_changed")
		return
	}
	offerResult := s.codexTurnStateCollector.OfferIfRefreshNeeded(binding.key, token, "response", now)
	if offerResult == OpenAICodexTurnStateOfferSkippedHealthy {
		// The same-lock decision also covers a concurrent response or probe that
		// became healthy after the diagnostic snapshot above was read.
		s.recordCodexTurnStateCandidateReason("active_healthy_skipped")
		return
	}
	if offerResult != OpenAICodexTurnStateOfferPublished {
		s.recordCodexTurnStateCandidateReason("state_time_rejected")
		s.recordCodexTurnStateProbeFailure("state_time_rejected")
		return
	}
	if activeUsable && refreshDue {
		s.recordCodexTurnStateCandidateReason("refresh_due")
	} else {
		s.recordCodexTurnStateCandidateReason("initial_missing")
	}
	s.recordCodexTurnStateProbeSuccess()
}

func (s *OpenAIGatewayService) recordCodexTurnStateProbeSuccess() {
	if s == nil {
		return
	}
	s.codexTurnStateProbeSuccesses.Add(1)
	s.codexTurnStateLastSuccessUnix.Store(time.Now().Unix())
}

func (s *OpenAIGatewayService) recordCodexTurnStateProbeFailure(code string) {
	if s == nil {
		return
	}
	if code == "" {
		code = "other"
	}
	s.codexTurnStateProbeFailures.Add(1)
	s.codexTurnStateLastFailureUnix.Store(time.Now().Unix())
	s.codexTurnStateLastError.Store(code)
}

// CodexTurnStateReliabilitySnapshot implements the optional Ops projection.
// Only aggregate counts and allow-listed error codes leave this service.
func (s *OpenAIGatewayService) CodexTurnStateReliabilitySnapshot(ctx context.Context) OpenAICodexTurnStateReliabilitySnapshot {
	if s == nil || s.codexTurnStateCollector == nil {
		return OpenAICodexTurnStateReliabilitySnapshot{Enabled: false, Status: "disabled"}
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return OpenAICodexTurnStateReliabilitySnapshot{Enabled: true, Status: "unavailable"}
		default:
		}
	}
	now := time.Now()
	metrics := s.codexTurnStateCollector.Metrics()
	statuses := s.codexTurnStateCollector.Statuses(now, metrics.Entries)
	active, ready, collecting := 0, 0, false
	cooling := false
	cookieCount, cookieActive, cookieExpired, cookieRemaining := 0, 0, 0, 0
	for _, status := range statuses {
		if status.Usable {
			active++
		}
		if status.Ready {
			ready++
		}
		collecting = collecting || status.ProbeInFlight
		cooling = cooling || (!status.NextProbeAt.IsZero() && now.Before(status.NextProbeAt))
		cookieCount += status.CookieCount
		if status.CookieCount > 0 {
			if status.CookieExpired {
				cookieExpired += status.CookieCount
			} else {
				if cookieActive == 0 || status.CookieRemainingSeconds < cookieRemaining {
					cookieRemaining = status.CookieRemainingSeconds
				}
				cookieActive += status.CookieCount
			}
		}
	}
	status := "idle"
	switch {
	case collecting:
		status = "collecting"
	case active > 0:
		status = "ready"
	case cooling:
		status = "cooldown"
	case s.codexTurnStateProbeFailures.Load() > 0:
		status = "degraded"
	case len(statuses) > 0:
		status = "stale"
	}
	var lastSuccess, lastFailure *time.Time
	if unix := s.codexTurnStateLastSuccessUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0).UTC()
		lastSuccess = &t
	}
	if unix := s.codexTurnStateLastFailureUnix.Load(); unix > 0 {
		t := time.Unix(unix, 0).UTC()
		lastFailure = &t
	}
	lastError := ""
	if raw := s.codexTurnStateLastError.Load(); raw != nil {
		lastError, _ = raw.(string)
	}
	probeEnabled, cacheInjectionEnabled := s.CodexTurnStateRuntimeSettings()
	successfulIPs, ipRegions, candidateBreakdown := s.codexTurnStateDiagnostics()
	nodes, budgetUsed, budgetLimit, budgetResetAt := s.codexTurnStateHarvestSummary(now)
	return OpenAICodexTurnStateReliabilitySnapshot{
		Enabled:                true,
		ProbeEnabled:           probeEnabled,
		InjectionEnabled:       cacheInjectionEnabled,
		Status:                 status,
		Ready:                  active > 0,
		Collecting:             collecting,
		ActiveEntries:          active,
		ReadyCandidates:        ready,
		Observations:           metrics.Observations,
		Successes:              s.codexTurnStateProbeSuccesses.Load(),
		Failures:               s.codexTurnStateProbeFailures.Load(),
		LastSuccessAt:          lastSuccess,
		LastFailureAt:          lastFailure,
		LastErrorCode:          lastError,
		CookieCount:            cookieCount,
		CookieActiveCount:      cookieActive,
		CookieExpiredCount:     cookieExpired,
		CookieRemainingSeconds: cookieRemaining,
		HarvestNodes:           nodes,
		BudgetUsed:             budgetUsed,
		BudgetLimit:            budgetLimit,
		BudgetResetAt:          budgetResetAt,
		ProxyPool:              summarizeOpenAICodexTurnStateProxyPool(s.CodexTurnStateProxyPool()),
		SuccessfulIPRegions:    ipRegions,
		SuccessfulIPs:          successfulIPs,
		CandidateBreakdown:     candidateBreakdown,
	}
}

// compile-time check: keep the provider contract visible at the implementation
// site so a future Ops refactor cannot silently drop the projection.
var _ OpenAICodexTurnStateReliabilityProvider = (*OpenAIGatewayService)(nil)
