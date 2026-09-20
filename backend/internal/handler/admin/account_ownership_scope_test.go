package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type accountOwnershipScopeAdminService struct {
	*stubAdminService
	getAccountErr    error
	getAccountResult *service.Account
	getAccountIDs    []int64
}

func (s *accountOwnershipScopeAdminService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	s.getAccountIDs = append(s.getAccountIDs, id)
	if s.getAccountErr != nil {
		return nil, s.getAccountErr
	}
	if s.getAccountResult != nil {
		return s.getAccountResult, nil
	}
	ownerID, _ := ctxkey.AccountAdminIDFromContext(ctx)
	return &service.Account{ID: id, AccountAdminID: &ownerID}, nil
}

func accountOwnershipScopeRouter(t *testing.T, svc *accountOwnershipScopeAdminService, scoped bool, path string, next *bool) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/accounts/data", h.AccountOwnershipScope(), func(c *gin.Context) {
		*next = true
		c.Status(http.StatusNoContent)
	})
	router.GET("/accounts/:id", h.AccountOwnershipScope(), func(c *gin.Context) {
		*next = true
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, path, nil)
	if scoped {
		request = request.WithContext(context.WithValue(request.Context(), ctxkey.AccountAdminID, int64(41)))
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestAccountOwnershipScopeChecksPositiveAccountIDsBeforeHandler(t *testing.T) {
	serviceStub := &accountOwnershipScopeAdminService{stubAdminService: newStubAdminService()}
	next := false
	response := accountOwnershipScopeRouter(t, serviceStub, true, "/accounts/42", &next)

	require.Equal(t, http.StatusNoContent, response.Code)
	require.True(t, next)
	require.Equal(t, []int64{42}, serviceStub.getAccountIDs)
}

func TestAccountOwnershipScopeStopsMissingAccountsBeforeHandler(t *testing.T) {
	serviceStub := &accountOwnershipScopeAdminService{
		stubAdminService: newStubAdminService(),
		getAccountErr:    service.ErrAccountNotFound,
	}
	next := false
	response := accountOwnershipScopeRouter(t, serviceStub, true, "/accounts/42", &next)

	require.Equal(t, http.StatusNotFound, response.Code)
	require.False(t, next)
	require.Contains(t, response.Body.String(), "ACCOUNT_NOT_FOUND")
}

func TestAccountOwnershipScopeStopsUnexpectedAccountBeforeHandler(t *testing.T) {
	serviceStub := &accountOwnershipScopeAdminService{
		stubAdminService: newStubAdminService(),
		getAccountResult: &service.Account{ID: 7},
	}
	next := false
	response := accountOwnershipScopeRouter(t, serviceStub, true, "/accounts/42", &next)

	require.Equal(t, http.StatusNotFound, response.Code)
	require.False(t, next)
	require.Equal(t, []int64{42}, serviceStub.getAccountIDs)
}

func TestAccountOwnershipScopeStopsForeignOwnerBeforeHandler(t *testing.T) {
	foreignOwnerID := int64(73)
	serviceStub := &accountOwnershipScopeAdminService{
		stubAdminService: newStubAdminService(),
		getAccountResult: &service.Account{ID: 42, AccountAdminID: &foreignOwnerID},
	}
	next := false
	response := accountOwnershipScopeRouter(t, serviceStub, true, "/accounts/42", &next)

	require.Equal(t, http.StatusNotFound, response.Code)
	require.False(t, next)
	require.Contains(t, response.Body.String(), "ACCOUNT_NOT_FOUND")
}

func TestAccountOwnershipScopePropagatesLookupErrorsAndDoesNotRunHandler(t *testing.T) {
	lookupErr := errors.New("database unavailable")
	serviceStub := &accountOwnershipScopeAdminService{
		stubAdminService: newStubAdminService(),
		getAccountErr:    lookupErr,
	}
	next := false
	response := accountOwnershipScopeRouter(t, serviceStub, true, "/accounts/42", &next)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.False(t, next)
}

func TestAccountOwnershipScopeSkipsUnscopedAndStaticOrInvalidRoutes(t *testing.T) {
	tests := []struct {
		name   string
		scoped bool
		path   string
	}{
		{name: "super administrator context", scoped: false, path: "/accounts/42"},
		{name: "static route", scoped: true, path: "/accounts/data"},
		{name: "zero account id", scoped: true, path: "/accounts/0"},
		{name: "malformed account id", scoped: true, path: "/accounts/not-an-id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceStub := &accountOwnershipScopeAdminService{stubAdminService: newStubAdminService()}
			next := false
			response := accountOwnershipScopeRouter(t, serviceStub, tt.scoped, tt.path, &next)

			require.Equal(t, http.StatusNoContent, response.Code)
			require.True(t, next)
			require.Empty(t, serviceStub.getAccountIDs)
		})
	}
}
