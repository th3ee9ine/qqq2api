package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type debugVerificationBudgetRepo struct {
	AccountRepository
	mu          sync.Mutex
	account     Account
	casErr      error
	boundaryErr error
	loadCalls   int
	casCalls    int
}

func cloneDebugVerificationExtra(extra map[string]any) map[string]any {
	payload, _ := json.Marshal(extra)
	var cloned map[string]any
	_ = json.Unmarshal(payload, &cloned)
	if cloned == nil {
		cloned = map[string]any{}
	}
	return cloned
}

func (r *debugVerificationBudgetRepo) GetCodexTurnStateSource(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadCalls++
	if id != r.account.ID {
		return nil, ErrAccountNotFound
	}
	copy := r.account
	copy.Extra = cloneDebugVerificationExtra(r.account.Extra)
	return &copy, nil
}

func (r *debugVerificationBudgetRepo) CompareAndSwapCodexTurnStateVerificationBudget(_ context.Context, id, expectedVersion int64, budget CodexTurnStateVerificationBudget) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.casCalls++
	if r.casErr != nil {
		return false, r.casErr
	}
	if id != r.account.ID {
		return false, ErrAccountNotFound
	}
	current, err := codexTurnStateVerificationBudgetFromAccount(&r.account)
	if err != nil {
		return false, err
	}
	if current.Version != expectedVersion {
		return false, nil
	}
	payload, _ := json.Marshal(budget)
	var stored map[string]any
	_ = json.Unmarshal(payload, &stored)
	if r.account.Extra == nil {
		r.account.Extra = map[string]any{}
	}
	r.account.Extra[CodexTurnStateVerificationBudgetExtraKey] = stored
	return true, nil
}

func (r *debugVerificationBudgetRepo) AdvanceCodexTurnStateProbeNotBefore(_ context.Context, id, notBefore int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.boundaryErr != nil {
		return r.boundaryErr
	}
	if id != r.account.ID {
		return ErrAccountNotFound
	}
	if r.account.Extra == nil {
		r.account.Extra = map[string]any{}
	}
	if existing := codexTurnStateAutoInt64(&r.account, CodexTurnStateAutoProbeNotBeforeExtraKey); notBefore > existing {
		r.account.Extra[CodexTurnStateAutoProbeNotBeforeExtraKey] = notBefore
	}
	return nil
}

func newDebugVerificationBudgetService(repo *debugVerificationBudgetRepo) *DebugWorkbenchService {
	return &DebugWorkbenchService{gateway: &OpenAIGatewayService{accountRepo: repo}}
}

func validDebugVerificationBaseline(model string, accountID, apiKeyID int64) *DebugStateVerification {
	return &DebugStateVerification{
		RequestedModel: model, ResponseCreatedModel: "gpt-5.6-luna", ResponseCompletedModel: "gpt-5.6-luna",
		UpstreamResponseModel: "gpt-5.6-luna", UsageLogVerified: true, ActualAccountID: accountID,
		UsageLogAccountID: accountID, UsageLogAPIKeyID: apiKeyID, UsageLogRequestedModel: model,
	}
}

func debugVerificationCaptureAccount(daily *Account, sid string) *Account {
	account := *daily
	proxyID := int64(len(sid) + int(daily.ID))
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{
		ID: proxyID, Protocol: "socks5", Host: codexTurnState1024ProxyHost, Port: codexTurnState1024ProxyPort,
		Username: "testacct-region-US-sid-" + sid + "-t-5", Password: "proxy-test-password", Status: StatusActive,
	}
	return &account
}

func TestDebugWorkbenchDurableBudgetSharesThreeSIDLimitAcrossInstances(t *testing.T) {
	const accountID, apiKeyID = int64(3), int64(9)
	repo := &debugVerificationBudgetRepo{account: Account{ID: accountID, Extra: map[string]any{}}}
	first := newDebugVerificationBudgetService(repo)
	second := newDebugVerificationBudgetService(repo)
	daily := &Account{ID: accountID}
	baselineInput := DebugWorkbenchRequest{VerificationStage: DebugVerificationStageBaseline, Body: json.RawMessage(`{"model":"gpt-6-astra"}`)}
	require.NoError(t, first.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, daily, daily, baselineInput))
	require.NoError(t, first.noteDebugWorkbenchVerificationBaselineContext(context.Background(), apiKeyID, accountID, validDebugVerificationBaseline("gpt-6-astra", accountID, apiKeyID)))

	captureInput := baselineInput
	captureInput.VerificationStage = DebugVerificationStageCapture
	sids := []string{"SessionA1", "SessionB2", "SessionC3", "SessionD4"}
	start := make(chan struct{})
	results := make(chan error, len(sids))
	for i, sid := range sids {
		service := first
		if i%2 == 1 {
			service = second
		}
		account := debugVerificationCaptureAccount(daily, sid)
		go func() {
			<-start
			results <- service.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, account, daily, captureInput)
		}()
	}
	close(start)
	succeeded := 0
	for range sids {
		if err := <-results; err == nil {
			succeeded++
		}
	}
	require.Equal(t, codexTurnStateProbePoolSize, succeeded)

	restarted := newDebugVerificationBudgetService(repo)
	require.Error(t, restarted.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, debugVerificationCaptureAccount(daily, sids[0]), daily, captureInput), "a used SID must remain consumed after restart")
	require.Error(t, restarted.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, debugVerificationCaptureAccount(daily, "SessionE5"), daily, captureInput), "a fourth SID must remain blocked after restart")

	repo.mu.Lock()
	serialized, err := json.Marshal(repo.account.Extra[CodexTurnStateVerificationBudgetExtraKey])
	repo.mu.Unlock()
	require.NoError(t, err)
	for _, secret := range append(sids, "proxy-test-password", "testacct-region-US") {
		require.NotContains(t, string(serialized), secret)
	}
}

func TestDebugWorkbench429BoundarySurvivesRestartAndBlocksOtherModel(t *testing.T) {
	const accountID, apiKeyID = int64(3), int64(9)
	repo := &debugVerificationBudgetRepo{account: Account{ID: accountID, Extra: map[string]any{}}}
	first := newDebugVerificationBudgetService(repo)
	attempts := []DebugUpstreamAttempt{{Response: &DebugHTTPSnapshot{StatusCode: http.StatusTooManyRequests, Headers: map[string][]string{"Retry-After": {"60"}}}}}
	require.NoError(t, first.noteDebugWorkbenchVerificationLimitContext(context.Background(), apiKeyID, accountID, attempts))

	restarted := newDebugVerificationBudgetService(repo)
	daily := &Account{ID: accountID}
	err := restarted.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, daily, daily, DebugWorkbenchRequest{
		VerificationStage: DebugVerificationStageBaseline,
		Body:              json.RawMessage(`{"model":"codex-auto-review"}`),
	})
	var inputErr *DebugWorkbenchInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, http.StatusTooManyRequests, inputErr.StatusCode)

	repo.mu.Lock()
	persisted := codexTurnStateAutoInt64(&repo.account, CodexTurnStateAutoProbeNotBeforeExtraKey)
	repo.mu.Unlock()
	require.Greater(t, persisted, time.Now().UnixMilli())
}

func TestDebugWorkbenchAccountRateLimitBoundaryBlocksBeforeBudgetMutation(t *testing.T) {
	const accountID, apiKeyID = int64(3), int64(9)
	repo := &debugVerificationBudgetRepo{account: Account{ID: accountID, Extra: map[string]any{}}}
	service := newDebugVerificationBudgetService(repo)
	resetAt := time.Now().Add(time.Hour)
	daily := &Account{ID: accountID, RateLimitResetAt: &resetAt}

	err := service.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, daily, daily, DebugWorkbenchRequest{
		VerificationStage: DebugVerificationStageBaseline,
		Body:              json.RawMessage(`{"model":"gpt-6-astra"}`),
	})
	var inputErr *DebugWorkbenchInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, http.StatusTooManyRequests, inputErr.StatusCode)
	require.Equal(t, "the selected account's upstream quota cooldown is active; do not rotate the proxy", inputErr.Message)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Zero(t, repo.loadCalls, "the account cooldown must block before loading a verification budget")
	require.Zero(t, repo.casCalls, "the account cooldown must block before writing a verification budget")
	require.NotContains(t, repo.account.Extra, CodexTurnStateVerificationBudgetExtraKey)
}

func TestDebugWorkbenchDurableBudgetFailsClosedAndLocksDedicatedKey(t *testing.T) {
	const accountID, apiKeyID = int64(3), int64(9)
	repo := &debugVerificationBudgetRepo{account: Account{ID: accountID, Extra: map[string]any{}}}
	service := newDebugVerificationBudgetService(repo)
	daily := &Account{ID: accountID}
	input := DebugWorkbenchRequest{VerificationStage: DebugVerificationStageBaseline, Body: json.RawMessage(`{"model":"gpt-6-astra"}`)}
	require.NoError(t, service.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, daily, daily, input))

	err := service.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID+1, daily, daily, input)
	var inputErr *DebugWorkbenchInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, http.StatusConflict, inputErr.StatusCode)

	repo.casErr = errors.New("database unavailable")
	failingRepo := &debugVerificationBudgetRepo{account: Account{ID: 4, Extra: map[string]any{}}, casErr: repo.casErr}
	failing := newDebugVerificationBudgetService(failingRepo)
	failingDaily := &Account{ID: 4}
	err = failing.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID, failingDaily, failingDaily, input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "database unavailable")
}

func TestDebugWorkbenchDurableBudgetCountryAndExpiryRules(t *testing.T) {
	const accountID, apiKeyID = int64(3), int64(9)
	expired := newCodexTurnStateVerificationBudget(apiKeyID, time.Now().Add(-2*time.Hour))
	expired.Version = 1
	expired.Country = "US"
	expired.SessionDigests = []string{debugWorkbenchVerificationSessionDigest("US", "OldSid01")}
	payload, err := json.Marshal(expired)
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal(payload, &stored))
	repo := &debugVerificationBudgetRepo{account: Account{ID: accountID, Extra: map[string]any{CodexTurnStateVerificationBudgetExtraKey: stored}}}
	service := newDebugVerificationBudgetService(repo)
	daily := &Account{ID: accountID}
	input := DebugWorkbenchRequest{VerificationStage: DebugVerificationStageBaseline, Body: json.RawMessage(`{"model":"gpt-6-astra"}`)}
	require.NoError(t, service.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID+1, daily, daily, input), "an expired window without cooldown may start over with the newly fixed key")

	repo.mu.Lock()
	budget, parseErr := codexTurnStateVerificationBudgetFromAccount(&repo.account)
	repo.mu.Unlock()
	require.NoError(t, parseErr)
	require.Equal(t, apiKeyID+1, budget.APIKeyID)
	require.Empty(t, budget.Country)
	require.Empty(t, budget.SessionDigests)
	require.Greater(t, budget.ExpiresAtMS, time.Now().UnixMilli())

	// A future account boundary remains authoritative even when the local
	// one-hour retention window is otherwise eligible for replacement.
	repo.mu.Lock()
	repo.account.Extra[CodexTurnStateAutoProbeNotBeforeExtraKey] = time.Now().Add(time.Minute).UnixMilli()
	repo.mu.Unlock()
	err = service.reserveDebugWorkbenchVerificationContext(context.Background(), apiKeyID+2, daily, daily, input)
	require.Error(t, err)
	require.Contains(t, fmt.Sprint(err), "Retry-After")
}
