package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/config"
)

type serializedSettingWriteRepo struct {
	mu sync.Mutex

	values       map[string]string
	activeWrites int
	maxActive    int
	writeCount   int

	firstWriteEntered  chan struct{}
	firstWriteReturned chan struct{}
	secondWriteEntered chan struct{}
}

func newSerializedSettingWriteRepo() *serializedSettingWriteRepo {
	return &serializedSettingWriteRepo{
		values:             make(map[string]string),
		firstWriteEntered:  make(chan struct{}),
		firstWriteReturned: make(chan struct{}),
		secondWriteEntered: make(chan struct{}),
	}
}

func (r *serializedSettingWriteRepo) Get(context.Context, string) (*Setting, error) {
	panic("unexpected Get call")
}

func (r *serializedSettingWriteRepo) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (r *serializedSettingWriteRepo) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (r *serializedSettingWriteRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (r *serializedSettingWriteRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	r.mu.Lock()
	r.writeCount++
	writeNumber := r.writeCount
	r.activeWrites++
	if r.activeWrites > r.maxActive {
		r.maxActive = r.activeWrites
	}
	for key, value := range settings {
		r.values[key] = value
	}
	switch writeNumber {
	case 1:
		close(r.firstWriteEntered)
	case 2:
		close(r.secondWriteEntered)
	}
	r.activeWrites--
	if writeNumber == 1 {
		close(r.firstWriteReturned)
	}
	r.mu.Unlock()
	return nil
}

func (r *serializedSettingWriteRepo) GetAll(context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, nil
}

func (r *serializedSettingWriteRepo) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func (r *serializedSettingWriteRepo) snapshot() (map[string]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	values := make(map[string]string, len(r.values))
	for key, value := range r.values {
		values[key] = value
	}
	return values, r.maxActive
}

func TestSettingServiceSerializesWriteAndCacheRefreshAcrossUpdateEntrypoints(t *testing.T) {
	accountIdentityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	t.Cleanup(func() {
		SetCodexAccountLocalDeviceIdentityEnabled(accountIdentityEnabled)
	})

	repo := newSerializedSettingWriteRepo()
	svc := NewSettingService(repo, &config.Config{})
	// Force the first update to stop inside refreshCachedSettings, after it has
	// published the Originator/UA cache but before it can invalidate the Version
	// cache. This proves the update mutex spans both repository persistence and
	// the entire cache refresh; merely serializing SetMultiple is insufficient.
	svc.openAICodexResponsesVersionMu.Lock()
	var releaseVersionMuOnce sync.Once
	releaseVersionMu := func() {
		releaseVersionMuOnce.Do(svc.openAICodexResponsesVersionMu.Unlock)
	}
	t.Cleanup(releaseVersionMu)

	firstSettings := &SystemSettings{
		OpenAICodexOriginator:                  "first-client",
		EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
	}
	secondSettings := &SystemSettings{
		OpenAICodexOriginator:                  "second-client",
		EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- svc.UpdateSettings(context.Background(), firstSettings)
	}()

	select {
	case <-repo.firstWriteEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first settings write")
	}
	select {
	case <-repo.firstWriteReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first repository write to return")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		cached, _ := svc.openAICodexOriginatorCache.Load().(*cachedOpenAICodexHeaderOverride)
		if cached != nil && cached.value == "first-client" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for first update to reach cache refresh")
		}
		time.Sleep(time.Millisecond)
	}

	secondStarted := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondDone <- svc.UpdateSettingsWithAuthSourceDefaults(context.Background(), secondSettings, nil)
	}()
	<-secondStarted

	// The first repository write has already returned and the update is blocked
	// later inside cache refresh. The second update must still remain outside
	// SetMultiple until that refresh finishes.
	select {
	case <-repo.secondWriteEntered:
		releaseVersionMu()
		t.Fatal("second settings write overlapped the first write/cache refresh transaction")
	case <-time.After(100 * time.Millisecond):
	}
	releaseVersionMu()

	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)

	values, maxActive := repo.snapshot()
	require.Equal(t, 1, maxActive)
	require.Equal(t, "second-client", values[SettingKeyOpenAICodexOriginator])
	require.Equal(t, "second-client", svc.GetOpenAICodexOriginator(context.Background()))
}

func TestSettingNotificationsSerializeWithoutExternalCacheRollback(t *testing.T) {
	accountIdentityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	t.Cleanup(func() {
		SetCodexAccountLocalDeviceIdentityEnabled(accountIdentityEnabled)
	})

	repo := newSerializedSettingWriteRepo()
	svc := NewSettingService(repo, &config.Config{})

	firstObserverRead := make(chan struct{})
	releaseFirstObserver := make(chan struct{})
	observersFinished := make(chan struct{})
	var observerCount atomic.Int32
	var observerActive atomic.Int32
	var observerMaxActive atomic.Int32
	var externalOriginator atomic.Value
	externalOriginator.Store("")

	svc.SetOnUpdateCallback(func() {
		active := observerActive.Add(1)
		for {
			maximum := observerMaxActive.Load()
			if active <= maximum || observerMaxActive.CompareAndSwap(maximum, active) {
				break
			}
		}

		values, _ := repo.snapshot()
		originator := values[SettingKeyOpenAICodexOriginator]
		call := observerCount.Add(1)
		if call == 1 {
			close(firstObserverRead)
			<-releaseFirstObserver
		}
		externalOriginator.Store(originator)
		observerActive.Add(-1)
		if call == 2 {
			close(observersFinished)
		}
	})

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- svc.UpdateSettings(context.Background(), &SystemSettings{
			OpenAICodexOriginator:                  "first-client",
			EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
		})
	}()

	select {
	case <-firstObserverRead:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the first observer to capture its DB snapshot")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- svc.UpdateSettings(context.Background(), &SystemSettings{
			OpenAICodexOriginator:                  "second-client",
			EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
		})
	}()

	select {
	case err := <-secondDone:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("second settings update was blocked by an external observer")
	}
	require.Equal(t, int32(1), observerCount.Load(), "a newer observer must not run concurrently")

	close(releaseFirstObserver)
	require.NoError(t, <-firstDone)
	select {
	case <-observersFinished:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the coalesced newest-revision observer")
	}

	require.Equal(t, int32(1), observerMaxActive.Load())
	require.Equal(t, int32(2), observerCount.Load())
	require.Equal(t, "second-client", externalOriginator.Load())
}

func TestSettingNotificationCallbackCanReenterUpdate(t *testing.T) {
	accountIdentityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	t.Cleanup(func() {
		SetCodexAccountLocalDeviceIdentityEnabled(accountIdentityEnabled)
	})

	repo := newSerializedSettingWriteRepo()
	svc := NewSettingService(repo, &config.Config{})
	var callbackOnce sync.Once
	var callbackCount atomic.Int32
	var nestedErr atomic.Value
	svc.SetOnUpdateCallback(func() {
		callbackCount.Add(1)
		callbackOnce.Do(func() {
			if err := svc.UpdateSettings(context.Background(), &SystemSettings{
				OpenAICodexOriginator:                  "nested-client",
				EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
			}); err != nil {
				nestedErr.Store(err)
			}
		})
	})

	done := make(chan error, 1)
	go func() {
		done <- svc.UpdateSettings(context.Background(), &SystemSettings{
			OpenAICodexOriginator:                  "outer-client",
			EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
		})
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("settings observer re-entry deadlocked")
	}
	if value := nestedErr.Load(); value != nil {
		require.NoError(t, value.(error))
	}
	values, _ := repo.snapshot()
	require.Equal(t, "nested-client", values[SettingKeyOpenAICodexOriginator])
	require.Equal(t, "nested-client", svc.GetOpenAICodexOriginator(context.Background()))
	require.GreaterOrEqual(t, callbackCount.Load(), int32(2))
}

func TestSettingNotificationSkipsLateOlderRevision(t *testing.T) {
	svc := NewSettingService(newSerializedSettingWriteRepo(), &config.Config{})
	var callbackCount atomic.Int32
	svc.SetOnUpdateCallback(func() {
		callbackCount.Add(1)
	})

	svc.notifySettingsUpdated(2)
	svc.notifySettingsUpdated(1)

	require.Equal(t, int32(1), callbackCount.Load())
}

func TestSettingNotificationRecoversObserverPanic(t *testing.T) {
	accountIdentityEnabled := codexAccountLocalDeviceIdentityEnabled.Load()
	t.Cleanup(func() {
		SetCodexAccountLocalDeviceIdentityEnabled(accountIdentityEnabled)
	})

	repo := newSerializedSettingWriteRepo()
	svc := NewSettingService(repo, &config.Config{})
	var callbackCount atomic.Int32
	var listenerCount atomic.Int32
	svc.SetOnUpdateCallback(func() {
		if callbackCount.Add(1) == 1 {
			panic("injected observer failure")
		}
	})
	unsubscribe := svc.SubscribeChannelMonitorRuntime(func() {
		listenerCount.Add(1)
	})
	t.Cleanup(unsubscribe)

	require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{
		OpenAICodexOriginator:                  "first-client",
		EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
	}))
	require.Equal(t, int32(1), callbackCount.Load())
	require.Equal(t, int32(1), listenerCount.Load(), "other observers must survive one callback panic")

	require.NoError(t, svc.UpdateSettings(context.Background(), &SystemSettings{
		OpenAICodexOriginator:                  "second-client",
		EnableOpenAIAccountLocalDeviceIdentity: accountIdentityEnabled,
	}))
	require.Equal(t, int32(2), callbackCount.Load(), "a panic must not poison the notification drainer")
	require.Equal(t, int32(2), listenerCount.Load())
}
