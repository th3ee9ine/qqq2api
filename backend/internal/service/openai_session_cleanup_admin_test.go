//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

func TestAdminServiceUpdateAccountExtraPersistsOpenAISessionCleanupConfig(t *testing.T) {
	accountID := int64(801)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra: map[string]any{
				"unrelated": "keep",
			},
		},
	}}

	err := (&adminServiceImpl{accountRepo: repo}).UpdateAccountExtra(context.Background(), accountID, map[string]any{
		OpenAISessionCleanupEnabledExtraKey:         true,
		OpenAISessionCleanupIntervalMinutesExtraKey: 30,
		OpenAISessionCleanupStateExtraKey:           map[string]any{"status": "forged"},
	})

	require.NoError(t, err)
	require.Equal(t, true, repo.accounts[accountID].Extra[OpenAISessionCleanupEnabledExtraKey])
	require.Equal(t, 30, repo.accounts[accountID].Extra[OpenAISessionCleanupIntervalMinutesExtraKey])
	require.NotContains(t, repo.accounts[accountID].Extra, OpenAISessionCleanupStateExtraKey)
	require.Len(t, repo.updates[accountID], 1)
	require.Equal(t, true, repo.updates[accountID][0][OpenAISessionCleanupEnabledExtraKey])
	require.Equal(t, 30, repo.updates[accountID][0][OpenAISessionCleanupIntervalMinutesExtraKey])
	require.NotContains(t, repo.updates[accountID][0], OpenAISessionCleanupStateExtraKey)
}

func TestAdminServiceUpdateAccountExtraDefaultsIntervalWhenEnablingCleanup(t *testing.T) {
	accountID := int64(802)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra:    map[string]any{},
		},
	}}

	err := (&adminServiceImpl{accountRepo: repo}).UpdateAccountExtra(context.Background(), accountID, map[string]any{
		OpenAISessionCleanupEnabledExtraKey: true,
	})

	require.NoError(t, err)
	require.Equal(t, OpenAISessionCleanupDefaultIntervalMinutes, repo.updates[accountID][0][OpenAISessionCleanupIntervalMinutesExtraKey])
}

func TestAdminServiceUpdateAccountExtraPreservesExistingIntervalWhenOnlyEnabling(t *testing.T) {
	accountID := int64(8021)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra: map[string]any{
				OpenAISessionCleanupEnabledExtraKey:         false,
				OpenAISessionCleanupIntervalMinutesExtraKey: 15,
			},
		},
	}}

	err := (&adminServiceImpl{accountRepo: repo}).UpdateAccountExtra(context.Background(), accountID, map[string]any{
		OpenAISessionCleanupEnabledExtraKey: true,
	})

	require.NoError(t, err)
	require.Len(t, repo.updates[accountID], 1)
	require.Equal(t, true, repo.updates[accountID][0][OpenAISessionCleanupEnabledExtraKey])
	require.Equal(t, 15, repo.updates[accountID][0][OpenAISessionCleanupIntervalMinutesExtraKey])
}

func TestAdminServiceUpdateAccountExtraPreservesExistingLegacyIntervalWhenOnlyEnabling(t *testing.T) {
	accountID := int64(8022)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra: map[string]any{
				OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey:         false,
				OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey: 25,
			},
		},
	}}

	err := (&adminServiceImpl{accountRepo: repo}).UpdateAccountExtra(context.Background(), accountID, map[string]any{
		OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey: true,
	})

	require.NoError(t, err)
	require.Len(t, repo.updates[accountID], 1)
	require.Equal(t, true, repo.updates[accountID][0][OpenAISessionCleanupEnabledExtraKey])
	require.Equal(t, 25, repo.updates[accountID][0][OpenAISessionCleanupIntervalMinutesExtraKey])
	require.NotContains(t, repo.updates[accountID][0], OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
	require.NotContains(t, repo.updates[accountID][0], OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey)
}

func TestAdminServiceUpdateAccountExtraRejectsMalformedOpenAISessionCleanupConfig(t *testing.T) {
	accountID := int64(803)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra:    map[string]any{},
		},
	}}

	err := (&adminServiceImpl{accountRepo: repo}).UpdateAccountExtra(context.Background(), accountID, map[string]any{
		OpenAISessionCleanupEnabledExtraKey:         true,
		OpenAISessionCleanupIntervalMinutesExtraKey: 4,
	})

	require.Equal(t, "OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_INTERVAL_INVALID", infraerrors.Reason(err))
	require.Empty(t, repo.updates)
	require.NotContains(t, repo.accounts[accountID].Extra, OpenAISessionCleanupEnabledExtraKey)
}

func TestAdminServiceUpdateAccountPreservesOpenAISessionCleanupConfigWhenOmitted(t *testing.T) {
	accountID := int64(804)
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra: map[string]any{
				OpenAISessionCleanupEnabledExtraKey:         true,
				OpenAISessionCleanupIntervalMinutesExtraKey: 25,
				"unrelated": "updated",
			},
		},
	}}

	updated, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Extra: map[string]any{"unrelated": "updated"},
	})

	require.NoError(t, err)
	require.Equal(t, true, updated.Extra[OpenAISessionCleanupEnabledExtraKey])
	require.Equal(t, 25, updated.Extra[OpenAISessionCleanupIntervalMinutesExtraKey])
	require.Equal(t, "updated", updated.Extra["unrelated"])
}

type openAISessionCleanupTypeMigrationRepo struct {
	*upstreamBillingProbeAccountRepo
}

func (r *openAISessionCleanupTypeMigrationRepo) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	return nil, nil
}

func TestAdminServiceUpdateAccountDropsCleanupConfigWhenMigratingAwayFromOAuth(t *testing.T) {
	accountID := int64(805)
	repo := &openAISessionCleanupTypeMigrationRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		accountID: {
			ID:       accountID,
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Status:   StatusActive,
			Extra: map[string]any{
				OpenAISessionCleanupEnabledExtraKey:         true,
				OpenAISessionCleanupIntervalMinutesExtraKey: 25,
				OpenAISessionCleanupStateExtraKey:           map[string]any{"status": OpenAISessionCleanupStatusSuccess},
			},
		},
	}}}

	updated, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), accountID, &UpdateAccountInput{
		Type:  AccountTypeAPIKey,
		Extra: map[string]any{"unrelated": "keep"},
	})

	require.NoError(t, err)
	require.Equal(t, AccountTypeAPIKey, updated.Type)
	require.NotContains(t, updated.Extra, OpenAISessionCleanupEnabledExtraKey)
	require.NotContains(t, updated.Extra, OpenAISessionCleanupIntervalMinutesExtraKey)
	require.NotContains(t, updated.Extra, OpenAISessionCleanupStateExtraKey)
}
