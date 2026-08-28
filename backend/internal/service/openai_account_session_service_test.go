//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

func TestDecodeOpenAIAccountSessionsAcceptsEnvelopeAliases(t *testing.T) {
	payload := []byte(`{
		"data": {
			"activeSessions": [{
				"sessionId": "sess-1",
				"device": {"name": "MacBook Pro", "type": "desktop", "os": "macOS", "browser": "Chrome"},
				"app": {"name": "ChatGPT"},
				"location": {"city": "Shanghai", "country": "China"},
				"signedInAt": "2026-08-20T10:00:00Z",
				"lastActiveAt": 1787900000,
				"isCurrent": false,
				"isTrusted": true,
				"canLogout": true
			}]
		}
	}`)

	result, err := decodeOpenAIAccountSessions(payload)

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	session := result.Sessions[0]
	require.Equal(t, "sess-1", session.ID)
	require.Equal(t, "MacBook Pro", session.DeviceName)
	require.Equal(t, "desktop", session.DeviceType)
	require.Equal(t, "macOS", session.OS)
	require.Equal(t, "Chrome", session.Browser)
	require.Equal(t, "ChatGPT", session.AppName)
	require.Equal(t, "Shanghai, China", session.Location)
	require.Equal(t, "1787900000", session.LastActiveAt)
	require.True(t, session.Trusted)
	require.True(t, session.StatusAvailable)
	require.True(t, session.CanRevoke)
}

func TestDecodeOpenAIAccountSessionsNeverAllowsCurrentOrUnavailableSessionRevoke(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`[
		{"id":"current","current":true,"can_revoke":true},
		{"id":"unknown","status":"session_status_unavailable","can_revoke":true}
	]`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 2)
	require.False(t, result.Sessions[0].CanRevoke)
	require.False(t, result.Sessions[1].StatusAvailable)
	require.False(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsHonorsEnvelopeCurrentSessionID(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"current_session_id":"sess-current",
		"sessions":[{"id":"sess-current","can_revoke":true}]
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	require.True(t, result.Sessions[0].Current)
	require.False(t, result.Sessions[0].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsAcceptsCurrentDevicesContract(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"show_session_manager": true,
		"devices": [{
			"render_id": "us-render-id",
			"display_name": "Mac",
			"human_readable_description": "Mac",
			"platform": "macos",
			"os_version": "10.15.7",
			"device_model": "Mac",
			"is_trusted_device": false,
			"is_current_device": true,
			"can_untrust": false,
			"hashed_device_id": "hashed-device-id",
			"session_id": "us_session-test",
			"last_signed_in_timestamp_second": 1787891587,
			"last_signed_in_city": "Reus",
			"last_signed_in_region_code": "CT",
			"last_signed_in_country": "ES",
			"app_sessions": [
				{"client_name": "ChatGPT Web"},
				{"client_name": "Codex"}
			]
		}]
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	session := result.Sessions[0]
	require.Equal(t, "us_session-test", session.ID)
	require.Equal(t, "Mac", session.DeviceName)
	require.Equal(t, "macos", session.DeviceType)
	require.Equal(t, "macos 10.15.7", session.OS)
	require.Equal(t, "ChatGPT Web, Codex", session.AppName)
	require.Equal(t, "Reus, CT, ES", session.Location)
	require.Equal(t, "1787891587", session.SignedInAt)
	require.True(t, session.Current)
	require.False(t, session.Trusted)
	require.True(t, session.StatusAvailable)
	require.False(t, session.CanRevoke)
}

func TestOpenAIAccountSessionServiceListsAndRevokes(t *testing.T) {
	account := &Account{
		ID:       71,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "acct-session-test",
		},
	}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "access-session-test"}}
	tokenProvider := NewOpenAITokenProvider(repo, cache, nil)

	var methods []string
	var paths []string
	var revokeBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		require.Equal(t, "Bearer access-session-test", r.Header.Get("authorization"))
		require.Equal(t, "acct-session-test", r.Header.Get("chatgpt-account-id"))
		require.Empty(t, r.Header.Get("cookie"), "Codex access tokens must not depend on a browser session cookie")
		w.Header().Set("content-type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"sessions":[{"id":"sess-a","device_name":"Firefox","can_revoke":true}]}`))
			return
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&revokeBody))
		_, _ = w.Write([]byte(`{"revoked_unified_sessions":1,"revoked_app_sessions":2}`))
	}))
	defer srv.Close()

	svc := NewOpenAIQuotaService(repo, nil, tokenProvider, newQuotaRedirectingFactory(srv))
	list, err := svc.ListSessions(context.Background(), account.ID)
	require.NoError(t, err)
	require.Len(t, list.Sessions, 1)
	require.Equal(t, "sess-a", list.Sessions[0].ID)
	require.NotZero(t, list.FetchedAt)

	err = svc.RevokeSession(context.Background(), account.ID, "us_session-test")
	require.NoError(t, err)
	require.Equal(t, []string{http.MethodGet, http.MethodPost}, methods)
	require.Equal(t, []string{"/backend-api/accounts/sessions", "/backend-api/accounts/sessions/revoke"}, paths)
	require.Equal(t, map[string]string{"session_id": "us_session-test"}, revokeBody)
}

func TestOpenAIAccountSessionServiceBatchRevokeReportsPartialSuccess(t *testing.T) {
	account := &Account{
		ID:       72,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "acct-batch-test",
		},
	}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "access-batch-test"}}
	tokenProvider := NewOpenAITokenProvider(repo, cache, nil)

	var revokedIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		revokedIDs = append(revokedIDs, body["session_id"])
		w.Header().Set("content-type", "application/json")
		if body["session_id"] == "sess-b" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"revoked_unified_sessions":1}`))
	}))
	defer srv.Close()

	svc := NewOpenAIQuotaService(repo, nil, tokenProvider, newQuotaRedirectingFactory(srv))
	result, err := svc.RevokeSessions(context.Background(), account.ID, []string{"sess-a", "sess-b", "sess-a", " sess-c "})

	require.NoError(t, err)
	require.Equal(t, []string{"sess-a", "sess-b", "sess-c"}, revokedIDs)
	require.Equal(t, 3, result.RequestedCount)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, []string{"sess-a", "sess-c"}, result.RevokedSessionIDs)
	require.Equal(t, []OpenAIAccountSessionRevokeFailure{{SessionID: "sess-b", Code: "OPENAI_SESSION_NOT_FOUND"}}, result.Failures)
}

func TestNormalizeOpenAIAccountSessionIDsRejectsInvalidBatch(t *testing.T) {
	_, err := normalizeOpenAIAccountSessionIDs(nil)
	require.Equal(t, "OPENAI_SESSIONS_BATCH_EMPTY", infraerrors.Reason(err))

	tooMany := make([]string, maxOpenAIAccountSessionBatchSize+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("sess-%d", index)
	}
	_, err = normalizeOpenAIAccountSessionIDs(tooMany)
	require.Equal(t, "OPENAI_SESSIONS_BATCH_TOO_LARGE", infraerrors.Reason(err))
}

func TestDecodeOpenAIAccountSessionsRejectsMissingCollection(t *testing.T) {
	_, err := decodeOpenAIAccountSessions([]byte(`{"result":"ok"}`))
	require.ErrorContains(t, err, "sessions collection is missing")
}
