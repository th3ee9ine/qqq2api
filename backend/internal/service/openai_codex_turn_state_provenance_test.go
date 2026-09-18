package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type turnStateProvenanceRepoStub struct {
	AccountRepository

	mu          sync.Mutex
	origins     map[string][]CodexTurnStateNativeProvenance
	recorded    []string
	findErr     error
	recordError error
}

func (r *turnStateProvenanceRepoStub) RecordCodexTurnStateProvenance(_ context.Context, accountID int64, model, digest string, expiresAtMS int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recordError != nil {
		return r.recordError
	}
	r.recorded = append(r.recorded, digest)
	if r.origins == nil {
		r.origins = make(map[string][]CodexTurnStateNativeProvenance)
	}
	origin := CodexTurnStateNativeProvenance{AccountID: accountID, Model: model, ExpiresAtMS: expiresAtMS}
	list := r.origins[digest]
	for i := range list {
		if list[i].AccountID == accountID && list[i].Model == model {
			list[i] = origin
			r.origins[digest] = list
			return nil
		}
	}
	r.origins[digest] = append(list, origin)
	return nil
}

func (r *turnStateProvenanceRepoStub) FindCodexTurnStateProvenance(_ context.Context, digest string) ([]CodexTurnStateNativeProvenance, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.findErr != nil {
		return nil, r.findErr
	}
	return append([]CodexTurnStateNativeProvenance(nil), r.origins[digest]...), nil
}

func turnStateModelRequest(model string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return request.WithContext(withCodexTurnStateModel(request.Context(), model))
}

func turnStateHeader(state string) http.Header {
	header := http.Header{}
	header.Set(openAICodexTurnStateHeader, state)
	return header
}

func TestCodexTurnStateNativeProvenancePersistsDigestOnly(t *testing.T) {
	repo := &turnStateProvenanceRepoStub{}
	service := &OpenAIGatewayService{accountRepo: repo}
	account := &Account{ID: 42}
	c, _ := newTurnStateTestContext(t, 7, "native-digest")
	const state = "opaque-native-state-must-not-be-persisted"
	before := time.Now()

	service.noteOpenAICodexTurnStateProvenance(c, account, state, turnStateModelRequest("gpt-6-astra"))

	digest := codexTurnStateCanonicalDigest(state)
	require.Len(t, digest, 64)
	require.NotEqual(t, state, digest)
	repo.mu.Lock()
	require.Equal(t, []string{digest}, repo.recorded)
	require.Equal(t, []CodexTurnStateNativeProvenance{{
		AccountID: 42, Model: "gpt-6-astra", ExpiresAtMS: repo.origins[digest][0].ExpiresAtMS,
	}}, repo.origins[digest])
	expiresAt := time.UnixMilli(repo.origins[digest][0].ExpiresAtMS)
	repo.mu.Unlock()
	require.WithinDuration(t, before.Add(time.Hour), expiresAt, 2*time.Second)

	raw, ok := service.openaiCodexTurnStateOrigins.Load(openAICodexTurnStateDigestKey(digest))
	require.True(t, ok)
	origin := raw.(openAICodexTurnStateOrigin)
	require.Equal(t, int64(42), origin.accountID)
	require.Equal(t, "gpt-6-astra", origin.model)
	require.Equal(t, digest, origin.digest)
	require.NotContains(t, origin.digest, state)
}

func TestCodexTurnStatePendingProvenanceRequiresVerifiedLifecycle(t *testing.T) {
	repo := &turnStateProvenanceRepoStub{}
	settings, settingsRepo := turnStateTestSettings("", "")
	settingsRepo.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	service := &OpenAIGatewayService{accountRepo: repo, settingService: settings}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	mismatchContext, _ := newTurnStateTestContext(t, 7, "native-mismatch")
	service.stagePendingCodexTurnStateObservation(
		mismatchContext,
		account,
		"luna-state-must-not-own-astra-scope",
		turnStateModelRequest("gpt-6-astra"),
	)
	require.Empty(t, repo.recorded, "a response header alone is not provenance")
	mismatchObserver := beginUpstreamResponseModelObservation(mismatchContext)
	mismatchObserver.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-5.6-luna"}}`), "response.created")
	mismatchObserver.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5.6-luna"}}`), "response.completed")
	service.commitPendingCodexTurnStateObservation(mismatchContext, true)
	require.Empty(t, repo.recorded, "a mismatched completed response cannot establish provenance")

	verifiedContext, _ := newTurnStateTestContext(t, 7, "native-verified")
	const verifiedState = "verified-astra-native-state"
	service.stagePendingCodexTurnStateObservation(
		verifiedContext,
		account,
		verifiedState,
		turnStateModelRequest("gpt-6-astra"),
	)
	verifiedObserver := beginUpstreamResponseModelObservation(verifiedContext)
	verifiedObserver.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"openai/gpt-6-astra-2026-09-18"}}`), "response.created")
	verifiedObserver.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-6"}}`), "response.completed")
	service.commitPendingCodexTurnStateObservation(verifiedContext, true)
	require.Equal(t, []string{codexTurnStateCanonicalDigest(verifiedState)}, repo.recorded)
}

func TestCodexTurnStateNativeProvenanceSurvivesRestartAndSeparatesScope(t *testing.T) {
	const state = "restart-persistent-native-state"
	repo := &turnStateProvenanceRepoStub{}
	issuer := &OpenAIGatewayService{accountRepo: repo}
	c, _ := newTurnStateTestContext(t, 7, "issued-before-restart")
	issuer.noteOpenAICodexTurnStateProvenance(c, &Account{ID: 42}, state, turnStateModelRequest("gpt-6-astra"))

	// A fresh service has no process-local origin. The repository is the only
	// provenance shared with another process/instance.
	fresh := func() *OpenAIGatewayService { return &OpenAIGatewayService{accountRepo: repo} }
	requestContext, _ := newTurnStateTestContext(t, 7, "")

	same := turnStateHeader(state)
	fresh().guardOpenAICodexTurnStateEcho(requestContext, &Account{ID: 42}, same, "gpt-6-astra")
	require.Equal(t, state, same.Get(openAICodexTurnStateHeader))

	crossModel := turnStateHeader(state)
	fresh().guardOpenAICodexTurnStateEcho(requestContext, &Account{ID: 42}, crossModel, "codex-auto-review")
	require.Empty(t, crossModel.Get(openAICodexTurnStateHeader))

	crossAccount := turnStateHeader(state)
	fresh().guardOpenAICodexTurnStateEcho(requestContext, &Account{ID: 43}, crossAccount, "gpt-6-astra")
	require.Empty(t, crossAccount.Get(openAICodexTurnStateHeader))
}

func TestCodexTurnStateNativeProvenanceUsesModelFamilyEquivalence(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 7, "family-equivalence")
	account := &Account{ID: 42}
	now := time.Now()
	tests := []struct {
		name      string
		issued    string
		replayed  string
		wantAllow bool
	}{
		{name: "astra_provider_date_to_alias", issued: "openai/gpt-6-astra-2026-09-19", replayed: "gpt-6", wantAllow: true},
		{name: "astra_alias_to_date", issued: "gpt-6", replayed: "gpt-6-astra-2026-09-20", wantAllow: true},
		{name: "auto_review_variant", issued: "codex-auto-review-2026-09-19", replayed: "codex-auto-review", wantAllow: true},
		{name: "astra_not_auto_review", issued: "gpt-6-astra", replayed: "codex-auto-review", wantAllow: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			origin := openAICodexTurnStateOrigin{accountID: account.ID, model: tc.issued, expiresAt: now.Add(time.Hour)}
			require.Equal(t, tc.wantAllow, codexTurnStateOriginMatches(origin, account.ID, tc.replayed))
		})
	}

	const state = "same-native-state-across-astra-variants"
	repo := &turnStateProvenanceRepoStub{}
	service := &OpenAIGatewayService{accountRepo: repo}
	service.noteOpenAICodexTurnStateProvenance(c, account, state, turnStateModelRequest("openai/gpt-6-astra-2026-09-19"))
	service.noteOpenAICodexTurnStateProvenance(c, account, state, turnStateModelRequest("gpt-6"))
	raw, ok := service.openaiCodexTurnStateOrigins.Load(openAICodexTurnStateDigestKey(codexTurnStateCanonicalDigest(state)))
	require.True(t, ok)
	require.False(t, raw.(openAICodexTurnStateOrigin).conflict, "same-family variants must not create ambiguous provenance")

	replayed := turnStateHeader(state)
	(&OpenAIGatewayService{accountRepo: repo}).guardOpenAICodexTurnStateEcho(c, account, replayed, "gpt-6-astra-2026-09-20")
	require.Equal(t, state, replayed.Get(openAICodexTurnStateHeader))
}

func TestCodexTurnStateNativeProvenanceExpiresAndRejectsAmbiguousOwnership(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 7, "")
	const (
		expiredState   = "expired-known-native-state"
		ambiguousState = "ambiguously-owned-native-state"
	)
	repo := &turnStateProvenanceRepoStub{origins: map[string][]CodexTurnStateNativeProvenance{
		codexTurnStateCanonicalDigest(expiredState): {{
			AccountID: 42, Model: "gpt-6-astra", ExpiresAtMS: time.Now().Add(-time.Millisecond).UnixMilli(),
		}},
		codexTurnStateCanonicalDigest(ambiguousState): {
			{AccountID: 42, Model: "gpt-6-astra", ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli()},
			{AccountID: 43, Model: "gpt-6-astra", ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli()},
		},
	}}

	expired := turnStateHeader(expiredState)
	(&OpenAIGatewayService{accountRepo: repo}).guardOpenAICodexTurnStateEcho(c, &Account{ID: 43}, expired, "gpt-6-astra")
	require.Equal(t, expiredState, expired.Get(openAICodexTurnStateHeader), "expired provenance becomes unknown instead of a permanent ownership claim")

	ambiguous := turnStateHeader(ambiguousState)
	(&OpenAIGatewayService{accountRepo: repo}).guardOpenAICodexTurnStateEcho(c, &Account{ID: 42}, ambiguous, "gpt-6-astra")
	require.Empty(t, ambiguous.Get(openAICodexTurnStateHeader), "a digest claimed by multiple accounts must fail closed")
}

func TestCodexTurnStateNativeProvenanceProtectsWithoutSessionSeed(t *testing.T) {
	service := &OpenAIGatewayService{}
	c, _ := newTurnStateTestContext(t, 7, "")
	const state = "sessionless-known-native-state"
	service.noteOpenAICodexTurnStateProvenance(c, &Account{ID: 42}, state, turnStateModelRequest("gpt-6-astra"))

	foreign := turnStateHeader(state)
	service.guardOpenAICodexTurnStateEcho(c, &Account{ID: 43}, foreign, "gpt-6-astra")
	require.Empty(t, foreign.Get(openAICodexTurnStateHeader))
}

func TestCodexTurnStateNativeProvenanceUnknownAndLookupFailureRemainCompatible(t *testing.T) {
	c, _ := newTurnStateTestContext(t, 7, "")
	account := &Account{ID: 42}

	unknown := turnStateHeader("official-state-from-before-provenance-deployment")
	(&OpenAIGatewayService{accountRepo: &turnStateProvenanceRepoStub{}}).
		guardOpenAICodexTurnStateEcho(c, account, unknown, "gpt-6-astra")
	require.NotEmpty(t, unknown.Get(openAICodexTurnStateHeader))

	lookupFailure := turnStateHeader("official-state-while-provenance-store-is-unavailable")
	(&OpenAIGatewayService{accountRepo: &turnStateProvenanceRepoStub{findErr: errors.New("database unavailable")}}).
		guardOpenAICodexTurnStateEcho(c, account, lookupFailure, "gpt-6-astra")
	require.NotEmpty(t, lookupFailure.Get(openAICodexTurnStateHeader))
}

func TestCodexTurnStateKnownForeignNativeFallsBackToAutomaticState(t *testing.T) {
	service, baseRepo, account := newTurnStateAutoService(t)
	automatic := recoveryTestToken(time.Now(), 10, 9)
	seedScopedTurnState(baseRepo, account, "gpt-5.5", automatic)
	provenance := &turnStateProvenanceRepoStub{
		AccountRepository: baseRepo,
		origins: map[string][]CodexTurnStateNativeProvenance{
			codexTurnStateCanonicalDigest("foreign-native"): {{
				AccountID: account.ID + 1, Model: "gpt-5.5", ExpiresAtMS: time.Now().Add(time.Hour).UnixMilli(),
			}},
		},
	}
	service.accountRepo = provenance

	headers := turnStateHeader("foreign-native")
	require.NoError(t, service.applyOpenAICodexTurnState(context.Background(), account, headers, "gpt-5.5"))
	require.Equal(t, automatic, headers.Get(openAICodexTurnStateHeader))

	unknown := turnStateHeader("unknown-official-native")
	require.NoError(t, service.applyOpenAICodexTurnState(context.Background(), account, unknown, "gpt-5.5"))
	require.Equal(t, "unknown-official-native", unknown.Get(openAICodexTurnStateHeader), "unknown native continuation keeps priority over automatic state")
}
