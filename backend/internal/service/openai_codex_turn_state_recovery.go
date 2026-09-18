package service

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Kept as a persisted compatibility key. Older builds wrote length-derived
// revocation metadata here; current builds deliberately ignore it because an
// opaque state length is diagnostic evidence, not a validity signal.
const CodexTurnStateAutoRecoveryExtraKey = "codex_turn_state_auto_recovery"
const codexTurnStateSignalHistoryLimit = 64

type codexTurnStateRecovery struct {
	InvalidatedAtMS int64    `json:"invalidated_at_ms"`
	Pending         bool     `json:"pending"`
	Rejected        []string `json:"rejected,omitempty"`
	Signals         []string `json:"signals,omitempty"`
}

func codexTurnStateIs312(state string) bool {
	_, blocks, ok := parseCodexTurnState(state)
	return ok && blocks == 11
}

func codexTurnStateIs356(state string) bool {
	_, blocks, ok := parseCodexTurnState(state)
	return ok && blocks == 13
}

// codexTurnStateIsNormal accepts both account-family variants of a usable
// state: 292 bytes / 10 Fernet blocks for personal accounts and 332 bytes / 12
// blocks for Team accounts. The helper intentionally does not enforce TTL;
// callers apply the issuance and expiry checks for their operation.
func codexTurnStateIsNormal(state string) bool {
	_, blocks, ok := parseCodexTurnState(state)
	return ok && codexTurnStateNormalBlocks(blocks)
}

func codexTurnStateNormalBlocks(blocks int) bool { return blocks == 10 || blocks == 12 }

// codexTurnStateIsRecoverySignal covers both account families. A 356-byte
// team-account signal has the same lifecycle meaning as the 312-byte
// personal-account signal: revoke the current state immediately and require a
// newly probed normal state (292-byte personal or 332-byte Team) before the
// next automatic request.
func codexTurnStateIsRecoverySignal(state string) bool {
	return codexTurnStateIs312(state) || codexTurnStateIs356(state)
}
func codexTurnStateCanonicalDigest(state string) string {
	// Padding is only a transport representation, never a new credential.
	return codexTurnStateDigest(strings.TrimRight(strings.TrimSpace(state), "="))
}
func appendCodexTurnStateDigest(list []string, state string) []string {
	if state == "" {
		return list
	}
	digest := codexTurnStateCanonicalDigest(state)
	for _, old := range list {
		if old == digest {
			return list
		}
	}
	if len(list) >= codexTurnStateSignalHistoryLimit {
		list = list[1:]
	}
	return append(list, digest)
}
func hasCodexTurnStateDigest(list []string, state string) bool {
	digest := codexTurnStateCanonicalDigest(state)
	for _, value := range list {
		if value == digest {
			return true
		}
	}
	return false
}
func codexTurnStateRecoveryFromAccount(account *Account) codexTurnStateRecovery {
	return codexTurnStateRecovery{}
}
func (r codexTurnStateRecovery) clone() codexTurnStateRecovery {
	r.Rejected = append([]string(nil), r.Rejected...)
	r.Signals = append([]string(nil), r.Signals...)
	return r
}
func (r codexTurnStateRecovery) allows(state string, now time.Time) bool {
	return strings.TrimSpace(state) != "" && ValidateOpenAICodexTurnState(state) == nil
}

// Native continuation remains authoritative. Length-derived revocation data
// from older builds must not suppress an official client's opaque state.
func (s *OpenAIGatewayService) codexTurnStateAllowed(ctx context.Context, account *Account, state string) bool {
	if state == "" {
		return false
	}
	if !codexTurnStateAutoEligible(account) || !s.codexTurnStateAutoEnabled(ctx) {
		return true
	}
	return ValidateOpenAICodexTurnState(state) == nil
}
func (s *OpenAIGatewayService) codexTurnStateRecoveryEpoch(ctx context.Context, account *Account) string {
	if !codexTurnStateAutoEligible(account) || !s.codexTurnStateAutoEnabled(ctx) {
		return ""
	}
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	entry := s.codexTurnStateEntryLocked(account, time.Now(), s.codexTurnStateModel(ctx))
	return strconv.FormatInt(entry.recovery.InvalidatedAtMS, 10)
}

type codexTurnStateEpochContextKey struct{}

func (s *OpenAIGatewayService) stampCodexTurnStateRequest(request *http.Request, account *Account) *http.Request {
	if request == nil {
		return nil
	}
	epoch := s.codexTurnStateRecoveryEpoch(request.Context(), account)
	if epoch == "" {
		return request
	}
	return request.WithContext(context.WithValue(request.Context(), codexTurnStateEpochContextKey{}, epoch))
}
func (s *OpenAIGatewayService) collectCodexTurnStateHTTP(ctx context.Context, account *Account, state string, requests ...*http.Request) {
	epoch, sent := "", ""
	if len(requests) > 0 && requests[0] != nil {
		epoch, _ = requests[0].Context().Value(codexTurnStateEpochContextKey{}).(string)
		sent = requests[0].Header.Get(openAICodexTurnStateHeader)
	}
	if len(requests) > 0 && requests[0] != nil {
		ctx = withCodexTurnStateModel(ctx, s.codexTurnStateModel(requests[0].Context()))
	}
	s.collectOpenAICodexTurnStateAtEpoch(ctx, account, state, epoch, sent)
}
