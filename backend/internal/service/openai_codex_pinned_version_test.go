package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAICodexPinnedVersionSelection(t *testing.T) {
	for _, tt := range []struct {
		name, mode, manual, synced, want string
		pinned                           bool
	}{
		{"legacy automatic floor", "", "0.125.0", "0.200.0", "0.200.0", false},
		{"automatic builtin floor", "auto", "0.125.0", "", codexResponsesVersionFallback, false},
		{"fixed historical stable", "pinned", "0.125.0", "0.200.0", "0.125.0", true},
		{"fixed higher version", "pinned", "0.300.0", "0.400.0", "0.300.0", true},
		{"invalid stored pin falls back", "pinned", "latest", "0.200.0", "0.200.0", false},
		{"stored prerelease pin falls back", "pinned", "0.200.0-alpha.1", "", codexResponsesVersionFallback, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := newCodexVersionSyncSettingRepoStub(map[string]string{
				SettingKeyOpenAICodexClientVersionMode: tt.mode, SettingKeyOpenAICodexClientVersion: tt.manual, SettingKeyOpenAICodexClientVersionSynced: tt.synced,
			})
			svc := NewSettingService(repo, nil)
			for i := 0; i < 2; i++ {
				version, sourceOK, _, pinned := svc.getOpenAICodexResponsesVersionWithMode(context.Background())
				require.Equal(t, tt.want, version)
				require.True(t, sourceOK)
				require.Equal(t, tt.pinned, pinned)
				require.Contains(t, svc.GetOpenAICodexCanonicalUserAgent(context.Background()), "/"+tt.want+" ")
			}
		})
	}
}

func TestOpenAICodexPinnedVersionSyncDoesNotLiftPin(t *testing.T) {
	for _, reloadFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful reload", true: "transient reload failure"}[reloadFails], func(t *testing.T) {
			repo := newCodexVersionSyncSettingRepoStub(map[string]string{
				SettingKeyOpenAICodexClientVersionMode: "pinned", SettingKeyOpenAICodexClientVersion: "0.125.0", SettingKeyOpenAICodexClientVersionSynced: codexResponsesVersionFallback,
			})
			settings := NewSettingService(repo, nil)
			require.Equal(t, "0.125.0", settings.GetOpenAICodexResponsesVersion(context.Background()))
			if reloadFails {
				repo.setGetMultipleError(errors.New("temporary read error"))
			}
			syncer := NewOpenAICodexVersionSyncService(repo, settings, &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.0"}}, time.Hour)
			syncer.runOnce()
			require.Equal(t, []string{"0.200.0"}, repo.syncedWrites())
			version, sourceOK, _, pinned := settings.getOpenAICodexResponsesVersionWithMode(context.Background())
			require.Equal(t, "0.125.0", version)
			require.True(t, pinned)
			require.Equal(t, !reloadFails, sourceOK)
		})
	}
}

func TestOpenAICodexPinnedVersionInvalidationAndPublication(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionMode: "pinned", SettingKeyOpenAICodexClientVersion: "0.125.0", SettingKeyOpenAICodexClientVersionSynced: "0.200.0",
	})
	svc := NewSettingService(repo, nil)
	require.Equal(t, "0.125.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
	oldEpoch := svc.openAICodexResponsesVersionEpoch.Load()
	svc.PublishOpenAICodexResponsesVersion("0.300.0")
	require.Equal(t, "0.125.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
	repo.mu.Lock()
	repo.values[SettingKeyOpenAICodexClientVersionMode] = "auto"
	repo.mu.Unlock()
	svc.InvalidateOpenAICodexClientVersionCache()
	_, stored := svc.storeOpenAICodexResponsesVersionModeAtEpoch("0.125.0", time.Minute, oldEpoch, true, true)
	require.False(t, stored)
	require.Equal(t, "0.200.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
	repo.mu.Lock()
	repo.values[SettingKeyOpenAICodexClientVersionMode] = "pinned"
	repo.values[SettingKeyOpenAICodexClientVersion] = "0.124.0"
	repo.mu.Unlock()
	svc.InvalidateOpenAICodexClientVersionCache()
	require.Equal(t, "0.124.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
}

func TestOpenAICodexPinnedVersionWinsAgainstInflightAutomaticRead(t *testing.T) {
	repo := &blockingCodexVersionSettingRepo{codexVersionSyncSettingRepoStub: newCodexVersionSyncSettingRepoStub(map[string]string{SettingKeyOpenAICodexClientVersionSynced: "0.300.0"}), started: make(chan struct{}), release: make(chan struct{})}
	svc := NewSettingService(repo, nil)
	result := make(chan string, 1)
	go func() { result <- svc.GetOpenAICodexResponsesVersion(context.Background()) }()
	<-repo.started
	repo.mu.Lock()
	repo.values[SettingKeyOpenAICodexClientVersionMode] = "pinned"
	repo.values[SettingKeyOpenAICodexClientVersion] = "0.125.0"
	repo.mu.Unlock()
	svc.InvalidateOpenAICodexClientVersionCache()
	close(repo.release)
	select {
	case got := <-result:
		require.Equal(t, "0.125.0", got)
	case <-time.After(time.Second):
		t.Fatal("version read did not retry invalidated epoch")
	}
}

func TestOpenAICodexPinnedVersionReachesAllOutboundPaths(t *testing.T) {
	installConfiguredCodexIdentityForOutboundTest(t)
	repo := &codexHeaderSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexOriginator: configuredCodexOriginator, SettingKeyOpenAICodexUserAgent: configuredCodexUserAgent,
		SettingKeyOpenAICodexClientVersionMode: "pinned", SettingKeyOpenAICodexClientVersion: "0.125.0", SettingKeyOpenAICodexClientVersionSynced: "0.300.0",
	}}
	settings := NewSettingService(repo, nil)
	SetCodexCanonicalUserAgentResolver(func() string { return settings.GetOpenAICodexCanonicalUserAgent(context.Background()) })
	SetCodexCanonicalOriginatorResolver(func() string { return settings.GetOpenAICodexOriginator(context.Background()) })
	SetCodexCanonicalResponsesVersionResolver(func() string { return settings.GetOpenAICodexResponsesVersion(context.Background()) })
	for _, local := range []bool{false, true} {
		t.Run(map[bool]string{false: "global pin", true: "local identity still wins"}[local], func(t *testing.T) {
			want := expectedCodexOutboundIdentity{configuredCodexOriginator, "system-gpt-client/0.125.0 (Linux 6.8; x86_64) xterm", "0.125.0"}
			var extra map[string]any
			if local {
				want = expectedCodexOutboundIdentity{"codex-tui", "codex-tui/0.120.0 (Linux; x86_64) xterm", "0.120.0"}
				extra = map[string]any{OpenAILocalDeviceOriginatorExtraKey: want.originator, OpenAILocalDeviceUserAgentExtraKey: want.userAgent, OpenAILocalDeviceVersionExtraKey: want.version}
			}
			t.Run("HTTP", func(t *testing.T) { testCodexIdentityHTTPResponsesUpstream(t, extra, want, false) })
			t.Run("passthrough", func(t *testing.T) { testCodexIdentityHTTPResponsesUpstream(t, extra, want, true) })
			t.Run("WebSocket", func(t *testing.T) { testCodexIdentityWSResponsesHandshake(t, extra, want) })
			t.Run("WHAM", func(t *testing.T) { testCodexIdentityAllWhamRequestsWithoutVersion(t, extra, want) })
			t.Run("input_tokens", func(t *testing.T) { testCodexIdentityInputTokensUpstreamWithoutVersion(t, extra, want) })
		})
	}
}

func TestOpenAICodexPinnedVersionReloadsExpiredSnapshot(t *testing.T) {
	for _, mode := range []string{"auto", "pinned"} {
		t.Run(mode, func(t *testing.T) {
			repo := newCodexVersionSyncSettingRepoStub(map[string]string{SettingKeyOpenAICodexClientVersionMode: "pinned", SettingKeyOpenAICodexClientVersion: "0.125.0", SettingKeyOpenAICodexClientVersionSynced: "0.200.0"})
			svc := NewSettingService(repo, nil)
			require.Equal(t, "0.125.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
			// Simulate a settings save on another instance: no local epoch invalidation.
			repo.mu.Lock()
			repo.values[SettingKeyOpenAICodexClientVersionMode] = mode
			repo.values[SettingKeyOpenAICodexClientVersion] = "0.124.0"
			repo.mu.Unlock()
			cached := *svc.openAICodexResponsesVersionCache.Load().(*cachedOpenAICodexResponsesVersion)
			cached.expiresAt = time.Now().Add(-time.Second).UnixNano()
			svc.openAICodexResponsesVersionCache.Store(&cached)
			want := "0.124.0"
			if mode == "auto" {
				want = "0.200.0"
			}
			version, ok, _, pinned := svc.getOpenAICodexResponsesVersionWithMode(context.Background())
			require.True(t, ok)
			require.Equal(t, want, version)
			require.Equal(t, mode == "pinned", pinned)
		})
	}
}

func TestOpenAICodexPinnedVersionColdPublishRespectsPersistedMode(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{SettingKeyOpenAICodexClientVersionMode: "pinned", SettingKeyOpenAICodexClientVersion: "0.125.0"})
	svc := NewSettingService(repo, nil)
	for i := 0; i < 2; i++ {
		svc.PublishOpenAICodexResponsesVersion("0.300.0")
		require.Equal(t, "0.125.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
		svc.InvalidateOpenAICodexClientVersionCache()
	}
}
