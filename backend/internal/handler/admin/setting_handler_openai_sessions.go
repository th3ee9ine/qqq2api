package admin

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// GetOpenAISessionCleanupSettings returns the installation-wide OpenAI device
// session cleanup policy.  The policy is intentionally exposed from the
// settings namespace because it no longer belongs to an individual account.
func (h *SettingHandler) GetOpenAISessionCleanupSettings(c *gin.Context) {
	if h == nil || h.settingService == nil {
		response.Error(c, 500, "setting service is not configured")
		return
	}
	settings, err := h.settingService.GetOpenAISessionCleanupGlobalSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// UpdateOpenAISessionCleanupSettings persists the global policy.  The worker
// picks up the value on its next scan, so this endpoint does not need a worker
// dependency or a process restart.
func (h *SettingHandler) UpdateOpenAISessionCleanupSettings(c *gin.Context) {
	if h == nil || h.settingService == nil {
		response.Error(c, 500, "setting service is not configured")
		return
	}
	var settings service.OpenAISessionCleanupGlobalSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if settings.IntervalMinutes < service.OpenAISessionCleanupMinIntervalMinutes || settings.IntervalMinutes > service.OpenAISessionCleanupMaxIntervalMinutes {
		response.BadRequest(c, fmt.Sprintf("interval_minutes must be between %d and %d", service.OpenAISessionCleanupMinIntervalMinutes, service.OpenAISessionCleanupMaxIntervalMinutes))
		return
	}
	if err := h.settingService.SetOpenAISessionCleanupGlobalSettings(c.Request.Context(), &settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	updated, err := h.settingService.GetOpenAISessionCleanupGlobalSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}

// Explicit Global-suffixed aliases are useful to integrations that expose
// installation-wide settings under their fully qualified operation names.
func (h *SettingHandler) GetOpenAISessionCleanupGlobalSettings(c *gin.Context) {
	h.GetOpenAISessionCleanupSettings(c)
}

func (h *SettingHandler) UpdateOpenAISessionCleanupGlobalSettings(c *gin.Context) {
	h.UpdateOpenAISessionCleanupSettings(c)
}
