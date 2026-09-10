package repository

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

// TestHTTPUpstreamConcurrentCacheHit verifies that concurrent requests for the
// same account and proxy all reuse one upstream client. This is a regression
// guard for the hot cache path used under high request concurrency.
func TestHTTPUpstreamConcurrentCacheHit(t *testing.T) {
	const workers = 128

	svc := NewHTTPUpstream(&config.Config{
		Gateway: config.GatewayConfig{
			ConnectionPoolIsolation: config.ConnectionPoolIsolationAccountProxy,
		},
	}).(*httpUpstreamService)

	entries := make(chan *upstreamClientEntry, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entry, err := svc.getOrCreateClient("", 42, 8)
			if err != nil {
				errs <- err
				return
			}
			entries <- entry
		}()
	}
	wg.Wait()
	close(entries)
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	var first *upstreamClientEntry
	for entry := range entries {
		if first == nil {
			first = entry
			continue
		}
		require.Same(t, first, entry, "concurrent cache hits must reuse the same client entry")
	}
	require.NotNil(t, first)

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	require.Len(t, svc.clients, 1)
}
