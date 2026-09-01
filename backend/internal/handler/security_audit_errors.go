package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/googleapi"
	"github.com/th3ee9ine/qqq2api/internal/securityaudit"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func (h *OpenAIGatewayHandler) openAISecurityAuditError(c *gin.Context, decision *securityaudit.Decision) {
	if decision == nil {
		return
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		h.errorResponse(c, securityAuditStatus(decision), securityAuditErrorCode(decision), securityAuditMessage(decision))
		return
	}
	errType := "api_error"
	if decision.Kind == securityaudit.DecisionBlock {
		errType = "permission_error"
	}
	c.JSON(securityAuditStatus(decision), gin.H{"error": gin.H{
		"type": errType, "code": securityAuditErrorCode(decision), "message": securityAuditMessage(decision),
	}})
}

func (h *GatewayHandler) openAISecurityAuditError(c *gin.Context, decision *securityaudit.Decision) {
	if decision == nil {
		return
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		h.chatCompletionsErrorResponse(c, securityAuditStatus(decision), securityAuditErrorCode(decision), securityAuditMessage(decision))
		return
	}
	errType := "api_error"
	if decision.Kind == securityaudit.DecisionBlock {
		errType = "permission_error"
	}
	c.JSON(securityAuditStatus(decision), gin.H{"error": gin.H{
		"type": errType, "code": securityAuditErrorCode(decision), "message": securityAuditMessage(decision),
	}})
}

func (h *GatewayHandler) responsesSecurityAuditError(c *gin.Context, decision *securityaudit.Decision) {
	if decision == nil {
		return
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		h.responsesErrorResponse(c, securityAuditStatus(decision), securityAuditErrorCode(decision), securityAuditMessage(decision))
		return
	}
	c.JSON(securityAuditStatus(decision), gin.H{"error": gin.H{
		"type": "api_error", "code": securityAuditErrorCode(decision), "message": securityAuditMessage(decision),
	}})
}

func (h *GatewayHandler) anthropicSecurityAuditError(c *gin.Context, decision *securityaudit.Decision) {
	if decision == nil {
		return
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		h.errorResponse(c, securityAuditStatus(decision), securityAuditErrorCode(decision), securityAuditMessage(decision))
		return
	}
	errType := "api_error"
	if decision.Kind == securityaudit.DecisionBlock {
		errType = "permission_error"
	}
	c.JSON(securityAuditStatus(decision), gin.H{"type": "error", "error": gin.H{
		"type": errType, "code": securityAuditErrorCode(decision), "message": securityAuditMessage(decision),
	}})
}

func (h *OpenAIGatewayHandler) anthropicSecurityAuditError(c *gin.Context, decision *securityaudit.Decision) {
	if decision == nil {
		return
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		h.anthropicErrorResponse(c, securityAuditStatus(decision), securityAuditErrorCode(decision), securityAuditMessage(decision))
		return
	}
	errType := "api_error"
	if decision.Kind == securityaudit.DecisionBlock {
		errType = "permission_error"
	}
	c.JSON(securityAuditStatus(decision), gin.H{"type": "error", "error": gin.H{
		"type": errType, "code": securityAuditErrorCode(decision), "message": securityAuditMessage(decision),
	}})
}

func googleSecurityAuditError(c *gin.Context, decision *securityaudit.Decision) {
	if decision == nil {
		return
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		googleError(c, securityAuditStatus(decision), securityAuditMessage(decision))
		return
	}
	status := securityAuditStatus(decision)
	googleStatus := googleapi.HTTPStatusToGoogleStatus(status)
	if status == http.StatusServiceUnavailable {
		googleStatus = "UNAVAILABLE"
	}
	requestID := ""
	if c != nil && c.Request != nil {
		requestID = contentModerationRequestID(c.Request.Context())
	}
	c.JSON(status, gin.H{"error": gin.H{
		"code": status, "message": securityAuditMessage(decision), "status": googleStatus,
		"details": []gin.H{{
			"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
			"reason": securityAuditErrorCode(decision), "domain": "sub2api.securityaudit",
			"metadata": gin.H{"request_id": requestID},
		}},
	}})
}

func writeSecurityAuditWSError(ctx context.Context, conn *coderws.Conn, decision *securityaudit.Decision) {
	if conn == nil || decision == nil {
		return
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		legacy := decision.Legacy
		writeContentModerationWSError(ctx, conn, (legacyContentModerationDecision{legacy}).toService())
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	payload, err := json.Marshal(gin.H{
		"event_id": "evt_prompt_guard_rejected", "type": "error",
		"error": gin.H{"type": "invalid_request_error", "code": securityAuditErrorCode(decision), "message": securityAuditMessage(decision)},
	})
	if err != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = conn.Write(writeCtx, coderws.MessageText, payload)
}

// markSecurityAuditWSError records the rejected response.create frame for the
// Ops stream-error path.  A WebSocket upgrade has no HTTP request body, so the
// ordinary request capture cannot expose the payload that caused the guard
// decision.  The frame is bounded and kept out of routine request logs; it is
// materialized only in the detailed Ops request snapshot.
func markSecurityAuditWSError(c *gin.Context, body []byte, turn int, decision *securityaudit.Decision) {
	if c == nil || decision == nil || decision.AllowNextStage {
		return
	}
	// The first frame is checked before the normal turn hook initializes the
	// stream context. Set the marker directly so MarkOpsStreamFailure tags the
	// exact frame turn; BeginOpsStreamTurn also clears prior-turn upstream
	// diagnostics, which is undesirable on a reused WS context. The regular
	// hook has already reset state for subsequent turns. Always overwrite a
	// stale marker: a rejection must be associated with the frame that produced
	// it, even if an earlier error path left another turn number in the shared
	// Gin context. Keep the helper deterministic for defensive callers that
	// supply a non-positive turn; setOpsRequestFrameBody applies the same
	// normalization.
	if turn < 0 {
		turn = 0
	}
	c.Set(service.OpsStreamTurnKey, turn)
	setOpsRequestFrameBody(c, turn, body)
	errType := "api_error"
	if decision.Kind == securityaudit.DecisionBlock || (decision.Legacy != nil && decision.Legacy.Blocked) {
		errType = "permission_error"
	}
	service.MarkOpsStreamFailure(c, errType, securityAuditErrorCode(decision), securityAuditMessage(decision), securityAuditStatus(decision))
}

type legacyContentModerationDecision struct{ value *securityaudit.LegacyDecision }

func (d legacyContentModerationDecision) toService() *service.ContentModerationDecision {
	if d.value == nil {
		return nil
	}
	return &service.ContentModerationDecision{Allowed: d.value.Allowed, Blocked: d.value.Blocked, Flagged: d.value.Flagged, Message: d.value.Message, StatusCode: d.value.StatusCode, Action: d.value.Action}
}

func securityAuditWSCloseStatus(decision *securityaudit.Decision) coderws.StatusCode {
	if decision == nil {
		return coderws.StatusInternalError
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		return coderws.StatusPolicyViolation
	}
	if decision.Kind == securityaudit.DecisionBlock {
		return coderws.StatusCode(4403)
	}
	return coderws.StatusTryAgainLater
}

func securityAuditWSCloseReason(decision *securityaudit.Decision) string {
	if decision == nil {
		return securityaudit.ErrorCodeUnavailable
	}
	if decision.Legacy != nil && decision.Legacy.Blocked {
		message := strings.TrimSpace(decision.Legacy.Message)
		if message != "" {
			return message
		}
		return "content_policy_violation"
	}
	code := securityAuditErrorCode(decision)
	if code == "" {
		return securityaudit.ErrorCodeUnavailable
	}
	return code
}
