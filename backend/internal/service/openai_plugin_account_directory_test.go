package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type pluginAccountDirectoryRepo struct {
	AccountRepository
	account *Account
}

func (r *pluginAccountDirectoryRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func TestResolvePluginOutboundIdentityRejectsInactiveAccounts(t *testing.T) {
	for _, status := range []string{StatusDisabled, StatusError, StatusExpired} {
		t.Run(status, func(t *testing.T) {
			repo := &pluginAccountDirectoryRepo{account: &Account{
				ID:       42,
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Status:   status,
				Credentials: map[string]any{
					"access_token": "must-not-leak",
				},
			}}
			svc := &OpenAIGatewayService{accountRepo: repo}

			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), repo.account.ID)
			require.NoError(t, err)
			require.Nil(t, identity)
		})
	}
}
