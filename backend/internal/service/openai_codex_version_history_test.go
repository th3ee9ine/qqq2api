package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type codexVersionHistoryGitHubStub struct {
	GitHubReleaseClient
	mu          sync.Mutex
	latest      *GitHubRelease
	pages       map[int][]*GitHubRelease
	err         error
	latestCalls int
	pageCalls   []int
	entered     chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (c *codexVersionHistoryGitHubStub) FetchLatestRelease(ctx context.Context, _ string) (*GitHubRelease, error) {
	c.mu.Lock()
	c.latestCalls++
	err, latest := c.err, c.latest
	c.mu.Unlock()
	if c.entered != nil {
		c.once.Do(func() { close(c.entered) })
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.release:
		}
	}
	return latest, err
}

func (c *codexVersionHistoryGitHubStub) FetchReleasesPage(_ context.Context, _ string, page, _ int) ([]*GitHubRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pageCalls = append(c.pageCalls, page)
	return c.pages[page], c.err
}

func codexPrereleasePage() []*GitHubRelease {
	page := make([]*GitHubRelease, openAICodexVersionSyncPerPage)
	for i := range page {
		page[i] = &GitHubRelease{TagName: "rust-v0.201.0-alpha.1", Prerelease: true}
	}
	return page
}

func TestOpenAICodexVersionHistoryFiltersSortsAndCaches(t *testing.T) {
	github := &codexVersionHistoryGitHubStub{
		latest: &GitHubRelease{TagName: "rust-v0.200.1"},
		pages: map[int][]*GitHubRelease{1: {
			{TagName: "rust-v0.150.1", PublishedAt: "2026-08-01T00:00:00Z", HTMLURL: "https://github.com/openai/codex/releases/tag/rust-v0.150.1"},
			{TagName: "rust-v0.200.1"}, {TagName: "rust-v0.150.1"},
			{TagName: "rust-v0.999.0", Draft: true}, {TagName: "rust-v0.998.0", Prerelease: true},
			{TagName: "rust-v0.200.2-alpha.1"}, {TagName: "rusty-v8-v999.1.0"}, {TagName: "rust-vv0.200.0"}, nil,
		}},
	}
	svc := NewOpenAICodexVersionSyncService(nil, nil, github, 0)
	result, err := svc.ListVersions(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "0.200.1", result.LatestVersion)
	require.Len(t, result.Versions, 2)
	require.Equal(t, "0.200.1", result.Versions[0].Version)
	require.Equal(t, "rust-v0.150.1", result.Versions[1].TagName)
	require.NotEmpty(t, result.Versions[1].PublishedAt)
	require.False(t, result.HasMore)
	require.Nil(t, result.NextPage)
	result.Versions[0].Version = "mutated"
	cached, err := svc.ListVersions(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "0.200.1", cached.Versions[0].Version)
	require.Equal(t, 1, github.latestCalls)
	require.Equal(t, []int{1}, github.pageCalls)
}

func TestOpenAICodexVersionHistoryAdvancesAcrossDensePrereleases(t *testing.T) {
	github := &codexVersionHistoryGitHubStub{
		latest: &GitHubRelease{TagName: "rust-v0.200.1"},
		pages:  map[int][]*GitHubRelease{1: codexPrereleasePage(), 2: codexPrereleasePage(), 3: codexPrereleasePage(), 4: {{TagName: "rust-v0.180.0"}}},
	}
	svc := NewOpenAICodexVersionSyncService(nil, nil, github, 0)
	result, err := svc.ListVersions(context.Background(), 1)
	require.NoError(t, err)
	require.Empty(t, result.Versions)
	require.True(t, result.HasMore)
	require.Equal(t, 4, *result.NextPage)
	result, err = svc.ListVersions(context.Background(), *result.NextPage)
	require.NoError(t, err)
	require.Equal(t, "0.180.0", result.Versions[0].Version)
	require.Equal(t, "0.200.1", result.LatestVersion)
	require.False(t, result.HasMore)
}

func TestOpenAICodexVersionHistoryCollapsesConcurrentFetches(t *testing.T) {
	github := &codexVersionHistoryGitHubStub{
		latest:  &GitHubRelease{TagName: "rust-v0.200.1"},
		pages:   map[int][]*GitHubRelease{1: {{TagName: "rust-v0.200.1"}}},
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	svc := NewOpenAICodexVersionSyncService(nil, nil, github, 0)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.ListVersions(context.Background(), 1)
			require.NoError(t, err)
		}()
	}
	<-github.entered
	close(github.release)
	wg.Wait()
	require.Equal(t, 1, github.latestCalls)
	require.Equal(t, []int{1}, github.pageCalls)
}

func TestOpenAICodexVersionHistoryErrorsAreNotCached(t *testing.T) {
	github := &codexVersionHistoryGitHubStub{err: errors.New("GitHub unavailable")}
	svc := NewOpenAICodexVersionSyncService(nil, nil, github, 0)
	_, err := svc.ListVersions(context.Background(), 1)
	require.Error(t, err)
	github.err = nil
	github.latest = &GitHubRelease{TagName: "rust-v0.200.1"}
	github.pages = map[int][]*GitHubRelease{1: {{TagName: "rust-v0.200.1"}}}
	result, err := svc.ListVersions(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, result.Versions, 1)
	_, err = svc.ListVersions(context.Background(), 0)
	require.Error(t, err)
	_, err = svc.ListVersions(context.Background(), OpenAICodexHistoryMaxPage+1)
	require.Error(t, err)
}

func TestOpenAICodexManualSyncIgnoresAutoSwitchAndPreservesPinnedVersion(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexVersionAutoSyncEnabled: "false",
		SettingKeyOpenAICodexClientVersion:          "0.100.0",
		SettingKeyOpenAICodexClientVersionMode:      "pinned",
		SettingKeyOpenAICodexOriginator:             "my-client",
		SettingKeyOpenAICodexUserAgent:              "my-client/0.100.0 (Linux)",
	})
	settings := NewSettingService(repo, nil)
	github := &codexVersionHistoryGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}
	svc := NewOpenAICodexVersionSyncService(repo, settings, github, 0)
	require.Equal(t, "0.100.0", settings.GetOpenAICodexResponsesVersion(context.Background()))
	result, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.True(t, result.Updated)
	require.Equal(t, "0.200.1", result.LatestVersion)
	require.Equal(t, "0.200.1", result.SyncedVersion)
	require.Equal(t, "0.200.1", result.Defaults.ClientVersion)
	require.Equal(t, "0.100.0", settings.GetOpenAICodexResponsesVersion(context.Background()))
	require.Equal(t, "pinned", repo.values[SettingKeyOpenAICodexClientVersionMode])
	require.Equal(t, "false", repo.values[SettingKeyOpenAICodexVersionAutoSyncEnabled])
	require.Equal(t, "my-client", repo.values[SettingKeyOpenAICodexOriginator])
	require.Equal(t, "my-client/0.100.0 (Linux)", repo.values[SettingKeyOpenAICodexUserAgent])
	result, err = svc.SyncNow(context.Background())
	require.NoError(t, err)
	require.False(t, result.Updated)
	require.Equal(t, 2, github.latestCalls, "manual synchronization always checks GitHub")
}

func TestOpenAICodexManualSyncReportsFetchAndPersistenceFailures(t *testing.T) {
	for _, fail := range []string{"fetch", "persist"} {
		t.Run(fail, func(t *testing.T) {
			repo := newCodexVersionSyncSettingRepoStub(map[string]string{SettingKeyOpenAICodexClientVersionSynced: "0.180.0"})
			github := &codexVersionHistoryGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}
			if fail == "fetch" {
				github.err = errors.New("offline")
			} else {
				repo.setErr = errors.New("database down")
			}
			svc := NewOpenAICodexVersionSyncService(repo, NewSettingService(repo, nil), github, 0)
			result, err := svc.SyncNow(context.Background())
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, "0.180.0", repo.values[SettingKeyOpenAICodexClientVersionSynced])
			require.Empty(t, repo.syncedWrites())
		})
	}
}

func TestOpenAICodexVersionHistoryUnknownLatestStillAllowsNextPage(t *testing.T) {
	github := &codexVersionHistoryGitHubStub{
		latest: &GitHubRelease{TagName: "rusty-v8-v999.1.0"},
		pages:  map[int][]*GitHubRelease{1: codexPrereleasePage(), 2: codexPrereleasePage(), 3: codexPrereleasePage(), 4: {{TagName: "rust-v0.180.0"}}},
	}
	svc := NewOpenAICodexVersionSyncService(nil, nil, github, 0)
	result, err := svc.ListVersions(context.Background(), 1)
	require.NoError(t, err)
	require.Empty(t, result.LatestVersion)
	require.Empty(t, result.Versions)
	require.True(t, result.HasMore)
	require.Equal(t, 4, *result.NextPage)
	result, err = svc.ListVersions(context.Background(), *result.NextPage)
	require.NoError(t, err)
	require.Equal(t, "0.180.0", result.Versions[0].Version)
	require.False(t, result.HasMore)
}

func TestOpenAICodexManualSyncDoesNotRestorePinAcrossConcurrentSettingSave(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersion:     "0.100.0",
		SettingKeyOpenAICodexClientVersionMode: "pinned",
	})
	settings := NewSettingService(repo, nil)
	repo.beforeCAS = func(string) {
		repo.mu.Lock()
		repo.values[SettingKeyOpenAICodexClientVersionMode] = "auto"
		repo.getMultiErr = errors.New("transient post-save database failure")
		repo.mu.Unlock()
		settings.InvalidateOpenAICodexClientVersionCache()
	}
	github := &codexVersionHistoryGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}
	svc := NewOpenAICodexVersionSyncService(repo, settings, github, 0)
	_, err := svc.SyncNow(context.Background())
	require.NoError(t, err)
	_, _, _, pinned := settings.getOpenAICodexResponsesVersionWithMode(context.Background())
	require.False(t, pinned, "an older pin must not be restored over a newer settings save")
}
