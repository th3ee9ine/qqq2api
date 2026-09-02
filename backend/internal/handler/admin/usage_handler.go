package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/handler/dto"
	"github.com/th3ee9ine/qqq2api/internal/pkg/logger"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/pkg/timezone"
	"github.com/th3ee9ine/qqq2api/internal/pkg/usagestats"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
)

// UsageHandler handles admin usage-related requests
type UsageHandler struct {
	usageService   *service.UsageService
	apiKeyService  *service.APIKeyService
	adminService   service.AdminService
	cleanupService *service.UsageCleanupService
}

// NewUsageHandler creates a new admin usage handler
func NewUsageHandler(
	usageService *service.UsageService,
	apiKeyService *service.APIKeyService,
	adminService service.AdminService,
	cleanupService *service.UsageCleanupService,
) *UsageHandler {
	return &UsageHandler{
		usageService:   usageService,
		apiKeyService:  apiKeyService,
		adminService:   adminService,
		cleanupService: cleanupService,
	}
}

// CreateUsageCleanupTaskRequest represents cleanup task creation request
type CreateUsageCleanupTaskRequest struct {
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	RecordType  string  `json:"record_type"`
	APIKeyID    *int64  `json:"api_key_id"`
	AccountID   *int64  `json:"account_id"`
	GroupID     *int64  `json:"group_id"`
	Model       *string `json:"model"`
	RequestType *string `json:"request_type"`
	Stream      *bool   `json:"stream"`
	BillingType *int8   `json:"billing_type"`
	Timezone    string  `json:"timezone"`
	// Error-tab filters.  They are applied only to ops_error_logs; usage-log
	// filters remain shared with the original cleanup task.
	ErrorPhase    *string `json:"error_phase"`
	ErrorCategory *string `json:"error_category"`
	StatusCode    *int    `json:"status_code"`
	StatusCodes   []int   `json:"status_codes"`
}

// List handles listing all usage records with filters
// GET /api/v1/admin/usage
func (h *UsageHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	exactTotal := false
	if exactTotalRaw := strings.TrimSpace(c.Query("exact_total")); exactTotalRaw != "" {
		parsed, err := strconv.ParseBool(exactTotalRaw)
		if err != nil {
			response.BadRequest(c, "Invalid exact_total value, use true or false")
			return
		}
		exactTotal = parsed
	}
	skipCount := false
	if skipCountRaw := strings.TrimSpace(c.Query("skip_count")); skipCountRaw != "" {
		parsed, err := strconv.ParseBool(skipCountRaw)
		if err != nil {
			response.BadRequest(c, "Invalid skip_count value, use true or false")
			return
		}
		skipCount = parsed
	}

	// Parse filters
	var apiKeyID, accountID, groupID int64

	if apiKeyIDStr := c.Query("api_key_id"); apiKeyIDStr != "" {
		id, err := strconv.ParseInt(apiKeyIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid api_key_id")
			return
		}
		apiKeyID = id
	}

	if accountIDStr := c.Query("account_id"); accountIDStr != "" {
		id, err := strconv.ParseInt(accountIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid account_id")
			return
		}
		accountID = id
	}

	if groupIDStr := c.Query("group_id"); groupIDStr != "" {
		id, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		groupID = id
	}

	model := c.Query("model")
	requestID := strings.TrimSpace(c.Query("request_id"))
	billingMode := strings.TrimSpace(c.Query("billing_mode"))

	var requestType *int16
	var stream *bool
	if requestTypeStr := strings.TrimSpace(c.Query("request_type")); requestTypeStr != "" {
		parsed, err := service.ParseUsageRequestType(requestTypeStr)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		value := int16(parsed)
		requestType = &value
	} else if streamStr := c.Query("stream"); streamStr != "" {
		val, err := strconv.ParseBool(streamStr)
		if err != nil {
			response.BadRequest(c, "Invalid stream value, use true or false")
			return
		}
		stream = &val
	}

	nativeCompactionV2, err := parseOptionalBoolDashboardFilter(c, "native_compaction_v2")
	if err != nil {
		response.BadRequest(c, "Invalid native_compaction_v2 value, use true or false")
		return
	}

	var billingType *int8
	if billingTypeStr := c.Query("billing_type"); billingTypeStr != "" {
		val, err := strconv.ParseInt(billingTypeStr, 10, 8)
		if err != nil {
			response.BadRequest(c, "Invalid billing_type")
			return
		}
		bt := int8(val)
		billingType = &bt
	}

	var upstreamModelMismatch *bool
	if raw := strings.TrimSpace(c.Query("upstream_model_mismatch")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			response.BadRequest(c, "Invalid upstream_model_mismatch value, use true or false")
			return
		}
		upstreamModelMismatch = &value
	}

	// Parse date range
	var startTime, endTime *time.Time
	userTZ := c.Query("timezone") // Get user's timezone from request
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		t, err := timezone.ParseInUserLocation("2006-01-02", startDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
			return
		}
		startTime = &t
	}

	if endDateStr := c.Query("end_date"); endDateStr != "" {
		t, err := timezone.ParseInUserLocation("2006-01-02", endDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
			return
		}
		// Use half-open range [start, end), move to next calendar day start (DST-safe).
		t = t.AddDate(0, 0, 1)
		endTime = &t
	}

	params := pagination.PaginationParams{
		Page:      page,
		PageSize:  pageSize,
		SortBy:    c.DefaultQuery("sort_by", "created_at"),
		SortOrder: c.DefaultQuery("sort_order", "desc"),
	}
	filters := usagestats.UsageLogFilters{
		UserID:                0,
		APIKeyID:              apiKeyID,
		AccountID:             accountID,
		GroupID:               groupID,
		RequestID:             requestID,
		Model:                 model,
		ModelFilterSource:     usagestats.ModelSourceRequested,
		RequestType:           requestType,
		Stream:                stream,
		NativeCompactionV2:    nativeCompactionV2,
		BillingType:           billingType,
		BillingMode:           billingMode,
		UpstreamModelMismatch: upstreamModelMismatch,
		StartTime:             startTime,
		EndTime:               endTime,
		ExactTotal:            exactTotal,
		SkipCount:             skipCount,
	}

	records, result, err := h.usageService.ListWithFilters(c.Request.Context(), params, filters)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	out := make([]dto.AdminUsageLog, 0, len(records))
	for i := range records {
		out = append(out, *dto.UsageLogFromServiceAdmin(&records[i]))
	}
	response.Paginated(c, out, result.Total, page, pageSize)
}

// Stats handles getting usage statistics with filters
// GET /api/v1/admin/usage/stats
func (h *UsageHandler) Stats(c *gin.Context) {
	// Parse filters - same as List endpoint
	var apiKeyID, accountID, groupID int64

	if apiKeyIDStr := c.Query("api_key_id"); apiKeyIDStr != "" {
		id, err := strconv.ParseInt(apiKeyIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid api_key_id")
			return
		}
		apiKeyID = id
	}

	if accountIDStr := c.Query("account_id"); accountIDStr != "" {
		id, err := strconv.ParseInt(accountIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid account_id")
			return
		}
		accountID = id
	}

	if groupIDStr := c.Query("group_id"); groupIDStr != "" {
		id, err := strconv.ParseInt(groupIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid group_id")
			return
		}
		groupID = id
	}

	model := c.Query("model")
	billingMode := strings.TrimSpace(c.Query("billing_mode"))

	var requestType *int16
	var stream *bool
	if requestTypeStr := strings.TrimSpace(c.Query("request_type")); requestTypeStr != "" {
		parsed, err := service.ParseUsageRequestType(requestTypeStr)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		value := int16(parsed)
		requestType = &value
	} else if streamStr := c.Query("stream"); streamStr != "" {
		val, err := strconv.ParseBool(streamStr)
		if err != nil {
			response.BadRequest(c, "Invalid stream value, use true or false")
			return
		}
		stream = &val
	}

	nativeCompactionV2, err := parseOptionalBoolDashboardFilter(c, "native_compaction_v2")
	if err != nil {
		response.BadRequest(c, "Invalid native_compaction_v2 value, use true or false")
		return
	}

	var billingType *int8
	if billingTypeStr := c.Query("billing_type"); billingTypeStr != "" {
		val, err := strconv.ParseInt(billingTypeStr, 10, 8)
		if err != nil {
			response.BadRequest(c, "Invalid billing_type")
			return
		}
		bt := int8(val)
		billingType = &bt
	}

	var upstreamModelMismatch *bool
	if raw := strings.TrimSpace(c.Query("upstream_model_mismatch")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			response.BadRequest(c, "Invalid upstream_model_mismatch value, use true or false")
			return
		}
		upstreamModelMismatch = &value
	}

	// Parse date range
	userTZ := c.Query("timezone")
	now := timezone.NowInUserLocation(userTZ)
	var startTime, endTime time.Time

	startDateStr := c.Query("start_date")
	endDateStr := c.Query("end_date")

	if startDateStr != "" && endDateStr != "" {
		var err error
		startTime, err = timezone.ParseInUserLocation("2006-01-02", startDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
			return
		}
		endTime, err = timezone.ParseInUserLocation("2006-01-02", endDateStr, userTZ)
		if err != nil {
			response.BadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
			return
		}
		// 与 SQL 条件 created_at < end 对齐，使用次日 00:00 作为上边界（DST-safe）。
		endTime = endTime.AddDate(0, 0, 1)
	} else {
		period := c.DefaultQuery("period", "today")
		switch period {
		case "today":
			startTime = timezone.StartOfDayInUserLocation(now, userTZ)
		case "week":
			startTime = now.AddDate(0, 0, -7)
		case "month":
			startTime = now.AddDate(0, -1, 0)
		default:
			startTime = timezone.StartOfDayInUserLocation(now, userTZ)
		}
		endTime = now
	}

	// Build filters and call GetStatsWithFilters
	filters := usagestats.UsageLogFilters{
		UserID:                0,
		APIKeyID:              apiKeyID,
		AccountID:             accountID,
		GroupID:               groupID,
		Model:                 model,
		ModelFilterSource:     usagestats.ModelSourceRequested,
		RequestType:           requestType,
		Stream:                stream,
		NativeCompactionV2:    nativeCompactionV2,
		BillingType:           billingType,
		BillingMode:           billingMode,
		UpstreamModelMismatch: upstreamModelMismatch,
		StartTime:             &startTime,
		EndTime:               &endTime,
	}

	var stats *usagestats.UsageStats
	// nocache: 绕过缓存直接回源,刷新者本人拿最新;不回写缓存(管理台"我刷新我自己拿最新"语义,非全局失效)。
	if parseBoolQueryWithDefault(c.Query("nocache"), false) {
		s, err := h.usageService.GetStatsWithFilters(c.Request.Context(), filters)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		stats = s
		c.Header("X-Usage-Stats-Cache", "bypass")
	} else {
		s, hit, err := h.getStatsCached(c.Request.Context(), filters)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		stats = s
		c.Header("X-Usage-Stats-Cache", cacheStatusValue(hit))
	}

	response.Success(c, stats)
}

// SearchUsers handles searching users by email keyword
// GET /api/v1/admin/usage/search-users
func (h *UsageHandler) SearchUsers(c *gin.Context) {
	keyword := c.Query("q")
	if keyword == "" {
		response.Success(c, []any{})
		return
	}

	// Limit to 30 results
	users, _, err := h.adminService.ListUsers(c.Request.Context(), 1, 30, service.UserListFilters{Search: keyword, IncludeDeleted: true}, "email", "asc")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Return simplified user list (only id, email and deleted flag)
	type SimpleUser struct {
		ID      int64  `json:"id"`
		Email   string `json:"email"`
		Deleted bool   `json:"deleted"`
	}

	result := make([]SimpleUser, len(users))
	for i, u := range users {
		result[i] = SimpleUser{
			ID:      u.ID,
			Email:   u.Email,
			Deleted: u.DeletedAt != nil,
		}
	}

	response.Success(c, result)
}

// SearchAPIKeys handles searching API keys by user
// GET /api/v1/admin/usage/search-api-keys
func (h *UsageHandler) SearchAPIKeys(c *gin.Context) {
	userIDStr := c.Query("user_id")
	keyword := c.Query("q")

	var userID int64
	if userIDStr != "" {
		id, err := strconv.ParseInt(userIDStr, 10, 64)
		if err != nil {
			response.BadRequest(c, "Invalid user_id")
			return
		}
		userID = id
	}

	keys, err := h.apiKeyService.SearchAPIKeys(c.Request.Context(), userID, keyword, 30)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Return simplified API key list (only id and name)
	type SimpleAPIKey struct {
		ID     int64  `json:"id"`
		Name   string `json:"name"`
		UserID int64  `json:"user_id"`
	}

	result := make([]SimpleAPIKey, len(keys))
	for i, k := range keys {
		result[i] = SimpleAPIKey{
			ID:     k.ID,
			Name:   k.Name,
			UserID: k.UserID,
		}
	}

	response.Success(c, result)
}

// ListCleanupTasks handles listing usage cleanup tasks
// GET /api/v1/admin/usage/cleanup-tasks
func (h *UsageHandler) ListCleanupTasks(c *gin.Context) {
	if h.cleanupService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Usage cleanup service unavailable")
		return
	}
	operator := int64(0)
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		operator = subject.UserID
	}
	page, pageSize := response.ParsePagination(c)
	logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 请求清理任务列表: operator=%d page=%d page_size=%d", operator, page, pageSize)
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	tasks, result, err := h.cleanupService.ListTasks(c.Request.Context(), params)
	if err != nil {
		logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 查询清理任务列表失败: operator=%d page=%d page_size=%d err=%v", operator, page, pageSize, err)
		response.ErrorFrom(c, err)
		return
	}
	out := make([]dto.UsageCleanupTask, 0, len(tasks))
	for i := range tasks {
		out = append(out, *dto.UsageCleanupTaskFromService(&tasks[i]))
	}
	logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 返回清理任务列表: operator=%d total=%d items=%d page=%d page_size=%d", operator, result.Total, len(out), page, pageSize)
	response.Paginated(c, out, result.Total, page, pageSize)
}

// CreateCleanupTask handles creating a usage cleanup task
// POST /api/v1/admin/usage/cleanup-tasks
func (h *UsageHandler) CreateCleanupTask(c *gin.Context) {
	if h.cleanupService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Usage cleanup service unavailable")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}

	var req CreateUsageCleanupTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	req.StartDate = strings.TrimSpace(req.StartDate)
	req.EndDate = strings.TrimSpace(req.EndDate)
	if req.StartDate == "" || req.EndDate == "" {
		response.BadRequest(c, "start_date and end_date are required")
		return
	}

	startTime, err := timezone.ParseInUserLocation("2006-01-02", req.StartDate, req.Timezone)
	if err != nil {
		response.BadRequest(c, "Invalid start_date format, use YYYY-MM-DD")
		return
	}
	endTime, err := timezone.ParseInUserLocation("2006-01-02", req.EndDate, req.Timezone)
	if err != nil {
		response.BadRequest(c, "Invalid end_date format, use YYYY-MM-DD")
		return
	}
	// Advance the calendar date in the requested location rather than adding a
	// fixed 24-hour duration.  A DST transition can make a local day 23 or 25
	// hours; using AddDate keeps the inclusive end-of-day boundary correct.
	endTime = endTime.AddDate(0, 0, 1).Add(-time.Nanosecond)

	if err := validateUsageCleanupRequestValues(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	recordType, err := service.ParseUsageCleanupRecordType(req.RecordType)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var requestType *int16
	stream := req.Stream
	if req.RequestType != nil {
		parsed, err := service.ParseUsageRequestType(*req.RequestType)
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		value := int16(parsed)
		requestType = &value
		stream = nil
	}

	// Convert the error-tab's phase/category/status filters into the compact
	// storage filters used by the cleanup repository.  Category filters are
	// intersected with an explicitly selected phase, matching the list API's
	// AND semantics.
	errorPhases, errorTypes, statusCodes, err := buildUsageCleanupErrorFilters(req.ErrorPhase, req.ErrorCategory, req.StatusCode, req.StatusCodes)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	filters := service.UsageCleanupFilters{
		StartTime:   startTime,
		EndTime:     endTime,
		RecordType:  recordType,
		APIKeyID:    req.APIKeyID,
		AccountID:   req.AccountID,
		GroupID:     req.GroupID,
		Model:       req.Model,
		RequestType: requestType,
		Stream:      stream,
		BillingType: req.BillingType,
		ErrorPhases: errorPhases,
		ErrorTypes:  errorTypes,
		StatusCodes: statusCodes,
	}

	var apiKeyID any
	if filters.APIKeyID != nil {
		apiKeyID = *filters.APIKeyID
	}
	var accountID any
	if filters.AccountID != nil {
		accountID = *filters.AccountID
	}
	var groupID any
	if filters.GroupID != nil {
		groupID = *filters.GroupID
	}
	var model any
	if filters.Model != nil {
		model = *filters.Model
	}
	var streamValue any
	if filters.Stream != nil {
		streamValue = *filters.Stream
	}
	var requestTypeName any
	if filters.RequestType != nil {
		requestTypeName = service.RequestTypeFromInt16(*filters.RequestType).String()
	}
	var billingType any
	if filters.BillingType != nil {
		billingType = *filters.BillingType
	}

	idempotencyPayload := struct {
		OperatorID int64                         `json:"operator_id"`
		Body       CreateUsageCleanupTaskRequest `json:"body"`
	}{
		OperatorID: subject.UserID,
		Body:       req,
	}
	executeAdminIdempotentJSON(c, "admin.usage.cleanup_tasks.create", idempotencyPayload, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 请求创建清理任务: operator=%d record_type=%s start=%s end=%s api_key_id=%v account_id=%v group_id=%v model=%v request_type=%v stream=%v billing_type=%v error_phase=%v error_category=%v status_codes=%v tz=%q",
			subject.UserID,
			filters.RecordType,
			filters.StartTime.Format(time.RFC3339),
			filters.EndTime.Format(time.RFC3339),
			apiKeyID,
			accountID,
			groupID,
			model,
			requestTypeName,
			streamValue,
			billingType,
			req.ErrorPhase,
			req.ErrorCategory,
			filters.StatusCodes,
			req.Timezone,
		)

		task, err := h.cleanupService.CreateTask(ctx, filters, subject.UserID)
		if err != nil {
			logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 创建清理任务失败: operator=%d err=%v", subject.UserID, err)
			return nil, err
		}
		logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 清理任务已创建: task=%d operator=%d status=%s", task.ID, subject.UserID, task.Status)
		return dto.UsageCleanupTaskFromService(task), nil
	})
}

// buildUsageCleanupErrorFilters converts the error tab's public filters to
// bounded repository predicates.  Category and phase use the same
// intersection semantics as the admin error list.
func buildUsageCleanupErrorFilters(phaseRaw, categoryRaw *string, statusCode *int, statusCodesRaw []int) ([]string, []string, []int, error) {
	var phases, errorTypes []string
	if categoryRaw != nil {
		category := strings.ToLower(strings.TrimSpace(*categoryRaw))
		if category != "" {
			known := map[string]bool{
				"auth": true, "rate_limit": true, "quota": true, "invalid_request": true,
				"service_unavailable": true, "upstream": true, "internal": true, "cyber": true,
			}
			if !known[category] {
				return nil, nil, nil, fmt.Errorf("invalid error_category %q", *categoryRaw)
			}
			phases, errorTypes = service.CategoryToFilter(category)
		}
	}
	if phaseRaw != nil {
		phase := strings.ToLower(strings.TrimSpace(*phaseRaw))
		if phase != "" {
			allowed := map[string]bool{
				"request": true, "auth": true, "account_auth": true, "routing": true,
				"upstream": true, "network": true, "internal": true,
			}
			if !allowed[phase] {
				return nil, nil, nil, fmt.Errorf("invalid error_phase %q", *phaseRaw)
			}
			if len(phases) == 0 {
				phases = []string{phase}
			} else {
				matched := false
				for _, candidate := range phases {
					if candidate == phase {
						matched = true
						break
					}
				}
				if matched {
					phases = []string{phase}
				} else {
					// A valid but disjoint combination is an empty result, not a
					// widened delete.  A sentinel keeps the SQL predicate bounded.
					phases = []string{"__no_matching_error_phase__"}
				}
			}
		}
	}

	statusCodes := append([]int(nil), statusCodesRaw...)
	if statusCode != nil {
		statusCodes = append(statusCodes, *statusCode)
	}
	if len(statusCodes) > 0 {
		if len(statusCodes) > 64 {
			return nil, nil, nil, fmt.Errorf("too many status codes; maximum is 64")
		}
		seen := make(map[int]struct{}, len(statusCodes))
		clean := make([]int, 0, len(statusCodes))
		for _, code := range statusCodes {
			if code < 0 || code > 999 {
				return nil, nil, nil, fmt.Errorf("invalid status code %d", code)
			}
			if _, exists := seen[code]; exists {
				continue
			}
			seen[code] = struct{}{}
			clean = append(clean, code)
		}
		statusCodes = clean
	}
	return phases, errorTypes, statusCodes, nil
}

func validateUsageCleanupRequestValues(req *CreateUsageCleanupTaskRequest) error {
	if req == nil {
		return fmt.Errorf("invalid cleanup request")
	}
	// Keep validation order stable so clients and tests receive deterministic
	// diagnostics when more than one identifier is malformed.
	for _, item := range []struct {
		name  string
		value *int64
	}{
		{name: "api_key_id", value: req.APIKeyID},
		{name: "account_id", value: req.AccountID},
		{name: "group_id", value: req.GroupID},
	} {
		if item.value != nil && *item.value <= 0 {
			return fmt.Errorf("%s must be positive", item.name)
		}
	}
	if req.BillingType != nil && *req.BillingType != service.BillingTypeBalance && *req.BillingType != service.BillingTypeSubscription {
		return fmt.Errorf("billing_type must be 0 or 1")
	}
	if len(req.StatusCodes) > 64 {
		return fmt.Errorf("too many status codes; maximum is 64")
	}
	return nil
}

// CancelCleanupTask handles canceling a usage cleanup task
// POST /api/v1/admin/usage/cleanup-tasks/:id/cancel
func (h *UsageHandler) CancelCleanupTask(c *gin.Context) {
	if h.cleanupService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Usage cleanup service unavailable")
		return
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "Unauthorized")
		return
	}
	idStr := strings.TrimSpace(c.Param("id"))
	taskID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || taskID <= 0 {
		response.BadRequest(c, "Invalid task id")
		return
	}
	logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 请求取消清理任务: task=%d operator=%d", taskID, subject.UserID)
	if err := h.cleanupService.CancelTask(c.Request.Context(), taskID, subject.UserID); err != nil {
		logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 取消清理任务失败: task=%d operator=%d err=%v", taskID, subject.UserID, err)
		response.ErrorFrom(c, err)
		return
	}
	logger.LegacyPrintf("handler.admin.usage", "[UsageCleanup] 清理任务已取消: task=%d operator=%d", taskID, subject.UserID)
	response.Success(c, gin.H{"id": taskID, "status": service.UsageCleanupStatusCanceled})
}
