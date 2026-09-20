//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

func accountAdminOAuthContext(ownerID int64) context.Context {
	return context.WithValue(context.Background(), ctxkey.AccountAdminID, ownerID)
}

func TestAuthorizeAccountAdminOAuthSessionRequiresMatchingOwner(t *testing.T) {
	require.NoError(t, authorizeAccountAdminOAuthSession(context.Background(), 0))
	require.NoError(t, authorizeAccountAdminOAuthSession(accountAdminOAuthContext(41), 41))

	for _, ownerID := range []int64{0, 42} {
		err := authorizeAccountAdminOAuthSession(accountAdminOAuthContext(41), ownerID)
		require.Equal(t, "OAUTH_SESSION_SCOPE", infraerrors.Reason(err))
	}
}

func TestOAuthServiceBindsSessionToAccountAdmin(t *testing.T) {
	svc := NewOAuthService(&mockProxyRepoForOAuth{}, &mockClaudeOAuthClient{})
	defer svc.Stop()

	result, err := svc.GenerateAuthURL(accountAdminOAuthContext(41), nil)
	require.NoError(t, err)
	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Equal(t, int64(41), session.AccountAdminID)

	_, err = svc.ExchangeCode(accountAdminOAuthContext(42), &ExchangeCodeInput{SessionID: result.SessionID, Code: "code"})
	require.Equal(t, "OAUTH_SESSION_SCOPE", infraerrors.Reason(err))
}

func TestOpenAIOAuthServiceBindsSessionToAccountAdmin(t *testing.T) {
	svc := NewOpenAIOAuthService(nil, &openaiOAuthClientStateStub{})
	defer svc.Stop()

	result, err := svc.GenerateAuthURL(accountAdminOAuthContext(41), nil, "", PlatformOpenAI)
	require.NoError(t, err)
	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Equal(t, int64(41), session.AccountAdminID)

	_, err = svc.ExchangeCode(accountAdminOAuthContext(42), &OpenAIExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "code",
		State:     session.State,
	})
	require.Equal(t, "OAUTH_SESSION_SCOPE", infraerrors.Reason(err))
}

func TestGrokOAuthServiceBindsSessionToAccountAdmin(t *testing.T) {
	svc := NewGrokOAuthService(nil, nil)
	defer svc.Stop()

	result, err := svc.GenerateAuthURL(accountAdminOAuthContext(41), nil, "")
	require.NoError(t, err)
	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Equal(t, int64(41), session.AccountAdminID)

	_, err = svc.ExchangeCode(accountAdminOAuthContext(42), &GrokExchangeCodeInput{
		SessionID: result.SessionID,
		Code:      "code",
		State:     session.State,
	})
	require.Equal(t, "OAUTH_SESSION_SCOPE", infraerrors.Reason(err))
}
