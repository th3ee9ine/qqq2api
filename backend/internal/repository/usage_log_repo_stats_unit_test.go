package repository

import "testing"

// A retained usage row can be written directly to a partition that was
// created before its child trigger was installed.  In that case the raw
// aggregate is temporarily larger than the durable bucket.  Reconciliation
// must still return the exact in-range retained subset rather than clamping it
// to zero.
func TestCombineAccountDailyUsageStatsUsesRawLowerBound(t *testing.T) {
	raw := accountDailyRawUsageStats{
		All: accountDailyUsageStats{
			Requests:            5,
			InputTokens:         50,
			OutputTokens:        25,
			CacheCreationTokens: 10,
			CacheReadTokens:     15,
			StandardCost:        2.5,
			AccountCost:         3.75,
			UserCost:            2.0,
			TotalDurationMs:     500,
			DurationCount:       4,
		},
		InRange: accountDailyUsageStats{
			Requests:            3,
			InputTokens:         30,
			OutputTokens:        15,
			CacheCreationTokens: 6,
			CacheReadTokens:     9,
			StandardCost:        1.5,
			AccountCost:         2.25,
			UserCost:            1.2,
			TotalDurationMs:     300,
			DurationCount:       2,
		},
	}

	// Every durable value is below raw.All, simulating an untriggered direct
	// child insert.  The result should be raw.InRange for every metric.
	got := combineAccountDailyUsageStats(accountDailyUsageStats{
		Requests:            1,
		InputTokens:         10,
		OutputTokens:        5,
		CacheCreationTokens: 2,
		CacheReadTokens:     3,
		StandardCost:        0.5,
		AccountCost:         0.75,
		UserCost:            0.4,
		TotalDurationMs:     100,
		DurationCount:       1,
	}, raw)

	if got != raw.InRange {
		t.Fatalf("reconciled stats = %#v, want %#v", got, raw.InRange)
	}
}

func TestCombineAccountDailyUsageStatsPreservesPurgedContribution(t *testing.T) {
	raw := accountDailyRawUsageStats{
		All:     accountDailyUsageStats{Requests: 2, InputTokens: 20, StandardCost: 1.0},
		InRange: accountDailyUsageStats{Requests: 1, InputTokens: 10, StandardCost: 0.5},
	}
	durable := accountDailyUsageStats{Requests: 5, InputTokens: 50, StandardCost: 2.5}

	got := combineAccountDailyUsageStats(durable, raw)
	want := accountDailyUsageStats{Requests: 4, InputTokens: 40, StandardCost: 2.0}
	if got != want {
		t.Fatalf("reconciled stats = %#v, want %#v", got, want)
	}
}
