package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexTurnStateCollectionLeaseStoreStub struct {
	mu sync.Mutex

	owner        string
	acquireErr   error
	refreshErr   error
	releaseErr   error
	refreshes    int
	releases     int
	acquisitions int
}

func (s *codexTurnStateCollectionLeaseStoreStub) TryAcquireCodexTurnStateCollectionLease(_ context.Context, _ int64, owner string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquisitions++
	if s.acquireErr != nil {
		return false, s.acquireErr
	}
	if s.owner != "" {
		return false, nil
	}
	s.owner = owner
	return true, nil
}

func (s *codexTurnStateCollectionLeaseStoreStub) RefreshCodexTurnStateCollectionLease(_ context.Context, _ int64, owner string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshes++
	if s.refreshErr != nil {
		return false, s.refreshErr
	}
	return s.owner == owner, nil
}

func (s *codexTurnStateCollectionLeaseStoreStub) ReleaseCodexTurnStateCollectionLease(_ context.Context, _ int64, owner string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	if s.releaseErr != nil {
		return false, s.releaseErr
	}
	if s.owner != owner {
		return false, nil
	}
	s.owner = ""
	return true, nil
}

func (s *codexTurnStateCollectionLeaseStoreStub) counts() (acquisitions, refreshes, releases int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acquisitions, s.refreshes, s.releases
}

type codexTurnStateCollectionLeaseGatewayCacheStub struct {
	stubGatewayCache
	leaseStore *codexTurnStateCollectionLeaseStoreStub
}

func (s *codexTurnStateCollectionLeaseGatewayCacheStub) TryAcquireCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string, ttl time.Duration) (bool, error) {
	return s.leaseStore.TryAcquireCodexTurnStateCollectionLease(ctx, accountID, owner, ttl)
}

func (s *codexTurnStateCollectionLeaseGatewayCacheStub) RefreshCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string, ttl time.Duration) (bool, error) {
	return s.leaseStore.RefreshCodexTurnStateCollectionLease(ctx, accountID, owner, ttl)
}

func (s *codexTurnStateCollectionLeaseGatewayCacheStub) ReleaseCodexTurnStateCollectionLease(ctx context.Context, accountID int64, owner string) (bool, error) {
	return s.leaseStore.ReleaseCodexTurnStateCollectionLease(ctx, accountID, owner)
}

func newTestCodexTurnStateCollectionLease(store *codexTurnStateCollectionLeaseStoreStub, refresh time.Duration) *codexTurnStateCollectionLease {
	store.mu.Lock()
	store.owner = "test-owner"
	store.mu.Unlock()
	return newCodexTurnStateCollectionLease(nil, store, 42, "test-owner", 100*time.Millisecond, refresh, 50*time.Millisecond, false)
}

func TestCodexTurnStateCollectionLeaseExecutionContext(t *testing.T) {
	t.Run("lease cancellation is synchronous", func(t *testing.T) {
		baseCtx, cancelBase := context.WithCancel(context.Background())
		defer cancelBase()
		leaseCtx, cancelLease := context.WithCancelCause(context.Background())
		mergedCtx, release := codexTurnStateCollectionLeaseExecutionContext(baseCtx, leaseCtx)
		defer release()

		cancelLease(errCodexTurnStateCollectionLeaseLost)

		require.ErrorIs(t, mergedCtx.Err(), context.Canceled)
		require.ErrorIs(t, context.Cause(mergedCtx), errCodexTurnStateCollectionLeaseLost)
	})

	t.Run("base task cancellation propagates", func(t *testing.T) {
		baseCause := errors.New("task canceled")
		baseCtx, cancelBase := context.WithCancelCause(context.Background())
		leaseCtx, cancelLease := context.WithCancel(context.Background())
		defer cancelLease()
		mergedCtx, release := codexTurnStateCollectionLeaseExecutionContext(baseCtx, leaseCtx)
		defer release()

		cancelBase(baseCause)

		require.Eventually(t, func() bool {
			return mergedCtx.Err() != nil
		}, time.Second, time.Millisecond)
		require.ErrorIs(t, context.Cause(mergedCtx), baseCause)
	})
}

func TestCodexTurnStateCollectionLeaseSharedUntilLastTaskFinishes(t *testing.T) {
	store := &codexTurnStateCollectionLeaseStoreStub{}
	lease := newTestCodexTurnStateCollectionLease(store, 10*time.Millisecond)
	s := &OpenAIGatewayService{}
	create := func(owner string) *CodexTurnStateCollectionTask {
		task, _, err := s.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
			AccountID:       42,
			AccountName:     "account",
			RequestModel:    owner,
			OwnerModel:      owner,
			Source:          CodexTurnStateCollectionSourceManual,
			collectionLease: lease,
		})
		require.NoError(t, err)
		return task
	}
	first := create("gpt-5.5")
	second := create("gpt-5.4")
	lease.releaseRef() // release the batch setup reference after every owner attached

	require.Eventually(t, func() bool {
		_, refreshes, _ := store.counts()
		return refreshes >= 2
	}, time.Second, 5*time.Millisecond, "a long-running batch must renew its account lease")

	_, ok := s.CompleteCodexTurnStateCollectionTask(first.ID)
	require.True(t, ok)
	_, _, releases := store.counts()
	require.Zero(t, releases, "the first owner must not release a lease shared by the second")
	require.NoError(t, lease.Context().Err())

	_, err := s.CancelCodexTurnStateCollectionTask(second.ID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		_, _, releases := store.counts()
		return releases == 1
	}, time.Second, 5*time.Millisecond)
}

func TestCodexTurnStateCollectionLeaseRefreshFailureCancelsTask(t *testing.T) {
	store := &codexTurnStateCollectionLeaseStoreStub{refreshErr: errors.New("redis unavailable")}
	lease := newTestCodexTurnStateCollectionLease(store, 10*time.Millisecond)
	s := &OpenAIGatewayService{}
	taskContexts := make([]context.Context, 0, 2)
	taskIDs := make([]string, 0, 2)
	for _, owner := range []string{"gpt-5.5", "gpt-5.4"} {
		task, taskCtx, err := s.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
			AccountID:       42,
			AccountName:     "account",
			RequestModel:    owner,
			OwnerModel:      owner,
			Source:          CodexTurnStateCollectionSourceAutomatic,
			collectionLease: lease,
		})
		require.NoError(t, err)
		taskContexts = append(taskContexts, taskCtx)
		taskIDs = append(taskIDs, task.ID)
	}
	lease.releaseRef()

	require.Eventually(t, func() bool {
		return taskContexts[0].Err() != nil && taskContexts[1].Err() != nil
	}, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		_, firstExists := s.GetCodexTurnStateCollectionTask(taskIDs[0])
		_, secondExists := s.GetCodexTurnStateCollectionTask(taskIDs[1])
		return !firstExists && !secondExists
	}, time.Second, 5*time.Millisecond)
	require.ErrorIs(t, context.Cause(lease.Context()), errCodexTurnStateCollectionLeaseLost)
}

func TestCodexTurnStateCollectionLeaseLossCannotPublishBeforeCancelCallback(t *testing.T) {
	store := &codexTurnStateCollectionLeaseStoreStub{}
	lease := newTestCodexTurnStateCollectionLease(store, time.Hour)
	s := &OpenAIGatewayService{}
	task, taskCtx, err := s.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
		AccountID:       42,
		AccountName:     "account",
		RequestModel:    "gpt-5.5",
		OwnerModel:      "gpt-5.5",
		Source:          CodexTurnStateCollectionSourceAutomatic,
		collectionLease: lease,
	})
	require.NoError(t, err)
	lease.releaseRef()
	entry := &codexTurnStateAutoEntry{collectionTaskID: task.ID, collectionTaskContext: taskCtx}
	lease.cancel(errCodexTurnStateCollectionLeaseLost)

	published := false
	s.openaiTurnStateMu.Lock()
	allowed := s.withActiveCodexTurnStateCollectionTaskLocked(entry, func() bool {
		published = true
		return true
	})
	s.openaiTurnStateMu.Unlock()
	require.False(t, allowed)
	require.False(t, published)

	_, _ = s.CancelCodexTurnStateCollectionTask(task.ID)
}

func TestCodexTurnStateCollectionLeaseAcquireErrorFailsClosed(t *testing.T) {
	store := &codexTurnStateCollectionLeaseStoreStub{acquireErr: errors.New("redis unavailable")}
	s := &OpenAIGatewayService{cache: &codexTurnStateCollectionLeaseGatewayCacheStub{leaseStore: store}}

	lease, acquired, err := s.acquireCodexTurnStateCollectionLease(context.Background(), 42)
	require.Error(t, err)
	require.False(t, acquired)
	require.Nil(t, lease)
	done := make(chan struct{})
	go func() {
		s.openaiTurnStateLeaseWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("failed acquisition leaked the service lease lifecycle slot")
	}
}

func TestCodexTurnStateCollectionLeaseServiceInstancesCompete(t *testing.T) {
	store := &codexTurnStateCollectionLeaseStoreStub{}
	first := &OpenAIGatewayService{cache: &codexTurnStateCollectionLeaseGatewayCacheStub{leaseStore: store}}
	second := &OpenAIGatewayService{cache: &codexTurnStateCollectionLeaseGatewayCacheStub{leaseStore: store}}

	firstLease, acquired, err := first.acquireCodexTurnStateCollectionLease(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotNil(t, firstLease)

	secondLease, acquired, err := second.acquireCodexTurnStateCollectionLease(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, acquired)
	require.Nil(t, secondLease)

	firstLease.releaseRef()
	first.openaiTurnStateLeaseWG.Wait()
	secondLease, acquired, err = second.acquireCodexTurnStateCollectionLease(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, acquired)
	secondLease.releaseRef()
	second.openaiTurnStateLeaseWG.Wait()
}

func TestStopCodexTurnStateCollectionTasksReleasesSharedLease(t *testing.T) {
	store := &codexTurnStateCollectionLeaseStoreStub{}
	s := &OpenAIGatewayService{cache: &codexTurnStateCollectionLeaseGatewayCacheStub{leaseStore: store}}
	lease, acquired, err := s.acquireCodexTurnStateCollectionLease(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, acquired)
	contexts := make([]context.Context, 0, 2)
	for _, owner := range []string{"gpt-5.5", "gpt-5.4"} {
		_, taskCtx, createErr := s.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
			AccountID:       42,
			AccountName:     "account",
			RequestModel:    owner,
			OwnerModel:      owner,
			Source:          CodexTurnStateCollectionSourceManual,
			collectionLease: lease,
		})
		require.NoError(t, createErr)
		contexts = append(contexts, taskCtx)
	}
	lease.releaseRef()

	s.StopCodexTurnStateCollectionTasks()
	s.openaiTurnStateLeaseWG.Wait()
	require.Error(t, contexts[0].Err())
	require.Error(t, contexts[1].Err())
	_, _, releases := store.counts()
	require.Equal(t, 1, releases)
	store.mu.Lock()
	require.Empty(t, store.owner)
	store.mu.Unlock()
}

func TestCodexTurnStateCollectionExecutionReferenceBlocksTakeoverUntilWorkerReturns(t *testing.T) {
	for _, terminal := range []string{"cancel", "stop"} {
		t.Run(terminal, func(t *testing.T) {
			store := &codexTurnStateCollectionLeaseStoreStub{}
			first := &OpenAIGatewayService{cache: &codexTurnStateCollectionLeaseGatewayCacheStub{leaseStore: store}}
			second := &OpenAIGatewayService{cache: &codexTurnStateCollectionLeaseGatewayCacheStub{leaseStore: store}}
			lease, acquired, err := first.acquireCodexTurnStateCollectionLease(context.Background(), 42)
			require.NoError(t, err)
			require.True(t, acquired)
			task, _, err := first.CreateCodexTurnStateCollectionTask(context.Background(), CodexTurnStateCollectionTaskInput{
				AccountID:       42,
				AccountName:     "account",
				RequestModel:    "gpt-5.5",
				OwnerModel:      "gpt-5.5",
				Source:          CodexTurnStateCollectionSourceManual,
				collectionLease: lease,
			})
			require.NoError(t, err)
			lease.releaseRef()

			executionLease, retained := first.retainCodexTurnStateCollectionTaskLease(task.ID)
			require.True(t, retained)
			require.Same(t, lease, executionLease)
			if terminal == "cancel" {
				_, err = first.CancelCodexTurnStateCollectionTask(task.ID)
				require.NoError(t, err)
			} else {
				first.StopCodexTurnStateCollectionTasks()
			}

			competing, acquired, err := second.acquireCodexTurnStateCollectionLease(context.Background(), 42)
			require.NoError(t, err)
			require.False(t, acquired, "task terminal state must not release a still-running worker's lease")
			require.Nil(t, competing)

			executionLease.releaseRef()
			first.openaiTurnStateLeaseWG.Wait()
			competing, acquired, err = second.acquireCodexTurnStateCollectionLease(context.Background(), 42)
			require.NoError(t, err)
			require.True(t, acquired, "takeover is allowed only after the old worker returns")
			competing.releaseRef()
			second.openaiTurnStateLeaseWG.Wait()
		})
	}
}

func TestCodexTurnStateCollectionLeaseContentionPreservesDetachedDirtyPersistence(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	store := &codexTurnStateCollectionLeaseStoreStub{owner: "other-instance"}
	s.cache = &codexTurnStateCollectionLeaseGatewayCacheStub{leaseStore: store}

	now := time.Now()
	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, "gpt-5")
	entry.probeAt = now.Add(-codexTurnStateAutoProbeInterval - time.Minute).UnixMilli()
	entry.probeCompletedAt = entry.probeAt
	entry.lastError = "request_failed"
	entry.dirty = true
	entry.manualOutcomePending = true
	s.openaiTurnStateMu.Unlock()

	require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, "gpt-5"))
	waitTurnStateAutoIdle(t, s)

	acquisitions, _, releases := store.counts()
	require.Equal(t, 1, acquisitions)
	require.Zero(t, releases, "the contender must not release another instance's lease")
	require.Empty(t, s.ListCodexTurnStateCollectionTasks())

	repo.mu.Lock()
	writes := repo.writes
	repo.mu.Unlock()
	require.Equal(t, 1, writes, "lease contention must retain and persist pre-existing dirty state")

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	storedSlot := codexTurnStateModelAccount(stored, "gpt-5")
	require.Equal(t, "request_failed", storedSlot.GetExtraString(CodexTurnStateAutoLastErrorExtraKey))

	s.openaiTurnStateMu.Lock()
	require.False(t, entry.dirty)
	require.False(t, entry.manualOutcomePending)
	require.Empty(t, entry.collectionTaskID)
	s.openaiTurnStateMu.Unlock()
}
