package admin

import (
	"context"
	"log/slog"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/handler/dto"
	"github.com/th3ee9ine/qqq2api/internal/pkg/openai"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
)

// OpenAIOAuthHandler handles OpenAI OAuth-related operations
type OpenAIOAuthHandler struct {
	openaiOAuthService *service.OpenAIOAuthService
	adminService       service.AdminService
	quotaService       openAIQuotaService
	sessionService     openAIAccountSessionService
	sessionCleanup     openAISessionCleanupRunner
	rateLimitService   openAIAccountStateRecoverer
}

type openAIQuotaService interface {
	QueryUsage(ctx context.Context, accountID int64) (*service.OpenAIQuotaUsage, error)
	CacheResetCreditsSnapshot(ctx context.Context, accountID int64, credits *service.OpenAIRateLimitResetCredits) error
	CachePostResetSnapshot(ctx context.Context, accountID int64, usage *service.OpenAIQuotaUsage) error
	ResetCredit(ctx context.Context, accountID int64) (*service.OpenAIQuotaResetResult, error)
}

type openAIAccountSessionService interface {
	ListSessions(ctx context.Context, accountID int64) (*service.OpenAIAccountSessionList, error)
	RevokeSession(ctx context.Context, accountID int64, sessionID string) error
	RevokeSessions(ctx context.Context, accountID int64, sessionIDs []string) (*service.OpenAIAccountSessionBatchRevokeResult, error)
}

// openAIAccountSessionTrustService is optional so older integrations and
// reduced test doubles that only support listing/revoking sessions remain
// source-compatible.  The production OpenAIQuotaService implements it.
type openAIAccountSessionTrustService interface {
	TrustSession(ctx context.Context, accountID int64, sessionID string) error
}

// openAIAccountCurrentSessionTrustService is an optional extension for
// implementations that can resolve and trust the current device without a
// session identifier.  The HTTP endpoint accepts an omitted session_id for
// those integrations while the UI sends the concrete current-session id.
type openAIAccountCurrentSessionTrustService interface {
	TrustCurrentSession(ctx context.Context, accountID int64) error
}

type openAIAccountStateRecoverer interface {
	RecoverAccountState(ctx context.Context, accountID int64, options service.AccountRecoveryOptions) (*service.SuccessfulTestRecoveryResult, error)
}

// OpenAISessionCleanupRunner is the narrow worker seam used by the admin
// handler.  Exporting the interface lets external integrations and tests wire
// a compatible runner without depending on an unexported parameter type.
type OpenAISessionCleanupRunner interface {
	RunAccount(ctx context.Context, accountID int64) error
}

type openAISessionCleanupRunner = OpenAISessionCleanupRunner

// openAISessionCleanupImmediateRunner is implemented by the production
// worker. It is optional so reduced deployments and existing test doubles that
// only implement the scheduled RunAccount method remain source-compatible.
type openAISessionCleanupImmediateRunner interface {
	RunAccountNow(ctx context.Context, accountID int64) error
}

// openAIQuotaResetPostProcessTimeout bounds the work performed AFTER the
// (non-refundable) reset credit has already been consumed upstream. The whole
// request must stay comfortably inside the panel HTTP client timeout, otherwise
// the browser aborts a mutation that already succeeded and the operator retries
// it — spending a second credit.
const openAIQuotaResetPostProcessTimeout = 8 * time.Second

type openAIQuotaResetResponse struct {
	service.OpenAIQuotaResetResult
	Quota                 *service.OpenAIQuotaUsage `json:"quota,omitempty"`
	Account               *dto.Account              `json:"account,omitempty"`
	CacheRefreshed        bool                      `json:"cache_refreshed"`
	AccountStateRecovered bool                      `json:"account_state_recovered"`
	WarningCode           string                    `json:"warning_code,omitempty"`
}

// openAIQuotaRefreshResponse is the reset-credit-persisting variant of the quota
// query. The usage payload is embedded so the shape stays identical to the plain
// query; cache_persisted reports whether the snapshot write succeeded, because a
// failed display-cache write must never discard a successful upstream read.
type openAIQuotaRefreshResponse struct {
	service.OpenAIQuotaUsage
	CachePersisted bool `json:"cache_persisted"`
}

// openAIQuotaResetPostProcessContext detaches the post-reset bookkeeping from the
// client connection. The credit is already spent at that point, so account-state
// recovery must complete even if the operator closes the tab (mirrors
// systemUpdateContext, added for the same reason in #4504).
func openAIQuotaResetPostProcessContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, openAIQuotaResetPostProcessTimeout)
}

func oauthPlatformFromPath(c *gin.Context) string {
	return service.PlatformOpenAI
}

// NewOpenAIOAuthHandler creates a new OpenAI OAuth handler
func NewOpenAIOAuthHandler(
	openaiOAuthService *service.OpenAIOAuthService,
	adminService service.AdminService,
	quotaService *service.OpenAIQuotaService,
	rateLimitService *service.RateLimitService,
	optionalSessionCleanup ...openAISessionCleanupRunner,
) *OpenAIOAuthHandler {
	h := &OpenAIOAuthHandler{
		openaiOAuthService: openaiOAuthService,
		adminService:       adminService,
	}
	// Assign through explicit nil checks: storing a nil *Service in an interface
	// field yields a non-nil interface, which would silently defeat the
	// `== nil` capability guards below and panic instead of returning 400.
	if quotaService != nil {
		h.quotaService = quotaService
		h.sessionService = quotaService
	}
	if rateLimitService != nil {
		h.rateLimitService = rateLimitService
	}
	// Keep the historical four-argument constructor source-compatible while
	// allowing integrations that wire the cleanup worker at construction time
	// to pass a fifth dependency.  The generated Wire graph still uses the
	// setter so older reduced deployments remain inert.
	if len(optionalSessionCleanup) > 0 {
		h.SetSessionCleanupService(optionalSessionCleanup[0])
	}
	return h
}

// SetSessionCleanupService injects the optional periodic non-current device
// session cleanup worker.  A setter keeps the historical constructor stable for
// lightweight handler tests and downstream integrations.
func (h *OpenAIOAuthHandler) SetSessionCleanupService(cleanup openAISessionCleanupRunner) {
	if h != nil {
		if isNilOpenAISessionCleanupRunner(cleanup) {
			cleanup = nil
		}
		h.sessionCleanup = cleanup
	}
}

func isNilOpenAISessionCleanupRunner(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

type openAISessionCleanupUpdateRequest struct {
	Enabled         *bool `json:"enabled"`
	IntervalMinutes *int  `json:"interval_minutes"`
}

type openAISessionCleanupResponse struct {
	Enabled         bool                               `json:"enabled"`
	IntervalMinutes int                                `json:"interval_minutes"`
	State           *service.OpenAISessionCleanupState `json:"state,omitempty"`
}

func parseOpenAISessionCleanupAccount(c *gin.Context, adminService service.AdminService) (*service.Account, int64, bool) {
	if isNilOpenAISessionCleanupDependency(adminService) {
		response.Error(c, http.StatusInternalServerError, "admin account service is not configured")
		return nil, 0, false
	}
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return nil, 0, false
	}
	account, err := adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil, 0, false
	}
	if account == nil {
		// A few lightweight AdminService implementations use the conventional
		// (nil, nil) not-found result. Normalize it to the same 404 envelope as
		// the production service instead of reporting a misleading platform
		// mismatch.
		response.ErrorFrom(c, service.ErrAccountNotFound)
		return nil, 0, false
	}
	if account.Platform != service.PlatformOpenAI || account.Type != service.AccountTypeOAuth || account.IsShadow() {
		response.BadRequest(c, "session cleanup requires an OpenAI OAuth parent account")
		return nil, 0, false
	}
	return account, accountID, true
}

// AdminService is an interface in the handler constructor. Treat an interface
// containing a typed-nil implementation the same as a nil interface so a
// partially wired test/deployment returns a stable 500 instead of panicking.
func isNilOpenAISessionCleanupDependency(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func openAISessionCleanupStateFromAccount(account *service.Account) *service.OpenAISessionCleanupState {
	if account == nil || account.Extra == nil {
		return nil
	}
	raw := account.Extra[service.OpenAISessionCleanupStateExtraKey]
	if raw == nil {
		raw = account.Extra[service.OpenAIAutoRevokeNonCurrentSessionsLegacyStateExtraKey]
	}
	return service.SanitizeOpenAISessionCleanupState(raw)
}

// GetSessionCleanup returns the account-level periodic session cleanup policy
// and its redacted runtime state.
// GET /api/v1/admin/openai/accounts/:id/sessions/cleanup
func (h *OpenAIOAuthHandler) GetSessionCleanup(c *gin.Context) {
	if h == nil {
		response.Error(c, http.StatusInternalServerError, "OpenAI OAuth handler is not configured")
		return
	}
	account, _, ok := parseOpenAISessionCleanupAccount(c, h.adminService)
	if !ok {
		return
	}
	config := service.ResolveOpenAINonCurrentSessionRevokeConfig(account)
	state := openAISessionCleanupStateFromAccount(account)
	response.Success(c, openAISessionCleanupResponse{Enabled: config.Enabled, IntervalMinutes: config.IntervalMinutes, State: state})
}

// UpdateSessionCleanup updates only the cleanup settings while preserving all
// unrelated account.extra keys.
// PUT /api/v1/admin/openai/accounts/:id/sessions/cleanup
func (h *OpenAIOAuthHandler) UpdateSessionCleanup(c *gin.Context) {
	if h == nil {
		response.Error(c, http.StatusInternalServerError, "OpenAI OAuth handler is not configured")
		return
	}
	_, accountID, ok := parseOpenAISessionCleanupAccount(c, h.adminService)
	if !ok {
		return
	}
	var req openAISessionCleanupUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	updates := make(map[string]any, 2)
	if req.Enabled != nil {
		updates[service.OpenAISessionCleanupEnabledExtraKey] = *req.Enabled
	}
	if req.IntervalMinutes != nil {
		updates[service.OpenAISessionCleanupIntervalMinutesExtraKey] = *req.IntervalMinutes
	}
	if err := h.adminService.UpdateAccountExtra(c.Request.Context(), accountID, updates); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	updated, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if updated == nil {
		response.Error(c, http.StatusInternalServerError, "updated account was not found")
		return
	}
	config := service.ResolveOpenAINonCurrentSessionRevokeConfig(updated)
	state := openAISessionCleanupStateFromAccount(updated)
	response.Success(c, openAISessionCleanupResponse{Enabled: config.Enabled, IntervalMinutes: config.IntervalMinutes, State: state})
}

// RunSessionCleanup triggers one immediate cleanup for an account.  It bypasses
// the periodic due timestamp but still enforces the current-device safety gate.
// POST /api/v1/admin/openai/accounts/:id/sessions/cleanup/run
func (h *OpenAIOAuthHandler) RunSessionCleanup(c *gin.Context) {
	if h == nil {
		response.Error(c, http.StatusInternalServerError, "OpenAI OAuth handler is not configured")
		return
	}
	_, accountID, ok := parseOpenAISessionCleanupAccount(c, h.adminService)
	if !ok {
		return
	}
	if isNilOpenAISessionCleanupRunner(h.sessionCleanup) {
		response.BadRequest(c, "openai session cleanup service is not enabled")
		return
	}
	if err := h.sessionCleanup.RunAccount(c.Request.Context(), accountID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "OpenAI session cleanup completed"})
}

// Explicitly prefixed aliases make the endpoint methods discoverable to
// integrations that group OpenAI controls by operation name.
func (h *OpenAIOAuthHandler) GetOpenAISessionCleanup(c *gin.Context) {
	h.GetSessionCleanup(c)
}

func (h *OpenAIOAuthHandler) UpdateOpenAISessionCleanup(c *gin.Context) {
	h.UpdateSessionCleanup(c)
}

func (h *OpenAIOAuthHandler) RunOpenAISessionCleanup(c *gin.Context) {
	h.RunSessionCleanup(c)
}

// Legacy auto-revoke spellings retained for integrations that adopted the
// original policy name before the UI-facing session-cleanup name shipped.
func (h *OpenAIOAuthHandler) GetOpenAIAutoRevokeNonCurrentSessions(c *gin.Context) {
	h.GetSessionCleanup(c)
}

func (h *OpenAIOAuthHandler) UpdateOpenAIAutoRevokeNonCurrentSessions(c *gin.Context) {
	h.UpdateSessionCleanup(c)
}

func (h *OpenAIOAuthHandler) RunOpenAIAutoRevokeNonCurrentSessions(c *gin.Context) {
	h.RunSessionCleanup(c)
}

// ListSessions returns the active ChatGPT sessions associated with an OpenAI
// OAuth account.
// GET /api/v1/admin/openai/accounts/:id/sessions
func (h *OpenAIOAuthHandler) ListSessions(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.sessionService == nil {
		response.BadRequest(c, "openai account session service is not enabled")
		return
	}

	sessions, err := h.sessionService.ListSessions(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if sessions == nil {
		response.Error(c, http.StatusInternalServerError, "openai session query returned an empty result")
		return
	}
	response.Success(c, sessions)
}

// RevokeSession logs a single device/browser session out of ChatGPT.
// DELETE /api/v1/admin/openai/accounts/:id/sessions/:session_id
func (h *OpenAIOAuthHandler) RevokeSession(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	sessionID := strings.TrimSpace(c.Param("session_id"))
	if sessionID == "" || len(sessionID) > 512 {
		response.BadRequest(c, "Invalid session ID")
		return
	}
	if h.sessionService == nil {
		response.BadRequest(c, "openai account session service is not enabled")
		return
	}

	if err := h.sessionService.RevokeSession(c.Request.Context(), accountID, sessionID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Session logged out successfully"})
}

type openAIRevokeSessionsRequest struct {
	SessionIDs []string `json:"session_ids"`
}

type openAISessionCleanupBatchRequest struct {
	AccountIDs []int64 `json:"account_ids"`
}

const openAISessionCleanupMaxAccountBatchSize = 100

type openAISessionCleanupBatchResult struct {
	RequestedCount int `json:"requested_count"`
	SuccessCount   int `json:"success_count"`
	FailedCount    int `json:"failed_count"`
	// Failures contains account IDs only. Upstream errors can include URLs,
	// session IDs, or credential material and must not be returned verbatim.
	Failures []int64 `json:"failures,omitempty"`
}

// RevokeSessions logs multiple device/browser sessions out of ChatGPT.
// POST /api/v1/admin/openai/accounts/:id/sessions/revoke
func (h *OpenAIOAuthHandler) RevokeSessions(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.sessionService == nil {
		response.BadRequest(c, "openai account session service is not enabled")
		return
	}

	var req openAIRevokeSessionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.sessionService.RevokeSessions(c.Request.Context(), accountID, req.SessionIDs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

type openAITrustSessionRequest struct {
	SessionID string `json:"session_id"`
}

// TrustSession marks the local/current ChatGPT device session as trusted.
// The panel only renders this action for rows identified as the current
// session.  A concrete session_id is accepted to support accounts whose
// current device exposes multiple first-party app sessions; implementations
// that resolve the current session themselves may omit it.
// POST /api/v1/admin/openai/accounts/:id/sessions/trust
func (h *OpenAIOAuthHandler) TrustSession(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h == nil || h.sessionService == nil {
		response.BadRequest(c, "openai account session service is not enabled")
		return
	}

	var req openAITrustSessionRequest
	// Keep an empty body valid for current-session-aware integrations.  A
	// malformed non-empty body is still rejected rather than silently trusting a
	// different session.
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.BadRequest(c, "Invalid request: "+err.Error())
			return
		}
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.SessionID == "" {
		// Accept the resource-style spelling as well as the body-based action
		// route. This keeps integrations that mirror the single-session revoke
		// endpoint from having to special-case trust operations.
		req.SessionID = strings.TrimSpace(c.Param("session_id"))
	}
	if req.SessionID != "" && len(req.SessionID) > 512 {
		response.BadRequest(c, "Invalid session ID")
		return
	}

	if req.SessionID == "" {
		if currentTrust, ok := h.sessionService.(openAIAccountCurrentSessionTrustService); ok && !isNilOpenAISessionTrustService(currentTrust) {
			if err := currentTrust.TrustCurrentSession(c.Request.Context(), accountID); err != nil {
				response.ErrorFrom(c, err)
				return
			}
			response.Success(c, gin.H{"message": "Current device session marked as trusted"})
			return
		}
		response.BadRequest(c, "session_id is required")
		return
	}

	trustService, ok := h.sessionService.(openAIAccountSessionTrustService)
	if !ok || isNilOpenAISessionTrustService(trustService) {
		response.BadRequest(c, "openai account session trust service is not enabled")
		return
	}
	if err := trustService.TrustSession(c.Request.Context(), accountID, req.SessionID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Device session marked as trusted"})
}

func isNilOpenAISessionTrustService(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// RunSessionsCleanup executes the global non-current-session cleanup for a
// selected set of accounts. The global policy controls the scheduler, while
// this endpoint is the explicit batch action used by the Account Device
// Sessions menu.
// POST /api/v1/admin/openai/sessions/cleanup/run
func (h *OpenAIOAuthHandler) RunSessionsCleanup(c *gin.Context) {
	if h == nil || isNilOpenAISessionCleanupRunner(h.sessionCleanup) {
		response.BadRequest(c, "openai session cleanup service is not enabled")
		return
	}
	var req openAISessionCleanupBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if len(req.AccountIDs) == 0 || len(req.AccountIDs) > openAISessionCleanupMaxAccountBatchSize {
		response.BadRequest(c, "account_ids must contain between 1 and 100 accounts")
		return
	}
	accountIDs := make([]int64, 0, len(req.AccountIDs))
	seen := make(map[int64]struct{}, len(req.AccountIDs))
	for _, accountID := range req.AccountIDs {
		if accountID <= 0 {
			response.BadRequest(c, "account_ids must contain positive account IDs")
			return
		}
		if _, exists := seen[accountID]; exists {
			continue
		}
		seen[accountID] = struct{}{}
		accountIDs = append(accountIDs, accountID)
	}
	if len(accountIDs) == 0 {
		response.BadRequest(c, "account_ids must contain between 1 and 100 accounts")
		return
	}

	// A cleanup call is bounded upstream, but a large selection should still be
	// processed in parallel so one slow account does not make the panel request
	// time out. Four workers keeps refresh/revoke traffic below the normal panel
	// concurrency used by the account list.
	type outcome struct {
		accountID int64
		err       error
	}
	jobs := make(chan int64)
	outcomes := make(chan outcome, len(accountIDs))
	workerCount := len(accountIDs)
	if workerCount > 4 {
		workerCount = 4
	}
	for i := 0; i < workerCount; i++ {
		go func() {
			for accountID := range jobs {
				run := h.sessionCleanup.RunAccount
				if immediate, ok := h.sessionCleanup.(openAISessionCleanupImmediateRunner); ok && !isNilOpenAISessionCleanupRunner(immediate) {
					run = immediate.RunAccountNow
				}
				outcomes <- outcome{accountID: accountID, err: run(c.Request.Context(), accountID)}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, accountID := range accountIDs {
			jobs <- accountID
		}
	}()
	batchResult := openAISessionCleanupBatchResult{RequestedCount: len(accountIDs), Failures: make([]int64, 0)}
	for range accountIDs {
		outcome := <-outcomes
		if outcome.err != nil {
			batchResult.FailedCount++
			batchResult.Failures = append(batchResult.Failures, outcome.accountID)
		} else {
			batchResult.SuccessCount++
		}
	}
	sort.Slice(batchResult.Failures, func(i, j int) bool { return batchResult.Failures[i] < batchResult.Failures[j] })
	response.Success(c, batchResult)
}

// OpenAIGenerateAuthURLRequest represents the request for generating OpenAI auth URL
type OpenAIGenerateAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

// GenerateAuthURL generates OpenAI OAuth authorization URL
// POST /api/v1/admin/openai/generate-auth-url
func (h *OpenAIOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req OpenAIGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Allow empty body
		req = OpenAIGenerateAuthURLRequest{}
	}

	result, err := h.openaiOAuthService.GenerateAuthURL(
		c.Request.Context(),
		req.ProxyID,
		req.RedirectURI,
		oauthPlatformFromPath(c),
	)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, result)
}

// OpenAIExchangeCodeRequest represents the request for exchanging OpenAI auth code
type OpenAIExchangeCodeRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	Code        string `json:"code" binding:"required"`
	State       string `json:"state" binding:"required"`
	RedirectURI string `json:"redirect_uri"`
	ProxyID     *int64 `json:"proxy_id"`
}

// ExchangeCode exchanges OpenAI authorization code for tokens
// POST /api/v1/admin/openai/exchange-code
func (h *OpenAIOAuthHandler) ExchangeCode(c *gin.Context) {
	var req OpenAIExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	tokenInfo, err := h.openaiOAuthService.ExchangeCode(c.Request.Context(), &service.OpenAIExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, tokenInfo)
}

// OpenAIRefreshTokenRequest represents the request for refreshing OpenAI token
type OpenAIRefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
	RT           string `json:"rt"`
	ClientID     string `json:"client_id"`
	ProxyID      *int64 `json:"proxy_id"`
}

type OpenAICodexPATCreateRequest struct {
	AccessToken             string         `json:"access_token" binding:"required"`
	Name                    string         `json:"name"`
	Notes                   *string        `json:"notes"`
	GroupIDs                []int64        `json:"group_ids"`
	ProxyID                 *int64         `json:"proxy_id"`
	AutoAssignProxy         bool           `json:"auto_assign_proxy"`
	Concurrency             *int           `json:"concurrency"`
	Priority                *int           `json:"priority"`
	RateMultiplier          *float64       `json:"rate_multiplier"`
	LoadFactor              *int           `json:"load_factor"`
	ExpiresAt               *int64         `json:"expires_at"`
	AutoPauseOnExpired      *bool          `json:"auto_pause_on_expired"`
	CredentialExtras        map[string]any `json:"credential_extras"`
	Extra                   map[string]any `json:"extra"`
	SkipDefaultGroupBind    *bool          `json:"skip_default_group_bind"`
	ConfirmMixedChannelRisk *bool          `json:"confirm_mixed_channel_risk"`
}

// RefreshToken refreshes an OpenAI OAuth token
// POST /api/v1/admin/openai/refresh-token
func (h *OpenAIOAuthHandler) RefreshToken(c *gin.Context) {
	var req OpenAIRefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(req.RT)
	}
	if refreshToken == "" {
		response.BadRequest(c, "refresh_token is required")
		return
	}

	var proxyURL string
	if req.ProxyID != nil {
		proxy, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err == nil && proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	// 未指定 client_id 时，根据请求路径平台自动设置默认值，避免 repository 层盲猜
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		platform := oauthPlatformFromPath(c)
		clientID, _ = openai.OAuthClientConfigByPlatform(platform)
	}

	tokenInfo, err := h.openaiOAuthService.RefreshTokenWithClientID(c.Request.Context(), refreshToken, proxyURL, clientID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, tokenInfo)
}

// RefreshAccountToken refreshes token for a specific OpenAI account
// POST /api/v1/admin/openai/accounts/:id/refresh
func (h *OpenAIOAuthHandler) RefreshAccountToken(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	// Get account
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	platform := oauthPlatformFromPath(c)
	if account.Platform != platform {
		response.BadRequest(c, "Account platform does not match OAuth endpoint")
		return
	}

	// Only refresh OAuth-based accounts
	if !account.IsOAuth() {
		response.BadRequest(c, "Cannot refresh non-OAuth account credentials")
		return
	}

	// spark 影子账号凭据透传母账号、自身恒空,刷新无意义;在调用上游前早拒,避免先打上游
	// 再被凭据写守卫拦下的无谓副作用(外审第6轮)。
	if account.IsCredentialShadow() {
		response.BadRequest(c, "Cannot refresh spark shadow account; its credentials are managed by the parent account")
		return
	}

	// Use OpenAI OAuth service to refresh token
	tokenInfo, err := h.openaiOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Build new credentials from token info
	newCredentials := h.openaiOAuthService.BuildAccountCredentials(tokenInfo)

	// Preserve non-token settings from existing credentials
	for k, v := range account.Credentials {
		if _, exists := newCredentials[k]; !exists {
			newCredentials[k] = v
		}
	}
	newCredentials = service.NormalizeOpenAIPersonalAccessTokenCredentials(account, tokenInfo, newCredentials)

	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(updatedAccount))
}

// RefreshAccountSubscription fetches the latest ChatGPT subscription expiry
// (active_until) and persists it in the account credentials.  The regular token
// refresh intentionally reuses cached subscription data when it is still
// present; this explicit account-management action always performs a fresh
// subscription lookup so stale dates can be corrected.
// POST /api/v1/admin/openai/accounts/:id/subscription/refresh
func (h *OpenAIOAuthHandler) RefreshAccountSubscription(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != oauthPlatformFromPath(c) {
		response.BadRequest(c, "Account platform does not match OAuth endpoint")
		return
	}
	if !account.IsOAuth() {
		response.BadRequest(c, "Cannot refresh subscription for non-OAuth account")
		return
	}
	if account.IsOpenAIPersonalAccessToken() {
		response.BadRequest(c, "ChatGPT subscription expiry is not available for personal access token accounts")
		return
	}
	if account.IsCredentialShadow() {
		response.BadRequest(c, "Cannot refresh subscription for a spark shadow account")
		return
	}
	if h.openaiOAuthService == nil {
		response.Error(c, http.StatusInternalServerError, "OpenAI OAuth service is not configured")
		return
	}

	tokenInfo, err := h.openaiOAuthService.RefreshAccountSubscription(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	newCredentials := h.openaiOAuthService.BuildAccountCredentials(tokenInfo)
	// Preserve provider credentials and account-level settings that are not part
	// of the token response (for example model_mapping and custom headers).
	for k, v := range account.Credentials {
		if _, exists := newCredentials[k]; !exists {
			newCredentials[k] = v
		}
	}
	if tokenInfo.SubscriptionFetched && strings.TrimSpace(tokenInfo.SubscriptionExpiresAt) == "" {
		delete(newCredentials, "subscription_expires_at")
	}
	newCredentials = service.NormalizeOpenAIPersonalAccessTokenCredentials(account, tokenInfo, newCredentials)

	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(updatedAccount))
}

// CreateAccountFromOAuth creates a new OpenAI OAuth account from token info
// POST /api/v1/admin/openai/create-from-oauth
func (h *OpenAIOAuthHandler) CreateAccountFromOAuth(c *gin.Context) {
	var req struct {
		SessionID   string  `json:"session_id" binding:"required"`
		Code        string  `json:"code" binding:"required"`
		State       string  `json:"state" binding:"required"`
		RedirectURI string  `json:"redirect_uri"`
		ProxyID     *int64  `json:"proxy_id"`
		Name        string  `json:"name"`
		Concurrency int     `json:"concurrency"`
		Priority    int     `json:"priority"`
		GroupIDs    []int64 `json:"group_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	// Exchange code for tokens
	tokenInfo, err := h.openaiOAuthService.ExchangeCode(c.Request.Context(), &service.OpenAIExchangeCodeInput{
		SessionID:   req.SessionID,
		Code:        req.Code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
		ProxyID:     req.ProxyID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Build credentials from token info
	credentials := h.openaiOAuthService.BuildAccountCredentials(tokenInfo)

	platform := oauthPlatformFromPath(c)

	// Use email as default name if not provided
	name := req.Name
	if name == "" && tokenInfo.Email != "" {
		name = tokenInfo.Email
	}
	if name == "" {
		name = "OpenAI OAuth Account"
	}

	// Create account
	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:        name,
		Platform:    platform,
		Type:        "oauth",
		Credentials: credentials,
		Extra:       nil,
		ProxyID:     req.ProxyID,
		Concurrency: req.Concurrency,
		Priority:    req.Priority,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(account))
}

// CreateAccountFromCodexPAT creates an OpenAI OAuth account from a Codex at-* personal access token.
// POST /api/v1/admin/openai/create-from-codex-pat
func (h *OpenAIOAuthHandler) CreateAccountFromCodexPAT(c *gin.Context) {
	var req OpenAICodexPATCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := service.ValidateOpenAILongContextBillingExtra(service.PlatformOpenAI, req.Extra); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if req.Concurrency != nil && *req.Concurrency < 0 {
		response.BadRequest(c, "concurrency must be >= 0")
		return
	}
	if req.Priority != nil && *req.Priority < 0 {
		response.BadRequest(c, "priority must be >= 0")
		return
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		response.BadRequest(c, "rate_multiplier must be >= 0")
		return
	}
	if req.LoadFactor != nil && *req.LoadFactor > 10000 {
		response.BadRequest(c, "load_factor must be <= 10000")
		return
	}
	if req.AutoAssignProxy && req.ProxyID != nil {
		response.ErrorFrom(c, service.ErrProxyAssignmentModeConflict)
		return
	}

	var proxyURL string
	if req.ProxyID != nil {
		proxy, err := h.adminService.GetProxy(c.Request.Context(), *req.ProxyID)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
		if proxy != nil {
			proxyURL = proxy.URL()
		}
	}

	tokenInfo, err := h.openaiOAuthService.ValidateCodexPersonalAccessToken(c.Request.Context(), req.AccessToken, proxyURL)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	credentials := mergeCodexImportMap(
		h.openaiOAuthService.BuildAccountCredentials(tokenInfo),
		sanitizeCodexImportCredentialExtras(req.CredentialExtras),
	)
	extra := mergeCodexImportMap(req.Extra, map[string]any{
		"import_source":       "codex_personal_access_token",
		"auth_provider":       "codex_personal_access_token",
		"imported_at":         time.Now().UTC().Format(time.RFC3339),
		"access_token_sha256": codexTokenFingerprint(req.AccessToken),
	})

	concurrency := 3
	if req.Concurrency != nil {
		concurrency = *req.Concurrency
	}
	priority := 50
	if req.Priority != nil {
		priority = *req.Priority
	}
	skipDefaultGroupBind := false
	if req.SkipDefaultGroupBind != nil {
		skipDefaultGroupBind = *req.SkipDefaultGroupBind
	}

	account, err := h.adminService.CreateAccount(c.Request.Context(), &service.CreateAccountInput{
		Name:                  buildOpenAICodexPATAccountName(req.Name, tokenInfo),
		Notes:                 req.Notes,
		Platform:              service.PlatformOpenAI,
		Type:                  service.AccountTypeOAuth,
		Credentials:           credentials,
		Extra:                 extra,
		ProxyID:               req.ProxyID,
		AutoAssignProxy:       req.AutoAssignProxy,
		Concurrency:           concurrency,
		Priority:              priority,
		RateMultiplier:        req.RateMultiplier,
		LoadFactor:            req.LoadFactor,
		GroupIDs:              req.GroupIDs,
		ExpiresAt:             req.ExpiresAt,
		AutoPauseOnExpired:    req.AutoPauseOnExpired,
		SkipDefaultGroupBind:  skipDefaultGroupBind,
		SkipMixedChannelCheck: req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromService(account))
}

func buildOpenAICodexPATAccountName(name string, tokenInfo *service.OpenAITokenInfo) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	if tokenInfo != nil {
		for _, candidate := range []string{tokenInfo.Email, tokenInfo.ChatGPTAccountID, tokenInfo.ChatGPTUserID} {
			if candidate = strings.TrimSpace(candidate); candidate != "" {
				return candidate
			}
		}
	}
	return "Codex PAT Account"
}

// QueryQuota queries the rate-limit / quota usage for an OpenAI account.
// GET /api/v1/admin/openai/accounts/:id/quota
func (h *OpenAIOAuthHandler) QueryQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}

	usage, err := h.quotaService.QueryUsage(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	service.NotifyOpenAIAutoResetCredit(accountID)
	response.Success(c, usage)
}

// RefreshQuota queries the rate-limit / quota usage AND persists the reset-credit
// snapshot so the card can be rehydrated without an upstream round-trip.
// POST /api/v1/admin/openai/accounts/:id/quota/refresh
//
// It is a POST (not a GET with a side-effect flag) because it writes account
// state: the audit middleware only records mutating verbs, so a persisting GET
// would mutate the database without an audit trail.
func (h *OpenAIOAuthHandler) RefreshQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}

	usage, err := h.quotaService.QueryUsage(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if usage == nil {
		response.Error(c, http.StatusInternalServerError, "openai quota query returned an empty result")
		return
	}
	service.NotifyOpenAIAutoResetCredit(accountID)

	refreshResponse := openAIQuotaRefreshResponse{OpenAIQuotaUsage: *usage}
	// A failed snapshot write leaves the previous cache intact — report it as a
	// partial success instead of discarding the usage payload we just fetched,
	// which would leave the card without a credit count at all.
	if err := h.quotaService.CacheResetCreditsSnapshot(c.Request.Context(), accountID, usage.RateLimitResetCredits); err != nil {
		slog.Warn("openai_quota_reset_credit_cache_persist_failed", "account_id", accountID, "error", err)
		response.Success(c, refreshResponse)
		return
	}
	refreshResponse.CachePersisted = true
	response.Success(c, refreshResponse)
}

// CreateShadowRequest is the request body for CreateShadow.
type CreateShadowRequest struct {
	Name        string  `json:"name"`
	Priority    int     `json:"priority"`
	Concurrency int     `json:"concurrency"`
	GroupIDs    []int64 `json:"group_ids"`
}

// CreateShadow creates a spark-dimension shadow account for a parent OpenAI OAuth account.
// POST /api/v1/admin/accounts/:id/shadow
func (h *OpenAIOAuthHandler) CreateShadow(c *gin.Context) {
	parentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req CreateShadowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	shadow, err := h.adminService.CreateShadow(c.Request.Context(), parentID, service.ShadowOptions{
		Name:        req.Name,
		Priority:    req.Priority,
		Concurrency: req.Concurrency,
		GroupIDs:    req.GroupIDs,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	response.Success(c, dto.AccountFromServiceShallow(shadow))
}

// ResetQuota consumes one rate-limit reset credit for an OpenAI account.
// POST /api/v1/admin/openai/accounts/:id/reset-quota
func (h *OpenAIOAuthHandler) ResetQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.quotaService == nil {
		response.BadRequest(c, "openai quota service is not enabled")
		return
	}
	result, err := h.quotaService.ResetCredit(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if result == nil {
		response.Error(c, http.StatusInternalServerError, "openai quota reset returned an empty result")
		return
	}

	resetResponse := openAIQuotaResetResponse{OpenAIQuotaResetResult: *result}
	postCtx, cancelPost := openAIQuotaResetPostProcessContext(c.Request.Context())
	defer cancelPost()

	postResult := service.RunOpenAIQuotaResetPostProcess(
		postCtx,
		accountID,
		h.quotaService,
		h.rateLimitService,
		h.adminService.GetAccount,
	)
	resetResponse.Quota = postResult.Quota
	resetResponse.CacheRefreshed = postResult.CacheRefreshed
	resetResponse.AccountStateRecovered = postResult.AccountStateRecovered
	resetResponse.WarningCode = postResult.WarningCode
	if postResult.Account != nil {
		resetResponse.Account = dto.AccountFromService(postResult.Account)
	}
	response.Success(c, resetResponse)
}
