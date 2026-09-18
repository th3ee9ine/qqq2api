package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
)

func TestOpenAICompatSessionStateIsScopedByAccountAndModelFamily(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 41, Type: AccountTypeAPIKey}
	otherAccount := &Account{ID: 42, Type: AccountTypeAPIKey}
	ctx := context.Background()
	const cacheKey = "shared-client-cache-key"

	svc.bindOpenAICompatSessionTurnState(ctx, nil, account, cacheKey, "openai/gpt-6-astra-2026-09-19", "astra-state")
	require.Equal(t, "astra-state", svc.getOpenAICompatSessionTurnState(ctx, nil, account, cacheKey, "gpt-6-astra"))
	require.Equal(t, "astra-state", svc.getOpenAICompatSessionTurnState(ctx, nil, account, cacheKey, "gpt-6"))
	require.Empty(t, svc.getOpenAICompatSessionTurnState(ctx, nil, otherAccount, cacheKey, "gpt-6-astra"))
	require.Empty(t, svc.getOpenAICompatSessionTurnState(ctx, nil, account, cacheKey, "codex-auto-review"))

	svc.bindOpenAICompatSessionTurnState(ctx, nil, account, cacheKey, "codex-auto-review-2026-09-19", "review-state")
	require.Equal(t, "astra-state", svc.getOpenAICompatSessionTurnState(ctx, nil, account, cacheKey, "gpt-6-astra"))
	require.Equal(t, "review-state", svc.getOpenAICompatSessionTurnState(ctx, nil, account, cacheKey, "codex-auto-review"))

	svc.bindOpenAICompatSessionResponseID(ctx, nil, account, cacheKey, "gpt-6-astra-2026-09-19", "astra-response")
	svc.bindOpenAICompatSessionResponseID(ctx, nil, account, cacheKey, "codex-auto-review-2026-09-19", "review-response")
	require.Equal(t, "astra-response", svc.getOpenAICompatSessionResponseID(ctx, nil, account, cacheKey, "gpt-6"))
	require.Equal(t, "review-response", svc.getOpenAICompatSessionResponseID(ctx, nil, account, cacheKey, "codex-auto-review-build-42"))
	require.Empty(t, svc.getOpenAICompatSessionResponseID(ctx, nil, otherAccount, cacheKey, "gpt-6-astra"))
}

func configureMessagesTurnStateScope(t *testing.T, service *OpenAIGatewayService) {
	t.Helper()
	repo := service.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	repo.values[SettingKeyOpenAICodexTurnStateModels] = "gpt-6*,codex-auto-review*"
	repo.values[SettingKeyOpenAICodexTurnStateDefaultModel] = "gpt-6-astra"
	service.settingService.InvalidateOpenAICodexTurnStateCache()
}

func TestForwardAsAnthropic_CompatTurnStateOverridesAutomaticInjection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service, repo, account := newTurnStateAutoService(t)
	configureMessagesTurnStateScope(t, service)
	service.cfg = &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}

	now := time.Now()
	automatic := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	compat := recoveryTestToken(now, 10, 2)
	const (
		cacheKey     = "compat-priority-session"
		storedModel  = "openai/gpt-6-astra-2026-09-19"
		requestModel = "gpt-6-astra-2026-09-20"
	)
	seedScopedTurnState(repo, account, storedModel, automatic)
	service.bindOpenAICompatSessionTurnState(context.Background(), nil, account, cacheKey, storedModel, compat)

	upstream := &httpUpstreamRecorder{resp: openAICompatSSECompletedResponse("resp_compat_priority", requestModel)}
	service.httpUpstream = upstream
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":false}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	result, err := service.ForwardAsAnthropic(context.Background(), c, account, body, cacheKey, requestModel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, compat, upstream.lastReq.Header.Get(openAICodexTurnStateHeader))
	require.NotEqual(t, automatic, upstream.lastReq.Header.Get(openAICodexTurnStateHeader))
}

func TestOpenAICompatTurnStatePriorityAtFinalRefresh(t *testing.T) {
	for _, tc := range []struct {
		name          string
		withCandidate bool
		withNative    bool
	}{
		{name: "compat_over_automatic"},
		{name: "compat_over_candidate", withCandidate: true},
		{name: "native_over_compat_and_automatic", withNative: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, account := newTurnStateAutoService(t)
			configureMessagesTurnStateScope(t, service)
			now := time.Now()
			automatic := recoveryTestToken(now.Add(-time.Minute), 10, 10)
			candidate := recoveryTestToken(now, 10, 11)
			compat := recoveryTestToken(now, 10, 12)
			native := recoveryTestToken(now, 10, 13)
			const model = "gpt-6-astra-2026-09-20"
			seedScopedTurnState(repo, account, model, automatic)

			requestContext := context.Background()
			if tc.withCandidate {
				service.openaiTurnStateMu.Lock()
				entry := service.codexTurnStateEntryLocked(account, now, model)
				service.stageCodexTurnStateUsageCandidateLocked(entry, candidate, entry.recovery.InvalidatedAtMS, now)
				service.openaiTurnStateMu.Unlock()
				requestContext = context.WithValue(requestContext, ctxkey.RequestID, "messages-priority-candidate")
				requestContext = WithOpenAICodexTurnStateUsageVerification(requestContext, 701)
			}

			request := httptest.NewRequest(http.MethodPost, "https://example.test/responses", nil).WithContext(requestContext)
			if tc.withNative {
				request.Header.Set(openAICodexTurnStateHeader, native)
			}
			prepared, err := service.prepareCodexTurnStateRequest(requestContext, request, account, model)
			require.NoError(t, err)
			if tc.withCandidate {
				require.Equal(t, candidate, prepared.Header.Get(openAICodexTurnStateHeader))
			} else if tc.withNative {
				require.Equal(t, native, prepared.Header.Get(openAICodexTurnStateHeader))
			} else {
				require.Equal(t, automatic, prepared.Header.Get(openAICodexTurnStateHeader))
			}
			prepared = preferOpenAICompatTurnState(prepared, compat)

			want := compat
			if tc.withNative {
				want = native
			}
			service.httpUpstream = &turnStateRawUpstream{call: func(sent *http.Request, _ string, _ int64) (*http.Response, error) {
				require.Equal(t, want, sent.Header.Get(openAICodexTurnStateHeader))
				return turnStateResponse(""), nil
			}}
			_, err = service.doOpenAIUpstream(prepared, "", account)
			require.NoError(t, err)
		})
	}
}

func TestOpenAICompatTurnStateIsolationDoesNotReachOutboundRequest(t *testing.T) {
	for _, tc := range []struct {
		name           string
		bindingAccount func(*Account) *Account
		bindingModel   string
	}{
		{
			name: "cross_account",
			bindingAccount: func(account *Account) *Account {
				copy := *account
				copy.ID++
				return &copy
			},
			bindingModel: "gpt-6-astra-2026-09-19",
		},
		{
			name:           "cross_family",
			bindingAccount: func(account *Account) *Account { return account },
			bindingModel:   "codex-auto-review-2026-09-19",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, account := newTurnStateAutoService(t)
			configureMessagesTurnStateScope(t, service)
			now := time.Now()
			automatic := recoveryTestToken(now.Add(-time.Minute), 10, 20)
			foreignCompat := recoveryTestToken(now, 10, 21)
			const (
				cacheKey = "isolated-compat-session"
				model    = "gpt-6-astra-2026-09-20"
			)
			seedScopedTurnState(repo, account, model, automatic)
			service.bindOpenAICompatSessionTurnState(context.Background(), nil, tc.bindingAccount(account), cacheKey, tc.bindingModel, foreignCompat)
			compat := service.getOpenAICompatSessionTurnState(context.Background(), nil, account, cacheKey, model)
			require.Empty(t, compat)

			request := httptest.NewRequest(http.MethodPost, "https://example.test/responses", nil)
			prepared, err := service.prepareCodexTurnStateRequest(context.Background(), request, account, model)
			require.NoError(t, err)
			if compat != "" {
				prepared = preferOpenAICompatTurnState(prepared, compat)
			}
			service.httpUpstream = &turnStateRawUpstream{call: func(sent *http.Request, _ string, _ int64) (*http.Response, error) {
				require.Equal(t, automatic, sent.Header.Get(openAICodexTurnStateHeader))
				require.NotEqual(t, foreignCompat, sent.Header.Get(openAICodexTurnStateHeader))
				return turnStateResponse(""), nil
			}}
			_, err = service.doOpenAIUpstream(prepared, "", account)
			require.NoError(t, err)
		})
	}
}
