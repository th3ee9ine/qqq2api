//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// newBalanceNotifyServiceForTest constructs a BalanceNotifyService with an
// in-memory settings repo and a non-nil emailService so that the guard-clause
// nil-checks pass. The emailService is intentionally minimal — tests must
// avoid crossing scenarios that would actually dispatch emails.
func newBalanceNotifyServiceForTest() (*BalanceNotifyService, *mockSettingRepo) {
	repo := newMockSettingRepo()
	// EmailService is a concrete type; construct with the same repo so that
	// any accidental fallback reads still succeed. Tests should not trigger a
	// crossing that reaches SendEmail.
	email := NewEmailService(repo)
	return NewBalanceNotifyService(email, repo, nil), repo
}

// ---------- nil-service guards on CheckAccountQuotaAfterIncrement ----------

func TestCheckAccountQuotaAfterIncrement_NilAccount(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	// Should not panic.
	s.CheckAccountQuotaAfterIncrement(context.Background(), nil, 10, nil)
}

func TestCheckAccountQuotaAfterIncrement_ZeroCost(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	a := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	s.CheckAccountQuotaAfterIncrement(context.Background(), a, 0, nil)
}

func TestCheckAccountQuotaAfterIncrement_NegativeCost(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	a := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
	s.CheckAccountQuotaAfterIncrement(context.Background(), a, -5, nil)
}

func TestCheckAccountQuotaAfterIncrement_GlobalDisabled(t *testing.T) {
	s, repo := newBalanceNotifyServiceForTest()
	repo.data[SettingKeyAccountQuotaNotifyEnabled] = "false"
	a := &Account{
		ID:       1,
		Platform: PlatformAnthropic,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"quota_notify_daily_enabled":   true,
			"quota_notify_daily_threshold": 100.0,
			"quota_daily_limit":            1000.0,
			"quota_daily_used":             950.0,
		},
	}
	// Global disabled → no processing even if a dim would cross.
	s.CheckAccountQuotaAfterIncrement(context.Background(), a, 100, nil)
}

// ---------- sanity: internal helpers still work ----------

func TestIsAccountQuotaNotifyEnabled(t *testing.T) {
	s, repo := newBalanceNotifyServiceForTest()

	// Missing key → false
	require.False(t, s.isAccountQuotaNotifyEnabled(context.Background()))

	// Explicit "false"
	repo.data[SettingKeyAccountQuotaNotifyEnabled] = "false"
	require.False(t, s.isAccountQuotaNotifyEnabled(context.Background()))

	// Explicit "true"
	repo.data[SettingKeyAccountQuotaNotifyEnabled] = "true"
	require.True(t, s.isAccountQuotaNotifyEnabled(context.Background()))
}

func TestGetSiteName_FallsBackToDefault(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	name := s.getSiteName(context.Background())
	require.Equal(t, defaultSiteName, name)
}

func TestGetSiteName_Configured(t *testing.T) {
	s, repo := newBalanceNotifyServiceForTest()
	repo.data[SettingKeySiteName] = "My Site"
	require.Equal(t, "My Site", s.getSiteName(context.Background()))
}

// ---------- checkQuotaDimCrossings ----------

func TestCheckQuotaDimCrossings_NoDimensions(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	// Empty dims → no crossing, no panic.
	s.checkQuotaDimCrossings(account, nil, 10, []string{"admin@example.com"}, "TestSite")
	s.checkQuotaDimCrossings(account, []quotaDim{}, 10, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_DisabledDimension(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	dims := []quotaDim{
		{
			name:          quotaDimDaily,
			enabled:       false, // disabled
			threshold:     100,
			thresholdType: thresholdTypeFixed,
			currentUsed:   950,
			limit:         1000,
		},
	}
	// Disabled dimension should be skipped even if crossing would occur.
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_ZeroThresholdSkipped(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	dims := []quotaDim{
		{
			name:          quotaDimDaily,
			enabled:       true,
			threshold:     0, // zero threshold
			thresholdType: thresholdTypeFixed,
			currentUsed:   950,
			limit:         1000,
		},
	}
	// Zero threshold → skipped.
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_NoCrossing_BothBelowThreshold(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	// threshold=400 remaining, limit=1000 → effectiveThreshold = 600 (usage trigger)
	// currentUsed=300 (after), oldUsed=300-50=250 (before). Both < 600, no crossing.
	dims := []quotaDim{
		{
			name:          quotaDimDaily,
			enabled:       true,
			threshold:     400,
			thresholdType: thresholdTypeFixed,
			currentUsed:   300,
			limit:         1000,
		},
	}
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_NoCrossing_BothAboveThreshold(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	// threshold=400 remaining, limit=1000 → effectiveThreshold = 600 (usage trigger)
	// currentUsed=800 (after), oldUsed=800-50=750 (before). Both >= 600, no crossing.
	dims := []quotaDim{
		{
			name:          quotaDimDaily,
			enabled:       true,
			threshold:     400,
			thresholdType: thresholdTypeFixed,
			currentUsed:   800,
			limit:         1000,
		},
	}
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_NegativeResolvedThreshold_Skipped(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	// threshold=1200 remaining, limit=1000 → effectiveThreshold = 1000-1200 = -200
	// Negative resolved threshold → skipped.
	dims := []quotaDim{
		{
			name:          quotaDimDaily,
			enabled:       true,
			threshold:     1200,
			thresholdType: thresholdTypeFixed,
			currentUsed:   950,
			limit:         1000,
		},
	}
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_PercentageThreshold_NoCrossing(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	// threshold=30%, limit=1000 → effectiveThreshold = 1000 * (1 - 0.30) = 700
	// currentUsed=500, oldUsed=500-50=450. Both < 700, no crossing.
	dims := []quotaDim{
		{
			name:          quotaDimWeekly,
			enabled:       true,
			threshold:     30,
			thresholdType: thresholdTypePercentage,
			currentUsed:   500,
			limit:         1000,
		},
	}
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_ZeroLimit_Skipped(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	// limit=0 → resolvedThreshold returns 0 → skipped.
	dims := []quotaDim{
		{
			name:          quotaDimTotal,
			enabled:       true,
			threshold:     100,
			thresholdType: thresholdTypeFixed,
			currentUsed:   50,
			limit:         0,
		},
	}
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}

func TestCheckQuotaDimCrossings_MultipleDims_MixedResults(t *testing.T) {
	s, _ := newBalanceNotifyServiceForTest()
	account := &Account{ID: 1, Name: "test", Platform: PlatformAnthropic}
	// dim1: no crossing (both below effective threshold)
	// dim2: disabled (skipped)
	// dim3: zero threshold (skipped)
	dims := []quotaDim{
		{
			name:          quotaDimDaily,
			enabled:       true,
			threshold:     400,
			thresholdType: thresholdTypeFixed,
			currentUsed:   300, // oldUsed=250, effectiveThreshold=600, both below
			limit:         1000,
		},
		{
			name:          quotaDimWeekly,
			enabled:       false,
			threshold:     100,
			thresholdType: thresholdTypeFixed,
			currentUsed:   900,
			limit:         1000,
		},
		{
			name:          quotaDimTotal,
			enabled:       true,
			threshold:     0,
			thresholdType: thresholdTypeFixed,
			currentUsed:   500,
			limit:         1000,
		},
	}
	// None should trigger. No panic expected.
	s.checkQuotaDimCrossings(account, dims, 50, []string{"admin@example.com"}, "TestSite")
}
