package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type codexTurnStateBoundedBody struct {
	reader *strings.Reader
	read   int
	closed bool
}

type codexTurnStateReleasedBody struct {
	reader  *strings.Reader
	started chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (b *codexTurnStateReleasedBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.started) })
	<-b.release
	return b.reader.Read(p)
}

func (b *codexTurnStateReleasedBody) Close() error { return nil }

func newCodexTurnStateBoundedBody(value string) *codexTurnStateBoundedBody {
	return &codexTurnStateBoundedBody{reader: strings.NewReader(value)}
}

func (b *codexTurnStateBoundedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	b.read += n
	return n, err
}

func (b *codexTurnStateBoundedBody) Close() error {
	b.closed = true
	return nil
}

func newCodexTurnStateGatewayTestService(upstream HTTPUpstream) *OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Gateway.CodexTurnState = config.GatewayCodexTurnStateConfig{
		ProbeTimeoutSeconds:   2,
		RefreshBeforeSeconds:  120,
		CooldownSeconds:       3,
		TTLSeconds:            600,
		ExpectedBlocks:        2,
		MaxEntries:            16,
		MaxTokenBytes:         2048,
		MaxProbeResponseBytes: 64 * 1024,
	}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	svc.initCodexTurnStateCollector()
	return svc
}

func newCodexTurnStateGatewayTestContext(t *testing.T, scope string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	bindOpenAICodexTurnStateExecutionScopeValue(c, scope)
	return c
}

func codexTurnStateGatewayTestAccount(id int64) *Account {
	return &Account{
		ID:       id,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "test-access-token",
			"chatgpt_account_id": "test-chatgpt-account",
		},
	}
}

func TestPrepareCodexTurnStatePrefersValidClientStateForCurrentAttempt(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(101)
	c := newCodexTurnStateGatewayTestContext(t, "execution-client-priority")
	now := time.Now().UTC()
	cachedValue := collectorTestToken(t, now.Add(-2*time.Minute), 2, 1)
	clientValue := collectorTestToken(t, now.Add(-time.Minute), 2, 2)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, cachedValue, "cached", now))

	incoming := make(http.Header)
	incoming.Set(openAICodexTurnStateHeader, clientValue)
	snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "gpt-5.5", incoming, false)

	require.True(t, ok)
	require.Equal(t, clientValue, snapshot.Token.Value)
	require.Equal(t, "client", snapshot.Route)
	require.Equal(t, clientValue, incoming.Get(openAICodexTurnStateHeader), "an accepted native state must remain on the outgoing request")

	// Admission of client evidence must not mutate the immutable cached snapshot
	// already active for other in-flight requests.
	active, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.True(t, usable)
	require.Equal(t, cachedValue, active.Token.Value)
	binding, bound := svc.codexTurnStateBinding(c, account, "gpt-5.5")
	require.True(t, bound)
	require.False(t, binding.used, "native state was not borrowed from the collector")
	require.Equal(t, clientValue, binding.snapshot.Token.Value)
	require.Equal(t, uint64(1), svc.codexTurnStateCollector.Metrics().Offers, "client input must not publish an unauthenticated reusable candidate")
}

func TestPrepareCodexTurnStateNativeStateIsNotPublishedWithoutUpstreamConfirmation(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(102)
	c := newCodexTurnStateGatewayTestContext(t, "execution-client-unconfirmed")
	now := time.Now().UTC()
	clientValue := collectorTestToken(t, now.Add(-time.Minute), 2, 3)
	incoming := make(http.Header)
	incoming.Set(openAICodexTurnStateHeader, clientValue)

	snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "gpt-5.5", incoming, false)

	require.True(t, ok)
	require.Equal(t, clientValue, snapshot.Token.Value)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	_, reusable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.False(t, reusable)
	require.Zero(t, svc.codexTurnStateCollector.Metrics().Offers)
	require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
}

func TestPrepareCodexTurnStateDoesNotReuseStateAcrossAccountSwitch(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	c := newCodexTurnStateGatewayTestContext(t, "execution-account-switch")
	accountA := codexTurnStateGatewayTestAccount(201)
	accountB := codexTurnStateGatewayTestAccount(202)
	now := time.Now().UTC()
	stateA := collectorTestToken(t, now.Add(-time.Minute), 2, 11)
	stateB := collectorTestToken(t, now.Add(-time.Minute), 2, 22)
	keyA := svc.codexTurnStateKey(c, accountA, "gpt-5.5")
	keyB := svc.codexTurnStateKey(c, accountB, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(keyA, stateA, "account-a", now))
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(keyB, stateB, "account-b", now))

	snapshotA, ok := svc.prepareCodexTurnState(context.Background(), c, accountA, "gpt-5.5", make(http.Header), false)
	require.True(t, ok)
	require.Equal(t, stateA, snapshotA.Token.Value)

	// A failover attempt reuses the execution scope but changes account. The
	// collector key and request binding must both follow the replacement account.
	snapshotB, ok := svc.prepareCodexTurnState(context.Background(), c, accountB, "gpt-5.5", make(http.Header), false)
	require.True(t, ok)
	require.Equal(t, stateB, snapshotB.Token.Value)
	require.NotEqual(t, stateA, snapshotB.Token.Value)
	binding, bound := svc.codexTurnStateBinding(c, accountB, "gpt-5.5")
	require.True(t, bound)
	require.Equal(t, accountB.ID, binding.key.AccountID)
	require.Equal(t, stateB, binding.snapshot.Token.Value)
	_, staleBinding := svc.codexTurnStateBinding(c, accountA, "gpt-5.5")
	require.False(t, staleBinding, "the prior account binding must not match after failover")
}

func TestPrepareCodexTurnStateAPIKeyNeverProbesOrInjects(t *testing.T) {
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 44)

	t.Run("api-key", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{}
		svc := newCodexTurnStateGatewayTestService(upstream)
		account := &Account{ID: 402, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
		c := newCodexTurnStateGatewayTestContext(t, "execution-api-key")
		key := svc.codexTurnStateKey(c, account, "gpt-5.5")
		require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, state, "seed", now))

		snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "gpt-5.5", make(http.Header), true)
		require.False(t, ok)
		require.Empty(t, snapshot.Token.Value, "API-key routes must not consume a collector entry even if one exists")
		require.Empty(t, upstream.requests)
	})
}

func TestPrepareCodexTurnStateInjectionDisabledDoesNotConsumeCachedState(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	svc := newCodexTurnStateGatewayTestService(upstream)
	svc.SetCodexTurnStateRuntimeSettings(true, false)
	account := codexTurnStateGatewayTestAccount(403)
	c := newCodexTurnStateGatewayTestContext(t, "execution-injection-disabled")
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 45)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, state, "seed", now))

	snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "gpt-5.5", make(http.Header), false)

	require.False(t, ok)
	require.Empty(t, snapshot.Token.Value)
	require.Empty(t, upstream.requests)
	binding, bound := svc.codexTurnStateBinding(c, account, "gpt-5.5")
	require.True(t, bound)
	require.False(t, binding.used, "disabled injection must not mark an unforwarded cache snapshot as used")
	require.Empty(t, binding.snapshot.Token.Value)
}

func TestPrepareCodexTurnStateMissingFinalModelFailsClosed(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(404)
	c := newCodexTurnStateGatewayTestContext(t, "execution-model-unknown")
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 46)
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(
		svc.codexTurnStateKey(c, account, "gpt-5.5"), state, "seed", now,
	))

	snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "", make(http.Header), true)

	require.False(t, ok)
	require.Empty(t, snapshot.Token.Value)
	require.Empty(t, upstream.requests, "an unknown final model must not trigger a default-model probe")
	binding, bound := svc.codexTurnStateBinding(c, account, "")
	require.True(t, bound)
	require.Empty(t, binding.key.Model)
	require.False(t, binding.used)
}

func TestPrepareCodexTurnStateMissingFinalModelPreservesValidNativeState(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(405)
	c := newCodexTurnStateGatewayTestContext(t, "execution-native-model-unknown")
	state := collectorTestToken(t, time.Now().Add(-time.Minute), 2, 47)
	incoming := make(http.Header)
	incoming.Set(openAICodexTurnStateHeader, state)

	snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "", incoming, true)

	require.True(t, ok)
	require.Equal(t, state, snapshot.Token.Value)
	require.Equal(t, state, incoming.Get(openAICodexTurnStateHeader))
	require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries, "a native value without a final model must not become reusable")
}

func TestObserveCodexTurnStateResponseRequiresCompleteKeyAndLiveClient(t *testing.T) {
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 48)
	headers := http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state}}
	account := codexTurnStateGatewayTestAccount(406)

	t.Run("missing final model", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c := newCodexTurnStateGatewayTestContext(t, "execution-response-model-unknown")
		svc.observeCodexTurnStateResponse(c, account, "", headers)
		require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
		require.Zero(t, svc.codexTurnStateCollector.Metrics().Offers)
	})

	t.Run("client context cancelled", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		c := newCodexTurnStateGatewayTestContext(t, "execution-response-cancelled")
		requestCtx, cancel := context.WithCancel(c.Request.Context())
		cancel()
		c.Request = c.Request.WithContext(requestCtx)
		svc.observeCodexTurnStateResponse(c, account, "gpt-5.5", headers)
		require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
		require.Zero(t, svc.codexTurnStateCollector.Metrics().Offers)
	})
}

func TestPrepareCodexTurnStateStripsInvalidIncomingBeforeCachedInjection(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(501)
	c := newCodexTurnStateGatewayTestContext(t, "execution-invalid-incoming")
	now := time.Now().UTC()
	cachedValue := collectorTestToken(t, now.Add(-time.Minute), 2, 55)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, cachedValue, "cached", now))

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "malformed", value: "not-base64-state"},
		{name: "wrong-block-count", value: collectorTestToken(t, now.Add(-time.Minute), 1, 56)},
		{name: "expired", value: collectorTestToken(t, now.Add(-20*time.Minute), 2, 57)},
	} {
		t.Run(test.name, func(t *testing.T) {
			incoming := make(http.Header)
			incoming.Set(openAICodexTurnStateHeader, test.value)

			snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "gpt-5.5", incoming, false)
			require.True(t, ok)
			require.Equal(t, cachedValue, snapshot.Token.Value)
			require.Empty(t, incoming.Get(openAICodexTurnStateHeader), "a rejected native value must not survive on the upstream request")
		})
	}
}

func TestPrepareCodexTurnStateIsolatesAccountScopeAndModel(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(601)
	otherAccount := codexTurnStateGatewayTestAccount(602)
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 66)
	baseContext := newCodexTurnStateGatewayTestContext(t, "execution-a")
	baseKey := svc.codexTurnStateKey(baseContext, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(baseKey, state, "base", now))

	tests := []struct {
		name    string
		account *Account
		scope   string
		model   string
		want    bool
	}{
		{name: "exact-key", account: account, scope: "execution-a", model: "gpt-5.5", want: true},
		{name: "other-account", account: otherAccount, scope: "execution-a", model: "gpt-5.5"},
		{name: "other-scope", account: account, scope: "execution-b", model: "gpt-5.5"},
		{name: "other-model", account: account, scope: "execution-a", model: "gpt-6-astra"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := newCodexTurnStateGatewayTestContext(t, test.scope)
			snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, test.account, test.model, make(http.Header), false)
			require.Equal(t, test.want, ok)
			if test.want {
				require.Equal(t, state, snapshot.Token.Value)
			} else {
				require.Empty(t, snapshot.Token.Value)
			}
		})
	}
}

func TestPrepareCodexTurnStateIncompleteProbeNeverOffersHeaderState(t *testing.T) {
	now := time.Now().UTC()
	candidate := collectorTestToken(t, now.Add(-time.Minute), 2, 33)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{candidate},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"model":"gpt-5.5"}}`,
			``,
		}, "\n"))),
	}}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(301)
	c := newCodexTurnStateGatewayTestContext(t, "execution-incomplete-probe")
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")

	snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "gpt-5.5", make(http.Header), true)

	require.False(t, ok)
	require.Empty(t, snapshot.Token.Value)
	_, usable := svc.codexTurnStateCollector.Acquire(key, time.Now())
	require.False(t, usable, "an HTTP 200 and header are insufficient without response.completed")
	metrics := svc.codexTurnStateCollector.Metrics()
	require.Zero(t, metrics.Offers)
	require.Zero(t, metrics.AcceptedOffers)
	require.Equal(t, uint64(1), svc.codexTurnStateProbeFailures.Load())
	require.Equal(t, "incomplete_stream", svc.codexTurnStateLastError.Load())
	require.NotNil(t, upstream.lastReq)
	require.Empty(t, upstream.lastReq.Header.Get(openAICodexTurnStateHeader), "probe requests must not replay an unverified candidate")
}

func TestPrepareCodexTurnStateClientCancellationDiscardsSuccessfulDetachedProbe(t *testing.T) {
	now := time.Now().UTC()
	candidate := collectorTestToken(t, now.Add(-time.Minute), 2, 34)
	release := make(chan struct{})
	body := &codexTurnStateReleasedBody{
		reader: strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"model":"gpt-5.5"}}`,
			``,
			`data: {"type":"response.completed","response":{"model":"gpt-5.5"}}`,
			``,
		}, "\n")),
		started: make(chan struct{}),
		release: release,
	}
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{candidate},
		},
		Body: body,
	}}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(302)
	c := newCodexTurnStateGatewayTestContext(t, "execution-cancelled-probe")
	clientCtx, cancelClient := context.WithCancel(c.Request.Context())
	defer cancelClient()
	c.Request = c.Request.WithContext(clientCtx)

	type prepareResult struct {
		snapshot OpenAICodexTurnStateSnapshot
		ok       bool
	}
	result := make(chan prepareResult, 1)
	go func() {
		// This mirrors the HTTP request builders, which receive a detached
		// generation context after downstream cancellation.
		snapshot, ok := svc.prepareCodexTurnState(context.WithoutCancel(clientCtx), c, account, "gpt-5.5", make(http.Header), true)
		result <- prepareResult{snapshot: snapshot, ok: ok}
	}()

	select {
	case <-body.started:
	case <-time.After(time.Second):
		t.Fatal("probe did not begin reading the upstream response")
	}
	cancelClient()
	close(release)

	var got prepareResult
	select {
	case got = <-result:
	case <-time.After(time.Second):
		t.Fatal("probe did not stop after client cancellation")
	}
	require.False(t, got.ok)
	require.Empty(t, got.snapshot.Token.Value)
	metrics := svc.codexTurnStateCollector.Metrics()
	require.Zero(t, metrics.Offers, "a late successful probe must not publish after its client disconnects")
	require.Zero(t, metrics.Entries, "a cancelled probe must not retain an empty collector entry")
	require.Equal(t, uint64(1), metrics.ProbeStarted)
	require.Equal(t, uint64(1), metrics.ProbeFinished)
	require.Equal(t, "cancelled", svc.codexTurnStateLastError.Load())
}

func TestPrepareCodexTurnStateCancelledClientNeverStartsProbe(t *testing.T) {
	upstream := &httpUpstreamRecorder{}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(304)
	c := newCodexTurnStateGatewayTestContext(t, "execution-cancelled-before-probe")
	clientCtx, cancelClient := context.WithCancel(c.Request.Context())
	cancelClient()
	c.Request = c.Request.WithContext(clientCtx)

	snapshot, ok := svc.prepareCodexTurnState(context.WithoutCancel(clientCtx), c, account, "gpt-5.5", make(http.Header), true)

	require.False(t, ok)
	require.Empty(t, snapshot.Token.Value)
	require.Empty(t, upstream.requests)
	metrics := svc.codexTurnStateCollector.Metrics()
	require.Zero(t, metrics.Entries)
	require.Zero(t, metrics.ProbeStarted)
	require.Zero(t, metrics.Offers)
}

func TestPrepareCodexTurnStateWaiterUsesClientRequestContext(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(303)
	c := newCodexTurnStateGatewayTestContext(t, "execution-cancelled-waiter")
	clientCtx, cancelClient := context.WithCancel(c.Request.Context())
	defer cancelClient()
	c.Request = c.Request.WithContext(clientCtx)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	probe, started := svc.codexTurnStateCollector.StartProbe(key, time.Now())
	require.True(t, started)
	defer svc.codexTurnStateCollector.FinishProbe(probe, time.Now())

	done := make(chan bool, 1)
	go func() {
		_, ok := svc.prepareCodexTurnState(context.WithoutCancel(clientCtx), c, account, "gpt-5.5", make(http.Header), true)
		done <- ok
	}()
	require.Eventually(t, func() bool {
		return svc.codexTurnStateCollector.Metrics().ProbeCoalesced == 1
	}, time.Second, time.Millisecond)
	cancelClient()

	select {
	case ok := <-done:
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("coalesced probe waiter ignored client cancellation")
	}
	require.Zero(t, svc.codexTurnStateCollector.Metrics().Offers)
}

func TestCodexTurnStateCollectionRequiresSuccessfulResponseTerminal(t *testing.T) {
	now := time.Now().UTC()
	state := collectorTestToken(t, now.Add(-time.Minute), 2, 67)

	t.Run("failed JSON", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		account := codexTurnStateGatewayTestAccount(603)
		c := newCodexTurnStateGatewayTestContext(t, "failed-json-terminal")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"resp_failed","model":"gpt-5.5","status":"failed","error":{"code":"server_error"},"usage":{"input_tokens":1,"output_tokens":0}}`)),
		}

		_, err := svc.handleNonStreamingResponse(context.Background(), resp, c, account, "gpt-5.5", "gpt-5.5")

		require.NoError(t, err)
		require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
	})

	t.Run("DONE without response completed", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		account := codexTurnStateGatewayTestAccount(604)
		c := newCodexTurnStateGatewayTestContext(t, "done-only-terminal")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
			},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`data: {"type":"response.output_text.delta","delta":"partial"}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"))),
		}

		_, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")

		require.NoError(t, err)
		require.Zero(t, svc.codexTurnStateCollector.Metrics().Entries)
	})

	t.Run("response completed followed by DONE", func(t *testing.T) {
		svc := newCodexTurnStateGatewayTestService(nil)
		account := codexTurnStateGatewayTestAccount(605)
		c := newCodexTurnStateGatewayTestContext(t, "completed-then-done-terminal")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{state},
			},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`data: {"type":"response.completed","response":{"id":"resp_completed_done","model":"gpt-5.5","status":"completed","usage":{"input_tokens":1,"output_tokens":1}}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"))),
		}

		_, err := svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "gpt-5.5", "gpt-5.5")

		require.NoError(t, err)
		require.Equal(t, 1, svc.codexTurnStateCollector.Metrics().Entries)
	})
}

func TestOpenAIJSONResponseCompletedRequiresAuthoritativeCompletion(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want bool
	}{
		{name: "completed response", body: `{"id":"resp_ok","status":"completed"}`, want: true},
		{name: "completed event", body: `{"type":"response.completed","response":{"id":"resp_ok"}}`, want: true},
		{name: "done event", body: `{"type":"response.done","response":{"id":"resp_ok"}}`, want: true},
		{name: "empty object", body: `{}`},
		{name: "missing status", body: `{"id":"resp_unknown","output":[]}`},
		{name: "in progress", body: `{"id":"resp_pending","status":"in_progress"}`},
		{name: "failed", body: `{"id":"resp_failed","status":"failed"}`},
		{name: "completed with error", body: `{"id":"resp_bad","status":"completed","error":{"code":"server_error"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, openAIJSONResponseCompleted(http.StatusOK, []byte(test.body)))
		})
	}
	require.False(t, openAIJSONResponseCompleted(http.StatusBadGateway, []byte(`{"status":"completed"}`)))
}

func TestReadCodexTurnStateProbeSSEExplicitFailureOverridesCompletion(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.failed","response":{"error":{"code":"server_is_overloaded"}}}`,
		``,
		`data: {"type":"response.completed","response":{"model":"gpt-5.5"}}`,
		``,
	}, "\n")

	_, completed, terminal, err := readCodexTurnStateProbeSSE(strings.NewReader(body), 64*1024)
	require.NoError(t, err)
	require.True(t, completed)
	require.Equal(t, "response.failed", terminal, "an explicit failure must prevent publication even if a later event claims completion")
}

func TestProbeCodexTurnStateUsesIsolatedReferenceEnvelope(t *testing.T) {
	installConfiguredCodexIdentityForOutboundTest(t)
	now := time.Now().UTC()
	candidate := collectorTestToken(t, now.Add(-time.Minute), 2, 77)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{candidate},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"model":"gpt-5.5"}}`,
			``,
			`data: {"type":"response.completed","response":{"model":"gpt-5.5"}}`,
			``,
		}, "\n"))),
	}}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(701)
	identity := make(http.Header)
	identity.Set("Authorization", "Bearer finalized-probe-credential")
	identity.Set("ChatGPT-Account-Id", "test-chatgpt-account")
	identity.Set("User-Agent", wantConfiguredCodexUA)
	identity.Set("Version", configuredCodexVersion)
	identity.Set("Originator", configuredCodexOriginator)
	identity.Set("OpenAI-Beta", "responses=experimental")
	identity.Set(openAICodexTurnStateHeader, "must-not-copy")
	identity.Set("session_id", "must-not-copy")
	identity.Set("conversation_id", "must-not-copy")
	identity.Set("previous_response_id", "must-not-copy")
	identity.Set("x-codex-turn-metadata", "must-not-copy")
	identity.Set("x-codex-window-id", "must-not-copy")
	identity.Set("x-client-request-id", "must-not-copy")

	token, failure := svc.probeCodexTurnState(context.Background(), account, "gpt-5.5", identity)
	require.Nil(t, failure)
	require.Equal(t, candidate, token.Value)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, chatgptCodexURL, upstream.lastReq.URL.String())
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.True(t, HTTPUpstreamRedirectsDisabled(upstream.lastReq.Context()))
	requireCodexOutboundIdentity(t, upstream.lastReq.Header, configuredCodexOutboundIdentity(), true)
	require.Equal(t, "Bearer finalized-probe-credential", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "test-chatgpt-account", upstream.lastReq.Header.Get("ChatGPT-Account-Id"))
	require.Equal(t, "responses=experimental", upstream.lastReq.Header.Get("OpenAI-Beta"))
	for _, name := range []string{
		openAICodexTurnStateHeader,
		"session_id",
		"conversation_id",
		"previous_response_id",
		"x-codex-turn-metadata",
		"x-codex-window-id",
		"x-client-request-id",
	} {
		require.Emptyf(t, upstream.lastReq.Header.Get(name), "synthetic probe must not carry per-conversation identity header %s", name)
	}

	var payload map[string]any
	require.NoError(t, json.Unmarshal(upstream.lastBody, &payload))
	require.Equal(t, "gpt-5.5", payload["model"])
	require.Equal(t, true, payload["stream"])
	require.Equal(t, false, payload["store"])
	require.Equal(t, true, payload["parallel_tool_calls"])
	require.NotContains(t, payload, "reasoning", "the collector must not force a reasoning effort")
	require.NotContains(t, payload, "previous_response_id")
	require.Equal(t, []any{"reasoning.encrypted_content"}, payload["include"])
	input, ok := payload["input"].([]any)
	require.True(t, ok)
	require.Len(t, input, 1)
	message, ok := input[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "message", message["type"])
	require.Equal(t, "user", message["role"])
	content, ok := message["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)
	block, ok := content[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "input_text", block["type"])
	probeText, ok := block["text"].(string)
	require.True(t, ok)
	require.NotEmpty(t, strings.TrimSpace(probeText))
	require.LessOrEqual(t, len(probeText), 64, "the collection prompt must stay small")
	require.Equal(t, probeText, payload["instructions"], "instructions and the one short user input must not request different output")
	require.NotContains(t, string(upstream.lastBody), "private prompt")
}

func TestProbeCodexTurnStateClassifiesTerminalAndHTTPFailures(t *testing.T) {
	now := time.Now().UTC()
	candidate := collectorTestToken(t, now.Add(-time.Minute), 2, 88)
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantCode   string
	}{
		{
			name:       "response-failed",
			statusCode: http.StatusOK,
			body:       `data: {"type":"response.failed","response":{"error":{"code":"server_is_overloaded"}}}` + "\n\n",
			wantCode:   "model_capacity",
		},
		{
			name:       "response-incomplete",
			statusCode: http.StatusOK,
			body:       `data: {"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}` + "\n\n",
			wantCode:   "incomplete_stream",
		},
		{name: "unauthorized", statusCode: http.StatusUnauthorized, wantCode: "upstream_401"},
		{name: "rate-limited", statusCode: http.StatusTooManyRequests, wantCode: "upstream_429"},
		{name: "server-error", statusCode: http.StatusServiceUnavailable, wantCode: "upstream_5xx"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var body io.ReadCloser = http.NoBody
			if test.body != "" {
				body = io.NopCloser(strings.NewReader(test.body))
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: test.statusCode,
				Header: http.Header{
					http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{candidate},
				},
				Body: body,
			}}
			svc := newCodexTurnStateGatewayTestService(upstream)

			token, failure := svc.probeCodexTurnState(context.Background(), codexTurnStateGatewayTestAccount(801), "gpt-5.5")
			require.Empty(t, token.Value)
			require.NotNil(t, failure)
			require.Equal(t, test.wantCode, failure.code)
		})
	}
}

func TestProbeCodexTurnStateBoundsAuthErrorBodyAndClassifiesAccount(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		wantCode   string
		accountID  int64
	}{
		{name: "unauthorized", statusCode: http.StatusUnauthorized, wantCode: "upstream_401", accountID: 811},
		{name: "forbidden", statusCode: http.StatusForbidden, wantCode: "upstream_403", accountID: 812},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := newCodexTurnStateBoundedBody(
				`{"detail":{"code":"deactivated_workspace","message":"Workspace is deactivated"}}` +
					strings.Repeat(" ", int(2*openAICodexTurnStateErrorMaxDefault)),
			)
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: test.statusCode,
				Header:     make(http.Header),
				Body:       body,
			}}
			svc := newCodexTurnStateGatewayTestService(upstream)
			account := codexTurnStateGatewayTestAccount(test.accountID)

			token, failure := svc.probeCodexTurnState(context.Background(), account, "gpt-5.5")
			require.Empty(t, token.Value)
			require.NotNil(t, failure)
			require.Equal(t, test.wantCode, failure.code)
			require.LessOrEqual(t, body.read, int(openAICodexTurnStateErrorMaxDefault), "auth classification must not consume an unbounded upstream body")
			require.True(t, body.closed)
			_, blocked := svc.openaiAccountRuntimeBlockUntil.Load(account.ID)
			require.True(t, blocked, "the bounded body must still reach the existing account access-state classifier")
		})
	}
}

func TestProbeCodexTurnStateRejectsResponseModelMismatch(t *testing.T) {
	now := time.Now().UTC()
	candidate := collectorTestToken(t, now.Add(-time.Minute), 2, 89)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{candidate},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"model":"gpt-5.5"}}`,
			``,
			`data: {"type":"response.completed","response":{"model":"gpt-6-astra"}}`,
			``,
		}, "\n"))),
	}}
	svc := newCodexTurnStateGatewayTestService(upstream)

	token, failure := svc.probeCodexTurnState(context.Background(), codexTurnStateGatewayTestAccount(802), "gpt-5.5")
	require.Empty(t, token.Value)
	require.NotNil(t, failure)
	require.Equal(t, "response_model_mismatch", failure.code)
	require.NotContains(t, failure.Error(), candidate)
}

func TestProbeCodexTurnStateUsesOnlyAllowlistedSSEErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		event    string
		wantCode string
	}{
		{
			name:     "capacity",
			event:    `{"type":"response.failed","response":{"error":{"code":"server_is_overloaded","message":"private organization SECRET"}}}`,
			wantCode: "model_capacity",
		},
		{
			name:     "slow-down",
			event:    `{"type":"error","error":{"code":"slow_down","message":"SECRET"}}`,
			wantCode: "model_capacity",
		},
		{
			name:     "rate-limit",
			event:    `{"type":"error","code":"rate_limit_exceeded","message":"SECRET"}`,
			wantCode: "upstream_rate_limited",
		},
		{
			name:     "quota",
			event:    `{"type":"response.failed","response":{"error":{"code":"insufficient_quota","message":"SECRET"}}}`,
			wantCode: "upstream_rate_limited",
		},
		{
			name:     "unknown-code",
			event:    `{"type":"response.failed","response":{"error":{"code":"SECRET-new-code","message":"SECRET private text"}}}`,
			wantCode: "response_failed",
		},
		{
			name:     "message-is-not-evidence",
			event:    `{"type":"response.failed","response":{"error":{"message":"server_is_overloaded rate_limit_exceeded SECRET"}}}`,
			wantCode: "response_failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("data: " + test.event + "\n\n")),
			}}
			svc := newCodexTurnStateGatewayTestService(upstream)

			token, failure := svc.probeCodexTurnState(context.Background(), codexTurnStateGatewayTestAccount(803), "gpt-5.5")
			require.Empty(t, token.Value)
			require.NotNil(t, failure)
			require.Equal(t, test.wantCode, failure.code)
			require.NotContains(t, failure.Error(), "SECRET", "free-form upstream error text and unknown codes must not escape")
		})
	}
}

func TestPrepareCodexTurnState429RetryAfterInstallsAccountCooldown(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header: http.Header{
			"Retry-After": []string{"120"},
		},
		Body: http.NoBody,
	}}
	svc := newCodexTurnStateGatewayTestService(upstream)
	account := codexTurnStateGatewayTestAccount(804)
	c := newCodexTurnStateGatewayTestContext(t, "execution-retry-after")
	startedAt := time.Now()

	snapshot, ok := svc.prepareCodexTurnState(context.Background(), c, account, "gpt-5.5", make(http.Header), true)
	require.False(t, ok)
	require.Empty(t, snapshot.Token.Value)
	rawUntil, blocked := svc.openaiAccountRuntimeBlockUntil.Load(account.ID)
	require.True(t, blocked, "probe 429 must share its quota boundary with normal account scheduling")
	until, ok := rawUntil.(time.Time)
	require.True(t, ok)
	require.False(t, until.Before(startedAt.Add(119*time.Second)), "Retry-After must not be shortened")
	_, retryStateExists := svc.openaiOAuth429RetryStartedAt.Load(account.ID)
	require.False(t, retryStateExists, "an auxiliary probe has no same-account generation retry path")
}

func TestProbeCodexTurnStateSSERateLimitInstallsCooldownWithoutRetryState(t *testing.T) {
	for index, eventCode := range []string{"rate_limit_exceeded", "insufficient_quota"} {
		t.Run(eventCode, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Retry-After": []string{"90"},
				},
				Body: io.NopCloser(strings.NewReader(
					`data: {"type":"response.failed","response":{"error":{"code":"` + eventCode + `"}}}` + "\n\n",
				)),
			}}
			svc := newCodexTurnStateGatewayTestService(upstream)
			account := codexTurnStateGatewayTestAccount(int64(820 + index))
			startedAt := time.Now()

			token, failure := svc.probeCodexTurnState(context.Background(), account, "gpt-5.5")
			require.Empty(t, token.Value)
			require.NotNil(t, failure)
			require.Equal(t, "upstream_rate_limited", failure.code)
			rawUntil, blocked := svc.openaiAccountRuntimeBlockUntil.Load(account.ID)
			require.True(t, blocked)
			until, ok := rawUntil.(time.Time)
			require.True(t, ok)
			require.False(t, until.Before(startedAt.Add(89*time.Second)), "SSE Retry-After must not be shortened")
			_, retryStateExists := svc.openaiOAuth429RetryStartedAt.Load(account.ID)
			require.False(t, retryStateExists, "an SSE probe failure must not reserve a generation retry")
		})
	}
}

func TestProbeCodexTurnStatePropagatesCancellationWithoutCandidate(t *testing.T) {
	upstream := &httpUpstreamRecorder{err: context.Canceled}
	svc := newCodexTurnStateGatewayTestService(upstream)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	token, failure := svc.probeCodexTurnState(ctx, codexTurnStateGatewayTestAccount(901), "gpt-5.5")
	require.Empty(t, token.Value)
	require.NotNil(t, failure)
	require.ErrorIs(t, failure, context.Canceled)
	require.Equal(t, "cancelled", failure.code)
}

func TestCodexTurnStateReliabilityJSONNeverLeaksCollectorIdentity(t *testing.T) {
	svc := newCodexTurnStateGatewayTestService(nil)
	account := codexTurnStateGatewayTestAccount(1001)
	const (
		scope = "raw-execution-scope-must-not-leak"
		route = "socks5://raw-proxy-credential-must-not-leak:1080"
	)
	c := newCodexTurnStateGatewayTestContext(t, scope)
	now := time.Now().UTC()
	value := collectorTestToken(t, now.Add(-time.Minute), 2, 99)
	key := svc.codexTurnStateKey(c, account, "gpt-5.5")
	require.True(t, svc.codexTurnStateCollector.OfferValueMust(key, value, route, now))
	svc.recordCodexTurnStateProbeFailure("upstream_429")

	snapshot := svc.CodexTurnStateReliabilitySnapshot(context.Background())
	projected := reliabilityTurnStateCollectorFromSnapshot(snapshot)
	encoded, err := json.Marshal(projected)
	require.NoError(t, err)
	jsonText := string(encoded)
	require.NotContains(t, jsonText, value)
	require.NotContains(t, jsonText, scope)
	require.NotContains(t, jsonText, route)
	require.NotContains(t, jsonText, "test-access-token")
	require.NotContains(t, jsonText, "test-chatgpt-account")
	require.Equal(t, "upstream_429", projected.LastErrorCode)
	require.Equal(t, 1, projected.ActiveEntries)

	for _, code := range []string{
		"model_capacity",
		"upstream_rate_limited",
		"response_failed",
		"response_model_mismatch",
		"upstream_5xx",
		"cancelled",
	} {
		t.Run(code, func(t *testing.T) {
			got := reliabilityTurnStateCollectorFromSnapshot(OpenAICodexTurnStateReliabilitySnapshot{LastErrorCode: code})
			require.Equal(t, code, got.LastErrorCode)
		})
	}
}
