package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

const (
	// The scanner wakes once per minute and the account-level interval decides
	// whether an individual account is due.  Keeping one global ticker avoids a
	// goroutine per account while still allowing intervals from 5 minutes to a
	// week.
	openAISessionCleanupScanInterval = time.Minute
	// Give callers a short, cancellable grace period after Start returns before
	// the first scan.  Besides avoiding a burst during process wiring, this makes
	// Start followed immediately by Stop deterministic: shutdown can cancel the
	// worker before it reaches the repository or upstream session client.  The
	// normal recurring cadence remains one minute after this initial pass.
	openAISessionCleanupStartupGrace = 100 * time.Millisecond
	// Keep the scan lock longer than one account's bounded cleanup call.  The
	// worker processes accounts serially, so a two-minute lock could expire at
	// the same moment as a slow upstream call and let a second instance start a
	// duplicate scan.  There is no lock-renewal primitive in LeaderLockCache;
	// use a conservative five-minute crash lease instead.
	openAISessionCleanupLockTTL       = 5 * time.Minute
	openAISessionCleanupAccountTTL    = 2 * time.Minute
	openAISessionCleanupLeaderLockKey = "jobs:openai-session-cleanup"
	openAISessionCleanupStateWriteTTL = 5 * time.Second
	openAISessionCleanupPageSize      = 100
	openAISessionCleanupMaxPages      = 10000
	// Exported for diagnostics and integration tests; production code should
	// use the service lifecycle rather than relying on these timings directly.
	OpenAISessionCleanupLeaderLockKey = openAISessionCleanupLeaderLockKey
)

var (
	// ErrOpenAISessionCleanupDisabled is returned by the explicit run endpoint
	// when an account has not opted into periodic cleanup.  Keeping this a
	// structured application error gives the admin API a useful 400 reason while
	// preserving ordinary error wrapping for callers outside HTTP.
	ErrOpenAISessionCleanupDisabled = infraerrors.BadRequest(
		"OPENAI_SESSION_CLEANUP_DISABLED",
		"OpenAI session cleanup is disabled for this account",
	)
	ErrOpenAISessionCleanupAccountInvalid = infraerrors.BadRequest(
		"OPENAI_SESSION_CLEANUP_ACCOUNT_INVALID",
		"account is not an active OpenAI OAuth parent account",
	)
	ErrOpenAISessionCleanupAlreadyRunning = infraerrors.Conflict(
		"OPENAI_SESSION_CLEANUP_ALREADY_RUNNING",
		"OpenAI session cleanup is already running for this account",
	)
)

// OpenAISessionCleanupStatus values are persisted in the account's
// openai_session_cleanup_state object.  They are intentionally small and
// stable because the frontend may retain a state snapshot across upgrades.
const (
	OpenAISessionCleanupStatusRunning = "running"
	OpenAISessionCleanupStatusSuccess = "success"
	OpenAISessionCleanupStatusSkipped = "skipped"
	OpenAISessionCleanupStatusFailed  = "failed"
)

// OpenAISessionCleanupState is a redacted runtime projection.  It contains no
// upstream session identifiers or token material; only aggregate counts and
// timestamps are retained for the account-management UI.
type OpenAISessionCleanupState struct {
	Status              string `json:"status"`
	LastRunAt           string `json:"last_run_at,omitempty"`
	LastSuccessAt       string `json:"last_success_at,omitempty"`
	LastResultAt        string `json:"last_result_at,omitempty"`
	RevokedCount        int    `json:"revoked_count"`
	FailedCount         int    `json:"failed_count"`
	CurrentSessionKnown bool   `json:"current_session_known"`
	ErrorCode           string `json:"error_code,omitempty"`
	Message             string `json:"message,omitempty"`
}

// OpenAINonCurrentSessionRevokeState is a descriptive alias for callers that
// use the policy terminology rather than the UI's cleanup terminology.
type OpenAINonCurrentSessionRevokeState = OpenAISessionCleanupState

// OpenAISessionCleanupClient is the upstream session-control seam.  The
// production OpenAIQuotaService implements it; a narrow interface keeps the
// worker straightforward to unit-test without HTTP or OAuth setup.
type OpenAISessionCleanupClient interface {
	ListSessions(ctx context.Context, accountID int64) (*OpenAIAccountSessionList, error)
	RevokeSession(ctx context.Context, accountID int64, sessionID string) error
}

// OpenAISessionCleanupBatchClient is optional.  Production uses the batch
// endpoint to reuse refreshed credentials and an HTTP client; test doubles or
// older providers can expose only RevokeSession and are handled as a fallback.
type OpenAISessionCleanupBatchClient interface {
	RevokeSessions(ctx context.Context, accountID int64, sessionIDs []string) (*OpenAIAccountSessionBatchRevokeResult, error)
}

// OpenAISessionCleanupService periodically removes every revokable,
// non-current ChatGPT device session for active OpenAI OAuth parent accounts
// while the global policy is enabled. It deliberately fails closed whenever
// the upstream response does not include an authoritative current-device marker.
type OpenAISessionCleanupService struct {
	accountRepo    AccountRepository
	sessionClient  OpenAISessionCleanupClient
	leaderLock     LeaderLockCache
	settingService *SettingService
	owner          string

	ctx    context.Context
	cancel context.CancelFunc
	start  sync.Once
	stop   sync.Once
	wg     sync.WaitGroup

	mu sync.Mutex
	// stopping is set before cancellation in Stop.  It closes the small race
	// between a scanner passing its context check and reserving an account while
	// shutdown is already waiting on the worker WaitGroup.  Without this bit a
	// goroutine that had just been scheduled could start one more upstream call
	// after Stop returned (or make Stop's zero-work lifecycle tests flaky).
	stopping bool
	lastRuns map[int64]time.Time
	running  map[int64]struct{}
	now      func() time.Time
}

// OpenAISessionCleanupWorker is retained as a semantic alias for callers that
// register background jobs by worker rather than service terminology.
type OpenAISessionCleanupWorker = OpenAISessionCleanupService

// Compatibility aliases for integrations that use the original policy name.
type OpenAINonCurrentSessionRevokeService = OpenAISessionCleanupService
type OpenAINonCurrentSessionRevokeWorker = OpenAISessionCleanupService
type OpenAIAutoRevokeNonCurrentSessionsService = OpenAISessionCleanupService
type OpenAIAutoRevokeNonCurrentSessionsWorker = OpenAISessionCleanupService
type OpenAIAutoRevokeNonCurrentSessionsState = OpenAISessionCleanupState

// NewOpenAISessionCleanupService constructs the periodic worker.  The worker
// is inert until Start is called, matching the lifecycle of the other service
// schedulers in this package.
func NewOpenAISessionCleanupService(
	accountRepo AccountRepository,
	sessionClient OpenAISessionCleanupClient,
	leaderLocks ...LeaderLockCache,
) *OpenAISessionCleanupService {
	var leaderLock LeaderLockCache
	if len(leaderLocks) > 0 {
		leaderLock = leaderLocks[0]
	}
	// Normalize typed-nil interface values at construction time.  Without this,
	// an optional *Repository/*QuotaService/*Redis client stored in an interface
	// compares non-nil and can panic later in the scanner.
	if isNilCleanupDependency(accountRepo) {
		accountRepo = nil
	}
	if isNilCleanupDependency(sessionClient) {
		sessionClient = nil
	}
	if isNilCleanupDependency(leaderLock) {
		leaderLock = nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &OpenAISessionCleanupService{
		accountRepo:   accountRepo,
		sessionClient: sessionClient,
		leaderLock:    leaderLock,
		owner:         uuid.NewString(),
		ctx:           ctx,
		cancel:        cancel,
		lastRuns:      make(map[int64]time.Time),
		running:       make(map[int64]struct{}),
		now:           time.Now,
	}
}

// SetSettingService switches the worker to the installation-wide cleanup
// policy.  Keeping this setter preserves the historical constructor used by
// integrations and unit tests; a nil setting service retains legacy
// account-extra behavior.
func (s *OpenAISessionCleanupService) SetSettingService(settings *SettingService) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.settingService = settings
	s.mu.Unlock()
}

func (s *OpenAISessionCleanupService) globalCleanupConfig(ctx context.Context) (OpenAISessionCleanupGlobalSettings, bool) {
	if s == nil {
		return OpenAISessionCleanupGlobalSettings{}, false
	}
	s.mu.Lock()
	settings := s.settingService
	s.mu.Unlock()
	if settings == nil {
		return OpenAISessionCleanupGlobalSettings{}, false
	}
	resolved, err := settings.GetOpenAISessionCleanupGlobalSettings(ctx)
	if err != nil || resolved == nil {
		return OpenAISessionCleanupGlobalSettings{IntervalMinutes: OpenAISessionCleanupDefaultIntervalMinutes}, true
	}
	return *resolved, true
}

// NewOpenAINonCurrentSessionRevokeService is an alias constructor for callers
// that use the feature's policy-oriented name.
func NewOpenAINonCurrentSessionRevokeService(
	accountRepo AccountRepository,
	sessionClient OpenAISessionCleanupClient,
	leaderLocks ...LeaderLockCache,
) *OpenAISessionCleanupService {
	return NewOpenAISessionCleanupService(accountRepo, sessionClient, leaderLocks...)
}

// NewOpenAISessionCleanupWorker is an alias constructor for job registries
// that use the worker spelling.
func NewOpenAISessionCleanupWorker(
	accountRepo AccountRepository,
	sessionClient OpenAISessionCleanupClient,
	leaderLocks ...LeaderLockCache,
) *OpenAISessionCleanupWorker {
	return NewOpenAISessionCleanupService(accountRepo, sessionClient, leaderLocks...)
}

// NewOpenAIAutoRevokeNonCurrentSessionsService is a compatibility constructor
// for the initial auto-revoke naming.
func NewOpenAIAutoRevokeNonCurrentSessionsService(
	accountRepo AccountRepository,
	sessionClient OpenAISessionCleanupClient,
	leaderLocks ...LeaderLockCache,
) *OpenAISessionCleanupService {
	return NewOpenAISessionCleanupService(accountRepo, sessionClient, leaderLocks...)
}

func NewOpenAIAutoRevokeNonCurrentSessionsWorker(
	accountRepo AccountRepository,
	sessionClient OpenAISessionCleanupClient,
	leaderLocks ...LeaderLockCache,
) *OpenAISessionCleanupService {
	return NewOpenAISessionCleanupService(accountRepo, sessionClient, leaderLocks...)
}

// Start launches the minute scanner. It performs one scan after a short,
// cancellable startup grace period and then wakes once per minute; the global
// settings policy determines whether cleanup is enabled and when each account
// is due.
func (s *OpenAISessionCleanupService) Start() {
	if s == nil || isNilCleanupDependency(s.accountRepo) || isNilCleanupDependency(s.sessionClient) {
		return
	}
	// A zero-value service, or one that was stopped before it was started, has
	// no live lifecycle context.  Keep Start inert in both cases rather than
	// launching a goroutine that can only exit immediately (or dereference a nil
	// context in a hand-assembled test deployment).
	if s.ctx == nil || s.ctx.Err() != nil {
		return
	}
	s.start.Do(func() {
		// Serialize the stopping check with Stop's flag update and the WaitGroup
		// increment.  This prevents the classic Add-after-Wait race when Stop is
		// called concurrently with the first Start invocation.
		s.mu.Lock()
		if s.stopping || s.ctx == nil || s.ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			startupTimer := time.NewTimer(openAISessionCleanupStartupGrace)
			defer startupTimer.Stop()
			select {
			case <-s.ctx.Done():
				return
			case <-startupTimer.C:
			}
			if s.isStopping() || s.ctx.Err() != nil {
				return
			}
			if err := s.RunDue(s.ctx); err != nil && !isCleanupContextError(err) {
				slog.Warn("openai_session_cleanup_run_failed", "error", err)
			}

			ticker := time.NewTicker(openAISessionCleanupScanInterval)
			defer ticker.Stop()
			for {
				select {
				case <-s.ctx.Done():
					return
				case <-ticker.C:
					if err := s.RunDue(s.ctx); err != nil && !isCleanupContextError(err) {
						slog.Warn("openai_session_cleanup_run_failed", "error", err)
					}
				}
			}
		}()
	})
}

// Stop is idempotent and waits for an in-flight scan to finish.
func (s *OpenAISessionCleanupService) Stop() {
	if s == nil {
		return
	}
	s.stop.Do(func() {
		// Mark stopping before canceling so a scanner that is between account
		// eligibility and cleanup cannot reserve new work while Stop waits.
		s.mu.Lock()
		s.stopping = true
		cancel := s.cancel
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	s.wg.Wait()
}

// RunOnce executes one leader-gated scan immediately.  It is exported so an
// admin endpoint and focused tests can trigger the same logic as the ticker.
// The error return mirrors the other maintenance workers; callers that only
// need fire-and-forget behavior may ignore it as a standalone statement.
func (s *OpenAISessionCleanupService) RunOnce(ctx context.Context) error {
	return s.RunDue(ctx)
}

// RunDue executes one bounded periodic cleanup pass and reports repository
// failures to scheduler callers.  The background loop historically used the
// fire-and-forget RunOnce spelling; keeping both forms lets integrations follow
// the RunDue convention used by the other account maintenance services without
// changing the existing lifecycle behavior.
func (s *OpenAISessionCleanupService) RunDue(ctx context.Context) error {
	if s == nil || isNilCleanupDependency(s.accountRepo) || isNilCleanupDependency(s.sessionClient) {
		return nil
	}
	if s.isStopping() {
		return context.Canceled
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	leaderLock := s.leaderLock
	if isNilCleanupDependency(leaderLock) {
		leaderLock = nil
	}
	release, ok := tryAcquireSingletonLeaderLock(ctx, leaderLock, nil, openAISessionCleanupLeaderLockKey, s.owner, openAISessionCleanupLockTTL)
	if !ok {
		return nil
	}
	defer release()
	accounts, err := s.listCleanupAccounts(ctx)
	if err != nil {
		return err
	}
	return s.processCleanupAccounts(ctx, accounts)
}

// runOnce is kept as a package-local spelling for service tests that exercise
// existing scheduler conventions.
func (s *OpenAISessionCleanupService) runOnce(ctx context.Context) { s.RunOnce(ctx) }

func (s *OpenAISessionCleanupService) scanAccounts(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	if s.isStopping() {
		return
	}
	// Prefer the bounded, filtered repository query so a large installation does
	// not ask the database to hydrate every account in one query.  The repository's
	// generic `status=active` filter also applies schedulable/rate-limit
	// predicates, so the paged call intentionally filters only by OpenAI + OAuth;
	// this worker then applies the exact active-account check below.  That keeps
	// session hygiene independent of traffic admission.  The ListByPlatform
	// fallback keeps reduced repositories and older test doubles source
	// compatible; it is also useful when a deployment has not implemented the
	// paginated method yet.
	accounts, err := s.listCleanupAccounts(ctx)
	if err != nil {
		slog.Warn("openai_session_cleanup_scan_failed", "error", err)
		return
	}
	if err := s.processCleanupAccounts(ctx, accounts); err != nil && !isCleanupContextError(err) {
		slog.Warn("openai_session_cleanup_scan_failed", "error", err)
	}
}

func (s *OpenAISessionCleanupService) processCleanupAccounts(ctx context.Context, accounts []Account) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var firstErr error
	now := s.clockNow()
	// The policy is installation-wide, so resolve it once per scan rather than
	// issuing one settings lookup for every account in the page.
	global, hasGlobal := s.globalCleanupConfig(ctx)
	for i := range accounts {
		if err := ctx.Err(); err != nil || s.isStopping() {
			if err != nil {
				return err
			}
			return context.Canceled
		}
		account := &accounts[i]
		if account == nil || account.ID <= 0 || !account.IsActive() || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsShadow() {
			continue
		}
		config := ResolveOpenAINonCurrentSessionRevokeConfig(account)
		if hasGlobal {
			config.Enabled = global.Enabled
			config.IntervalMinutes = global.IntervalMinutes
		}
		if !config.Enabled || !s.claimDue(account, config.IntervalMinutes, now) {
			continue
		}
		// Stop may race with claimDue; do not begin an upstream call after the
		// shutdown flag is visible.  claimDue reserves the account so a later
		// scanner cannot overlap it, while this check avoids starting work that
		// Stop is about to cancel.
		if s.isStopping() || ctx.Err() != nil {
			s.releaseAccountRun(account.ID)
			if err := ctx.Err(); err != nil {
				return err
			}
			return context.Canceled
		}
		if err := s.cleanupAccount(ctx, account, now); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// listCleanupAccounts returns active OpenAI accounts using a bounded query when
// available, with a compatibility fallback for repositories that only provide
// the older ListByPlatform implementation.  Some unit/integration fakes embed a
// nil AccountRepository and therefore panic when a promoted method is invoked;
// the safe wrappers convert that shape into a normal fallback instead of
// crashing the background worker.
func (s *OpenAISessionCleanupService) listCleanupAccounts(ctx context.Context) ([]Account, error) {
	if s == nil || isNilCleanupDependency(s.accountRepo) {
		return nil, errors.New("account repository is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	accounts, err, panicked := listCleanupAccountsPaged(s.accountRepo, ctx)
	if !panicked {
		if err != nil {
			return nil, err
		}
		if len(accounts) > 0 {
			return accounts, nil
		}
		// An older fake may implement ListWithFilters as a no-op while exposing
		// useful data through ListByPlatform.  An empty result is therefore the
		// one case where the compatibility path is worth probing as well.
	}

	fallback, fallbackErr, fallbackPanicked := listCleanupAccountsByPlatform(s.accountRepo, ctx)
	if !fallbackPanicked {
		if fallbackErr != nil {
			return nil, fallbackErr
		}
		return fallback, nil
	}
	return nil, errors.New("account repository does not expose a usable OpenAI account listing")
}

func listCleanupAccountsPaged(repo AccountRepository, ctx context.Context) (accounts []Account, err error, panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			accounts = nil
			err = nil
			panicked = true
		}
	}()
	accounts = make([]Account, 0, openAISessionCleanupPageSize)
	for page := 1; page <= openAISessionCleanupMaxPages; page++ {
		pageAccounts, pageInfo, pageErr := repo.ListWithFilters(ctx, pagination.PaginationParams{
			Page:     page,
			PageSize: openAISessionCleanupPageSize,
		}, PlatformOpenAI, AccountTypeOAuth, "", "", 0, "")
		if pageErr != nil {
			return nil, pageErr, false
		}
		accounts = append(accounts, pageAccounts...)
		// A short page is definitive even when the repository omits pagination
		// metadata.  When metadata is present, only a positive Pages value is
		// authoritative: lightweight fakes (and a few legacy repositories)
		// commonly return Pages=0 while still returning a full page.  Continue in
		// that case and let the next short page terminate the scan; the hard page
		// cap below prevents an unbounded loop if a broken implementation keeps
		// returning full pages forever.
		if len(pageAccounts) < openAISessionCleanupPageSize || (pageInfo != nil && pageInfo.Pages > 0 && page >= pageInfo.Pages) {
			return accounts, nil, false
		}
	}
	return accounts, nil, false
}

func listCleanupAccountsByPlatform(repo AccountRepository, ctx context.Context) (accounts []Account, err error, panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			accounts = nil
			err = nil
			panicked = true
		}
	}()
	accounts, err = repo.ListByPlatform(ctx, PlatformOpenAI)
	return accounts, err, false
}

// ScanAccounts is an exported alias useful to diagnostics and integration
// tests. It has the same leader gating as RunOnce.
func (s *OpenAISessionCleanupService) ScanAccounts(ctx context.Context) { s.RunOnce(ctx) }

func (s *OpenAISessionCleanupService) clockNow() time.Time {
	s.mu.Lock()
	now := s.now
	s.mu.Unlock()
	if now == nil {
		return time.Now()
	}
	return now()
}

func (s *OpenAISessionCleanupService) isStopping() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	return stopping
}

// SetClock allows deterministic due/interval tests. It is intentionally tiny
// and safe to call before Start; production code never needs it.
func (s *OpenAISessionCleanupService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if now == nil {
		s.now = time.Now
	} else {
		s.now = now
	}
	s.mu.Unlock()
}

func (s *OpenAISessionCleanupService) claimDue(account *Account, intervalMinutes int, now time.Time) bool {
	if account == nil || account.ID <= 0 || intervalMinutes <= 0 {
		return false
	}
	interval := time.Duration(intervalMinutes) * time.Minute
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastRuns == nil {
		s.lastRuns = make(map[int64]time.Time)
	}
	if s.running == nil {
		s.running = make(map[int64]struct{})
	}
	if s.stopping {
		return false
	}
	if _, ok := s.running[account.ID]; ok {
		return false
	}
	if last, ok := s.lastRuns[account.ID]; ok && now.Before(last.Add(interval)) {
		return false
	}
	// Keep the interval across process restarts when the previous run state was
	// persisted in accounts.extra.  A missing/malformed timestamp intentionally
	// falls through and runs immediately.
	if persisted := persistedCleanupLastRun(account); !persisted.IsZero() && now.Before(persisted.Add(interval)) {
		return false
	}
	// Reserve before network I/O so a manually triggered scan cannot overlap a
	// ticker scan in the same process. A failed upstream call is retried after
	// the configured interval rather than hammering ChatGPT every minute.
	s.lastRuns[account.ID] = now
	s.running[account.ID] = struct{}{}
	return true
}

// RunAccount executes cleanup for one account regardless of its due timestamp.
// The account is reloaded first so an admin can disable the policy while a
// previous scan is still in flight.  It still honors the global enabled switch
// (or the legacy account switch when no SettingService is injected).
func (s *OpenAISessionCleanupService) RunAccount(ctx context.Context, accountID int64) error {
	return s.runAccount(ctx, accountID, true)
}

// RunAccountNow executes an explicit operator-triggered cleanup without
// requiring the periodic scheduler to be enabled.  The global switch controls
// scheduled runs; the dedicated Account Device Sessions menu must still be able
// to perform a one-off cleanup when that scheduler is turned off.
func (s *OpenAISessionCleanupService) RunAccountNow(ctx context.Context, accountID int64) error {
	return s.runAccount(ctx, accountID, false)
}

func (s *OpenAISessionCleanupService) runAccount(ctx context.Context, accountID int64, requireEnabled bool) error {
	if s == nil || isNilCleanupDependency(s.accountRepo) || isNilCleanupDependency(s.sessionClient) || accountID <= 0 {
		return errors.New("openai session cleanup service is not configured")
	}
	// Manual runs share the worker's lifecycle.  Reject a request that arrives
	// after shutdown starts before touching the repository; otherwise an admin
	// request could reserve work while Stop is waiting for the worker and make
	// shutdown ordering nondeterministic.  Register the run in the same
	// WaitGroup as the ticker goroutine while holding the lifecycle mutex.  This
	// closes the Add-vs-Wait race and makes application cleanup wait for an
	// in-flight admin run before closing Ent/Redis underneath its persistence
	// calls.
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return context.Canceled
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	if ctx == nil {
		ctx = context.Background()
	}
	// Tie an explicit/manual run to the service lifecycle as well as the
	// request context.  A shutdown must cancel an admin-triggered upstream call
	// just like a ticker-driven call; context.AfterFunc avoids a helper goroutine
	// per request and is detached again when the run completes normally.
	if s.ctx != nil {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		lifecycleCtx, lifecycleCancel := context.WithCancel(ctx)
		stopLifecycle := context.AfterFunc(s.ctx, lifecycleCancel)
		defer func() {
			stopLifecycle()
			lifecycleCancel()
		}()
		ctx = lifecycleCtx
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	if s.isStopping() || ctx.Err() != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return context.Canceled
	}
	if account == nil || !account.IsActive() || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.IsShadow() {
		return ErrOpenAISessionCleanupAccountInvalid
	}
	if requireEnabled {
		if global, ok := s.globalCleanupConfig(ctx); ok {
			if !global.Enabled {
				return ErrOpenAISessionCleanupDisabled
			}
		} else if !ResolveOpenAINonCurrentSessionRevokeConfig(account).Enabled {
			return ErrOpenAISessionCleanupDisabled
		}
	}
	now := s.clockNow()
	// Force this invocation while still reserving the account against concurrent
	// ticker work.
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return context.Canceled
	}
	if s.running == nil {
		s.running = make(map[int64]struct{})
	}
	if _, running := s.running[account.ID]; running {
		s.mu.Unlock()
		return ErrOpenAISessionCleanupAlreadyRunning
	}
	if s.lastRuns == nil {
		s.lastRuns = make(map[int64]time.Time)
	}
	s.lastRuns[account.ID] = now
	s.running[account.ID] = struct{}{}
	s.mu.Unlock()
	return s.cleanupAccount(ctx, account, now)
}

func (s *OpenAISessionCleanupService) cleanupAccount(parentCtx context.Context, account *Account, now time.Time) error {
	if account == nil {
		return nil
	}
	defer s.releaseAccountRun(account.ID)
	ctx := parentCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.isStopping() {
		return context.Canceled
	}
	callCtx, cancel := context.WithTimeout(ctx, openAISessionCleanupAccountTTL)
	defer cancel()

	state := OpenAISessionCleanupState{
		Status:    OpenAISessionCleanupStatusRunning,
		LastRunAt: now.UTC().Format(time.RFC3339),
	}
	if previous := decodeCleanupStateFromAccount(account); previous != nil {
		state.LastSuccessAt = previous.LastSuccessAt
	}
	_ = s.persistStateBestEffort(ctx, account.ID, state)

	list, err := s.sessionClient.ListSessions(callCtx, account.ID)
	if err != nil {
		state.Status = OpenAISessionCleanupStatusFailed
		state.ErrorCode = cleanupErrorCode(err)
		// The runtime projection is persisted in account.extra and returned to
		// the admin UI.  Never copy an upstream/client error verbatim here: a
		// wrapped transport error can contain a URL, query string, bearer token,
		// or a session identifier.  Keep the detailed error in process logs and
		// expose only a stable, redacted explanation in durable state.
		state.Message = cleanupSafeErrorMessage(state.ErrorCode)
		state.LastResultAt = s.clockNow().UTC().Format(time.RFC3339)
		_ = s.persistStateBestEffort(ctx, account.ID, state)
		slog.Warn("openai_session_cleanup_list_failed", "account_id", account.ID, "error_code", state.ErrorCode)
		return err
	}
	if list == nil {
		err = errors.New("upstream returned an empty session result")
		state.Status = OpenAISessionCleanupStatusFailed
		state.ErrorCode = "OPENAI_SESSIONS_EMPTY_RESULT"
		state.Message = cleanupSafeErrorMessage(state.ErrorCode)
		state.LastResultAt = s.clockNow().UTC().Format(time.RFC3339)
		_ = s.persistStateBestEffort(ctx, account.ID, state)
		return err
	}
	// A session row carrying an explicit current marker is authoritative for the
	// cleanup contract.  CurrentKnown is decoder provenance and is intentionally
	// not required here: custom providers/test doubles may construct the typed
	// projection directly (with CurrentKnown left at its zero value), while the
	// decoder itself still fails closed by leaving every row non-current whenever
	// the upstream response omitted a marker.
	currentSessionKnown := hasCurrentOpenAISession(list.Sessions)
	state.CurrentSessionKnown = currentSessionKnown
	if !currentSessionKnown {
		// This is the critical safety invariant: an endpoint response without an
		// authoritative current marker must never be interpreted as “all rows are
		// old” and mass-revoked.
		state.Status = OpenAISessionCleanupStatusSkipped
		state.ErrorCode = "OPENAI_CURRENT_SESSION_UNKNOWN"
		state.Message = "upstream did not identify the current device"
		state.LastResultAt = s.clockNow().UTC().Format(time.RFC3339)
		_ = s.persistStateBestEffort(ctx, account.ID, state)
		slog.Warn("openai_session_cleanup_current_unknown", "account_id", account.ID, "session_count", len(list.Sessions))
		return nil
	}

	ids := revokableNonCurrentSessionIDs(list.Sessions)
	if len(ids) == 0 {
		state.Status = OpenAISessionCleanupStatusSuccess
		state.LastSuccessAt = s.clockNow().UTC().Format(time.RFC3339)
		state.LastResultAt = state.LastSuccessAt
		_ = s.persistStateBestEffort(ctx, account.ID, state)
		return nil
	}

	var firstErr error
	for start := 0; start < len(ids); start += maxOpenAIAccountSessionBatchSize {
		end := start + maxOpenAIAccountSessionBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		if err := callCtx.Err(); err != nil {
			firstErr = err
			state.FailedCount += len(ids) - start
			break
		}
		// Give providers an independent slice so an implementation that sorts or
		// normalizes its input cannot mutate the candidate list used by later
		// batches or state accounting.
		batch := append([]string(nil), ids[start:end]...)
		if batchClient, ok := s.sessionClient.(OpenAISessionCleanupBatchClient); ok && !isNilCleanupDependency(batchClient) {
			result, revokeErr := batchClient.RevokeSessions(callCtx, account.ID, batch)
			if revokeErr != nil {
				if firstErr == nil {
					firstErr = revokeErr
				}
				state.FailedCount += len(batch)
				if isCleanupContextError(revokeErr) {
					state.FailedCount += len(ids) - end
					break
				}
				continue
			}
			if result == nil {
				if firstErr == nil {
					firstErr = errors.New("upstream returned an empty revoke result")
				}
				state.FailedCount += len(batch)
				continue
			}
			successCount, failedCount, resultErr := normalizeCleanupBatchResult(result, batch)
			state.RevokedCount += successCount
			state.FailedCount += failedCount
			if resultErr != nil && firstErr == nil {
				firstErr = resultErr
			}
			continue
		}
		cancelled := false
		for index, id := range batch {
			if err := callCtx.Err(); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				state.FailedCount += len(batch) - index
				state.FailedCount += len(ids) - end
				cancelled = true
				break
			}
			if revokeErr := s.sessionClient.RevokeSession(callCtx, account.ID, id); revokeErr != nil {
				state.FailedCount++
				if firstErr == nil {
					firstErr = revokeErr
				}
				if isCleanupContextError(revokeErr) {
					state.FailedCount += len(batch) - index - 1
					state.FailedCount += len(ids) - end
					cancelled = true
					break
				}
				continue
			}
			state.RevokedCount++
		}
		if cancelled {
			break
		}
	}

	state.LastResultAt = s.clockNow().UTC().Format(time.RFC3339)
	if firstErr != nil {
		state.Status = OpenAISessionCleanupStatusFailed
		state.ErrorCode = cleanupErrorCode(firstErr)
		state.Message = cleanupSafeErrorMessage(state.ErrorCode)
	} else {
		state.Status = OpenAISessionCleanupStatusSuccess
		state.LastSuccessAt = state.LastResultAt
	}
	if persistErr := s.persistStateBestEffort(ctx, account.ID, state); persistErr != nil && firstErr == nil {
		firstErr = persistErr
	}
	slog.Info("openai_session_cleanup_complete",
		"account_id", account.ID,
		"revoked_count", state.RevokedCount,
		"failed_count", state.FailedCount,
		"current_session_known", state.CurrentSessionKnown,
	)
	return firstErr
}

func (s *OpenAISessionCleanupService) releaseAccountRun(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.mu.Lock()
	if s.running != nil {
		delete(s.running, accountID)
	}
	s.mu.Unlock()
}

func persistedCleanupLastRun(account *Account) time.Time {
	state := decodeCleanupStateFromAccount(account)
	if state == nil || strings.TrimSpace(state.LastRunAt) == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(state.LastRunAt))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func decodeCleanupStateFromAccount(account *Account) *OpenAISessionCleanupState {
	if account == nil || account.Extra == nil {
		return nil
	}
	if state := decodeOpenAISessionCleanupState(account.Extra[OpenAISessionCleanupStateExtraKey]); state != nil {
		return state
	}
	return decodeOpenAISessionCleanupState(account.Extra[OpenAIAutoRevokeNonCurrentSessionsLegacyStateExtraKey])
}

func revokableNonCurrentSessionIDs(sessions []OpenAIAccountSession) []string {
	ids := make([]string, 0, len(sessions))
	seen := make(map[string]struct{}, len(sessions))
	currentIDs := make(map[string]struct{}, len(sessions))
	// Upstream retries can occasionally duplicate a row with conflicting
	// markers.  Gather every positively identified current ID first so a later
	// non-current duplicate can never cause that ID to be revoked.
	for _, session := range sessions {
		if !session.Current {
			continue
		}
		if id := strings.TrimSpace(session.ID); id != "" && len(id) <= 512 {
			currentIDs[id] = struct{}{}
		}
	}
	for _, session := range sessions {
		if session.Current || !session.CanRevoke {
			continue
		}
		id := strings.TrimSpace(session.ID)
		if id == "" || len(id) > 512 {
			continue
		}
		if _, current := currentIDs[id]; current {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func hasCurrentOpenAISession(sessions []OpenAIAccountSession) bool {
	for _, session := range sessions {
		// A bare boolean marker without a stable session identifier does not
		// identify which device must be preserved.  Treat it as unknown so a
		// malformed upstream row can never turn every other row into a revoke
		// candidate.
		id := strings.TrimSpace(session.ID)
		if session.Current && id != "" && len(id) <= 512 {
			return true
		}
	}
	return false
}

func (s *OpenAISessionCleanupService) persistState(ctx context.Context, accountID int64, state OpenAISessionCleanupState) error {
	if s == nil || isNilCleanupDependency(s.accountRepo) || accountID <= 0 {
		return errors.New("account repository is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// State may carry fields read from a legacy JSONB value (notably
	// LastSuccessAt).  Sanitize at the persistence boundary as well as at API
	// read time so malformed/secret-bearing historical values cannot be copied
	// forward by a failed cleanup run.
	state = sanitizeOpenAISessionCleanupStateForPersistence(state)
	return s.accountRepo.UpdateExtra(ctx, accountID, map[string]any{OpenAISessionCleanupStateExtraKey: state})
}

// persistStateBestEffort records runtime state even when the upstream call was
// canceled by a client disconnect or worker shutdown.  It detaches cancellation
// while retaining a short bounded write deadline, so observability never turns
// into an unbounded shutdown delay.
func (s *OpenAISessionCleanupService) persistStateBestEffort(parent context.Context, accountID int64, state OpenAISessionCleanupState) error {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), openAISessionCleanupStateWriteTTL)
	defer cancel()
	return s.persistState(ctx, accountID, state)
}

// Interface fields can contain a typed nil pointer (for example
// (*OpenAIQuotaService)(nil)), which compares unequal to nil and would panic
// when the worker calls it.  Keep lifecycle guards resilient for providers and
// tests that pass optional dependencies through interfaces.
func isNilCleanupDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func normalizeCleanupBatchResult(result *OpenAIAccountSessionBatchRevokeResult, batch []string) (success, failed int, resultErr error) {
	if result == nil {
		return 0, len(batch), errors.New("upstream returned an empty revoke result")
	}
	requested := len(batch)
	rawSuccess, rawFailed := result.SuccessCount, result.FailedCount
	// A non-zero requested_count is an integrity check supplied by the
	// upstream client.  Older implementations omitted it (zero), so preserve
	// compatibility in that case while marking an explicit mismatch as
	// malformed and accounting conservatively below.
	malformed := result.RequestedCount != 0 && result.RequestedCount != requested
	// Older providers only returned revoked_session_ids/failures.  Derive the
	// counts in that case, while constraining them to the requested batch.
	if rawSuccess == 0 && len(result.RevokedSessionIDs) > 0 {
		rawSuccess = countUniqueCleanupIDs(result.RevokedSessionIDs, batch)
	}
	if rawFailed == 0 && len(result.Failures) > 0 {
		rawFailed = countUniqueCleanupFailureIDs(result.Failures, batch)
	}
	malformed = malformed || rawSuccess < 0 || rawFailed < 0 || rawSuccess > requested || rawFailed > requested || rawSuccess+rawFailed > requested
	if rawSuccess < 0 {
		rawSuccess = 0
	}
	if rawFailed < 0 {
		rawFailed = 0
	}
	if rawSuccess > requested {
		rawSuccess = requested
	}
	if rawFailed > requested-rawSuccess {
		rawFailed = requested - rawSuccess
	}
	// Any unaccounted request is treated as failed rather than silently
	// reporting success.  This keeps state totals conservative on malformed or
	// older upstream responses.
	unaccounted := requested - rawSuccess - rawFailed
	if unaccounted > 0 {
		rawFailed += unaccounted
		malformed = true
	}
	if rawFailed > 0 || malformed {
		message := "one or more session revocations failed"
		if len(result.Failures) > 0 {
			if code := strings.TrimSpace(result.Failures[0].Code); code != "" {
				message = code
			}
		}
		return rawSuccess, rawFailed, errors.New(message)
	}
	return rawSuccess, rawFailed, nil
}

func countUniqueCleanupIDs(ids, batch []string) int {
	allowed := make(map[string]struct{}, len(batch))
	for _, id := range batch {
		allowed[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := allowed[id]; !ok {
			continue
		}
		seen[id] = struct{}{}
	}
	return len(seen)
}

func countUniqueCleanupFailureIDs(failures []OpenAIAccountSessionRevokeFailure, batch []string) int {
	ids := make([]string, 0, len(failures))
	for _, failure := range failures {
		ids = append(ids, failure.SessionID)
	}
	return countUniqueCleanupIDs(ids, batch)
}

func cleanupErrorCode(err error) string {
	if err == nil {
		return ""
	}
	code := strings.TrimSpace(infraerrors.Reason(err))
	// Batch revoke responses carry per-session failure codes in a plain
	// `errors.New(code)` so the worker can continue processing the remainder of
	// the batch.  Recover only an exact allowlisted token from that error text;
	// never persist arbitrary upstream details.
	if code == "" {
		candidate := strings.TrimSpace(err.Error())
		if isAllowedCleanupErrorCode(candidate) {
			code = candidate
		}
	}
	// Error reasons are normally fixed constants from the OpenAI client, but
	// the cleanup seam is intentionally injectable and a provider could return
	// an arbitrary reason.  This value is persisted in account.extra, so keep an
	// explicit allowlist rather than writing potentially sensitive upstream text
	// (or a token-bearing custom reason) to durable state.
	if isAllowedCleanupErrorCode(code) {
		return code
	}
	return "OPENAI_SESSION_CLEANUP_FAILED"
}

func isAllowedCleanupErrorCode(code string) bool {
	switch strings.TrimSpace(code) {
	case "OPENAI_SESSIONS_PROXY_UNAVAILABLE",
		"OPENAI_SESSIONS_AUTH_FAILED",
		"OPENAI_SESSIONS_UPSTREAM_ERROR",
		"OPENAI_SESSIONS_REQUEST_FAILED",
		"OPENAI_SESSIONS_INVALID_RESPONSE",
		"OPENAI_SESSIONS_CLIENT_ERROR",
		"OPENAI_SESSIONS_EMPTY_RESULT",
		"OPENAI_SESSION_REVOKE_PROXY_UNAVAILABLE",
		"OPENAI_SESSION_REVOKE_UPSTREAM_ERROR",
		"OPENAI_SESSION_REVOKE_REQUEST_FAILED",
		"OPENAI_SESSION_NOT_FOUND",
		"OPENAI_SESSION_INVALID_ID",
		"OPENAI_SESSIONS_BATCH_EMPTY",
		"OPENAI_SESSIONS_BATCH_TOO_LARGE",
		"OPENAI_SESSION_CLEANUP_DISABLED",
		"OPENAI_SESSION_CLEANUP_ACCOUNT_INVALID",
		"OPENAI_SESSION_CLEANUP_ALREADY_RUNNING",
		"OPENAI_SESSION_CLEANUP_FAILED",
		"OPENAI_CURRENT_SESSION_UNKNOWN":
		return true
	}
	return false
}

func isCleanupContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// cleanupSafeErrorMessage deliberately uses fixed text instead of an upstream
// error string.  Account extra is durable and may be replicated/backed up, so
// even a best-effort regex redaction would be too easy to bypass with a custom
// transport error.  The structured ErrorCode carries the diagnostic detail and
// is localized by the frontend.
func cleanupSafeErrorMessage(code string) string {
	switch strings.TrimSpace(code) {
	case "OPENAI_SESSIONS_PROXY_UNAVAILABLE":
		return "the account proxy could not connect to ChatGPT"
	case "OPENAI_SESSIONS_AUTH_FAILED":
		return "the ChatGPT session request could not be authenticated"
	case "OPENAI_SESSIONS_UPSTREAM_ERROR", "OPENAI_SESSIONS_REQUEST_FAILED", "OPENAI_SESSIONS_INVALID_RESPONSE":
		return "the ChatGPT session list request failed"
	case "OPENAI_SESSION_REVOKE_PROXY_UNAVAILABLE":
		return "the account proxy could not connect to ChatGPT for logout"
	case "OPENAI_SESSION_REVOKE_UPSTREAM_ERROR", "OPENAI_SESSION_REVOKE_REQUEST_FAILED", "OPENAI_SESSION_NOT_FOUND":
		return "one or more ChatGPT session logouts failed"
	case "OPENAI_SESSIONS_EMPTY_RESULT":
		return "the upstream returned no session result"
	case "OPENAI_CURRENT_SESSION_UNKNOWN":
		return "the upstream did not identify the current device"
	default:
		return "the OpenAI session cleanup request failed"
	}
}

// decodeOpenAISessionCleanupState is intentionally tolerant of JSONB values
// decoded as map[string]any or as the typed struct used by UpdateExtra.
func decodeOpenAISessionCleanupState(value any) *OpenAISessionCleanupState {
	if value == nil {
		return nil
	}
	if state, ok := value.(OpenAISessionCleanupState); ok {
		copy := state
		return &copy
	}
	if state, ok := value.(*OpenAISessionCleanupState); ok && state != nil {
		copy := *state
		return &copy
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var state OpenAISessionCleanupState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return nil
	}
	return &state
}

// DecodeOpenAISessionCleanupState exposes the tolerant state decoder to the
// admin handler without exposing any mutable internal representation.
func DecodeOpenAISessionCleanupState(value any) *OpenAISessionCleanupState {
	return decodeOpenAISessionCleanupState(value)
}

// SanitizeOpenAISessionCleanupState returns the small, durable state projection
// that is safe to expose through the admin API.  Older installations may have
// persisted a free-form provider error/message before the worker introduced
// its allowlist; returning that value verbatim could disclose a bearer token,
// URL, or session identifier.  Normalize timestamps and error codes as well so
// a forged/legacy JSONB value cannot be reflected back to the browser.
func SanitizeOpenAISessionCleanupState(value any) *OpenAISessionCleanupState {
	state := decodeOpenAISessionCleanupState(value)
	if state == nil {
		return nil
	}
	sanitized := *state
	sanitized.Status = sanitizeOpenAISessionCleanupStatus(sanitized.Status)
	sanitized.LastRunAt = sanitizeOpenAISessionCleanupTimestamp(sanitized.LastRunAt)
	sanitized.LastSuccessAt = sanitizeOpenAISessionCleanupTimestamp(sanitized.LastSuccessAt)
	sanitized.LastResultAt = sanitizeOpenAISessionCleanupTimestamp(sanitized.LastResultAt)
	if sanitized.RevokedCount < 0 {
		sanitized.RevokedCount = 0
	}
	if sanitized.FailedCount < 0 {
		sanitized.FailedCount = 0
	}
	code := strings.TrimSpace(sanitized.ErrorCode)
	if code == "" {
		sanitized.ErrorCode = ""
		sanitized.Message = ""
	} else {
		if !isAllowedCleanupErrorCode(code) {
			code = "OPENAI_SESSION_CLEANUP_FAILED"
		}
		sanitized.ErrorCode = code
		sanitized.Message = cleanupSafeErrorMessage(code)
	}
	return &sanitized
}

// sanitizeOpenAISessionCleanupStateForPersistence is the value-form variant
// used by UpdateExtra.  Keeping the conversion in one place ensures every
// worker write validates timestamps and redacts legacy provider messages,
// including the intermediate "running" snapshot written before an upstream
// request starts.
func sanitizeOpenAISessionCleanupStateForPersistence(state OpenAISessionCleanupState) OpenAISessionCleanupState {
	sanitized := SanitizeOpenAISessionCleanupState(state)
	if sanitized == nil {
		return OpenAISessionCleanupState{}
	}
	return *sanitized
}

func sanitizeOpenAISessionCleanupStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case OpenAISessionCleanupStatusRunning,
		OpenAISessionCleanupStatusSuccess,
		OpenAISessionCleanupStatusSkipped,
		OpenAISessionCleanupStatusFailed:
		return strings.TrimSpace(strings.ToLower(status))
	default:
		return ""
	}
}

func sanitizeOpenAISessionCleanupTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	timestamp, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return timestamp.UTC().Format(time.RFC3339)
}
