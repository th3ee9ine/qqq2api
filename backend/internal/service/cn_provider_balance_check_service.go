package service

import (
	"context"
	"sync"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/config"
)

// cnQuotaProber 抽象额度探测（*CNProviderQuotaService 实现，测试可替换）。
type cnQuotaProber interface {
	QueryUsage(ctx context.Context, accountID int64) (*CNProviderQuotaProbeResult, error)
}

// CNProviderBalanceCheckService 周期性探测国产供应商账号：
//   - payg（按量付费）：余额低于阈值则临时停调，恢复则清除（仅清除本服务写入的停调）；
//   - coding plan：调用 CNProviderQuotaService 探测 5h/weekly 滚动窗口并落 extra 快照，
//     调度阈值评估（cnProviderThresholdCandidates）据此自动停调/恢复。
//
// 克隆自 AccountExpiryService 的 Start/Stop/runOnce + ticker 骨架。
// 余额探测仅覆盖有公开余额端点的 kimi / deepseek；智谱无余额端点，仅靠响应式 429/402。
// 额度探测覆盖 kimi / zhipu 的 coding plan 账号（deepseek 无 coding 套餐）。
type CNProviderBalanceCheckService struct {
	accountRepo    AccountRepository
	balanceService *CNProviderBalanceService
	quotaService   cnQuotaProber
	cfg            *config.Config
	interval       time.Duration
	stopCh         chan struct{}
	stopOnce       sync.Once
	wg             sync.WaitGroup
}

// NewCNProviderBalanceCheckService 构造周期余额/额度检测服务。
// interval <= 0 时 Start() 直接返回（不启动），便于通过配置关闭。
func NewCNProviderBalanceCheckService(
	accountRepo AccountRepository,
	balanceService *CNProviderBalanceService,
	quotaService *CNProviderQuotaService,
	cfg *config.Config,
	interval time.Duration,
) *CNProviderBalanceCheckService {
	return &CNProviderBalanceCheckService{
		accountRepo:    accountRepo,
		balanceService: balanceService,
		quotaService:   quotaService,
		cfg:            cfg,
		interval:       interval,
		stopCh:         make(chan struct{}),
	}
}

func (s *CNProviderBalanceCheckService) Start() {
	// Kimi, Zhipu/GLM, and DeepSeek are retired. Keep the historical service
	// type for migrations/tests, but never start its production poller.
}

func (s *CNProviderBalanceCheckService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
}

func (s *CNProviderBalanceCheckService) runOnce() {
	// Fail closed even for direct callers: no retired provider account may be
	// loaded or probed after the platform shutdown.
}

// allCNBalancesBelowThreshold 判断全部币种余额是否均低于阈值。
// 无明细时退回主币种判定（与旧行为一致）。
func allCNBalancesBelowThreshold(result *CNProviderBalanceResult, threshold float64) bool {
	if len(result.Balances) == 0 {
		return result.Balance < threshold
	}
	for _, entry := range result.Balances {
		if entry.Balance >= threshold {
			return false
		}
	}
	return true
}
