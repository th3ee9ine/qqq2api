package service

import (
	"context"
	"time"
)

const (
	codexTurnStateProbeBurstCASLimit = 4
)

var (
	errCodexTurnStateProbeBurstExhausted   = codexTurnStateAutoError("probe_burst_exhausted")
	errCodexTurnStateProbeBurstPersistence = codexTurnStateAutoError("probe_burst_persistence_failed")
	errCodexTurnStateProbeCandidatePending = codexTurnStateAutoError("probe_burst_candidate_pending")
	errCodexTurnStateProbeBurstInFlight    = codexTurnStateAutoError("probe_burst_in_flight")
)

type codexTurnStateProbeBurstReservation struct {
	deadline time.Time
	attempt  int
	version  int64
}

// codexTurnStateProbeCandidatePendingOwner identifies the exact durable
// pending marker created by one successful candidate race. The CAS version and
// timestamp together prevent a late cleanup from clearing a newer instance's
// marker for the same account/model generation.
type codexTurnStateProbeCandidatePendingOwner struct {
	accountID  int64
	model      string
	generation int64
	version    int64
	untilMS    int64
}

func codexTurnStateProbeNeedsBurst(entry *codexTurnStateAutoEntry, now time.Time) bool {
	// An opaque token is not a usable state merely because it parses and has not
	// aged out. Only a state that passed the usage-evidence gate for this model
	// family may suppress the missing-state burst. Normalize both sides here as
	// a guard for legacy/in-memory entries that still contain a dated variant.
	if entry == nil {
		return true
	}
	model := codexTurnStateOwnerModel(entry.model)
	verifiedModel := codexTurnStateOwnerModel(entry.verifiedModel)
	if entry.recovery.Pending || entry.token == "" || entry.verifiedAt <= 0 || model == "" || verifiedModel != model || !entry.recovery.allows(entry.token, now) {
		return true
	}
	expiresAt := codexTurnStateAutoExpiry(entry.token, entry.setAt, now)
	return expiresAt <= 0 || now.UnixMilli() >= expiresAt
}

// The generation changes only after a different verified state is atomically
// published (setAt) or an explicit recovery invalidates that state. verifiedAt
// is deliberately excluded: replaying the same token can update local
// verification time without creating a new durable state generation.
func codexTurnStateProbeBurstGeneration(entry *codexTurnStateAutoEntry) int64 {
	if entry == nil {
		return 0
	}
	var generation int64
	if entry.token != "" && entry.setAt > 0 && entry.verifiedAt > 0 && entry.model != "" &&
		codexTurnStateOwnerModel(entry.verifiedModel) == codexTurnStateOwnerModel(entry.model) {
		generation = entry.setAt
	}
	if entry.recovery.InvalidatedAtMS > generation {
		generation = entry.recovery.InvalidatedAtMS
	}
	return generation
}

func codexTurnStateProbeBurstOwnerModel(model string) string {
	return codexTurnStateOwnerModel(model)
}

// Each fresh collection route gets at most the normal sticky-session timeout,
// additionally capped by the durable burst deadline. The same-route replay
// reuses the parent context and does not consume another burst attempt.
func codexTurnStateProbeAttemptContext(parent context.Context, burstDeadline time.Time) (context.Context, context.CancelFunc) {
	return codexTurnStateProbeBoundedContext(parent, codexTurnStateProbeSessionTimeout, burstDeadline)
}

func codexTurnStateProbeReplayContext(parent context.Context, burstDeadline time.Time) (context.Context, context.CancelFunc) {
	return codexTurnStateProbeBoundedContext(parent, codexTurnStateProbeAttemptTimeout, burstDeadline)
}

func codexTurnStateProbeBoundedContext(parent context.Context, timeout time.Duration, burstDeadline time.Time) (context.Context, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
	if parentDeadline, ok := parent.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	if !burstDeadline.IsZero() && burstDeadline.Before(deadline) {
		deadline = burstDeadline
	}
	return context.WithDeadline(parent, deadline)
}

// reserveCodexTurnStateProbeBurstAttempt performs a durable reservation before
// a new exit is contacted. CAS loss is retried from the authoritative account
// row; database/interface failure is fail-closed and never falls back to an
// in-process counter that another service instance could bypass.
func (s *OpenAIGatewayService) reserveCodexTurnStateProbeBurstAttempt(
	ctx context.Context,
	accountID int64,
	model string,
	generation int64,
) (codexTurnStateProbeBurstReservation, error) {
	model = codexTurnStateProbeBurstOwnerModel(model)
	if s == nil || s.accountRepo == nil || accountID <= 0 || model == "" || generation < 0 {
		return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstPersistence
	}
	source, sourceOK := s.accountRepo.(CodexTurnStateSourceRepository)
	repository, repositoryOK := s.accountRepo.(CodexTurnStateProbeBurstBudgetRepository)
	if !sourceOK || !repositoryOK {
		return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstPersistence
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	for casAttempt := 0; casAttempt < codexTurnStateProbeBurstCASLimit; casAttempt++ {
		loadCtx, cancelLoad := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
		account, loadErr := source.GetCodexTurnStateSource(loadCtx, accountID)
		cancelLoad()
		if loadErr != nil || account == nil || account.ID != accountID {
			return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstPersistence
		}

		budget, parseErr := codexTurnStateProbeBurstBudgetFromAccount(account, slot)
		if parseErr != nil {
			return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstPersistence
		}
		expectedVersion := budget.Version
		now := time.Now()
		switch {
		case budget.Version == 0:
			budget = CodexTurnStateProbeBurstBudget{
				Generation:  generation,
				Model:       model,
				StartedAtMS: now.UnixMilli(),
			}
		case generation > budget.Generation:
			budget.Generation = generation
			budget.Model = model
			budget.StartedAtMS = now.UnixMilli()
			budget.Attempts = 0
			budget.InFlightUntilMS = 0
			budget.CandidatePendingUntilMS = 0
		case generation < budget.Generation || budget.Model != model:
			return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstExhausted
		}

		deadline := time.UnixMilli(budget.StartedAtMS).Add(codexTurnStateProbeBurstWindow)
		if budget.CandidatePendingUntilMS > now.UnixMilli() {
			return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeCandidatePending
		}
		if budget.StartedAtMS <= 0 || !now.Before(deadline) || budget.Attempts >= codexTurnStateProbeBurstMaxAttempts {
			return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstExhausted
		}
		if budget.InFlightUntilMS > now.UnixMilli() {
			return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstInFlight
		}
		// An expired pending marker is logically cleared as part of the next
		// reservation. In normal operation its TTL exceeds the one-minute burst,
		// so this only matters for deterministic recovery from stale metadata.
		budget.CandidatePendingUntilMS = 0
		budget.Attempts++
		budget.Version = expectedVersion + 1
		budget.InFlightUntilMS = deadline.UnixMilli()

		writeCtx, cancelWrite := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
		updated, updateErr := repository.CompareAndSwapCodexTurnStateProbeBurstBudget(
			writeCtx, accountID, slot, expectedVersion, budget,
		)
		cancelWrite()
		if updateErr != nil {
			return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstPersistence
		}
		if updated {
			return codexTurnStateProbeBurstReservation{deadline: deadline, attempt: budget.Attempts, version: budget.Version}, nil
		}
	}
	return codexTurnStateProbeBurstReservation{}, errCodexTurnStateProbeBurstPersistence
}

// releaseCodexTurnStateProbeBurstAttempt releases only the caller's own lease.
// A newer reservation or pending candidate changes the version and makes a late
// release fail closed instead of clearing another instance's ownership.
func (s *OpenAIGatewayService) releaseCodexTurnStateProbeBurstAttempt(
	ctx context.Context,
	accountID int64,
	model string,
	generation int64,
	reservation codexTurnStateProbeBurstReservation,
) error {
	model = codexTurnStateProbeBurstOwnerModel(model)
	if s == nil || s.accountRepo == nil || accountID <= 0 || model == "" || generation < 0 || reservation.version <= 0 {
		return errCodexTurnStateProbeBurstPersistence
	}
	source, sourceOK := s.accountRepo.(CodexTurnStateSourceRepository)
	repository, repositoryOK := s.accountRepo.(CodexTurnStateProbeBurstBudgetRepository)
	if !sourceOK || !repositoryOK {
		return errCodexTurnStateProbeBurstPersistence
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	loadCtx, cancelLoad := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	account, loadErr := source.GetCodexTurnStateSource(loadCtx, accountID)
	cancelLoad()
	if loadErr != nil || account == nil || account.ID != accountID {
		return errCodexTurnStateProbeBurstPersistence
	}
	budget, parseErr := codexTurnStateProbeBurstBudgetFromAccount(account, slot)
	if parseErr != nil || budget.Version != reservation.version || budget.Model != model || budget.Generation != generation || budget.InFlightUntilMS <= 0 {
		return errCodexTurnStateProbeBurstPersistence
	}
	expectedVersion := budget.Version
	budget.Version++
	budget.InFlightUntilMS = 0
	writeCtx, cancelWrite := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	updated, updateErr := repository.CompareAndSwapCodexTurnStateProbeBurstBudget(
		writeCtx, accountID, slot, expectedVersion, budget,
	)
	cancelWrite()
	if updateErr != nil || !updated {
		return errCodexTurnStateProbeBurstPersistence
	}
	return nil
}

// markCodexTurnStateProbeCandidatePending makes the first verified maintenance
// candidate visible to every service instance before its opaque state is staged
// in process memory. Only timing metadata is persisted; the state is never
// written to the burst slot. A competing successful probe that loses this CAS
// must discard its candidate.
func (s *OpenAIGatewayService) markCodexTurnStateProbeCandidatePending(
	ctx context.Context,
	accountID int64,
	model string,
	generation int64,
	reservation codexTurnStateProbeBurstReservation,
) (bool, error) {
	won, _, err := s.markCodexTurnStateProbeCandidatePendingOwned(ctx, accountID, model, generation, reservation)
	return won, err
}

// markCodexTurnStateProbeCandidatePendingOwned is the ownership-bearing form
// used by the production probe. The compatibility wrapper above intentionally
// keeps the original return shape for existing callers and tests.
func (s *OpenAIGatewayService) markCodexTurnStateProbeCandidatePendingOwned(
	ctx context.Context,
	accountID int64,
	model string,
	generation int64,
	reservation codexTurnStateProbeBurstReservation,
) (bool, codexTurnStateProbeCandidatePendingOwner, error) {
	model = codexTurnStateProbeBurstOwnerModel(model)
	if s == nil || s.accountRepo == nil || accountID <= 0 || model == "" || generation < 0 || reservation.version <= 0 {
		return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstPersistence
	}
	source, sourceOK := s.accountRepo.(CodexTurnStateSourceRepository)
	repository, repositoryOK := s.accountRepo.(CodexTurnStateProbeBurstBudgetRepository)
	if !sourceOK || !repositoryOK {
		return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstPersistence
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	for casAttempt := 0; casAttempt < codexTurnStateProbeBurstCASLimit; casAttempt++ {
		loadCtx, cancelLoad := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
		account, loadErr := source.GetCodexTurnStateSource(loadCtx, accountID)
		cancelLoad()
		if loadErr != nil || account == nil || account.ID != accountID {
			return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstPersistence
		}
		budget, parseErr := codexTurnStateProbeBurstBudgetFromAccount(account, slot)
		if parseErr != nil || budget.Version == 0 || budget.Model != model || budget.Generation != generation {
			return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstPersistence
		}
		now := time.Now()
		if budget.CandidatePendingUntilMS > now.UnixMilli() {
			return false, codexTurnStateProbeCandidatePendingOwner{}, nil
		}
		if budget.Version != reservation.version || budget.InFlightUntilMS <= 0 {
			return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstPersistence
		}
		if !now.Before(time.UnixMilli(budget.StartedAtMS).Add(codexTurnStateProbeBurstWindow)) {
			return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstExhausted
		}
		expectedVersion := budget.Version
		budget.Version++
		budget.InFlightUntilMS = 0
		budget.CandidatePendingUntilMS = now.Add(codexTurnStateUsageCandidateTTL).UnixMilli()
		writeCtx, cancelWrite := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
		updated, updateErr := repository.CompareAndSwapCodexTurnStateProbeBurstBudget(
			writeCtx, accountID, slot, expectedVersion, budget,
		)
		cancelWrite()
		if updateErr != nil {
			return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstPersistence
		}
		if updated {
			return true, codexTurnStateProbeCandidatePendingOwner{
				accountID: accountID, model: model, generation: generation,
				version: budget.Version, untilMS: budget.CandidatePendingUntilMS,
			}, nil
		}
	}
	return false, codexTurnStateProbeCandidatePendingOwner{}, errCodexTurnStateProbeBurstPersistence
}

// clearCodexTurnStateProbeCandidatePending clears only a marker still owned by
// the caller. A version/model/generation/timestamp mismatch is a normal
// ownership loss and deliberately does nothing; retrying in that case could
// erase a later candidate owned by another service instance.
func (s *OpenAIGatewayService) clearCodexTurnStateProbeCandidatePending(ctx context.Context, owner codexTurnStateProbeCandidatePendingOwner) error {
	model := codexTurnStateProbeBurstOwnerModel(owner.model)
	if s == nil || s.accountRepo == nil || owner.accountID <= 0 || model == "" || owner.generation < 0 || owner.version <= 0 || owner.untilMS <= 0 {
		return errCodexTurnStateProbeBurstPersistence
	}
	source, sourceOK := s.accountRepo.(CodexTurnStateSourceRepository)
	repository, repositoryOK := s.accountRepo.(CodexTurnStateProbeBurstBudgetRepository)
	if !sourceOK || !repositoryOK {
		return errCodexTurnStateProbeBurstPersistence
	}
	if ctx == nil {
		ctx = context.Background()
	}
	slot := codexTurnStateProbeBurstBudgetExtraKey(model)
	loadCtx, cancelLoad := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	account, loadErr := source.GetCodexTurnStateSource(loadCtx, owner.accountID)
	cancelLoad()
	if loadErr != nil || account == nil || account.ID != owner.accountID {
		return errCodexTurnStateProbeBurstPersistence
	}
	budget, parseErr := codexTurnStateProbeBurstBudgetFromAccount(account, slot)
	if parseErr != nil {
		return errCodexTurnStateProbeBurstPersistence
	}
	if budget.Version != owner.version || budget.Model != model || budget.Generation != owner.generation ||
		budget.InFlightUntilMS != 0 || budget.CandidatePendingUntilMS != owner.untilMS {
		return nil
	}
	expectedVersion := budget.Version
	budget.Version++
	budget.CandidatePendingUntilMS = 0
	writeCtx, cancelWrite := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	_, updateErr := repository.CompareAndSwapCodexTurnStateProbeBurstBudget(
		writeCtx, owner.accountID, slot, expectedVersion, budget,
	)
	cancelWrite()
	if updateErr != nil {
		return errCodexTurnStateProbeBurstPersistence
	}
	// A CAS loss means ownership changed; do not retry against the new version.
	return nil
}
