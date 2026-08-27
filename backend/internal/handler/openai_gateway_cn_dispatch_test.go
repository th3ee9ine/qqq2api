package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestAllowOpenAICompatibleMessagesDispatch_RetiredProvidersRejected(t *testing.T) {
	require.True(t, allowOpenAICompatibleMessagesDispatch(nil, nil), "无 key 保持放行")

	for _, platform := range []string{service.PlatformKimi, service.PlatformZhipu, service.PlatformDeepseek, service.PlatformGrok} {
		apiKey := &service.APIKey{Group: &service.Group{Platform: platform, AllowMessagesDispatch: false}}
		require.False(t, allowOpenAICompatibleMessagesDispatch(nil, apiKey), "platform=%s", platform)
	}

	// 非回归：openai 分组仍受开关控制。
	openaiOff := &service.APIKey{Group: &service.Group{Platform: service.PlatformOpenAI, AllowMessagesDispatch: false}}
	require.False(t, allowOpenAICompatibleMessagesDispatch(nil, openaiOff))
	openaiOn := &service.APIKey{Group: &service.Group{Platform: service.PlatformOpenAI, AllowMessagesDispatch: true}}
	require.True(t, allowOpenAICompatibleMessagesDispatch(nil, openaiOn))
}

func TestAllowOpenAICompatibleMessagesDispatch_CompositeResolvedTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newCompositeCtx := func(model string, allow bool) (*gin.Context, *service.APIKey) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite, AllowMessagesDispatch: allow}}
		ensureCompositeTargetPlatform(c, apiKey, model)
		return c, apiKey
	}

	// 退役目标不再解析，且必须 fail-closed。
	for _, model := range []string{"grok-4.3", "kimi-k2-thinking", "glm-5.2", "deepseek-v3.2"} {
		c, apiKey := newCompositeCtx(model, false)
		_, resolved := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.False(t, resolved, "model=%s", model)
		require.False(t, allowOpenAICompatibleMessagesDispatch(c, apiKey), "model=%s", model)
	}

	// 解析到 openai 目标：受 composite 分组自身开关控制。
	c, apiKey := newCompositeCtx("gpt-5.5", false)
	require.False(t, allowOpenAICompatibleMessagesDispatch(c, apiKey))
	c, apiKey = newCompositeCtx("gpt-5.5", true)
	require.True(t, allowOpenAICompatibleMessagesDispatch(c, apiKey))

	// 未解析出目标平台：保持拒绝，不放宽。
	cNone, _ := gin.CreateTestContext(httptest.NewRecorder())
	cNone.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	require.False(t, allowOpenAICompatibleMessagesDispatch(cNone,
		&service.APIKey{Group: &service.Group{Platform: service.PlatformComposite, AllowMessagesDispatch: false}}))
}

func TestResolveOpenAIMessagesDispatchMappedModel_CompositeRetiredTargetsFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, model := range []string{"kimi-k2-thinking", "glm-5.2", "deepseek-v3.2", "grok-4.3"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}
		ensureCompositeTargetPlatform(c, apiKey, model)

		require.Empty(t, resolveOpenAIMessagesDispatchMappedModel(c, apiKey, "claude-sonnet-4-5-20250929"), "model=%s", model)
	}
}
