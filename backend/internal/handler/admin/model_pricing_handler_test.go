//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupModelPricingRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewModelPricingHandler(service.NewBillingService(nil, nil))
	router.GET("/channels/model-pricing", handler.GetDefaultPricing)
	return router
}

func TestModelPricingHandlerReturnsRetainedPlatformDefaults(t *testing.T) {
	router := setupModelPricingRouter()

	for _, query := range []string{
		"platform=anthropic&model=claude-sonnet-4",
		"platform=openai&model=gpt-5.4",
	} {
		req := httptest.NewRequest(http.MethodGet, "/channels/model-pricing?"+query, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, query)
		var body struct {
			Data struct {
				Found       bool    `json:"found"`
				InputPrice  float64 `json:"input_price"`
				OutputPrice float64 `json:"output_price"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.True(t, body.Data.Found)
		require.Positive(t, body.Data.InputPrice)
		require.Positive(t, body.Data.OutputPrice)
	}
}

func TestModelPricingHandlerReturnsNotFoundWithoutBlockingEditor(t *testing.T) {
	router := setupModelPricingRouter()
	req := httptest.NewRequest(http.MethodGet,
		"/channels/model-pricing?platform=openai&model=unknown-model-without-pricing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"found":false}}`, w.Body.String())
}

func TestModelPricingHandlerRejectsMissingOrRetiredPlatforms(t *testing.T) {
	router := setupModelPricingRouter()
	for _, platform := range []string{"", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek"} {
		path := "/channels/model-pricing?model=claude-sonnet-4"
		if platform != "" {
			path += "&platform=" + platform
		}
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, platform)
	}
}
