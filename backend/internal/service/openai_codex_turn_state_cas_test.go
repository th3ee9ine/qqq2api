package service

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
)

type codexTurnStateCASWinnerRepo struct {
	*turnStateAutoRepo
	winner        *Account
	sourceErr     error
	boundaryErr   error
	updateStart   chan struct{}
	updateResume  chan struct{}
	sourceStart   chan struct{}
	sourceResume  chan struct{}
	boundary      atomic.Int64
	boundaryCalls atomic.Int32
	updateCalls   atomic.Int32
	sourceCalls   atomic.Int32
	getCalls      atomic.Int32
}

func (r *codexTurnStateCASWinnerRepo) AdvanceCodexTurnStateProbeNotBefore(_ context.Context, _ int64, notBefore int64) error {
	r.boundaryCalls.Add(1)
	r.boundary.Store(notBefore)
	return r.boundaryErr
}

func (r *codexTurnStateCASWinnerRepo) UpdateCodexTurnState(ctx context.Context, _ int64, _ string, _ map[string]any) (bool, error) {
	r.updateCalls.Add(1)
	if r.updateStart != nil {
		close(r.updateStart)
	}
	if r.updateResume != nil {
		select {
		case <-r.updateResume:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	return false, nil
}

func (r *codexTurnStateCASWinnerRepo) GetCodexTurnStateSource(ctx context.Context, _ int64) (*Account, error) {
	r.sourceCalls.Add(1)
	if r.sourceStart != nil {
		close(r.sourceStart)
	}
	if r.sourceResume != nil {
		select {
		case <-r.sourceResume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if r.sourceErr != nil {
		return nil, r.sourceErr
	}
	if r.winner == nil {
		return nil, ErrAccountNotFound
	}
	copy := *r.winner
	copy.Extra = mergeMap(nil, r.winner.Extra)
	copy.Credentials = mergeMap(nil, r.winner.Credentials)
	return &copy, nil
}

func (r *codexTurnStateCASWinnerRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.getCalls.Add(1)
	return r.turnStateAutoRepo.GetByID(ctx, id)
}

func codexTurnStateCASWinnerAccount(account *Account, model, state string, setAt, verifiedAt, probeAt, notBefore int64) *Account {
	winner := *account
	winner.Extra = map[string]any{codexTurnStateModelExtraKey(model): map[string]any{
		CodexTurnStateAutoExtraKey:               state,
		CodexTurnStateAutoSetAtExtraKey:          setAt,
		CodexTurnStateAutoVerifiedAtExtraKey:     verifiedAt,
		CodexTurnStateAutoVerifiedModelExtraKey:  model,
		CodexTurnStateAutoProbeAtExtraKey:        probeAt,
		CodexTurnStateAutoProbeNotBeforeExtraKey: notBefore,
		CodexTurnStateAutoLastErrorExtraKey:      "http_429",
	}}
	return &winner
}

func enableCodexTurnStateCASModel(s *OpenAIGatewayService, model string) {
	settings, repo := turnStateTestSettings("", model+"*")
	repo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "true"
	repo.values[SettingKeyOpenAICodexTurnStateDefaultModel] = model
	s.settingService = settings
}

func TestPersistCodexTurnStateCASLossBlocksLocalInjectionAndLoadsDatabaseWinner(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	enableCodexTurnStateCASModel(s, model)
	now := time.Now()
	setAt := now.Add(-5 * time.Minute).UnixMilli()
	verifiedAt := now.Add(-4 * time.Minute).UnixMilli()
	probeAt := now.Add(-3 * time.Minute).UnixMilli()
	notBefore := now.Add(10 * time.Minute).UnixMilli()
	winnerState := "database-winner-state"
	repo := &codexTurnStateCASWinnerRepo{
		turnStateAutoRepo: baseRepo,
		winner:            codexTurnStateCASWinnerAccount(account, model, winnerState, setAt, verifiedAt, probeAt, notBefore),
		updateStart:       make(chan struct{}),
		updateResume:      make(chan struct{}),
		sourceStart:       make(chan struct{}),
		sourceResume:      make(chan struct{}),
	}
	s.accountRepo = repo

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	entry.token = "losing-process-state"
	entry.setAt = now.Add(-2 * time.Minute).UnixMilli()
	entry.verifiedAt = now.Add(-time.Minute).UnixMilli()
	entry.verifiedModel = model
	s.openaiTurnStateMu.Unlock()

	result := make(chan error, 1)
	go func() { result <- s.persistCodexTurnState(account.ID, entry) }()
	<-repo.updateStart
	close(repo.updateResume)
	<-repo.sourceStart

	verificationCtx := WithOpenAICodexTurnStateUsageVerification(context.Background(), 901)
	verificationCtx = context.WithValue(verificationCtx, ctxkey.RequestID, "cas-reconciliation")
	s.openaiTurnStateMu.Lock()
	require.True(t, entry.reconciling)
	entry.token = "concurrent-local-state"
	entry.setAt = now.UnixMilli()
	entry.verifiedAt = now.UnixMilli()
	entry.verifiedModel = model
	entry.candidate = codexTurnStateUsageCandidate{
		state:                 "unverified-local-candidate",
		collectedAt:           now,
		recoveryGeneration:    entry.recovery.InvalidatedAtMS,
		expectedResponseModel: model,
	}
	require.Empty(t, s.codexTurnStateUsageCandidateForRequestLocked(verificationCtx, entry, model, now), "a candidate must not be reserved during CAS reconciliation")
	s.openaiTurnStateMu.Unlock()
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, model), "the losing process state must not be injectable while the winner read is blocked")

	close(repo.sourceResume)
	require.NoError(t, <-result)
	require.EqualValues(t, 1, repo.updateCalls.Load())
	require.EqualValues(t, 1, repo.sourceCalls.Load())
	require.Zero(t, repo.getCalls.Load(), "the narrow database state-source read should win over full account hydration")

	s.openaiTurnStateMu.Lock()
	require.False(t, entry.reconciling)
	require.Equal(t, winnerState, entry.token)
	require.Equal(t, setAt, entry.setAt)
	require.Equal(t, verifiedAt, entry.verifiedAt)
	require.Equal(t, model, entry.verifiedModel)
	require.Equal(t, probeAt, entry.probeAt)
	require.Equal(t, notBefore, entry.probeNotBefore)
	require.Equal(t, "http_429", entry.lastError)
	require.Equal(t, time.UnixMilli(notBefore), entry.probeRetryAfter)
	require.Empty(t, entry.candidate)
	require.False(t, entry.dirty)
	require.False(t, entry.probe)
	require.False(t, entry.forceProbe)
	require.Contains(t, entry.knownTokens, codexTurnStateCanonicalDigest(winnerState))
	require.NotContains(t, entry.knownTokens, codexTurnStateCanonicalDigest("losing-process-state"))
	require.NotContains(t, entry.knownTokens, codexTurnStateCanonicalDigest("concurrent-local-state"))
	s.openaiTurnStateMu.Unlock()

	require.Equal(t, winnerState, s.autoTurnStateForAccount(context.Background(), repo.winner, model))
}

func TestPersistCodexTurnStateCASLossStillPersistsAccountProbeBoundary(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	enableCodexTurnStateCASModel(s, model)
	now := time.Now()
	notBefore := now.Add(10 * time.Minute).UnixMilli()
	repo := &codexTurnStateCASWinnerRepo{
		turnStateAutoRepo: baseRepo,
		winner: codexTurnStateCASWinnerAccount(
			account,
			model,
			"database-winner-state",
			now.Add(-5*time.Minute).UnixMilli(),
			now.Add(-4*time.Minute).UnixMilli(),
			now.Add(-3*time.Minute).UnixMilli(),
			notBefore,
		),
	}
	s.accountRepo = repo

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	entry.token = "losing-process-state"
	entry.setAt = now.Add(-2 * time.Minute).UnixMilli()
	entry.verifiedAt = now.Add(-time.Minute).UnixMilli()
	entry.verifiedModel = model
	entry.probeNotBefore = notBefore
	s.openaiTurnStateMu.Unlock()

	require.NoError(t, s.persistCodexTurnState(account.ID, entry))
	require.EqualValues(t, 1, repo.boundaryCalls.Load(), "the account boundary must be persisted independently of the model-slot CAS")
	require.Equal(t, notBefore, repo.boundary.Load())
	require.EqualValues(t, 1, repo.updateCalls.Load(), "the model-slot CAS must still execute")
	require.EqualValues(t, 1, repo.sourceCalls.Load(), "a lost CAS must reconcile with the database winner")
}

func TestCodexTurnStateCASReconciliationBlocksFinalHeaderInjection(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	enableCodexTurnStateCASModel(s, model)
	s.accountRepo = nil
	now := time.Now()

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	entry.reconciling = true
	entry.token = "losing-process-state"
	entry.setAt = now.UnixMilli()
	entry.verifiedAt = now.UnixMilli()
	entry.verifiedModel = model
	entry.candidate = codexTurnStateUsageCandidate{
		state:                 "unverified-local-candidate",
		collectedAt:           now,
		recoveryGeneration:    entry.recovery.InvalidatedAtMS,
		expectedResponseModel: model,
	}
	s.openaiTurnStateMu.Unlock()

	ctx := WithOpenAICodexTurnStateUsageVerification(context.Background(), 902)
	ctx = context.WithValue(ctx, ctxkey.RequestID, "cas-final-header-check")
	headers := make(http.Header)
	require.NoError(t, s.applyOpenAICodexTurnState(ctx, account, headers, model))
	require.Empty(t, headers.Get(openAICodexTurnStateHeader))
}

func TestPersistCodexTurnStateCASLossDoesNotPromoteUnverifiedDatabaseValue(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	now := time.Now()
	winner := *account
	winner.Extra = map[string]any{codexTurnStateModelExtraKey(model): map[string]any{
		CodexTurnStateAutoExtraKey:      "database-value-without-verification",
		CodexTurnStateAutoSetAtExtraKey: now.UnixMilli(),
	}}
	repo := &codexTurnStateCASWinnerRepo{turnStateAutoRepo: baseRepo, winner: &winner}
	s.accountRepo = repo

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	entry.token = "losing-process-state"
	entry.setAt = now.UnixMilli()
	entry.verifiedAt = now.UnixMilli()
	entry.verifiedModel = model
	entry.candidate = codexTurnStateUsageCandidate{state: "unverified-local-candidate", collectedAt: now, expectedResponseModel: model}
	s.openaiTurnStateMu.Unlock()

	require.NoError(t, s.persistCodexTurnState(account.ID, entry))
	s.openaiTurnStateMu.Lock()
	require.Empty(t, entry.token)
	require.Empty(t, entry.candidate)
	require.Zero(t, entry.verifiedAt)
	require.Empty(t, entry.verifiedModel)
	require.False(t, entry.reconciling)
	s.openaiTurnStateMu.Unlock()
	require.EqualValues(t, 1, repo.sourceCalls.Load())
}

func TestPersistCodexTurnStateManualCASLossKeepsFreshProbeIntentWithoutRestoringLoser(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	now := time.Now()
	winner := *account
	winner.Extra = map[string]any{codexTurnStateModelExtraKey(model): map[string]any{
		CodexTurnStateAutoExtraKey:      "database-value-without-verification",
		CodexTurnStateAutoSetAtExtraKey: now.UnixMilli(),
	}}
	repo := &codexTurnStateCASWinnerRepo{turnStateAutoRepo: baseRepo, winner: &winner}
	s.accountRepo = repo

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	entry.token = "losing-process-state"
	entry.setAt = now.UnixMilli()
	entry.verifiedAt = now.UnixMilli()
	entry.verifiedModel = model
	entry.candidate = codexTurnStateUsageCandidate{state: "losing-local-candidate", collectedAt: now, manual: true, expectedResponseModel: model}
	s.openaiTurnStateMu.Unlock()

	require.NoError(t, s.persistCodexTurnStateWithMode(account.ID, entry, true))
	s.openaiTurnStateMu.Lock()
	require.Empty(t, entry.token)
	require.Empty(t, entry.candidate, "CAS reconciliation must never restore the losing local candidate")
	require.True(t, entry.probe)
	require.True(t, entry.forceProbe)
	require.True(t, entry.manualProbe, "an invalid database winner must retain the administrator's collection intent")
	s.openaiTurnStateMu.Unlock()
	require.EqualValues(t, 1, repo.sourceCalls.Load())
}

func TestPersistCodexTurnStateCASWinnerReadFailureStaysFailClosed(t *testing.T) {
	s, baseRepo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	now := time.Now()
	readErr := errors.New("winner read failed")
	repo := &codexTurnStateCASWinnerRepo{turnStateAutoRepo: baseRepo, sourceErr: readErr}
	s.accountRepo = repo

	s.openaiTurnStateMu.Lock()
	entry := s.codexTurnStateEntryLocked(account, now, model)
	entry.token = "losing-process-state"
	entry.setAt = now.UnixMilli()
	entry.verifiedAt = now.UnixMilli()
	entry.verifiedModel = model
	entry.candidate = codexTurnStateUsageCandidate{state: "unverified-local-candidate", collectedAt: now, expectedResponseModel: model}
	s.openaiTurnStateMu.Unlock()

	require.ErrorIs(t, s.persistCodexTurnState(account.ID, entry), readErr)
	s.openaiTurnStateMu.Lock()
	require.False(t, entry.reconciling)
	require.Empty(t, entry.token)
	require.Empty(t, entry.candidate)
	require.Zero(t, entry.verifiedAt)
	require.Empty(t, entry.verifiedModel)
	s.openaiTurnStateMu.Unlock()
	require.EqualValues(t, 1, repo.sourceCalls.Load())
}
