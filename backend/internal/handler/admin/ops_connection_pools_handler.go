package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
)

// GetConnectionPoolStats returns live counters for the instance serving this
// request. GET /api/v1/admin/ops/runtime/connection-pools
func (h *OpsHandler) GetConnectionPoolStats(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if h == nil || h.opsService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Ops service not available")
		return
	}
	if err := h.opsService.RequireMonitoringEnabled(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.opsService.GetConnectionPoolStats())
}
