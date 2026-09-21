package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAICodexTurnStateCollectorDeleteAccountInvalidatesInFlightGeneration(t *testing.T) {
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	collector := NewOpenAICodexTurnStateCollector(collectorTestPolicy())
	logicalKey := OpenAICodexTurnStateKey{AccountID: 42, Scope: "execution", Model: "gpt-5"}
	staleKey := collector.BindKey(logicalKey)
	require.NotZero(t, staleKey.generation)

	probe, started := collector.StartProbe(staleKey, base)
	require.True(t, started)
	waitResult := make(chan struct {
		waited bool
		err    error
	}, 1)
	go func() {
		waited, err := collector.WaitProbe(context.Background(), staleKey)
		waitResult <- struct {
			waited bool
			err    error
		}{waited: waited, err: err}
	}()
	// Give WaitProbe an opportunity to capture the active lease. DeleteAccount
	// closes the same channel either way, so the assertion remains race-free.
	time.Sleep(time.Millisecond)

	collector.DeleteAccount(logicalKey.AccountID)
	select {
	case result := <-waitResult:
		require.True(t, result.waited)
		require.NoError(t, result.err)
	case <-time.After(time.Second):
		t.Fatal("account invalidation did not release the probe waiter")
	}

	metricsAfterDelete := collector.Metrics()
	require.Equal(t, uint64(1), metricsAfterDelete.ProbeFinished)
	collector.FinishProbe(probe, base.Add(time.Second))
	require.Equal(t, metricsAfterDelete.ProbeFinished, collector.Metrics().ProbeFinished,
		"a stale probe completion must not close or count the lease twice")
	require.False(t, collector.IsCurrentKey(staleKey))

	staleValue := collectorTestToken(t, base, 2, 1)
	_, accepted, err := collector.OfferValue(staleKey, staleValue, "stale-response", base)
	require.Error(t, err)
	require.False(t, accepted, "a response bound before invalidation must not repopulate the collector")
	require.Zero(t, collector.Metrics().Entries)

	freshKey := collector.BindKey(logicalKey)
	require.NotZero(t, freshKey.generation)
	require.NotEqual(t, staleKey.generation, freshKey.generation)
	freshValue := collectorTestToken(t, base, 2, 2)
	_, accepted, err = collector.OfferValue(freshKey, freshValue, "fresh-response", base)
	require.NoError(t, err)
	require.True(t, accepted)
	snapshot, usable := collector.Acquire(freshKey, base)
	require.True(t, usable)
	require.Equal(t, freshValue, snapshot.Token.Value)
}

func TestOpenAICodexTurnStateCollectorBoundsAccountGenerations(t *testing.T) {
	policy := collectorTestPolicy()
	policy.MaxEntries = 2
	collector := NewOpenAICodexTurnStateCollector(policy)

	first := collector.BindKey(OpenAICodexTurnStateKey{AccountID: 1, Scope: "scope", Model: "gpt-5"})
	for accountID := int64(2); accountID <= 12; accountID++ {
		collector.BindKey(OpenAICodexTurnStateKey{AccountID: accountID, Scope: "scope", Model: "gpt-5"})
		collector.mu.Lock()
		generationCount := len(collector.accountGenerations)
		collector.mu.Unlock()
		require.LessOrEqual(t, generationCount, policy.MaxEntries)
	}
	require.False(t, collector.IsCurrentKey(first), "an evicted lifecycle lease must remain stale")
}
