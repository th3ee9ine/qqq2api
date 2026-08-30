package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/pkg/pagination"
)

// UsageCleanupRecordType identifies the record families affected by a cleanup
// task.  The empty value is normalized to usage so tasks created by older
// versions retain their original usage-only semantics when they are resumed.
// New callers that want both tables must explicitly select all.
type UsageCleanupRecordType string

const (
	UsageCleanupRecordTypeAll    UsageCleanupRecordType = "all"
	UsageCleanupRecordTypeUsage  UsageCleanupRecordType = "usage"
	UsageCleanupRecordTypeErrors UsageCleanupRecordType = "errors"
)

// Normalize returns a supported record type.  Invalid non-empty values are
// intentionally preserved so the service validation layer can reject them
// instead of silently widening a destructive operation.
func (t UsageCleanupRecordType) Normalize() UsageCleanupRecordType {
	switch t {
	case "":
		return UsageCleanupRecordTypeUsage
	case UsageCleanupRecordTypeAll, UsageCleanupRecordTypeUsage, UsageCleanupRecordTypeErrors:
		return t
	default:
		return t
	}
}

// ParseUsageCleanupRecordType parses the public API value.  An omitted value
// keeps the legacy usage-only behavior, while unknown values are rejected.
func ParseUsageCleanupRecordType(raw string) (UsageCleanupRecordType, error) {
	value := UsageCleanupRecordType(strings.ToLower(strings.TrimSpace(raw)))
	if value == "" {
		return UsageCleanupRecordTypeUsage, nil
	}
	switch value {
	case UsageCleanupRecordTypeAll, UsageCleanupRecordTypeUsage, UsageCleanupRecordTypeErrors:
		return value, nil
	default:
		return "", fmt.Errorf("invalid record_type %q, use all, usage, or errors", raw)
	}
}

const (
	UsageCleanupStatusPending   = "pending"
	UsageCleanupStatusRunning   = "running"
	UsageCleanupStatusSucceeded = "succeeded"
	UsageCleanupStatusFailed    = "failed"
	UsageCleanupStatusCanceled  = "canceled"
)

// UsageCleanupFilters 定义清理任务过滤条件
// 时间范围为必填，其他字段可选
// JSON 序列化用于存储任务参数
//
// start_time/end_time 使用 RFC3339 时间格式
// 以 UTC 或用户时区解析后的时间为准
//
// 说明：
// - nil 表示未设置该过滤条件
// - 过滤条件均为精确匹配
type UsageCleanupFilters struct {
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	RecordType  UsageCleanupRecordType `json:"record_type,omitempty"`
	UserID      *int64                 `json:"user_id,omitempty"`
	APIKeyID    *int64                 `json:"api_key_id,omitempty"`
	AccountID   *int64                 `json:"account_id,omitempty"`
	GroupID     *int64                 `json:"group_id,omitempty"`
	Model       *string                `json:"model,omitempty"`
	RequestType *int16                 `json:"request_type,omitempty"`
	Stream      *bool                  `json:"stream,omitempty"`
	BillingType *int8                  `json:"billing_type,omitempty"`

	// ErrorPhases/ErrorTypes/StatusCodes are used when RecordType includes
	// error records.  They mirror the filters shown in the admin error tab.
	ErrorPhases []string `json:"error_phases,omitempty"`
	ErrorTypes  []string `json:"error_types,omitempty"`
	StatusCodes []int    `json:"status_codes,omitempty"`
}

// UsageCleanupTask 表示使用记录清理任务
// 状态包含 pending/running/succeeded/failed/canceled
type UsageCleanupTask struct {
	ID          int64
	Status      string
	Filters     UsageCleanupFilters
	CreatedBy   int64
	DeletedRows int64
	ErrorMsg    *string
	CanceledBy  *int64
	CanceledAt  *time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// UsageCleanupRepository 定义清理任务持久层接口
type UsageCleanupRepository interface {
	CreateTask(ctx context.Context, task *UsageCleanupTask) error
	ListTasks(ctx context.Context, params pagination.PaginationParams) ([]UsageCleanupTask, *pagination.PaginationResult, error)
	// ClaimNextPendingTask 抢占下一条可执行任务：
	// - 优先 pending
	// - 若 running 超过 staleRunningAfterSeconds（可能由于进程退出/崩溃/超时），允许重新抢占继续执行
	ClaimNextPendingTask(ctx context.Context, staleRunningAfterSeconds int64) (*UsageCleanupTask, error)
	// GetTaskStatus 查询任务状态；若不存在返回 sql.ErrNoRows
	GetTaskStatus(ctx context.Context, taskID int64) (string, error)
	// UpdateTaskProgress 更新任务进度（deleted_rows）用于断点续跑/展示
	UpdateTaskProgress(ctx context.Context, taskID int64, deletedRows int64) error
	// CancelTask 将任务标记为 canceled（仅允许 pending/running）
	CancelTask(ctx context.Context, taskID int64, canceledBy int64) (bool, error)
	MarkTaskSucceeded(ctx context.Context, taskID int64, deletedRows int64) error
	MarkTaskFailed(ctx context.Context, taskID int64, deletedRows int64, errorMsg string) error
	DeleteUsageLogsBatch(ctx context.Context, filters UsageCleanupFilters, limit int) (int64, error)
	DeleteErrorLogsBatch(ctx context.Context, filters UsageCleanupFilters, limit int) (int64, error)
}
