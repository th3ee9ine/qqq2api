package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
)

// openAIRecheckSingleflightRepo makes the fresh account read observable while
// keeping all other AccountRepository methods delegated through the embedded
// interface. The read is held open so concurrent rechecks must share a flight.
type openAIRecheckSingleflightRepo struct {
	AccountRepository
	account     *Account
	calls       atomic.Int32
	entered     chan struct{}
	enteredOnce sync.Once
	release     chan struct{}
}

func (r *openAIRecheckSingleflightRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.calls.Add(1)
	r.enteredOnce.Do(func() { close(r.entered) })
	select {
	case <-r.release:
		return r.account, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRecheckSelectedOpenAIAccountFromDBBeforeProfitCollapsesConcurrentReads(t *testing.T) {
	account := &Account{
		ID:          901,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
		Status:      StatusActive,
		Schedulable: true,
	}
	repo := &openAIRecheckSingleflightRepo{
		account: account,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := &OpenAIGatewayService{
		accountRepo:       repo,
		schedulerSnapshot: NewSchedulerSnapshotService(nil, nil, nil, nil, nil),
	}

	const concurrentRequests = 32
	start := make(chan struct{})
	results := make(chan *Account, concurrentRequests)
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)
	for i := 0; i < concurrentRequests; i++ {
		go func() {
			defer wg.Done()
			<-start
			results <- svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(
				context.Background(), account, nil, PlatformOpenAI, "gpt-4o", false,
				OpenAIEndpointCapabilityChatCompletions,
			)
		}()
	}
	close(start)

	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the shared account read")
	}
	// Let all callers reach the in-flight singleflight before releasing the
	// repository read. This keeps the assertion deterministic without adding a
	// production synchronization point.
	time.Sleep(20 * time.Millisecond)
	close(repo.release)
	wg.Wait()
	close(results)

	for result := range results {
		require.NotNil(t, result)
		require.Equal(t, account.ID, result.ID)
	}
	require.Equal(t, int32(1), repo.calls.Load(), "concurrent scheduler rechecks should share one DB read")
}

func TestRecheckSelectedOpenAIAccountFromDBKeepsAdminScopesSeparate(t *testing.T) {
	account := &Account{
		ID:          902,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
		Status:      StatusActive,
		Schedulable: true,
	}
	repo := &openAIRecheckSingleflightRepo{
		account: account,
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	svc := &OpenAIGatewayService{
		accountRepo:       repo,
		schedulerSnapshot: NewSchedulerSnapshotService(nil, nil, nil, nil, nil),
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for _, ownerID := range []int64{1001, 1002} {
		go func(ownerID int64) {
			defer wg.Done()
			<-start
			ctx := context.WithValue(context.Background(), ctxkey.AccountAdminID, ownerID)
			_ = svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(
				ctx, account, nil, PlatformOpenAI, "gpt-4o", false,
				OpenAIEndpointCapabilityChatCompletions,
			)
		}(ownerID)
	}
	close(start)

	select {
	case <-repo.entered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the scoped account read")
	}
	require.Eventually(t, func() bool {
		return repo.calls.Load() == 2
	}, time.Second, time.Millisecond, "different account-admin scopes must not share one repository result")
	close(repo.release)
	wg.Wait()
}
