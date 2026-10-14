package repository

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/pkg/tlsfingerprint"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestHTTPUpstreamIdleSweepIsBoundedAndEventuallyReclaims(t *testing.T) {
	const accounts = upstreamClientIdleSweepBatchSize*3 + 1
	svc := NewHTTPUpstream(&config.Config{Gateway: config.GatewayConfig{
		MaxUpstreamClients: accounts + 10,
	}}).(*httpUpstreamService)
	entries := make([]*upstreamClientEntry, 0, accounts)
	for id := 1; id <= accounts; id++ {
		entries = append(entries, mustGetOrCreateClient(t, svc, "", int64(id), 8))
	}
	for _, entry := range entries {
		atomic.StoreInt64(&entry.lastUsed, time.Now().Add(-time.Hour).UnixNano())
	}
	atomic.StoreInt64(&entries[0].inFlight, 1)

	// A miss must leave some expired residents for later bounded sweeps.
	mustGetOrCreateClient(t, svc, "", accounts+1, 8)
	require.GreaterOrEqual(t, len(svc.clients), accounts+1-upstreamClientIdleSweepBatchSize)
	for id := accounts + 2; id <= accounts+6; id++ {
		mustGetOrCreateClient(t, svc, "", int64(id), 8)
	}
	require.True(t, hasEntry(svc, entries[0]), "an active stream must survive TTL sweeps")
	for _, entry := range entries[1:] {
		require.False(t, hasEntry(svc, entry), "successive misses must eventually reclaim every expired idle entry")
	}
	atomic.StoreInt64(&entries[0].inFlight, 0)
	mustGetOrCreateClient(t, svc, "", accounts+7, 8)
	require.False(t, hasEntry(svc, entries[0]))
}

func TestHTTPUpstreamConcurrentMissRespectsActiveCapacity(t *testing.T) {
	for _, tlsEnabled := range []bool{false, true} {
		name := "HTTP"
		if tlsEnabled {
			name = "TLSFingerprint"
		}
		t.Run(name, func(t *testing.T) {
			const capacity, workers = 8, 64
			svc := NewHTTPUpstream(&config.Config{Gateway: config.GatewayConfig{
				MaxUpstreamClients: capacity,
			}}).(*httpUpstreamService)
			type result struct {
				entry *upstreamClientEntry
				err   error
			}
			results := make(chan result, workers)
			var wg sync.WaitGroup
			for id := 1; id <= workers; id++ {
				wg.Add(1)
				go func(id int64) {
					defer wg.Done()
					entry, err := acquireEvictionTestClient(svc, id, 8, tlsEnabled)
					results <- result{entry, err}
				}(int64(id))
			}
			wg.Wait()
			close(results)
			var active []*upstreamClientEntry
			for result := range results {
				if result.err != nil {
					require.ErrorIs(t, result.err, errUpstreamClientLimitReached)
					continue
				}
				active = append(active, result.entry)
			}
			require.Len(t, active, capacity)
			require.Len(t, svc.clients, capacity)
			for _, entry := range active {
				require.Equal(t, int64(1), atomic.LoadInt64(&entry.inFlight))
				require.True(t, hasEntry(svc, entry))
			}
			// Releasing one stream makes exactly one cache slot available again.
			atomic.StoreInt64(&active[0].inFlight, 0)
			_, err := acquireEvictionTestClient(svc, workers+1, 8, tlsEnabled)
			require.NoError(t, err)
			require.Len(t, svc.clients, capacity)
			require.False(t, hasEntry(svc, active[0]))
		})
	}
}

func TestHTTPUpstreamReplacementRetainsActiveEntry(t *testing.T) {
	for _, tlsEnabled := range []bool{false, true} {
		svc := NewHTTPUpstream(&config.Config{Gateway: config.GatewayConfig{
			MaxUpstreamClients: 1,
		}}).(*httpUpstreamService)
		original, err := acquireEvictionTestClient(svc, 1, 8, tlsEnabled)
		require.NoError(t, err)
		replacement, err := acquireEvictionTestClient(svc, 1, 16, tlsEnabled)
		require.NoError(t, err)
		require.NotSame(t, original, replacement)
		require.Equal(t, int64(1), atomic.LoadInt64(&original.inFlight))
		require.Equal(t, int64(1), atomic.LoadInt64(&replacement.inFlight))
		require.Len(t, svc.clients, 1)
		require.False(t, hasEntry(svc, original))
		require.True(t, hasEntry(svc, replacement))
		atomic.AddInt64(&original.inFlight, -1)
		reused, err := acquireEvictionTestClient(svc, 1, 16, tlsEnabled)
		require.NoError(t, err)
		require.Same(t, replacement, reused)
	}
}

func TestHTTPUpstreamRetiredConnectionCloseDoesNotBlockCacheHits(t *testing.T) {
	svc := NewHTTPUpstream(&config.Config{Gateway: config.GatewayConfig{
		MaxUpstreamClients: 3,
	}}).(*httpUpstreamService)
	expired := mustGetOrCreateClient(t, svc, "", 1, 8)
	live := mustGetOrCreateClient(t, svc, "", 2, 8)
	started, resume := make(chan struct{}), make(chan struct{})
	defer close(resume)
	expired.client.Transport = &blockingUpstreamIdleClose{started: started, resume: resume}
	atomic.StoreInt64(&expired.lastUsed, time.Now().Add(-time.Hour).UnixNano())
	missDone := make(chan error, 1)
	go func() {
		_, err := svc.acquireClient("", 3, 8)
		missDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("expired transport was not closed")
	}
	hitDone := make(chan *upstreamClientEntry, 1)
	go func() {
		entry, _ := svc.acquireClient("", 2, 8)
		hitDone <- entry
	}()
	select {
	case entry := <-hitDone:
		require.Same(t, live, entry)
	case <-time.After(2 * time.Second):
		t.Fatal("closing retired idle connections held the cache lock")
	}
	// Let the deferred close release the retired transport before awaiting the
	// miss, including on a failed assertion, so no test goroutine is stranded.
	t.Cleanup(func() { require.NoError(t, <-missDone) })
}

func acquireEvictionTestClient(svc *httpUpstreamService, accountID int64, concurrency int, tlsEnabled bool) (*upstreamClientEntry, error) {
	if tlsEnabled {
		return svc.acquireClientWithTLS("", accountID, concurrency, &tlsfingerprint.Profile{Name: "test"}, service.HTTPUpstreamProfileDefault)
	}
	return svc.acquireClient("", accountID, concurrency)
}

type blockingUpstreamIdleClose struct {
	started chan struct{}
	resume  chan struct{}
}

func (t *blockingUpstreamIdleClose) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused test transport")
}

func (t *blockingUpstreamIdleClose) CloseIdleConnections() {
	close(t.started)
	<-t.resume
}
