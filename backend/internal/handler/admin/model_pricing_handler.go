package admin

import (
	"fmt"
	"strings"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ModelPricingHandler exposes the read-only model pricing lookup used by the
// group pricing editor. Channel management remains detached from the router.
type ModelPricingHandler struct {
	billingService *service.BillingService
}

func NewModelPricingHandler(billingService *service.BillingService) *ModelPricingHandler {
	return &ModelPricingHandler{billingService: billingService}
}

// GetDefaultPricing returns catalog prices in per-token units for the retained
// Claude/OpenAI group platforms.
// GET /api/v1/admin/channels/model-pricing?platform=anthropic&model=claude-sonnet-4
func (h *ModelPricingHandler) GetDefaultPricing(c *gin.Context) {
	platform := strings.ToLower(strings.TrimSpace(c.Query("platform")))
	if platform == "" {
		response.ErrorFrom(c, infraerrors.BadRequest("MISSING_PARAMETER", "platform parameter is required").
			WithMetadata(map[string]string{"param": "platform"}))
		return
	}
	if platform != service.PlatformAnthropic && platform != service.PlatformOpenAI {
		response.ErrorFrom(c, infraerrors.BadRequest("UNSUPPORTED_PLATFORM",
			fmt.Sprintf("unsupported platform: %s", platform)).
			WithMetadata(map[string]string{"param": "platform"}))
		return
	}

	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		response.ErrorFrom(c, infraerrors.BadRequest("MISSING_PARAMETER", "model parameter is required").
			WithMetadata(map[string]string{"param": "model"}))
		return
	}

	pricing, err := h.billingService.GetModelPricing(model)
	if err != nil {
		response.Success(c, gin.H{"found": false})
		return
	}

	response.Success(c, gin.H{
		"found":              true,
		"input_price":        pricing.InputPricePerToken,
		"output_price":       pricing.OutputPricePerToken,
		"cache_write_price":  pricing.CacheCreationPricePerToken,
		"cache_read_price":   pricing.CacheReadPricePerToken,
		"image_input_price":  pricing.ImageInputPricePerToken,
		"image_output_price": pricing.ImageOutputPricePerToken,
	})
}
