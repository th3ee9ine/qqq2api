//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

type openAISessionCleanupRepoStub struct {
	AccountRepository

	mu        sync.Mutex
	accounts  []Account
	listErr   error
	getErr    error
	updateErr error
	listCalls int
	getCalls  []int64
	states    map[int64][]OpenAISessionCleanupState
}

var _ AccountRepository = (*openAISessionCleanupRepoStub)(nil)

func (r *openAISessionCleanupRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	result := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			result = append(result, account)
		}
	}
	return result, nil
}

func (r *openAISessionCleanupRepoStub) ListWithFilters(
	ctx context.Context,
	params pagination.PaginationParams,
	platform, accountType, status, _ string,
	_ int64,
	_ string,
) ([]Account, *pagination.PaginationResult, error) {
	accounts, err := r.ListByPlatform(ctx, platform)
	if err != nil {
		return nil, nil, err
	}
	filtered := accounts[:0]
	for i := range accounts {
		if (accountType == "" || accounts[i].Type == accountType) && (status == "" || accounts[i].Status == status) {
			filtered = append(filtered, accounts[i])
		}
	}
	start := params.Offset()
	if start >= len(filtered) {
		return []Account{}, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Total: int64(len(filtered))}, nil
	}
	end := start + params.Limit()
	if end > len(filtered) {
		end = len(filtered)
	}
	pages := (len(filtered) + params.Limit() - 1) / params.Limit()
	return append([]Account(nil), filtered[start:end]...), &pagination.PaginationResult{
		Page: params.Page, PageSize: params.PageSize, Pages: pages, Total: int64(len(filtered)),
	}, nil
}

func (r *openAISessionCleanupRepoStub) GetByID(ctx context.Context, accountID int64) (*Account, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getCalls = append(r.getCalls, accountID)
	if r.getErr != nil {
		return nil, r.getErr
	}
	for i := range r.accounts {
		if r.accounts[i].ID == accountID {
			copy := r.accounts[i]
			return &copy, nil
		}
	}
	return nil, ErrAccountNotFound
}

func (r *openAISessionCleanupRepoStub) UpdateExtra(ctx context.Context, accountID int64, updates map[string]any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	state := decodeOpenAISessionCleanupState(updates[OpenAISessionCleanupStateExtraKey])
	if state != nil {
		if r.states == nil {
			r.states = make(map[int64][]OpenAISessionCleanupState)
		}
		r.states[accountID] = append(r.states[accountID], *state)
	}
	return nil
}

func (r *openAISessionCleanupRepoStub) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listCalls
}

func (r *openAISessionCleanupRepoStub) latestState(accountID int64) *OpenAISessionCleanupState {
	r.mu.Lock()
	defer r.mu.Unlock()
	states := r.states[accountID]
	if len(states) == 0 {
		return nil
	}
	state := states[len(states)-1]
	return &state
}

type openAISessionCleanupPagedRepoStub struct {
	AccountRepository
	mu             sync.Mutex
	accounts       []Account
	pageCalls      []pagination.PaginationParams
	fallbackCalled bool
	omitPages      bool
}

func (r *openAISessionCleanupPagedRepoStub) ListWithFilters(
	ctx context.Context,
	params pagination.PaginationParams,
	platform, accountType, status, search string,
	groupID int64,
	privacyMode string,
) ([]Account, *pagination.PaginationResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if platform != PlatformOpenAI || accountType != AccountTypeOAuth || status != "" || search != "" || groupID != 0 || privacyMode != "" {
		return nil, nil, errors.New("unexpected cleanup account filters")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pageCalls = append(r.pageCalls, params)
	start := params.Offset()
	if start >= len(r.accounts) {
		pages := (len(r.accounts) + params.PageSize - 1) / params.PageSize
		if r.omitPages {
			pages = 0
		}
		return []Account{}, &pagination.PaginationResult{Page: params.Page, PageSize: params.PageSize, Pages: pages, Total: int64(len(r.accounts))}, nil
	}
	end := start + params.Limit()
	if end > len(r.accounts) {
		end = len(r.accounts)
	}
	pages := (len(r.accounts) + params.PageSize - 1) / params.PageSize
	if r.omitPages {
		pages = 0
	}
	return append([]Account(nil), r.accounts[start:end]...), &pagination.PaginationResult{
		Page: params.Page, PageSize: params.PageSize, Pages: pages, Total: int64(len(r.accounts)),
	}, nil
}

func (r *openAISessionCleanupPagedRepoStub) ListByPlatform(context.Context, string) ([]Account, error) {
	r.mu.Lock()
	r.fallbackCalled = true
	r.mu.Unlock()
	return nil, errors.New("unexpected ListByPlatform fallback")
}

type openAISessionCleanupClientStub struct {
	mu sync.Mutex

	lists      map[int64]*OpenAIAccountSessionList
	listErrs   map[int64]error
	revokeErrs map[string]error
	listCalls  []int64
	revoked    []string

	listStarted chan struct{}
	blockList   bool
}

var _ OpenAISessionCleanupClient = (*openAISessionCleanupClientStub)(nil)

func (c *openAISessionCleanupClientStub) ListSessions(ctx context.Context, accountID int64) (*OpenAIAccountSessionList, error) {
	c.mu.Lock()
	c.listCalls = append(c.listCalls, accountID)
	block := c.blockList
	started := c.listStarted
	err := c.listErrs[accountID]
	list := c.lists[accountID]
	c.mu.Unlock()

	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return list, err
}

func (c *openAISessionCleanupClientStub) RevokeSession(ctx context.Context, _ int64, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.revoked = append(c.revoked, sessionID)
	return c.revokeErrs[sessionID]
}

func (c *openAISessionCleanupClientStub) snapshot() (listCalls []int64, revoked []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int64(nil), c.listCalls...), append([]string(nil), c.revoked...)
}

type openAISessionCleanupBatchClientStub struct {
	*openAISessionCleanupClientStub

	mu       sync.Mutex
	batches  [][]string
	batchErr error
	results  []*OpenAIAccountSessionBatchRevokeResult
}

var _ OpenAISessionCleanupBatchClient = (*openAISessionCleanupBatchClientStub)(nil)

func (c *openAISessionCleanupBatchClientStub) RevokeSessions(
	ctx context.Context,
	_ int64,
	sessionIDs []string,
) (*OpenAIAccountSessionBatchRevokeResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.batches = append(c.batches, append([]string(nil), sessionIDs...))
	if c.batchErr != nil {
		return nil, c.batchErr
	}
	if len(c.results) > 0 {
		result := c.results[0]
		c.results = c.results[1:]
		return result, nil
	}
	return &OpenAIAccountSessionBatchRevokeResult{
		RequestedCount:    len(sessionIDs),
		SuccessCount:      len(sessionIDs),
		RevokedSessionIDs: append([]string(nil), sessionIDs...),
	}, nil
}

func (c *openAISessionCleanupBatchClientStub) batchSnapshot() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([][]string, len(c.batches))
	for i := range c.batches {
		result[i] = append([]string(nil), c.batches[i]...)
	}
	return result
}

type openAISessionCleanupLeaderLockStub struct {
	mu          sync.Mutex
	acquired    bool
	acquireErr  error
	acquireCall int
	releaseCall int
}

var _ LeaderLockCache = (*openAISessionCleanupLeaderLockStub)(nil)

func (l *openAISessionCleanupLeaderLockStub) TryAcquireLeaderLock(context.Context, string, string, time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.acquireCall++
	if l.acquireErr != nil {
		return false, l.acquireErr
	}
	return l.acquired, nil
}

func (l *openAISessionCleanupLeaderLockStub) ReleaseLeaderLock(context.Context, string, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.releaseCall++
	return nil
}

func (l *openAISessionCleanupLeaderLockStub) calls() (int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.acquireCall, l.releaseCall
}

func newOpenAISessionCleanupTestAccount(id int64, enabled bool, intervalMinutes int) Account {
	extra := map[string]any{OpenAISessionCleanupEnabledExtraKey: enabled}
	if intervalMinutes > 0 {
		extra[OpenAISessionCleanupIntervalMinutesExtraKey] = intervalMinutes
	}
	return Account{
		ID:          id,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Schedulable: true,
		Extra:       extra,
	}
}

func TestOpenAISessionCleanupListsAccountsWithBoundedPagination(t *testing.T) {
	accounts := make([]Account, openAISessionCleanupPageSize+5)
	for i := range accounts {
		accounts[i] = newOpenAISessionCleanupTestAccount(int64(i+1), true, 60)
	}
	repo := &openAISessionCleanupPagedRepoStub{accounts: accounts}
	svc := NewOpenAISessionCleanupService(repo, &openAISessionCleanupClientStub{}, nil)

	got, err := svc.listCleanupAccounts(context.Background())

	require.NoError(t, err)
	require.Len(t, got, len(accounts))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []pagination.PaginationParams{
		{Page: 1, PageSize: openAISessionCleanupPageSize},
		{Page: 2, PageSize: openAISessionCleanupPageSize},
	}, repo.pageCalls)
	require.False(t, repo.fallbackCalled)
}

func TestOpenAISessionCleanupPaginationContinuesWhenPageCountIsOmitted(t *testing.T) {
	accounts := make([]Account, openAISessionCleanupPageSize+5)
	for i := range accounts {
		accounts[i] = newOpenAISessionCleanupTestAccount(int64(i+1), true, 60)
	}
	repo := &openAISessionCleanupPagedRepoStub{accounts: accounts, omitPages: true}
	svc := NewOpenAISessionCleanupService(repo, &openAISessionCleanupClientStub{}, nil)

	got, err := svc.listCleanupAccounts(context.Background())

	require.NoError(t, err)
	require.Len(t, got, len(accounts))
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []pagination.PaginationParams{
		{Page: 1, PageSize: openAISessionCleanupPageSize},
		{Page: 2, PageSize: openAISessionCleanupPageSize},
	}, repo.pageCalls)
	require.False(t, repo.fallbackCalled)
}

func knownCurrentOnlySessionList() *OpenAIAccountSessionList {
	return &OpenAIAccountSessionList{
		CurrentKnown: true,
		Sessions: []OpenAIAccountSession{
			{ID: "current", Current: true, CanRevoke: false},
		},
	}
}

func TestOpenAISessionCleanupRunOnceRevokesOnlyEligibleNonCurrentSessions(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(41, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
		account.ID: {
			CurrentKnown: true,
			Sessions: []OpenAIAccountSession{
				{ID: "current", Current: true, CanRevoke: true},
				{ID: " revoke-me ", CanRevoke: true},
				{ID: "revoke-me", CanRevoke: true}, // duplicate after trimming
				{ID: "not-allowed", CanRevoke: false},
				{ID: "", CanRevoke: true},
			},
		},
	}}
	svc := NewOpenAISessionCleanupService(repo, client, nil)
	svc.SetClock(func() time.Time { return time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC) })

	svc.RunOnce(context.Background())

	listCalls, revoked := client.snapshot()
	require.Equal(t, []int64{account.ID}, listCalls)
	require.Equal(t, []string{"revoke-me"}, revoked)
	state := repo.latestState(account.ID)
	require.NotNil(t, state)
	require.Equal(t, OpenAISessionCleanupStatusSuccess, state.Status)
	require.True(t, state.CurrentSessionKnown)
	require.Equal(t, 1, state.RevokedCount)
	require.Zero(t, state.FailedCount)
	require.NotEmpty(t, state.LastSuccessAt)
}

func TestOpenAISessionCleanupNeverRevokesIDAlsoMarkedCurrent(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(411, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
		account.ID: {
			CurrentKnown: true,
			Sessions: []OpenAIAccountSession{
				// Conflicting duplicate rows must resolve in favor of preserving
				// the positively identified current device.
				{ID: "same-device", CanRevoke: true},
				{ID: "same-device", Current: true, CanRevoke: true},
				{ID: "other-device", CanRevoke: true},
			},
		},
	}}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	_, revoked := client.snapshot()
	require.Equal(t, []string{"other-device"}, revoked)
}

func TestOpenAISessionCleanupRunOnceSkipsWhenAnotherInstanceOwnsLeaderLock(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(45, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
		account.ID: knownCurrentOnlySessionList(),
	}}
	lock := &openAISessionCleanupLeaderLockStub{}
	svc := NewOpenAISessionCleanupService(repo, client, lock)

	svc.RunOnce(context.Background())

	require.Equal(t, 1, func() int { acquire, _ := lock.calls(); return acquire }())
	_, release := lock.calls()
	require.Zero(t, release)
	require.Zero(t, repo.callCount())
	listCalls, revoked := client.snapshot()
	require.Empty(t, listCalls)
	require.Empty(t, revoked)
}

func TestOpenAISessionCleanupRunOnceHandlesAccountListError(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(46, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}, listErr: errors.New("database unavailable")}
	client := &openAISessionCleanupClientStub{}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	require.Equal(t, 1, repo.callCount())
	listCalls, revoked := client.snapshot()
	require.Empty(t, listCalls)
	require.Empty(t, revoked)
}

func TestOpenAISessionCleanupStateDoesNotPersistUpstreamErrorDetails(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(46_1, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{
		lists:    map[int64]*OpenAIAccountSessionList{account.ID: nil},
		listErrs: map[int64]error{account.ID: errors.New("access_token=SECRET session_id=SECRET")},
	}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	state := repo.latestState(account.ID)
	require.NotNil(t, state)
	require.NotContains(t, state.Message, "SECRET")
	require.Equal(t, "the OpenAI session cleanup request failed", state.Message)
}

func TestOpenAISessionCleanupStateRedactsCustomErrorReason(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(46_2, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{
		listErrs: map[int64]error{
			account.ID: infraerrors.New(http.StatusBadGateway, "CUSTOM access_token=SECRET", "upstream detail SECRET"),
		},
	}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	state := repo.latestState(account.ID)
	require.NotNil(t, state)
	require.Equal(t, "OPENAI_SESSION_CLEANUP_FAILED", state.ErrorCode)
	require.NotContains(t, state.ErrorCode, "SECRET")
}

func TestSanitizeOpenAISessionCleanupStateRedactsLegacyFields(t *testing.T) {
	state := SanitizeOpenAISessionCleanupState(map[string]any{
		"status":         "failed",
		"last_run_at":    "access_token=SECRET",
		"last_result_at": "2026-09-02T12:00:00+08:00",
		"revoked_count":  -4,
		"failed_count":   -2,
		"error_code":     "CUSTOM access_token=SECRET",
		"message":        "session_id=SECRET",
	})

	require.NotNil(t, state)
	require.Equal(t, OpenAISessionCleanupStatusFailed, state.Status)
	require.Equal(t, "OPENAI_SESSION_CLEANUP_FAILED", state.ErrorCode)
	require.Equal(t, "the OpenAI session cleanup request failed", state.Message)
	require.Empty(t, state.LastRunAt)
	require.Equal(t, "2026-09-02T04:00:00Z", state.LastResultAt)
	require.Zero(t, state.RevokedCount)
	require.Zero(t, state.FailedCount)
}

func TestOpenAISessionCleanupPersistenceSanitizesPreviousLastSuccessAt(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(46_3, true, 60)
	account.Extra[OpenAISessionCleanupStateExtraKey] = map[string]any{
		"status":          OpenAISessionCleanupStatusSuccess,
		"last_success_at": "session_id=SECRET",
	}
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{
		listErrs: map[int64]error{account.ID: errors.New("upstream unavailable")},
	}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	state := repo.latestState(account.ID)
	require.NotNil(t, state)
	// The failed run carries the previous LastSuccessAt forward.  Persistence
	// must validate it instead of writing the legacy value verbatim.
	require.Empty(t, state.LastSuccessAt)
}

func TestOpenAISessionCleanupRunOnceFailsClosedWithoutActualCurrentSession(t *testing.T) {
	for _, tt := range []struct {
		name         string
		currentKnown bool
		sessions     []OpenAIAccountSession
	}{
		{
			name: "marker absent",
			sessions: []OpenAIAccountSession{
				{ID: "other", CanRevoke: true},
			},
		},
		{
			name:         "only explicit false markers",
			currentKnown: true,
			sessions: []OpenAIAccountSession{
				{ID: "other-a", CanRevoke: true},
				{ID: "other-b", CanRevoke: true},
			},
		},
		{
			name:         "current marker without an identifier",
			currentKnown: true,
			sessions: []OpenAIAccountSession{
				{Current: true},
				{ID: "other-c", CanRevoke: true},
			},
		},
		{
			name:         "current marker with an oversized identifier",
			currentKnown: true,
			sessions: []OpenAIAccountSession{
				{ID: strings.Repeat("x", 513), Current: true},
				{ID: "other-d", CanRevoke: true},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := newOpenAISessionCleanupTestAccount(42, true, 60)
			repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
			client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
				account.ID: {CurrentKnown: tt.currentKnown, Sessions: tt.sessions},
			}}

			NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

			_, revoked := client.snapshot()
			require.Empty(t, revoked)
			state := repo.latestState(account.ID)
			require.NotNil(t, state)
			require.Equal(t, OpenAISessionCleanupStatusSkipped, state.Status)
			require.Equal(t, "OPENAI_CURRENT_SESSION_UNKNOWN", state.ErrorCode)
			require.False(t, state.CurrentSessionKnown)
			require.Zero(t, state.RevokedCount)
		})
	}
}

func TestOpenAISessionCleanupAcceptsTypedCurrentProjectionWithoutProvenance(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(42_1, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
		account.ID: {
			// A custom client may construct the typed projection directly and
			// leave the decoder-only CurrentKnown provenance field at false.
			Sessions: []OpenAIAccountSession{
				{ID: "current", Current: true},
				{ID: "other", CanRevoke: true},
			},
		},
	}}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	_, revoked := client.snapshot()
	require.Equal(t, []string{"other"}, revoked)
}

func TestOpenAISessionCleanupRunOnceDeduplicatesAndChunksBatchRevocation(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(43, true, 60)
	sessions := []OpenAIAccountSession{{ID: "current", Current: true}}
	wantIDs := make([]string, 0, maxOpenAIAccountSessionBatchSize+5)
	for i := 0; i < maxOpenAIAccountSessionBatchSize+5; i++ {
		id := fmt.Sprintf("session-%02d", i)
		wantIDs = append(wantIDs, id)
		sessions = append(sessions, OpenAIAccountSession{ID: id, CanRevoke: true})
	}
	sessions = append(sessions,
		OpenAIAccountSession{ID: " session-00 ", CanRevoke: true},
		OpenAIAccountSession{ID: "blocked", CanRevoke: false},
	)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupBatchClientStub{openAISessionCleanupClientStub: &openAISessionCleanupClientStub{
		lists: map[int64]*OpenAIAccountSessionList{
			account.ID: {CurrentKnown: true, Sessions: sessions},
		},
	}}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	batches := client.batchSnapshot()
	require.Len(t, batches, 2)
	require.Len(t, batches[0], maxOpenAIAccountSessionBatchSize)
	require.Len(t, batches[1], 5)
	require.Equal(t, wantIDs, append(append([]string(nil), batches[0]...), batches[1]...))
	_, fallbackRevokes := client.snapshot()
	require.Empty(t, fallbackRevokes)
	state := repo.latestState(account.ID)
	require.NotNil(t, state)
	require.Equal(t, len(wantIDs), state.RevokedCount)
	require.Equal(t, OpenAISessionCleanupStatusSuccess, state.Status)
}

func TestOpenAISessionCleanupRunOnceHonorsPerAccountInterval(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(44, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
		account.ID: knownCurrentOnlySessionList(),
	}}
	svc := NewOpenAISessionCleanupService(repo, client, nil)
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return now })

	svc.RunOnce(context.Background())
	now = now.Add(59 * time.Minute)
	svc.RunOnce(context.Background())
	listCalls, _ := client.snapshot()
	require.Equal(t, []int64{account.ID}, listCalls)

	now = now.Add(time.Minute)
	svc.RunOnce(context.Background())
	listCalls, _ = client.snapshot()
	require.Equal(t, []int64{account.ID, account.ID}, listCalls)
}

func TestOpenAISessionCleanupRunOnceFiltersIneligibleAccounts(t *testing.T) {
	eligible := newOpenAISessionCleanupTestAccount(51, true, 60)
	unschedulable := newOpenAISessionCleanupTestAccount(52, true, 60)
	unschedulable.Schedulable = false // session hygiene is independent of traffic scheduling
	disabled := newOpenAISessionCleanupTestAccount(53, false, 60)
	inactive := newOpenAISessionCleanupTestAccount(54, true, 60)
	inactive.Status = StatusDisabled
	apiKey := newOpenAISessionCleanupTestAccount(55, true, 60)
	apiKey.Type = AccountTypeAPIKey
	shadow := newOpenAISessionCleanupTestAccount(56, true, 60)
	parentID := int64(51)
	shadow.ParentAccountID = &parentID
	otherPlatform := newOpenAISessionCleanupTestAccount(57, true, 60)
	otherPlatform.Platform = PlatformAnthropic

	repo := &openAISessionCleanupRepoStub{accounts: []Account{
		eligible, unschedulable, disabled, inactive, apiKey, shadow, otherPlatform,
	}}
	client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
		eligible.ID:      knownCurrentOnlySessionList(),
		unschedulable.ID: knownCurrentOnlySessionList(),
	}}

	NewOpenAISessionCleanupService(repo, client, nil).RunOnce(context.Background())

	listCalls, _ := client.snapshot()
	require.Equal(t, []int64{eligible.ID, unschedulable.ID}, listCalls)
}

func TestOpenAISessionCleanupRunAccountValidatesEligibility(t *testing.T) {
	parentID := int64(61)
	tests := []struct {
		name    string
		account Account
	}{
		{
			name: "inactive",
			account: func() Account {
				a := newOpenAISessionCleanupTestAccount(61, true, 60)
				a.Status = StatusDisabled
				return a
			}(),
		},
		{
			name: "wrong platform",
			account: func() Account {
				a := newOpenAISessionCleanupTestAccount(62, true, 60)
				a.Platform = PlatformAnthropic
				return a
			}(),
		},
		{
			name: "wrong type",
			account: func() Account {
				a := newOpenAISessionCleanupTestAccount(63, true, 60)
				a.Type = AccountTypeAPIKey
				return a
			}(),
		},
		{
			name: "shadow",
			account: func() Account {
				a := newOpenAISessionCleanupTestAccount(64, true, 60)
				a.ParentAccountID = &parentID
				return a
			}(),
		},
		{
			name:    "disabled policy",
			account: newOpenAISessionCleanupTestAccount(65, false, 60),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &openAISessionCleanupRepoStub{accounts: []Account{tt.account}}
			client := &openAISessionCleanupClientStub{}
			err := NewOpenAISessionCleanupService(repo, client, nil).RunAccount(context.Background(), tt.account.ID)
			require.Error(t, err)
			listCalls, revoked := client.snapshot()
			require.Empty(t, listCalls)
			require.Empty(t, revoked)
		})
	}
}

func TestOpenAISessionCleanupRunAccountContinuesAfterIndividualRevokeError(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(71, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{
		lists: map[int64]*OpenAIAccountSessionList{
			account.ID: {
				CurrentKnown: true,
				Sessions: []OpenAIAccountSession{
					{ID: "current", Current: true},
					{ID: "fails", CanRevoke: true},
					{ID: "succeeds", CanRevoke: true},
				},
			},
		},
		revokeErrs: map[string]error{"fails": errors.New("upstream failed")},
	}

	err := NewOpenAISessionCleanupService(repo, client, nil).RunAccount(context.Background(), account.ID)

	require.ErrorContains(t, err, "upstream failed")
	_, revoked := client.snapshot()
	require.Equal(t, []string{"fails", "succeeds"}, revoked)
	state := repo.latestState(account.ID)
	require.NotNil(t, state)
	require.Equal(t, OpenAISessionCleanupStatusFailed, state.Status)
	require.Equal(t, 1, state.RevokedCount)
	require.Equal(t, 1, state.FailedCount)
}

func TestOpenAISessionCleanupRunAccountCancellationStopsBeforeRevocation(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(72, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	started := make(chan struct{})
	client := &openAISessionCleanupClientStub{
		listStarted: started,
		blockList:   true,
	}
	svc := NewOpenAISessionCleanupService(repo, client, nil)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- svc.RunAccount(ctx, account.ID)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("session listing did not start")
	}
	cancel()

	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cleanup did not stop after context cancellation")
	}
	_, revoked := client.snapshot()
	require.Empty(t, revoked)
}

func TestOpenAISessionCleanupStopCancelsAndWaitsForManualRun(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(72_1, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	started := make(chan struct{})
	client := &openAISessionCleanupClientStub{
		listStarted: started,
		blockList:   true,
	}
	svc := NewOpenAISessionCleanupService(repo, client, nil)
	runResult := make(chan error, 1)
	go func() {
		runResult <- svc.RunAccount(context.Background(), account.ID)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("manual cleanup did not start")
	}

	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the in-flight manual cleanup")
	}
	select {
	case err := <-runResult:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("manual cleanup did not return after Stop")
	}
}

func TestOpenAISessionCleanupStartStopCancelsStartupDelayAndIsIdempotent(t *testing.T) {
	account := newOpenAISessionCleanupTestAccount(73, true, 60)
	repo := &openAISessionCleanupRepoStub{accounts: []Account{account}}
	client := &openAISessionCleanupClientStub{lists: map[int64]*OpenAIAccountSessionList{
		account.ID: knownCurrentOnlySessionList(),
	}}
	svc := NewOpenAISessionCleanupService(repo, client, nil)
	svc.Start()
	svc.Start()

	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the worker startup delay")
	}
	listCalls, revoked := client.snapshot()
	require.Empty(t, listCalls)
	require.Empty(t, revoked)
}
