package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type turnStateBurstCASLossRepo struct {
	*turnStateAutoRepo
	calls atomic.Int32
}

func (r *turnStateBurstCASLossRepo) CompareAndSwapCodexTurnStateProbeBurstBudget(
	context.Context,
	int64,
	string,
	int64,
	CodexTurnStateProbeBurstBudget,
) (bool, error) {
	r.calls.Add(1)
	return false, nil
}

type turnStateNoBurstRepository struct {
	AccountRepository
}

func TestCodexTurnStateProbeBurstOnlyStartsWithoutValidVerifiedState(t *testing.T) {
	now := time.Now()
	valid := testGlobalTurnStateToken(now, 12)
	expired := testGlobalTurnStateToken(now.Add(-codexTurnStateTTL-time.Second), 12)

	tests := []struct {
		name  string
		entry *codexTurnStateAutoEntry
		want  bool
	}{
		{
			name:  "missing state",
			entry: &codexTurnStateAutoEntry{model: "gpt-6-astra"},
			want:  true,
		},
		{
			name: "expired state",
			entry: &codexTurnStateAutoEntry{
				model: "gpt-6-astra", token: expired,
				setAt:      now.Add(-codexTurnStateTTL - time.Second).UnixMilli(),
				verifiedAt: now.Add(-codexTurnStateTTL - time.Second).UnixMilli(), verifiedModel: "gpt-6-astra",
			},
			want: true,
		},
		{
			name: "recovery pending",
			entry: &codexTurnStateAutoEntry{
				model: "gpt-6-astra", token: valid, setAt: now.UnixMilli(),
				verifiedAt: now.UnixMilli(), verifiedModel: "gpt-6-astra",
				recovery: codexTurnStateRecovery{Pending: true},
			},
			want: true,
		},
		{
			name: "unverified token",
			entry: &codexTurnStateAutoEntry{
				model: "gpt-6-astra", token: valid, setAt: now.UnixMilli(),
			},
			want: true,
		},
		{
			name: "verified for another model",
			entry: &codexTurnStateAutoEntry{
				model: "gpt-6-astra", token: valid, setAt: now.UnixMilli(),
				verifiedAt: now.UnixMilli(), verifiedModel: "codex-auto-review",
			},
			want: true,
		},
		{
			name: "valid verified state",
			entry: &codexTurnStateAutoEntry{
				model: "gpt-6-astra", token: valid, setAt: now.UnixMilli(),
				verifiedAt: now.UnixMilli(), verifiedModel: "gpt-6-astra",
			},
			want: false,
		},
		{
			name: "verified same family variant",
			entry: &codexTurnStateAutoEntry{
				model: "gpt-6-astra", token: valid, setAt: now.UnixMilli(),
				verifiedAt: now.UnixMilli(), verifiedModel: "provider/gpt-6-astra-2026-09-19",
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, codexTurnStateProbeNeedsBurst(tc.entry, now))
		})
	}

	// A missing/expired state gets a bounded budget of three full-pool rounds.
	// Each route renews the same short lease instead of extending one lock to the
	// size of the captured pool.
	require.EqualValues(t, time.Minute.Milliseconds(), CodexTurnStateProbeBurstMaxLeaseMS)
	require.Equal(t, 3, codexTurnStateProbeBurstMaxAttempts)
	variant := &codexTurnStateAutoEntry{
		model: "gpt-6-astra", token: valid, setAt: now.UnixMilli(),
		verifiedAt: now.UnixMilli(), verifiedModel: "provider/gpt-6-astra-2026-09-19",
	}
	require.Equal(t, variant.setAt, codexTurnStateProbeBurstGeneration(variant))
}

func TestReserveCodexTurnStateProbeBurstAttemptBoundsAttemptsAndWindow(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	var firstDeadline time.Time
	for attempt := 1; attempt <= codexTurnStateProbeBurstMaxAttempts; attempt++ {
		reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
		require.NoError(t, err)
		require.Equal(t, attempt, reservation.attempt)
		require.Positive(t, reservation.version)
		if attempt == 1 {
			firstDeadline = reservation.deadline
		} else {
			require.WithinDuration(t, firstDeadline, reservation.deadline, time.Second)
		}
		require.NoError(t, s.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, reservation))
	}
	_, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstExhausted)

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, codexTurnStateProbeBurstMaxAttempts, budget.Attempts)
	require.EqualValues(t, codexTurnStateProbeBurstMaxAttempts*2, budget.Version)
	require.Zero(t, budget.InFlightUntilMS)
	require.WithinDuration(t, time.Now().Add(time.Minute), firstDeadline, 2*time.Second)
}

func TestRenewCodexTurnStateProbeBurstAttemptKeepsOneRoundAndAdvancesOwnership(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.NoError(t, err)

	for range codexTurnStateProxyURLsMaxSize + 44 {
		stale := reservation
		reservation, err = s.renewCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, reservation)
		require.NoError(t, err)
		require.Equal(t, 1, reservation.attempt)
		require.Equal(t, stale.version+1, reservation.version)
		require.WithinDuration(t, time.Now().Add(time.Minute), reservation.deadline, 2*time.Second)
		require.ErrorIs(t, s.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, stale), errCodexTurnStateProbeBurstPersistence)
	}

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, 1, budget.Attempts, "renewing every route must still count as one collection round")
	require.Equal(t, reservation.version, budget.Version)
	require.Equal(t, reservation.deadline.UnixMilli(), budget.InFlightUntilMS)
	require.NoError(t, s.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, reservation))
}

func TestReserveCodexTurnStateProbeBurstAttemptDoesNotRollExhaustedGeneration(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	original := CodexTurnStateProbeBurstBudget{
		Version: 4, Generation: 7, Model: model,
		StartedAtMS: time.Now().Add(-time.Hour).UnixMilli(), Attempts: codexTurnStateProbeBurstMaxAttempts,
	}
	repo.mu.Lock()
	repo.accounts[account.ID].Extra = map[string]any{slot: original}
	repo.mu.Unlock()

	_, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, original.Generation)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstExhausted)
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	unchanged, err := codexTurnStateProbeBurstBudgetFromAccount(stored, slot)
	require.NoError(t, err)
	require.Equal(t, original, unchanged, "elapsed wall time must not reopen an exhausted generation")

	reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, original.Generation+1)
	require.NoError(t, err, "a strictly newer verified/recovery generation may open one new burst")
	require.Equal(t, 1, reservation.attempt)
	stored, err = repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	reset, err := codexTurnStateProbeBurstBudgetFromAccount(stored, slot)
	require.NoError(t, err)
	require.Equal(t, original.Generation+1, reset.Generation)
	require.Equal(t, 1, reset.Attempts)
	require.Equal(t, original.Version+1, reset.Version)

	_, err = s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, original.Generation)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstExhausted, "a stale process cannot roll the generation backward")
}

func TestReserveCodexTurnStateProbeBurstAttemptCASLossFailsClosed(t *testing.T) {
	s, base, account := newTurnStateAutoService(t)
	repo := &turnStateBurstCASLossRepo{turnStateAutoRepo: base}
	s.accountRepo = repo

	_, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, "gpt-6-astra", 0)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstPersistence)
	require.EqualValues(t, codexTurnStateProbeBurstCASLimit, repo.calls.Load())
}

func TestReserveCodexTurnStateProbeBurstAttemptAcrossInstancesIsSingleInFlight(t *testing.T) {
	first, repo, account := newTurnStateAutoService(t)
	second := &OpenAIGatewayService{accountRepo: repo}
	services := []*OpenAIGatewayService{first, second}
	var successes atomic.Int32
	reservations := make(chan codexTurnStateProbeBurstReservation, 16)
	var wg sync.WaitGroup
	for index := 0; index < 16; index++ {
		wg.Add(1)
		go func(service *OpenAIGatewayService) {
			defer wg.Done()
			if reservation, err := service.reserveCodexTurnStateProbeBurstAttempt(
				context.Background(), account.ID, "gpt-6-astra", 0,
			); err == nil {
				successes.Add(1)
				reservations <- reservation
			}
		}(services[index%len(services)])
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load(), "durable lease permits only one reservation across instances")
	winner := <-reservations
	_, err := second.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, "gpt-6-astra", 0)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstInFlight)
	require.NoError(t, first.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, "gpt-6-astra", 0, winner))

	secondReservation, err := second.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, "gpt-6-astra", 0)
	require.NoError(t, err)
	require.Equal(t, 2, secondReservation.attempt)
	require.NoError(t, second.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, "gpt-6-astra", 0, secondReservation))
	thirdReservation, err := first.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, "gpt-6-astra", 0)
	require.NoError(t, err)
	require.Equal(t, 3, thirdReservation.attempt)
	_, err = second.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, "gpt-6-astra", 0)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstExhausted)

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(
		stored, codexTurnStateProbeBurstBudgetExtraKey("gpt-6-astra"),
	)
	require.NoError(t, err)
	require.Equal(t, codexTurnStateProbeBurstMaxAttempts, budget.Attempts)
	require.EqualValues(t, 5, budget.Version)
	require.Equal(t, thirdReservation.deadline.UnixMilli(), budget.InFlightUntilMS)
}

func TestExpiredCodexTurnStateProbeBurstLeaseAllowsNextRoundButRejectsOldOwner(t *testing.T) {
	first, repo, account := newTurnStateAutoService(t)
	second := &OpenAIGatewayService{accountRepo: repo}
	model := "gpt-6-astra"
	firstReservation, err := first.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.NoError(t, err)

	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	repo.mu.Lock()
	budget := repo.accounts[account.ID].Extra[slot].(CodexTurnStateProbeBurstBudget)
	budget.StartedAtMS = time.Now().Add(-2 * time.Second).UnixMilli()
	budget.InFlightUntilMS = time.Now().Add(-time.Second).UnixMilli()
	repo.accounts[account.ID].Extra[slot] = budget
	repo.mu.Unlock()

	secondReservation, err := second.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.NoError(t, err)
	require.Equal(t, 2, secondReservation.attempt, "a crashed round's expired lease permits the next complete round")
	require.Greater(t, secondReservation.version, firstReservation.version)
	_, err = first.renewCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, firstReservation)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstPersistence)
	require.ErrorIs(t, first.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, firstReservation), errCodexTurnStateProbeBurstPersistence)
	_, _, err = first.markCodexTurnStateProbeCandidatePendingOwned(context.Background(), account.ID, model, 0, firstReservation)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstPersistence)
	require.NoError(t, second.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, secondReservation))
}

func TestCodexTurnStateProbeCandidatePendingIsDurableAcrossInstances(t *testing.T) {
	first, repo, account := newTurnStateAutoService(t)
	second := &OpenAIGatewayService{accountRepo: repo}
	model := "gpt-6-astra"
	reservation, err := first.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.NoError(t, err)
	require.False(t, reservation.deadline.IsZero())

	won, err := first.markCodexTurnStateProbeCandidatePending(context.Background(), account.ID, model, 0, reservation)
	require.NoError(t, err)
	require.True(t, won)
	won, err = second.markCodexTurnStateProbeCandidatePending(context.Background(), account.ID, model, 0, reservation)
	require.NoError(t, err)
	require.False(t, won, "only the first valid candidate may become pending")
	_, err = second.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.ErrorIs(t, err, errCodexTurnStateProbeCandidatePending)

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, 1, budget.Attempts)
	require.EqualValues(t, 2, budget.Version)
	require.Zero(t, budget.InFlightUntilMS)
	require.Greater(t, budget.CandidatePendingUntilMS, time.Now().UnixMilli())
	payload, err := json.Marshal(budget)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "opaque-candidate-state")
}

func TestRestoreCodexTurnStateProbeRoundKeepsOneAttemptAndReturnsLiveLease(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	const model = "gpt-6-astra"

	initial, err := s.reserveCodexTurnStateProbeRound(context.Background(), account.ID, model, 0)
	require.NoError(t, err)
	require.Equal(t, 1, initial.attempt)
	won, owner, err := s.markCodexTurnStateProbeCandidatePendingOwned(context.Background(), account.ID, model, 0, initial)
	require.NoError(t, err)
	require.True(t, won)

	restored, err := s.restoreCodexTurnStateProbeRound(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, 1, restored.attempt, "restoring after a rejected candidate stays in the same pool round")
	require.Equal(t, owner.version+1, restored.version)
	require.WithinDuration(t, time.Now().Add(time.Minute), restored.deadline, 2*time.Second)

	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, 1, budget.Attempts)
	require.Equal(t, restored.version, budget.Version)
	require.Equal(t, restored.deadline.UnixMilli(), budget.InFlightUntilMS)
	require.Zero(t, budget.CandidatePendingUntilMS)

	_, err = s.renewCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, initial)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstPersistence, "the pre-candidate lease must remain stale")
	renewed, err := s.renewCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, restored)
	require.NoError(t, err)
	require.Equal(t, 1, renewed.attempt)
	require.Equal(t, restored.version+1, renewed.version)
	require.NoError(t, s.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0, renewed))

	stored, err = repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err = codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, 1, budget.Attempts)
	require.Zero(t, budget.InFlightUntilMS)
	require.Zero(t, budget.CandidatePendingUntilMS)
}

func TestClearCodexTurnStateProbeCandidatePendingRequiresExactOwner(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.NoError(t, err)
	won, firstOwner, err := s.markCodexTurnStateProbeCandidatePendingOwned(context.Background(), account.ID, model, 0, reservation)
	require.NoError(t, err)
	require.True(t, won)

	require.NoError(t, s.clearCodexTurnStateProbeCandidatePending(context.Background(), firstOwner))
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, firstOwner.version+1, budget.Version)
	require.Zero(t, budget.InFlightUntilMS)
	require.Zero(t, budget.CandidatePendingUntilMS)

	secondReservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, 0)
	require.NoError(t, err)
	require.Equal(t, 2, secondReservation.attempt)
	won, secondOwner, err := s.markCodexTurnStateProbeCandidatePendingOwned(context.Background(), account.ID, model, 0, secondReservation)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, s.clearCodexTurnStateProbeCandidatePending(context.Background(), firstOwner))
	stored, err = repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err = codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, secondOwner.version, budget.Version)
	require.Equal(t, secondOwner.untilMS, budget.CandidatePendingUntilMS, "a stale owner cannot clear a newer marker")
	require.NoError(t, s.clearCodexTurnStateProbeCandidatePending(context.Background(), secondOwner))
}

func TestCodexTurnStateProbeCandidatePendingExpiryAndNewGeneration(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	now := time.Now()
	original := CodexTurnStateProbeBurstBudget{
		Version: 2, Generation: 7, Model: model,
		StartedAtMS: now.Add(-30 * time.Second).UnixMilli(), Attempts: 1,
		CandidatePendingUntilMS: now.Add(-time.Second).UnixMilli(),
	}
	repo.mu.Lock()
	repo.accounts[account.ID].Extra = map[string]any{slot: original}
	repo.mu.Unlock()

	reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, original.Generation)
	require.NoError(t, err, "an expired pending marker is no longer a stopper while its burst remains open")
	require.Equal(t, 2, reservation.attempt)
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	current, err := codexTurnStateProbeBurstBudgetFromAccount(stored, slot)
	require.NoError(t, err)
	require.Zero(t, current.CandidatePendingUntilMS)
	require.Equal(t, reservation.deadline.UnixMilli(), current.InFlightUntilMS)
	require.NoError(t, s.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, original.Generation, reservation))
	stored, err = repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	current, err = codexTurnStateProbeBurstBudgetFromAccount(stored, slot)
	require.NoError(t, err)

	current.CandidatePendingUntilMS = time.Now().Add(time.Minute).UnixMilli()
	repo.mu.Lock()
	repo.accounts[account.ID].Extra[slot] = current
	repo.mu.Unlock()
	reservation, err = s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, original.Generation+1)
	require.NoError(t, err, "a strictly newer durable generation replaces the old pending marker")
	require.Equal(t, 1, reservation.attempt)
	stored, err = repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	current, err = codexTurnStateProbeBurstBudgetFromAccount(stored, slot)
	require.NoError(t, err)
	require.Zero(t, current.CandidatePendingUntilMS)
}

func TestCodexTurnStateProbeBurstGenerationIgnoresLocalVerificationTime(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	setAt := time.Now().Add(-2 * time.Hour).UnixMilli()
	entry := &codexTurnStateAutoEntry{
		model: model, token: "same-durable-state", setAt: setAt,
		verifiedAt: time.Now().Add(-time.Hour).UnixMilli(), verifiedModel: model,
	}
	firstGeneration := codexTurnStateProbeBurstGeneration(entry)
	entry.verifiedAt = time.Now().UnixMilli()
	secondGeneration := codexTurnStateProbeBurstGeneration(entry)
	require.Equal(t, setAt, firstGeneration)
	require.Equal(t, firstGeneration, secondGeneration, "local same-token verification time cannot buy a new burst")

	for range codexTurnStateProbeBurstMaxAttempts {
		reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, firstGeneration)
		require.NoError(t, err)
		require.NoError(t, s.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, firstGeneration, reservation))
	}
	_, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, secondGeneration)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstExhausted)

	entry.token = "atomically-published-different-state"
	entry.setAt = setAt + 1
	newGeneration := codexTurnStateProbeBurstGeneration(entry)
	reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, model, newGeneration)
	require.NoError(t, err)
	require.Equal(t, 1, reservation.attempt)
}

func TestCodexTurnStateProbeBurstFamilyVariantsShareBudget(t *testing.T) {
	s, _, account := newTurnStateAutoService(t)
	variants := []string{"gpt-6", "gpt-6-astra", "provider/gpt-6-astra-2026-09-19"}
	for attempt, variant := range variants {
		owner := codexTurnStateProbeBurstOwnerModel(variant)
		require.Equal(t, "gpt-6-astra", owner)
		reservation, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, owner, 0)
		require.NoError(t, err)
		require.Equal(t, attempt+1, reservation.attempt)
		require.NoError(t, s.releaseCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, owner, 0, reservation))
	}
	_, err := s.reserveCodexTurnStateProbeBurstAttempt(
		context.Background(), account.ID, codexTurnStateProbeBurstOwnerModel("gpt-6-astra-next"), 0,
	)
	require.ErrorIs(t, err, errCodexTurnStateProbeBurstExhausted)

	reviewOwner := codexTurnStateProbeBurstOwnerModel("codex-auto-review-2026-09-19")
	require.Equal(t, "codex-auto-review", reviewOwner)
	review, err := s.reserveCodexTurnStateProbeBurstAttempt(context.Background(), account.ID, reviewOwner, 0)
	require.NoError(t, err, "codex-auto-review owns an independent model-family budget")
	require.Equal(t, 1, review.attempt)
}

func TestCodexTurnStateProbeBurstCountsFullRouteRoundAcrossRestart(t *testing.T) {
	s, repo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	enableCodexTurnStateCASModel(s, model)
	s.proxyRepo = &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
	require.Len(t, s.codexTurnStatePoolRoutes(context.Background(), ""), codexTurnStateProbePoolSize)
	var calls atomic.Int32
	upstream := &turnStateRawUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return turnStateModelResponse("luna-state", "gpt-5.6-luna"), nil
	}}
	s.httpUpstream = upstream

	require.Empty(t, s.autoTurnStateForAccount(context.Background(), account, model))
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, codexTurnStateProbeBurstMaxAttempts, calls.Load())
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err := codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, 1, budget.Attempts, "all three routes belong to one durable round")
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, model)))

	// Simulate the ordinary five-minute scheduler boundary and a process restart.
	// A new full-pool round consumes the second durable attempt.
	scope := codexTurnStateModelAccount(stored, model)
	scope.Extra[CodexTurnStateAutoProbeAtExtraKey] = time.Now().Add(-6 * time.Minute).UnixMilli()
	updateCtx, cancelUpdate := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, repo.UpdateExtra(updateCtx, account.ID, map[string]any{codexTurnStateModelExtraKey(model): scope.Extra}))
	cancelUpdate()
	stored, err = repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	restarted := &OpenAIGatewayService{
		settingService: s.settingService,
		accountRepo:    repo,
		proxyRepo:      s.proxyRepo,
		httpUpstream:   upstream,
	}
	require.Empty(t, restarted.autoTurnStateForAccount(context.Background(), stored, model))
	waitTurnStateAutoIdle(t, restarted)
	require.EqualValues(t, codexTurnStateProbeBurstMaxAttempts*2, calls.Load())
	stored, err = repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	budget, err = codexTurnStateProbeBurstBudgetFromAccount(stored, codexTurnStateProbeBurstBudgetExtraKey(model))
	require.NoError(t, err)
	require.Equal(t, 2, budget.Attempts)
	require.Equal(t, "response_model_mismatch", codexTurnStateModelAccount(stored, model).GetExtraString(CodexTurnStateAutoLastErrorExtraKey))
}

func TestCodexTurnStateProbeCandidatePendingStopsSecondWorker(t *testing.T) {
	first, repo, account := newTurnStateAutoService(t)
	model := "gpt-6-astra"
	enableCodexTurnStateCASModel(first, model)
	first.proxyRepo = &turnStateProxyRepo{proxies: []Proxy{turnStateDedicatedTemplateProxy()}}
	candidate := "opaque-candidate-state"
	var calls atomic.Int32
	upstream := &turnStateRawUpstream{call: func(request *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		if request.Header.Get(openAICodexTurnStateHeader) == "" {
			return turnStateModelResponse(candidate, model), nil
		}
		return turnStateModelResponse("", model), nil
	}}
	first.httpUpstream = upstream

	require.Empty(t, first.autoTurnStateForAccount(context.Background(), account, model))
	waitTurnStateAutoIdle(t, first)
	require.EqualValues(t, 2, calls.Load(), "collection and same-route replay must complete once")
	first.openaiTurnStateMu.Lock()
	require.Equal(t, candidate, first.openaiTurnStates[codexTurnStateKey{account.ID, model}].candidate.state)
	first.openaiTurnStateMu.Unlock()
	stored, err := repo.GetByID(context.Background(), account.ID)
	require.NoError(t, err)
	serialized, err := json.Marshal(stored.Extra)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), candidate, "the durable stopper must not contain opaque state")

	second := &OpenAIGatewayService{
		settingService: first.settingService,
		accountRepo:    repo,
		proxyRepo:      first.proxyRepo,
		httpUpstream:   upstream,
	}
	second.openaiTurnStateMu.Lock()
	entry := second.codexTurnStateEntryLocked(stored, time.Now(), model)
	entry.forceProbe = true
	second.openaiTurnStateMu.Unlock()
	second.runCodexTurnStateProbe(account.ID, entry)
	require.EqualValues(t, 2, calls.Load(), "a fresh instance must observe pending before contacting another route")
	second.openaiTurnStateMu.Lock()
	require.Empty(t, entry.candidate.state)
	second.openaiTurnStateMu.Unlock()
}

func TestCodexTurnStateProbeBurstContractOnlyAppliesWithoutValidState(t *testing.T) {
	for _, tc := range []struct {
		name      string
		seedState bool
		wantCalls int32
	}{
		{name: "missing_state_fails_closed", wantCalls: 0},
		{name: "valid_state_renewal_keeps_existing_path", seedState: true, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, base, account := newTurnStateAutoService(t)
			model := "gpt-5"
			if tc.seedState {
				old := testGlobalTurnStateToken(time.Now().Add(-51*time.Minute), 10)
				seedAutomaticTurnState(account, old, model)
				scope := codexTurnStateModelAccount(account, model)
				scope.Extra[CodexTurnStateAutoSetAtExtraKey] = time.Now().Add(-51 * time.Minute).UnixMilli()
				scope.Extra[CodexTurnStateAutoVerifiedAtExtraKey] = time.Now().Add(-51 * time.Minute).UnixMilli()
				base.mu.Lock()
				base.accounts[account.ID].Extra = mergeMap(nil, account.Extra)
				base.mu.Unlock()
			}
			s.accountRepo = &turnStateNoBurstRepository{AccountRepository: base}
			var calls atomic.Int32
			s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls.Add(1)
				return turnStateModelResponse("renewed", model), nil
			}}

			s.autoTurnStateForAccount(context.Background(), account, model)
			waitTurnStateAutoIdle(t, s)
			require.Equal(t, tc.wantCalls, calls.Load())
		})
	}
}
