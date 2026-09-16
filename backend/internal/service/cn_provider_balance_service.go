package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/config"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/singleflight"
)

// 国产供应商 payg（按量付费）账号余额探测服务。
//
// 仅覆盖有公开余额端点的供应商：
//   - Kimi/Moonshot：GET https://api.moonshot.cn/v1/users/me/balance (Bearer) → data.available_balance
//
// 智谱（zhipu）无公开余额端点（OpenAPI 规格验证），仅靠响应式 429/402（见
// ratelimit_cn_providers.go）。
const (
	cnBalanceUpstreamTimeout = 15 * time.Second
	cnBalanceMaxBodyBytes    = 256 * 1024

	// Extra 余额快照键后缀（加 provider 前缀）。
	cnBalanceExtraSuffixBalance   = "balance"
	cnBalanceExtraSuffixCurrency  = "balance_currency"
	cnBalanceExtraSuffixAvailable = "balance_available" // 余额快照健康标记
	cnBalanceExtraSuffixUpdated   = "balance_updated_at"
	cnBalanceExtraSuffixBalances  = "balances" // 币种明细
)

// CNProviderBalanceEntry 是单一币种的余额明细。
type CNProviderBalanceEntry struct {
	Currency string  `json:"currency"`
	Balance  float64 `json:"balance"`
}

// CNProviderBalanceResult 是余额探测的返回结构（管理端 + UI 消费）。
type CNProviderBalanceResult struct {
	Provider string `json:"provider"`
	Success  bool   `json:"success"`
	// Balance/Currency 为主币种；Balances 保留明细以兼容现有快照格式。
	Balance    float64                  `json:"balance"`
	Currency   string                   `json:"currency,omitempty"`
	Balances   []CNProviderBalanceEntry `json:"balances,omitempty"`
	Available  bool                     `json:"available"` // 成功读取余额时为 true
	StatusCode int                      `json:"status_code,omitempty"`
	FetchedAt  int64                    `json:"fetched_at"`
	Persisted  bool                     `json:"persisted"`
	Error      string                   `json:"error,omitempty"`
}

// CNProviderBalanceService 探测 Kimi payg 账号的账户余额。
type CNProviderBalanceService struct {
	accountRepo  AccountRepository
	proxyRepo    ProxyRepository
	httpUpstream HTTPUpstream
	cfg          *config.Config
	flight       singleflight.Group
}

// NewCNProviderBalanceService 构造余额探测服务。
func NewCNProviderBalanceService(
	accountRepo AccountRepository,
	proxyRepo ProxyRepository,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
) *CNProviderBalanceService {
	return &CNProviderBalanceService{
		accountRepo:  accountRepo,
		proxyRepo:    proxyRepo,
		httpUpstream: httpUpstream,
		cfg:          cfg,
	}
}

// QueryBalance 探测指定 payg 账号的余额并落 Extra 快照。
func (s *CNProviderBalanceService) QueryBalance(ctx context.Context, accountID int64) (*CNProviderBalanceResult, error) {
	account, err := s.loadPayGAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return s.QueryBalanceForAccount(ctx, account)
}

// QueryBalanceForAccount 探测已加载账号（配额监控 fetcher / 周期余额检测复用，
// 避免二次 GetByID）。singleflight key 与 QueryBalance 相同，按账号 ID 合并。
func (s *CNProviderBalanceService) QueryBalanceForAccount(ctx context.Context, account *Account) (*CNProviderBalanceResult, error) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, infraerrors.New(http.StatusInternalServerError, "CN_BALANCE_NOT_CONFIGURED", "cn provider balance service is not configured")
	}
	if err := validatePayGAccount(account); err != nil {
		return nil, err
	}
	key := "cn_balance:" + strconv.FormatInt(account.ID, 10)
	resultCh := s.flight.DoChan(key, func() (any, error) {
		probeCtx, cancel := context.WithTimeout(context.Background(), cnBalanceUpstreamTimeout+5*time.Second)
		defer cancel()
		return s.queryBalanceForAccount(probeCtx, account)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case flightResult := <-resultCh:
		if flightResult.Err != nil {
			return nil, flightResult.Err
		}
		result, ok := flightResult.Val.(*CNProviderBalanceResult)
		if !ok || result == nil {
			return nil, infraerrors.New(http.StatusInternalServerError, "CN_BALANCE_PROBE_RESULT_INVALID", "invalid cn provider balance probe result")
		}
		cloned := *result
		return &cloned, nil
	}
}

func (s *CNProviderBalanceService) queryBalanceForAccount(ctx context.Context, account *Account) (*CNProviderBalanceResult, error) {
	provider := account.Platform
	if provider != PlatformKimi {
		return nil, infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_ENDPOINT", "account provider has no balance endpoint")
	}

	apiKey := strings.TrimSpace(account.GetCNAPIKey())
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CN_BALANCE_NO_APIKEY", "account api_key is empty")
	}

	targetURL := cnBalanceURL(account)
	// 探测发起前过出站 URL 安全策略（与网关转发/Grok 探测同一套校验）：
	validatedURL, err := cnValidateProbeURL(s.cfg, targetURL)
	if err != nil {
		return nil, infraerrors.New(http.StatusForbidden, "CN_BALANCE_URL_REJECTED", err.Error())
	}
	targetURL = validatedURL
	proxyURL := s.resolveProxyURL(ctx, account)
	callCtx, cancel := context.WithTimeout(ctx, cnBalanceUpstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "CN_BALANCE_REQUEST_BUILD_FAILED", "build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	account.ApplyHeaderOverrides(req.Header)

	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CN_BALANCE_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, cnBalanceMaxBodyBytes))

	now := time.Now().UTC()
	result := &CNProviderBalanceResult{
		Provider:   provider,
		FetchedAt:  now.Unix(),
		StatusCode: resp.StatusCode,
		Available:  true,
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Error = fmt.Sprintf("Authentication failed (HTTP %d)", resp.StatusCode)
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Error = fmt.Sprintf("API error (HTTP %d): %s", resp.StatusCode, truncate(strings.TrimSpace(string(bodyBytes)), 240))
		return result, nil
	}

	// Moonshot returns a single CNY balance.
	balance, _ := cnParseF64(gjson.GetBytes(bodyBytes, "data.available_balance").Value())
	entries := []CNProviderBalanceEntry{{Currency: "CNY", Balance: balance}}
	result.Balances = entries
	result.Balance = entries[0].Balance
	result.Currency = entries[0].Currency
	result.Available = true
	result.Success = true

	balanceUpdates := make([]any, 0, len(entries))
	for _, entry := range entries {
		balanceUpdates = append(balanceUpdates, map[string]any{
			"currency": entry.Currency,
			"balance":  entry.Balance,
		})
	}
	updates := map[string]any{
		cnExtraKey(provider, cnBalanceExtraSuffixBalance):   result.Balance,
		cnExtraKey(provider, cnBalanceExtraSuffixCurrency):  result.Currency,
		cnExtraKey(provider, cnBalanceExtraSuffixAvailable): result.Available,
		cnExtraKey(provider, cnBalanceExtraSuffixUpdated):   now.Format(time.RFC3339),
		cnExtraKey(provider, cnBalanceExtraSuffixBalances):  balanceUpdates,
		// 余额探测成功即清除响应式 402/429 写下的 balance_low 标记。
		cnExtraKey(provider, cnBalanceExtraSuffixLow): false,
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		slog.Warn("cn_balance_persist_failed", "account_id", account.ID, "provider", provider, "error", err)
	} else {
		result.Persisted = true
	}
	return result, nil
}

// loadPayGAccount 加载 payg 模式的国产供应商账号（余额仅对 payg 有意义；coding 走额度）。
func (s *CNProviderBalanceService) loadPayGAccount(ctx context.Context, accountID int64) (*Account, error) {
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusNotFound, "CN_BALANCE_ACCOUNT_NOT_FOUND", "account not found: %v", err)
	}
	if err := validatePayGAccount(account); err != nil {
		return nil, err
	}
	return account, nil
}

// validatePayGAccount 加载后的非 DB 校验（ForAccount 入口同样复用，
// 保证直传 account 也不绕过平台/模式检查）。
func validatePayGAccount(account *Account) error {
	if account == nil {
		return infraerrors.New(http.StatusNotFound, "CN_BALANCE_ACCOUNT_NOT_FOUND", "account not found")
	}
	if !account.IsCNProvider() {
		return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_INVALID_PLATFORM", "account is not a CN provider account")
	}
	// coding 账号走额度探测，余额端点不适用。
	if account.IsCodingPlan() {
		return infraerrors.New(http.StatusBadRequest, "CN_BALANCE_CODING_PLAN", "coding plan account has no balance endpoint; use quota probe")
	}
	return nil
}

func (s *CNProviderBalanceService) resolveProxyURL(ctx context.Context, account *Account) string {
	if account == nil || account.ProxyID == nil {
		return ""
	}
	if account.Proxy != nil {
		return account.Proxy.URL()
	}
	if s != nil && s.proxyRepo != nil {
		if proxy, err := s.proxyRepo.GetByID(ctx, *account.ProxyID); err == nil && proxy != nil {
			account.Proxy = proxy
			return proxy.URL()
		}
	}
	return ""
}

// cnBalanceURL 解析账号的余额端点。
//
//   - Kimi：固定 https://api.moonshot.cn/v1/users/me/balance（与 base_url 无关，Moonshot 仅此一处）
func cnBalanceURL(account *Account) string {
	switch account.Platform {
	case PlatformKimi:
		return "https://api.moonshot.cn/v1/users/me/balance"
	default:
		return ""
	}
}
