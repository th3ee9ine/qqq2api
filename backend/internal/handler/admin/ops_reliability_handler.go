package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// GetReliabilityStatus returns the bounded, read-only reliability projection
// used by the administrator reliability workbench.
// GET /api/v1/admin/reliability/status
func (h *OpsHandler) GetReliabilityStatus(c *gin.Context) {
	// This response contains live operational state and must not be cached by a
	// browser or an intermediary. Credentials and request bodies are excluded by
	// the service projection itself.
	c.Header("Cache-Control", "no-store")
	if h == nil || h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}

	status, err := h.opsService.GetReliabilityStatus(c.Request.Context())
	if err != nil {
		writeReliabilityStatusError(c, err)
		return
	}
	response.Success(c, status)
}

// UpdateCodexTurnStateRuntimeSettingsRequest requires a complete switch
// snapshot. Pointer fields distinguish an explicit false from an omitted key.
type UpdateCodexTurnStateRuntimeSettingsRequest struct {
	ProbeEnabled     *bool `json:"probe_enabled"`
	InjectionEnabled *bool `json:"injection_enabled"`
	// A pointer distinguishes an omitted pool (preserve the current pool) from
	// an explicit empty array (clear the pool).
	ProxyPoolURLs *[]string `json:"proxy_pool_urls"`
}

// codexTurnStateRuntimeSettingsResponse is deliberately separate from the
// internal settings type. ProxyPoolURLs may contain credentials and must
// never be sent back to an administrator browser or an intermediary. The
// client treats the proxy pool as write-only and uses these metadata fields
// to show whether a pool is configured without learning its secrets.
type codexTurnStateRuntimeSettingsResponse struct {
	ProbeEnabled        bool `json:"probe_enabled"`
	InjectionEnabled    bool `json:"injection_enabled"`
	ProxyPoolConfigured bool `json:"proxy_pool_configured"`
	ProxyPoolCount      int  `json:"proxy_pool_count"`
}

func projectCodexTurnStateRuntimeSettings(settings *service.CodexTurnStateRuntimeSettings) codexTurnStateRuntimeSettingsResponse {
	if settings == nil {
		return codexTurnStateRuntimeSettingsResponse{}
	}
	return codexTurnStateRuntimeSettingsResponse{
		ProbeEnabled:        settings.ProbeEnabled,
		InjectionEnabled:    settings.InjectionEnabled,
		ProxyPoolConfigured: len(settings.ProxyPoolURLs) > 0,
		ProxyPoolCount:      len(settings.ProxyPoolURLs),
	}
}

// GetCodexTurnStateRuntimeSettings returns the persisted controls used by the
// reliability workbench.
// GET /api/v1/admin/reliability/turn-state-settings
func (h *OpsHandler) GetCodexTurnStateRuntimeSettings(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	settings, err := h.opsService.GetCodexTurnStateRuntimeSettings(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to get Codex turn-state settings")
		return
	}
	response.Success(c, projectCodexTurnStateRuntimeSettings(settings))
}

// UpdateCodexTurnStateRuntimeSettings persists and immediately publishes both
// controls to the OpenAI gateway runtime.
// PUT /api/v1/admin/reliability/turn-state-settings
func (h *OpsHandler) UpdateCodexTurnStateRuntimeSettings(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}

	var req UpdateCodexTurnStateRuntimeSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ProbeEnabled == nil || req.InjectionEnabled == nil {
		response.BadRequest(c, "probe_enabled and injection_enabled are required boolean values")
		return
	}
	var proxyPoolURLs []string
	if req.ProxyPoolURLs != nil {
		normalized, err := service.NormalizeOpenAICodexTurnStateProxyPool(*req.ProxyPoolURLs)
		if err != nil {
			response.BadRequest(c, "proxy_pool_urls contains an invalid proxy URL")
			return
		}
		proxyPoolURLs = normalized
	}
	updated, err := h.opsService.UpdateCodexTurnStateRuntimeSettings(c.Request.Context(), service.CodexTurnStateRuntimeSettings{
		ProbeEnabled:     *req.ProbeEnabled,
		InjectionEnabled: *req.InjectionEnabled,
		ProxyPoolURLs:    proxyPoolURLs,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "Failed to update Codex turn-state settings")
		return
	}
	response.Success(c, projectCodexTurnStateRuntimeSettings(updated))
}

func writeReliabilityStatusError(c *gin.Context, err error) {
	if isOpsRealtimeRequestCanceled(c, err) || errors.Is(err, context.Canceled) {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		response.Error(c, http.StatusGatewayTimeout, "Reliability status collection timed out")
		return
	}
	response.ErrorFrom(c, err)
}
