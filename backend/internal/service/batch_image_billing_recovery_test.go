//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recordingBatchImageQueue struct {
	*fakeBatchImageQueue
	enqueued []string
}

func (q *recordingBatchImageQueue) Enqueue(_ context.Context, batchID string) error {
	q.enqueued = append(q.enqueued, batchID)
	return nil
}

func TestBatchImageBillingRecoveryService_ReleasesStaleUnsubmittedHold(t *testing.T) {
	repo := newFakeBatchImageRepository()
	apiKeyID := int64(22)
	holdAmount := 0.5
	stale := &BatchImageJob{
		BatchID:       "imgbatch_stale_created",
		UserID:        11,
		APIKeyID:      &apiKeyID,
		Status:        BatchImageJobStatusCreated,
		EstimatedCost: holdAmount,
		HoldAmount:    &holdAmount,
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now().Add(-time.Hour),
	}
	activeProviderName := "providers/job"
	active := &BatchImageJob{
		BatchID:         "imgbatch_has_provider",
		UserID:          11,
		APIKeyID:        &apiKeyID,
		Status:          BatchImageJobStatusSubmitted,
		ProviderJobName: &activeProviderName,
		EstimatedCost:   holdAmount,
		HoldAmount:      &holdAmount,
		CreatedAt:       time.Now().Add(-time.Hour),
		UpdatedAt:       time.Now().Add(-time.Hour),
	}
	repo.jobs[stale.BatchID] = stale
	repo.jobs[active.BatchID] = active
	billing := &fakeBatchImageBillingRepo{}
	svc := &BatchImageBillingRecoveryService{Repo: repo, Billing: billing, StaleAfter: time.Minute, Limit: 10}

	released, err := svc.ReleaseStaleUnsubmittedOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, released)
	require.Equal(t, BatchImageJobStatusFailed, repo.jobs[stale.BatchID].Status)
	require.Equal(t, "SUBMIT_STALE_BEFORE_PROVIDER", batchImageDerefString(repo.jobs[stale.BatchID].LastErrorCode))
	require.Empty(t, billing.releases, "global API keys have no user balance hold to release")
	require.Equal(t, BatchImageJobStatusSubmitted, repo.jobs[active.BatchID].Status)
}

func TestBatchImageBillingRecoveryService_SkipsJobRefreshedByHeartbeat(t *testing.T) {
	repo := newFakeBatchImageRepository()
	apiKeyID := int64(22)
	holdAmount := 0.5
	// updated_at 在 cutoff 之后（慢提交心跳持续续期）：不得误杀退款。
	fresh := &BatchImageJob{
		BatchID:       "imgbatch_fresh_uploading",
		UserID:        11,
		APIKeyID:      &apiKeyID,
		Status:        BatchImageJobStatusUploading,
		EstimatedCost: holdAmount,
		HoldAmount:    &holdAmount,
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now(),
	}
	repo.jobs[fresh.BatchID] = fresh
	billing := &fakeBatchImageBillingRepo{}
	svc := &BatchImageBillingRecoveryService{Repo: repo, Billing: billing, StaleAfter: time.Minute, Limit: 10}

	released, err := svc.ReleaseStaleUnsubmittedOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, released)
	require.Equal(t, BatchImageJobStatusUploading, repo.jobs[fresh.BatchID].Status)
	require.Empty(t, billing.releases)
}

func TestBatchImageBillingRecoveryService_IgnoresLegacyWalletReleaseFailureForGlobalKey(t *testing.T) {
	repo := newFakeBatchImageRepository()
	apiKeyID := int64(22)
	holdAmount := 0.5
	stale := &BatchImageJob{
		BatchID:       "imgbatch_stale_release_fail",
		UserID:        11,
		APIKeyID:      &apiKeyID,
		Status:        BatchImageJobStatusCreated,
		EstimatedCost: holdAmount,
		HoldAmount:    &holdAmount,
		CreatedAt:     time.Now().Add(-time.Hour),
		UpdatedAt:     time.Now().Add(-time.Hour),
	}
	repo.jobs[stale.BatchID] = stale
	billing := &fakeBatchImageBillingRepo{releaseErr: ErrBatchImageBillingHoldFailed}
	queue := &recordingBatchImageQueue{fakeBatchImageQueue: newFakeBatchImageQueue("")}
	svc := &BatchImageBillingRecoveryService{Repo: repo, Billing: billing, Queue: queue, StaleAfter: time.Minute, Limit: 10}

	released, err := svc.ReleaseStaleUnsubmittedOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, released)
	require.Equal(t, BatchImageJobStatusFailed, repo.jobs[stale.BatchID].Status)
	require.Empty(t, billing.releases)
	require.Empty(t, queue.enqueued)
}
