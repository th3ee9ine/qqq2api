//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/geminicli"
)

func TestGeminiOAuthServiceBindsSessionToAccountAdmin(t *testing.T) {
	t.Parallel()

	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()
	svc.sessionStore.Set("gemini-session", &geminicli.OAuthSession{
		State:          "state",
		CodeVerifier:   "verifier",
		AccountAdminID: 41,
		CreatedAt:      time.Now(),
	})

	_, err := svc.ExchangeCode(accountAdminOAuthContext(42), &GeminiExchangeCodeInput{
		SessionID: "gemini-session",
		State:     "state",
		Code:      "code",
	})
	require.Equal(t, "OAUTH_SESSION_SCOPE", infraerrors.Reason(err))
}

func TestAntigravityOAuthServiceBindsSessionToAccountAdmin(t *testing.T) {
	t.Parallel()

	svc := NewAntigravityOAuthService(nil)
	defer svc.Stop()
	result, err := svc.GenerateAuthURL(accountAdminOAuthContext(41), nil)
	require.NoError(t, err)
	session, ok := svc.sessionStore.Get(result.SessionID)
	require.True(t, ok)
	require.Equal(t, int64(41), session.AccountAdminID)

	_, err = svc.ExchangeCode(accountAdminOAuthContext(42), &AntigravityExchangeCodeInput{
		SessionID: result.SessionID,
		State:     session.State,
		Code:      "code",
	})
	require.Equal(t, "OAUTH_SESSION_SCOPE", infraerrors.Reason(err))
}

func TestGeminiOAuthServiceAllowsLegacySessionForUnscopedCaller(t *testing.T) {
	// A zero owner is retained for sessions created by super-admin/background
	// flows before the owner binding was introduced. The restricted context must
	// still reject it; this assertion covers the unscoped side of the contract.
	svc := NewGeminiOAuthService(nil, nil, nil, nil, &config.Config{})
	defer svc.Stop()
	svc.sessionStore.Set("legacy-session", &geminicli.OAuthSession{
		State:        "state",
		CodeVerifier: "verifier",
		CreatedAt:    time.Now(),
	})

	_, err := svc.ExchangeCode(context.Background(), &GeminiExchangeCodeInput{
		SessionID: "legacy-session",
		State:     "wrong-state",
		Code:      "code",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid state")
}
