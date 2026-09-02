package handler

import (
	"crypto/sha256"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/securityaudit"
	middleware2 "github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"go.uber.org/zap"
)

const securityAuditCompletedContextKey = "sub2api.security_audit.completed"
const securityAuditWSTurnContextKey = "sub2api.security_audit.ws_turn"
const securityAuditWSDedupeContextKey = "sub2api.security_audit.ws_dedupe"

// securityAuditDecisionContextKey carries bounded decision metadata into the
// Ops error-detail snapshot. It contains scanner IDs/evidence and policy
// versions without duplicating the full prompt body, so an omitted body can
// still be triaged without copying a multi-megabyte payload into the log row.
const securityAuditDecisionContextKey = "sub2api.security_audit.decision"

// Ops request details are retained alongside error rows and may be rendered
// by an admin UI. Keep scanner-controlled metadata bounded even when a remote
// audit backend returns a very large evidence string or an unexpectedly large
// map. Normal rule IDs/evidence remain byte-for-byte unchanged below these
// limits.
const (
	maxSecurityAuditMetadataEntries    = 64
	maxSecurityAuditMetadataValueBytes = 512
)

type securityAuditWSDedupeEntry struct {
	stage    string
	turn     int
	bodyHash [sha256.Size]byte
	decision securityaudit.Decision
}

// cachesSecurityAuditCompletion reports whether a successful audit may be
// reused for the rest of the gin request. WebSocket turns share one Context
// across many response.create frames and must be audited independently.
func cachesSecurityAuditCompletion(stage string) bool {
	switch strings.TrimSpace(stage) {
	case "", "http":
		return true
	default:
		return false
	}
}

func isSecurityAuditWebSocketStage(stage string) bool {
	switch strings.TrimSpace(stage) {
	case "first_turn", "subsequent_turn":
		return true
	default:
		return false
	}
}

func (h *GatewayHandler) checkSecurityAudit(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol, model string, body []byte) *securityaudit.Decision {
	if h == nil {
		return nil
	}
	return runSecurityAudit(c, reqLog, h.securityAuditCoordinator, h.contentModerationService, apiKey, subject, protocol, model, body, "http")
}

func (h *OpenAIGatewayHandler) checkSecurityAudit(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol, model string, body []byte) *securityaudit.Decision {
	if h == nil {
		return nil
	}
	return runSecurityAudit(c, reqLog, h.securityAuditCoordinator, h.contentModerationService, apiKey, subject, protocol, model, body, "http")
}

func (h *OpenAIGatewayHandler) checkSecurityAuditStage(c *gin.Context, reqLog *zap.Logger, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol, model string, body []byte, stage string) *securityaudit.Decision {
	if h == nil {
		return nil
	}
	return runSecurityAudit(c, reqLog, h.securityAuditCoordinator, h.contentModerationService, apiKey, subject, protocol, model, body, stage)
}

func runSecurityAudit(c *gin.Context, reqLog *zap.Logger, coordinator *securityaudit.Coordinator, legacy *service.ContentModerationService, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol, model string, body []byte, stage string) *securityaudit.Decision {
	if c == nil || c.Request == nil {
		return nil
	}
	// A gin Context can be reused across WebSocket turns and compatibility
	// middleware may invoke this helper more than once. Clear the previous
	// explanation before considering the completion cache so a later error row
	// never inherits metadata from an earlier payload.
	c.Set(securityAuditDecisionContextKey, nil)
	cacheCompletion := cachesSecurityAuditCompletion(stage)
	if cacheCompletion {
		if completed, exists := c.Get(securityAuditCompletedContextKey); exists && completed == true {
			return nil
		}
	}
	if coordinator == nil {
		legacyDecision := runContentModeration(c, reqLog, legacy, apiKey, subject, protocol, model, body)
		if legacyDecision == nil {
			return nil
		}
		decision := securityaudit.Decision{Kind: securityaudit.DecisionAllow, HTTPStatus: http.StatusOK, AllowNextStage: true}
		decision.Legacy = &securityaudit.LegacyDecision{
			Allowed: legacyDecision.Allowed, Blocked: legacyDecision.Blocked, Flagged: legacyDecision.Flagged,
			Message: legacyDecision.Message, StatusCode: legacyDecision.StatusCode,
			ErrorCode: "content_policy_violation", Action: legacyDecision.Action,
		}
		if legacyDecision.Blocked {
			decision.Kind, decision.HTTPStatus, decision.ErrorCode, decision.ClientMessage, decision.AllowNextStage = securityaudit.DecisionBlock, contentModerationStatus(legacyDecision), "content_policy_violation", legacyDecision.Message, false
		}
		if decision.AllowNextStage && cacheCompletion {
			c.Set(securityAuditCompletedContextKey, true)
		}
		setSecurityAuditDecisionMetadata(c, &decision)
		return &decision
	}
	request := buildSecurityAuditRequest(c, apiKey, subject, protocol, model, body, stage)
	if isSecurityAuditWebSocketStage(request.Stage) {
		if turnNo, ok := securityAuditWSTurn(c); ok {
			bodyHash := sha256.Sum256(body)
			if cached, exists := c.Get(securityAuditWSDedupeContextKey); exists {
				if entry, ok := cached.(securityAuditWSDedupeEntry); ok &&
					entry.stage == request.Stage && entry.turn == turnNo && entry.bodyHash == bodyHash {
					decision := entry.decision
					setSecurityAuditDecisionMetadata(c, &decision)
					logSecurityAuditDone(reqLog, request, decision, true)
					return &decision
				}
			}
			logSecurityAuditStart(reqLog, request, len(body), false)
			decision := coordinator.Check(c.Request.Context(), request)
			if decision.Kind == securityaudit.DecisionAllow {
				c.Set(securityAuditWSDedupeContextKey, securityAuditWSDedupeEntry{
					stage: request.Stage, turn: turnNo, bodyHash: bodyHash, decision: decision,
				})
			}
			logSecurityAuditDone(reqLog, request, decision, false)
			setSecurityAuditDecisionMetadata(c, &decision)
			return &decision
		}
	}
	logSecurityAuditStart(reqLog, request, len(body), false)
	decision := coordinator.Check(c.Request.Context(), request)
	if decision.AllowNextStage && cacheCompletion {
		c.Set(securityAuditCompletedContextKey, true)
	}
	logSecurityAuditDone(reqLog, request, decision, false)
	setSecurityAuditDecisionMetadata(c, &decision)
	return &decision
}

// setSecurityAuditDecisionMetadata stores a compact, structured explanation of
// the latest audit decision for the Ops request-detail builder. Keeping this
// separate from the client response avoids exposing the full audit payload
// while preserving rule IDs, bounded evidence, roles, scores, and policy
// versions for false-positive review when the request body is omitted or
// truncated.
func setSecurityAuditDecisionMetadata(c *gin.Context, decision *securityaudit.Decision) {
	if c == nil || decision == nil {
		return
	}
	metadata := map[string]any{
		"decision":         string(decision.Kind),
		"error_code":       boundSecurityAuditMetadataString(decision.ErrorCode),
		"allow_next_stage": decision.AllowNextStage,
	}
	if decision.Legacy != nil {
		metadata["legacy"] = map[string]any{
			"blocked":     decision.Legacy.Blocked,
			"flagged":     decision.Legacy.Flagged,
			"action":      boundSecurityAuditMetadataString(decision.Legacy.Action),
			"status_code": decision.Legacy.StatusCode,
			"error_code":  boundSecurityAuditMetadataString(decision.Legacy.ErrorCode),
			"message":     boundSecurityAuditMetadataString(decision.Legacy.Message),
		}
	}
	if result := decision.Prompt; result != nil {
		prompt := map[string]any{
			"decision":         string(result.Kind),
			"error_code":       boundSecurityAuditMetadataString(result.ErrorCode),
			"allow_next_stage": result.AllowNextStage,
		}
		if normalized := result.Result; normalized != nil {
			// Keep the PromptDecision kind (`allow`/`block`/`flag`) in
			// `decision`; the normalized event decision is a separate field so
			// downstream triage does not confuse the two enums.
			prompt["event_decision"] = string(normalized.Decision)
			prompt["risk_level"] = string(normalized.RiskLevel)
			prompt["action"] = string(normalized.Action)
			prompt["safety"] = boundSecurityAuditMetadataString(normalized.Safety)
			prompt["categories"] = cloneBoundedStringSlice(normalized.Categories)
			prompt["matched_scanners"] = cloneBoundedStringSlice(normalized.MatchedScanners)
			prompt["scanner_scores"] = cloneBoundedStringFloatMap(normalized.ScannerScores)
			prompt["scanner_evidence"] = cloneBoundedStringStringMap(normalized.ScannerEvidence)
			prompt["scanner_backend"] = boundSecurityAuditMetadataString(normalized.ScannerBackend)
			prompt["scanner_version"] = boundSecurityAuditMetadataString(normalized.ScannerVersion)
			prompt["guard_endpoint_id"] = boundSecurityAuditMetadataString(normalized.GuardEndpointID)
			prompt["policy_id"] = boundSecurityAuditMetadataString(normalized.PolicyID)
			prompt["policy_version"] = normalized.PolicyVersion
			prompt["chunk_total"] = normalized.ChunkTotal
			prompt["latency_ms"] = normalized.LatencyMS
			prompt["unknown_categories"] = cloneBoundedStringSlice(normalized.UnknownCategories)
		}
		metadata["prompt"] = prompt
	}
	c.Set(securityAuditDecisionContextKey, metadata)
}

func cloneStringFloatMap(input map[string]float64) map[string]float64 {
	return cloneBoundedStringFloatMap(input)
}

func cloneBoundedStringFloatMap(input map[string]float64) map[string]float64 {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > maxSecurityAuditMetadataEntries {
		keys = keys[:maxSecurityAuditMetadataEntries]
	}
	output := make(map[string]float64, minSecurityAuditMetadataEntries(len(keys)))
	for _, key := range keys {
		output[boundSecurityAuditMetadataString(key)] = input[key]
	}
	return output
}

func cloneStringStringMap(input map[string]string) map[string]string {
	return cloneBoundedStringStringMap(input)
}

func cloneBoundedStringStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > maxSecurityAuditMetadataEntries {
		keys = keys[:maxSecurityAuditMetadataEntries]
	}
	output := make(map[string]string, minSecurityAuditMetadataEntries(len(keys)))
	for _, key := range keys {
		output[boundSecurityAuditMetadataString(key)] = boundSecurityAuditMetadataString(input[key])
	}
	return output
}

func cloneBoundedStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	limit := len(input)
	if limit > maxSecurityAuditMetadataEntries {
		limit = maxSecurityAuditMetadataEntries
	}
	output := make([]string, 0, limit)
	for _, value := range input {
		if len(output) >= limit {
			break
		}
		output = append(output, boundSecurityAuditMetadataString(value))
	}
	return output
}

func minSecurityAuditMetadataEntries(length int) int {
	if length < 1 {
		return 1
	}
	if length > maxSecurityAuditMetadataEntries {
		return maxSecurityAuditMetadataEntries
	}
	return length
}

func boundSecurityAuditMetadataString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxSecurityAuditMetadataValueBytes {
		return value
	}
	limit := maxSecurityAuditMetadataValueBytes - len("…")
	if limit < 1 {
		return "…"
	}
	for limit > 0 && limit < len(value) && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + "…"
}

func logSecurityAuditStart(reqLog *zap.Logger, request securityaudit.Request, bodyBytes int, cached bool) {
	if reqLog == nil {
		return
	}
	reqLog.Info("security_audit.gateway_check_start",
		zap.String("request_id", request.RequestID), zap.Int64("user_id", request.UserID),
		zap.Int64("api_key_id", request.APIKeyID), zap.Int64p("group_id", request.GroupID),
		zap.String("endpoint", request.Endpoint), zap.String("provider", request.Provider),
		zap.String("protocol", request.Protocol), zap.String("model", request.Model), zap.String("stage", request.Stage),
		zap.Int("body_bytes", bodyBytes), zap.Bool("cached", cached))
}

func logSecurityAuditDone(reqLog *zap.Logger, request securityaudit.Request, decision securityaudit.Decision, cached bool) {
	if reqLog == nil {
		return
	}
	reqLog.Info("security_audit.gateway_check_done",
		zap.String("request_id", request.RequestID), zap.String("decision", string(decision.Kind)),
		zap.String("error_code", decision.ErrorCode), zap.Bool("allow_next_stage", decision.AllowNextStage),
		zap.String("stage", request.Stage), zap.Bool("cached", cached))
}

func securityAuditWSTurn(c *gin.Context) (int, bool) {
	turn, exists := c.Get(securityAuditWSTurnContextKey)
	if !exists {
		return 0, false
	}
	turnNo, ok := turn.(int)
	return turnNo, ok
}

func buildSecurityAuditRequest(c *gin.Context, apiKey *service.APIKey, subject middleware2.AuthSubject, protocol, model string, body []byte, stage string) securityaudit.Request {
	legacy := buildContentModerationInput(c, apiKey, subject, protocol, model, body)
	request := securityaudit.Request{
		RequestID: legacy.RequestID, UserID: legacy.UserID, UserEmail: legacy.UserEmail,
		APIKeyID: legacy.APIKeyID, APIKeyName: legacy.APIKeyName, GroupID: cloneSecurityAuditGroupID(legacy.GroupID),
		GroupName: legacy.GroupName, Provider: legacy.Provider, Endpoint: legacy.Endpoint,
		Protocol: legacy.Protocol, Model: legacy.Model, Body: body, Stage: strings.TrimSpace(stage),
	}
	if apiKey != nil && apiKey.User != nil {
		request.Username = apiKey.User.Username
		if request.UserEmail == "" {
			request.UserEmail = apiKey.User.Email
		}
	}
	if request.Stage == "" {
		request.Stage = "http"
	}
	return request
}

func securityAuditStatus(decision *securityaudit.Decision) int {
	if decision == nil || decision.HTTPStatus < 400 || decision.HTTPStatus > 599 {
		return http.StatusForbidden
	}
	return decision.HTTPStatus
}

func securityAuditErrorCode(decision *securityaudit.Decision) string {
	if decision == nil || strings.TrimSpace(decision.ErrorCode) == "" {
		return "content_policy_violation"
	}
	return decision.ErrorCode
}

func securityAuditMessage(decision *securityaudit.Decision) string {
	if decision == nil {
		return "Request blocked by content policy"
	}
	if decision.Legacy != nil && decision.Legacy.Blocked && strings.TrimSpace(decision.Legacy.Message) != "" {
		return decision.Legacy.Message
	}
	if strings.TrimSpace(decision.ClientMessage) != "" {
		return decision.ClientMessage
	}
	return "Request blocked by content policy"
}

func cloneSecurityAuditGroupID(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
