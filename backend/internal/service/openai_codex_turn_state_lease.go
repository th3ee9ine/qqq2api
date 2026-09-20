package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	codexTurnStateCollectionLeaseTTL              = 60 * time.Second
	codexTurnStateCollectionLeaseRefreshInterval  = 20 * time.Second
	codexTurnStateCollectionLeaseOperationTimeout = 2 * time.Second
)

var (
	errCodexTurnStateCollectionLeaseLost     = errors.New("codex turn state collection lease lost")
	errCodexTurnStateCollectionLeaseReleased = errors.New("codex turn state collection lease released")
)

// CodexTurnStateCollectionLeaseStore is the optional distributed account gate
// implemented by the Redis-backed GatewayCache. A configured store is
// authoritative: callers fail closed on every acquire or refresh error instead
// of falling back to a process-local lock and risking duplicate upstream work.
type CodexTurnStateCollectionLeaseStore interface {
	TryAcquireCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string, ttl time.Duration) (bool, error)
	RefreshCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string, ttl time.Duration) (bool, error)
	ReleaseCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string) (bool, error)
}

type codexTurnStateCollectionLease struct {
	service *OpenAIGatewayService
	store   CodexTurnStateCollectionLeaseStore

	accountID int64
	owner     string
	ttl       time.Duration
	refresh   time.Duration
	timeout   time.Duration
	tracked   bool

	ctx    context.Context
	cancel context.CancelCauseFunc
	done   chan struct{}

	mu      sync.Mutex
	refs    int
	closing bool
}

func newCodexTurnStateCollectionLease(
	service *OpenAIGatewayService,
	store CodexTurnStateCollectionLeaseStore,
	accountID int64,
	owner string,
	ttl, refresh, timeout time.Duration,
	tracked bool,
) *codexTurnStateCollectionLease {
	ctx, cancel := context.WithCancelCause(context.Background())
	lease := &codexTurnStateCollectionLease{
		service:   service,
		store:     store,
		accountID: accountID,
		owner:     owner,
		ttl:       ttl,
		refresh:   refresh,
		timeout:   timeout,
		tracked:   tracked,
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		refs:      1,
	}
	go lease.refreshLoop()
	return lease
}

func (l *codexTurnStateCollectionLease) Context() context.Context {
	if l == nil || l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}

func (l *codexTurnStateCollectionLease) retain() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing || l.ctx == nil || l.ctx.Err() != nil {
		return false
	}
	l.refs++
	return true
}

// releaseRef never performs Redis I/O inline. Task completion commonly happens
// while openaiTurnStateMu is held, so the final compare-and-delete runs in a
// tracked goroutine and CloseOpenAIWSPool waits for it during graceful shutdown.
func (l *codexTurnStateCollectionLease) releaseRef() {
	if l == nil {
		return
	}
	l.mu.Lock()
	if l.refs > 0 {
		l.refs--
	}
	if l.refs != 0 || l.closing {
		l.mu.Unlock()
		return
	}
	l.closing = true
	l.mu.Unlock()

	l.cancel(errCodexTurnStateCollectionLeaseReleased)
	release := func() {
		<-l.done
		if l.store == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
		defer cancel()
		if _, err := l.store.ReleaseCodexTurnStateCollectionLease(ctx, l.accountID, l.owner); err != nil {
			slog.Warn("openai_codex_turn_state_collection_lease_release_failed", "account_id", l.accountID, "error", err)
		}
	}
	if l.service == nil || !l.tracked {
		go release()
		return
	}
	go func() {
		defer l.service.openaiTurnStateLeaseWG.Done()
		release()
	}()
}

func (l *codexTurnStateCollectionLease) refreshLoop() {
	defer close(l.done)
	if l == nil || l.store == nil || l.refresh <= 0 {
		return
	}
	ticker := time.NewTicker(l.refresh)
	defer ticker.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), l.timeout)
			refreshed, err := l.store.RefreshCodexTurnStateCollectionLease(ctx, l.accountID, l.owner, l.ttl)
			cancel()
			if err == nil && refreshed {
				continue
			}
			if err != nil {
				slog.Warn("openai_codex_turn_state_collection_lease_refresh_failed", "account_id", l.accountID, "error", err)
			} else {
				slog.Warn("openai_codex_turn_state_collection_lease_lost", "account_id", l.accountID)
			}
			l.cancel(errCodexTurnStateCollectionLeaseLost)
			return
		}
	}
}

// acquireCodexTurnStateCollectionLease acquires the production Redis account
// gate without holding openaiTurnStateMu. Tests and explicitly reduced
// deployments whose GatewayCache does not expose the optional capability retain
// the existing process-local behavior. A present-but-failing store never does.
func (s *OpenAIGatewayService) acquireCodexTurnStateCollectionLease(ctx context.Context, accountID int64) (*codexTurnStateCollectionLease, bool, error) {
	if s == nil || accountID <= 0 {
		return nil, false, errors.New("invalid Codex Turn State collection lease account")
	}
	store, configured := s.cache.(CodexTurnStateCollectionLeaseStore)
	if !configured {
		return nil, true, nil
	}
	s.openaiTurnStateMu.Lock()
	if s.openaiTurnStateStopping {
		s.openaiTurnStateMu.Unlock()
		return nil, false, errors.New("Codex Turn State collection service is stopping")
	}
	// Register the acquisition before shutdown can publish stopping=true. The
	// final owner-token delete owns this WaitGroup slot, so graceful shutdown
	// cannot return while an acquired lease remains in Redis.
	s.openaiTurnStateLeaseWG.Add(1)
	s.openaiTurnStateMu.Unlock()
	tracked := true
	defer func() {
		if tracked {
			s.openaiTurnStateLeaseWG.Done()
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	owner := uuid.NewString()
	acquireCtx, cancel := context.WithTimeout(ctx, codexTurnStateCollectionLeaseOperationTimeout)
	acquired, err := store.TryAcquireCodexTurnStateCollectionLease(acquireCtx, accountID, owner, codexTurnStateCollectionLeaseTTL)
	cancel()
	if err != nil || !acquired {
		return nil, acquired, err
	}
	lease := newCodexTurnStateCollectionLease(
		s,
		store,
		accountID,
		owner,
		codexTurnStateCollectionLeaseTTL,
		codexTurnStateCollectionLeaseRefreshInterval,
		codexTurnStateCollectionLeaseOperationTimeout,
		true,
	)
	tracked = false
	return lease, true, nil
}

// ensureCodexTurnStateCollectionTaskLease is called only after the local
// account gate has been acquired and before a worker marks its task running or
// performs repository/upstream work. Explicit manual batches arrive with a
// shared lease already attached; automatic and renewal tasks acquire here.
func (s *OpenAIGatewayService) ensureCodexTurnStateCollectionTaskLease(ctx context.Context, accountID int64, taskID string) (*codexTurnStateCollectionLease, bool, error) {
	lease, exists := s.retainCodexTurnStateCollectionTaskLease(taskID)
	if !exists {
		return nil, false, context.Canceled
	}
	if lease != nil {
		return lease, true, nil
	}

	lease, acquired, err := s.acquireCodexTurnStateCollectionLease(ctx, accountID)
	if err != nil || !acquired {
		return nil, acquired, err
	}
	if lease == nil {
		// The optional capability is intentionally absent, as in focused unit
		// tests and explicitly reduced single-process deployments.
		return nil, true, nil
	}
	attached := s.attachCodexTurnStateCollectionTaskLease(taskID, lease)
	if !attached {
		lease.releaseRef()
		return nil, false, context.Canceled
	}
	executionLease, retained := s.retainCodexTurnStateCollectionTaskLease(taskID)
	// Drop the acquisition reference. The registry task and this worker execution
	// now own independent references. If cancellation won before the retain, the
	// acquisition reference still keeps ownership until this point.
	lease.releaseRef()
	if !retained || executionLease == nil {
		return nil, false, context.Canceled
	}
	return executionLease, true, nil
}
