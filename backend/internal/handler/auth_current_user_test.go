//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAuthHandlerGetCurrentUserReturnsProfileCompatibilityFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	repo := &userHandlerRepoStub{
		user: &service.User{
			ID:       31,
			Email:    "admin@example.com",
			Username: "admin",
			Role:     service.RoleAdmin,
			Status:   service.StatusActive,
		},
	}

	cfg := &config.Config{}
	cfg.Default.AdminEmail = "admin@example.com"
	handler := &AuthHandler{
		authService: service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil),
		userService: service.NewUserService(repo, nil, nil, nil),
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 31})

	handler.GetCurrentUser(c)

	require.Equal(t, http.StatusOK, recorder.Code)

	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, true, resp.Data["email_bound"])
	require.Equal(t, false, resp.Data["linuxdo_bound"])
	require.Equal(t, "admin", resp.Data["role"])
}

func TestAuthHandlerGetCurrentUserRejectsOtherDatabaseAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &userHandlerRepoStub{user: &service.User{
		ID: 2, Email: "other-admin@example.com", Role: service.RoleAdmin, Status: service.StatusActive,
	}}
	cfg := &config.Config{}
	cfg.Default.AdminEmail = "admin@example.com"
	handler := &AuthHandler{
		authService: service.NewAuthService(nil, repo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil),
		userService: service.NewUserService(repo, nil, nil, nil),
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 2})

	handler.GetCurrentUser(c)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ADMIN_ONLY_MODE")
}
