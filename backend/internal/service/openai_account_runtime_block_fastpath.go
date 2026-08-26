package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	openAIAccountStateUpdateTimeout = 5 * time.Second
	// OpenAI OAuth 429s get one bounded same-account recovery attempt. When
	// that attempt is unavailable or exhausted, keep the credential out of the
	// scheduler for at least 30s so a burst is not amplified upstream.
	openAIOAuth429FallbackCooldown        = 30 * time.Second
	openAIOAuth429RetryWindow             = 10 * time.Second
	openAIOAuth429RetryDelay              = 500 * time.Millisecond
	openAIOAuth429RetryJitterRatio        = 0.2
	openAIStopSchedulingBridgeCooldown    = 2 * time.Minute
	openAIOAuth429StormWindow             = 10 * time.Second
	openAIOAuth429StormMaxAccountSwitches = 1
)

// openAIOAuth429RetryState is credential-local and shared by all requests so
// concurrent 429s cannot each obtain their own retry. retryNotBefore captures
// Retry-After (when present) or the jittered fallback delay.
type openAIOAuth429RetryState struct {
	startedAt      time.Time
	retryNotBefore time.Time
	deadline       time.Time
	retryGranted   bool
}

// OpenAIOAuth429FailoverState tracks the request-local follow-up budget after
// the first OAuth 429. Once that 429 occurs, exactly one different account may
// be attempted; any failure from that follow-up account ends failover even when
// a mixed pool selects an API-key credential or returns a non-429 status.
type OpenAIOAuth429FailoverState struct {
	openAIOAuth429FollowupPending bool
	grokOAuth429FollowupPending   bool
}

type openAIOAuth429Disposition uint8

const (
	openAIOAuth429Transient openAIOAuth429Disposition = iota
	openAIOAuth429Quota5h
	openAIOAuth429Quota7d
	openAIOAuth429QuotaReset
)

// classifyOpenAIOAuth429 区分账号配额耗尽信号与普通瞬时 429。明确窗口达到
// 100% 时以该窗口为准；没有 100% 标记但包含重置头时，沿用 v179 的兼容语义，
// 仍视为配额限流信号。
func classifyOpenAIOAuth429(headers http.Header, responseBody []byte) (openAIOAuth429Disposition, *time.Time) {
	if snapshot := ParseCodexRateLimitHeaders(headers); snapshot != nil {
		if normalized := snapshot.Normalize(); normalized != nil {
			if normalized.Used7dPercent != nil && *normalized.Used7dPercent >= 100 {
				if normalized.Reset7dSeconds != nil {
					now := time.Now()
					resetAt := now.Add(time.Duration(*normalized.Reset7dSeconds) * time.Second)
					return openAIOAuth429Quota7d, &resetAt
				}
				return openAIOAuth429Quota7d, nil
			}
			if normalized.Used5hPercent != nil && *normalized.Used5hPercent >= 100 {
				if normalized.Reset5hSeconds != nil {
					now := time.Now()
					resetAt := now.Add(time.Duration(*normalized.Reset5hSeconds) * time.Second)
					return openAIOAuth429Quota5h, &resetAt
				}
				return openAIOAuth429Quota5h, nil
			}
		}
	}
	if resetAt := calculateOpenAI429ResetTime(headers); resetAt != nil {
		return openAIOAuth429QuotaReset, resetAt
	}
	if resetUnix := parseOpenAIRateLimitResetTime(responseBody); resetUnix != nil {
		resetAt := time.Unix(*resetUnix, 0)
		return openAIOAuth429QuotaReset, &resetAt
	}
	return openAIOAuth429Transient, nil
}

func openAIAccountStateContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, openAIAccountStateUpdateTimeout)
}

func isOpenAIOAuthAccount(account *Account) bool {
	return account != nil && account.IsOpenAIOAuthLike()
}

func isGrokOAuthAccount(account *Account) bool {
	return account != nil && account.Platform == PlatformGrok && account.Type == AccountTypeOAuth
}

func isOpenAIAccount(account *Account) bool {
	return account != nil && (account.Platform == PlatformOpenAI || account.Platform == PlatformGrok)
}

// handleOpenAIAccountUpstreamError expects canonicalModel to be the model used
// for scheduling after applying account mapping exactly once.
func (s *OpenAIGatewayService) handleOpenAIAccountUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte, canonicalModel ...string) bool {
	if account != nil && account.Platform == PlatformGrok && isGrokContentPolicyRejection(statusCode, responseBody) {
		return false
	}
	// Any non-2xx upstream HTTP response means the model request was actually sent.
	if s != nil {
		scheduleOllamaCloudUsageActivity(s.deferredService, account)
	}
	// Capacity shedding describes this request, not account health. Keep the
	// account schedulable while the request-local retry budget handles recovery.
	if account != nil && account.Platform == PlatformOpenAI && isOpenAIRequestScopedCapacityShed("", responseBody) {
		return false
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	if account != nil && account.Platform == PlatformOpenAI && isOpenAIHTTPUpstreamAccessStateError(statusCode, "", responseBody) {
		message := "OpenAI upstream account or workspace is unavailable"
		if upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(responseBody)); upstreamMsg != "" {
			message = upstreamMsg
		}
		if s != nil && s.rateLimitService != nil {
			s.rateLimitService.handleAuthError(stateCtx, account, message)
		}
		if s != nil {
			s.BlockAccountScheduling(account, time.Time{}, "openai_access_state")
		}
		return true
	}

	if account != nil && account.Platform == PlatformOpenAI && isOpenAIContextWindowError("", responseBody) {
		return false
	}

	if isOpenAIImageRateLimitError(statusCode, responseBody) {
		if s != nil && s.rateLimitService != nil {
			_ = s.rateLimitService.HandleOpenAIImageRateLimit(stateCtx, account, statusCode, headers, responseBody)
		}
		return false
	}

	if s == nil || account == nil {
		return false
	}
	// Team 联动熔断必须先于 model-not-found 与账户级临时不可调度规则的早退。
	if s.rateLimitService != nil {
		s.rateLimitService.maybeHandleOpenAITeamLinkedError(stateCtx, account, statusCode, responseBody)
	}
	stateCtx = withTempUnschedulableModel(stateCtx, canonicalModel)
	if s.rateLimitService != nil && len(canonicalModel) > 0 && s.rateLimitService.HandleUpstreamModelNotFound(stateCtx, account, canonicalModel[0], statusCode, responseBody) {
		return true
	}
	// Isolate a custom temporary-unschedulable match to the known upstream
	// model before entering the generic account error path. This keeps the
	// account available to other models and avoids the account runtime blocker.
	if s.rateLimitService != nil && statusCode != http.StatusUnauthorized && len(canonicalModel) > 0 && strings.TrimSpace(canonicalModel[0]) != "" &&
		s.rateLimitService.HandleTempUnschedulable(stateCtx, account, statusCode, responseBody, canonicalModel[0]) {
		return true
	}
	if statusCode == http.StatusTooManyRequests {
		s.markOpenAIOAuth429RateLimited(stateCtx, account, headers, responseBody)
	}
	if s.rateLimitService == nil {
		return false
	}
	shouldDisable := s.rateLimitService.HandleUpstreamError(stateCtx, account, statusCode, headers, responseBody)
	modelTempMatched := statusCode != http.StatusUnauthorized && tempUnschedulableModel(stateCtx, nil) != "" &&
		len(matchTempUnschedulableRules(account, statusCode, responseBody)) > 0
	if shouldDisable && !modelTempMatched {
		s.BlockAccountScheduling(account, time.Time{}, "upstream_disable")
	}
	// Pool-mode retryable upstream errors are already bounded by the request-local
	// same-account retry budget. Recording the generic account+model transient
	// cooldown here would block the next approved retry before that budget is used.
	poolModeRetryable := account.IsPoolMode() && account.IsPoolModeRetryableStatus(statusCode)
	if !shouldDisable && account.Platform == PlatformOpenAI && account.Type == AccountTypeAPIKey &&
		shouldCooldownOpenAITransientUpstreamError(statusCode, responseBody) && !poolModeRetryable {
		model := ""
		if len(canonicalModel) > 0 {
			model = canonicalModel[0]
		}
		decision := s.recordOpenAIAccountModelTransientFailure(account, model, time.Now())
		if decision.FailureStreak > 0 {
			slog.Warn("openai_model_transient_state",
				"account_id", account.ID,
				"model", openAIAccountModelTransientModel(model),
				"failure_streak", decision.FailureStreak,
				"cooldown_ms", decision.Cooldown.Milliseconds(),
				"block_scope", "account_model",
			)
		}
	}
	return shouldDisable
}

// handleOpenAIAuxiliaryUpstreamError records failures from Codex control-plane
// surfaces that do not implement the Responses same-account retry contract
// (for example models manifest and Live call creation). A 429 must therefore
// persist its cooldown immediately instead of reserving a retry that this
// request path will never consume.
func (s *OpenAIGatewayService) handleOpenAIAuxiliaryUpstreamError(ctx context.Context, account *Account, statusCode int, headers http.Header, responseBody []byte) bool {
	if s == nil || account == nil {
		return false
	}
	if statusCode != http.StatusTooManyRequests {
		return s.handleOpenAIAccountUpstreamError(ctx, account, statusCode, headers, responseBody)
	}
	stateCtx, cancel := openAIAccountStateContext(ctx)
	defer cancel()
	if s.rateLimitService == nil {
		return false
	}
	return s.rateLimitService.HandleUpstreamError(stateCtx, account, statusCode, headers, responseBody)
}

func shouldCooldownOpenAITransientUpstreamError(statusCode int, responseBody []byte) bool {
	switch statusCode {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 520, 521, 522, 523, 524:
		return true
	case http.StatusBadRequest:
		return isOpenAITransientProcessingError(statusCode, "", responseBody)
	default:
		return false
	}
}

func (s *OpenAIGatewayService) markOpenAIOAuth429RateLimited(ctx context.Context, account *Account, headers http.Header, responseBody []byte) {
	if s == nil || !isOpenAIOAuthAccount(account) {
		return
	}
	// Spark 影子：不按 /responses 429 的 global x-codex-* 信号做内存运行时熔断(同 handle429,外审第8轮 P1)。
	// 同时避免把 spark 的 429 计入全局 429 storm 计数(recordOpenAIOAuth429),否则会误伤母账号 failover 决策。
	if account.IsShadow() {
		return
	}
	s.recordOpenAIOAuth429()
	disposition, resetAt := classifyOpenAIOAuth429(headers, responseBody)
	now := time.Now()
	cooldownUntil := s.openAIOAuth429CooldownUntil(ctx, account, headers, resetAt, now)
	if disposition != openAIOAuth429Transient {
		s.BlockAccountScheduling(account, cooldownUntil, "429")
		s.openaiOAuth429RetryStartedAt.Delete(account.ID)
		return
	}

	mu := s.openAIAccountRuntimeBlockLock(account.ID)
	mu.Lock()
	defer mu.Unlock()

	if value, ok := s.openaiOAuth429RetryStartedAt.Load(account.ID); ok {
		state, valid := normalizeOpenAIOAuth429RetryState(value, account.ID)
		// A retry already granted (or a retry budget that elapsed before it
		// could run) means this 429 exhausted recovery. Park the account now;
		// do not create a fresh window for every response.
		if !valid || state.retryGranted || !now.Before(state.deadline) {
			s.openaiOAuth429RetryStartedAt.Delete(account.ID)
			_, _ = s.blockAccountSchedulingLocked(account, cooldownUntil, "429_retry_exhausted")
			return
		}
		// Duplicate/concurrent observation before the single retry is granted.
		// Keep the original retry schedule; the grant is atomically consumed by
		// shouldRetryOpenAIOAuth429OnSameAccountWithResponse below.
		s.deferOpenAIOAuthAdmission(account.ID, state.retryNotBefore)
		return
	}

	state, retryFitsBudget := newOpenAIOAuth429RetryState(account.ID, headers, now)
	if retryFitsBudget {
		s.openaiOAuth429RetryStartedAt.Store(account.ID, state)
		s.deferOpenAIOAuthAdmission(account.ID, state.retryNotBefore)
		return
	}
	// Retry-After is authoritative. When it exceeds the in-request budget,
	// skip the retry and cool the credential until at least that instant.
	if state.retryNotBefore.After(cooldownUntil) {
		cooldownUntil = state.retryNotBefore
	}
	_, _ = s.blockAccountSchedulingLocked(account, cooldownUntil, "429_retry_after_budget")
}

func (s *OpenAIGatewayService) openAIOAuth429CooldownUntil(
	ctx context.Context,
	account *Account,
	headers http.Header,
	resetAt *time.Time,
	now time.Time,
) time.Time {
	cooldownUntil := now.Add(openAIOAuth429FallbackCooldown)
	if resetAt != nil && resetAt.After(cooldownUntil) {
		cooldownUntil = *resetAt
	}
	if retryAt := parseRetryAfterResetTime(headers, now); retryAt != nil && retryAt.After(cooldownUntil) {
		cooldownUntil = *retryAt
	}
	if s != nil && s.rateLimitService != nil {
		if cooldown, ok := s.rateLimitService.get429FallbackCooldown(ctx, account); ok && cooldown > 0 {
			configuredUntil := now.Add(cooldown)
			if configuredUntil.After(cooldownUntil) {
				cooldownUntil = configuredUntil
			}
		}
	}
	return cooldownUntil
}

func (s *OpenAIGatewayService) shouldRetryOpenAIOAuth429OnSameAccount(account *Account, statusCode int, shouldDisable bool) bool {
	return s.shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account, statusCode, shouldDisable, nil, nil)
}

func (s *OpenAIGatewayService) shouldRetryOpenAIOAuth429OnSameAccountWithResponse(account *Account, statusCode int, shouldDisable bool, headers http.Header, responseBody []byte) bool {
	if shouldDisable || statusCode != http.StatusTooManyRequests || !isOpenAIOAuthAccount(account) || account.IsShadow() {
		return false
	}
	disposition, _ := classifyOpenAIOAuth429(headers, responseBody)
	if disposition != openAIOAuth429Transient {
		return false
	}
	// markOpenAIOAuth429RateLimited parks the account once the window expires.
	// Do not accidentally create a fresh window after that transition.
	if s.isOpenAIAccountRuntimeBlocked(account) {
		return false
	}
	return s.openAIOAuth429RetryAvailable(account, headers, true, true)
}

// ShouldRetryOpenAIOAuth429 lets RateLimitService defer persistent account
// cooldown until the gateway's same-account retry window is exhausted.
func (s *OpenAIGatewayService) ShouldRetryOpenAIOAuth429(account *Account, headers http.Header, responseBody []byte) bool {
	if s == nil || !isOpenAIOAuthAccount(account) || account.IsShadow() || s.isOpenAIAccountRuntimeBlocked(account) {
		return false
	}
	disposition, _ := classifyOpenAIOAuth429(headers, responseBody)
	if disposition != openAIOAuth429Transient {
		return false
	}
	// RateLimitService only peeks here so it can defer persistence. The retry
	// grant is consumed later when the failover error is constructed. Requiring
	// existing state under the account lock also prevents a concurrent clear
	// from being replaced with an orphaned retry window.
	return s.openAIOAuth429RetryAvailable(account, headers, false, false)
}

func (s *OpenAIGatewayService) openAIOAuth429RetryAvailable(account *Account, headers http.Header, consume, allowCreate bool) bool {
	if s == nil || !isOpenAIOAuthAccount(account) || account.IsShadow() {
		return false
	}
	now := time.Now()
	mu := s.openAIAccountRuntimeBlockLock(account.ID)
	mu.Lock()
	defer mu.Unlock()

	value, ok := s.openaiOAuth429RetryStartedAt.Load(account.ID)
	var state openAIOAuth429RetryState
	if ok {
		state, ok = normalizeOpenAIOAuth429RetryState(value, account.ID)
	} else if allowCreate {
		state, ok = newOpenAIOAuth429RetryState(account.ID, headers, now)
	} else {
		return false
	}
	if !ok || state.retryGranted || !now.Before(state.deadline) || state.retryNotBefore.After(state.deadline) {
		return false
	}
	if consume {
		state.retryGranted = true
	}
	s.openaiOAuth429RetryStartedAt.Store(account.ID, state)
	return true
}

func (s *OpenAIGatewayService) openAIOAuth429RetryMetadata(account *Account) (time.Time, time.Duration) {
	if s == nil || !isOpenAIOAuthAccount(account) || account.IsShadow() {
		return time.Time{}, 0
	}
	value, ok := s.openaiOAuth429RetryStartedAt.Load(account.ID)
	if !ok {
		return time.Time{}, 0
	}
	state, ok := normalizeOpenAIOAuth429RetryState(value, account.ID)
	if !ok {
		return time.Time{}, 0
	}
	delay := time.Until(state.retryNotBefore)
	if delay < 0 {
		delay = 0
	}
	if remaining := time.Until(state.deadline); delay > remaining {
		delay = remaining
	}
	if delay < 0 {
		delay = 0
	}
	return state.deadline, delay
}

func newOpenAIOAuth429RetryState(accountID int64, headers http.Header, now time.Time) (openAIOAuth429RetryState, bool) {
	delay := jitterOpenAIOAuth429RetryDelay(accountID, now)
	if retryAt := parseRetryAfterResetTime(headers, now); retryAt != nil && retryAt.After(now.Add(delay)) {
		delay = retryAt.Sub(now)
	}
	state := openAIOAuth429RetryState{
		startedAt:      now,
		retryNotBefore: now.Add(delay),
		deadline:       now.Add(openAIOAuth429RetryWindow),
	}
	return state, !state.retryNotBefore.After(state.deadline)
}

func normalizeOpenAIOAuth429RetryState(value any, accountID int64) (openAIOAuth429RetryState, bool) {
	switch typed := value.(type) {
	case openAIOAuth429RetryState:
		return typed, !typed.startedAt.IsZero() && !typed.deadline.IsZero()
	case time.Time:
		// Backward-compatible with in-process state created before a rolling
		// deployment and with older tests that seed the historical time value.
		if typed.IsZero() {
			return openAIOAuth429RetryState{}, false
		}
		delay := jitterOpenAIOAuth429RetryDelay(accountID, typed)
		return openAIOAuth429RetryState{
			startedAt:      typed,
			retryNotBefore: typed.Add(delay),
			deadline:       typed.Add(openAIOAuth429RetryWindow),
		}, true
	default:
		return openAIOAuth429RetryState{}, false
	}
}

func jitterOpenAIOAuth429RetryDelay(accountID int64, startedAt time.Time) time.Duration {
	// A stable per-account/per-window xorshift avoids a synchronized retry herd
	// without introducing shared PRNG state on the hot path.
	x := uint64(accountID) ^ uint64(startedAt.UnixNano())
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	unit := float64(x%10_001) / 10_000
	factor := 1 - openAIOAuth429RetryJitterRatio + 2*openAIOAuth429RetryJitterRatio*unit
	return time.Duration(float64(openAIOAuth429RetryDelay) * factor)
}

func (s *OpenAIGatewayService) BlockAccountScheduling(account *Account, until time.Time, reason string) {
	if s == nil || !isOpenAIAccount(account) {
		return
	}
	mu := s.openAIAccountRuntimeBlockLock(account.ID)
	mu.Lock()
	defer mu.Unlock()
	_, _ = s.blockAccountSchedulingLocked(account, until, reason)
}

func (s *OpenAIGatewayService) openAIAccountRuntimeBlockLock(accountID int64) *sync.Mutex {
	actual, _ := s.openaiAccountRuntimeBlockLocks.LoadOrStore(accountID, &sync.Mutex{})
	mu, ok := actual.(*sync.Mutex)
	if !ok {
		mu = &sync.Mutex{}
		s.openaiAccountRuntimeBlockLocks.Store(accountID, mu)
	}
	return mu
}

func (s *OpenAIGatewayService) blockAccountSchedulingLocked(account *Account, until time.Time, _ string) (uint64, bool) {
	generation := s.openaiAccountRuntimeBlockSequence.Add(1)
	s.openaiAccountRuntimeBlockGeneration.Store(account.ID, generation)
	now := time.Now()
	blockUntil := until
	if blockUntil.IsZero() || !blockUntil.After(now) {
		blockUntil = now.Add(openAIStopSchedulingBridgeCooldown)
	}
	if account.IsOpenAIOAuthLike() {
		// Wake-time admission checks observe this deadline even when the
		// request reserved its start before a concurrent 429 was received.
		defer s.deferOpenAIOAuthAdmission(account.ID, blockUntil)
	}

	for {
		current, loaded := s.openaiAccountRuntimeBlockUntil.Load(account.ID)
		if !loaded {
			actual, stored := s.openaiAccountRuntimeBlockUntil.LoadOrStore(account.ID, blockUntil)
			if !stored {
				return generation, true
			}
			current = actual
		}

		currentUntil, ok := current.(time.Time)
		if !ok || currentUntil.IsZero() {
			if s.openaiAccountRuntimeBlockUntil.CompareAndSwap(account.ID, current, blockUntil) {
				return generation, true
			}
			continue
		}
		if !blockUntil.After(currentUntil) {
			return generation, false
		}
		if s.openaiAccountRuntimeBlockUntil.CompareAndSwap(account.ID, current, blockUntil) {
			return generation, true
		}
	}
}

func (s *OpenAIGatewayService) ClearAccountSchedulingBlock(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	mu := s.openAIAccountRuntimeBlockLock(accountID)
	mu.Lock()
	defer mu.Unlock()
	s.openaiAccountRuntimeBlockUntil.Delete(accountID)
	s.openaiOAuth429RetryStartedAt.Delete(accountID)
	s.openaiAccountRuntimeBlockGeneration.Store(accountID, s.openaiAccountRuntimeBlockSequence.Add(1))
	s.clearOpenAIOAuthAdmissionDefer(accountID)
}

func (s *OpenAIGatewayService) isOpenAIAccountRuntimeBlocked(account *Account) bool {
	if s == nil || !isOpenAIAccount(account) {
		return false
	}
	mu := s.openAIAccountRuntimeBlockLock(account.ID)
	mu.Lock()
	defer mu.Unlock()
	value, ok := s.openaiAccountRuntimeBlockUntil.Load(account.ID)
	if !ok {
		return false
	}
	cooldownUntil, ok := value.(time.Time)
	if !ok || cooldownUntil.IsZero() {
		s.openaiAccountRuntimeBlockUntil.Delete(account.ID)
		s.openaiAccountRuntimeBlockGeneration.Store(account.ID, s.openaiAccountRuntimeBlockSequence.Add(1))
		return false
	}
	if time.Now().Before(cooldownUntil) {
		return true
	}
	s.openaiAccountRuntimeBlockUntil.Delete(account.ID)
	s.openaiAccountRuntimeBlockGeneration.Store(account.ID, s.openaiAccountRuntimeBlockSequence.Add(1))
	return false
}

func (s *OpenAIGatewayService) getOpenAIAccountModelTransientState() *openAIAccountModelTransientState {
	if s == nil {
		return nil
	}
	s.openaiModelTransientOnce.Do(func() {
		if s.openaiModelTransient == nil {
			s.openaiModelTransient = newOpenAIAccountModelTransientState(openAIModelTransientDefaultMax)
		}
	})
	return s.openaiModelTransient
}

func canonicalOpenAIAccountSchedulingModel(account *Account, requestedModel string) string {
	model := strings.TrimSpace(requestedModel)
	if account == nil || model == "" {
		return model
	}
	if account.IsOpenAI() {
		return resolveOpenAIAccountUpstreamModelForRequest(account, model, false)
	}
	if mapped := strings.TrimSpace(account.GetMappedModel(model)); mapped != "" {
		return mapped
	}
	return model
}

func openAIAccountModelTransientModel(canonicalModel string) string {
	return normalizeOpenAIAccountModelTransientModel(canonicalModel)
}

func (s *OpenAIGatewayService) recordOpenAIAccountModelTransientFailure(account *Account, canonicalModel string, now time.Time) openAIAccountModelTransientDecision {
	if s == nil || account == nil {
		return openAIAccountModelTransientDecision{}
	}
	state := s.getOpenAIAccountModelTransientState()
	if state == nil {
		return openAIAccountModelTransientDecision{}
	}
	return state.recordFailure(account.ID, openAIAccountModelTransientModel(canonicalModel), now)
}

func (s *OpenAIGatewayService) clearOpenAIAccountModelTransientState(accountID int64, model string) {
	state := s.getOpenAIAccountModelTransientState()
	if state == nil {
		return
	}
	state.recordSuccess(accountID, model)
}

func (s *OpenAIGatewayService) isOpenAIAccountModelRuntimeBlocked(account *Account, requestedModel string) bool {
	if s == nil || account == nil {
		return false
	}
	state := s.getOpenAIAccountModelTransientState()
	if state == nil {
		return false
	}
	canonicalModel := canonicalOpenAIAccountSchedulingModel(account, requestedModel)
	return state.isBlocked(account.ID, openAIAccountModelTransientModel(canonicalModel), time.Now())
}

func (s *OpenAIGatewayService) isOpenAIAccountRequestRuntimeBlocked(account *Account, requestedModel string) bool {
	return s != nil && (s.isOpenAIAccountRuntimeBlocked(account) || s.isOpenAIAccountModelRuntimeBlocked(account, requestedModel))
}

func (s *OpenAIGatewayService) recordOpenAIOAuth429() {
	if s == nil {
		return
	}
	now := time.Now()
	windowStart := s.openaiOAuth429WindowStartUnixNano.Load()
	if windowStart == 0 || now.Sub(time.Unix(0, windowStart)) >= openAIOAuth429StormWindow {
		if s.openaiOAuth429WindowStartUnixNano.CompareAndSwap(windowStart, now.UnixNano()) {
			s.openaiOAuth429WindowCount.Store(1)
			return
		}
	}
	s.openaiOAuth429WindowCount.Add(1)
}

func (s *OpenAIGatewayService) ShouldStopOpenAIOAuth429Failover(account *Account, statusCode int, failedSwitches int, state *OpenAIOAuth429FailoverState) bool {
	if failedSwitches < openAIOAuth429StormMaxAccountSwitches {
		return false
	}
	if state != nil && state.openAIOAuth429FollowupPending && failedSwitches > openAIOAuth429StormMaxAccountSwitches {
		// The first OpenAI OAuth 429 already armed one alternate-account
		// attempt. Close the request after that alternate fails regardless of
		// its account type or status so a single request cannot walk the pool.
		return true
	}
	if state != nil && state.grokOAuth429FollowupPending && failedSwitches > openAIOAuth429StormMaxAccountSwitches {
		// The follow-up budget was armed by a Grok OAuth 429. Consume it on
		// any failing follow-up account, even if a mixed pool selected an API-key
		// account next.
		return true
	}
	if isGrokOAuthAccount(account) {
		if state == nil {
			// Preserve the old threshold for callers that have not adopted the
			// request-local state contract yet.
			return statusCode == http.StatusTooManyRequests && failedSwitches >= 2
		}
		if statusCode == http.StatusTooManyRequests {
			state.grokOAuth429FollowupPending = true
		}
		return false
	}
	if statusCode != http.StatusTooManyRequests || !isOpenAIOAuthAccount(account) {
		return false
	}
	if state != nil {
		state.openAIOAuth429FollowupPending = true
		return false
	}
	// Each OpenAI OAuth credential already consumed its one same-account retry
	// before reaching this point. Allow at most one alternate credential so a
	// provider-wide throttle cannot fan one client request across the whole
	// account pool.
	return failedSwitches > openAIOAuth429StormMaxAccountSwitches
}
