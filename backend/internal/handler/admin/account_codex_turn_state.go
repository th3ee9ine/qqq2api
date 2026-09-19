package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// SetCodexTurnStateService attaches the gateway used by the account-level
// manual collection action. The setter keeps NewAccountHandler source
// compatible for integrations and focused handler tests.
func (h *AccountHandler) SetCodexTurnStateService(gateway *service.OpenAIGatewayService) {
	h.codexTurnStateService = gateway
}

type collectCodexTurnStateRequest struct {
	Model string `json:"model"`
}

// CollectCodexTurnState queues a bounded maintenance collection for an
// account whose state is missing or already expired.
// POST /api/v1/admin/accounts/:id/codex-turn-state/collect
func (h *AccountHandler) CollectCodexTurnState(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.adminService == nil {
		response.Error(c, http.StatusServiceUnavailable, "admin account service is unavailable")
		return
	}
	if h.codexTurnStateService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex Turn State collection service is unavailable")
		return
	}

	var req collectCodexTurnStateRequest
	if c.Request != nil && c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		decoder := json.NewDecoder(c.Request.Body)
		decodeErr := decoder.Decode(&req)
		if decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
			response.BadRequest(c, "Invalid request body")
			return
		}
		if decodeErr == nil {
			var trailing json.RawMessage
			if trailingErr := decoder.Decode(&trailing); !errors.Is(trailingErr, io.EOF) {
				response.BadRequest(c, "Invalid request body")
				return
			}
		}
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil || account == nil {
		response.NotFound(c, "Account not found")
		return
	}
	result, err := h.codexTurnStateService.RequestCodexTurnStateCollection(c.Request.Context(), account, req.Model)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex Turn State collection returned no result")
		return
	}
	switch result.Status {
	case service.CodexTurnStateManualStatusQueued:
		response.Accepted(c, result)
	case service.CodexTurnStateManualStatusAlreadyValid:
		response.Success(c, result)
	default:
		// Keep the redacted result in data so the UI can render the exact reason
		// (for example protocol eligibility or an upstream cooldown) without
		// scraping text.
		c.JSON(http.StatusConflict, response.Response{
			Code:    http.StatusConflict,
			Message: result.Message,
			Reason:  result.Reason,
			Data:    result,
		})
	}
}
