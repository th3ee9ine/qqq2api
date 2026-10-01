//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

// mockGatewayAccountRepo 网关测试用的 mock
type mockGatewayAccountRepo struct {
	accounts           []Account
	accountsByID       map[int64]*Account
	listByGroupFunc    func(ctx context.Context, groupID int64, platforms []string) ([]Account, error)
	listByPlatformFunc func(ctx context.Context, platforms []string) ([]Account, error)
}

func (m *mockGatewayAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	if acc, ok := m.accountsByID[id]; ok {
		return acc, nil
	}
	return nil, errors.New("account not found")
}

func (m *mockGatewayAccountRepo) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	var result []*Account
	for _, id := range ids {
		if acc, ok := m.accountsByID[id]; ok {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (m *mockGatewayAccountRepo) ExistsByID(ctx context.Context, id int64) (bool, error) {
	if m.accountsByID == nil {
		return false, nil
	}
	_, ok := m.accountsByID[id]
	return ok, nil
}

func (m *mockGatewayAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range m.accounts {
		if acc.Platform == platform && acc.IsSchedulable() {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (m *mockGatewayAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	// 测试时不区分 groupID，直接按 platform 过滤
	return m.ListSchedulableByPlatform(ctx, platform)
}

// Stub methods to implement AccountRepository interface
func (m *mockGatewayAccountRepo) Create(ctx context.Context, account *Account) error { return nil }
func (m *mockGatewayAccountRepo) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*Account, error) {
	return nil, nil
}

func (m *mockGatewayAccountRepo) FindByExtraField(ctx context.Context, key string, value any) ([]Account, error) {
	return nil, nil
}

func (m *mockGatewayAccountRepo) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	return nil, nil
}
func (m *mockGatewayAccountRepo) Update(ctx context.Context, account *Account) error { return nil }
func (m *mockGatewayAccountRepo) Delete(ctx context.Context, id int64) error         { return nil }
func (m *mockGatewayAccountRepo) List(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *mockGatewayAccountRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *mockGatewayAccountRepo) ListAllWithFilters(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, error) {
	return nil, nil
}
func (m *mockGatewayAccountRepo) ListByGroup(ctx context.Context, groupID int64) ([]Account, error) {
	return nil, nil
}
func (m *mockGatewayAccountRepo) ListActive(ctx context.Context) ([]Account, error) {
	return nil, nil
}
func (m *mockGatewayAccountRepo) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return nil, nil
}
func (m *mockGatewayAccountRepo) UpdateLastUsed(ctx context.Context, id int64) error { return nil }
func (m *mockGatewayAccountRepo) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	return nil
}
func (m *mockGatewayAccountRepo) SetError(ctx context.Context, id int64, errorMsg string) error {
	return nil
}
func (m *mockGatewayAccountRepo) ClearError(ctx context.Context, id int64) error {
	return nil
}
func (m *mockGatewayAccountRepo) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	return nil
}
func (m *mockGatewayAccountRepo) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
}
func (m *mockGatewayAccountRepo) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	return nil
}
func (m *mockGatewayAccountRepo) ListSchedulable(ctx context.Context) ([]Account, error) {
	return nil, nil
}
func (m *mockGatewayAccountRepo) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	return nil, nil
}
func (m *mockGatewayAccountRepo) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	if m.listByPlatformFunc != nil {
		return m.listByPlatformFunc(ctx, platforms)
	}
	var result []Account
	platformSet := make(map[string]bool)
	for _, p := range platforms {
		platformSet[p] = true
	}
	for _, acc := range m.accounts {
		if platformSet[acc.Platform] && acc.IsSchedulable() {
			result = append(result, acc)
		}
	}
	return result, nil
}
func (m *mockGatewayAccountRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]Account, error) {
	if m.listByGroupFunc != nil {
		return m.listByGroupFunc(ctx, groupID, platforms)
	}
	return m.ListSchedulableByPlatforms(ctx, platforms)
}
func (m *mockGatewayAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	return m.ListSchedulableByPlatform(ctx, platform)
}
func (m *mockGatewayAccountRepo) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	return m.ListSchedulableByPlatforms(ctx, platforms)
}
func (m *mockGatewayAccountRepo) ListModelAvailabilityCandidates(ctx context.Context, _ *int64, platforms []string, _ bool) ([]Account, error) {
	return m.ListSchedulableByPlatforms(ctx, platforms)
}
func (m *mockGatewayAccountRepo) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	return nil
}
func (m *mockGatewayAccountRepo) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	return nil
}
func (m *mockGatewayAccountRepo) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	return nil
}
func (m *mockGatewayAccountRepo) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	return nil
}
func (m *mockGatewayAccountRepo) ClearTempUnschedulable(ctx context.Context, id int64) error {
	return nil
}
func (m *mockGatewayAccountRepo) ClearRateLimit(ctx context.Context, id int64) error { return nil }

func (m *mockGatewayAccountRepo) ClearModelRateLimits(ctx context.Context, id int64) error {
	return nil
}
func (m *mockGatewayAccountRepo) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	return nil
}
func (m *mockGatewayAccountRepo) UpdateSessionWindowEnd(ctx context.Context, id int64, end time.Time) error {
	return nil
}
func (m *mockGatewayAccountRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	return nil
}
func (m *mockGatewayAccountRepo) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	return 0, nil
}

func (m *mockGatewayAccountRepo) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	return nil
}

func (m *mockGatewayAccountRepo) ResetQuotaUsedAndClearRateLimitCooldown(ctx context.Context, id int64) error {
	return nil
}

func (m *mockGatewayAccountRepo) RevertProxyFallback(ctx context.Context, accountID int64) error {
	return nil
}

func (m *mockGatewayAccountRepo) ListShadowsByParent(ctx context.Context, parentID int64) ([]*Account, error) {
	return nil, nil
}

// Verify interface implementation
var _ AccountRepository = (*mockGatewayAccountRepo)(nil)

// mockGatewayGroupRepo 网关测试用的 group repo mock
type mockGatewayGroupRepo struct {
	groups           map[int64]*Group
	getByIDCalls     int
	getByIDLiteCalls int
}

func (m *mockGatewayGroupRepo) GetByID(ctx context.Context, id int64) (*Group, error) {
	m.getByIDCalls++
	if g, ok := m.groups[id]; ok {
		return g, nil
	}
	return nil, errors.New("group not found")
}

func (m *mockGatewayGroupRepo) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	m.getByIDLiteCalls++
	if g, ok := m.groups[id]; ok {
		return g, nil
	}
	return nil, errors.New("group not found")
}

// Stub methods to implement GroupRepository interface
func (m *mockGatewayGroupRepo) Create(ctx context.Context, group *Group) error { return nil }
func (m *mockGatewayGroupRepo) Update(ctx context.Context, group *Group) error { return nil }
func (m *mockGatewayGroupRepo) Delete(ctx context.Context, id int64) error     { return nil }
func (m *mockGatewayGroupRepo) DeleteCascade(ctx context.Context, id int64) ([]int64, error) {
	return nil, nil
}
func (m *mockGatewayGroupRepo) List(ctx context.Context, params pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *mockGatewayGroupRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]Group, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (m *mockGatewayGroupRepo) ListActive(ctx context.Context) ([]Group, error) { return nil, nil }
func (m *mockGatewayGroupRepo) ListActiveByPlatform(ctx context.Context, platform string) ([]Group, error) {
	return nil, nil
}
func (m *mockGatewayGroupRepo) ExistsByName(ctx context.Context, name string) (bool, error) {
	return false, nil
}
func (m *mockGatewayGroupRepo) GetAccountCount(ctx context.Context, groupID int64) (int64, int64, error) {
	return 0, 0, nil
}
func (m *mockGatewayGroupRepo) DeleteAccountGroupsByGroupID(ctx context.Context, groupID int64) (int64, error) {
	return 0, nil
}

func (m *mockGatewayGroupRepo) BindAccountsToGroup(ctx context.Context, groupID int64, accountIDs []int64) error {
	return nil
}

func (m *mockGatewayGroupRepo) GetAccountIDsByGroupIDs(ctx context.Context, groupIDs []int64) ([]int64, error) {
	return nil, nil
}

func (m *mockGatewayGroupRepo) UpdateSortOrders(ctx context.Context, updates []GroupSortOrderUpdate) error {
	return nil
}

var _ GroupRepository = (*mockGatewayGroupRepo)(nil)

// mockGatewayCache 网关测试用的 cache mock
type mockGatewayCache struct {
	sessionBindings map[string]int64
	deletedSessions map[string]int
}

func (m *mockGatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	if id, ok := m.sessionBindings[sessionHash]; ok {
		return id, nil
	}
	return 0, errors.New("not found")
}

func (m *mockGatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	if m.sessionBindings == nil {
		m.sessionBindings = make(map[string]int64)
	}
	m.sessionBindings[sessionHash] = accountID
	return nil
}

func (m *mockGatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	return nil
}

func (m *mockGatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	if m.sessionBindings == nil {
		return nil
	}
	if m.deletedSessions == nil {
		m.deletedSessions = make(map[string]int)
	}
	m.deletedSessions[sessionHash]++
	delete(m.sessionBindings, sessionHash)
	return nil
}

func (m *mockGatewayCache) SetGrokVideoPendingBilling(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}
func (m *mockGatewayCache) GetGrokVideoPendingBilling(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}
func (m *mockGatewayCache) ClaimGrokVideoBilled(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (m *mockGatewayCache) ReleaseGrokVideoBilled(_ context.Context, _ string) error {
	return nil
}

func (m *mockGatewayCache) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (m *mockGatewayCache) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", ErrReasoningContentNotFound
}

type tempUnschedCall struct {
	accountID int64
	until     time.Time
	reason    string
}

// splitChain 辅助函数：按 "-" 分割摘要链
func splitChain(chain string) []string {
	if chain == "" {
		return nil
	}
	var parts []string
	start := 0
	for i := 0; i < len(chain); i++ {
		if chain[i] == '-' {
			parts = append(parts, chain[start:i])
			start = i + 1
		}
	}
	if start < len(chain) {
		parts = append(parts, chain[start:])
	}
	return parts
}

func toolAnthropicSSEStream() string {
	return strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","content":[],"model":"glm-4.7","usage":{"input_tokens":10}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"status\"}"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
}
