package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	middleware2 "github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestCompositeTargetPlatformAllowedResolvesKnownAllowedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

	require.True(t, compositeTargetPlatformAllowed(c, apiKey, "text-embedding-3-large", service.PlatformOpenAI))
	platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformOpenAI, platform)
}

func TestOpenAICompatibleTextTargetAllowsOpenAIAndGrok(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{"/v1/messages", "/v1/chat/completions", "/v1/responses", "/v1/responses/input_tokens", "/v1/messages/count_tokens"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", path, nil)
		apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

		require.True(t, openAICompatibleTextTargetAllowed(c, apiKey, "gpt-5.5"), "path=%s", path)
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok, "path=%s", path)
		require.Equal(t, service.PlatformOpenAI, platform, "path=%s", path)

		grokCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		grokCtx.Request = httptest.NewRequest("POST", path, nil)
		require.True(t, openAICompatibleTextTargetAllowed(grokCtx, apiKey, "grok-4.7"), "path=%s", path)
		grokPlatform, resolved := service.ResolvedTargetPlatformFromContext(grokCtx.Request.Context())
		require.True(t, resolved)
		require.Equal(t, service.PlatformGrok, grokPlatform)

		for _, model := range []string{"kimi-k2-thinking", "glm-5.2", "glm-v3.2", "gemini-2.5-flash"} {
			retiredCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
			retiredCtx.Request = httptest.NewRequest("POST", path, nil)
			require.False(t, openAICompatibleTextTargetAllowed(retiredCtx, apiKey, model), "path=%s model=%s", path, model)
		}
	}
}

func TestResponsesWebSocketCompositePlatformGuardAllowsOpenAIAndGrok(t *testing.T) {
	require.True(t, isResponsesWebSocketCompositePlatform(service.PlatformOpenAI))
	require.True(t, isResponsesWebSocketCompositePlatform(service.PlatformGrok))
	for _, platform := range []string{
		service.PlatformKimi, service.PlatformZhipu,
		service.PlatformAnthropic, service.PlatformGemini,
	} {
		require.False(t, isResponsesWebSocketCompositePlatform(platform), "platform=%s", platform)
	}
}

func TestCompositeGrokMessagesDispatchPreservesAccountModelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformGrok))
	key := &service.APIKey{Group: &service.Group{
		Platform:              service.PlatformComposite,
		AllowMessagesDispatch: false,
		MessagesDispatchModelConfig: service.OpenAIMessagesDispatchModelConfig{
			SonnetMappedModel: "gpt-5.4",
		},
	}}

	require.True(t, allowOpenAICompatibleMessagesDispatch(c, key))
	require.Empty(t, resolveOpenAIMessagesDispatchMappedModel(c, key, "claude-sonnet-4-6"),
		"Grok targets must use their account mapping without inheriting OpenAI group defaults")

	c.Request = c.Request.WithContext(service.WithResolvedTargetPlatform(c.Request.Context(), service.PlatformOpenAI))
	require.False(t, allowOpenAICompatibleMessagesDispatch(c, key))
	key.Group.AllowMessagesDispatch = true
	require.True(t, allowOpenAICompatibleMessagesDispatch(c, key))
	require.Equal(t, "gpt-5.4", resolveOpenAIMessagesDispatchMappedModel(c, key, "claude-sonnet-4-6"))
}

func TestCompositeTargetPlatformAllowedRejectsWrongOrUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name  string
		model string
	}{
		{name: "wrong provider", model: "claude-sonnet-4-5"},
		{name: "unknown provider", model: "llama-4-maverick"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/embeddings", nil)
			apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

			require.False(t, compositeTargetPlatformAllowed(c, apiKey, tc.model, service.PlatformOpenAI))
		})
	}
}

func TestCompositeTargetPlatformResolvedRejectsUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformComposite}}

	require.False(t, compositeTargetPlatformResolved(c, apiKey, "llama-4-maverick"))
	_, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.False(t, ok)
}

func TestCompositeTargetPlatformResolvedAllowsConcreteGroupWithoutResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	apiKey := &service.APIKey{Group: &service.Group{Platform: service.PlatformAnthropic}}

	require.True(t, compositeTargetPlatformResolved(c, apiKey, "llama-4-maverick"))
}

func TestOpenAIReasoningEffortPolicyForCompositeTarget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := &service.Group{
		Platform:           service.PlatformComposite,
		MaxReasoningEffort: "medium",
		ReasoningEffortMappings: []service.ReasoningEffortMapping{
			{From: "max", To: "xhigh"},
		},
	}
	apiKey := &service.APIKey{Group: group}
	body := []byte(`{"reasoning":{"effort":"max"}}`)

	openAICtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	openAICtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	openAICtx.Request = openAICtx.Request.WithContext(service.WithResolvedTargetPlatform(openAICtx.Request.Context(), service.PlatformOpenAI))
	got, changed, err := applyOpenAIReasoningEffortPolicyForRequest(openAICtx, apiKey, body)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"reasoning":{"effort":"medium"}}`, string(got))
	requested := service.RequestedReasoningEffortFromContext(openAICtx.Request.Context())
	require.NotNil(t, requested)
	require.Equal(t, "max", *requested)

	bindOpenAIReasoningEffortPolicyForMessagesRequest(openAICtx, apiKey, []byte(`{"output_config":{"effort":"max"}}`))
	bound, changed, err := service.ApplyOpenAIReasoningEffortPolicyFromContext(openAICtx.Request.Context(), body)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"reasoning":{"effort":"medium"}}`, string(bound))

	omittedCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	omittedCtx.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	omittedCtx.Request = omittedCtx.Request.WithContext(service.WithResolvedTargetPlatform(omittedCtx.Request.Context(), service.PlatformOpenAI))
	bindOpenAIReasoningEffortPolicyForMessagesRequest(omittedCtx, apiKey, []byte(`{"model":"gpt-5"}`))
	omitted, changed, err := service.ApplyOpenAIReasoningEffortPolicyFromContext(omittedCtx.Request.Context(), body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, omitted)

	denyGroup := *group
	denyGroup.MaxReasoningEffortOverLimit = service.ReasoningEffortOverLimitDeny
	denyAPIKey := &service.APIKey{Group: &denyGroup}
	denyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	denyCtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	denyCtx.Request = denyCtx.Request.WithContext(service.WithResolvedTargetPlatform(denyCtx.Request.Context(), service.PlatformOpenAI))
	_, _, err = applyOpenAIReasoningEffortPolicyForRequest(denyCtx, denyAPIKey, body)
	require.Error(t, err)
	var overLimit *service.ReasoningEffortOverLimitError
	require.ErrorAs(t, err, &overLimit)

	mappingDenyGroup := *group
	mappingDenyGroup.MaxReasoningEffort = ""
	mappingDenyGroup.ReasoningEffortMappings = []service.ReasoningEffortMapping{
		{From: "max", To: service.ReasoningEffortMappingDeny},
	}
	mappingDenyAPIKey := &service.APIKey{Group: &mappingDenyGroup}
	mappingDenyCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	mappingDenyCtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	mappingDenyCtx.Request = mappingDenyCtx.Request.WithContext(service.WithResolvedTargetPlatform(mappingDenyCtx.Request.Context(), service.PlatformOpenAI))
	_, changed, err = applyOpenAIReasoningEffortPolicyForRequest(mappingDenyCtx, mappingDenyAPIKey, body)
	require.Error(t, err)
	require.False(t, changed)
	var mappingDenied *service.ReasoningEffortMappingDeniedError
	require.ErrorAs(t, err, &mappingDenied)
	require.Equal(t, "max", mappingDenied.Requested)
	require.Contains(t, mappingDenied.Error(), "denied by this group's mapping policy")

	grokCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	grokCtx.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	grokCtx.Request = grokCtx.Request.WithContext(service.WithResolvedTargetPlatform(grokCtx.Request.Context(), service.PlatformGrok))
	got, changed, err = applyOpenAIReasoningEffortPolicyForRequest(grokCtx, apiKey, body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, body, got)
}

func TestClientRequestedModelUsesCompositePublicModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request = c.Request.WithContext(service.WithCompositeRouteDecision(c.Request.Context(), service.CompositeRouteDecision{
		Matched:        true,
		Source:         service.CompositeRouteSourceExplicit,
		PublicModel:    "public-alias",
		TargetPlatform: service.PlatformOpenAI,
		UpstreamModel:  "gpt-5",
	}))

	input := buildContentModerationInput(c, nil, middleware2.AuthSubject{UserID: 42}, service.ContentModerationProtocolOpenAIChat, "gpt-5", nil)
	require.Equal(t, "public-alias", input.Model)
	require.Equal(t, service.PlatformOpenAI, input.Provider)

	fields := clientRequestedUsageFields(c, service.ChannelMappingResult{MappedModel: "gpt-5"}, "gpt-5", "gpt-5")
	require.Equal(t, "public-alias", fields.OriginalModel)
	require.Equal(t, "public-alias", fields.ChannelMappedModel)
	require.Equal(t, "public-alias\u2192gpt-5", fields.ModelMappingChain)
}
