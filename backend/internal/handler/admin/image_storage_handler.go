package admin

import (
	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// ImageStorageHandler manages the object storage used by asynchronous OpenAI
// image tasks. It is intentionally independent from database backup storage.
type ImageStorageHandler struct {
	imageStorage *service.ImageStorageSettingService
}

func NewImageStorageHandler(imageStorage *service.ImageStorageSettingService) *ImageStorageHandler {
	return &ImageStorageHandler{imageStorage: imageStorage}
}

// Get returns the current image-storage configuration with the secret masked.
func (h *ImageStorageHandler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := h.imageStorage.Get(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"config":            cfg,
		"secret_configured": h.imageStorage.SecretConfigured(ctx),
	})
}

// Update persists the standalone image-storage configuration and applies it
// immediately. SecretAccessKey is never echoed in the response.
func (h *ImageStorageHandler) Update(c *gin.Context) {
	var req service.ImageStorageSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.imageStorage.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// TestConnection validates the supplied standalone image-storage settings
// without persisting them.
func (h *ImageStorageHandler) TestConnection(c *gin.Context) {
	var req service.ImageStorageSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.imageStorage.TestConnection(c.Request.Context(), req); err != nil {
		response.Success(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	response.Success(c, gin.H{"ok": true, "message": "connection successful"})
}
