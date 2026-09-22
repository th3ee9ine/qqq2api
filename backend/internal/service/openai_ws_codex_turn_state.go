package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const openAIWSTurnStateModelMismatchContextKey = "openai_ws_turn_state_model_mismatch"

func markOpenAIWSTurnStateModelMismatch(c *gin.Context) {
	if c != nil {
		c.Set(openAIWSTurnStateModelMismatchContextKey, true)
	}
}

// openAIWSTurnStateModelMismatchMarked reports whether the current request or
// WS turn has already observed a response-model conflict.  The marker is
// shared by HTTP/SSE and WS forwarding so a conflict found before a terminal
// event cannot be undone by a later handshake/provenance commit.
func openAIWSTurnStateModelMismatchMarked(c *gin.Context) bool {
	if c == nil {
		return false
	}
	raw, ok := c.Get(openAIWSTurnStateModelMismatchContextKey)
	if !ok {
		return false
	}
	value, _ := raw.(bool)
	return value
}

// clearOpenAIWSTurnStateModelMismatch starts a new forwarding attempt/turn.
// A Gin context can be reused across account failover attempts and multiple
// ingress WS turns, so the previous turn's decision must not leak forward.
func clearOpenAIWSTurnStateModelMismatch(c *gin.Context) {
	if c != nil {
		c.Set(openAIWSTurnStateModelMismatchContextKey, false)
	}
}

// consumeOpenAIWSTurnStateModelMismatch lets a WS forwarder retire a pooled
// connection after the response observer invalidates its handshake lineage.
// The flag is consumed so a later turn on the same Gin context cannot inherit
// the previous turn's decision.
func consumeOpenAIWSTurnStateModelMismatch(c *gin.Context) bool {
	if c == nil {
		return false
	}
	raw, ok := c.Get(openAIWSTurnStateModelMismatchContextKey)
	if !ok {
		return false
	}
	c.Set(openAIWSTurnStateModelMismatchContextKey, false)
	value, _ := raw.(bool)
	return value
}

// openAIWSSessionTurnStateUsable validates reusable session-store and
// handshake state for collector-enabled Codex accounts. Legacy transports keep
// their existing opaque-header behavior when the collector is not applicable.
func (s *OpenAIGatewayService) openAIWSSessionTurnStateUsable(account *Account, state string) bool {
	state = strings.TrimSpace(state)
	if state == "" {
		return false
	}
	if !s.codexTurnStateEligible(account) {
		return true
	}
	_, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), time.Now())
	return err == nil
}

// openAIWSSessionTurnStateNeedsRefresh applies the collector's local refresh
// window to a state loaded from the WS session store. The store only keeps the
// opaque value and its sticky-store TTL; without this check a one-hour sticky
// entry would bypass the collector's 55-minute refresh window entirely.
func (s *OpenAIGatewayService) openAIWSSessionTurnStateNeedsRefresh(account *Account, state string) bool {
	state = strings.TrimSpace(state)
	if state == "" || !s.codexTurnStateEligible(account) {
		return false
	}
	token, err := ValidateOpenAICodexTurnState(state, s.codexTurnStatePolicy(), time.Now())
	if err != nil {
		return true
	}
	policy := s.codexTurnStatePolicy().normalized()
	return openAICodexTurnStateTokenNeedsRefresh(token, policy, time.Now())
}

func openAICodexTurnStateTokenNeedsRefresh(token OpenAICodexTurnStateToken, policy OpenAICodexTurnStatePolicy, now time.Time) bool {
	policy = policy.normalized()
	if now.IsZero() {
		now = time.Now()
	}
	return policy.RefreshBefore > 0 && now.Add(policy.RefreshBefore).After(token.IssuedAt.Add(policy.TTL))
}

func (s *OpenAIGatewayService) getOpenAIWSSessionTurnState(
	c *gin.Context,
	store OpenAIWSStateStore,
	groupID int64,
	account *Account,
	scope, model string,
) (string, bool) {
	if store == nil || account == nil {
		return "", false
	}
	generation, current := s.codexTurnStateRequestGenerationCurrent(c, account.ID)
	if !current || generation == 0 {
		return "", false
	}
	if strict, ok := store.(openAIWSSessionTurnStateGenerationStore); ok {
		return strict.GetSessionTurnStateForGeneration(groupID, account.ID, scope, generation, model)
	}
	// A legacy store cannot prove which collector generation minted its value.
	// Fail closed for OAuth/Codex Turn-State while leaving its connection and
	// response-ID features untouched.
	return "", false
}

func (s *OpenAIGatewayService) bindOpenAIWSSessionTurnStateIfRefreshNeeded(
	c *gin.Context,
	store OpenAIWSStateStore,
	groupID int64,
	account *Account,
	scope, model, state string,
) bool {
	if store == nil || account == nil {
		return false
	}
	generation, current := s.codexTurnStateRequestGenerationCurrent(c, account.ID)
	if !current || generation == 0 {
		return false
	}
	now := time.Now()
	if strict, ok := store.(openAIWSSessionTurnStateGenerationStore); ok {
		written := strict.BindSessionTurnStateIfRefreshNeeded(
			groupID, account.ID, scope, state, generation, s.openAIWSSessionStickyTTL(), s.codexTurnStatePolicy(), now, model,
		)
		if written {
			if latest, stillCurrent := s.codexTurnStateRequestGenerationCurrent(c, account.ID); !stillCurrent || latest != generation {
				strict.DeleteSessionTurnStateIfMatch(groupID, account.ID, scope, state, generation, model)
				return false
			}
		}
		return written
	}
	return false
}

// canUseOpenAIWSSessionTurnStateStore keeps reusable WS turn state behind the
// collector injection switch and a complete isolation key. Content-derived
// session hashes remain valid for connection affinity, but must never become a
// turn-state cache scope.
func (s *OpenAIGatewayService) canUseOpenAIWSSessionTurnStateStore(account *Account, scope, model string) bool {
	_, cacheInjectionEnabled := s.CodexTurnStateRuntimeSettings()
	return s != nil && cacheInjectionEnabled && s.codexTurnStateEligible(account) &&
		strings.TrimSpace(scope) != "" && codexTurnStateModel(model) != ""
}

// canCommitOpenAIWSSessionTurnState rejects a response from a WS request whose
// account generation, authoritative scheduling row, or OAuth identity changed
// while it was in flight. Session-store state is a reusable collector candidate,
// so it must pass the same cold-publication checks as an HTTP response before it
// is persisted or exposed as a response header.
func (s *OpenAIGatewayService) canCommitOpenAIWSSessionTurnState(ctx context.Context, c *gin.Context, account *Account, model, state string) bool {
	return s.canCommitOpenAICodexTurnState(ctx, c, account, model, state)
}

// resolveOpenAIWSCodexTurnState keeps a provenance-validated native WS state or
// a request-scoped trusted session-store state authoritative for the current
// attempt. Collector injection is considered only when neither is admissible.
func (s *OpenAIGatewayService) resolveOpenAIWSCodexTurnState(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	model string,
	current string,
	allowProbe bool,
	probeIdentity ...http.Header,
) string {
	current = strings.TrimSpace(current)
	// `current` can come from either the client handshake or the model-scoped
	// session store.  A malformed client value is evidence that the client
	// lineage itself must be retired, while a malformed store value is only a
	// bad cache entry: an independently validated collector snapshot for the
	// same account/model remains safe to reuse.
	invalidNative := false
	if current != "" {
		if _, err := ValidateOpenAICodexTurnState(current, s.codexTurnStatePolicy(), time.Now()); err != nil {
			invalidValue := current
			nativeHeaderState := ""
			if c != nil && c.Request != nil {
				nativeHeaderState = strings.TrimSpace(c.Request.Header.Get(openAIWSTurnStateHeader))
			}
			if c != nil && c.Request != nil {
				c.Request.Header.Del(openAICodexTurnStateHeader)
				c.Request.Header.Del(openAIWSTurnStateHeader)
			}
			// Drop the invalid value and continue below. If probing is enabled,
			// prepareCodexTurnState can now collect a fresh state for this request.
			current = ""
			invalidNative = nativeHeaderState != "" && nativeHeaderState == invalidValue
		} else {
			if !s.codexTurnStateEligible(account) {
				return current
			}

			incoming := make(http.Header)
			if c != nil && c.Request != nil {
				if cloned := c.Request.Header.Clone(); cloned != nil {
					incoming = cloned
				}
			}
			incoming.Set(openAICodexTurnStateHeader, current)
			if snapshot, ok := s.prepareCodexTurnState(ctx, c, account, model, incoming, false); ok && snapshot.Route == "client" {
				return snapshot.Token.Value
			}
			// prepareCodexTurnState clears the request header when provenance
			// proves that this native state belongs to another model/account (or
			// when the model itself is invalid). Do not return the stale `current`
			// value after that rejection, because the caller would immediately put
			// it back on the next WS handshake.
			// `current` may have come from the sticky session store rather than
			// the request header, so inspect the local header passed to prepare,
			// not only c.Request.Header. A cleared local value means the state was
			// rejected and must not be reinstalled by the caller.
			if strings.TrimSpace(incoming.Get(openAICodexTurnStateHeader)) == "" {
				return ""
			}
			return current
		}
	}
	if !s.codexTurnStateEligible(account) {
		return ""
	}

	incoming := make(http.Header)
	if c != nil && c.Request != nil {
		if cloned := c.Request.Header.Clone(); cloned != nil {
			incoming = cloned
		}
	}
	incoming.Del(openAICodexTurnStateHeader)
	if invalidNative {
		// A malformed/expired native value is evidence that this session's
		// lineage is unusable. Do not silently substitute a cached value from the
		// same key; force the normal probe path to mint a fresh state instead.
		key := s.codexTurnStateKey(c, account, model)
		s.codexTurnStateCollector.DeleteAndForceRefresh(key)
	}
	if snapshot, ok := s.prepareCodexTurnState(ctx, c, account, model, incoming, allowProbe, probeIdentity...); ok {
		return snapshot.Token.Value
	}
	return ""
}
