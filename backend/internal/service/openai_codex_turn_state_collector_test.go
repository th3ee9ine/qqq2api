package service

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
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
