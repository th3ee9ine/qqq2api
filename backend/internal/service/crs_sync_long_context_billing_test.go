//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type crsLongContextAccountRepo struct {
	AccountRepository
	accounts map[string]*Account
	nextID   int64
}

type crsOpenAILongContextSource struct {
	collection  string
	credentials map[string]any
	extra       map[string]any
}

func newCRSLongContextAccountRepo(existing ...*Account) *crsLongContextAccountRepo {
	repo := &crsLongContextAccountRepo{accounts: make(map[string]*Account)}
	for _, account := range existing {
		if account == nil {
			continue
		}
		crsID, _ := account.Extra["crs_account_id"].(string)
		repo.accounts[crsID] = account
		if account.ID > repo.nextID {
			repo.nextID = account.ID
		}
	}
	return repo
}

func (r *crsLongContextAccountRepo) Create(_ context.Context, account *Account) error {
	r.nextID++
	account.ID = r.nextID
	crsID, _ := account.Extra["crs_account_id"].(string)
	r.accounts[crsID] = account
	return nil
}

func (r *crsLongContextAccountRepo) Update(_ context.Context, account *Account) error {
	crsID, _ := account.Extra["crs_account_id"].(string)
	r.accounts[crsID] = account
	return nil
}

func (r *crsLongContextAccountRepo) GetByCRSAccountID(_ context.Context, crsID string) (*Account, error) {
	return r.accounts[crsID], nil
}

func (r *crsLongContextAccountRepo) ListShadowsByParent(_ context.Context, _ int64) ([]*Account, error) {
	return nil, nil
}

func TestCRSSyncOpenAILongContextBilling(t *testing.T) {
	tests := []struct {
		name          string
		collection    string
		credentials   map[string]any
		sourceExtra   map[string]any
		existingExtra map[string]any
		wantAction    string
		wantEnabled   bool
	}{
		{name: "OAuth create defaults missing value disabled", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, wantAction: "created"},
		{name: "OAuth create preserves source true", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "created", wantEnabled: true},
		{name: "OAuth create preserves source false", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: false}, wantAction: "created"},
		{name: "OAuth update defaults missing value disabled", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, existingExtra: map[string]any{"existing": true}, wantAction: "updated"},
		{name: "OAuth update preserves existing true when source omits value", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "updated", wantEnabled: true},
		{name: "OAuth update preserves existing false when source omits value", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: false}, wantAction: "updated"},
		{name: "OAuth update preserves source true over existing false", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: true}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: false}, wantAction: "updated", wantEnabled: true},
		{name: "OAuth update preserves source false over existing true", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: false}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "updated"},
		{name: "OAuth rejects malformed source value", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: "false"}, wantAction: "failed"},
		{name: "OAuth rejects malformed existing value", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: "false"}, wantAction: "failed"},
		{name: "OAuth update rejects malformed source value", collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "oauth-token"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: "false"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "failed"},
		{name: "API key create defaults missing value disabled", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, wantAction: "created"},
		{name: "API key create preserves source true", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "created", wantEnabled: true},
		{name: "API key create preserves source false", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: false}, wantAction: "created"},
		{name: "API key update defaults missing value disabled", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, existingExtra: map[string]any{"existing": true}, wantAction: "updated"},
		{name: "API key update preserves existing true when source omits value", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "updated", wantEnabled: true},
		{name: "API key update preserves existing false when source omits value", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: false}, wantAction: "updated"},
		{name: "API key update preserves source true over existing false", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: true}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: false}, wantAction: "updated", wantEnabled: true},
		{name: "API key update preserves source false over existing true", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: false}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "updated"},
		{name: "API key rejects malformed source value", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: "false"}, wantAction: "failed"},
		{name: "API key rejects malformed existing value", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: "false"}, wantAction: "failed"},
		{name: "API key update rejects malformed source value", collection: "openaiResponsesAccounts", credentials: map[string]any{"api_key": "sk-test"}, sourceExtra: map[string]any{openAILongContextBillingEnabledKey: "false"}, existingExtra: map[string]any{openAILongContextBillingEnabledKey: true}, wantAction: "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const crsID = "crs-openai-1"
			var existing *Account
			if tt.existingExtra != nil {
				existingExtra := mergeMap(tt.existingExtra, map[string]any{"crs_account_id": crsID})
				accountType := AccountTypeOAuth
				if tt.collection == "openaiResponsesAccounts" {
					accountType = AccountTypeAPIKey
				}
				existing = &Account{ID: 41, Platform: PlatformOpenAI, Type: accountType, Extra: existingExtra}
			}
			repo := newCRSLongContextAccountRepo(existing)
			result := runCRSOpenAILongContextSync(t, repo, crsOpenAILongContextSource{
				collection:  tt.collection,
				credentials: tt.credentials,
				extra:       tt.sourceExtra,
			})

			require.Len(t, result.Items, 1)
			require.Equal(t, tt.wantAction, result.Items[0].Action)
			if tt.wantAction == "failed" {
				require.Contains(t, result.Items[0].Error, "openai_long_context_billing_enabled must be a boolean")
				return
			}
			stored, ok := repo.accounts[crsID].Extra[openAILongContextBillingEnabledKey]
			require.True(t, ok)
			require.Equal(t, tt.wantEnabled, stored)
		})
	}
}

func TestCRSSyncSkipsRetiredGeminiCollections(t *testing.T) {
	repo := newCRSLongContextAccountRepo()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/web/auth/login" {
			_, _ = response.Write([]byte(`{"success":true,"token":"admin-token"}`))
			return
		}
		require.Equal(t, "/admin/sync/export-accounts", request.URL.Path)
		require.NoError(t, json.NewEncoder(response).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"claudeAccounts": []map[string]any{
					{"kind": "claude", "id": "retired-in-claude", "name": "Retired in Claude", "platform": "antigravity", "authType": "oauth", "credentials": map[string]any{"access_token": "legacy"}},
				},
				"claudeConsoleAccounts": []map[string]any{
					{"kind": "claude-console", "id": "retired-in-console", "name": "Retired in Console", "platform": "deepseek", "credentials": map[string]any{"api_key": "legacy"}},
				},
				"openaiOAuthAccounts": []map[string]any{
					{"kind": "openai-oauth", "id": "retired-in-openai-oauth", "name": "Retired in OpenAI OAuth", "platform": "grok", "credentials": map[string]any{"access_token": "legacy"}},
				},
				"openaiResponsesAccounts": []map[string]any{
					{"kind": "openai-responses", "id": "retired-in-openai-api", "name": "Retired in OpenAI API", "platform": "kimi", "credentials": map[string]any{"api_key": "legacy"}},
				},
				"geminiOAuthAccounts": []map[string]any{
					{"kind": "gemini-oauth", "id": "retired-oauth", "name": "Gemini OAuth", "credentials": map[string]any{"refresh_token": "legacy"}},
				},
				"geminiApiKeyAccounts": []map[string]any{
					{"kind": "gemini-api-key", "id": "retired-apikey", "name": "Gemini API Key", "credentials": map[string]any{"api_key": "legacy"}},
				},
			},
		}))
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	syncService := NewCRSSyncService(repo, nil, nil, nil, nil, cfg)
	result, err := syncService.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "password",
	})
	require.NoError(t, err)
	require.Zero(t, result.Created)
	require.Zero(t, result.Updated)
	require.Equal(t, 6, result.Skipped)
	require.Zero(t, result.Failed)
	require.Len(t, result.Items, 6)
	for _, item := range result.Items {
		require.Equal(t, "skipped", item.Action)
		require.Equal(t, "platform is retired", item.Error)
	}
	require.Empty(t, repo.accounts)
}

func runCRSOpenAILongContextSync(t *testing.T, repo AccountRepository, source crsOpenAILongContextSource) *SyncFromCRSResult {
	t.Helper()
	account := map[string]any{
		"kind":        "openai",
		"id":          "crs-openai-1",
		"name":        "OpenAI CRS",
		"isActive":    true,
		"schedulable": true,
		"credentials": source.credentials,
	}
	if source.extra != nil {
		account["extra"] = source.extra
	}

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/web/auth/login" {
			_, _ = response.Write([]byte(`{"success":true,"token":"admin-token"}`))
			return
		}
		require.Equal(t, "/admin/sync/export-accounts", request.URL.Path)
		require.NoError(t, json.NewEncoder(response).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{source.collection: []any{account}},
		}))
	}))
	t.Cleanup(server.Close)

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	service := NewCRSSyncService(repo, nil, nil, nil, nil, cfg)
	result, err := service.SyncFromCRS(context.Background(), SyncFromCRSInput{
		BaseURL:  server.URL,
		Username: "admin",
		Password: "password",
	})
	require.NoError(t, err)
	return result
}
