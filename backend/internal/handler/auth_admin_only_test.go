package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestAuthHandlerAllowsOnlyAdministratorInteractiveLogin(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.AdminEmail = "admin@example.com"
	handler := &AuthHandler{authService: service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)}

	require.NoError(t, handler.ensureBackendModeAllowsUser(context.Background(), &service.User{
		Email:  "ADMIN@example.com",
		Role:   service.RoleAdmin,
		Status: service.StatusActive,
	}))

	err := handler.ensureBackendModeAllowsUser(context.Background(), &service.User{
		Email:  "user@example.com",
		Role:   service.RoleUser,
		Status: service.StatusActive,
	})
	require.Equal(t, http.StatusForbidden, infraerrors.Code(err))
	require.Equal(t, "ADMIN_ONLY_MODE", infraerrors.Reason(err))

	err = handler.ensureBackendModeAllowsUser(context.Background(), &service.User{
		Email:  "other-admin@example.com",
		Role:   service.RoleAdmin,
		Status: service.StatusActive,
	})
	require.Equal(t, http.StatusForbidden, infraerrors.Code(err))
	require.Equal(t, "ADMIN_ONLY_MODE", infraerrors.Reason(err))

	err = handler.ensureBackendModeAllowsNewUserLogin(context.Background())
	require.Equal(t, http.StatusForbidden, infraerrors.Code(err))
	require.Equal(t, "ADMIN_ONLY_MODE", infraerrors.Reason(err))
}

func TestAuthHandlerLogoutAcceptsEmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)

	(&AuthHandler{}).Logout(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, "Logged out successfully", resp.Data.Message)
}
