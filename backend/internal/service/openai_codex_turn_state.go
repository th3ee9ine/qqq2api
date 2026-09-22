package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// openAICodexTurnStateHeader 是 Codex 的回合状态头。上游在响应头中铸造该
// 不透明 blob，客户端在同一回合的后续请求中原样回带（codex-rs 侧从
// /responses SSE、/responses/compact JSON 与 WS 握手三种响应中捕获，见
// codex-api/src/sse/responses.rs 与 endpoint/compact.rs）。
const openAICodexTurnStateHeader = "x-codex-turn-state"

const openAICodexTurnStateExecutionScopeContextKey = "openai_codex_turn_state_execution_scope"
const openAICodexTurnStateTrustedInjectionContextKey = "openai_codex_turn_state_trusted_injection"

// Model-specific provenance keys prevent concurrent requests for different
// models that share one downstream execution scope from overwriting each
// other's origin. The plain seed key is retained as a compatibility mirror for
// current records; strict outbound admission still requires a recorded model
// and collector generation.
const openAICodexTurnStateOriginModelKeySeparator = "\x00turn-state-model:"
const openAICodexTurnStateInvalidationSuffix = "\x00turn-state-invalidated"

// turn-state blob 是上游在"出站身份"（含 #5553 指纹收敛改写后的
// installation/session/thread 标识）下铸造的，同账号回放自洽；跨账号回放
// （failover 换号后客户端仍回带旧账号的 blob）是代理链独有、真实 Codex
// 永远不会产生的矛盾信号。溯源表记录每个下游会话最近一次铸造该 blob 的
// 账号，出站守卫据此剥离已知异账号的回带值。
type openAICodexTurnStateOrigin struct {
	accountID  int64
	model      string
	stateHash  [sha256.Size]byte
	expiresAt  time.Time
	generation uint64
}

type openAICodexTurnStateInvalidation struct {
	stateHash  [sha256.Size]byte
	expiresAt  time.Time
	generation uint64
}

// openAICodexTurnStateTrustedInjection identifies a server-owned state that is
// temporarily staged on the ordinary request-header path. Messages compatibility
// and the model-scoped WS store are already isolated by account/model and fenced
// by the collector generation, so they do not need downstream provenance. The
// exact hash keeps this narrow marker from authorizing an unrelated client echo.
type openAICodexTurnStateTrustedInjection struct {
	accountID  int64
	model      string
	stateHash  [sha256.Size]byte
	generation uint64
}

func clearOpenAICodexTurnStateTrustedInjection(c *gin.Context) {
	if c != nil {
		c.Set(openAICodexTurnStateTrustedInjectionContextKey, nil)
	}
}

func (s *OpenAIGatewayService) markOpenAICodexTurnStateTrustedInjection(
	c *gin.Context,
	account *Account,
	model, state string,
	generation uint64,
) {
	if c == nil || account == nil || account.ID <= 0 || generation == 0 ||
		canonicalOpenAICodexTurnStateOriginModel(model) == "" || strings.TrimSpace(state) == "" {
		clearOpenAICodexTurnStateTrustedInjection(c)
		return
	}
	c.Set(openAICodexTurnStateTrustedInjectionContextKey, openAICodexTurnStateTrustedInjection{
		accountID:  account.ID,
		model:      canonicalOpenAICodexTurnStateOriginModel(model),
		stateHash:  sha256.Sum256([]byte(strings.TrimSpace(state))),
		generation: generation,
	})
}

func (s *OpenAIGatewayService) openAICodexTurnStateTrustedInjectionMatches(
	c *gin.Context,
	account *Account,
	model, state string,
) bool {
	if s == nil || c == nil || account == nil || s.codexTurnStateCollector == nil {
		return false
	}
	model = canonicalOpenAICodexTurnStateOriginModel(model)
	state = strings.TrimSpace(state)
	if model == "" || state == "" {
		return false
	}
	raw, ok := c.Get(openAICodexTurnStateTrustedInjectionContextKey)
	if !ok {
		return false
	}
	binding, ok := raw.(openAICodexTurnStateTrustedInjection)
	if !ok || binding.accountID != account.ID || binding.generation == 0 ||
		binding.stateHash != sha256.Sum256([]byte(state)) ||
		!codexTurnStateModelIdentitiesMatch(binding.model, model) {
		return false
	}
	return s.codexTurnStateCollector.IsCurrentKey(OpenAICodexTurnStateKey{
		AccountID:  account.ID,
		generation: binding.generation,
	})
}

func (s *OpenAIGatewayService) markCurrentOpenAICodexTurnStateTrustedInjection(
	c *gin.Context,
	account *Account,
	model, state string,
) bool {
	if s == nil || account == nil {
		clearOpenAICodexTurnStateTrustedInjection(c)
		return false
	}
	generation, current := s.codexTurnStateRequestGenerationCurrent(c, account.ID)
	if !current || generation == 0 {
		clearOpenAICodexTurnStateTrustedInjection(c)
		return false
	}
	s.markOpenAICodexTurnStateTrustedInjection(c, account, model, state, generation)
	if !s.openAICodexTurnStateTrustedInjectionMatches(c, account, model, state) {
		clearOpenAICodexTurnStateTrustedInjection(c)
		return false
	}
	return true
}

// BindOpenAICodexTurnStateExecutionScope resolves the immutable client
// execution identity before account-specific request rewriting. An explicitly
// bound empty value is significant: requests without a reliable identity must
// not fall back to content-derived hashes for provenance decisions.
func BindOpenAICodexTurnStateExecutionScope(c *gin.Context, body []byte) string {
	if c == nil {
		return ""
	}
	if scope, bound := boundOpenAICodexTurnStateExecutionScope(c); bound {
		return scope
	}
	scope, _ := resolveOpenAIWSExecutionScope(c, body, getAPIKeyIDFromContext(c))
	return bindOpenAICodexTurnStateExecutionScopeValue(c, scope)
}

func bindOpenAICodexTurnStateExecutionScopeValue(c *gin.Context, scope string) string {
	if c == nil {
		return ""
	}
	if existing, bound := boundOpenAICodexTurnStateExecutionScope(c); bound {
		return existing
	}
	scope = strings.TrimSpace(scope)
	c.Set(openAICodexTurnStateExecutionScopeContextKey, scope)
	return scope
}

func boundOpenAICodexTurnStateExecutionScope(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	value, exists := c.Get(openAICodexTurnStateExecutionScopeContextKey)
	if !exists {
		return "", false
	}
	scope, _ := value.(string)
	return strings.TrimSpace(scope), true
}

// openAICodexTurnStateSeed 返回溯源表基础键：API Key + 客户端原始会话标识。
// 客户端会话标识取自请求头（与指纹收敛的 thread 派生同源，见
// extractClientSessionID），确保同一下游会话的记录/守卫两侧使用同一键。
// 无会话标识时返回空串，表示不做跟踪（保持透传现状）。
func openAICodexTurnStateSeed(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if scope, bound := boundOpenAICodexTurnStateExecutionScope(c); bound {
		return scope
	}
	sessionID := extractClientSessionID(c.Request.Header)
	if sessionID == "" {
		return ""
	}
	return strconv.FormatInt(getAPIKeyIDFromContext(c), 10) + "\x00" + sessionID
}

func openAICodexTurnStateCommitModel(c *gin.Context, models ...string) string {
	if len(models) > 0 {
		if model := codexTurnStateModel(models[0]); model != "" {
			return model
		}
	}
	if c != nil {
		if raw, ok := c.Get(openAICodexTurnStateContextKey); ok {
			if binding, ok := raw.(openAICodexTurnStateRequestBinding); ok {
				return codexTurnStateModel(binding.model)
			}
		}
	}
	return ""
}

func (s *OpenAIGatewayService) openAICodexTurnStateResponseCommitAllowed(
	c *gin.Context,
	account *Account,
	state string,
	models ...string,
) bool {
	// Preserve the existing opaque-header behavior for integrations outside the
	// OAuth/Codex collector. Strict identity publication applies only to the
	// account type whose Turn-State lifecycle this gateway owns.
	if !codexTurnStateEligibleAccount(account) {
		return true
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	return s.canCommitOpenAICodexTurnState(ctx, c, account, openAICodexTurnStateCommitModel(c, models...), state)
}

// relayOpenAICodexTurnState 将上游响应中的 turn-state 显式写入下游响应头，
// 并记录铸造账号。必须在响应头提交点调用（WriteHeader 之前、且确认本次
// 上游响应就是将要写回客户端的响应之后）。上游无该头时主动清除 writer 上
// 可能残留的上一 failover attempt 的值——否则换号后旧账号的 blob 会粘到
// 新账号的响应上，这正是本文件要防止的跨账号矛盾。
func (s *OpenAIGatewayService) relayOpenAICodexTurnState(c *gin.Context, account *Account, upstream http.Header, model ...string) {
	if c == nil || c.Writer == nil {
		return
	}
	// Response headers are committed before the normal terminal observer runs
	// on non-streaming and compatibility paths. Validate here as well so an
	// expired, malformed, or wrong-shape state can never be relayed merely
	// because the body write happens before observation.
	s.sanitizeOpenAICodexTurnStateResponseHeader(account, upstream)
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		c.Writer.Header().Del(canonical)
		return
	}
	if !s.openAICodexTurnStateResponseCommitAllowed(c, account, state, model...) {
		deleteOpenAIHeaderEqualFold(upstream, openAICodexTurnStateHeader)
		c.Writer.Header().Del(canonical)
		return
	}
	c.Writer.Header().Set(canonical, state)
	s.noteOpenAICodexTurnStateProvenance(c, account, state, model...)
}

// stageOpenAICodexTurnState 将上游 turn-state 暂存到延迟提交的响应头集合
// （首输出守卫路径先缓存头、见到首个输出事件才提交）。此处**不**记录铸造
// 账号：该 attempt 仍可能在首输出超时后 failover，暂存头会被整体丢弃，
// 客户端从未收到该 blob。溯源必须在真正提交时记录，见
// noteStagedOpenAICodexTurnStateCommitted。
func stageOpenAICodexTurnState(dst *http.Header, upstream http.Header) {
	if dst == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		if *dst != nil {
			dst.Del(canonical)
		}
		return
	}
	if *dst == nil {
		*dst = http.Header{}
	}
	dst.Set(canonical, state)
}

// prepareOpenAICodexTurnStateForWrite places the staged state on the pending
// downstream headers without recording provenance. The caller must invoke
// commitOpenAICodexTurnStateAfterWrite only after the response body write has
// completed successfully.
func (s *OpenAIGatewayService) prepareOpenAICodexTurnStateForWrite(c *gin.Context, account *Account, upstream http.Header, model ...string) (http.Header, bool) {
	// This method is the last common point before an HTTP response commits its
	// headers. Keep the response-side admission check here even when a caller
	// already observed the body: compact JSON and Messages compatibility paths
	// can reach this method before their deferred collector observation.
	s.sanitizeOpenAICodexTurnStateResponseHeader(account, upstream)
	var staged http.Header
	stageOpenAICodexTurnState(&staged, upstream)
	if c == nil || c.Writer == nil {
		return staged, false
	}
	// Freeze the compact heartbeat before inspecting pending headers. If it has
	// already emitted a beat, HTTP headers are immutable and a newly learned
	// upstream state cannot have reached the client.
	headersAlreadyCommitted := StopOpenAICompactSSEKeepaliveCommitted(c) || c.Writer.Written()

	deleteOpenAIHeaderEqualFold(c.Writer.Header(), openAICodexTurnStateHeader)
	state := strings.TrimSpace(staged.Get(openAICodexTurnStateHeader))
	if state == "" {
		return staged, !headersAlreadyCommitted
	}
	if !s.openAICodexTurnStateResponseCommitAllowed(c, account, state, model...) {
		deleteOpenAIHeaderEqualFold(upstream, openAICodexTurnStateHeader)
		staged.Del(openAICodexTurnStateHeader)
		return staged, !headersAlreadyCommitted
	}
	if !headersAlreadyCommitted {
		c.Writer.Header().Set(openAICodexTurnStateHeader, state)
	}
	return staged, !headersAlreadyCommitted
}

func (s *OpenAIGatewayService) commitOpenAICodexTurnStateAfterWrite(
	c *gin.Context,
	account *Account,
	model string,
	upstream http.Header,
	staged http.Header,
	responseCompleted bool,
	downstreamWriteSucceeded bool,
	turnStateHeaderDelivered bool,
) {
	if !downstreamWriteSucceeded || c == nil {
		return
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return
	}
	if turnStateHeaderDelivered {
		s.noteStagedOpenAICodexTurnStateCommitted(c, account, staged, model)
	}
	if responseCompleted {
		s.observeCodexTurnStateResponse(c, account, model, upstream)
	}
}

// noteStagedOpenAICodexTurnStateCommitted 在暂存响应头真正写入下游时记录
// 铸造账号——只有此刻客户端才确定收到了该 blob，溯源表才与客户端持有的
// 值一致（否则被 failover 丢弃的 attempt 会污染溯源，导致后续误剥离）。
func (s *OpenAIGatewayService) noteStagedOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, staged http.Header, model ...string) {
	if staged == nil || strings.TrimSpace(staged.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	state := strings.TrimSpace(staged.Get(openAICodexTurnStateHeader))
	if !s.openAICodexTurnStateResponseCommitAllowed(c, account, state, model...) {
		staged.Del(openAICodexTurnStateHeader)
		if c != nil && c.Writer != nil && !c.Writer.Written() {
			c.Writer.Header().Del(openAICodexTurnStateHeader)
		}
		return
	}
	s.noteOpenAICodexTurnStateProvenance(c, account, state, model...)
}

func extractOpenAICodexTurnState(upstream http.Header) string {
	if upstream == nil {
		return ""
	}
	return strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
}

// sanitizeOpenAICodexTurnStateResponseHeader removes a response state that
// cannot be admitted by the local Codex collector policy. The check is scoped
// to collector-eligible OAuth/Codex accounts; other upstream integrations may
// use an opaque header format that this optional heuristic must not reject.
func (s *OpenAIGatewayService) sanitizeOpenAICodexTurnStateResponseHeader(account *Account, upstream http.Header) {
	if s == nil || upstream == nil || !s.codexTurnStateEligible(account) {
		return
	}
	state := strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
	if state == "" {
		return
	}
	if _, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), time.Now()); err != nil {
		deleteOpenAIHeaderEqualFold(upstream, openAICodexTurnStateHeader)
	}
}

// noteOpenAICodexTurnStateProvenance records the issuing account for the exact
// state delivered to a downstream session.
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenance(c *gin.Context, account *Account, state string, model ...string) {
	if s == nil || account == nil || account.ID <= 0 {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	state = strings.TrimSpace(state)
	if seed == "" || state == "" {
		return
	}
	if !s.openAICodexTurnStateResponseCommitAllowed(c, account, state, model...) {
		return
	}
	generation, current := s.codexTurnStateRequestGenerationCurrent(c, account.ID)
	if !current {
		if c != nil && c.Writer != nil && !c.Writer.Written() {
			c.Writer.Header().Del(openAICodexTurnStateHeader)
		}
		return
	}
	modelValue := ""
	if len(model) > 0 {
		modelValue = codexTurnStateModel(model[0])
	}
	if modelValue == "" && c != nil {
		if raw, ok := c.Get(openAICodexTurnStateContextKey); ok {
			if binding, ok := raw.(openAICodexTurnStateRequestBinding); ok {
				modelValue = codexTurnStateModel(binding.model)
			}
		}
	}
	origin := openAICodexTurnStateOrigin{
		accountID:  account.ID,
		model:      modelValue,
		stateHash:  sha256.Sum256([]byte(state)),
		expiresAt:  time.Now().Add(s.openAICodexTurnStateProvenanceTTL()),
		generation: generation,
	}
	s.clearOpenAICodexTurnStateInvalidationForModel(c, modelValue, state)
	if s.openAICodexTurnStateInvalidated(c, modelValue, state) {
		// A response carrying the exact state retired by a model mismatch must
		// not recreate its provenance binding. It remains fenced until a fresh
		// state is observed or the bounded tombstone expires.
		return
	}
	// Publish the model shard first. A concurrent guard for that model can then
	// find the correct origin even while the compatibility seed entry is being
	// updated by a sibling request.
	if modelKey := openAICodexTurnStateOriginModelKey(seed, modelValue); modelKey != "" {
		s.openaiCodexTurnStateOrigins.Store(modelKey, origin)
	}
	// Keep the historical seed entry for compatibility with older in-memory
	// records and callers that do not provide a model. Requests with a known
	// model always prefer the sharded entry above, so a concurrent sibling model
	// cannot make a valid state look foreign once its shard is published.
	s.openaiCodexTurnStateOrigins.Store(seed, origin)
	s.sweepOpenAICodexTurnStateOrigins()
}

// openAICodexTurnStateProvenanceTTL keeps account/model provenance alive for
// at least as long as a locally admissible Turn-State.  The WS sticky-session
// TTL is intentionally configurable and may be much shorter than the
// collector's one-hour state lifetime.  Letting provenance expire first would
// make a still-valid state look anonymous after failover, allowing it to cross
// an account boundary.  The small clock-skew allowance mirrors the collector's
// admission window.
func (s *OpenAIGatewayService) openAICodexTurnStateProvenanceTTL() time.Duration {
	stickyTTL := s.openAIWSSessionStickyTTL()
	policy := s.codexTurnStatePolicy().normalized()
	stateTTL := policy.TTL
	if policy.ClockSkew > 0 {
		stateTTL += policy.ClockSkew
	}
	if stateTTL > stickyTTL {
		return stateTTL
	}
	return stickyTTL
}

// clearOpenAICodexTurnStateProvenance removes the downstream session's
// account binding after a model-mismatched response. Keeping the old origin
// would make the next request look like a valid same-account echo even though
// the collector has already invalidated its state lineage.
func (s *OpenAIGatewayService) clearOpenAICodexTurnStateProvenance(c *gin.Context) {
	if s == nil {
		return
	}
	if seed := openAICodexTurnStateSeed(c); seed != "" {
		s.deleteOpenAICodexTurnStateOriginKeys(seed)
	}
}

// clearOpenAICodexTurnStateProvenanceForModel retires only the provenance
// shard that participated in a model-mismatch decision. A downstream
// execution scope can carry concurrent turns for more than one model; wiping
// every shard here would make a healthy sibling model lose its account/state
// binding and could allow an otherwise-known foreign value through the
// compatibility fallback entry.
func (s *OpenAIGatewayService) clearOpenAICodexTurnStateProvenanceForModel(c *gin.Context, model string) {
	if s == nil {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	if seed == "" {
		return
	}
	model = canonicalOpenAICodexTurnStateOriginModel(model)
	if model == "" {
		s.deleteOpenAICodexTurnStateOriginKeys(seed)
		return
	}
	if modelKey := openAICodexTurnStateOriginModelKey(seed, model); modelKey != "" {
		s.openaiCodexTurnStateOrigins.Delete(modelKey)
	}
	// The plain seed is a compatibility record for callers that predate
	// model-sharded provenance. Remove it only when it belongs to the model
	// being retired (or is malformed/legacy); preserve a sibling model's
	// record so its isolation boundary remains enforceable.
	if raw, ok := s.openaiCodexTurnStateOrigins.Load(seed); ok {
		origin, valid := raw.(openAICodexTurnStateOrigin)
		if !valid || origin.model == "" || codexTurnStateModelIdentitiesMatch(model, origin.model) {
			s.openaiCodexTurnStateOrigins.Delete(seed)
		}
	}
}

func openAICodexTurnStateInvalidationKey(seed, model string) string {
	seed = strings.TrimSpace(seed)
	model = canonicalOpenAICodexTurnStateOriginModel(model)
	if seed == "" || model == "" {
		return ""
	}
	return openAICodexTurnStateOriginModelKey(seed, model) + openAICodexTurnStateInvalidationSuffix
}

// invalidateOpenAICodexTurnStateForModel records a bounded tombstone for the
// exact state that an upstream response proved unusable. The tombstone is
// separate from provenance: deleting provenance alone would make the next
// client echo look like an untracked native value and send it again.
func (s *OpenAIGatewayService) invalidateOpenAICodexTurnStateForModel(c *gin.Context, model, state string) {
	if s == nil {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	key := openAICodexTurnStateInvalidationKey(seed, model)
	if key == "" {
		return
	}
	now := time.Now()
	trimmedState := strings.TrimSpace(state)
	marker := openAICodexTurnStateInvalidation{
		expiresAt: now.Add(s.openAICodexTurnStateProvenanceTTL()),
	}
	if trimmedState != "" {
		marker.stateHash = sha256.Sum256([]byte(trimmedState))
	}
	// When the response omitted the header, use the last known provenance hash
	// for this model. A zero hash means "invalidate any value until a fresh one
	// is observed", which is conservative for an untracked lineage.
	if trimmedState == "" {
		if origin, _, ok := s.loadOpenAICodexTurnStateOrigin(seed, model); ok {
			// loadOpenAICodexTurnStateOrigin keeps the historical seed fallback
			// for processes that predate model sharding. Do not borrow a sibling
			// model's hash through that fallback: a mismatch for model A must not
			// retire or whitelist the state lineage for model B.
			originModel := canonicalOpenAICodexTurnStateOriginModel(origin.model)
			requestedModel := canonicalOpenAICodexTurnStateOriginModel(model)
			if originModel == "" || originModel == requestedModel {
				marker.stateHash = origin.stateHash
				marker.generation = origin.generation
				if origin.expiresAt.After(marker.expiresAt) {
					marker.expiresAt = origin.expiresAt
				}
			}
		}
	}
	s.openaiCodexTurnStateInvalidations.Store(key, marker)
}

func (s *OpenAIGatewayService) openAICodexTurnStateInvalidated(c *gin.Context, model, state string) bool {
	if s == nil {
		return false
	}
	key := openAICodexTurnStateInvalidationKey(openAICodexTurnStateSeed(c), model)
	if key == "" {
		return false
	}
	raw, ok := s.openaiCodexTurnStateInvalidations.Load(key)
	if !ok {
		return false
	}
	marker, ok := raw.(openAICodexTurnStateInvalidation)
	if !ok || (!marker.expiresAt.IsZero() && time.Now().After(marker.expiresAt)) {
		s.openaiCodexTurnStateInvalidations.Delete(key)
		return false
	}
	if marker.stateHash == ([sha256.Size]byte{}) {
		return true
	}
	return marker.stateHash == sha256.Sum256([]byte(strings.TrimSpace(state)))
}

// clearOpenAICodexTurnStateInvalidationForModel removes a tombstone only when
// a different state has been observed. Re-observing the exact invalidated blob
// must not resurrect it.
func (s *OpenAIGatewayService) clearOpenAICodexTurnStateInvalidationForModel(c *gin.Context, model, state string) {
	if s == nil {
		return
	}
	key := openAICodexTurnStateInvalidationKey(openAICodexTurnStateSeed(c), model)
	if key == "" {
		return
	}
	raw, ok := s.openaiCodexTurnStateInvalidations.Load(key)
	if !ok {
		return
	}
	marker, ok := raw.(openAICodexTurnStateInvalidation)
	if !ok || (!marker.expiresAt.IsZero() && time.Now().After(marker.expiresAt)) {
		s.openaiCodexTurnStateInvalidations.Delete(key)
		return
	}
	if marker.stateHash == ([sha256.Size]byte{}) || marker.stateHash != sha256.Sum256([]byte(strings.TrimSpace(state))) {
		s.openaiCodexTurnStateInvalidations.Delete(key)
	}
}

func openAICodexTurnStateOriginModelKey(seed, model string) string {
	seed = strings.TrimSpace(seed)
	model = canonicalOpenAICodexTurnStateOriginModel(model)
	if seed == "" || model == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(model))
	return seed + openAICodexTurnStateOriginModelKeySeparator + hex.EncodeToString(digest[:8])
}

func canonicalOpenAICodexTurnStateOriginModel(model string) string {
	return codexTurnStateModelIdentity(model)
}

func openAICodexTurnStateOriginKeyBelongsToSeed(key, seed string) bool {
	return key == seed || strings.HasPrefix(key, seed+openAICodexTurnStateOriginModelKeySeparator)
}

func (s *OpenAIGatewayService) deleteOpenAICodexTurnStateOriginKeys(seed string) {
	if s == nil || strings.TrimSpace(seed) == "" {
		return
	}
	s.openaiCodexTurnStateOrigins.Range(func(key, _ any) bool {
		keyString, ok := key.(string)
		if ok && openAICodexTurnStateOriginKeyBelongsToSeed(keyString, seed) {
			s.openaiCodexTurnStateOrigins.Delete(key)
		}
		return true
	})
}

func (s *OpenAIGatewayService) loadOpenAICodexTurnStateOrigin(seed, model string) (openAICodexTurnStateOrigin, string, bool) {
	if s == nil || strings.TrimSpace(seed) == "" {
		return openAICodexTurnStateOrigin{}, "", false
	}
	keys := make([]string, 0, 2)
	if modelKey := openAICodexTurnStateOriginModelKey(seed, model); modelKey != "" {
		keys = append(keys, modelKey)
	}
	keys = append(keys, seed)
	now := time.Now()
	for _, key := range keys {
		raw, ok := s.openaiCodexTurnStateOrigins.Load(key)
		if !ok {
			continue
		}
		origin, ok := raw.(openAICodexTurnStateOrigin)
		if !ok {
			s.openaiCodexTurnStateOrigins.Delete(key)
			continue
		}
		if !origin.expiresAt.IsZero() && now.After(origin.expiresAt) {
			s.openaiCodexTurnStateOrigins.Delete(key)
			continue
		}
		return origin, key, true
	}
	return openAICodexTurnStateOrigin{}, "", false
}

// openAICodexTurnStateEchoExpected admits an outbound state only when it is a
// server-owned trusted injection or its downstream provenance proves the exact
// account, final model, state hash, and live collector generation. Unknown,
// expired, and legacy-unscoped provenance fail closed.
func (s *OpenAIGatewayService) openAICodexTurnStateEchoExpected(c *gin.Context, account *Account, model, state string) bool {
	if s == nil || account == nil || account.ID <= 0 {
		return false
	}
	model = canonicalOpenAICodexTurnStateOriginModel(model)
	state = strings.TrimSpace(state)
	if model == "" || state == "" {
		return false
	}
	if s.openAICodexTurnStateTrustedInjectionMatches(c, account, model, state) {
		return true
	}
	seed := openAICodexTurnStateSeed(c)
	if seed == "" {
		return false
	}
	origin, storedOriginKey, ok := s.loadOpenAICodexTurnStateOrigin(seed, model)
	if !ok || origin.generation == 0 || s.codexTurnStateCollector == nil {
		return false
	}
	if !s.codexTurnStateCollector.IsCurrentKey(OpenAICodexTurnStateKey{
		AccountID:  origin.accountID,
		generation: origin.generation,
	}) {
		// Lifecycle invalidation retires every model shard for this execution
		// scope. Keeping the compatibility seed would let a later echo appear
		// valid after credentials or scheduling identity changed.
		s.deleteOpenAICodexTurnStateOriginKeys(seed)
		return false
	}
	modelMismatch := origin.model == "" || !codexTurnStateModelIdentitiesMatch(model, origin.model)
	stateHash := sha256.Sum256([]byte(state))
	if origin.accountID == account.ID && !modelMismatch && origin.stateHash == stateHash {
		return true
	}
	if modelMismatch {
		// A missing target shard may fall back to the compatibility seed for a
		// concurrently active sibling model. Preserve that sibling unless this is
		// the exact legacy record selected for the echoed value.
		if storedOriginKey != seed || origin.stateHash == stateHash {
			s.openaiCodexTurnStateOrigins.Delete(storedOriginKey)
		}
	}
	return false
}

// clearOpenAIWSSessionTurnStateForModel removes a model-scoped WS session
// binding after an upstream response proves that the lineage is invalid. The
// default in-process store supports exact account/model deletion; narrow
// alternate stores fall back to deleting the whole execution scope rather than
// allowing a known-bad state to be reused.
func (s *OpenAIGatewayService) clearOpenAIWSSessionTurnStateForModel(c *gin.Context, account *Account, model string) {
	if s == nil || account == nil {
		return
	}
	stateStore := s.getOpenAIWSStateStore()
	if stateStore == nil {
		return
	}
	scope, _ := boundOpenAICodexTurnStateExecutionScope(c)
	if scope == "" {
		scope = openAICodexTurnStateSeed(c)
	}
	if strings.TrimSpace(scope) == "" {
		return
	}
	groupID := getOpenAIGroupIDFromContext(c)
	if deleter, ok := stateStore.(openAIWSSessionTurnStateModelDeleter); ok {
		deleter.DeleteSessionTurnStateForModel(groupID, account.ID, scope, model)
		return
	}
	stateStore.DeleteSessionTurnState(groupID, scope)
}

// guardOpenAICodexTurnStateEcho is the final outbound admission boundary for a
// client echo. OAuth/Codex state is retained only with exact local provenance;
// the envelope alone cannot authenticate its account or model. Server-owned
// Messages/WS injections carry a narrow request-scoped trusted binding.
func (s *OpenAIGatewayService) guardOpenAICodexTurnStateEcho(c *gin.Context, account *Account, h http.Header, model ...string) {
	if s == nil || h == nil || account == nil {
		return
	}
	state := strings.TrimSpace(h.Get(openAICodexTurnStateHeader))
	if state == "" {
		return
	}
	// Strip malformed or expired values before any account/seed fast path so
	// API-key and other non-collector routes cannot forward an unexpected state.
	if _, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), time.Now()); err != nil {
		h.Del(openAICodexTurnStateHeader)
		return
	}
	// Strict provenance applies to OAuth/Codex credentials. Other integrations
	// may legitimately use the same opaque header name with their own lifecycle;
	// they still passed the envelope/lifetime guard above.
	if !codexTurnStateEligibleAccount(account) {
		return
	}
	modelValue := ""
	if len(model) > 0 {
		modelValue = codexTurnStateModel(model[0])
	}
	if modelValue == "" && c != nil {
		if raw, ok := c.Get(openAICodexTurnStateContextKey); ok {
			if binding, ok := raw.(openAICodexTurnStateRequestBinding); ok {
				modelValue = codexTurnStateModel(binding.model)
			}
		}
	}
	// A model-mismatched response can retire the collector/provenance while the
	// client still holds the old header. Keep that exact value fenced until it
	// expires or a different state is observed; otherwise it would look like an
	// anonymous native state and be sent upstream again.
	if s.openAICodexTurnStateInvalidated(c, modelValue, state) {
		h.Del(openAICodexTurnStateHeader)
		return
	}
	if !s.openAICodexTurnStateEchoExpected(c, account, modelValue, state) {
		h.Del(openAICodexTurnStateHeader)
	}
}

// ClearOpenAIWSTurnStateForAccountSwitch removes state that was valid only for
// the failed account before the handler selects another account. Callers must
// invoke this only after deciding to switch accounts; same-account retries and
// connection-local reconnects intentionally retain their state.
func (s *OpenAIGatewayService) ClearOpenAIWSTurnStateForAccountSwitch(c *gin.Context, stickySessionHash string) {
	if s == nil {
		return
	}
	// Account failover starts a fresh forwarding attempt. A conflict observed
	// on the failed account must not suppress state observation for its
	// replacement account.
	clearOpenAIWSTurnStateModelMismatch(c)
	if c != nil && c.Request != nil {
		c.Request.Header.Del(openAICodexTurnStateHeader)
	}
	if c != nil && c.Writer != nil && !c.Writer.Written() {
		// A failed WS attempt may have staged its handshake state on the HTTP
		// response. Do not let it survive when the replacement account emits none.
		c.Writer.Header().Del(openAICodexTurnStateHeader)
	}

	stateStore := s.getOpenAIWSStateStore()
	if stateStore == nil {
		return
	}
	activeScope, _ := boundOpenAICodexTurnStateExecutionScope(c)
	if activeScope == "" {
		activeScope = strings.TrimSpace(stickySessionHash)
	}
	if activeScope != "" {
		groupID := getOpenAIGroupIDFromContext(c)
		stateStore.DeleteSessionTurnState(groupID, activeScope)
		stateStore.DeleteSessionConn(groupID, activeScope)
	}
}

// sweepOpenAICodexTurnStateOrigins 机会式清扫过期溯源记录：每 256 次写入
// 全量遍历一轮，防止仅靠读侧惰性删除导致的慢泄漏（会话键无上界）。
func (s *OpenAIGatewayService) sweepOpenAICodexTurnStateOrigins() {
	if s.openaiCodexTurnStateWrites.Add(1)%256 != 0 {
		return
	}
	now := time.Now()
	s.openaiCodexTurnStateOrigins.Range(func(key, value any) bool {
		origin, ok := value.(openAICodexTurnStateOrigin)
		if !ok || (!origin.expiresAt.IsZero() && now.After(origin.expiresAt)) {
			s.openaiCodexTurnStateOrigins.Delete(key)
		}
		return true
	})
	s.openaiCodexTurnStateInvalidations.Range(func(key, value any) bool {
		marker, ok := value.(openAICodexTurnStateInvalidation)
		if !ok || (!marker.expiresAt.IsZero() && now.After(marker.expiresAt)) {
			s.openaiCodexTurnStateInvalidations.Delete(key)
		}
		return true
	})
}
