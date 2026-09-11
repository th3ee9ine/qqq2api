package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type debugWorkbenchHandlerSource struct{ gets int }

func (s *debugWorkbenchHandlerSource) GetAccount(context.Context, int64) (*service.Account, error) {
	s.gets++
	return nil, nil
}
func (s *debugWorkbenchHandlerSource) GetProxy(context.Context, int64) (*service.Proxy, error) {
	return nil, nil
}

func TestDebugWorkbenchHandlerStrictValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name, id, body string
		authenticated  bool
		status         int
	}{
		{"unauthenticated", "7", `{"endpoint":"responses","body":{}}`, false, http.StatusUnauthorized},
		{"invalid account", "no", `{"endpoint":"responses","body":{}}`, true, http.StatusBadRequest},
		{"zero account", "0", `{"endpoint":"responses","body":{}}`, true, http.StatusBadRequest},
		{"trailing json", "7", `{"endpoint":"responses","body":{}} {}`, true, http.StatusBadRequest},
		{"unknown envelope", "7", `{"endpoint":"responses","body":{},"url":"https://injected"}`, true, http.StatusBadRequest},
		{"wrong header value", "7", `{"endpoint":"responses","body":{},"headers":{"x-test":17}}`, true, http.StatusBadRequest},
		{"non-object body", "7", `{"endpoint":"responses","body":[]}`, true, http.StatusBadRequest},
		{"unsupported endpoint", "7", `{"endpoint":"files","body":{}}`, true, http.StatusBadRequest},
		{"crlf header", "7", `{"endpoint":"responses","body":{},"headers":{"x-test":"value\r\nInjected: x"}}`, true, http.StatusBadRequest},
		{"oversized envelope", "7", `{"endpoint":"responses","body":{"input":"` + strings.Repeat("x", service.DebugWorkbenchMaxEnvelopeBytes) + `"}}`, true, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := &debugWorkbenchHandlerSource{}
			h := &AccountHandler{}
			h.SetDebugWorkbenchService(service.NewDebugWorkbenchService(&service.OpenAIGatewayService{}, source))
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Params = gin.Params{{Key: "id", Value: tt.id}}
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/"+tt.id+"/debug", strings.NewReader(tt.body))
			c.Request.Header.Set("Authorization", "Bearer administrator-secret")
			if tt.authenticated {
				c.Set("user", middleware.AuthSubject{UserID: 23})
			}
			h.Debug(c)
			require.Equal(t, tt.status, recorder.Code, recorder.Body.String())
			require.Zero(t, source.gets)
			require.NotContains(t, recorder.Body.String(), "administrator-secret")
			var envelope map[string]any
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Equal(t, float64(tt.status), envelope["code"])
		})
	}
}

func TestDebugWorkbenchHandlerRequiresConfiguredService(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Params = gin.Params{{Key: "id", Value: "7"}}
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/7/debug", bytes.NewBufferString(`{"endpoint":"responses","body":{}}`))
	c.Set("user", middleware.AuthSubject{UserID: 23})
	(&AccountHandler{}).Debug(c)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
