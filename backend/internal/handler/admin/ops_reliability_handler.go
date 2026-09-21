package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
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
