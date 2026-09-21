package service

import (
	"crypto/sha256"
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

// turn-state blob 是上游在"出站身份"（含 #5553 指纹收敛改写后的
// installation/session/thread 标识）下铸造的，同账号回放自洽；跨账号回放
// （failover 换号后客户端仍回带旧账号的 blob）是代理链独有、真实 Codex
// 永远不会产生的矛盾信号。溯源表记录每个下游会话最近一次铸造该 blob 的
// 账号，出站守卫据此剥离已知异账号的回带值。
type openAICodexTurnStateOrigin struct {
	accountID int64
	stateHash [sha256.Size]byte
	expiresAt time.Time
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

// relayOpenAICodexTurnState 将上游响应中的 turn-state 显式写入下游响应头，
// 并记录铸造账号。必须在响应头提交点调用（WriteHeader 之前、且确认本次
// 上游响应就是将要写回客户端的响应之后）。上游无该头时主动清除 writer 上
// 可能残留的上一 failover attempt 的值——否则换号后旧账号的 blob 会粘到
// 新账号的响应上，这正是本文件要防止的跨账号矛盾。
func (s *OpenAIGatewayService) relayOpenAICodexTurnState(c *gin.Context, account *Account, upstream http.Header) {
	if c == nil || c.Writer == nil {
		return
	}
	canonical := http.CanonicalHeaderKey(openAICodexTurnStateHeader)
	state := extractOpenAICodexTurnState(upstream)
	if state == "" {
		c.Writer.Header().Del(canonical)
		return
	}
	c.Writer.Header().Set(canonical, state)
	s.noteOpenAICodexTurnStateProvenance(c, account, state)
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
func (s *OpenAIGatewayService) noteStagedOpenAICodexTurnStateCommitted(c *gin.Context, account *Account, staged http.Header) {
	if staged == nil || strings.TrimSpace(staged.Get(openAICodexTurnStateHeader)) == "" {
		return
	}
	s.noteOpenAICodexTurnStateProvenance(c, account, staged.Get(openAICodexTurnStateHeader))
}

func extractOpenAICodexTurnState(upstream http.Header) string {
	if upstream == nil {
		return ""
	}
	return strings.TrimSpace(upstream.Get(openAICodexTurnStateHeader))
}

// noteOpenAICodexTurnStateProvenance records the issuing account for the exact
// state delivered to a downstream session.
func (s *OpenAIGatewayService) noteOpenAICodexTurnStateProvenance(c *gin.Context, account *Account, state string) {
	if s == nil || account == nil || account.ID <= 0 {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	state = strings.TrimSpace(state)
	if seed == "" || state == "" {
		return
	}
	s.openaiCodexTurnStateOrigins.Store(seed, openAICodexTurnStateOrigin{
		accountID: account.ID,
		stateHash: sha256.Sum256([]byte(state)),
		expiresAt: time.Now().Add(s.openAIWSSessionStickyTTL()),
	})
	s.sweepOpenAICodexTurnStateOrigins()
}

// guardOpenAICodexTurnStateEcho 出站守卫：客户端回带的 turn-state 若已知由
// 其他账号铸造则剥离，同账号或无溯源记录时保持原样。只剥离、不注入——
// /responses 路径的客户端是真实 Codex，会按自身回合语义自行回带；服务端
// 注入是 Claude 兼容桥（无法回带的客户端）的专属行为。
func (s *OpenAIGatewayService) guardOpenAICodexTurnStateEcho(c *gin.Context, account *Account, h http.Header) {
	if s == nil || h == nil || account == nil {
		return
	}
	state := strings.TrimSpace(h.Get(openAICodexTurnStateHeader))
	if state == "" {
		return
	}
	seed := openAICodexTurnStateSeed(c)
	if seed == "" {
		return
	}
	raw, ok := s.openaiCodexTurnStateOrigins.Load(seed)
	if !ok {
		return
	}
	origin, ok := raw.(openAICodexTurnStateOrigin)
	if !ok {
		s.openaiCodexTurnStateOrigins.Delete(seed)
		return
	}
	if !origin.expiresAt.IsZero() && time.Now().After(origin.expiresAt) {
		s.openaiCodexTurnStateOrigins.Delete(seed)
		return
	}
	if origin.accountID != account.ID || origin.stateHash != sha256.Sum256([]byte(state)) {
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
}
