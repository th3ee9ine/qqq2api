package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

// --- mock: 只实现同步任务用到的读写，其余方法不应被调用 ---

type codexVersionSyncSettingRepoStub struct {
	SettingRepository // 嵌入接口，未实现的方法会 panic（不应被调用）

	mu          sync.Mutex
	values      map[string]string
	getErr      error
	getMultiErr error
	setErr      error
	updatedAt   time.Time
	writes      []string
	beforeCAS   func(value string)
}

func newCodexVersionSyncSettingRepoStub(values map[string]string) *codexVersionSyncSettingRepoStub {
	if values == nil {
		values = map[string]string{}
	}
	return &codexVersionSyncSettingRepoStub{values: values}
}

func (r *codexVersionSyncSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return "", r.getErr
	}
	return r.values[key], nil
}

func (r *codexVersionSyncSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getMultiErr != nil {
		return nil, r.getMultiErr
	}
	if r.getErr != nil {
		return nil, r.getErr
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}

func (r *codexVersionSyncSettingRepoStub) setGetMultipleError(err error) {
	r.mu.Lock()
	r.getMultiErr = err
	r.mu.Unlock()
}

type blockingCodexVersionSettingRepo struct {
	*codexVersionSyncSettingRepoStub
	blockOnce sync.Once
	started   chan struct{}
	release   chan struct{}
}

func (r *blockingCodexVersionSettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	err := r.getMultiErr
	if err == nil {
		err = r.getErr
	}
	r.mu.Unlock()

	shouldBlock := false
	r.blockOnce.Do(func() { shouldBlock = true })
	if shouldBlock {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *codexVersionSyncSettingRepoStub) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return r.setErr
	}
	r.values[key] = value
	r.writes = append(r.writes, value)
	return nil
}

func (r *codexVersionSyncSettingRepoStub) CompareAndSwap(
	_ context.Context,
	key string,
	expected *string,
	value string,
) (bool, error) {
	if r.beforeCAS != nil {
		r.beforeCAS(value)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.setErr != nil {
		return false, r.setErr
	}
	current, exists := r.values[key]
	if expected == nil {
		if exists {
			return false, nil
		}
	} else if !exists || current != *expected {
		return false, nil
	}
	changed := !exists || current != value
	r.values[key] = value
	r.updatedAt = time.Now()
	if changed {
		r.writes = append(r.writes, value)
	}
	return true, nil
}

func (r *codexVersionSyncSettingRepoStub) syncedWrites() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.writes...)
}

type codexVersionSyncGitHubStub struct {
	GitHubReleaseClient // 嵌入接口，未实现的方法会 panic（不应被调用）

	// 主路径 /releases/latest；latest 为 nil 且 latestErr 为 nil 时模拟「拿不到可用值」，
	// 使调用落到回退的列表扫描上。
	latest      *GitHubRelease
	latestErr   error
	latestCalls int

	releases []*GitHubRelease
	err      error
	calls    int
}

func (c *codexVersionSyncGitHubStub) FetchLatestRelease(_ context.Context, _ string) (*GitHubRelease, error) {
	c.latestCalls++
	if c.latestErr != nil {
		return nil, c.latestErr
	}
	return c.latest, nil
}

func (c *codexVersionSyncGitHubStub) FetchRecentReleases(_ context.Context, _ string, _ int) ([]*GitHubRelease, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return c.releases, nil
}

func newCodexVersionSyncService(
	repo SettingRepository,
	github GitHubReleaseClient,
) *OpenAICodexVersionSyncService {
	return NewOpenAICodexVersionSyncService(repo, &SettingService{}, github, openAICodexVersionSyncInterval)
}

// 同仓库同时发布预发布版与其他组件的 tag，必须只认 rust-v 前缀的稳定版，
// 否则会把无关组件的版本号（如 rusty-v8-v150.4.0）当成客户端版本同步出去。
func TestLatestCodexStableReleaseVersion(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "rusty-v8-v150.4.0"},
		{TagName: "rust-v0.147.0-alpha.4", Prerelease: true},
		{TagName: "rust-v0.146.0"},
		{TagName: "rust-v0.145.0"},
		{TagName: "rust-v0.999.0", Draft: true},
		{TagName: "not-a-tag"},
		nil,
	}

	require.Equal(t, "0.146.0", latestCodexStableReleaseVersion(releases))
	require.Empty(t, latestCodexStableReleaseVersion(nil))
	require.Empty(t, latestCodexStableReleaseVersion([]*GitHubRelease{{TagName: "rusty-v8-v150.4.0"}}))
	// 预发布 tag 即使漏标 Prerelease 也要被版本号后缀挡住。
	require.Empty(t, latestCodexStableReleaseVersion([]*GitHubRelease{{TagName: "rust-v0.147.0-alpha.4"}}))
}

func TestOpenAICodexVersionSyncWritesLatestStableVersion(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(nil)
	github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{
		{TagName: "rust-v0.150.1"},
		{TagName: "rust-v0.150.2"},
	}}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Equal(t, []string{"0.150.2"}, repo.syncedWrites())
}

func TestOpenAICodexVersionSyncRejectsReleaseBelowBuiltinFallback(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(nil)
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.150.0"}}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Empty(t, repo.syncedWrites())
}

// 只向前推进：上游偶发返回旧数据或重新发布历史 tag 时不把已同步版本降级。
func TestOpenAICodexVersionSyncNeverMovesBackwards(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
	})
	github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.150.2"}}}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Empty(t, repo.syncedWrites())
}

func TestOpenAICodexVersionSyncReplacesPrereleaseWithSameCoreStable(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.200.1-alpha.1",
	})
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
}

// 两个副本同时基于旧快照同步不同版本时，较低版本的 CAS 必须失败并读取胜出的新版，
// 不能在较高版本写入后再把数据库覆盖回旧值。
func TestOpenAICodexVersionSyncCASKeepsHighestAcrossInstances(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: codexResponsesVersionFallback,
	})
	highWritten := make(chan struct{})
	repo.beforeCAS = func(value string) {
		if value == "0.200.1" {
			<-highWritten
		}
	}

	lowSettings := NewSettingService(repo, nil)
	highSettings := NewSettingService(repo, nil)
	low := NewOpenAICodexVersionSyncService(
		repo,
		lowSettings,
		&codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}},
		openAICodexVersionSyncInterval,
	)
	high := NewOpenAICodexVersionSyncService(
		repo,
		highSettings,
		&codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.2"}},
		openAICodexVersionSyncInterval,
	)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		low.runOnce()
	}()
	go func() {
		defer wg.Done()
		high.runOnce()
		close(highWritten)
	}()
	wg.Wait()

	value, err := repo.GetValue(context.Background(), SettingKeyOpenAICodexClientVersionSynced)
	require.NoError(t, err)
	require.Equal(t, "0.200.2", value)
	require.Equal(t, "0.200.2", lowSettings.GetOpenAICodexResponsesVersion(context.Background()))
	require.Equal(t, "0.200.2", highSettings.GetOpenAICodexResponsesVersion(context.Background()))
}

func TestOpenAICodexVersionSyncSkippedWhenDisabled(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexVersionAutoSyncEnabled: "false",
	})
	github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.200.1"}}}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Zero(t, github.latestCalls, "关闭自动同步后不应请求上游")
	require.Zero(t, github.calls, "关闭自动同步后不应请求上游")
	require.Empty(t, repo.syncedWrites())
}

// 面板开关缺失或为空一律视为开启，与设置默认值一致。
func TestOpenAICodexVersionSyncEnabledByDefault(t *testing.T) {
	for _, value := range []string{"", "true"} {
		repo := newCodexVersionSyncSettingRepoStub(map[string]string{
			SettingKeyOpenAICodexVersionAutoSyncEnabled: value,
		})
		github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.200.1"}}}

		newCodexVersionSyncService(repo, github).runOnce()

		require.Equal(t, []string{"0.200.1"}, repo.syncedWrites(), "开关值 %q", value)
	}
}

// 抓取失败保持既有值，不清空、不降级。两条取数路径都失败才算真正拿不到。
func TestOpenAICodexVersionSyncKeepsValueOnFetchError(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
	})
	github := &codexVersionSyncGitHubStub{
		latestErr: errors.New("network down"),
		err:       errors.New("network down"),
	}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Equal(t, 1, github.latestCalls)
	require.Equal(t, 1, github.calls)
	require.Empty(t, repo.syncedWrites())
	value, err := repo.GetValue(context.Background(), SettingKeyOpenAICodexClientVersionSynced)
	require.NoError(t, err)
	require.Equal(t, "0.200.1", value)
}

// 主路径 /releases/latest：该端点已排除 draft / prerelease，直接给出最新正式发布，
// 因此不受该仓库预发布密度影响，也不必再去拉 ~10MB 的列表页。
func TestOpenAICodexVersionSyncUsesLatestReleaseEndpoint(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(nil)
	github := &codexVersionSyncGitHubStub{
		latest: &GitHubRelease{TagName: "rust-v0.200.1"},
		// 列表若被调用会给出不同答案，用于证明取值确实来自主路径。
		releases: []*GitHubRelease{{TagName: "rust-v0.150.1"}},
	}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Equal(t, 1, github.latestCalls)
	require.Zero(t, github.calls, "主路径可用时不应再拉列表页")
	require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
}

// 回退列表扫描：latest 是跨 tag 家族按 published_at 取的，主路径拿不到客户端稳定版时
// 必须继续扫一页 release，否则版本号会静默停更。三种拿不到的形态都要回退。
func TestOpenAICodexVersionSyncFallsBackToReleaseList(t *testing.T) {
	tests := []struct {
		name      string
		latest    *GitHubRelease
		latestErr error
	}{
		// 同仓库其他组件（rusty-v8-*）某天发了正式 release 而成为 latest。
		{name: "latest 属于其他 tag 家族", latest: &GitHubRelease{TagName: "rusty-v8-v150.4.0"}},
		// 端点语义若变化（返回 draft/prerelease），过滤仍然拦得住，并照常回退。
		{name: "latest 是预发布", latest: &GitHubRelease{TagName: "rust-v0.200.2-alpha.4", Prerelease: true}},
		{name: "latest 抓取失败", latestErr: errors.New("network down")},
		// 上游返回空对象：不得因此 panic，按拿不到处理。
		{name: "latest 为空", latest: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newCodexVersionSyncSettingRepoStub(nil)
			github := &codexVersionSyncGitHubStub{
				latest:    tt.latest,
				latestErr: tt.latestErr,
				releases: []*GitHubRelease{
					{TagName: "rust-v0.200.2-alpha.4", Prerelease: true},
					{TagName: "rust-v0.200.1"},
					{TagName: "rust-v0.150.1"},
				},
			}

			newCodexVersionSyncService(repo, github).runOnce()

			require.Equal(t, 1, github.latestCalls)
			require.Equal(t, 1, github.calls)
			require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
		})
	}
}

// 两条路径共用同一套过滤：主路径的单条 latest 同样要过前缀 / 版本号形态校验，
// 不能因为「端点保证是正式发布」就直接采信 tag。
func TestOpenAICodexVersionSyncLatestSharesFiltering(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(nil)
	github := &codexVersionSyncGitHubStub{
		// 仓库里确实存在 rust-vv0.99.0-alpha.8 / rust-vrust-v0.145.0-alpha.6 这类畸形 tag：
		// 剥掉前缀后不是合法版本号，必须被拒绝而不是写成 v0.99.0。
		latest:   &GitHubRelease{TagName: "rust-vv0.99.0"},
		releases: []*GitHubRelease{{TagName: "rust-v0.200.1"}},
	}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
}

// 依赖缺失时 Start 必须直接返回，不能起一个空转的 goroutine。
func TestOpenAICodexVersionSyncStartRequiresDependencies(t *testing.T) {
	require.NotPanics(t, func() {
		svc := NewOpenAICodexVersionSyncService(nil, nil, nil, openAICodexVersionSyncInterval)
		svc.Start()
		svc.Stop()
	})
	require.NotPanics(t, func() {
		svc := NewOpenAICodexVersionSyncService(
			newCodexVersionSyncSettingRepoStub(nil),
			nil,
			&codexVersionSyncGitHubStub{},
			openAICodexVersionSyncInterval,
		)
		svc.Start()
		svc.Stop()
	})
}

// --- mock: 版本号读取只用到 GetMultiple ---

type codexVersionSettingRepoStub struct {
	SettingRepository // 嵌入接口，未实现的方法会 panic（不应被调用）

	values map[string]string
	err    error
}

func (r *codexVersionSettingRepoStub) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = r.values[key]
	}
	return out, nil
}

// 规范 UA 解析会先读面板的完整 UA 键。
func (r *codexVersionSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.values[key], nil
}

// 统一版本语义：管理员热修复值、官方同步值和编译期下限中取最高的
// 合法稳定版。客户端宿主类型不改变这个选择，UA engine 与 Responses/WS
// Version 必须消费同一结果。
func TestGetOpenAICodexClientVersionPriority(t *testing.T) {
	tests := []struct {
		name      string
		override  string
		synced    string
		userAgent string
		want      string
	}{
		{name: "同步值高于管理员值", override: "0.160.0", synced: "0.200.1", want: "0.200.1"},
		{name: "管理员值高于同步值", override: "0.200.2", synced: "0.200.1", want: "0.200.2"},
		{name: "默认 Desktop 使用较新同步值", synced: "0.200.1", want: "0.200.1"},
		{name: "显式 Desktop UA 使用较新同步值", synced: "0.200.1", userAgent: codexCLIUserAgent, want: "0.200.1"},
		{name: "VS Code 宿主使用较新同步值", synced: "0.200.1", userAgent: "codex_vscode/0.146.0 (codex_vscode; 0.146.0)", want: "0.200.1"},
		{name: "Xcode 宿主使用较新同步值", synced: "0.200.1", userAgent: "Codex Xcode/0.146.0 (Codex Xcode; 0.146.0)", want: "0.200.1"},
		{name: "CLI 身份使用较新同步值", synced: "0.200.1", userAgent: "codex_cli_rs/0.146.0", want: "0.200.1"},
		{name: "TUI 的旧同步值不降级内置版本", synced: "0.146.0", userAgent: "codex-tui/0.146.0", want: codexCLIVersion},
		{name: "两者皆空时用内置常量", want: codexCLIVersion},
		{name: "非法管理员值回退同步值", override: "latest", synced: "0.200.1", userAgent: "codex_cli_rs/0.146.0", want: "0.200.1"},
		{name: "非法同步值回退内置常量", synced: "not-a-version", userAgent: "codex_cli_rs/0.146.0", want: codexCLIVersion},
		{name: "预发布同步值回退内置常量", synced: "0.200.1-alpha.1", userAgent: "codex_cli_rs/0.146.0", want: codexCLIVersion},
		{name: "预发布管理员值不覆盖稳定同步值", override: "0.300.0-alpha.1", synced: "0.200.1", want: "0.200.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersion:       tt.override,
				SettingKeyOpenAICodexClientVersionSynced: tt.synced,
				SettingKeyOpenAICodexUserAgent:           tt.userAgent,
			}}, nil)

			require.Equal(t, tt.want, svc.GetOpenAICodexClientVersion(context.Background()))
		})
	}
}

// 读取失败必须回退内置常量，不能返回空串把畸形身份拼给上游。
func TestGetOpenAICodexClientVersionFallsBackOnError(t *testing.T) {
	svc := NewSettingService(&codexVersionSettingRepoStub{err: errors.New("db down")}, nil)
	require.Equal(t, codexCLIVersion, svc.GetOpenAICodexClientVersion(context.Background()))
}

// Responses/WS Version 与 UA engine 共用统一版本选择：管理员值、同步值、
// 内置下限中取最高稳定版。旧值、非法值和预发布值均不参与竞选。
func TestGetOpenAICodexResponsesVersionUsesLatestStableSync(t *testing.T) {
	tests := []struct {
		name     string
		synced   string
		override string
		want     string
	}{
		{name: "无同步值使用新版内置下限", want: codexResponsesVersionFallback},
		{name: "官方更新版向前推进", synced: "0.200.1", want: "0.200.1"},
		{name: "旧同步值不降级", synced: "0.146.0", want: codexResponsesVersionFallback},
		{name: "非法同步值回退", synced: "latest", want: codexResponsesVersionFallback},
		{name: "预发布同步值回退", synced: "0.200.1-alpha.1", want: codexResponsesVersionFallback},
		{
			name:     "较低管理员值不降级同步值",
			synced:   "0.200.1",
			override: "0.160.0",
			want:     "0.200.1",
		},
		{
			name:     "较高管理员值向前推进统一版本",
			synced:   "0.200.1",
			override: "0.200.2",
			want:     "0.200.2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
				SettingKeyOpenAICodexClientVersion:       tt.override,
				SettingKeyOpenAICodexClientVersionSynced: tt.synced,
			}}, nil)
			require.Equal(t, tt.want, svc.GetOpenAICodexResponsesVersion(context.Background()))
		})
	}
}

func TestGetOpenAICodexResponsesVersionFallsBackOnError(t *testing.T) {
	svc := NewSettingService(&codexVersionSettingRepoStub{err: errors.New("db down")}, nil)
	require.Equal(t, codexResponsesVersionFallback, svc.GetOpenAICodexResponsesVersion(context.Background()))
}

func TestOpenAICodexResponsesVersionCacheNeverMovesBackwards(t *testing.T) {
	svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{}}, nil)
	svc.PublishOpenAICodexResponsesVersion("0.200.1")

	// 模拟一个在同步发布之前就已开始、但在之后才完成的旧 DB 读取。
	version, stored := svc.storeOpenAICodexResponsesVersionAtEpoch(
		codexResponsesVersionFallback,
		time.Minute,
		svc.openAICodexResponsesVersionEpoch.Load(),
		true,
	)
	require.True(t, stored)
	require.Equal(t, "0.200.1", version)
	require.Equal(t, "0.200.1", svc.GetOpenAICodexResponsesVersion(context.Background()))
}

func TestOpenAICodexResponsesVersionInvalidationRejectsStaleInFlightRead(t *testing.T) {
	baseRepo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersion:       "0.160.0",
		SettingKeyOpenAICodexClientVersionSynced: codexResponsesVersionFallback,
	})
	repo := &blockingCodexVersionSettingRepo{
		codexVersionSyncSettingRepoStub: baseRepo,
		started:                         make(chan struct{}),
		release:                         make(chan struct{}),
	}
	svc := NewSettingService(repo, nil)
	resultCh := make(chan string, 1)
	go func() {
		resultCh <- svc.GetOpenAICodexResponsesVersion(context.Background())
	}()

	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the old version read to start")
	}
	baseRepo.mu.Lock()
	baseRepo.values[SettingKeyOpenAICodexClientVersion] = "0.300.0"
	baseRepo.mu.Unlock()
	svc.InvalidateOpenAICodexClientVersionCache()
	close(repo.release)

	select {
	case got := <-resultCh:
		require.Equal(t, "0.300.0", got, "the pre-invalidation snapshot must be retried in the new epoch")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the version read to retry")
	}
	require.Equal(t, "0.300.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
}

func TestOpenAICodexResponsesVersionPublishDoesNotCrossInvalidationEpoch(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersion:       "0.200.1",
		SettingKeyOpenAICodexClientVersionSynced: codexResponsesVersionFallback,
	})
	svc := NewSettingService(repo, nil)
	staleVersion, sourceOK, staleEpoch := svc.getOpenAICodexResponsesVersion(context.Background())
	require.Equal(t, "0.200.1", staleVersion)
	require.True(t, sourceOK)

	// 模拟同步任务刚算完 effective，管理员就保存了更高的 manual floor。
	repo.mu.Lock()
	repo.values[SettingKeyOpenAICodexClientVersion] = "0.300.0"
	repo.mu.Unlock()
	svc.InvalidateOpenAICodexClientVersionCache()

	_, stored := svc.storeOpenAICodexResponsesVersionAtEpoch(
		staleVersion,
		openAICodexClientVersionCacheTTL,
		staleEpoch,
		sourceOK,
	)
	require.False(t, stored, "a sync result computed before invalidation must not publish into the new epoch")
	require.Equal(t, "0.300.0", svc.GetOpenAICodexResponsesVersion(context.Background()))
}

func TestOpenAICodexVersionSyncPublishesUnifiedIdentityVersion(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(nil)
	settings := NewSettingService(repo, nil)
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}
	syncService := NewOpenAICodexVersionSyncService(
		repo,
		settings,
		github,
		openAICodexVersionSyncInterval,
	)

	SetCodexCanonicalUserAgentResolver(func() string {
		return settings.GetOpenAICodexCanonicalUserAgent(context.Background())
	})
	SetCodexCanonicalResponsesVersionResolver(func() string {
		return settings.GetOpenAICodexResponsesVersion(context.Background())
	})
	t.Cleanup(func() {
		SetCodexCanonicalUserAgentResolver(nil)
		SetCodexCanonicalResponsesVersionResolver(nil)
	})

	// 先填充两个运行时缓存，验证同步成功会原子更新 UA engine
	// 与 Responses/WS Version。
	require.Equal(t, codexCLIUserAgent, CodexCanonicalUserAgent())
	require.Equal(t, codexResponsesVersionFallback, CodexCanonicalClientVersion())

	syncService.runOnce()

	require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
	wantUA := openai.CodexDefaultOriginator + "/0.200.1" + codexDesktopUserAgentSuffix
	require.Equal(t, wantUA, CodexCanonicalUserAgent())
	require.Equal(t, "0.200.1", CodexCanonicalClientVersion())

	headers := make(http.Header)
	ensureCodexIdentityHeaders(headers)
	enforceCodexIdentityHeaders(headers)
	require.Equal(t, wantUA, headers.Get("User-Agent"))
	require.Equal(t, openai.CodexDefaultOriginator, headers.Get("Originator"))
	require.Equal(t, "0.200.1", headers.Get("Version"))
	require.Contains(t, headers.Get("User-Agent"), "(Codex Desktop; "+codexDesktopAppVersion+")",
		"UA trailer 使用独立 Desktop app 版本，不随 rust-v 改写")

	authHeaders := http.Header{"Version": []string{"stale"}}
	ApplyCodexCanonicalAuthIdentity(authHeaders)
	require.Equal(t, wantUA, authHeaders.Get("User-Agent"))
	require.Equal(t, openai.CodexDefaultOriginator, authHeaders.Get("Originator"))
	require.Empty(t, authHeaders.Get("Version"), "auth.openai.com 不发送 Responses Version")
}

// 官方同步只更新 synced 候选值；管理员已配置更高的稳定版热修复下限时，
// 同步后立即发布的进程内缓存也必须保留该更高值，不能在 TTL 内短暂回退。
func TestOpenAICodexVersionSyncKeepsHigherManualFloorInPublishedCache(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersion:       "0.300.0",
		SettingKeyOpenAICodexClientVersionSynced: codexResponsesVersionFallback,
	})
	settings := NewSettingService(repo, nil)
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}
	syncService := NewOpenAICodexVersionSyncService(
		repo,
		settings,
		github,
		openAICodexVersionSyncInterval,
	)

	// 预热旧缓存，覆盖 runOnce 的「失效→回源合并→发布」完整路径。
	require.Equal(t, "0.300.0", settings.GetOpenAICodexResponsesVersion(context.Background()))
	syncService.runOnce()

	require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
	require.Equal(t, "0.300.0", settings.GetOpenAICodexResponsesVersion(context.Background()))
	require.Equal(t,
		openai.CodexDefaultOriginator+"/0.300.0"+codexDesktopUserAgentSuffix,
		settings.GetOpenAICodexCanonicalUserAgent(context.Background()),
	)
}

func TestOpenAICodexVersionSyncKeepsHigherManualFloorWhenPostSyncReloadFails(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersion:       "0.300.0",
		SettingKeyOpenAICodexClientVersionSynced: codexResponsesVersionFallback,
	})
	settings := NewSettingService(repo, nil)
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}
	syncService := NewOpenAICodexVersionSyncService(
		repo,
		settings,
		github,
		openAICodexVersionSyncInterval,
	)

	// 先缓存已知的更高 manual floor，再模拟同步落库后 GetMultiple 瞬时失败。
	require.Equal(t, "0.300.0", settings.GetOpenAICodexResponsesVersion(context.Background()))
	repo.setGetMultipleError(errors.New("temporary get-multiple failure"))
	syncService.runOnce()

	require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
	version, sourceOK, _ := settings.getOpenAICodexResponsesVersion(context.Background())
	require.Equal(t, "0.300.0", version)
	require.False(t, sourceOK, "the short-lived cache must retain that the post-sync DB merge was not confirmed")
	cached, ok := settings.openAICodexResponsesVersionCache.Load().(*cachedOpenAICodexResponsesVersion)
	require.True(t, ok)
	require.NotNil(t, cached)
	require.LessOrEqual(t, time.Until(time.Unix(0, cached.expiresAt)), openAICodexClientVersionErrorTTL)
}

// 规范 UA：面板未填完整 UA 时按当前生效引擎版本号拼出
// 标准 Desktop 形态，尾部 Desktop app 版本保持独立。
func TestGetOpenAICodexCanonicalUserAgentBuildsFromVersion(t *testing.T) {
	svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
		SettingKeyOpenAICodexClientVersion: "0.200.1",
	}}, nil)

	require.Equal(t,
		openai.CodexDefaultOriginator+"/0.200.1"+codexDesktopUserAgentSuffix,
		svc.GetOpenAICodexCanonicalUserAgent(context.Background()),
	)
}

// 回归：面板完整 UA 是唯一能改 OS / 架构 / 终端指纹的地方，必须保留；但它填写于某个
// 历史版本，逐字沿用会绕过版本自动同步、把出站身份永久钉死在陈旧版本上——而陈旧身份
// 正是上游优先降载的那一侧。因此只借它的指纹，版本段一律用生效版本重建。
func TestGetOpenAICodexCanonicalUserAgentRebuildsPanelUAVersion(t *testing.T) {
	t.Run("陈旧面板 UA 跟随生效版本", func(t *testing.T) {
		svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
			// 历史面板 placeholder 的原文，照抄填写过的存量部署就是这个值。
			SettingKeyOpenAICodexUserAgent:           "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
		}}, nil)

		require.Equal(t,
			"codex_cli_rs/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			svc.GetOpenAICodexCanonicalUserAgent(context.Background()),
		)
	})

	t.Run("自定义指纹原样保留", func(t *testing.T) {
		svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexUserAgent:           "codex_cli_rs/0.140.0 (Mac OS X 15.1.0; arm64) iTerm.app",
			SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
		}}, nil)

		require.Equal(t,
			"codex_cli_rs/0.200.1 (Mac OS X 15.1.0; arm64) iTerm.app",
			svc.GetOpenAICodexCanonicalUserAgent(context.Background()),
		)
	})

	t.Run("TUI UA 的首尾两个版本号同时更新", func(t *testing.T) {
		svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexUserAgent:           "codex-tui/0.146.1 (Ubuntu 22.4.0; x86_64) WindowsTerminal (codex-tui; 0.146.1)",
			SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
		}}, nil)

		require.Equal(t,
			"codex-tui/0.200.1 (Ubuntu 22.4.0; x86_64) WindowsTerminal (codex-tui; 0.200.1)",
			svc.GetOpenAICodexCanonicalUserAgent(context.Background()),
		)
	})

	t.Run("Desktop UA 的 engine 更新但 app 版本保留", func(t *testing.T) {
		svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexUserAgent: "Codex Desktop/0.146.1 (Mac OS 26.1.0; arm64) unknown " +
				"(Codex Desktop; 26.700.10000)",
			SettingKeyOpenAICodexClientVersion: "0.200.1",
		}}, nil)

		require.Equal(t,
			"Codex Desktop/0.200.1 (Mac OS 26.1.0; arm64) unknown (Codex Desktop; 26.700.10000)",
			svc.GetOpenAICodexCanonicalUserAgent(context.Background()),
		)
	})

	t.Run("Desktop UA engine 跟随同步且 app trailer 保持不变", func(t *testing.T) {
		svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexUserAgent:           codexCLIUserAgent,
			SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
		}}, nil)

		require.Equal(t,
			openai.CodexDefaultOriginator+"/0.200.1"+codexDesktopUserAgentSuffix,
			svc.GetOpenAICodexCanonicalUserAgent(context.Background()),
		)
	})

	// 手工值是热修复候选而非降级锁；同步值更高时仍应使用较新稳定版。
	t.Run("同步值高于面板版本时向前推进", func(t *testing.T) {
		svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexUserAgent:           "codex_cli_rs/0.144.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			SettingKeyOpenAICodexClientVersion:       "0.160.0",
			SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
		}}, nil)

		require.Equal(t,
			"codex_cli_rs/0.200.1 (Ubuntu 22.4.0; x86_64) xterm-256color",
			svc.GetOpenAICodexCanonicalUserAgent(context.Background()),
		)
	})

	// 非 `{client}/{version}` 形态无法重建，原样返回，由收口整体回退规范身份。
	t.Run("非 Codex 形态原样返回", func(t *testing.T) {
		svc := NewSettingService(&codexVersionSettingRepoStub{values: map[string]string{
			SettingKeyOpenAICodexUserAgent: "not-a-codex-client",
		}}, nil)

		require.Equal(t, "not-a-codex-client", svc.GetOpenAICodexCanonicalUserAgent(context.Background()))
	})
}

func (r *codexVersionSyncSettingRepoStub) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.getErr != nil {
		return nil, r.getErr
	}
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value, UpdatedAt: r.updatedAt}, nil
}

// 启动同步防抖：同步值仍在一个周期内时跳过，避免频繁重启/滚动发布把启动同步
// 放大成对 GitHub 的连续请求。
func TestOpenAICodexVersionSyncInitialSkipsWhenRecentlySynced(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: codexResponsesVersionFallback,
	})
	repo.updatedAt = time.Now().Add(-time.Hour)
	github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.200.1"}}}

	newCodexVersionSyncService(repo, github).runInitial()

	require.Zero(t, github.calls, "同步值仍在周期内时不应请求上游")
	require.Empty(t, repo.syncedWrites())
}

func TestOpenAICodexVersionSyncInitialRefreshesRecentValueBelowBuiltinFallback(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.150.0",
	})
	repo.updatedAt = time.Now().Add(-time.Hour)
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.150.1"}}

	settings := NewSettingService(repo, nil)
	NewOpenAICodexVersionSyncService(repo, settings, github, openAICodexVersionSyncInterval).runInitial()

	require.Equal(t, 1, github.latestCalls)
	require.Equal(t, []string{"0.150.1"}, repo.syncedWrites())
}

func TestOpenAICodexVersionSyncInitialRefreshesRecentPrerelease(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.200.1-alpha.1",
	})
	repo.updatedAt = time.Now().Add(-time.Hour)
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}

	newCodexVersionSyncService(repo, github).runInitial()

	require.Equal(t, 1, github.latestCalls)
	require.Equal(t, []string{"0.200.1"}, repo.syncedWrites())
}

func TestOpenAICodexVersionSyncInitialRefreshesTimestampWhenVersionUnchanged(t *testing.T) {
	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.200.1",
	})
	repo.updatedAt = time.Now().Add(-7 * time.Hour)
	github := &codexVersionSyncGitHubStub{latest: &GitHubRelease{TagName: "rust-v0.200.1"}}
	svc := newCodexVersionSyncService(repo, github)

	svc.runInitial()
	require.Equal(t, 1, github.latestCalls)
	require.Empty(t, repo.syncedWrites(), "版本未变化时不应记录语义写入")

	// 成功核对会刷新 UpdatedAt，紧接着再次启动不应重复访问 GitHub。
	svc.runInitial()
	require.Equal(t, 1, github.latestCalls)
}

func TestOpenAICodexVersionSyncInitialRunsWhenStaleOrMissing(t *testing.T) {
	t.Run("同步值已过期", func(t *testing.T) {
		repo := newCodexVersionSyncSettingRepoStub(map[string]string{
			SettingKeyOpenAICodexClientVersionSynced: "0.150.1",
		})
		repo.updatedAt = time.Now().Add(-7 * time.Hour)
		github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.150.2"}}}

		newCodexVersionSyncService(repo, github).runInitial()

		require.Equal(t, 1, github.calls)
		require.Equal(t, []string{"0.150.2"}, repo.syncedWrites())
	})

	// 首次部署尚无同步值：必须立刻同步，不能被防抖挡住。
	t.Run("尚无同步值", func(t *testing.T) {
		repo := newCodexVersionSyncSettingRepoStub(nil)
		repo.updatedAt = time.Now()
		github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.150.1"}}}

		newCodexVersionSyncService(repo, github).runInitial()

		require.Equal(t, 1, github.calls)
		require.Equal(t, []string{"0.150.1"}, repo.syncedWrites())
	})
}

// 版本比较必须按段取数字：字典序会把 0.99.0 判为大于 0.146.0，
// 从而让「取最大值」和「只向前推进」两处逻辑同时判错。
func TestCodexVersionComparisonIsNumericNotLexical(t *testing.T) {
	require.Greater(t, CompareVersions("0.146.0", "0.99.0"), 0)

	require.Equal(t, "0.146.0", latestCodexStableReleaseVersion([]*GitHubRelease{
		{TagName: "rust-v0.99.0"},
		{TagName: "rust-v0.146.0"},
	}))

	repo := newCodexVersionSyncSettingRepoStub(map[string]string{
		SettingKeyOpenAICodexClientVersionSynced: "0.146.0",
	})
	github := &codexVersionSyncGitHubStub{releases: []*GitHubRelease{{TagName: "rust-v0.99.0"}}}

	newCodexVersionSyncService(repo, github).runOnce()

	require.Empty(t, repo.syncedWrites(), "0.99.0 低于内置新版与已同步版本，不得写入")
}
