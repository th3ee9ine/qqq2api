package service

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	// The default is intentionally conservative: account concurrency still
	// controls in-flight work, while this guard limits how quickly new work may
	// start on one OAuth credential.
	openAIOAuthAdmissionBaseInterval          = 250 * time.Millisecond
	openAIOAuthAdmissionHighQuotaInterval     = 500 * time.Millisecond
	openAIOAuthAdmissionCriticalQuotaInterval = time.Second
	openAIOAuthAdmissionSevereQuotaInterval   = 2 * time.Second
	openAIOAuthAdmissionMaxQueueWait          = 2 * time.Second
	openAIOAuthAdmissionUnknownResetCooldown  = 30 * time.Second
	openAIOAuthAdmissionStateTTL              = 10 * time.Minute
	openAIOAuthAdmissionMaxAccounts           = 4096
)

// OpenAIOAuthAdmissionError reports a local, provider-protective rejection.
// No upstream request was made, so callers should return 429 without trying a
// different identity. RetryAfter is a lower bound derived from the local queue
// or the provider's known quota/cooldown reset.
type OpenAIOAuthAdmissionError struct {
	Reason     string
	RetryAfter time.Duration
}

func (e *OpenAIOAuthAdmissionError) Error() string {
	if e == nil {
		return "OpenAI OAuth account admission delayed"
	}
	if e.RetryAfter > 0 {
		return fmt.Sprintf("OpenAI OAuth account admission delayed (%s, retry after %s)", e.Reason, e.RetryAfter.Round(time.Millisecond))
	}
	return fmt.Sprintf("OpenAI OAuth account admission delayed (%s)", e.Reason)
}

// RetryAfterSeconds returns an RFC-compatible Retry-After delta, rounded up.
func (e *OpenAIOAuthAdmissionError) RetryAfterSeconds() int {
	if e == nil || e.RetryAfter <= 0 {
		return 1
	}
	seconds := int((e.RetryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

type openAIOAuthAdmissionPolicy struct {
	baseInterval          time.Duration
	highQuotaInterval     time.Duration
	criticalQuotaInterval time.Duration
	severeQuotaInterval   time.Duration
	maxQueueWait          time.Duration
	stateTTL              time.Duration
	maxAccounts           int
}

func defaultOpenAIOAuthAdmissionPolicy() openAIOAuthAdmissionPolicy {
	return openAIOAuthAdmissionPolicy{
		baseInterval:          openAIOAuthAdmissionBaseInterval,
		highQuotaInterval:     openAIOAuthAdmissionHighQuotaInterval,
		criticalQuotaInterval: openAIOAuthAdmissionCriticalQuotaInterval,
		severeQuotaInterval:   openAIOAuthAdmissionSevereQuotaInterval,
		maxQueueWait:          openAIOAuthAdmissionMaxQueueWait,
		stateTTL:              openAIOAuthAdmissionStateTTL,
		maxAccounts:           openAIOAuthAdmissionMaxAccounts,
	}
}

func (p openAIOAuthAdmissionPolicy) normalized() openAIOAuthAdmissionPolicy {
	defaults := defaultOpenAIOAuthAdmissionPolicy()
	if p.baseInterval <= 0 {
		p.baseInterval = defaults.baseInterval
	}
	if p.highQuotaInterval < p.baseInterval {
		p.highQuotaInterval = p.baseInterval
	}
	if p.criticalQuotaInterval < p.highQuotaInterval {
		p.criticalQuotaInterval = p.highQuotaInterval
	}
	if p.severeQuotaInterval < p.criticalQuotaInterval {
		p.severeQuotaInterval = p.criticalQuotaInterval
	}
	if p.maxQueueWait <= 0 {
		p.maxQueueWait = defaults.maxQueueWait
	}
	if p.stateTTL <= 0 {
		p.stateTTL = defaults.stateTTL
	}
	if p.maxAccounts <= 0 {
		p.maxAccounts = defaults.maxAccounts
	}
	return p
}

type openAIOAuthAdmissionState struct {
	nextStart time.Time
	notBefore time.Time
	lastSeen  time.Time
}

// openAIOAuthAdmissionController is deliberately process-local. Distributed
// account concurrency and durable cooldown remain owned by the existing Redis
// and account-repository mechanisms; this controller closes the sub-second
// burst window inside each gateway process without adding a second source of
// truth for provider quota.
type openAIOAuthAdmissionController struct {
	mu       sync.Mutex
	accounts map[int64]*openAIOAuthAdmissionState
	policy   openAIOAuthAdmissionPolicy
}

func newOpenAIOAuthAdmissionController(policy openAIOAuthAdmissionPolicy) *openAIOAuthAdmissionController {
	return &openAIOAuthAdmissionController{
		accounts: make(map[int64]*openAIOAuthAdmissionState),
		policy:   policy.normalized(),
	}
}

// wait reserves one request-start instant. Reservations are serialized per
// account and bounded by one total queue budget. A canceled reservation may
// leave a conservative gap, but can never allow a later request to start early.
func (c *openAIOAuthAdmissionController) wait(
	ctx context.Context,
	accountID int64,
	interval time.Duration,
	notBefore time.Time,
) error {
	if c == nil || accountID <= 0 || interval <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	startedAt := time.Now()
	queueDeadline := startedAt.Add(c.policy.maxQueueWait)
	for {
		scheduledAt, err := c.reserve(accountID, interval, notBefore, queueDeadline)
		if err != nil {
			return err
		}

		wait := time.Until(scheduledAt)
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return ctx.Err()
			case <-timer.C:
			}
		}

		// A concurrent 429 may have extended notBefore while this request was
		// sleeping. Re-reserve instead of releasing a herd at the old instant.
		now := time.Now()
		deferredUntil := c.accountNotBefore(accountID)
		if deferredUntil.After(now) {
			notBefore = deferredUntil
			continue
		}
		return nil
	}
}

func (c *openAIOAuthAdmissionController) reserve(
	accountID int64,
	interval time.Duration,
	notBefore time.Time,
	queueDeadline time.Time,
) (time.Time, error) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.accounts[accountID]
	if !ok {
		c.pruneLocked(now)
		if len(c.accounts) >= c.policy.maxAccounts {
			return time.Time{}, &OpenAIOAuthAdmissionError{
				Reason:     "local_state_capacity",
				RetryAfter: c.policy.baseInterval,
			}
		}
		state = &openAIOAuthAdmissionState{}
		c.accounts[accountID] = state
	}
	state.lastSeen = now
	if notBefore.After(state.notBefore) {
		state.notBefore = notBefore
	}

	scheduledAt := now
	if state.nextStart.After(scheduledAt) {
		scheduledAt = state.nextStart
	}
	if state.notBefore.After(scheduledAt) {
		scheduledAt = state.notBefore
	}
	if scheduledAt.After(queueDeadline) {
		retryAfter := scheduledAt.Sub(now)
		if retryAfter < c.policy.baseInterval {
			retryAfter = c.policy.baseInterval
		}
		return time.Time{}, &OpenAIOAuthAdmissionError{
			Reason:     "bounded_queue_full",
			RetryAfter: retryAfter,
		}
	}

	state.nextStart = scheduledAt.Add(interval)
	return scheduledAt, nil
}

func (c *openAIOAuthAdmissionController) deferUntil(accountID int64, until time.Time) {
	if c == nil || accountID <= 0 || until.IsZero() {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.accounts[accountID]
	if !ok {
		c.pruneLocked(now)
		if len(c.accounts) >= c.policy.maxAccounts {
			return
		}
		state = &openAIOAuthAdmissionState{}
		c.accounts[accountID] = state
	}
	state.lastSeen = now
	if until.After(state.notBefore) {
		state.notBefore = until
	}
}

func (c *openAIOAuthAdmissionController) clearDefer(accountID int64) {
	if c == nil || accountID <= 0 {
		return
	}
	c.mu.Lock()
	if state := c.accounts[accountID]; state != nil {
		state.notBefore = time.Time{}
		state.lastSeen = time.Now()
	}
	c.mu.Unlock()
}

func (c *openAIOAuthAdmissionController) accountNotBefore(accountID int64) time.Time {
	if c == nil || accountID <= 0 {
		return time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if state := c.accounts[accountID]; state != nil {
		return state.notBefore
	}
	return time.Time{}
}

func (c *openAIOAuthAdmissionController) pruneLocked(now time.Time) {
	cutoff := now.Add(-c.policy.stateTTL)
	for accountID, state := range c.accounts {
		if state == nil || (state.lastSeen.Before(cutoff) && !state.nextStart.After(now) && !state.notBefore.After(now)) {
			delete(c.accounts, accountID)
		}
	}
}

func (s *OpenAIGatewayService) getOpenAIOAuthAdmissionController() *openAIOAuthAdmissionController {
	if s == nil {
		return nil
	}
	s.openAIOAuthAdmissionOnce.Do(func() {
		if s.openAIOAuthAdmission == nil {
			s.openAIOAuthAdmission = newOpenAIOAuthAdmissionController(defaultOpenAIOAuthAdmissionPolicy())
		}
	})
	return s.openAIOAuthAdmission
}

// WaitForOpenAIOAuthAccountAdmission is the terminal, post-slot request-start
// guard used by HTTP and WebSocket gateway paths. It never increases an
// upstream allowance: configured base_rpm can only lengthen the default
// interval, and quota pressure can only slow it further.
func (s *OpenAIGatewayService) WaitForOpenAIOAuthAccountAdmission(ctx context.Context, account *Account) error {
	if s == nil || account == nil || !account.IsOpenAIOAuthLike() {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)

	now := time.Now()
	controller := s.getOpenAIOAuthAdmissionController()
	if controller == nil {
		return nil
	}

	if paused, decision := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
		resetAt := openAIOAuthAdmissionQuotaResetAt(account.Extra, decision.window, now)
		if !resetAt.After(now) {
			resetAt = now.Add(openAIOAuthAdmissionUnknownResetCooldown)
		}
		controller.deferUntil(account.ID, resetAt)
		reason := "quota"
		if decision.window != "" {
			reason += "_" + decision.window
		}
		return &OpenAIOAuthAdmissionError{
			Reason:     reason,
			RetryAfter: resetAt.Sub(now),
		}
	}

	notBefore := s.openAIOAuthAdmissionNotBefore(account, now)
	if notBefore.After(now) {
		controller.deferUntil(account.ID, notBefore)
		if notBefore.After(now.Add(controller.policy.maxQueueWait)) {
			return &OpenAIOAuthAdmissionError{
				Reason:     "account_cooldown",
				RetryAfter: notBefore.Sub(now),
			}
		}
	}

	interval := openAIOAuthAdmissionInterval(account, now, controller.policy)
	return controller.wait(ctx, account.ID, interval, notBefore)
}

func (s *OpenAIGatewayService) deferOpenAIOAuthAdmission(accountID int64, until time.Time) {
	if s == nil || accountID <= 0 || until.IsZero() {
		return
	}
	if controller := s.getOpenAIOAuthAdmissionController(); controller != nil {
		controller.deferUntil(accountID, until)
	}
}

func (s *OpenAIGatewayService) clearOpenAIOAuthAdmissionDefer(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	if controller := s.getOpenAIOAuthAdmissionController(); controller != nil {
		controller.clearDefer(accountID)
	}
}

func (s *OpenAIGatewayService) openAIOAuthAdmissionNotBefore(account *Account, now time.Time) time.Time {
	var notBefore time.Time
	advance := func(candidate *time.Time) {
		if candidate != nil && candidate.After(now) && candidate.After(notBefore) {
			notBefore = *candidate
		}
	}
	advance(account.RateLimitResetAt)
	advance(account.OverloadUntil)
	advance(account.TempUnschedulableUntil)

	if s != nil {
		if value, ok := s.openaiAccountRuntimeBlockUntil.Load(account.ID); ok {
			if until, valid := value.(time.Time); valid && until.After(now) && until.After(notBefore) {
				notBefore = until
			}
		}
		if value, ok := s.openaiOAuth429RetryStartedAt.Load(account.ID); ok {
			if state, valid := normalizeOpenAIOAuth429RetryState(value, account.ID); valid && now.Before(state.deadline) && state.retryNotBefore.After(notBefore) {
				notBefore = state.retryNotBefore
			}
		}
	}
	return notBefore
}

func openAIOAuthAdmissionInterval(account *Account, now time.Time, policy openAIOAuthAdmissionPolicy) time.Duration {
	policy = policy.normalized()
	interval := policy.baseInterval
	if account == nil {
		return interval
	}
	if baseRPM := account.GetBaseRPM(); baseRPM > 0 {
		if rpmInterval := time.Minute / time.Duration(baseRPM); rpmInterval > interval {
			interval = rpmInterval
		}
	}
	if configuredMS, ok := resolveAccountExtraNumber(account.Extra, "openai_min_request_interval_ms"); ok && configuredMS > 0 {
		// Avoid float-to-duration overflow on malformed JSON while retaining a
		// generous operator-controlled upper bound.
		configuredMS = math.Min(configuredMS, float64((10*time.Minute)/time.Millisecond))
		if configured := time.Duration(configuredMS * float64(time.Millisecond)); configured > interval {
			interval = configured
		}
	}

	maxUtilization := float64(0)
	for _, window := range []string{"5h", "7d"} {
		if utilization, ok := resolveOpenAIQuotaUtilization(account.Extra, window, now); ok && utilization > maxUtilization {
			maxUtilization = utilization
		}
	}
	switch {
	case maxUtilization >= 0.97 && policy.severeQuotaInterval > interval:
		interval = policy.severeQuotaInterval
	case maxUtilization >= 0.90 && policy.criticalQuotaInterval > interval:
		interval = policy.criticalQuotaInterval
	case maxUtilization >= 0.80 && policy.highQuotaInterval > interval:
		interval = policy.highQuotaInterval
	}
	return interval
}

func openAIOAuthAdmissionQuotaResetAt(extra map[string]any, window string, now time.Time) time.Time {
	if len(extra) == 0 || (window != "5h" && window != "7d") {
		return time.Time{}
	}
	if raw, ok := extra["codex_"+window+"_reset_at"]; ok {
		if resetAt, err := parseTime(fmt.Sprint(raw)); err == nil && resetAt.After(now) {
			return resetAt
		}
	}
	resetAfterSeconds := parseExtraInt(extra["codex_"+window+"_reset_after_seconds"])
	if resetAfterSeconds <= 0 {
		return time.Time{}
	}
	updatedAt, err := parseTime(fmt.Sprint(extra["codex_usage_updated_at"]))
	if err != nil {
		return time.Time{}
	}
	resetAt := updatedAt.Add(time.Duration(resetAfterSeconds) * time.Second)
	if resetAt.After(now) {
		return resetAt
	}
	return time.Time{}
}
