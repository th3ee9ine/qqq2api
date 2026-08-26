package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountAdminHandlerServiceStub struct {
	*stubAdminService
	targetRole string

	createInput *service.CreateUserInput
	updateInput *service.UpdateUserInput
	updateID    int64
	updateCalls int
	deleteID    int64
	deleteCalls int
}

func newAccountAdminHandlerServiceStub() *accountAdminHandlerServiceStub {
	return &accountAdminHandlerServiceStub{stubAdminService: newStubAdminService()}
}

func (s *accountAdminHandlerServiceStub) CreateUser(_ context.Context, input *service.CreateUserInput) (*service.User, error) {
	copied := *input
	s.createInput = &copied
	return &service.User{
		ID:          100,
		Email:       input.Email,
		Username:    input.Username,
		Notes:       input.Notes,
		Role:        input.Role,
		Balance:     *input.Balance,
		Concurrency: input.Concurrency,
		RPMLimit:    input.RPMLimit,
		Status:      service.StatusActive,
	}, nil
}

func (s *accountAdminHandlerServiceStub) GetUser(_ context.Context, id int64) (*service.User, error) {
	return &service.User{
		ID:     id,
		Email:  "target@example.com",
		Role:   s.targetRole,
		Status: service.StatusActive,
	}, nil
}

func (s *accountAdminHandlerServiceStub) UpdateUser(_ context.Context, id int64, input *service.UpdateUserInput) (*service.User, error) {
	s.updateCalls++
	s.updateID = id
	copied := *input
	s.updateInput = &copied
	return &service.User{ID: id, Email: input.Email, Role: service.RoleAccountAdmin, Status: service.StatusActive}, nil
}

func (s *accountAdminHandlerServiceStub) DeleteUser(_ context.Context, id int64) error {
	s.deleteCalls++
	s.deleteID = id
	return nil
}

func newAccountAdminHandlerRouter(t *testing.T, svc service.AdminService, actorID int64) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if actorID > 0 {
		router.Use(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: actorID})
			c.Next()
		})
	}
	h := NewAccountAdminHandler(svc)
	router.GET("/api/v1/admin/account-admins", h.List)
	router.POST("/api/v1/admin/account-admins", h.Create)
	router.PUT("/api/v1/admin/account-admins/:id", h.Update)
	router.DELETE("/api/v1/admin/account-admins/:id", h.Delete)
	return router
}

func performAccountAdminJSONRequest(t *testing.T, router http.Handler, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestAccountAdminHandlerCreateForcesRestrictedOperatorFields(t *testing.T) {
	svc := newAccountAdminHandlerServiceStub()
	router := newAccountAdminHandlerRouter(t, svc, 88)

	response := performAccountAdminJSONRequest(t, router, http.MethodPost, "/api/v1/admin/account-admins", map[string]any{
		"email":          "operator@example.com",
		"password":       "pass123",
		"username":       "  operator  ",
		"notes":          "restricted operator",
		"role":           service.RoleAdmin,
		"balance":        999,
		"concurrency":    99,
		"rpm_limit":      999,
		"actor_admin_id": 999,
	})

	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	require.NotNil(t, svc.createInput)
	require.Equal(t, "operator@example.com", svc.createInput.Email)
	require.Equal(t, "operator", svc.createInput.Username)
	require.Equal(t, service.RoleAccountAdmin, svc.createInput.Role)
	require.NotNil(t, svc.createInput.Balance)
	require.Zero(t, *svc.createInput.Balance)
	require.Zero(t, svc.createInput.Concurrency)
	require.Zero(t, svc.createInput.RPMLimit)
	require.Equal(t, int64(88), svc.createInput.ActorAdminID)
}

func TestAccountAdminHandlerCreateRejectsPasswordOverBcryptLimit(t *testing.T) {
	svc := newAccountAdminHandlerServiceStub()
	router := newAccountAdminHandlerRouter(t, svc, 88)

	response := performAccountAdminJSONRequest(t, router, http.MethodPost, "/api/v1/admin/account-admins", map[string]any{
		"email": "operator@example.com",
		// 25 runes pass the validator's character limit but occupy 75 UTF-8
		// bytes, which bcrypt cannot accept.
		"password": strings.Repeat("密", 25),
	})

	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	require.Nil(t, svc.createInput)
}

func TestAccountAdminHandlerListAlwaysFiltersRestrictedOperators(t *testing.T) {
	svc := newAccountAdminHandlerServiceStub()
	svc.users = []service.User{{
		ID:     7,
		Email:  "operator@example.com",
		Role:   service.RoleAccountAdmin,
		Status: service.StatusActive,
	}}
	router := newAccountAdminHandlerRouter(t, svc, 88)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/account-admins?page=2&page_size=5&role=admin&status=active&search=%20%20ops%20%20", nil)
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, 1, svc.lastListUsers.calls)
	require.Equal(t, 2, svc.lastListUsers.page)
	require.Equal(t, 5, svc.lastListUsers.pageSize)
	require.Equal(t, service.RoleAccountAdmin, svc.lastListUsers.filters.Role)
	require.Equal(t, service.StatusActive, svc.lastListUsers.filters.Status)
	require.Equal(t, "ops", svc.lastListUsers.filters.Search)
	require.NotNil(t, svc.lastListUsers.filters.IncludeSubscriptions)
	require.False(t, *svc.lastListUsers.filters.IncludeSubscriptions)
	require.Equal(t, "created_at", svc.lastListUsers.sortBy)
	require.Equal(t, "desc", svc.lastListUsers.sortOrder)
}

func TestAccountAdminHandlerUpdateAndDeleteRejectNonAccountAdminTargets(t *testing.T) {
	for _, targetRole := range []string{service.RoleAdmin, service.RoleUser} {
		for _, operation := range []struct {
			name   string
			method string
		}{
			{name: "update", method: http.MethodPut},
			{name: "delete", method: http.MethodDelete},
		} {
			t.Run(targetRole+"/"+operation.name, func(t *testing.T) {
				svc := newAccountAdminHandlerServiceStub()
				svc.targetRole = targetRole
				router := newAccountAdminHandlerRouter(t, svc, 88)
				response := performAccountAdminJSONRequest(t, router, operation.method, "/api/v1/admin/account-admins/42", map[string]any{
					"email": "changed@example.com",
				})

				require.Equal(t, http.StatusNotFound, response.Code, response.Body.String())
				require.Zero(t, svc.updateCalls, "non-account-admin target must not reach UpdateUser")
				require.Zero(t, svc.deleteCalls, "non-account-admin target must not reach DeleteUser")
			})
		}
	}
}
