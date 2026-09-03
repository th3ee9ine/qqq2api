//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

func TestResolveOpenAINonCurrentSessionRevokeConfigDefaultsAndEligibility(t *testing.T) {
	parentID := int64(91)
	tests := []struct {
		name            string
		account         *Account
		wantEnabled     bool
		wantIntervalMin int
	}{
		{
			name:            "nil account",
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
		{
			name: "missing settings are disabled",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
			},
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
		{
			name: "canonical settings",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey:         true,
					OpenAISessionCleanupIntervalMinutesExtraKey: json.Number("15"),
				},
			},
			wantEnabled:     true,
			wantIntervalMin: 15,
		},
		{
			name: "legacy settings remain readable",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey:         true,
					OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey: "30",
				},
			},
			wantEnabled:     true,
			wantIntervalMin: 30,
		},
		{
			name: "valid interval remains visible while disabled",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey:         false,
					OpenAISessionCleanupIntervalMinutesExtraKey: 30,
				},
			},
			wantIntervalMin: 30,
		},
		{
			name: "malformed enabled value fails closed",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey:         "true",
					OpenAISessionCleanupIntervalMinutesExtraKey: 30,
				},
			},
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
		{
			name: "malformed interval fails closed instead of using default while enabled",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey:         true,
					OpenAISessionCleanupIntervalMinutesExtraKey: "not-a-number",
				},
			},
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
		{
			name: "out of range interval fails closed",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey:         true,
					OpenAISessionCleanupIntervalMinutesExtraKey: 1,
				},
			},
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
		{
			name: "api key is ineligible",
			account: &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey: true,
				},
			},
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
		{
			name: "other platform is ineligible",
			account: &Account{
				Platform: PlatformAnthropic,
				Type:     AccountTypeOAuth,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey: true,
				},
			},
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
		{
			name: "shadow account is ineligible",
			account: &Account{
				Platform:        PlatformOpenAI,
				Type:            AccountTypeOAuth,
				ParentAccountID: &parentID,
				Extra: map[string]any{
					OpenAISessionCleanupEnabledExtraKey: true,
				},
			},
			wantIntervalMin: OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveOpenAINonCurrentSessionRevokeConfig(tt.account)
			require.Equal(t, tt.wantEnabled, got.Enabled)
			require.Equal(t, tt.wantIntervalMin, got.IntervalMinutes)
		})
	}
}

func TestNormalizeOpenAINonCurrentSessionRevokeExtraDefaultsAndCanonicalizesLegacy(t *testing.T) {
	source := map[string]any{
		"unrelated": "keep",
		OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey: true,
		OpenAISessionCleanupStateExtraKey: map[string]any{
			"status": "forged",
		},
	}

	got, err := NormalizeOpenAINonCurrentSessionRevokeExtra(PlatformOpenAI, AccountTypeOAuth, false, source)

	require.NoError(t, err)
	require.Equal(t, "keep", got["unrelated"])
	require.Equal(t, true, got[OpenAISessionCleanupEnabledExtraKey])
	require.Equal(t, OpenAIAutoRevokeNonCurrentSessionsDefaultIntervalMinutes, got[OpenAISessionCleanupIntervalMinutesExtraKey])
	require.NotContains(t, got, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
	require.NotContains(t, got, OpenAIAutoRevokeNonCurrentSessionsIntervalMinutesExtraKey)
	require.NotContains(t, got, OpenAISessionCleanupStateExtraKey)

	// Normalization must not mutate the caller-owned map before the account
	// write succeeds.
	require.Contains(t, source, OpenAIAutoRevokeNonCurrentSessionsEnabledExtraKey)
	require.Contains(t, source, OpenAISessionCleanupStateExtraKey)
}

func TestNormalizeOpenAINonCurrentSessionRevokeExtraValidatesInterval(t *testing.T) {
	tests := []struct {
		name  string
		value any
		ok    bool
	}{
		{name: "minimum", value: OpenAIAutoRevokeNonCurrentSessionsMinIntervalMinutes, ok: true},
		{name: "maximum", value: OpenAIAutoRevokeNonCurrentSessionsMaxIntervalMinutes, ok: true},
		{name: "numeric string", value: "60", ok: true},
		{name: "integral json number", value: json.Number("120"), ok: true},
		{name: "below minimum", value: OpenAIAutoRevokeNonCurrentSessionsMinIntervalMinutes - 1},
		{name: "above maximum", value: OpenAIAutoRevokeNonCurrentSessionsMaxIntervalMinutes + 1},
		{name: "fractional", value: 5.5},
		{name: "wrong type", value: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeOpenAINonCurrentSessionRevokeExtra(
				PlatformOpenAI,
				AccountTypeOAuth,
				false,
				map[string]any{
					OpenAISessionCleanupEnabledExtraKey:         true,
					OpenAISessionCleanupIntervalMinutesExtraKey: tt.value,
				},
			)
			if tt.ok {
				require.NoError(t, err)
				value, parsed := parseOpenAINonCurrentSessionInterval(got[OpenAISessionCleanupIntervalMinutesExtraKey])
				require.True(t, parsed)
				require.Equal(t, mustParseCleanupInterval(t, tt.value), value)
				return
			}
			require.Nil(t, got)
			require.Equal(t, "OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_INTERVAL_INVALID", infraerrors.Reason(err))
		})
	}
}

func TestNormalizeOpenAINonCurrentSessionRevokeExtraRejectsWrongAccountAndEnabledType(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform string
		typeName string
		shadow   bool
	}{
		{name: "wrong platform", platform: PlatformAnthropic, typeName: AccountTypeOAuth},
		{name: "wrong type", platform: PlatformOpenAI, typeName: AccountTypeAPIKey},
		{name: "shadow", platform: PlatformOpenAI, typeName: AccountTypeOAuth, shadow: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeOpenAINonCurrentSessionRevokeExtra(tt.platform, tt.typeName, tt.shadow, map[string]any{
				OpenAISessionCleanupEnabledExtraKey: true,
			})
			require.Nil(t, got)
			require.Equal(t, "OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_ACCOUNT_INVALID", infraerrors.Reason(err))
		})
	}

	got, err := NormalizeOpenAINonCurrentSessionRevokeExtra(PlatformOpenAI, AccountTypeOAuth, false, map[string]any{
		OpenAISessionCleanupEnabledExtraKey: "true",
	})
	require.Nil(t, got)
	require.Equal(t, "OPENAI_AUTO_REVOKE_NON_CURRENT_SESSIONS_ENABLED_INVALID", infraerrors.Reason(err))
}

func mustParseCleanupInterval(t *testing.T, value any) int {
	t.Helper()
	parsed, ok := parseOpenAINonCurrentSessionInterval(value)
	require.True(t, ok)
	return parsed
}
