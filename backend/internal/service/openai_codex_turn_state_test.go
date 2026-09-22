package service

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/util/responseheaders"
)

func newTurnStateTestContext(t *testing.T, apiKeyID int64, sessionID string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	if sessionID != "" {
		c.Request.Header.Set("session_id", sessionID)
	}
	if apiKeyID > 0 {
		c.Set("api_key", &APIKey{ID: apiKeyID})
	}
	return c, rec
}

func TestOpenAICodexTurnStateSeed(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 7, "sess-1")
	require.Equal(t, "7\x00sess-1", openAICodexTurnStateSeed(c))

	// 连字符形式优先（Codex CLI 标准头）
	c.Request.Header.Set("session-id", "sess-hyphen")
	require.Equal(t, "7\x00sess-hyphen", openAICodexTurnStateSeed(c))

	// 无会话标识 → 不跟踪
	cNoSession, _ := newTurnStateTestContext(t, 7, "")
	require.Empty(t, openAICodexTurnStateSeed(cNoSession))

	require.Empty(t, openAICodexTurnStateSeed(nil))
}

func TestRelayOpenAICodexTurnState_SetsHeaderAndRecordsProvenance(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42}
	c, _ := newTurnStateTestContext(t, 7, "sess-relay")

	upstream := http.Header{}
	upstream.Set("x-codex-turn-state", "blob-A")
	svc.relayOpenAICodexTurnState(c, account, upstream)

	require.Equal(t, "blob-A", c.Writer.Header().Get("X-Codex-Turn-State"))

	raw, ok := svc.openaiCodexTurnStateOrigins.Load("7\x00sess-relay")
	require.True(t, ok)
	origin, ok := raw.(openAICodexTurnStateOrigin)
	require.True(t, ok)
	require.Equal(t, int64(42), origin.accountID)
	require.Equal(t, sha256.Sum256([]byte("blob-A")), origin.stateHash)
	require.True(t, origin.expiresAt.After(time.Now()))
}

func TestOpenAICodexTurnStateProvenanceTTLOutlivesShortStickyTTL(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.StickySessionTTLSeconds = 180
	svc := &OpenAIGatewayService{cfg: cfg}
	svc.codexTurnStateCollector = NewOpenAICodexTurnStateCollector(collectorTestPolicy())

	// The WS sticky session may be configured for three minutes, while the
	// default collector accepts a state for one hour (plus clock skew).  The
	// account binding must not disappear at the shorter boundary.
	require.Equal(t, time.Hour+openAICodexTurnStateCollectorDefaultClockSkew, svc.openAICodexTurnStateProvenanceTTL())

	c, _ := newTurnStateTestContext(t, 7, "sess-short-sticky")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 199)
	svc.noteOpenAICodexTurnStateProvenance(c, codexTurnStateGatewayTestAccount(42), state, "gpt-5.5")

	seed := openAICodexTurnStateSeed(c)
	origin, _, ok := svc.loadOpenAICodexTurnStateOrigin(seed, "gpt-5.5")
	require.True(t, ok)
	require.True(t, origin.expiresAt.After(time.Now().Add(time.Hour)))

	// A failover after the three-minute sticky window must still reject the
	// state when it is presented to another account.
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, state)
	svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateGatewayTestAccount(43), h, "gpt-5.5")
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
}

func TestRelayOpenAICodexTurnState_ClearsStaleValueWhenUpstreamAbsent(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 7, "sess-stale")
	// 模拟上一 failover attempt 残留的值
	c.Writer.Header().Set("X-Codex-Turn-State", "blob-old")

	svc.relayOpenAICodexTurnState(c, &Account{ID: 43}, http.Header{})

	require.Empty(t, c.Writer.Header().Get("X-Codex-Turn-State"))
	_, ok := svc.openaiCodexTurnStateOrigins.Load("7\x00sess-stale")
	require.False(t, ok)
}

func TestStageOpenAICodexTurnState_StagedHeaders(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 9, "sess-staged")

	// nil 集合 + 上游有值 → 创建集合并写入，但此刻还不记录溯源
	var staged http.Header
	upstream := http.Header{}
	upstream.Set("x-codex-turn-state", "blob-B")
	stageOpenAICodexTurnState(&staged, upstream)
	require.NotNil(t, staged)
	require.Equal(t, "blob-B", staged.Get("X-Codex-Turn-State"))
	_, noted := svc.openaiCodexTurnStateOrigins.Load("9\x00sess-staged")
	require.False(t, noted, "暂存阶段不得记录溯源：该 attempt 仍可能 failover 丢弃")

	// 真正提交时才记录
	svc.noteStagedOpenAICodexTurnStateCommitted(c, &Account{ID: 44}, staged)
	raw, ok := svc.openaiCodexTurnStateOrigins.Load("9\x00sess-staged")
	require.True(t, ok)
	origin, ok := raw.(openAICodexTurnStateOrigin)
	require.True(t, ok)
	require.Equal(t, int64(44), origin.accountID)

	// 上游无值 → 清除已暂存的值；nil 集合保持 nil
	stageOpenAICodexTurnState(&staged, http.Header{})
	require.Empty(t, staged.Get("X-Codex-Turn-State"))
	var nilStaged http.Header
	stageOpenAICodexTurnState(&nilStaged, http.Header{})
	require.Nil(t, nilStaged)
}

// 首输出超时导致 attempt 被丢弃时，溯源不得被该 attempt 污染——否则后续
// 请求会把客户端持有的合法 blob 误判成跨账号回带而剥离。
func TestStagedTurnState_AbandonedAttemptDoesNotPoisonProvenance(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	c, _ := newTurnStateTestContext(t, 11, "sess-abandoned")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 180)

	// 账号 A 的 attempt 暂存了 blob，但从未提交（首输出超时 → failover）
	var staged http.Header
	upstreamA := http.Header{}
	upstreamA.Set("x-codex-turn-state", state)
	stageOpenAICodexTurnState(&staged, upstreamA)

	// 账号 B 接手并真正提交
	account := codexTurnStateGatewayTestAccount(52)
	svc.relayOpenAICodexTurnState(c, account, upstreamA, "gpt-5.5")

	// 客户端回带的 blob 来自 B，出站到 B 时不得被剥离
	h := http.Header{}
	h.Set("x-codex-turn-state", state)
	svc.guardOpenAICodexTurnStateEcho(c, account, h, "gpt-5.5")
	require.Equal(t, state, h.Get("x-codex-turn-state"))

	raw, ok := svc.openaiCodexTurnStateOrigins.Load("11\x00sess-abandoned")
	require.True(t, ok)
	origin, ok := raw.(openAICodexTurnStateOrigin)
	require.True(t, ok)
	require.Equal(t, int64(52), origin.accountID)
}

func TestNoteStagedOpenAICodexTurnStateCommitted_NoopWithoutState(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 12, "sess-nostate")

	svc.noteStagedOpenAICodexTurnStateCommitted(c, &Account{ID: 60}, nil)
	svc.noteStagedOpenAICodexTurnStateCommitted(c, &Account{ID: 60}, http.Header{"X-Request-Id": []string{"rid"}})

	_, ok := svc.openaiCodexTurnStateOrigins.Load("12\x00sess-nostate")
	require.False(t, ok)
}

func TestGuardOpenAICodexTurnStateEcho(t *testing.T) {
	newOutbound := func(state string) http.Header {
		h := http.Header{}
		if state != "" {
			h.Set("x-codex-turn-state", state)
		}
		return h
	}

	t.Run("same_account_keeps_echo", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c, _ := newTurnStateTestContext(t, 7, "sess-g1")
		account := codexTurnStateGatewayTestAccount(42)
		state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 181)
		upstream := http.Header{}
		upstream.Set("x-codex-turn-state", state)
		svc.relayOpenAICodexTurnState(c, account, upstream, "gpt-5.5")

		h := newOutbound(state)
		svc.guardOpenAICodexTurnStateEcho(c, account, h, "gpt-5.5")
		require.Equal(t, state, h.Get("x-codex-turn-state"))
	})

	t.Run("foreign_account_strips_echo", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c, _ := newTurnStateTestContext(t, 7, "sess-g2")
		state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 182)
		upstream := http.Header{}
		upstream.Set("x-codex-turn-state", state)
		svc.relayOpenAICodexTurnState(c, codexTurnStateGatewayTestAccount(42), upstream, "gpt-5.5")

		// failover 换到账号 43：blob 由 42 铸造，必须剥离
		h := newOutbound(state)
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateGatewayTestAccount(43), h, "gpt-5.5")
		require.Empty(t, h.Get("x-codex-turn-state"))
	})

	t.Run("no_provenance_fails_closed", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c, _ := newTurnStateTestContext(t, 7, "sess-g3")
		state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 183)
		h := newOutbound(state)
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateGatewayTestAccount(43), h, "gpt-5.5")
		require.Empty(t, h.Get("x-codex-turn-state"))
	})

	t.Run("expired_provenance_fails_closed_and_is_pruned", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c, _ := newTurnStateTestContext(t, 7, "sess-g4")
		account := codexTurnStateGatewayTestAccount(42)
		state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 184)
		key := svc.codexTurnStateKey(c, account, "gpt-5.5")
		originKey := openAICodexTurnStateOriginModelKey("7\x00sess-g4", "gpt-5.5")
		svc.openaiCodexTurnStateOrigins.Store(originKey, openAICodexTurnStateOrigin{
			accountID:  account.ID,
			model:      "gpt-5.5",
			stateHash:  sha256.Sum256([]byte(state)),
			expiresAt:  time.Now().Add(-time.Minute),
			generation: key.generation,
		})
		h := newOutbound(state)
		svc.guardOpenAICodexTurnStateEcho(c, account, h, "gpt-5.5")
		require.Empty(t, h.Get("x-codex-turn-state"))
		_, ok := svc.openaiCodexTurnStateOrigins.Load(originKey)
		require.False(t, ok)
	})

	t.Run("no_session_seed_fails_closed", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c, _ := newTurnStateTestContext(t, 7, "")
		state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 185)
		h := newOutbound(state)
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateGatewayTestAccount(43), h, "gpt-5.5")
		require.Empty(t, h.Get("x-codex-turn-state"))
	})

	t.Run("no_echo_noop", func(t *testing.T) {
		svc := &OpenAIGatewayService{}
		c, _ := newTurnStateTestContext(t, 7, "sess-g5")
		h := newOutbound("")
		svc.guardOpenAICodexTurnStateEcho(c, &Account{ID: 43}, h)
		require.Empty(t, h.Get("x-codex-turn-state"))
	})
}

func TestGuardOpenAICodexTurnStateEchoStripsKnownSameAccountDifferentModel(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	c, _ := newTurnStateTestContext(t, 17, "sess-model-mismatch")
	account := codexTurnStateGatewayTestAccount(42)
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 186)
	upstream := http.Header{}
	upstream.Set(openAICodexTurnStateHeader, state)
	svc.relayOpenAICodexTurnState(c, account, upstream, "gpt-5.5")

	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, state)
	svc.guardOpenAICodexTurnStateEcho(c, account, h, "gpt-6-astra")
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
	_, exists := svc.openaiCodexTurnStateOrigins.Load("17\x00sess-model-mismatch")
	require.False(t, exists, "model mismatch must retire the provenance record")
}

func TestOpenAICodexTurnStateProvenanceIsolatedByConcurrentModel(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	c, _ := newTurnStateTestContext(t, 18, "sess-model-concurrent")
	account := codexTurnStateGatewayTestAccount(42)
	stateA := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 190)
	stateB := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 191)

	// Two model requests can share one downstream execution scope. Their
	// provenance records must not overwrite one another.
	svc.noteOpenAICodexTurnStateProvenance(c, account, stateA, "gpt-5.5")
	svc.noteOpenAICodexTurnStateProvenance(c, account, stateB, "gpt-6-astra")

	for _, test := range []struct {
		name, model, state string
	}{
		{name: "model_a", model: "gpt-5.5", state: stateA},
		{name: "model_b", model: "gpt-6-astra", state: stateB},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := http.Header{}
			h.Set(openAICodexTurnStateHeader, test.state)
			svc.guardOpenAICodexTurnStateEcho(c, account, h, test.model)
			require.Equal(t, test.state, h.Get(openAICodexTurnStateHeader))
		})
	}

	// A state from the sibling model is still rejected when presented for the
	// current model, even though both provenance shards belong to the account.
	h := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{stateB}}
	svc.guardOpenAICodexTurnStateEcho(c, account, h, "gpt-5.5")
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
}

func TestClearOpenAICodexTurnStateProvenanceForModelPreservesSibling(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	c, _ := newTurnStateTestContext(t, 19, "sess-model-clear")
	account := codexTurnStateGatewayTestAccount(42)
	stateA := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 192)
	stateB := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 193)
	svc.noteOpenAICodexTurnStateProvenance(c, account, stateA, "gpt-5.5")
	svc.noteOpenAICodexTurnStateProvenance(c, account, stateB, "gpt-6-astra")

	seed := openAICodexTurnStateSeed(c)
	modelAKey := openAICodexTurnStateOriginModelKey(seed, "gpt-5.5")
	modelBKey := openAICodexTurnStateOriginModelKey(seed, "gpt-6-astra")
	if _, ok := svc.openaiCodexTurnStateOrigins.Load(modelAKey); !ok {
		t.Fatal("model A provenance shard was not recorded")
	}
	if _, ok := svc.openaiCodexTurnStateOrigins.Load(modelBKey); !ok {
		t.Fatal("model B provenance shard was not recorded")
	}

	svc.clearOpenAICodexTurnStateProvenanceForModel(c, "gpt-5.5")

	_, modelAExists := svc.openaiCodexTurnStateOrigins.Load(modelAKey)
	_, modelBExists := svc.openaiCodexTurnStateOrigins.Load(modelBKey)
	require.False(t, modelAExists)
	require.True(t, modelBExists, "clearing one model must preserve the sibling provenance shard")
	origin, _, ok := svc.loadOpenAICodexTurnStateOrigin(seed, "gpt-6-astra")
	require.True(t, ok)
	require.Equal(t, account.ID, origin.accountID)
	require.Equal(t, sha256.Sum256([]byte(stateB)), origin.stateHash)

	// If the target shard is gone, the guard may fall back to the shared seed
	// record for the sibling model. Reject the echo but do not delete that
	// sibling binding as a side effect.
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, stateA)
	svc.guardOpenAICodexTurnStateEcho(c, account, h, "gpt-5.5")
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
	_, siblingSeedExists := svc.openaiCodexTurnStateOrigins.Load(seed)
	require.True(t, siblingSeedExists, "a model mismatch against the legacy seed must preserve the sibling record")
}

func TestOpenAICodexTurnStateProvenanceIsolatesSiblingExecutionScopes(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	const apiKeyID = int64(71)
	const model = "gpt-5.5"
	bodyA := []byte(`{"client_metadata":{"thread_id":"child-a"}}`)
	bodyB := []byte(`{"client_metadata":{"thread_id":"child-b"}}`)
	cA, _ := newTurnStateTestContext(t, apiKeyID, "shared-session")
	cB, _ := newTurnStateTestContext(t, apiKeyID, "shared-session")
	scopeA := BindOpenAICodexTurnStateExecutionScope(cA, bodyA)
	scopeB := BindOpenAICodexTurnStateExecutionScope(cB, bodyB)
	require.NotEmpty(t, scopeA)
	require.NotEmpty(t, scopeB)
	require.NotEqual(t, scopeA, scopeB)
	require.Equal(t, scopeA, BindOpenAICodexTurnStateExecutionScope(cA, bodyB), "scope binding must be immutable for the request lifecycle")

	stateA := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 187)
	stateB := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 188)
	accountA := codexTurnStateGatewayTestAccount(91)
	accountB := codexTurnStateGatewayTestAccount(92)
	svc.noteOpenAICodexTurnStateProvenance(cA, accountA, stateA, model)
	svc.noteOpenAICodexTurnStateProvenance(cB, accountB, stateB, model)

	assertGuard := func(c *gin.Context, accountID int64, state string, wantKept bool) {
		h := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state}}
		svc.guardOpenAICodexTurnStateEcho(c, codexTurnStateGatewayTestAccount(accountID), h, model)
		if wantKept {
			require.Equal(t, state, h.Get(openAICodexTurnStateHeader))
		} else {
			require.Empty(t, h.Get(openAICodexTurnStateHeader))
		}
	}
	assertGuard(cA, 91, stateA, true)
	assertGuard(cB, 92, stateB, true)
	assertGuard(cA, 92, stateA, false)
	assertGuard(cB, 91, stateB, false)
	staleState := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 189)
	assertGuard(cA, 91, staleState, false)
}

func TestClearOpenAIWSTurnStateForAccountSwitch_ClearsOnlyAttemptScopes(t *testing.T) {
	const (
		apiKeyID  = int64(71)
		accountID = int64(91)
	)
	groupID := int64(81)
	stateStore := NewOpenAIWSStateStore(nil)
	svc := newCodexTurnStateGatewayTestService(nil)
	svc.openaiWSStateStore = stateStore
	accountA := codexTurnStateGatewayTestAccount(accountID)
	accountB := codexTurnStateGatewayTestAccount(accountID + 1)
	requestBody := []byte(`{"model":"gpt-5.1","client_metadata":{"thread_id":"child-a"},"input":"hello"}`)
	siblingBody := []byte(`{"model":"gpt-5.1","client_metadata":{"thread_id":"child-b"},"input":"hello"}`)
	c, _ := newTurnStateTestContext(t, apiKeyID, "shared-session")
	c.Set("api_key", &APIKey{ID: apiKeyID, GroupID: &groupID})
	executionScope := BindOpenAICodexTurnStateExecutionScope(c, requestBody)
	require.NotEmpty(t, executionScope)
	stateFromAccountA := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 190)
	c.Request.Header.Set(openAICodexTurnStateHeader, stateFromAccountA)
	c.Writer.Header().Set(openAICodexTurnStateHeader, stateFromAccountA)
	svc.noteOpenAICodexTurnStateProvenance(c, accountA, stateFromAccountA, "gpt-5.1")
	stateStore.BindSessionTurnState(groupID, accountID, executionScope, "account-a-state", time.Hour)
	stateStore.BindSessionConn(groupID, executionScope, "account-a-conn", time.Hour)

	siblingContext, _ := newTurnStateTestContext(t, apiKeyID, "shared-session")
	siblingContext.Set("api_key", &APIKey{ID: apiKeyID, GroupID: &groupID})
	siblingScope := BindOpenAICodexTurnStateExecutionScope(siblingContext, siblingBody)
	require.NotEqual(t, executionScope, siblingScope)
	stateStore.BindSessionTurnState(groupID, accountID, siblingScope, "sibling-state", time.Hour)
	stateStore.BindSessionConn(groupID, siblingScope, "sibling-conn", time.Hour)
	siblingStateValue := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 191)
	svc.noteOpenAICodexTurnStateProvenance(siblingContext, accountA, siblingStateValue, "gpt-5.1")

	svc.ClearOpenAIWSTurnStateForAccountSwitch(c, "legacy-sticky-hash")

	require.Empty(t, c.Request.Header.Get(openAICodexTurnStateHeader))
	require.Empty(t, c.Writer.Header().Get(openAICodexTurnStateHeader))
	_, stateExists := stateStore.GetSessionTurnState(groupID, accountID, executionScope)
	_, connExists := stateStore.GetSessionConn(groupID, executionScope)
	require.False(t, stateExists)
	require.False(t, connExists)
	siblingState, siblingStateExists := stateStore.GetSessionTurnState(groupID, accountID, siblingScope)
	siblingConn, siblingConnExists := stateStore.GetSessionConn(groupID, siblingScope)
	require.True(t, siblingStateExists)
	require.Equal(t, "sibling-state", siblingState)
	require.True(t, siblingConnExists)
	require.Equal(t, "sibling-conn", siblingConn)
	_, activeOriginExists := svc.openaiCodexTurnStateOrigins.Load(executionScope)
	_, siblingOriginExists := svc.openaiCodexTurnStateOrigins.Load(siblingScope)
	require.True(t, activeOriginExists, "immutable provenance must survive mutable state cleanup")
	require.True(t, siblingOriginExists)

	oldState := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{stateFromAccountA}}
	svc.guardOpenAICodexTurnStateEcho(c, accountB, oldState, "gpt-5.1")
	require.Empty(t, oldState.Get(openAICodexTurnStateHeader))
}

func TestClearOpenAIWSTurnStateForAccountSwitchUsesSingleLegacyFallback(t *testing.T) {
	const accountID = int64(91)
	groupID := int64(81)
	stateStore := NewOpenAIWSStateStore(nil)
	svc := &OpenAIGatewayService{openaiWSStateStore: stateStore}
	c, _ := newTurnStateTestContext(t, 71, "")
	c.Set("api_key", &APIKey{ID: 71, GroupID: &groupID})
	stateStore.BindSessionTurnState(groupID, accountID, "legacy-active", "state", time.Hour)
	stateStore.BindSessionConn(groupID, "legacy-active", "conn", time.Hour)
	stateStore.BindSessionTurnState(groupID, accountID, "legacy-sibling", "sibling-state", time.Hour)

	svc.ClearOpenAIWSTurnStateForAccountSwitch(c, "legacy-active")

	_, activeStateExists := stateStore.GetSessionTurnState(groupID, accountID, "legacy-active")
	_, activeConnExists := stateStore.GetSessionConn(groupID, "legacy-active")
	_, siblingStateExists := stateStore.GetSessionTurnState(groupID, accountID, "legacy-sibling")
	require.False(t, activeStateExists)
	require.False(t, activeConnExists)
	require.True(t, siblingStateExists)
}

func TestSweepOpenAICodexTurnStateOrigins_PrunesExpiredEntries(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.openaiCodexTurnStateOrigins.Store("expired", openAICodexTurnStateOrigin{
		accountID: 1,
		stateHash: sha256.Sum256([]byte("expired")),
		expiresAt: time.Now().Add(-time.Minute),
	})
	svc.openaiCodexTurnStateOrigins.Store("alive", openAICodexTurnStateOrigin{
		accountID: 2,
		stateHash: sha256.Sum256([]byte("alive")),
		expiresAt: time.Now().Add(time.Hour),
	})

	// 计数器推进到触发清扫的边界
	svc.openaiCodexTurnStateWrites.Store(255)
	svc.sweepOpenAICodexTurnStateOrigins()

	_, expiredOK := svc.openaiCodexTurnStateOrigins.Load("expired")
	require.False(t, expiredOK)
	_, aliveOK := svc.openaiCodexTurnStateOrigins.Load("alive")
	require.True(t, aliveOK)
}

func TestWriteOpenAIPassthroughResponseHeaders_RelaysAndClearsTurnState(t *testing.T) {
	// filter=nil 走 content-type 兜底分支；turn-state 强制放行不依赖 filter。
	dst := http.Header{}
	src := http.Header{}
	src.Set("X-Codex-Turn-State", "blob-P")
	writeOpenAIPassthroughResponseHeaders(dst, src, nil)
	require.Equal(t, "blob-P", dst.Get("X-Codex-Turn-State"))

	// 上游缺失时清除残留（failover 换号防串扰）
	writeOpenAIPassthroughResponseHeaders(dst, http.Header{"Content-Type": []string{"application/json"}}, nil)
	require.Empty(t, dst.Get("X-Codex-Turn-State"))
}

func TestWriteOpenAIPassthroughResponseHeaders_RelaysReasoningIncluded(t *testing.T) {
	dst := http.Header{}
	src := http.Header{}
	src.Set("X-Reasoning-Included", "1")

	writeOpenAIPassthroughResponseHeaders(
		dst,
		src,
		responseheaders.CompileHeaderFilter(config.ResponseHeaderConfig{}),
	)
	require.Equal(t, "1", dst.Get("X-Reasoning-Included"))
}

func TestNewStreamHeaderWriter_DoesNotGenericallyRelayTurnState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	upstream := http.Header{
		"Content-Type":       []string{"text/event-stream"},
		"X-Codex-Turn-State": []string{"opaque-state"},
		"X-Request-Id":       []string{"request-1"},
	}
	svc := &OpenAIGatewayService{
		responseHeaderFilter: responseheaders.CompileHeaderFilter(config.ResponseHeaderConfig{
			Enabled:           true,
			AdditionalAllowed: []string{"x-codex-turn-state"},
		}),
	}

	svc.newStreamHeaderWriter(c, upstream)()

	require.Empty(t, recorder.Header().Get(openAICodexTurnStateHeader))
	require.Equal(t, "request-1", recorder.Header().Get("X-Request-Id"))
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

func TestEnsureOpenAIRemoteCompactionV2BetaFeature(t *testing.T) {
	t.Run("absent_sets_feature", func(t *testing.T) {
		h := http.Header{}
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("present_unchanged", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-codex-beta-features", "responses_websockets_v2, remote_compaction_v2")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "responses_websockets_v2, remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("other_tokens_merged", func(t *testing.T) {
		h := http.Header{}
		h.Set("x-codex-beta-features", "responses_websockets_v2")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, "responses_websockets_v2,remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("multi_line_values_merged_single_line", func(t *testing.T) {
		h := http.Header{}
		h.Add("x-codex-beta-features", "feature_a")
		h.Add("x-codex-beta-features", "feature_b")
		ensureOpenAIRemoteCompactionV2BetaFeature(h)
		require.Equal(t, []string{"feature_a,feature_b,remote_compaction_v2"}, h.Values("x-codex-beta-features"))
	})
}

// 对齐真实 Codex：该头是会话级常量，挂在 OAuth 的每个请求上，而不是只在
// 压缩回合出现（codex-rs build_model_client_beta_features_header）。
func TestApplyOpenAICodexBetaFeatures(t *testing.T) {
	oauthAccount := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	apiKeyAccount := &Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	t.Run("oauth_plain_request_gets_default_codex_shape", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, oauthAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"),
			"OAuth 的普通请求也必须带会话级 beta 头")
	})

	t.Run("client_declared_header_preserved", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(c, oauthAccount, h)
		require.Equal(t, "some_other_feature", h.Get("x-codex-beta-features"),
			"客户端显式声明的能力集不得被网关改写（非空即视为用户已关闭 v2）")
	})

	t.Run("native_v2_forces_feature_even_when_client_trimmed_it", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		MarkOpenAINativeCompactionV2(c)
		h := http.Header{}
		h.Set("x-codex-beta-features", "some_other_feature")
		applyOpenAICodexBetaFeatures(c, oauthAccount, h)
		require.Contains(t, h.Get("x-codex-beta-features"), "remote_compaction_v2",
			"body 带 compaction_trigger 是实锤，必须确保 v2 在列")
		require.Contains(t, h.Get("x-codex-beta-features"), "some_other_feature")
	})

	t.Run("native_v2_applies_to_non_oauth_too", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		MarkOpenAINativeCompactionV2(c)
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, apiKeyAccount, h)
		require.Equal(t, "remote_compaction_v2", h.Get("x-codex-beta-features"))
	})

	t.Run("non_oauth_plain_request_untouched", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, apiKeyAccount, h)
		require.Empty(t, h.Get("x-codex-beta-features"),
			"非 Codex 后端不做会话级注入")
	})

	t.Run("nil_account_plain_request_untouched", func(t *testing.T) {
		c, _ := newTurnStateTestContext(t, 7, "sess-beta")
		h := http.Header{}
		applyOpenAICodexBetaFeatures(c, nil, h)
		require.Empty(t, h.Get("x-codex-beta-features"))
	})
}

// WS 握手与 HTTP 出站必须给出同一份会话级 beta 头：真实 Codex 的
// build_websocket_headers 复用 build_responses_headers（client.rs），
// 两侧不一致还会让预热连接与实际请求落进不同的连接池兼容分桶。
func TestBuildOpenAIWSHeaders_CarriesSessionBetaFeatures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{}
	decision := OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}

	build := func(t *testing.T, account *Account, clientBeta string) http.Header {
		t.Helper()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
		if clientBeta != "" {
			c.Request.Header.Set("x-codex-beta-features", clientBeta)
		}
		headers, _, err := svc.buildOpenAIWSHeaders(
			context.Background(), c, account, "test-token", decision,
			true, "", "", "", "gpt-5.6-codex", "",
		)
		require.NoError(t, err)
		return headers
	}

	oauthAccount := &Account{
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{"chatgpt_account_id": "test-account"},
	}

	headers := build(t, oauthAccount, "")
	require.Equal(t, "remote_compaction_v2", headers.Get("x-codex-beta-features"),
		"WS 握手也必须带会话级 beta 头")

	declared := build(t, oauthAccount, "some_other_feature")
	require.Equal(t, []string{"some_other_feature"}, declared.Values("x-codex-beta-features"),
		"客户端已声明时原样保留")

	apiKeyHeaders := build(t, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "")
	require.Empty(t, apiKeyHeaders.Get("x-codex-beta-features"),
		"非 Codex 后端不注入")
}
