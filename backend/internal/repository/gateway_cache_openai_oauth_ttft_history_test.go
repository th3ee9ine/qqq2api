package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

type ttftHistoryRedisHook struct {
	mu         sync.Mutex
	batches    []int
	singleRead int
}

func (h *ttftHistoryRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *ttftHistoryRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "zrevrange" {
			h.mu.Lock()
			h.singleRead++
			h.mu.Unlock()
		}
		return next(ctx, cmd)
	}
}

func (h *ttftHistoryRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		reads := 0
		for _, cmd := range cmds {
			if cmd.Name() == "zrevrange" {
				reads++
			}
		}
		if reads > 0 {
			h.mu.Lock()
			h.batches = append(h.batches, reads)
			h.mu.Unlock()
		}
		return next(ctx, cmds)
	}
}

func newTTFTHistoryRedisClient(t *testing.T, server *miniredis.Miniredis) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestGatewayCacheOpenAIOAuthTTFTHistoryBoundedPersistentAndBatched(t *testing.T) {
	server := miniredis.RunT(t)
	client := newTTFTHistoryRedisClient(t, server)
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTHistoryCache)
	ctx := context.Background()
	start := time.Now().UTC()
	for i := 0; i < 7; i++ {
		value := i
		require.NoError(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, 17, service.OpenAIOAuthTTFTObservation{
			FirstTokenMs: &value, Model: fmt.Sprintf("model-%d", i), ObservedAt: start.Add(time.Duration(i) * time.Second),
		}))
	}
	negative, zero := -1, 0
	for i, value := range []*int{nil, &negative, &zero} {
		require.NoError(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, 18, service.OpenAIOAuthTTFTObservation{FirstTokenMs: value, Model: "gpt-5.4", ObservedAt: start.Add(time.Duration(i))}))
	}
	server.FastForward(7 * 24 * time.Hour)
	hook := &ttftHistoryRedisHook{}
	client.AddHook(hook)
	history, err := cache.GetOpenAIOAuthTTFTObservations(ctx, []int64{17, 18, 19, 17, 0, -1})
	require.NoError(t, err)
	require.Len(t, history, 3)
	require.Equal(t, []int{3}, hook.batches, "all distinct valid account histories use one pipeline")
	require.Zero(t, hook.singleRead, "history reads must not make a roundtrip per account")
	require.Len(t, history[17], 5)
	for i, observation := range history[17] {
		require.Equal(t, 6-i, *observation.FirstTokenMs)
		require.Equal(t, fmt.Sprintf("model-%d", 6-i), observation.Model)
		require.Equal(t, start.Add(time.Duration(6-i)*time.Second), observation.ObservedAt)
	}
	require.Len(t, history[18], 3)
	require.NotNil(t, history[18][0].FirstTokenMs)
	require.Zero(t, *history[18][0].FirstTokenMs)
	require.Nil(t, history[18][1].FirstTokenMs)
	require.Nil(t, history[18][2].FirstTokenMs)
	require.NotNil(t, history[19])
	require.Empty(t, history[19])
	// Streak resets and output mutation never alter separately stored history.
	require.NoError(t, NewGatewayCache(client).(service.OpenAIOAuthTTFTStreakCache).ResetOpenAIOAuthTTFTStreak(ctx, 17))
	*history[17][0].FirstTokenMs = 1234
	again, err := cache.GetOpenAIOAuthTTFTObservations(ctx, []int64{17})
	require.NoError(t, err)
	require.Equal(t, 6, *again[17][0].FirstTokenMs)
}

func TestGatewayCacheOpenAIOAuthTTFTHistoryConcurrentInstances(t *testing.T) {
	server := miniredis.RunT(t)
	clients := []*redis.Client{newTTFTHistoryRedisClient(t, server), newTTFTHistoryRedisClient(t, server)}
	caches := []service.OpenAIOAuthTTFTHistoryCache{
		NewGatewayCache(clients[0]).(service.OpenAIOAuthTTFTHistoryCache),
		NewGatewayCache(clients[1]).(service.OpenAIOAuthTTFTHistoryCache),
	}
	ctx := context.Background()
	var wg sync.WaitGroup
	const count = 80
	errs := make([]error, count)
	sizes := make([]int64, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			value := i
			errs[i] = caches[i%2].AppendOpenAIOAuthTTFTObservation(ctx, 17, service.OpenAIOAuthTTFTObservation{FirstTokenMs: &value, Model: fmt.Sprintf("request-%d", i), ObservedAt: time.Now().UTC()})
			if errs[i] == nil {
				sizes[i], errs[i] = clients[i%2].ZCard(ctx, openAIOAuthTTFTHistoryKey(17)).Result()
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		require.NoError(t, err)
		require.LessOrEqual(t, sizes[i], int64(5), "ZADD and trim must be atomic to observers")
	}
	value := 999
	require.NoError(t, caches[0].AppendOpenAIOAuthTTFTObservation(ctx, 17, service.OpenAIOAuthTTFTObservation{FirstTokenMs: &value, Model: "final", ObservedAt: time.Now().UTC()}))
	one, err := caches[0].GetOpenAIOAuthTTFTObservations(ctx, []int64{17})
	require.NoError(t, err)
	two, err := caches[1].GetOpenAIOAuthTTFTObservations(ctx, []int64{17})
	require.NoError(t, err)
	require.Equal(t, one, two)
	require.Len(t, two[17], 5)
	require.Equal(t, "final", two[17][0].Model)
	seen := map[string]bool{}
	for _, observation := range two[17] {
		require.False(t, seen[observation.Model])
		seen[observation.Model] = true
	}
}

func TestGatewayCacheOpenAIOAuthTTFTHistoryCorruptionIsIsolated(t *testing.T) {
	server := miniredis.RunT(t)
	client := newTTFTHistoryRedisClient(t, server)
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTHistoryCache)
	ctx := context.Background()
	for _, id := range []int64{17, 18} {
		require.NoError(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, id, service.OpenAIOAuthTTFTObservation{Model: "valid", ObservedAt: time.Now().UTC()}))
	}
	memberPrefix := time.Now().UTC().Format(openAIOAuthTTFTHistoryTimeLayout) + "\n00000000-0000-4000-8000-000000000001\n"
	require.NoError(t, client.ZAdd(ctx, openAIOAuthTTFTHistoryKey(17),
		redis.Z{Member: "invalid-member"},
		redis.Z{Member: memberPrefix + "invalid-json"},
		redis.Z{Member: memberPrefix + "null"},
		redis.Z{Member: memberPrefix + `{}`},
	).Err())
	require.NoError(t, client.Set(ctx, openAIOAuthTTFTHistoryKey(19), "wrong-type", 0).Err())
	history, err := cache.GetOpenAIOAuthTTFTObservations(ctx, []int64{17, 18, 19})
	require.NoError(t, err)
	require.Len(t, history[17], 1, "valid records in the same account survive corrupt entries")
	require.Equal(t, "valid", history[17][0].Model)
	require.Len(t, history[18], 1, "another account must not lose its history")
	require.NotContains(t, history, int64(19), "wrong Redis types degrade independently to local state")
}

func TestGatewayCacheOpenAIOAuthTTFTHistoryErrors(t *testing.T) {
	server := miniredis.RunT(t)
	client := newTTFTHistoryRedisClient(t, server)
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTHistoryCache)
	ctx := context.Background()
	require.Error(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, 0, service.OpenAIOAuthTTFTObservation{}))
	var nilCache *gatewayCache
	require.Error(t, nilCache.AppendOpenAIOAuthTTFTObservation(ctx, 1, service.OpenAIOAuthTTFTObservation{}))
	_, err := nilCache.GetOpenAIOAuthTTFTObservations(ctx, []int64{1})
	require.Error(t, err)
	empty, err := cache.GetOpenAIOAuthTTFTObservations(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.NoError(t, client.Close())
	require.Error(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, 1, service.OpenAIOAuthTTFTObservation{}))
	_, err = cache.GetOpenAIOAuthTTFTObservations(ctx, []int64{1})
	require.Error(t, err)
}

// Drop one successful script response after Redis has executed it. The client
// sees EOF and retries the same command through its real connection machinery.
type ttftHistoryDroppedReplyConn struct {
	net.Conn
	evalWritten atomic.Bool
	dropped     *atomic.Bool
}

func (c *ttftHistoryDroppedReplyConn) Write(payload []byte) (int, error) {
	if bytes.Contains(payload, []byte("\r\neval\r\n")) || bytes.Contains(payload, []byte("\r\nevalsha\r\n")) {
		c.evalWritten.Store(true)
	}
	return c.Conn.Write(payload)
}

func (c *ttftHistoryDroppedReplyConn) Read(payload []byte) (int, error) {
	n, err := c.Conn.Read(payload)
	if c.evalWritten.Load() && n > 0 && bytes.Contains(payload[:n], []byte(":1\r\n")) && c.dropped.CompareAndSwap(false, true) {
		_ = c.Conn.Close()
		return 0, io.EOF
	}
	return n, err
}

func TestGatewayCacheOpenAIOAuthTTFTHistoryRetryAfterDroppedReplyIsIdempotent(t *testing.T) {
	server := miniredis.RunT(t)
	var dropped atomic.Bool
	client := redis.NewClient(&redis.Options{
		Addr: server.Addr(),
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &ttftHistoryDroppedReplyConn{Conn: conn, dropped: &dropped}, nil
		},
	})
	t.Cleanup(func() { _ = client.Close() })
	require.Equal(t, 3, client.Options().MaxRetries, "exercise go-redis's production default retries")
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTHistoryCache)
	latency := 15001
	observation := service.OpenAIOAuthTTFTObservation{FirstTokenMs: &latency, Model: "one-actual-call", ObservedAt: time.Now().UTC()}
	require.NoError(t, cache.AppendOpenAIOAuthTTFTObservation(context.Background(), 17, observation))
	require.True(t, dropped.Load(), "a successful server reply must have been dropped")
	history, err := cache.GetOpenAIOAuthTTFTObservations(context.Background(), []int64{17})
	require.NoError(t, err)
	require.Equal(t, []service.OpenAIOAuthTTFTObservation{observation}, history[17], "transport retries must not become additional actual requests")
}

func TestGatewayCacheOpenAIOAuthTTFTHistoryLateRetryDoesNotEvictNewerRequests(t *testing.T) {
	server := miniredis.RunT(t)
	client := newTTFTHistoryRedisClient(t, server)
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTHistoryCache)
	ctx := context.Background()
	start := time.Now().UTC()
	require.NoError(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, 17, service.OpenAIOAuthTTFTObservation{Model: "old", ObservedAt: start}))
	oldMembers, err := client.ZRange(ctx, openAIOAuthTTFTHistoryKey(17), 0, -1).Result()
	require.NoError(t, err)
	require.Len(t, oldMembers, 1)
	for i := 1; i <= 5; i++ {
		require.NoError(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, 17, service.OpenAIOAuthTTFTObservation{
			Model: fmt.Sprintf("new-%d", i), ObservedAt: start.Add(time.Duration(i) * time.Nanosecond),
		}))
	}
	before, err := cache.GetOpenAIOAuthTTFTObservations(ctx, []int64{17})
	require.NoError(t, err)
	require.Len(t, before[17], 5)
	for i, observation := range before[17] {
		require.Equal(t, fmt.Sprintf("new-%d", 5-i), observation.Model, "timestamp ordering must retain nanosecond precision")
	}
	// The original member has fallen out of the window. A late retry can
	// reinsert it, but the same atomic trim must immediately remove it again.
	for i := 0; i < 3; i++ {
		require.NoError(t, openAIOAuthTTFTHistoryAppendScript.Run(ctx, client, []string{openAIOAuthTTFTHistoryKey(17)}, oldMembers[0], service.OpenAIOAuthTTFTHistoryLimit).Err())
	}
	after, err := cache.GetOpenAIOAuthTTFTObservations(ctx, []int64{17})
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestGatewayCacheOpenAIOAuthTTFTHistoryIdenticalRealCallsAreDistinct(t *testing.T) {
	server := miniredis.RunT(t)
	client := newTTFTHistoryRedisClient(t, server)
	cache := NewGatewayCache(client).(service.OpenAIOAuthTTFTHistoryCache)
	ctx := context.Background()
	latency := 0
	observation := service.OpenAIOAuthTTFTObservation{Model: "same-model", ObservedAt: time.Now().UTC(), FirstTokenMs: &latency}
	for i := 0; i < 3; i++ {
		require.NoError(t, cache.AppendOpenAIOAuthTTFTObservation(ctx, 17, observation))
	}
	history, err := cache.GetOpenAIOAuthTTFTObservations(ctx, []int64{17})
	require.NoError(t, err)
	require.Len(t, history[17], 3, "same time, model and TTFT can still describe independent actual calls")
	for _, actual := range history[17] {
		require.Equal(t, observation, actual)
	}
}
