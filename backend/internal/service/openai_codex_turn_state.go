package service

import (
	"context"
	"log/slog"
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

// CodexTurnStateNativeProvenanceExtraKey stores only SHA-256 digests and their
// account/model ownership. It lives under the managed state prefix so account
// export/import stripping cannot move provenance to another account.
const CodexTurnStateNativeProvenanceExtraKey = CodexTurnStateModelExtraPrefix + "!native_provenance_v1"

const CodexTurnStateNativeProvenanceLimit = 64

const codexTurnStateNativeProvenanceRecheckInterval = 5 * time.Second

type CodexTurnStateNativeProvenance struct {
	AccountID   int64  `json:"-"`
	Model       string `json:"model"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

// CodexTurnStateProvenanceRepository is deliberately narrower than the account
// repository. Production persists digest ownership in accounts.extra; tests or
// installations without this optional capability retain the in-process guard.
type CodexTurnStateProvenanceRepository interface {
	RecordCodexTurnStateProvenance(ctx context.Context, accountID int64, model, digest string, expiresAtMS int64) error
	FindCodexTurnStateProvenance(ctx context.Context, digest string) ([]CodexTurnStateNativeProvenance, error)
}

// turn-state blob 是上游在"出站身份"（含 #5553 指纹收敛改写后的
// installation/session/thread 标识）下铸造的，同账号回放自洽；跨账号回放
// （failover 换号后客户端仍回带旧账号的 blob）是代理链独有、真实 Codex
// 永远不会产生的矛盾信号。溯源表记录每个下游会话最近一次铸造该 blob 的
// 账号，出站守卫据此剥离已知异账号的回带值。
type openAICodexTurnStateOrigin struct {
	accountID int64
	model     string
	digest    string
	conflict  bool
	expiresAt time.Time
	recheckAt time.Time
}

type openAICodexTurnStateDigestKey string

// openAICodexTurnStateSeed 返回溯源表键：API Key + 客户端原始会话标识。
// 客户端会话标识取自请求头（与指纹收敛的 thread 派生同源，见
// extractClientSessionID），确保同一下游会话的记录/守卫两侧使用同一键。
// 无会话标识时返回空串，表示不做跟踪（保持透传现状）。
func openAICodexTurnStateSeed(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	sessionID := extractClientSessionID(c.Request.Header)
	if sessionID == "" {
		return ""
	}
	return strconv.FormatInt(getAPIKeyIDFromContext(c), 10) + "\x00" + sessionID
}

// relayOpenAICodexTurnState 将上游响应中的 turn-state 显式写入下游响应头，
// 并记录铸造账号。必须在响应头提交点调用（WriteHeader 之前、且确认本次
// 上游响应就是将要写回客户端的响应之后）。上游无该头时主动清除 writer 上
// 可能残留的上一 failover attempt 的值——否则换号后旧账号的 blob 会粘到
// 新账号的响应上，这正是本文件要防止的跨账号矛盾。
func (s *OpenAIGatewayService) relayOpenAICodexTurnState(c *gin.Context, account *Account, upstream http.Header, requests ...*http.Request) {
	if c == nil || c.Writer == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		c.Writer.Header().Del(canonical)
		s.clearPendingCodexTurnStateObservation(c)
		return
	}
	c.Writer.Header().Set(canonical, state)
	// The response header is forwarded immediately for native client behavior,
	// but provenance and promotion both wait for raw model evidence.
	s.stagePendingCodexTurnStateObservation(c, account, state, requests...)
	s.commitPendingCodexTurnStateObservation(c, false)
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

// noteStagedOpenAICodexTurnStateCommitted 在暂存响应头真正写入下游时记录
// 铸造账号——只有此刻客户端才确定收到了该 blob，溯源表才与客户端持有的
// 值一致（否则被 failover 丢弃的 attempt 会污染溯源，导致后续误剥离）。
func (s *OpenAIGatewayService) noteStagedOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, staged http.Header, requests ...*http.Request) {
	if staged == nil || strings.TrimSpace(staged.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	state := extractOpenAICodexTurnState(staged)
	s.stagePendingCodexTurnStateObservation(c, account, state, requests...)
	s.commitPendingCodexTurnStateObservation(c, false)
}

func extractOpenAICodexTurnState(upstream http.Header) string {
	if upstream == nil {
		return ""
	}
	return strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
}

// noteOpenAICodexTurnStateProvenance records a state digest under the issuing
// account and request-model family scope. The opaque state itself is never added
// to provenance storage.
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenance(c *gin.Context, account *Account, state string, requests ...*http.Request) {
	model := ""
	if len(requests) > 0 && requests[0] != nil {
		model, _ = requests[0].Context().Value(codexTurnStateModelContextKey{}).(string)
	}
	if strings.TrimSpace(model) == "" && c != nil && c.Request != nil {
		model, _ = c.Request.Context().Value(codexTurnStateModelContextKey{}).(string)
	}
	s.noteOpenAICodexTurnStateProvenanceForModel(c, account, state, model)
}

func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenanceForModel(c *gin.Context, account *Account, state, model string) {
	state = strings.TrimSpace(state)
	if s == nil || account == nil || account.ID <= 0 || state == "" {
		return
	}
	model = strings.TrimSpace(model)
	digest := codexTurnStateCanonicalDigest(state)
	now := time.Now()
	origin := openAICodexTurnStateOrigin{
		accountID: account.ID,
		model:     model,
		digest:    digest,
		expiresAt: now.Add(codexTurnStateTTL),
	}
	digestKey := openAICodexTurnStateDigestKey(digest)
	repo, persistent := s.accountRepo.(CodexTurnStateProvenanceRepository)
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = context.WithoutCancel(c.Request.Context())
	}
	if persistent && model != "" && !s.codexTurnStateNativeScopeAllowed(ctx, account, model, state) {
		origin.conflict = true
	} else if previous, ok := s.openaiCodexTurnStateOrigins.Load(digestKey); ok {
		if known, valid := previous.(openAICodexTurnStateOrigin); valid && now.Before(known.expiresAt) &&
			(known.accountID != origin.accountID || !codexTurnStateResponseModelsMatch(known.model, origin.model)) {
			origin.conflict = true
		}
	}
	s.openaiCodexTurnStateOrigins.Store(digestKey, origin)
	if seed := openAICodexTurnStateSeed(c); seed != "" {
		s.openaiCodexTurnStateOrigins.Store(seed, origin)
	}
	s.sweepOpenAICodexTurnStateOrigins()

	if !persistent || model == "" || origin.conflict {
		return
	}
	dbCtx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	if err := repo.RecordCodexTurnStateProvenance(dbCtx, account.ID, model, digest, origin.expiresAt.UnixMilli()); err != nil {
		slog.Warn("openai_codex_turn_state_provenance_persist_failed", "account_id", account.ID, "code", "persistence_failed")
		return
	}
	origin.recheckAt = now.Add(codexTurnStateNativeProvenanceRecheckInterval)
	s.openaiCodexTurnStateOrigins.Store(digestKey, origin)
	if seed := openAICodexTurnStateSeed(c); seed != "" {
		s.openaiCodexTurnStateOrigins.Store(seed, origin)
	}
}

// guardOpenAICodexTurnStateEcho 出站守卫：客户端回带的 turn-state 若已知由
// 其他账号铸造则剥离，同账号或无溯源记录时保持原样。只剥离、不注入——
// /responses 客户端按自身回合语义回带，兼容桥可补入原生续链状态。
// GPT 系统设置中的显式全局覆盖在此守卫之后独立应用，不改变溯源记录。
func (s *OpenAIGatewayService) guardOpenAICodexTurnStateEcho(c *gin.Context, account *Account, h http.Header, models ...string) {
	if s == nil || h == nil || account == nil || c == nil || c.Request == nil {
		return
	}
	state := strings.TrimSpace(h.Get(openAICodexTurnStateHeader))
	if state == "" {
		return
	}
	model := s.codexTurnStateModel(c.Request.Context(), models...)
	digest := codexTurnStateCanonicalDigest(state)
	seed := openAICodexTurnStateSeed(c)
	if seed != "" {
		if raw, ok := s.openaiCodexTurnStateOrigins.Load(seed); ok {
			origin, valid := raw.(openAICodexTurnStateOrigin)
			if !valid || (!origin.expiresAt.IsZero() && time.Now().After(origin.expiresAt)) {
				s.openaiCodexTurnStateOrigins.Delete(seed)
			} else if origin.digest == digest && !codexTurnStateOriginMatches(origin, account.ID, model) {
				h.Del(openAICodexTurnStateHeader)
				return
			}
		}
	}
	if !s.codexTurnStateNativeScopeAllowed(c.Request.Context(), account, model, state) {
		h.Del(openAICodexTurnStateHeader)
	}
}

func codexTurnStateOriginMatches(origin openAICodexTurnStateOrigin, accountID int64, model string) bool {
	if origin.conflict || origin.accountID != accountID {
		return false
	}
	originModel := strings.TrimSpace(origin.model)
	model = strings.TrimSpace(model)
	// A missing model is legacy/unknown provenance, not proof of a cross-model
	// conflict. Only a pair of known, different model identities is rejected.
	return originModel == "" || model == "" || codexTurnStateResponseModelsMatch(originModel, model)
}

// codexTurnStateNativeScopeAllowed rejects only proven ownership conflicts.
// Missing provenance and lookup failures remain compatible with native clients
// that obtained a state before deployment, restart, or from an official path
// outside this process.
func (s *OpenAIGatewayService) codexTurnStateNativeScopeAllowed(ctx context.Context, account *Account, model, state string) bool {
	if s == nil || account == nil || account.ID <= 0 || strings.TrimSpace(state) == "" {
		return true
	}
	model = strings.TrimSpace(model)
	digest := codexTurnStateCanonicalDigest(state)
	now := time.Now()
	repo, persistent := s.accountRepo.(CodexTurnStateProvenanceRepository)
	localKnown := false
	localAllowed := true
	var localOrigin openAICodexTurnStateOrigin
	if raw, ok := s.openaiCodexTurnStateOrigins.Load(openAICodexTurnStateDigestKey(digest)); ok {
		origin, valid := raw.(openAICodexTurnStateOrigin)
		if !valid || (!origin.expiresAt.IsZero() && now.After(origin.expiresAt)) {
			s.openaiCodexTurnStateOrigins.Delete(openAICodexTurnStateDigestKey(digest))
		} else {
			localOrigin = origin
			localKnown = true
			localAllowed = codexTurnStateOriginMatches(origin, account.ID, model)
			if !localAllowed {
				return false
			}
			if !persistent || (!origin.recheckAt.IsZero() && now.Before(origin.recheckAt)) {
				return true
			}
		}
	}

	if !persistent {
		return !localKnown || localAllowed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), gatewayForwardingDBTimeout)
	defer cancel()
	origins, err := repo.FindCodexTurnStateProvenance(dbCtx, digest)
	if err != nil {
		return !localKnown || localAllowed
	}
	known := false
	allowed := true
	var cacheOrigin openAICodexTurnStateOrigin
	for _, persisted := range origins {
		if persisted.ExpiresAtMS <= now.UnixMilli() {
			continue
		}
		origin := openAICodexTurnStateOrigin{
			accountID: persisted.AccountID,
			model:     strings.TrimSpace(persisted.Model),
			digest:    digest,
			expiresAt: time.UnixMilli(persisted.ExpiresAtMS),
			recheckAt: now.Add(codexTurnStateNativeProvenanceRecheckInterval),
		}
		if !known {
			cacheOrigin = origin
		} else if cacheOrigin.accountID != origin.accountID || !codexTurnStateResponseModelsMatch(cacheOrigin.model, origin.model) {
			cacheOrigin.conflict = true
		}
		known = true
		if !codexTurnStateOriginMatches(origin, account.ID, model) {
			allowed = false
		}
	}
	if known {
		s.openaiCodexTurnStateOrigins.Store(openAICodexTurnStateDigestKey(digest), cacheOrigin)
		return allowed
	}
	if localKnown {
		localOrigin.recheckAt = now.Add(codexTurnStateNativeProvenanceRecheckInterval)
		s.openaiCodexTurnStateOrigins.Store(openAICodexTurnStateDigestKey(digest), localOrigin)
	}
	return !localKnown || localAllowed
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
}
