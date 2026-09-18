package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	CodexTurnStateVerificationBudgetExtraKey = "codex_turn_state_verification_budget"
	debugWorkbenchVerificationBudgetTTL      = time.Hour
	debugWorkbenchVerificationBudgetCASLimit = 4
	debugWorkbenchVerificationMaxBaselines   = 16
)

// CodexTurnStateVerificationBudget is account-wide and locks one dedicated API
// key for the local one-hour test window. Only SHA-256 session fingerprints are
// persisted; proxy URLs, usernames, SIDs, and passwords never enter account
// metadata.
type CodexTurnStateVerificationBudget struct {
	Version        int64    `json:"version"`
	StartedAtMS    int64    `json:"started_at_ms"`
	ExpiresAtMS    int64    `json:"expires_at_ms"`
	APIKeyID       int64    `json:"api_key_id"`
	Country        string   `json:"country,omitempty"`
	SessionDigests []string `json:"session_digests,omitempty"`
	Baselines      []string `json:"baselines,omitempty"`
	NotBeforeMS    int64    `json:"not_before_ms,omitempty"`
}

type CodexTurnStateVerificationBudgetRepository interface {
	CompareAndSwapCodexTurnStateVerificationBudget(context.Context, int64, int64, CodexTurnStateVerificationBudget) (bool, error)
}

var (
	errDebugWorkbenchVerificationBudgetUnavailable = errors.New("verification budget persistence unavailable")
	errDebugWorkbenchVerificationBudgetConflict    = errors.New("verification budget changed concurrently")
	errDebugWorkbenchVerificationBudgetCorrupt     = errors.New("verification budget is invalid")
)

func codexTurnStateVerificationBudgetFromAccount(account *Account) (CodexTurnStateVerificationBudget, error) {
	var result CodexTurnStateVerificationBudget
	if account == nil || account.Extra == nil {
		return result, nil
	}
	raw, exists := account.Extra[CodexTurnStateVerificationBudgetExtraKey]
	if !exists || raw == nil {
		return result, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(payload, &result) != nil {
		return CodexTurnStateVerificationBudget{}, errDebugWorkbenchVerificationBudgetCorrupt
	}
	if result.Version <= 0 || result.StartedAtMS <= 0 || result.ExpiresAtMS <= result.StartedAtMS || result.APIKeyID <= 0 ||
		len(result.SessionDigests) > codexTurnStateProbePoolSize || len(result.Baselines) > debugWorkbenchVerificationMaxBaselines {
		return CodexTurnStateVerificationBudget{}, errDebugWorkbenchVerificationBudgetCorrupt
	}
	if result.Country != "" && !codexTurnStateProbeRegionSafe(result.Country) {
		return CodexTurnStateVerificationBudget{}, errDebugWorkbenchVerificationBudgetCorrupt
	}
	seen := make(map[string]struct{}, len(result.SessionDigests))
	for _, digest := range result.SessionDigests {
		if len(digest) != sha256.Size*2 {
			return CodexTurnStateVerificationBudget{}, errDebugWorkbenchVerificationBudgetCorrupt
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return CodexTurnStateVerificationBudget{}, errDebugWorkbenchVerificationBudgetCorrupt
		}
		if _, duplicate := seen[digest]; duplicate {
			return CodexTurnStateVerificationBudget{}, errDebugWorkbenchVerificationBudgetCorrupt
		}
		seen[digest] = struct{}{}
	}
	return result, nil
}

func newCodexTurnStateVerificationBudget(apiKeyID int64, now time.Time) CodexTurnStateVerificationBudget {
	return CodexTurnStateVerificationBudget{
		StartedAtMS: now.UnixMilli(),
		ExpiresAtMS: now.Add(debugWorkbenchVerificationBudgetTTL).UnixMilli(),
		APIKeyID:    apiKeyID,
	}
}

func debugWorkbenchVerificationSessionDigest(country, sessionID string) string {
	digest := sha256.Sum256([]byte(strings.ToUpper(country) + "\x00" + sessionID))
	return hex.EncodeToString(digest[:])
}

func debugWorkbenchStringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func appendSortedUnique(values []string, value string) []string {
	if debugWorkbenchStringSliceContains(values, value) {
		return values
	}
	values = append(values, value)
	sort.Strings(values)
	return values
}

func (s *DebugWorkbenchService) verificationBudgetRepositories() (CodexTurnStateSourceRepository, CodexTurnStateVerificationBudgetRepository, bool, error) {
	if s == nil || s.gateway == nil || s.gateway.accountRepo == nil {
		return nil, nil, false, nil
	}
	source, sourceOK := s.gateway.accountRepo.(CodexTurnStateSourceRepository)
	budget, budgetOK := s.gateway.accountRepo.(CodexTurnStateVerificationBudgetRepository)
	if !sourceOK || !budgetOK {
		return nil, nil, true, errDebugWorkbenchVerificationBudgetUnavailable
	}
	return source, budget, true, nil
}

// mutateDebugWorkbenchVerificationBudget performs a bounded read/CAS loop. A
// fourth concurrent SID cannot pass: a loser reloads the winning three-session
// snapshot before it is allowed to retry the mutation.
func (s *DebugWorkbenchService) mutateDebugWorkbenchVerificationBudget(
	ctx context.Context,
	accountID int64,
	mutate func(*Account, *CodexTurnStateVerificationBudget, time.Time) (bool, error),
) error {
	source, repo, configured, err := s.verificationBudgetRepositories()
	if err != nil {
		return err
	}
	if !configured {
		return errDebugWorkbenchVerificationBudgetUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; attempt < debugWorkbenchVerificationBudgetCASLimit; attempt++ {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		account, loadErr := source.GetCodexTurnStateSource(loadCtx, accountID)
		cancel()
		if loadErr != nil || account == nil || account.ID != accountID {
			return errDebugWorkbenchVerificationBudgetUnavailable
		}
		budget, parseErr := codexTurnStateVerificationBudgetFromAccount(account)
		if parseErr != nil {
			return parseErr
		}
		expectedVersion := budget.Version
		changed, mutateErr := mutate(account, &budget, time.Now())
		if mutateErr != nil {
			return mutateErr
		}
		if !changed {
			return nil
		}
		budget.Version = expectedVersion + 1
		writeCtx, writeCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		updated, updateErr := repo.CompareAndSwapCodexTurnStateVerificationBudget(writeCtx, accountID, expectedVersion, budget)
		writeCancel()
		if updateErr != nil {
			return updateErr
		}
		if updated {
			return nil
		}
	}
	return errDebugWorkbenchVerificationBudgetConflict
}

func activateDebugWorkbenchVerificationBudget(budget *CodexTurnStateVerificationBudget, apiKeyID int64, now time.Time) (bool, error) {
	if budget == nil || apiKeyID <= 0 {
		return false, errDebugWorkbenchVerificationBudgetCorrupt
	}
	if budget.Version == 0 || now.UnixMilli() >= budget.ExpiresAtMS && now.UnixMilli() >= budget.NotBeforeMS {
		version := budget.Version
		*budget = newCodexTurnStateVerificationBudget(apiKeyID, now)
		budget.Version = version
		return true, nil
	}
	if budget.APIKeyID != apiKeyID {
		return false, debugInputError(http.StatusConflict, "the verification window is locked to a different dedicated API key")
	}
	return false, nil
}

func (s *DebugWorkbenchService) reserveDebugWorkbenchVerificationContext(ctx context.Context, apiKeyID int64, account, daily *Account, input DebugWorkbenchRequest) error {
	_, _, configured, repoErr := s.verificationBudgetRepositories()
	if !configured {
		return s.reserveDebugWorkbenchVerification(apiKeyID, account, daily, input)
	}
	if repoErr != nil {
		return debugInputError(http.StatusServiceUnavailable, repoErr.Error())
	}
	if account == nil || daily == nil || account.ID <= 0 || account.ID != daily.ID {
		return debugInputError(http.StatusConflict, "verification account scope is invalid")
	}
	now := time.Now()
	if daily.RateLimitResetAt != nil && daily.RateLimitResetAt.After(now) {
		return debugInputError(http.StatusTooManyRequests, "the selected account's upstream quota cooldown is active; do not rotate the proxy")
	}
	model := debugWorkbenchRequestModel(input.Body)
	stage := input.VerificationStage
	country, sessionDigest := "", ""
	if stage == DebugVerificationStageBaseline && codexTurnStateAccountProxy(account) != codexTurnStateAccountProxy(daily) {
		return debugInputError(http.StatusConflict, "baseline must use the account's daily route")
	}
	if stage == DebugVerificationStageAutomatic && codexTurnStateAccountProxy(account) != codexTurnStateAccountProxy(daily) {
		return debugInputError(http.StatusConflict, "automatic acceptance must use the account's daily route")
	}
	if stage == DebugVerificationStageCapture {
		proxy := account.Proxy
		if proxy == nil || proxy.Host != codexTurnState1024ProxyHost || proxy.Port != codexTurnState1024ProxyPort || proxy.Password == "" || !strings.EqualFold(proxy.Protocol, "socks5") {
			return debugInputError(http.StatusConflict, "capture requires a fixed-country 1024Proxy sticky SID route (region-US, not region-Rand)")
		}
		parts := debugStickyResidentialUsername.FindStringSubmatch(proxy.Username)
		if parts == nil {
			return debugInputError(http.StatusConflict, "capture requires a fixed-country 1024Proxy sticky SID route (region-US, not region-Rand)")
		}
		if codexTurnStateAccountProxy(account) == codexTurnStateAccountProxy(daily) {
			return debugInputError(http.StatusConflict, "capture requires a new dedicated exit, not the daily proxy")
		}
		country = strings.ToUpper(parts[1])
		sessionDigest = debugWorkbenchVerificationSessionDigest(country, parts[2])
	}
	err := s.mutateDebugWorkbenchVerificationBudget(ctx, account.ID, func(source *Account, budget *CodexTurnStateVerificationBudget, current time.Time) (bool, error) {
		accountBoundary := codexTurnStateAutoInt64(source, CodexTurnStateAutoProbeNotBeforeExtraKey)
		if accountBoundary > current.UnixMilli() {
			return false, debugInputError(http.StatusTooManyRequests, "upstream Retry-After cooldown is active; do not rotate the proxy")
		}
		changed, activateErr := activateDebugWorkbenchVerificationBudget(budget, apiKeyID, current)
		if activateErr != nil {
			return false, activateErr
		}
		if budget.NotBeforeMS > current.UnixMilli() {
			return false, debugInputError(http.StatusTooManyRequests, "upstream Retry-After cooldown is active; do not rotate the proxy")
		}
		if stage == DebugVerificationStageBaseline {
			return changed, nil
		}
		if !debugWorkbenchStringSliceContains(budget.Baselines, model) {
			return false, debugInputError(http.StatusConflict, "run this model's no-state baseline first")
		}
		if stage != DebugVerificationStageCapture {
			return changed, nil
		}
		if budget.Country != "" && budget.Country != country {
			return false, debugInputError(http.StatusConflict, "all capture sessions must keep the same country")
		}
		if debugWorkbenchStringSliceContains(budget.SessionDigests, sessionDigest) {
			return false, debugInputError(http.StatusConflict, "this sticky proxy session has already been used for capture")
		}
		if len(budget.SessionDigests) >= codexTurnStateProbePoolSize {
			return false, debugInputError(http.StatusConflict, "the three-new-proxy-session budget is exhausted")
		}
		budget.Country = country
		budget.SessionDigests = appendSortedUnique(budget.SessionDigests, sessionDigest)
		return true, nil
	})
	if errors.Is(err, errDebugWorkbenchVerificationBudgetUnavailable) || errors.Is(err, errDebugWorkbenchVerificationBudgetConflict) || errors.Is(err, errDebugWorkbenchVerificationBudgetCorrupt) {
		return debugInputError(http.StatusServiceUnavailable, err.Error())
	}
	return err
}

func (s *DebugWorkbenchService) noteDebugWorkbenchVerificationBaselineContext(ctx context.Context, apiKeyID, accountID int64, evidence *DebugStateVerification) error {
	if !debugWorkbenchVerifiedBaseline(evidence, accountID, apiKeyID) {
		return nil
	}
	_, _, configured, repoErr := s.verificationBudgetRepositories()
	if !configured {
		s.noteDebugWorkbenchVerificationBaseline(apiKeyID, accountID, evidence)
		return nil
	}
	if repoErr != nil {
		return repoErr
	}
	return s.mutateDebugWorkbenchVerificationBudget(ctx, accountID, func(_ *Account, budget *CodexTurnStateVerificationBudget, now time.Time) (bool, error) {
		changed, err := activateDebugWorkbenchVerificationBudget(budget, apiKeyID, now)
		if err != nil {
			return false, err
		}
		if debugWorkbenchStringSliceContains(budget.Baselines, evidence.RequestedModel) {
			return changed, nil
		}
		if len(budget.Baselines) >= debugWorkbenchVerificationMaxBaselines {
			return false, errDebugWorkbenchVerificationBudgetCorrupt
		}
		budget.Baselines = appendSortedUnique(budget.Baselines, evidence.RequestedModel)
		return true, nil
	})
}

func debugWorkbenchRetryAfterBoundary(attempts []DebugUpstreamAttempt, now time.Time) int64 {
	var boundary time.Time
	for _, attempt := range attempts {
		if attempt.Response == nil || attempt.Response.StatusCode != http.StatusTooManyRequests {
			continue
		}
		delay, ok := codexTurnStateProbeRetryAfter(http.Header(attempt.Response.Headers), now)
		if !ok {
			delay = time.Hour
		}
		var body struct {
			Error struct {
				ResetsAt int64 `json:"resets_at"`
			} `json:"error"`
		}
		if len(attempt.Response.Body) > 0 && json.Unmarshal(attempt.Response.Body, &body) == nil && body.Error.ResetsAt > 0 {
			if resetDelay := time.Unix(body.Error.ResetsAt, 0).Sub(now); resetDelay > delay {
				delay = resetDelay
			}
		}
		if candidate := now.Add(delay); candidate.After(boundary) {
			boundary = candidate
		}
	}
	return boundary.UnixMilli()
}

func (s *DebugWorkbenchService) noteDebugWorkbenchVerificationLimitContext(ctx context.Context, apiKeyID, accountID int64, attempts []DebugUpstreamAttempt) error {
	// Always keep the current process blocked even if the durable write fails.
	s.noteDebugWorkbenchVerificationLimit(apiKeyID, accountID, attempts)
	boundary := debugWorkbenchRetryAfterBoundary(attempts, time.Now())
	if boundary <= 0 {
		return nil
	}
	_, _, configured, repoErr := s.verificationBudgetRepositories()
	if !configured {
		return nil
	}
	if repoErr != nil {
		return repoErr
	}
	boundaryRepo, ok := s.gateway.accountRepo.(CodexTurnStateAccountProbeBoundaryRepository)
	if !ok {
		return errDebugWorkbenchVerificationBudgetUnavailable
	}
	var boundaryErr error
	for attempt := 0; attempt < 3; attempt++ {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		boundaryErr = boundaryRepo.AdvanceCodexTurnStateProbeNotBefore(writeCtx, accountID, boundary)
		cancel()
		if boundaryErr == nil {
			break
		}
	}
	budgetErr := s.mutateDebugWorkbenchVerificationBudget(ctx, accountID, func(_ *Account, budget *CodexTurnStateVerificationBudget, now time.Time) (bool, error) {
		changed, err := activateDebugWorkbenchVerificationBudget(budget, apiKeyID, now)
		if err != nil {
			// The cooldown is account-wide. Preserve an existing key lock but still
			// advance its boundary when a request using the wrong key was attempted.
			if _, ok := err.(*DebugWorkbenchInputError); !ok {
				return false, err
			}
		}
		if boundary <= budget.NotBeforeMS {
			return changed, nil
		}
		budget.NotBeforeMS = boundary
		return true, nil
	})
	if boundaryErr != nil {
		return boundaryErr
	}
	return budgetErr
}
