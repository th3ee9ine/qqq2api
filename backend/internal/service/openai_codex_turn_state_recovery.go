package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 292/312 describe padded base64url envelope lengths, not HTTP statuses.
// Use decoded block counts so omitted '=' padding has identical semantics.
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
	if account == nil {
		return codexTurnStateRecovery{}
	}
	// Decode only the small service-owned projection, never reflect arbitrary
	// imported/legacy fields in a DTO, log or write-back.
	var out codexTurnStateRecovery
	raw, _ := json.Marshal(account.Extra[CodexTurnStateAutoRecoveryExtraKey])
	_ = json.Unmarshal(raw, &out)
	sanitize := func(values []string) []string {
		result := make([]string, 0, len(values))
		for _, v := range values {
			if len(v) != 64 || strings.Trim(v, "0123456789abcdef") != "" {
				continue
			}
			result = append(result, v)
			if len(result) == codexTurnStateSignalHistoryLimit {
				break
			}
		}
		return result
	}
	out.Rejected, out.Signals = sanitize(out.Rejected), sanitize(out.Signals)
	if out.InvalidatedAtMS <= 0 {
		return codexTurnStateRecovery{}
	}
	return out
}
func (r codexTurnStateRecovery) clone() codexTurnStateRecovery {
	r.Rejected = append([]string(nil), r.Rejected...)
	r.Signals = append([]string(nil), r.Signals...)
	return r
}
func (r codexTurnStateRecovery) allows(state string, now time.Time) bool {
	if state == "" || codexTurnStateIs312(state) {
		return false
	}
	if r.InvalidatedAtMS == 0 {
		return true
	}
	issued, blocks, ok := parseCodexTurnState(state)
	return ok && blocks == 10 &&
		!hasCodexTurnStateDigest(r.Rejected, state) &&
		issued.Unix() >= r.InvalidatedAtMS/1000 &&
		!issued.After(now.Add(time.Minute)) && now.Before(issued.Add(codexTurnStateTTL))
}

// Invalidation applies even before TTL expiry. A duplicate 312 never resets
// cooldown; each new recovery episode gets one immediate probe. Caller holds
// the state mutex, including while sharing the generation with probe workers.
func (s *OpenAIGatewayService) invalidateCodexTurnStateLocked(entry *codexTurnStateAutoEntry, state string, now time.Time, sent ...string) {
	if hasCodexTurnStateDigest(entry.recovery.Signals, state) {
		return
	}
	entry.recovery.Signals = appendCodexTurnStateDigest(entry.recovery.Signals, state)
	if !entry.recovery.Pending {
		at := now.UnixMilli()
		if at <= entry.recovery.InvalidatedAtMS {
			at = entry.recovery.InvalidatedAtMS + 1
		}
		entry.recovery.InvalidatedAtMS = at
		entry.forceProbe = true
	}
	entry.recovery.Pending = true
	for _, token := range append(sent, entry.token) {
		entry.recovery.Rejected = appendCodexTurnStateDigest(entry.recovery.Rejected, token)
	}
	entry.token, entry.setAt, entry.lastError = "", now.UnixMilli(), "state_312"
	entry.dirty, entry.probe = true, true
	// Preserve the most recent routed model recorded by outgoing requests.
}

// Also guard global/manual and native continuation sources: otherwise their
// higher priority would reintroduce a revoked state after the cache is cleared.
func (s *OpenAIGatewayService) codexTurnStateAllowed(ctx context.Context, account *Account, state string) bool {
	if state == "" {
		return false
	}
	if !codexTurnStateAutoEligible(account) || !s.codexTurnStateAutoEnabled(ctx) {
		return true
	}
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	entry := s.codexTurnStateEntryLocked(account, time.Now())
	return entry.recovery.allows(state, time.Now())
}
func (s *OpenAIGatewayService) codexTurnStateRecoveryEpoch(ctx context.Context, account *Account) string {
	if !codexTurnStateAutoEligible(account) || !s.codexTurnStateAutoEnabled(ctx) {
		return ""
	}
	s.openaiTurnStateMu.Lock()
	defer s.openaiTurnStateMu.Unlock()
	entry := s.codexTurnStateEntryLocked(account, time.Now())
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
	s.collectOpenAICodexTurnStateAtEpoch(ctx, account, state, epoch, sent)
}
