package routes

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RegisterGatewayRoutes registers the retained Anthropic/OpenAI-compatible gateways.
func RegisterGatewayRoutes(
	r *gin.Engine,
	h *handler.Handlers,
	apiKeyAuth middleware.APIKeyAuthMiddleware,
	_ *service.APIKeyService,
	_ *service.SubscriptionService,
	opsService *service.OpsService,
	settingService *service.SettingService,
	compositeResolver *service.CompositeRouteResolver,
	cfg *config.Config,
) {
	bodyLimit := middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize)
	textBodyLimit := middleware.RequestBodyLimit(cfg.Gateway.TextMaxBodySize)
	clientRequestID := middleware.ClientRequestID()
	opsErrorLogger := handler.OpsErrorLoggerMiddleware(opsService)
	endpointNorm := handler.InboundEndpointMiddleware()
	compositeTarget := compositeTargetPlatformMiddleware(compositeResolver)

	// 未分组 Key 拦截中间件（按协议格式区分错误响应）
	requireGroupAnthropic := middleware.RequireGroupAssignment(settingService, middleware.AnthropicErrorWriter)

	isOpenAIResponsesCompatibleGatewayPlatform := func(c *gin.Context) bool {
		return getGroupPlatform(c) == service.PlatformOpenAI
	}
	countTokensHandler := func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.CountTokens(c)
		default:
			h.Gateway.CountTokens(c)
		}
	}
	modelsHandler := func(c *gin.Context) {
		if c.Query("client_version") != "" {
			switch getGroupPlatform(c) {
			case service.PlatformOpenAI, service.PlatformComposite:
				h.OpenAIGateway.CodexModels(c)
				return
			}
		}
		h.Gateway.Models(c)
	}
	isOpenAIOnlyEndpointGatewayPlatform := func(c *gin.Context) bool {
		return getGroupPlatform(c) == service.PlatformOpenAI
	}
	imagesHandler := func(c *gin.Context) {
		switch getGroupPlatform(c) {
		case service.PlatformOpenAI:
			h.OpenAIGateway.Images(c)
		default:
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Images API is not supported for this platform",
				},
			})
		}
	}
	// /responses/*subpath 的子路径会被转发到上游同名端点之后，因此在入口就拒掉
	// 不可转发的子路径，不让它进入调度与转发流程。可转发的判定见
	// service.IsForwardableOpenAIResponsesRequestPath 及 upstream_path_guard.go。
	guardResponsesSubpath := func(next gin.HandlerFunc) gin.HandlerFunc {
		return func(c *gin.Context) {
			if !service.IsForwardableOpenAIResponsesRequestPath(c) {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalPolicyDenied)
				c.AbortWithStatusJSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Unsupported responses subpath",
					},
				})
				return
			}
			if service.IsOpenAIResponsesInputTokensRequestPath(c) && isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.ResponsesInputTokens(c)
				return
			}
			next(c)
		}
	}

	// API网关（Claude API兼容）
	gateway := r.Group("/v1")
	gateway.Use(bodyLimit)
	gateway.Use(clientRequestID)
	gateway.Use(opsErrorLogger)
	gateway.Use(endpointNorm)
	gateway.Use(gin.HandlerFunc(apiKeyAuth))
	gateway.GET("/billing", h.Gateway.KeyBillingInfo)
	gateway.Use(compositeTarget)
	gateway.Use(requireGroupAnthropic)
	{
		// /v1/messages: auto-route based on group platform
		gateway.POST("/messages", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Messages(c)
				return
			}
			h.Gateway.Messages(c)
		})
		// /v1/messages/count_tokens bridges OpenAI and retains the native
		// Anthropic-compatible path for Anthropic groups.
		gateway.POST("/messages/count_tokens", countTokensHandler)
		// Codex CLI / Codex app refresh their model picker from the provider's
		// /models endpoint with a client_version query and expect the ChatGPT
		// Codex manifest format; other clients keep the OpenAI-style list.
		gateway.GET("/models", modelsHandler)
		gateway.GET("/usage", h.Gateway.Usage)
		gateway.POST("/live", h.OpenAIGateway.Live)
		gateway.GET("/live/:call_id", h.OpenAIGateway.LiveSideband)
		// OpenAI Responses API: auto-route based on group platform
		gateway.POST("/responses", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		})
		gateway.POST("/responses/*subpath", guardResponsesSubpath(func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.Responses(c)
				return
			}
			h.Gateway.Responses(c)
		}))
		gateway.POST("/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
		gateway.GET("/responses", func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		// OpenAI Chat Completions API: auto-route based on group platform
		gateway.POST("/chat/completions", func(c *gin.Context) {
			if isOpenAIResponsesCompatibleGatewayPlatform(c) {
				h.OpenAIGateway.ChatCompletions(c)
				return
			}
			h.Gateway.ChatCompletions(c)
		})
		gateway.POST("/embeddings", textBodyLimit, func(c *gin.Context) {
			if !isOpenAIOnlyEndpointGatewayPlatform(c) {
				service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
				c.JSON(http.StatusNotFound, gin.H{
					"error": gin.H{
						"type":    "not_found_error",
						"message": "Embeddings API is not supported for this platform",
					},
				})
				return
			}
			h.OpenAIGateway.Embeddings(c)
		})
		gateway.POST("/images/generations", imagesHandler)
		gateway.POST("/images/edits", imagesHandler)
		gateway.POST("/images/generations/async", h.AsyncImage.Submit)
		gateway.POST("/images/edits/async", h.AsyncImage.Submit)
		gateway.GET("/images/tasks/:task_id", h.AsyncImage.Get)
	}

	// OpenAI Responses API（不带v1前缀的别名）— auto-route based on group platform
	responsesHandler := func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			h.OpenAIGateway.Responses(c)
			return
		}
		h.Gateway.Responses(c)
	}
	r.POST("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, responsesHandler)
	r.POST("/responses/*subpath", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, guardResponsesSubpath(responsesHandler))
	r.POST("/alpha/search", textBodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.OpenAIGateway.AlphaSearch)
	r.GET("/responses", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		h.OpenAIGateway.ResponsesWebSocket(c)
	})
	r.GET("/models", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), requireGroupAnthropic, modelsHandler)
	r.POST("/messages/count_tokens", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, countTokensHandler)
	codexDirect := r.Group("/backend-api/codex")
	codexDirect.Use(bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic)
	{
		codexDirect.POST("/realtime/calls", h.OpenAIGateway.Live)
		codexDirect.GET("/:call_id", h.OpenAIGateway.LiveSideband)
		codexDirect.POST("/responses", responsesHandler)
		codexDirect.POST("/responses/*subpath", guardResponsesSubpath(responsesHandler))
		codexDirect.POST("/alpha/search", textBodyLimit, h.OpenAIGateway.AlphaSearch)
		codexDirect.GET("/responses", func(c *gin.Context) {
			h.OpenAIGateway.ResponsesWebSocket(c)
		})
		codexDirect.GET("/models", h.OpenAIGateway.CodexModels)
	}
	// OpenAI Chat Completions API（不带v1前缀的别名）— auto-route based on group platform
	r.POST("/chat/completions", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if isOpenAIResponsesCompatibleGatewayPlatform(c) {
			h.OpenAIGateway.ChatCompletions(c)
			return
		}
		h.Gateway.ChatCompletions(c)
	})
	r.POST("/embeddings", textBodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, func(c *gin.Context) {
		if !isOpenAIOnlyEndpointGatewayPlatform(c) {
			service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
			c.JSON(http.StatusNotFound, gin.H{
				"error": gin.H{
					"type":    "not_found_error",
					"message": "Embeddings API is not supported for this platform",
				},
			})
			return
		}
		h.OpenAIGateway.Embeddings(c)
	})
	r.POST("/images/generations", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, imagesHandler)
	r.POST("/images/edits", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, imagesHandler)
	r.POST("/images/generations/async", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.AsyncImage.Submit)
	r.POST("/images/edits/async", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.AsyncImage.Submit)
	r.GET("/images/tasks/:task_id", bodyLimit, clientRequestID, opsErrorLogger, endpointNorm, gin.HandlerFunc(apiKeyAuth), compositeTarget, requireGroupAnthropic, h.AsyncImage.Get)
}

// getGroupPlatform extracts the group platform from the API Key stored in context.
func getGroupPlatform(c *gin.Context) string {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey.Group == nil {
		return ""
	}
	if apiKey.Group.Platform == service.PlatformComposite {
		if platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context()); ok {
			return platform
		}
	}
	return apiKey.Group.Platform
}

func compositeTargetPlatformMiddleware(resolver *service.CompositeRouteResolver) gin.HandlerFunc {
	if resolver == nil {
		resolver = service.NewCompositeRouteResolver(nil)
	}
	return func(c *gin.Context) {
		apiKey, ok := middleware.GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || apiKey.Group == nil || apiKey.Group.Platform != service.PlatformComposite {
			c.Next()
			return
		}
		if c.Request == nil || c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
		if err != nil {
			status := http.StatusBadRequest
			message := "Failed to read request body"
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
				message = "Request body is too large"
			}
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "message": message}})
			c.Abort()
			return
		}

		model := compositeRequestModelFromBody(c.GetHeader("Content-Type"), body)
		if model != "" {
			decision, err := resolver.Resolve(c.Request.Context(), apiKey.Group.ID, model, compositeRouteEndpointForPath(c.Request.URL.Path))
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "server_error", "message": "Failed to resolve composite model route"}})
				c.Abort()
				return
			}
			if decision.Matched {
				c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), decision))
				if upstreamModel := strings.TrimSpace(decision.UpstreamModel); upstreamModel != "" && upstreamModel != model && gjson.ValidBytes(body) {
					if _, modelPath := compositeJSONRequestModel(body); modelPath != "" {
						if rewritten, rewriteErr := sjson.SetBytes(body, modelPath, upstreamModel); rewriteErr == nil {
							body = rewritten
						}
					}
				}
			}
		}
		resetRequestBody(c, body)
		c.Next()
	}
}

func compositeRequestModelFromBody(contentType string, body []byte) string {
	if model, _ := compositeJSONRequestModel(body); model != "" {
		return model
	}
	return compositeMultipartModelFromBody(contentType, body)
}

func compositeJSONRequestModel(body []byte) (string, string) {
	for _, path := range []string{"model", "session.model"} {
		model := gjson.GetBytes(body, path)
		if model.Type != gjson.String {
			continue
		}
		if value := strings.TrimSpace(model.String()); value != "" {
			return value, path
		}
	}
	return "", ""
}

func compositeMultipartModelFromBody(contentType string, body []byte) string {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil || !strings.EqualFold(mediaType, "multipart/form-data") {
		return ""
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return ""
	}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return ""
		}
		if err != nil {
			return ""
		}
		fieldName := part.FormName()
		if part.FileName() != "" || (fieldName != "model" && fieldName != "session") {
			continue
		}
		data, err := io.ReadAll(part)
		if err != nil {
			return ""
		}
		switch fieldName {
		case "model":
			return strings.TrimSpace(string(data))
		case "session":
			if model, _ := compositeJSONRequestModel(data); model != "" {
				return model
			}
		}
	}
}

func resetRequestBody(c *gin.Context, body []byte) {
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Length", strconv.Itoa(len(body)))
}

func compositeRouteEndpointForPath(path string) string {
	switch {
	case strings.Contains(path, "/messages/count_tokens"):
		return service.CompositeRouteEndpointCountTokens
	case strings.Contains(path, "/messages"):
		return service.CompositeRouteEndpointMessages
	case strings.Contains(path, "/responses"),
		strings.Contains(path, "/alpha/search"),
		strings.Contains(path, "/realtime/calls"),
		strings.HasSuffix(strings.TrimRight(path, "/"), "/live"):
		return service.CompositeRouteEndpointResponses
	case strings.Contains(path, "/chat/completions"):
		return service.CompositeRouteEndpointChatCompletions
	case strings.Contains(path, "/embeddings"):
		return service.CompositeRouteEndpointEmbeddings
	case strings.Contains(path, "/images/"):
		return service.CompositeRouteEndpointImages
	default:
		return service.CompositeRouteEndpointAny
	}
}
