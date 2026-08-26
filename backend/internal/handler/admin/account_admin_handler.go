package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// AccountAdminHandler exposes a deliberately narrow operator-management API.
// The role is fixed server-side so this surface can never create or modify the
// configured super administrator.
type AccountAdminHandler struct {
	adminService service.AdminService
}

func NewAccountAdminHandler(adminService service.AdminService) *AccountAdminHandler {
	return &AccountAdminHandler{adminService: adminService}
}

type CreateAccountAdminRequest struct {
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=6,max=72"`
	Username string `json:"username" binding:"omitempty,max=100"`
	Notes    string `json:"notes"`
}

type UpdateAccountAdminRequest struct {
	Email    string  `json:"email" binding:"omitempty,email,max=255"`
	Password string  `json:"password" binding:"omitempty,min=6,max=72"`
	Username *string `json:"username" binding:"omitempty,max=100"`
	Notes    *string `json:"notes"`
	Status   string  `json:"status" binding:"omitempty,oneof=active disabled"`
}

// List returns only restricted account administrators.
func (h *AccountAdminHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	search := strings.TrimSpace(c.Query("search"))
	if runes := []rune(search); len(runes) > 100 {
		search = string(runes[:100])
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != service.StatusActive && status != service.StatusDisabled {
		response.BadRequest(c, "Invalid status")
		return
	}

	noSubscriptions := false
	users, total, err := h.adminService.ListUsers(c.Request.Context(), page, pageSize, service.UserListFilters{
		Role:                 service.RoleAccountAdmin,
		Status:               status,
		Search:               search,
		IncludeSubscriptions: &noSubscriptions,
	}, "created_at", "desc")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]*dto.AdminUser, 0, len(users))
	for i := range users {
		out = append(out, dto.UserFromServiceAdmin(&users[i]))
	}
	response.Paginated(c, out, total, page, pageSize)
}

// Create creates an active account administrator. Balance and concurrency are
// explicitly zero because this identity is not used for gateway traffic.
func (h *AccountAdminHandler) Create(c *gin.Context) {
	var req CreateAccountAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len([]byte(req.Password)) > 72 {
		response.BadRequest(c, "Password must not exceed 72 bytes")
		return
	}

	zeroBalance := 0.0
	user, err := h.adminService.CreateUser(c.Request.Context(), &service.CreateUserInput{
		Email:        strings.TrimSpace(req.Email),
		Password:     req.Password,
		Username:     strings.TrimSpace(req.Username),
		Notes:        req.Notes,
		Role:         service.RoleAccountAdmin,
		Balance:      &zeroBalance,
		Concurrency:  0,
		RPMLimit:     0,
		ActorAdminID: getAdminIDFromContext(c),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, dto.UserFromServiceAdmin(user))
}

// Update changes profile, password, or status for an account administrator.
// Role changes are intentionally absent from the request contract.
func (h *AccountAdminHandler) Update(c *gin.Context) {
	userID, ok := parseAccountAdminID(c)
	if !ok {
		return
	}
	if !h.requireAccountAdminTarget(c, userID) {
		return
	}

	var req UpdateAccountAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len([]byte(req.Password)) > 72 {
		response.BadRequest(c, "Password must not exceed 72 bytes")
		return
	}
	if req.Username != nil {
		trimmed := strings.TrimSpace(*req.Username)
		req.Username = &trimmed
	}

	user, err := h.adminService.UpdateUser(c.Request.Context(), userID, &service.UpdateUserInput{
		Email:        strings.TrimSpace(req.Email),
		Password:     req.Password,
		Username:     req.Username,
		Notes:        req.Notes,
		Status:       req.Status,
		ActorAdminID: getAdminIDFromContext(c),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.UserFromServiceAdmin(user))
}

// Delete removes an account administrator without exposing general user CRUD.
func (h *AccountAdminHandler) Delete(c *gin.Context) {
	userID, ok := parseAccountAdminID(c)
	if !ok {
		return
	}
	if !h.requireAccountAdminTarget(c, userID) {
		return
	}
	if err := h.adminService.DeleteUser(c.Request.Context(), userID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Account administrator deleted successfully"})
}

func parseAccountAdminID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account administrator ID")
		return 0, false
	}
	return id, true
}

func (h *AccountAdminHandler) requireAccountAdminTarget(c *gin.Context, id int64) bool {
	user, err := h.adminService.GetUser(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	if user == nil || user.Role != service.RoleAccountAdmin {
		response.NotFound(c, "Account administrator not found")
		return false
	}
	return true
}
