package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func (h *AccountHandler) SetDebugWorkbenchService(debug *service.DebugWorkbenchService) {
	h.debugWorkbench = debug
}

// Debug executes the full editor payload using the selected account's gateway.
func (h *AccountHandler) Debug(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Error(c, http.StatusUnauthorized, "authenticated administrator required")
		return
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "invalid account ID")
		return
	}
	if h.debugWorkbench == nil {
		response.Error(c, http.StatusServiceUnavailable, "debug workbench service is not configured")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.DebugWorkbenchMaxEnvelopeBytes)
	input, err := service.DecodeDebugWorkbenchRequest(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(c, http.StatusRequestEntityTooLarge, "debug request exceeds 9 MiB")
			return
		}
		response.BadRequest(c, "invalid debug JSON request")
		return
	}
	result, err := h.debugWorkbench.Run(c.Request.Context(), subject.UserID, accountID, input)
	if err != nil {
		var inputErr *service.DebugWorkbenchInputError
		if errors.As(err, &inputErr) {
			response.Error(c, inputErr.StatusCode, inputErr.Message)
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
