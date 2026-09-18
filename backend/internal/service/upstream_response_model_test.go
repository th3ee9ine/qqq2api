package service

import (
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamResponseModelObserverTerminalWinsAndRecordsConflict(t *testing.T) {
	observer := &upstreamResponseModelObserver{}

	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-5.5"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5.4"}}`), "response.completed")

	require.Equal(t, "gpt-5.4", observer.Model())
	require.True(t, observer.Conflict())
}

func TestUpstreamResponseModelObserverTreatsAstraLifecycleVariantsAsEquivalent(t *testing.T) {
	observer := &upstreamResponseModelObserver{}

	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"openai/gpt-6-astra-2026-09-18"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-6"}}`), "response.completed")

	require.Equal(t, "gpt-6", observer.Model())
	require.False(t, observer.Conflict())
}

func TestUpstreamResponseModelObserverSupportsAnthropicAndGeminiShapes(t *testing.T) {
	t.Run("anthropic", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveAnthropic([]byte(`{"type":"message_start","message":{"model":"claude-sonnet-4-20250514"}}`))
		require.Equal(t, "claude-sonnet-4-20250514", observer.Model())
	})

	t.Run("gemini outer and nested", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveGemini([]byte(`{"response":{"modelVersion":"gemini-2.5-pro"}}`))
		observer.ObserveGemini([]byte(`{"modelVersion":"gemini-2.5-pro-latest"}`))
		require.Equal(t, "gemini-2.5-pro-latest", observer.Model())
		require.True(t, observer.Conflict())
	})
}

func TestUpstreamResponseModelObservationAttemptReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)

	first := beginUpstreamResponseModelObservation(c)
	first.Observe("failed-attempt-model", false)
	second := beginUpstreamResponseModelObservation(c)
	second.Observe("successful-attempt-model", false)

	require.Equal(t, "successful-attempt-model", observedUpstreamResponseModel(c))
	require.False(t, observedUpstreamResponseModelConflict(c))
}

func TestUpstreamModelMismatchThreeStateAndCaseInsensitiveComparison(t *testing.T) {
	require.Nil(t, upstreamModelMismatch("gpt-5.5", ""))

	matched := upstreamModelMismatch("gpt-5.5", "GPT-5.5")
	require.NotNil(t, matched)
	require.False(t, *matched)

	mismatched := upstreamModelMismatch("gpt-5.5", "gpt-5.4")
	require.NotNil(t, mismatched)
	require.True(t, *mismatched)
}

func TestUpstreamModelMismatchTreatsGrokBuildRuntimeIDsAsAliases(t *testing.T) {
	tests := []struct {
		name          string
		sentModel     string
		responseModel string
	}{
		{
			name:          "issue 5634 grok 4.6",
			sentModel:     "grok-4.6",
			responseModel: "grok-4.6-build",
		},
		{
			name:          "grok 4.6 latest",
			sentModel:     "grok-4.6-latest",
			responseModel: "grok-4.6-build",
		},
		{
			name:          "issue 5647 grok 4.5 latest",
			sentModel:     "grok-4.5-latest",
			responseModel: "grok-4.5-build",
		},
		{
			name:          "grok 4.5 canonical",
			sentModel:     "grok-4.5",
			responseModel: "GROK-4.5-BUILD",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mismatch := upstreamModelMismatch(tt.sentModel, tt.responseModel)

			require.NotNil(t, mismatch)
			require.False(t, *mismatch)
		})
	}
}

func TestUpstreamModelMismatchKeepsCodexAutoReviewAsItsOwnRuntime(t *testing.T) {
	matched := upstreamModelMismatch("codex-auto-review", "codex-auto-review")
	require.NotNil(t, matched)
	require.False(t, *matched)

	astra := upstreamModelMismatch("codex-auto-review", "gpt-6-astra")
	require.NotNil(t, astra)
	require.True(t, *astra)

	luna := upstreamModelMismatch("codex-auto-review", "gpt-5.6-luna")
	require.NotNil(t, luna)
	require.True(t, *luna)

	direct := upstreamModelMismatch("gpt-6-astra", "gpt-6-astra")
	require.NotNil(t, direct)
	require.False(t, *direct)

	prefixed := upstreamModelMismatch("codex-auto-review", "provider/codex-auto-review")
	require.NotNil(t, prefixed)
	require.False(t, *prefixed)

	prefixedSelf := upstreamModelMismatch("provider/codex-auto-review", "provider/codex-auto-review")
	require.NotNil(t, prefixedSelf)
	require.False(t, *prefixedSelf)
}

func TestUpstreamModelMismatchTreatsAstraVariantsAsOneRuntime(t *testing.T) {
	tests := []struct {
		name          string
		sentModel     string
		responseModel string
	}{
		{name: "base to dated", sentModel: "gpt-6-astra", responseModel: "gpt-6-astra-2026-09-18"},
		{name: "dated to base", sentModel: "gpt-6-astra-2026-09-18", responseModel: "gpt-6-astra"},
		{name: "provider build to public alias", sentModel: "openai/gpt-6-astra-build-42", responseModel: "gpt-6"},
		{name: "public alias to provider dated", sentModel: "gpt-6", responseModel: "provider/gpt-6-astra-2026-09-18"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mismatch := upstreamModelMismatch(tt.sentModel, tt.responseModel)
			require.NotNil(t, mismatch)
			require.False(t, *mismatch)
		})
	}

	for _, degraded := range []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra"} {
		mismatch := upstreamModelMismatch("gpt-6-astra", degraded)
		require.NotNil(t, mismatch)
		require.True(t, *mismatch, degraded)
	}
}

func TestUpstreamModelMismatchDoesNotCollapseDifferentModels(t *testing.T) {
	tests := []struct {
		name          string
		sentModel     string
		responseModel string
	}{
		{
			name:          "different grok versions",
			sentModel:     "grok-4.5",
			responseModel: "grok-4.6-build",
		},
		{
			name:          "unrelated build suffix",
			sentModel:     "gpt-5.5",
			responseModel: "gpt-5.5-build",
		},
		{
			name:          "different grok runtime",
			sentModel:     "grok-build-0.1",
			responseModel: "grok-4.5-build",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mismatch := upstreamModelMismatch(tt.sentModel, tt.responseModel)

			require.NotNil(t, mismatch)
			require.True(t, *mismatch)
		})
	}
}

func TestObserveOpenAISSEBodyIgnoresMalformedPayload(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observeOpenAISSEBody(observer, "data: not-json\n\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-5.4\"}}\n\n")

	require.Equal(t, "gpt-5.4", observer.Model())
	require.False(t, observer.Conflict())
}

func TestObserveAntigravityGeminiSSELineReadsWrapperModelWithoutUnwrap(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{
			name:    "top-level sibling",
			payload: `{"modelVersion":"gemini-3-pro","response":{"candidates":[]}}`,
			want:    "gemini-3-pro",
		},
		{
			name:    "single wrapper",
			payload: `{"response":{"modelVersion":"gemini-3-pro","candidates":[]}}`,
			want:    "gemini-3-pro",
		},
		{
			name:    "nested response after one wrapper",
			payload: `{"response":{"response":{"modelVersion":"gemini-3-pro","candidates":[]}}}`,
			want:    "gemini-3-pro",
		},
		{
			name:    "outer declaration takes precedence",
			payload: `{"modelVersion":"gemini-outer","response":{"modelVersion":"gemini-inner","candidates":[]}}`,
			want:    "gemini-outer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			c, _ := gin.CreateTestContext(nil)
			beginUpstreamResponseModelObservation(c)

			svc := &AntigravityGatewayService{}
			svc.observeAntigravityGeminiSSELine(c, "data: "+tt.payload)

			require.Equal(t, tt.want, observedUpstreamResponseModel(c))
			require.False(t, observedUpstreamResponseModelConflict(c))
		})
	}
}

func TestUpstreamResponseModelObserverRejectsMalformedJSONWithModelField(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.ObserveOpenAI([]byte(`{"response":{"model":"gpt-5.4"}`), "response.completed")

	require.Empty(t, observer.Model())
}

func TestUpstreamResponseModelObserverBoundsUntrustedModelName(t *testing.T) {
	observer := &upstreamResponseModelObserver{}
	observer.Observe("  "+strings.Repeat("模", upstreamResponseModelMaxLength+1)+"  ", false)

	require.Len(t, []rune(observer.Model()), upstreamResponseModelMaxLength)
}

func TestUpstreamResponseModelObserverServiceTierTerminalEventWins(t *testing.T) {
	observer := &upstreamResponseModelObserver{}

	// response.created echoes the requested tier and must not count as a declaration.
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"id":"resp_1","model":"gpt-5.6-sol","service_tier":"priority"}}`), "response.created")
	require.Equal(t, "", observer.ServiceTier())

	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","service_tier":"default","usage":{"input_tokens":1,"output_tokens":2}}}`), "response.completed")
	require.Equal(t, "default", observer.ServiceTier())
	require.False(t, observer.Conflict(), "tier echo must not be reported as a model conflict")
}

func TestUpstreamResponseModelObserverServiceTierUntypedPayloads(t *testing.T) {
	t.Run("chat completions chunks agree", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveOpenAI([]byte(`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.4","service_tier":"default","choices":[]}`), "")
		observer.ObserveOpenAI([]byte(`{"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-5.4","service_tier":"default","choices":[]}`), "")
		require.Equal(t, "default", observer.ServiceTier())
	})

	t.Run("disagreeing chunks are not trusted", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveOpenAI([]byte(`{"model":"gpt-5.4","service_tier":"priority"}`), "")
		observer.ObserveOpenAI([]byte(`{"model":"gpt-5.4","service_tier":"default"}`), "")
		require.Equal(t, "", observer.ServiceTier())
	})

	t.Run("non-stream body normalises fast and drops auto", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveOpenAI([]byte(`{"id":"resp_1","object":"response","model":"gpt-5.6-sol","service_tier":"fast"}`), "")
		require.Equal(t, "priority", observer.ServiceTier())

		auto := &upstreamResponseModelObserver{}
		auto.ObserveOpenAI([]byte(`{"id":"resp_2","object":"response","model":"gpt-5.6-sol","service_tier":"auto"}`), "")
		require.Equal(t, "", auto.ServiceTier())
	})

	t.Run("model-free deltas never declare a tier", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveOpenAI([]byte(`{"type":"response.output_text.delta","delta":"hi","service_tier":"default"}`), "response.output_text.delta")
		require.Equal(t, "", observer.ServiceTier())
	})
}

func TestUpstreamResponseModelObserverServiceTierAnthropicSpeed(t *testing.T) {
	t.Run("message_start reports standard", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveAnthropic([]byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":8,"output_tokens":1,"speed":"standard"}}}`))
		observer.ObserveAnthropic([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`))
		require.Equal(t, "standard", observer.ServiceTier())
		require.Equal(t, "claude-opus-5", observer.Model())
	})

	t.Run("non-stream body reports fast", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveAnthropic([]byte(`{"id":"msg_1","type":"message","model":"claude-opus-5","usage":{"input_tokens":8,"output_tokens":12,"speed":"fast"}}`))
		require.Equal(t, "fast", observer.ServiceTier())
	})

	t.Run("missing speed declares nothing", func(t *testing.T) {
		observer := &upstreamResponseModelObserver{}
		observer.ObserveAnthropic([]byte(`{"id":"msg_1","type":"message","model":"claude-opus-5","usage":{"input_tokens":8,"output_tokens":12}}`))
		require.Equal(t, "", observer.ServiceTier())
	})
}

func TestObservedUpstreamResponseServiceTierFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	require.Equal(t, "", observedUpstreamResponseServiceTier(c))

	observer := beginUpstreamResponseModelObservation(c)
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"id":"resp_1","model":"gpt-5.6-sol","service_tier":"default"}}`), "response.completed")
	require.Equal(t, "default", observedUpstreamResponseServiceTier(c))
}

func TestReplaceOpenAIWSMessageModelPreservesAcceptanceFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
	}{
		{name: "luna cannot masquerade as astra", from: "gpt-5.6-luna", to: "gpt-6-astra"},
		{name: "dated astra stays raw", from: "openai/gpt-6-astra-2026-09-18", to: "gpt-6-astra"},
		{name: "luna cannot masquerade as auto review", from: "gpt-5.6-luna", to: "codex-auto-review"},
		{name: "auto review variant stays raw", from: "provider/codex-auto-review-2026-09-18", to: "codex-auto-review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := []byte(`{"type":"response.completed","response":{"model":"` + tc.from + `"}}`)
			require.Equal(t, string(message), string(replaceOpenAIWSMessageModel(message, tc.from, tc.to)))
		})
	}

	ordinary := []byte(`{"type":"response.completed","response":{"model":"gpt-5.5"}}`)
	require.JSONEq(t,
		`{"type":"response.completed","response":{"model":"client-alias"}}`,
		string(replaceOpenAIWSMessageModel(ordinary, "gpt-5.5", "client-alias")),
	)
}
