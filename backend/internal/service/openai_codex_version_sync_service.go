package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// openAICodexVersionCASRepository 是同步任务使用的可选原子写接口。
// 生产 SettingRepository 实现该接口，使多实例的「读取当前值→只向前写入」成为
// 单个 compare-and-swap 操作；普通测试桩仍可只实现 SettingRepository。
type openAICodexVersionCASRepository interface {
	CompareAndSwap(ctx context.Context, key string, expected *string, value string) (bool, error)
}

const (
	// openAICodexVersionSyncInterval 自动同步间隔。上游客户端发版频率是天级，
	// 6 小时足够及时跟上，同时把对 GitHub API 的调用压到每天 4 次。
	openAICodexVersionSyncInterval = 6 * time.Hour
	// openAICodexVersionSyncTimeout 单次同步的整体超时。
	openAICodexVersionSyncTimeout = 30 * time.Second
	// openAICodexVersionSyncRepo 官方 Codex 客户端仓库。
	openAICodexVersionSyncRepo = "openai/codex"
	// openAICodexVersionSyncPerPage 回退路径单次拉取的 release 数量（主路径见
	// fetchLatestStableVersion）。该仓库预发布极密集——0.145.0 与 0.146.0 之间隔着 20 多个
	// alpha，实测 30 条里只有 2 条稳定版，第二条已排在第 26 位，因此这个页大小不能再往下调，
	// 否则整页扫不到稳定版、同步会静默停更。
	openAICodexVersionSyncPerPage = 30
	// openAICodexVersionTagPrefix 客户端 release 的 tag 前缀（如 rust-v0.146.0）。
	// 同仓库还有其他组件的 tag（如 rusty-v8-*），必须按前缀过滤，否则会同步到无关版本号。
	openAICodexVersionTagPrefix = "rust-v"
)

// OpenAICodexVersionSyncService 周期性把官方 Codex rust-v 的最新稳定版同步到设置，
// 供 User-Agent engine 与 Responses/WS Version 同源使用，避免为了跟上游版本而
// 发布网关新版本。Codex Desktop trailer 中的 app build 仍作为独立宿主信号保留。
//
// 同步值写入 SettingKeyOpenAICodexClientVersionSynced（本服务独占写入）。
// 手动版本及版本选择模式由 SettingService 解析；同步不会覆盖已固定的历史版本。
type OpenAICodexVersionSyncService struct {
	settingRepo    SettingRepository
	settingService *SettingService
	githubClient   GitHubReleaseClient
	interval       time.Duration
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
	historyMu      sync.Mutex
	historyEpoch   uint64
	historyCache   map[int]cachedCodexVersionHistory
	historySF      singleflight.Group
	syncSF         singleflight.Group
}

func NewOpenAICodexVersionSyncService(
	settingRepo SettingRepository,
	settingService *SettingService,
	githubClient GitHubReleaseClient,
	interval time.Duration,
) *OpenAICodexVersionSyncService {
	return &OpenAICodexVersionSyncService{
		settingRepo:    settingRepo,
		settingService: settingService,
		githubClient:   githubClient,
		interval:       interval,
		stopCh:         make(chan struct{}),
	}
}

func (s *OpenAICodexVersionSyncService) Start() {
	if s == nil || s.settingRepo == nil || s.settingService == nil || s.githubClient == nil || s.interval <= 0 {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		s.runInitial()
		for {
			select {
			case <-ticker.C:
				s.runOnce()
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *OpenAICodexVersionSyncService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

// runInitial 执行启动时的首次同步。若同步值在一个同步周期内已被刷新过则跳过：
// 频繁重启、滚动发布或崩溃重启会让「启动即同步」放大成对 GitHub 的连续请求，
// 而版本号是天级变化的，重启后没有立刻重新拉取的必要。
func (s *OpenAICodexVersionSyncService) runInitial() {
	if s.syncedWithinInterval() {
		return
	}
	s.runOnce()
}

// syncedWithinInterval 判断已同步值是否仍在一个同步周期内。
// 借设置行自身的 UpdatedAt 判断，无需额外记录时间戳的设置项。
// 读取失败或尚无有效同步值时返回 false，让启动同步照常执行。
func (s *OpenAICodexVersionSyncService) syncedWithinInterval() bool {
	if s.interval <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), openAICodexVersionSyncTimeout)
	defer cancel()

	setting, err := s.settingRepo.Get(ctx, SettingKeyOpenAICodexClientVersionSynced)
	if err != nil || setting == nil || setting.UpdatedAt.IsZero() {
		return false
	}
	version := normalizeStableCodexClientVersion(setting.Value)
	if version == "" || CompareVersions(version, codexResponsesVersionFallback) < 0 {
		return false
	}
	return time.Since(setting.UpdatedAt) < s.interval
}

func (s *OpenAICodexVersionSyncService) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), openAICodexVersionSyncTimeout)
	defer cancel()

	if !s.autoSyncEnabled(ctx) {
		return
	}

	if _, err := s.SyncNow(ctx); err != nil {
		slog.Warn("openai_codex_version_sync_failed", "error", err)
	}
}

// SyncNow checks the official stable release even when automatic syncing is off.
// Only the synced candidate is changed; manual values and fixed-version mode remain untouched.
func (s *OpenAICodexVersionSyncService) SyncNow(ctx context.Context) (*OpenAICodexVersionSyncResult, error) {
	if s == nil || s.settingRepo == nil || s.settingService == nil || s.githubClient == nil {
		return nil, errors.New("Codex version sync service is not configured")
	}
	result := s.syncSF.DoChan("sync", func() (any, error) {
		syncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), openAICodexVersionSyncTimeout)
		defer cancel()
		return s.syncNow(syncCtx)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, result.Err
		}
		return result.Val.(*OpenAICodexVersionSyncResult), nil
	}
}

func (s *OpenAICodexVersionSyncService) syncNow(ctx context.Context) (*OpenAICodexVersionSyncResult, error) {
	latest, err := s.fetchLatestStableVersion(ctx)
	if err != nil {
		return nil, err
	}
	if CompareVersions(latest, codexResponsesVersionFallback) < 0 {
		return nil, fmt.Errorf("official Codex version %s is below built-in version %s", latest, codexResponsesVersionFallback)
	}

	previousEffective, _, previousEpoch, previousPinned := s.settingService.getOpenAICodexResponsesVersionWithMode(ctx)
	current, updated, ok := s.persistLatestStableVersion(ctx, latest)
	if !ok {
		return nil, errors.New("failed to persist official Codex version")
	}
	s.settingService.InvalidateOpenAICodexClientVersionCache()
	_, sourceOK, effectiveEpoch := s.settingService.getOpenAICodexResponsesVersion(ctx)
	if !sourceOK && effectiveEpoch == previousEpoch+1 {
		// Preserve the last confirmed effective value on transient DB errors, including
		// an intentionally fixed historical version. Never raise it to the synced candidate.
		_, _ = s.settingService.storeOpenAICodexResponsesVersionModeAtEpoch(
			previousEffective, openAICodexClientVersionErrorTTL, effectiveEpoch, false, previousPinned,
		)
	}
	if updated {
		slog.Info("openai_codex_version_synced", "version", current)
	}
	defaults := ResolveOpenAICodexHeaderDefaults(current)
	s.historyMu.Lock()
	s.historyCache = nil
	s.historyEpoch++
	s.historyMu.Unlock()
	return &OpenAICodexVersionSyncResult{
		LatestVersion: latest,
		SyncedVersion: current,
		Updated:       updated,
		Defaults: OpenAICodexVersionSyncDefaults{
			Originator: defaults.Originator, UserAgent: defaults.UserAgent, ClientVersion: defaults.ClientVersion,
		},
	}, nil
}

// persistLatestStableVersion 以数据库 CAS 保证多实例下也只向前推进。
// 两个实例即使同时读到旧值，较晚写入者也必须重新读取胜出值，而不能把新版覆盖成旧版。
// 返回值依次为数据库最终生效版本、是否由本次调用写入、操作是否成功。
func (s *OpenAICodexVersionSyncService) persistLatestStableVersion(
	ctx context.Context,
	latest string,
) (string, bool, bool) {
	casRepo, supportsCAS := s.settingRepo.(openAICodexVersionCASRepository)
	if !supportsCAS {
		// 兼容只实现基础 SettingRepository 的轻量测试桩；生产仓库始终走 CAS 分支。
		currentRaw, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexClientVersionSynced)
		if err != nil && !errors.Is(err, ErrSettingNotFound) {
			slog.Warn("openai_codex_version_sync_current_read_failed", "error", err)
			return "", false, false
		}
		current := normalizeStableCodexClientVersion(currentRaw)
		if current != "" && CompareVersions(latest, current) <= 0 {
			return current, false, true
		}
		if err := s.settingRepo.Set(ctx, SettingKeyOpenAICodexClientVersionSynced, latest); err != nil {
			slog.Warn("openai_codex_version_sync_persist_failed", "version", latest, "error", err)
			return "", false, false
		}
		return latest, true, true
	}

	for ctx.Err() == nil {
		setting, err := s.settingRepo.Get(ctx, SettingKeyOpenAICodexClientVersionSynced)
		var expected *string
		currentRaw := ""
		switch {
		case err == nil && setting != nil:
			currentRaw = setting.Value
			expected = &currentRaw
		case err == nil || errors.Is(err, ErrSettingNotFound):
			// expected=nil 表示仅当设置行仍不存在时插入。
		default:
			slog.Warn("openai_codex_version_sync_current_read_failed", "error", err)
			return "", false, false
		}

		current := normalizeStableCodexClientVersion(currentRaw)
		// 只向前推进；预发布/非法值不是稳定版上界，允许同 core 正式版替换。
		if current != "" && CompareVersions(latest, current) <= 0 {
			// 同值 CAS 仅刷新 UpdatedAt，记录本轮已成功核对官方版本。
			// runInitial 依赖该时间戳抑制频繁重启造成的 GitHub 请求风暴。
			touched, err := casRepo.CompareAndSwap(
				ctx,
				SettingKeyOpenAICodexClientVersionSynced,
				expected,
				currentRaw,
			)
			if err != nil {
				slog.Warn("openai_codex_version_sync_touch_failed", "version", current, "error", err)
				return "", false, false
			}
			if touched {
				return current, false, true
			}
			continue
		}

		swapped, err := casRepo.CompareAndSwap(
			ctx,
			SettingKeyOpenAICodexClientVersionSynced,
			expected,
			latest,
		)
		if err != nil {
			slog.Warn("openai_codex_version_sync_persist_failed", "version", latest, "error", err)
			return "", false, false
		}
		if swapped {
			return latest, true, true
		}
		// CAS 失败说明其他实例刚更新了行；重新读取并比较实际胜出值。
	}

	return "", false, false
}

// fetchLatestStableVersion prefers the compact latest endpoint, then scans release
// pages if that endpoint points at another component or is temporarily unavailable.
func (s *OpenAICodexVersionSyncService) fetchLatestStableVersion(ctx context.Context) (string, error) {
	release, latestErr := s.githubClient.FetchLatestRelease(ctx, openAICodexVersionSyncRepo)
	if latestErr == nil {
		if version := latestCodexStableReleaseVersion([]*GitHubRelease{release}); version != "" {
			return version, nil
		}
	}
	for page := 1; page <= openAICodexHistoryScanPages; page++ {
		releases, err := s.fetchReleasePage(ctx, page)
		if err != nil {
			return "", fmt.Errorf("fetch official Codex releases: %w", errors.Join(latestErr, err))
		}
		if version := latestCodexStableReleaseVersion(releases); version != "" {
			return version, nil
		}
		if len(releases) < openAICodexVersionSyncPerPage {
			break
		}
		if _, ok := s.githubClient.(codexPaginatedGitHubReleaseClient); !ok {
			break
		}
	}
	return "", errors.New("no official stable Codex release found")
}

// autoSyncEnabled 读取面板开关。缺失或空值视为开启，与设置默认值一致；
// 读取失败时保持开启，避免一次数据库抖动就静默停掉版本跟随。
func (s *OpenAICodexVersionSyncService) autoSyncEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexVersionAutoSyncEnabled)
	if err != nil {
		return true
	}
	if strings.TrimSpace(value) == "" {
		return true
	}
	return strings.TrimSpace(value) == "true"
}

// latestCodexStableReleaseVersion 从 release 列表里挑出最大的稳定版客户端版本号。
// 过滤条件：tag 前缀为 rust-v（排除同仓库其他组件的 tag）、非草稿、非预发布、
// 版本号不带 -alpha/-beta 之类后缀。取最大值而非最新发布，避免重新发布历史 tag 造成回退。
// 主路径的单条 /releases/latest 结果也走本函数（单元素切片），保证两条取数路径的过滤语义一致。
func latestCodexStableReleaseVersion(releases []*GitHubRelease) string {
	best := ""
	for _, release := range releases {
		if release == nil || release.Draft || release.Prerelease {
			continue
		}
		tag := strings.TrimSpace(release.TagName)
		if !strings.HasPrefix(tag, openAICodexVersionTagPrefix) {
			continue
		}
		version := normalizeStableCodexClientVersion(strings.TrimPrefix(tag, openAICodexVersionTagPrefix))
		if version == "" {
			continue
		}
		if best == "" || CompareVersions(version, best) > 0 {
			best = version
		}
	}
	return best
}
