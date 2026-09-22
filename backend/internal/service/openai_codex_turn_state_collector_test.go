package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func collectorTestToken(t *testing.T, issuedAt time.Time, blocks int, marker byte) string {
	t.Helper()
	if blocks < 1 {
		blocks = 1
	}
	raw := make([]byte, 57+16*blocks)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issuedAt.Unix()))
	raw[9] = marker
	return base64.RawURLEncoding.EncodeToString(raw)
}

func collectorTestPolicy() OpenAICodexTurnStatePolicy {
	return OpenAICodexTurnStatePolicy{
		Blocks:        2,
		TTL:           10 * time.Minute,
		RefreshBefore: 2 * time.Minute,
		ClockSkew:     time.Second,
		ProbeCooldown: 3 * time.Second,
		MaxEntries:    8,
	}
}

func TestParseOpenAICodexTurnStateValidatesOpaqueEnvelope(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	value := collectorTestToken(t, now.Add(-time.Minute), 2, 7)
	token, err := ParseOpenAICodexTurnState(value)
	require.NoError(t, err)
	require.Equal(t, value, token.Value)
	require.NotEmpty(t, token.Fingerprint)
	require.True(t, now.Add(-time.Minute).Equal(token.IssuedAt))
	require.Equal(t, 2, token.Blocks)

	invalid := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "embedded whitespace", value: value[:8] + " " + value[8:]},
		{name: "control", value: value[:8] + "\r" + value[8:]},
		{name: "bad base64", value: "%%%"},
		{name: "short envelope", value: "AA"},
		{name: "wrong block shape", value: collectorTestToken(t, now, 1, 1)[:10]},
		{name: "old timestamp", value: collectorTestToken(t, time.Unix(1, 0), 2, 1)},
		{name: "future timestamp", value: collectorTestToken(t, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC), 2, 1)},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseOpenAICodexTurnState(test.value)
			require.Error(t, err)
		})
	}

	tooLong := strings.Repeat("A", 2049)
	_, err = ParseOpenAICodexTurnState(tooLong)
	require.ErrorIs(t, err, ErrOpenAICodexTurnStateEncoding)
}

func TestOpenAICodexTurnStateCollectorKeepsActiveUntilRefresh(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 42, Scope: " scope-a ", Model: "GPT-5"}
	first := collectorTestToken(t, base.Add(-time.Minute), 2, 1)
	second := collectorTestToken(t, base, 2, 2)

	_, accepted, err := collector.OfferValue(key, first, "route-a", base)
	require.NoError(t, err)
	require.True(t, accepted)
	active, usable := collector.Acquire(key, base)
	require.True(t, usable)
	require.Equal(t, first, active.Token.Value)
	require.Equal(t, "scope-a", activeKeyScope(collector.Status(key, base)))
	require.Equal(t, "gpt-5", collector.Status(key, base).Key.Model)

	_, accepted, err = collector.OfferValue(key, second, "route-b", base)
	require.NoError(t, err)
	require.True(t, accepted)
	stillActive, usable := collector.Acquire(key, base.Add(1*time.Minute))
	require.True(t, usable)
	require.Equal(t, first, stillActive.Token.Value, "a healthy active value must not be replaced by observation")
	status := collector.Status(key, base.Add(time.Minute))
	require.True(t, status.Ready)
	require.NotEmpty(t, status.ReadyFingerprint)

	// The active value is within its refresh window at this point. The newer
	// standby candidate can now be promoted without changing the request
	// snapshot already handed to an in-flight attempt.
	promoted, usable := collector.Acquire(key, base.Add(9*time.Minute))
	require.True(t, usable)
	require.Equal(t, second, promoted.Token.Value)
	require.Greater(t, promoted.Version, active.Version)
	oldSnapshot := active
	require.Equal(t, first, oldSnapshot.Token.Value)
}

func TestOpenAICodexTurnStateCollectorCookieLifetimeBoundary(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 510, Scope: "cookie-boundary", Model: "gpt-5"}
	value := collectorTestToken(t, base, 2, 51)
	token, err := ParseOpenAICodexTurnState(value)
	require.NoError(t, err)
	require.True(t, collector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
		Token: token, Route: "probe", HarvestSessionID: "collector-session",
		HarvestCookies:   []OpenAICodexTurnStateCookie{{Name: "session", Value: "opaque"}},
		HarvestCookiesAt: base,
	}, base))

	snapshot, usable := collector.Acquire(key, base)
	require.True(t, usable)
	at239 := base.Add(239 * time.Second)
	require.True(t, snapshot.cookiesFresh(at239))
	status := collector.Status(key, at239)
	require.Equal(t, 1, status.CookieCount)
	require.Equal(t, 1, status.CookieRemainingSeconds)
	require.False(t, status.CookieExpired)
	headers := make(http.Header)
	applyOpenAICodexTurnStateTicketHeaders(headers, snapshot, at239)
	require.Equal(t, "session=opaque", headers.Get("Cookie"))
	justBeforeExpiry := base.Add(240*time.Second - time.Nanosecond)
	status = collector.Status(key, justBeforeExpiry)
	require.False(t, status.CookieExpired)
	require.Equal(t, 1, status.CookieRemainingSeconds)
	applyOpenAICodexTurnStateTicketHeaders(headers, snapshot, justBeforeExpiry)
	require.Equal(t, "session=opaque", headers.Get("Cookie"))

	at240 := base.Add(240 * time.Second)
	require.False(t, snapshot.cookiesFresh(at240))
	status = collector.Status(key, at240)
	require.Equal(t, 1, status.CookieCount, "expired cookies may remain in the snapshot for diagnostics")
	require.Zero(t, status.CookieRemainingSeconds)
	require.True(t, status.CookieExpired)
	applyOpenAICodexTurnStateTicketHeaders(headers, snapshot, at240)
	require.Empty(t, headers.Get("Cookie"), "expired cookies must never be replayed")
}

func TestOpenAICodexTurnStateCollectorQualifiedRotationKeepsCookieClockWithoutUpdates(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 511, Scope: "qualified-response", Model: "gpt-5"}
	first := collectorTestToken(t, base, 2, 52)
	second := collectorTestToken(t, base.Add(time.Second), 2, 53)
	token, err := ParseOpenAICodexTurnState(first)
	require.NoError(t, err)
	require.True(t, collector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
		Token: token, Route: "probe", EgressProxyURL: "http://proxy.example:8080", EgressPinned: true,
		HarvestSessionID: "collector-session",
		HarvestCookies:   []OpenAICodexTurnStateCookie{{Name: "session", Value: "first"}},
		HarvestCookiesAt: base,
	}, base))
	used, usable := collector.Acquire(key, base)
	require.True(t, usable)
	require.False(t, collector.ObserveQualified(key, "malformed", used,
		[]OpenAICodexTurnStateCookie{{Name: "session", Value: "untrusted"}}, base.Add(time.Second)))
	require.False(t, collector.ObserveQualified(key, second,
		OpenAICodexTurnStateSnapshot{Token: used.Token, Route: "client", Version: used.Version},
		[]OpenAICodexTurnStateCookie{{Name: "session", Value: "client"}}, base.Add(time.Second)))
	unchanged, usable := collector.Acquire(key, base.Add(time.Second))
	require.True(t, usable)
	require.Equal(t, first, unchanged.Token.Value)
	require.Equal(t, base, unchanged.HarvestCookiesAt)

	require.True(t, collector.ObserveQualified(key, second, used, nil, base.Add(2*time.Second)))
	rotated, usable := collector.Acquire(key, base.Add(2*time.Second))
	require.True(t, usable)
	require.Equal(t, second, rotated.Token.Value)
	require.Greater(t, rotated.Version, used.Version)
	require.Equal(t, base, rotated.HarvestCookiesAt, "a state-only response cannot extend cookie freshness")
	require.Equal(t, used.HarvestCookies, rotated.HarvestCookies)
	require.Equal(t, used.HarvestSessionID, rotated.HarvestSessionID)
	require.Equal(t, used.EgressProxyURL, rotated.EgressProxyURL)
	require.True(t, rotated.EgressPinned)

	third := collectorTestToken(t, base.Add(3*time.Second), 2, 54)
	require.True(t, collector.ObserveQualified(key, third, rotated,
		[]OpenAICodexTurnStateCookie{{Name: "session", Value: "refreshed"}}, base.Add(3*time.Second)))
	refreshed, usable := collector.Acquire(key, base.Add(3*time.Second))
	require.True(t, usable)
	require.Equal(t, base.Add(3*time.Second), refreshed.HarvestCookiesAt)
	require.Equal(t, []OpenAICodexTurnStateCookie{{Name: "session", Value: "refreshed"}}, refreshed.HarvestCookies)
}

func TestOpenAICodexTurnStateCollectorQualifiedRotationRejectsStaleConcurrentResponse(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 512, Scope: "concurrent-response", Model: "gpt-5"}
	first := collectorTestToken(t, base, 2, 55)
	require.True(t, collector.OfferValueMust(key, first, "probe", base))
	oldA, usable := collector.Acquire(key, base)
	require.True(t, usable)
	oldB, usable := collector.Acquire(key, base)
	require.True(t, usable)
	newA := collectorTestToken(t, base.Add(time.Second), 2, 56)
	newB := collectorTestToken(t, base.Add(2*time.Second), 2, 57)
	require.True(t, collector.ObserveQualified(key, newA, oldA, nil, base.Add(time.Second)))
	require.False(t, collector.ObserveQualified(key, newB, oldB,
		[]OpenAICodexTurnStateCookie{{Name: "session", Value: "stale"}}, base.Add(2*time.Second)),
		"a second in-flight response cannot replace the first winner or refresh its cookies")
	active, usable := collector.Acquire(key, base.Add(2*time.Second))
	require.True(t, usable)
	require.Equal(t, newA, active.Token.Value)
	require.Equal(t, oldA.Version+1, active.Version)
	require.Empty(t, active.HarvestCookies)
	require.Zero(t, collector.Status(key, base.Add(2*time.Second)).CookieCount)
}

func TestOpenAICodexTurnStateCollectorQualifiedRotationDoesNotResurrectExpiredCookies(t *testing.T) {
	base := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 513, Scope: "expired-cookie-response", Model: "gpt-5"}
	first := collectorTestToken(t, base, 2, 58)
	token, err := ParseOpenAICodexTurnState(first)
	require.NoError(t, err)
	require.True(t, collector.OfferSnapshot(key, OpenAICodexTurnStateSnapshot{
		Token: token, Route: "probe", HarvestSessionID: "collector-session",
		HarvestCookies: []OpenAICodexTurnStateCookie{
			{Name: "session", Value: "expired-session"},
			{Name: "region", Value: "expired-region"},
		},
		HarvestCookiesAt: base,
	}, base))
	responseAt := base.Add(241 * time.Second)
	used, usable := collector.Acquire(key, responseAt)
	require.True(t, usable)
	require.False(t, used.cookiesFresh(responseAt))
	second := collectorTestToken(t, responseAt, 2, 59)
	require.True(t, collector.ObserveQualified(key, second, used,
		[]OpenAICodexTurnStateCookie{{Name: "region", Value: "new-region"}}, responseAt))
	rotated, usable := collector.Acquire(key, responseAt)
	require.True(t, usable)
	require.Equal(t, []OpenAICodexTurnStateCookie{{Name: "region", Value: "new-region"}}, rotated.HarvestCookies)
	require.Equal(t, responseAt, rotated.HarvestCookiesAt)
	headers := make(http.Header)
	applyOpenAICodexTurnStateTicketHeaders(headers, rotated, responseAt)
	require.Equal(t, "region=new-region", headers.Get("Cookie"))
}

func TestOpenAICodexTurnStateCollectorOfferIfRefreshNeededConcurrentInitial(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 43, Scope: "execution", Model: "gpt-5"}
	values := []string{
		collectorTestToken(t, base, 2, 11),
		collectorTestToken(t, base, 2, 12),
	}
	tokens := make([]OpenAICodexTurnStateToken, len(values))
	for i, value := range values {
		var err error
		tokens[i], err = ParseOpenAICodexTurnState(value)
		require.NoError(t, err)
	}

	start := make(chan struct{})
	results := make(chan OpenAICodexTurnStateOfferResult, len(tokens))
	var wg sync.WaitGroup
	for i, token := range tokens {
		wg.Add(1)
		go func(route string, token OpenAICodexTurnStateToken) {
			defer wg.Done()
			<-start
			results <- collector.OfferIfRefreshNeeded(key, token, route, base)
		}("response-"+string(rune('a'+i)), token)
	}
	close(start)
	wg.Wait()
	close(results)

	published, skipped := 0, 0
	for result := range results {
		switch result {
		case OpenAICodexTurnStateOfferPublished:
			published++
		case OpenAICodexTurnStateOfferSkippedHealthy:
			skipped++
		default:
			t.Fatalf("unexpected offer result: %d", result)
		}
	}
	require.Equal(t, 1, published)
	require.Equal(t, 1, skipped)
	status := collector.Status(key, base)
	require.True(t, status.Usable)
	require.False(t, status.Ready, "the losing response must not remain as a standby candidate")
	require.Zero(t, status.Candidates)
}

func TestOpenAICodexTurnStateCollectorResponseAndProbeRefreshRaceLeavesNoReadyCandidate(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 44, Scope: "execution", Model: "gpt-5"}
	active := collectorTestToken(t, base, 2, 21)
	require.True(t, collector.OfferValueMust(key, active, "seed", base))

	refreshAt := base.Add(8*time.Minute + time.Second)
	require.True(t, collector.NeedsRefresh(key, refreshAt))
	probe, started := collector.StartProbe(key, refreshAt)
	require.True(t, started)
	defer collector.FinishProbe(probe, refreshAt)

	responseToken, err := ParseOpenAICodexTurnState(collectorTestToken(t, refreshAt, 2, 22))
	require.NoError(t, err)
	probeToken, err := ParseOpenAICodexTurnState(collectorTestToken(t, refreshAt, 2, 23))
	require.NoError(t, err)

	start := make(chan struct{})
	results := make(chan OpenAICodexTurnStateOfferResult, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		results <- collector.OfferIfRefreshNeeded(key, responseToken, "response", refreshAt)
	}()
	go func() {
		defer wg.Done()
		<-start
		results <- collector.OfferProbeIfRefreshNeeded(probe, probeToken, "probe", refreshAt)
	}()
	close(start)
	wg.Wait()
	close(results)

	published, skipped := 0, 0
	for result := range results {
		switch result {
		case OpenAICodexTurnStateOfferPublished:
			published++
		case OpenAICodexTurnStateOfferSkippedHealthy:
			skipped++
		default:
			t.Fatalf("unexpected offer result: %d", result)
		}
	}
	require.Equal(t, 1, published)
	require.Equal(t, 1, skipped)
	status := collector.Status(key, refreshAt)
	require.True(t, status.Usable)
	require.False(t, status.Ready, "the losing probe/response must not remain as a standby candidate")
	require.Equal(t, uint64(1), status.Candidates)
}

func TestOpenAICodexTurnStateCollectorStartProbeRechecksRefreshAfterConcurrentResponse(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 45, Scope: "execution", Model: "gpt-5"}
	require.True(t, collector.OfferValueMust(key, collectorTestToken(t, base, 2, 31), "seed", base))

	refreshAt := base.Add(8*time.Minute + time.Second)
	require.True(t, collector.NeedsRefresh(key, refreshAt), "the caller's initial snapshot should require refresh")
	fresh, err := ParseOpenAICodexTurnState(collectorTestToken(t, refreshAt, 2, 32))
	require.NoError(t, err)
	require.Equal(
		t,
		OpenAICodexTurnStateOfferPublished,
		collector.OfferIfRefreshNeeded(key, fresh, "concurrent-response", refreshAt),
	)

	probe, result := collector.StartProbeIfRefreshNeeded(key, refreshAt)
	require.Equal(t, OpenAICodexTurnStateProbeSkippedHealthy, result)
	require.Nil(t, probe.done, "a fresh response must close the precheck-to-probe race without reserving a lease")
	require.False(t, collector.Status(key, refreshAt).ProbeInFlight)
	require.Equal(t, uint64(0), collector.Metrics().ProbeStarted)
}

func TestOpenAICodexTurnStateCollectorRefreshStartsAfterFiftyFiveMinutes(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	policy := collectorTestPolicy()
	policy.TTL = time.Hour
	policy.RefreshBefore = 5 * time.Minute
	collector := NewOpenAICodexTurnStateCollector(policy)
	key := OpenAICodexTurnStateKey{AccountID: 46, Scope: "execution", Model: "gpt-5"}
	require.True(t, collector.OfferValueMust(key, collectorTestToken(t, base, 2, 41), "seed", base))

	require.False(t, collector.NeedsRefresh(key, base.Add(55*time.Minute)), "state remains reusable through the exact 55-minute boundary")
	require.True(t, collector.NeedsRefresh(key, base.Add(55*time.Minute+time.Nanosecond)), "refresh starts immediately after 55 minutes")
}

// activeKeyScope keeps the assertion above readable while avoiding direct
// access to the collector's internal map.
func activeKeyScope(status OpenAICodexTurnStateStatus) string { return status.Key.Scope }

func TestOpenAICodexTurnStateCollectorObserveAndRejectAreSnapshotScoped(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 7, Scope: "execution-a", Model: "gpt-5"}
	first := collectorTestToken(t, base, 2, 1)
	second := collectorTestToken(t, base.Add(time.Second), 2, 2)
	require.True(t, collector.OfferValueMust(key, first, "route-a", base))
	used, usable := collector.Acquire(key, base)
	require.True(t, usable)
	require.True(t, collector.OfferValueMust(key, second, "route-b", base))

	// Two bad observations for the exact active snapshot promote the standby.
	require.True(t, collector.Observe(key, "not-a-state", used, base))
	require.True(t, collector.Observe(key, "still-not-a-state", used, base))
	next, usable := collector.Acquire(key, base)
	require.True(t, usable)
	require.Equal(t, second, next.Token.Value)
	require.NotEqual(t, used.Version, next.Version)

	// A late response belonging to the old immutable snapshot cannot add a
	// strike to the newly promoted active value.
	require.True(t, collector.Observe(key, "bad-late-response", used, base))
	require.Zero(t, collector.Status(key, base).Strikes)
	require.False(t, collector.RejectAndPromote(key, used, base), "stale snapshot must not invalidate current state")
	require.False(t, collector.RejectAndPromote(key, next, base), "rejecting the last active state leaves the key unusable")
	_, stillUsable := collector.Acquire(key, base)
	require.False(t, stillUsable)
}

func TestOpenAICodexTurnStateCollectorInvalidateDropsStandbyWithoutPromotion(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 8, Scope: "execution", Model: "gpt-5"}
	active := collectorTestToken(t, base, 2, 71)
	standby := collectorTestToken(t, base.Add(time.Second), 2, 72)
	require.True(t, collector.OfferValueMust(key, active, "active", base))
	used, usable := collector.Acquire(key, base)
	require.True(t, usable)
	require.True(t, collector.OfferValueMust(key, standby, "standby", base))
	probe, started := collector.StartProbe(key, base)
	require.True(t, started)
	collector.FinishProbe(probe, base)

	require.True(t, collector.Invalidate(key, used, base.Add(time.Second)))
	_, usable = collector.Acquire(key, base.Add(time.Second))
	require.False(t, usable, "model-mismatched state must require a fresh probe")
	probe, started = collector.StartProbe(key, base.Add(time.Second))
	require.True(t, started, "model mismatch must bypass an older probe cooldown")
	collector.AbortProbe(probe, base.Add(time.Second))
	status := collector.Status(key, base.Add(time.Second))
	require.False(t, status.Ready)
	require.Empty(t, status.ActiveFingerprint)
	require.Empty(t, status.ReadyFingerprint)
}

func TestOpenAICodexTurnStateCollectorFencesProbeAfterExactKeyDelete(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 81, Scope: "execution", Model: "gpt-5"}
	siblingKey := OpenAICodexTurnStateKey{AccountID: 81, Scope: "execution", Model: "gpt-6-astra"}
	sibling := collectorTestToken(t, base, 2, 82)
	probeValue := collectorTestToken(t, base, 2, 83)
	require.True(t, collector.OfferValueMust(siblingKey, sibling, "sibling", base))

	probe, started := collector.StartProbe(key, base)
	require.True(t, started)
	// This models a response-model mismatch retiring the exact account/model
	// lineage while the old probe is still reading its upstream response.
	collector.Delete(key)
	probeToken, err := ParseOpenAICodexTurnState(probeValue)
	require.NoError(t, err)
	require.Equal(t, OpenAICodexTurnStateOfferRejected,
		collector.OfferProbeIfRefreshNeeded(probe, probeToken, "probe", base),
		"a fenced lease must be rejected before the healthy-state shortcut")
	require.False(t, collector.OfferProbe(probe, probeToken, "probe", base), "an invalidated probe must not publish a candidate")
	collector.FinishProbe(probe, base)

	_, usable := collector.Acquire(key, base)
	require.False(t, usable)
	snapshot, usable := collector.Acquire(siblingKey, base)
	require.True(t, usable, "invalidating one model must preserve the sibling model entry")
	require.Equal(t, sibling, snapshot.Token.Value)
}

func TestOpenAICodexTurnStateCollectorProbeSingleFlightAndCooldown(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 9, Scope: "execution", Model: "gpt-5"}

	probe, started := collector.StartProbe(key, base)
	require.True(t, started)
	_, started = collector.StartProbe(key, base)
	require.False(t, started)

	waited := make(chan error, 1)
	go func() {
		_, err := collector.WaitProbe(context.Background(), key)
		waited <- err
	}()
	collector.FinishProbe(probe, base)
	require.NoError(t, <-waited)
	collector.FinishProbe(probe, base) // duplicate completion is harmless

	_, started = collector.StartProbe(key, base.Add(time.Second))
	require.False(t, started, "probe cooldown must suppress an immediate retry")
	probe, started = collector.StartProbe(key, base.Add(4*time.Second))
	require.True(t, started)
	collector.FinishProbe(probe, base.Add(4*time.Second))

	metrics := collector.Metrics()
	require.Equal(t, uint64(2), metrics.ProbeStarted)
	require.Equal(t, uint64(1), metrics.ProbeCoalesced)
	require.Equal(t, uint64(2), metrics.ProbeFinished)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe, started = collector.StartProbe(key, base.Add(8*time.Second))
	require.True(t, started)
	wait, err := collector.WaitProbe(ctx, key)
	require.True(t, wait)
	require.ErrorIs(t, err, context.Canceled)
	collector.FinishProbe(probe, base.Add(8*time.Second))
}

func TestOpenAICodexTurnStateCollectorHealthyAccountModelSuppressesNewScopesWithoutAllocating(t *testing.T) {
	base := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	policy := collectorTestPolicy()
	policy.MaxEntries = 4
	collector := NewOpenAICodexTurnStateCollector(policy)
	healthyKey := OpenAICodexTurnStateKey{AccountID: 91, Scope: "existing", Model: "gpt-5"}
	healthyState := collectorTestToken(t, base, 2, 91)
	require.True(t, collector.OfferValueMust(healthyKey, healthyState, "seed", base))

	for i := 0; i < 10_000; i++ {
		key := OpenAICodexTurnStateKey{AccountID: 91, Scope: "new-scope-" + strconv.Itoa(i), Model: "gpt-5"}
		_, result := collector.StartProbeIfRefreshNeeded(key, base)
		require.Equal(t, OpenAICodexTurnStateProbeSkippedAccountModelHealthy, result)
	}

	require.Equal(t, 1, collector.Metrics().Entries, "suppressed scopes must not allocate empty collector entries")
	snapshot, usable := collector.Acquire(healthyKey, base)
	require.True(t, usable)
	require.Equal(t, healthyState, snapshot.Token.Value, "scope churn must not evict the healthy account/model gate")
	require.Zero(t, collector.Metrics().ProbeStarted)
}

func TestOpenAICodexTurnStateCollectorResponseOfferHealthyAccountModelSuppressesNewScopesWithoutAllocating(t *testing.T) {
	base := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	policy := collectorTestPolicy()
	policy.MaxEntries = 4
	collector := NewOpenAICodexTurnStateCollector(policy)
	healthyKey := OpenAICodexTurnStateKey{AccountID: 93, Scope: "existing", Model: "gpt-5"}
	healthyState := collectorTestToken(t, base, 2, 93)
	require.True(t, collector.OfferValueMust(healthyKey, healthyState, "seed", base))
	responseToken, err := ParseOpenAICodexTurnState(collectorTestToken(t, base, 2, 94))
	require.NoError(t, err)

	for i := 0; i < 10_000; i++ {
		key := OpenAICodexTurnStateKey{AccountID: 93, Scope: "new-scope-" + strconv.Itoa(i), Model: "gpt-5"}
		result := collector.OfferIfRefreshNeeded(key, responseToken, "response", base)
		if result != OpenAICodexTurnStateOfferSkippedHealthy {
			t.Fatalf("scope %d: unexpected offer result %d", i, result)
		}
	}

	require.Equal(t, 1, collector.Metrics().Entries, "suppressed response scopes must not allocate collector entries")
	snapshot, usable := collector.Acquire(healthyKey, base)
	require.True(t, usable)
	require.Equal(t, healthyState, snapshot.Token.Value, "response scope churn must not evict the healthy account/model gate")
}

func TestOpenAICodexTurnStateCollectorResponseOfferExactInvalidationBypassesHealthySibling(t *testing.T) {
	// DeleteAndForceRefresh intentionally uses the wall clock for its TTL sweep.
	// Keep this test on the same clock domain so it remains deterministic when
	// run more than one collector TTL after the original fixture timestamp.
	base := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	sibling := OpenAICodexTurnStateKey{AccountID: 94, Scope: "S1", Model: "gpt-5"}
	target := OpenAICodexTurnStateKey{AccountID: 94, Scope: "S2", Model: "gpt-5"}
	require.True(t, collector.OfferValueMust(sibling, collectorTestToken(t, base, 2, 95), "seed", base))
	responseState := collectorTestToken(t, base, 2, 96)
	responseToken, err := ParseOpenAICodexTurnState(responseState)
	require.NoError(t, err)

	require.Equal(t, OpenAICodexTurnStateOfferSkippedHealthy,
		collector.OfferIfRefreshNeeded(target, responseToken, "response", base))
	require.Equal(t, 1, collector.Metrics().Entries)

	collector.DeleteAndForceRefresh(target)
	require.Equal(t, OpenAICodexTurnStateOfferPublished,
		collector.OfferIfRefreshNeeded(target, responseToken, "response", base),
		"an exact forced refresh must bypass account/model sibling suppression")
	snapshot, usable := collector.Acquire(target, base)
	require.True(t, usable)
	require.Equal(t, responseState, snapshot.Token.Value)
	require.Equal(t, 2, collector.Metrics().Entries)
}

func TestOpenAICodexTurnStateCollectorResponseOfferExistingEntryHonorsHealthySibling(t *testing.T) {
	base := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

	t.Run("empty placeholder", func(t *testing.T) {
		collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
		target := OpenAICodexTurnStateKey{AccountID: 95, Scope: "empty", Model: "gpt-5"}
		sibling := OpenAICodexTurnStateKey{AccountID: 95, Scope: "healthy", Model: "gpt-5"}
		probe, started := collector.StartProbe(target, base)
		require.True(t, started)
		collector.FinishProbe(probe, base)
		require.True(t, collector.OfferValueMust(sibling, collectorTestToken(t, base, 2, 97), "seed", base))
		responseToken, err := ParseOpenAICodexTurnState(collectorTestToken(t, base, 2, 98))
		require.NoError(t, err)

		require.Equal(t, OpenAICodexTurnStateOfferSkippedHealthy,
			collector.OfferIfRefreshNeeded(target, responseToken, "response", base))
		_, usable := collector.Acquire(target, base)
		require.False(t, usable, "an old empty placeholder must not bypass the account/model gate")
	})

	t.Run("refresh due", func(t *testing.T) {
		collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
		target := OpenAICodexTurnStateKey{AccountID: 96, Scope: "refresh-due", Model: "gpt-5"}
		sibling := OpenAICodexTurnStateKey{AccountID: 96, Scope: "healthy", Model: "gpt-5"}
		targetState := collectorTestToken(t, base, 2, 99)
		require.True(t, collector.OfferValueMust(target, targetState, "seed-target", base))
		refreshAt := base.Add(8*time.Minute + time.Second)
		require.True(t, collector.OfferValueMust(sibling, collectorTestToken(t, refreshAt, 2, 100), "seed-sibling", refreshAt))
		responseToken, err := ParseOpenAICodexTurnState(collectorTestToken(t, refreshAt, 2, 101))
		require.NoError(t, err)

		require.True(t, collector.NeedsRefresh(target, refreshAt))
		require.Equal(t, OpenAICodexTurnStateOfferSkippedHealthy,
			collector.OfferIfRefreshNeeded(target, responseToken, "response", refreshAt))
		snapshot, usable := collector.Acquire(target, refreshAt)
		require.True(t, usable)
		require.Equal(t, targetState, snapshot.Token.Value, "the sibling gate must not replace the exact active snapshot")
		require.False(t, collector.Status(target, refreshAt).Ready)
	})
}

func TestOpenAICodexTurnStateCollectorProbeOfferRechecksHealthySibling(t *testing.T) {
	base := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	target := OpenAICodexTurnStateKey{AccountID: 97, Scope: "probe", Model: "gpt-5"}
	sibling := OpenAICodexTurnStateKey{AccountID: 97, Scope: "response", Model: "gpt-5"}
	probe, result := collector.StartProbeIfRefreshNeeded(target, base)
	require.Equal(t, OpenAICodexTurnStateProbeStarted, result)
	defer collector.FinishProbe(probe, base)

	require.True(t, collector.OfferValueMust(sibling, collectorTestToken(t, base, 2, 102), "response", base))
	probeToken, err := ParseOpenAICodexTurnState(collectorTestToken(t, base, 2, 103))
	require.NoError(t, err)
	require.Equal(t, OpenAICodexTurnStateOfferSkippedHealthy,
		collector.OfferProbeIfRefreshNeeded(probe, probeToken, "probe", base),
		"a probe must not publish after another scope makes the account/model healthy")
	_, usable := collector.Acquire(target, base)
	require.False(t, usable)
}

func TestOpenAICodexTurnStateCollectorExactInvalidationBypassesHealthySibling(t *testing.T) {
	// DeleteAndForceRefresh performs its TTL sweep against the wall clock. Keep
	// the sibling genuinely healthy so this test exercises the force-refresh
	// bypass instead of passing after the fixed fixture has silently expired.
	base := time.Now().UTC().Truncate(time.Second)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	target := OpenAICodexTurnStateKey{AccountID: 92, Scope: "invalidated", Model: "gpt-5"}
	sibling := OpenAICodexTurnStateKey{AccountID: 92, Scope: "healthy", Model: "gpt-5"}
	require.True(t, collector.OfferValueMust(sibling, collectorTestToken(t, base, 2, 92), "seed", base))

	_, result := collector.StartProbeIfRefreshNeeded(target, base)
	require.Equal(t, OpenAICodexTurnStateProbeSkippedAccountModelHealthy, result)
	collector.DeleteAndForceRefresh(target)
	probe, result := collector.StartProbeIfRefreshNeeded(target, base)
	require.Equal(t, OpenAICodexTurnStateProbeStarted, result, "explicit invalidation must bypass sibling-health suppression")
	collector.AbortProbe(probe, base)
	probe, result = collector.StartProbeIfRefreshNeeded(target, base)
	require.Equal(t, OpenAICodexTurnStateProbeStarted, result, "an aborted forced refresh must remain forced")
	probeState := collectorTestToken(t, base, 2, 93)
	probeToken, err := ParseOpenAICodexTurnState(probeState)
	require.NoError(t, err)
	require.Equal(t, OpenAICodexTurnStateOfferPublished,
		collector.OfferProbeIfRefreshNeeded(probe, probeToken, "probe", base),
		"an exact forced probe must bypass account/model sibling suppression")
	collector.FinishProbe(probe, base)
	snapshot, usable := collector.Acquire(target, base)
	require.True(t, usable)
	require.Equal(t, probeState, snapshot.Token.Value)
}

func TestOpenAICodexTurnStateCollectorZeroProbeCooldownAllowsImmediateRetry(t *testing.T) {
	policy := collectorTestPolicy()
	policy.ProbeCooldown = 0
	collector := NewOpenAICodexTurnStateCollector(policy)
	key := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 10, Scope: "execution", Model: "gpt-5"})
	now := time.Now()

	probe, started := collector.StartProbe(key, now)
	require.True(t, started)
	collector.FinishProbe(probe, now)
	_, started = collector.StartProbe(key, now)
	require.True(t, started, "an explicit zero cooldown must permit an immediate retry")
}

func TestOpenAICodexTurnStateCollectorAbortProbePreservesConcurrentState(t *testing.T) {
	policy := collectorTestPolicy()
	collector := NewOpenAICodexTurnStateCollector(policy)
	key := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 11, Scope: "execution", Model: "gpt-5"})
	now := time.Now().UTC()
	probe, started := collector.StartProbe(key, now)
	require.True(t, started)
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 111)
	require.True(t, collector.OfferValueMust(key, state, "response", now))

	collector.AbortProbe(probe, now)

	snapshot, usable := collector.Acquire(key, now)
	require.True(t, usable)
	require.Equal(t, state, snapshot.Token.Value)
	metrics := collector.Metrics()
	require.Equal(t, 1, metrics.Entries)
	require.Equal(t, uint64(1), metrics.ProbeFinished)
}

func TestOpenAICodexTurnStateCollectorStatusNeverContainsOpaqueValue(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 1, Scope: "scope", Model: "gpt-5"}
	value := collectorTestToken(t, base, 2, 99)
	require.True(t, collector.OfferValueMust(key, value, "route", base))
	status := collector.Status(key, base)
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), value)
	require.NotContains(t, string(encoded), "Token")

	// The aggregate metrics are likewise safe to serialize.
	metrics, err := json.Marshal(collector.Metrics())
	require.NoError(t, err)
	require.NotContains(t, string(metrics), value)
}

func (c *OpenAICodexTurnStateCollector) OfferValueMust(key OpenAICodexTurnStateKey, value, route string, now time.Time) bool {
	_, accepted, err := c.OfferValue(key, value, route, now)
	if err != nil {
		return false
	}
	return accepted
}

func TestOpenAICodexTurnStatePolicyRejectsWrongBlockCount(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	key := OpenAICodexTurnStateKey{AccountID: 1, Scope: "scope", Model: "gpt-5"}
	value := collectorTestToken(t, now, 1, 1)
	_, accepted, err := collector.OfferValue(key, value, "route", now)
	require.ErrorIs(t, err, ErrOpenAICodexTurnStateShape)
	require.False(t, accepted)
	require.Equal(t, uint64(1), collector.Metrics().RejectedOffers)

	_, err = ParseOpenAICodexTurnState(value)
	require.NoError(t, err, "the parser only checks the envelope; policy owns block-count admission")
	require.False(t, errors.Is(err, ErrOpenAICodexTurnStateShape))
}
