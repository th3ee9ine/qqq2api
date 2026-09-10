package admin

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type codexVersionManager interface {
	ListVersions(context.Context, int) (*service.OpenAICodexVersionHistory, error)
	SyncNow(context.Context) (*service.OpenAICodexVersionSyncResult, error)
}

func (h *SettingHandler) SetCodexVersionSyncService(svc *service.OpenAICodexVersionSyncService) {
	h.codexVersionManager = svc
}

// GetOpenAICodexVersions lists official stable releases for the settings version selector.
func (h *SettingHandler) GetOpenAICodexVersions(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 || page > service.OpenAICodexHistoryMaxPage {
		response.BadRequest(c, "Invalid Codex version page")
		return
	}
	if h.codexVersionManager == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex version service is not configured")
		return
	}
	result, err := h.codexVersionManager.ListVersions(c.Request.Context(), page)
	if err != nil {
		slog.Warn("admin_codex_version_history_failed", "error", err)
		response.Error(c, http.StatusBadGateway, "Failed to fetch official Codex version history. Please retry.")
		return
	}
	response.Success(c, result)
}

// SyncOpenAICodexVersion explicitly refreshes the official stable candidate without
// changing the manual identity values, fixed-version selection or automatic-sync switch.
func (h *SettingHandler) SyncOpenAICodexVersion(c *gin.Context) {
	if h.codexVersionManager == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex version service is not configured")
		return
	}
	result, err := h.codexVersionManager.SyncNow(c.Request.Context())
	if err != nil {
		slog.Warn("admin_codex_version_sync_failed", "error", err)
		response.Error(c, http.StatusBadGateway, "Failed to sync the official Codex version. The current version settings were preserved.")
		return
	}
	response.Success(c, result)
}
