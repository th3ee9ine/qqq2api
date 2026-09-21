package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

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

// canUseOpenAIWSSessionTurnStateStore keeps reusable WS turn state behind the
// collector injection switch and a complete isolation key. Content-derived
// session hashes remain valid for connection affinity, but must never become a
// turn-state cache scope.
func (s *OpenAIGatewayService) canUseOpenAIWSSessionTurnStateStore(account *Account, scope, model string) bool {
	_, cacheInjectionEnabled := s.CodexTurnStateRuntimeSettings()
	return s != nil && cacheInjectionEnabled && s.codexTurnStateEligible(account) &&
		strings.TrimSpace(scope) != "" && codexTurnStateModel(model) != ""
}

// canCommitOpenAIWSSessionTurnState additionally rejects a response from a WS
// request whose account generation was invalidated while it was in flight.
func (s *OpenAIGatewayService) canCommitOpenAIWSSessionTurnState(c *gin.Context, account *Account, state string) bool {
	if !s.openAIWSSessionTurnStateUsable(account, state) {
		return false
	}
	if account == nil {
		return true
	}
	_, current := s.codexTurnStateRequestGenerationCurrent(c, account.ID)
	return current
}

// resolveOpenAIWSCodexTurnState keeps a valid native WS state authoritative
// for the current attempt. Collector injection is only considered when the
// request and session store supplied no state at all.
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
	if !s.codexTurnStateEligible(account) {
		return current
	}

	if current != "" {
		if _, err := ValidateOpenAICodexTurnState(current, s.codexTurnStatePolicy(), time.Now()); err != nil {
			if c != nil && c.Request != nil {
				c.Request.Header.Del(openAICodexTurnStateHeader)
			}
			key := s.codexTurnStateKey(c, account, model)
			s.bindCodexTurnStateRequest(c, key, model, OpenAICodexTurnStateSnapshot{}, false)
			return ""
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
		return current
	}

	incoming := make(http.Header)
	if c != nil && c.Request != nil {
		if cloned := c.Request.Header.Clone(); cloned != nil {
			incoming = cloned
		}
	}
	incoming.Del(openAICodexTurnStateHeader)
	if snapshot, ok := s.prepareCodexTurnState(ctx, c, account, model, incoming, allowProbe, probeIdentity...); ok {
		return snapshot.Token.Value
	}
	return ""
}
