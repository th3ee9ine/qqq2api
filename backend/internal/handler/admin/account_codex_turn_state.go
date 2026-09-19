package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

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
	Model  string `json:"model"`
	Source string `json:"source"`
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
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = "manual"
	}
	if source != "manual" && source != "bulk" {
		response.BadRequest(c, "source must be manual or bulk")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil || account == nil {
		response.NotFound(c, "Account not found")
		return
	}
	collectionCtx := service.WithCodexTurnStateCollectionSource(c.Request.Context(), source)
	result, err := h.codexTurnStateService.RequestCodexTurnStateCollection(collectionCtx, account, req.Model)
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

// ListCodexTurnStateCollectionTasks returns the bounded in-memory history of
// background collection and renewal work.
// GET /api/v1/admin/accounts/codex-turn-state/tasks
func (h *AccountHandler) ListCodexTurnStateCollectionTasks(c *gin.Context) {
	if h.codexTurnStateService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex Turn State collection service is unavailable")
		return
	}
	response.Success(c, h.codexTurnStateService.ListCodexTurnStateCollectionTasks())
}

// GetCodexTurnStateCollectionTask returns one task with its progress and
// redacted event history.
// GET /api/v1/admin/accounts/codex-turn-state/tasks/:task_id
func (h *AccountHandler) GetCodexTurnStateCollectionTask(c *gin.Context) {
	if h.codexTurnStateService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex Turn State collection service is unavailable")
		return
	}
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		response.BadRequest(c, "Invalid task ID")
		return
	}
	task, ok := h.codexTurnStateService.GetCodexTurnStateCollectionTask(taskID)
	if !ok || task == nil {
		response.NotFound(c, "Codex Turn State collection task not found")
		return
	}
	response.Success(c, task)
}

// CancelCodexTurnStateCollectionTask requests cancellation of queued or
// running work. Completed tasks are rejected by the service with a conflict.
// POST /api/v1/admin/accounts/codex-turn-state/tasks/:task_id/cancel
func (h *AccountHandler) CancelCodexTurnStateCollectionTask(c *gin.Context) {
	if h.codexTurnStateService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex Turn State collection service is unavailable")
		return
	}
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		response.BadRequest(c, "Invalid task ID")
		return
	}
	task, err := h.codexTurnStateService.CancelCodexTurnStateCollectionTask(taskID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, task)
}

// RetryCodexTurnStateCollectionTask submits a new task from a failed or
// canceled task's immutable account/model snapshot.
// POST /api/v1/admin/accounts/codex-turn-state/tasks/:task_id/retry
func (h *AccountHandler) RetryCodexTurnStateCollectionTask(c *gin.Context) {
	if h.codexTurnStateService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex Turn State collection service is unavailable")
		return
	}
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		response.BadRequest(c, "Invalid task ID")
		return
	}
	task, err := h.codexTurnStateService.RetryCodexTurnStateCollectionTask(c.Request.Context(), taskID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, task)
}
