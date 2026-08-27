package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
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
// SettingKeyOpenAICodexClientVersion 是统一版本的管理员热修复值；最终取它、同步值与
// 编译期下限中的最高稳定版。
type OpenAICodexVersionSyncService struct {
	settingRepo    SettingRepository
	settingService *SettingService
	githubClient   GitHubReleaseClient
	interval       time.Duration
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
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

	latest := s.fetchLatestStableVersion(ctx)
	if latest == "" {
		return
	}
	if CompareVersions(latest, codexResponsesVersionFallback) < 0 {
		slog.Warn("openai_codex_version_sync_below_builtin_fallback",
			"version", latest,
			"fallback", codexResponsesVersionFallback,
		)
		return
	}

	// 失效前保留已知的生效值。若同步落库后的 manual/synced 合并读取恰好
	// 遇到 DB 瞬时错误，它可防止已缓存的更高管理员热修复下限被临时降级。
	previousEffective := s.settingService.GetOpenAICodexResponsesVersion(ctx)
	current, updated, ok := s.persistLatestStableVersion(ctx, latest)
	if !ok {
		return
	}
	s.settingService.InvalidateOpenAICodexClientVersionCache()
	effective, sourceOK, effectiveEpoch := s.settingService.getOpenAICodexResponsesVersion(ctx)
	if CompareVersions(current, effective) > 0 {
		effective = current
	}
	cacheTTL := openAICodexClientVersionCacheTTL
	if !sourceOK {
		if CompareVersions(previousEffective, effective) > 0 {
			effective = previousEffective
		}
		// DB 合并未确认时只做短缓存，让后续请求尽快重试读取 manual floor。
		cacheTTL = openAICodexClientVersionErrorTTL
	}
	// 只能发布到产生 effective 的同一 epoch。管理员若在「合并完成→发布」
	// 之间保存了新 manual floor，Invalidate 会让此写入失败，新代次随后自行回源。
	_, _ = s.settingService.storeOpenAICodexResponsesVersionAtEpoch(
		effective,
		cacheTTL,
		effectiveEpoch,
		sourceOK,
	)
	if updated {
		slog.Info("openai_codex_version_synced", "version", current)
	}
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
		current := normalizeStableCodexClientVersion(s.currentSyncedVersion(ctx))
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

// fetchLatestStableVersion 取官方最新稳定版客户端版本号；取不到时返回空串，
// 由调用方保持既有值（不清空、不降级），各失败分支自行落日志。
//
// 主路径 /releases/latest：该端点本身就排除 draft 与 prerelease，直接给出最新正式发布，
// 因此不受该仓库预发布密度的影响，也不需要为了「窗口里得有一条稳定版」而多拉数据——
// 实测单条 release 约 0.3MB，而 per_page=30 的列表页约 10MB。
//
// 回退列表扫描：latest 是跨 tag 家族按 published_at 取的，若同仓库其他组件
// （如 rusty-v8-*）某天发了正式 release 而成为 latest，主路径会被 rust-v 前缀过滤挡掉；
// 此时必须扫一页 release 才能继续跟随官方版本，否则版本号会静默停更。
// 两条路径共用同一套过滤（前缀 / draft / prerelease / 版本号形态），语义不会分叉。
func (s *OpenAICodexVersionSyncService) fetchLatestStableVersion(ctx context.Context) string {
	release, err := s.githubClient.FetchLatestRelease(ctx, openAICodexVersionSyncRepo)
	if err != nil {
		slog.Warn("openai_codex_version_sync_latest_fetch_failed", "error", err)
	} else if version := latestCodexStableReleaseVersion([]*GitHubRelease{release}); version != "" {
		return version
	}

	// 主路径没拿到可用版本（抓取失败，或 latest 不是客户端 tag 家族的稳定版）。
	releases, err := s.githubClient.FetchRecentReleases(ctx, openAICodexVersionSyncRepo, openAICodexVersionSyncPerPage)
	if err != nil {
		slog.Warn("openai_codex_version_sync_fetch_failed", "error", err)
		return ""
	}
	version := latestCodexStableReleaseVersion(releases)
	if version == "" {
		slog.Warn("openai_codex_version_sync_no_stable_release", "repo", openAICodexVersionSyncRepo)
	}
	return version
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

func (s *OpenAICodexVersionSyncService) currentSyncedVersion(ctx context.Context) string {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAICodexClientVersionSynced)
	if err != nil {
		return ""
	}
	return value
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
