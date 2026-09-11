package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestGetOpenAITestDefaultsReturnsCompleteRedactedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"responses", "chat/completions", "images/generations"} {
		t.Run(endpoint, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/test-defaults?endpoint="+endpoint, nil)
			(&AccountHandler{}).GetOpenAITestDefaults(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Data service.OpenAITestDefaults `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, "api.openai.com", response.Data.Headers["Host"])
			require.Equal(t, "Bearer ••••••••", response.Data.Headers["Authorization"])
			require.Equal(t, "application/json", response.Data.Headers["Content-Type"])
			require.Equal(t, "<generated-per-request>", response.Data.Headers["X-Client-Request-Id"])
			require.NotEmpty(t, response.Data.Notes)
			require.NotEmpty(t, response.Data.HeaderDetails)
			var requestIDExplained bool
			for _, detail := range response.Data.HeaderDetails {
				if detail.Name == "X-Client-Request-Id" {
					requestIDExplained = true
					require.True(t, detail.DefaultIncluded)
					require.Equal(t, "recommended", detail.Requirement)
					require.NotEmpty(t, detail.Purpose)
				}
			}
			require.True(t, requestIDExplained)
			if endpoint == "responses" {
				require.Equal(t, "text/event-stream", response.Data.Headers["Accept"])
				require.NotEmpty(t, response.Data.Headers["User-Agent"])
				require.NotEmpty(t, response.Data.Headers["Originator"])
				require.NotEmpty(t, response.Data.Headers["Version"])
				require.Equal(t, "<generated-per-request>", response.Data.Headers["X-Codex-Window-Id"])
			}
		})
	}
}

func TestGetOpenAITestDefaultsExplicitDirectProxy(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/test-defaults?endpoint=responses&proxy_id=0", nil)
	(&AccountHandler{}).GetOpenAITestDefaults(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	var envelope struct {
		Data service.OpenAITestDefaults `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	require.Nil(t, envelope.Data.ProxyID)
	require.Empty(t, envelope.Data.ProxyURL)
	require.Contains(t, envelope.Data.Notes, "调试请求显式使用直连，不使用账号绑定代理")
}
