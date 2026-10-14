package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestGetConnectionPoolStatsNoStoreAndAvailability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		svc  *service.OpsService
		code int
	}{
		{name: "available", svc: service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil), code: http.StatusOK},
		{name: "disabled", svc: service.NewOpsService(nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil), code: http.StatusNotFound},
		{name: "missing", code: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/connection-pools", NewOpsHandler(tc.svc).GetConnectionPoolStats)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/connection-pools", nil))
			require.Equal(t, tc.code, recorder.Code)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			if tc.code == http.StatusOK {
				var payload struct {
					Data service.OpsConnectionPools `json:"data"`
				}
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
				require.Equal(t, "instance", payload.Data.Scope)
				require.NotEmpty(t, payload.Data.InstanceID)
				require.Contains(t, recorder.Body.String(), `"database":null`)
				require.Contains(t, recorder.Body.String(), `"redis":null`)
			}
		})
	}
}
