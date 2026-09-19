package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

const (
	CodexTurnStateCollectionSourceManual    = "manual"
	CodexTurnStateCollectionSourceBulk      = "bulk"
	CodexTurnStateCollectionSourceAutomatic = "automatic"
	CodexTurnStateCollectionSourceRenewal   = "renewal"
	CodexTurnStateCollectionSourceRetry     = "retry"

	CodexTurnStateCollectionTaskStatusQueued    = "queued"
	CodexTurnStateCollectionTaskStatusRunning   = "running"
	CodexTurnStateCollectionTaskStatusSucceeded = "succeeded"
	CodexTurnStateCollectionTaskStatusFailed    = "failed"
	CodexTurnStateCollectionTaskStatusCanceled  = "canceled"

	CodexTurnStateCollectionTaskStageQueued          = "queued"
	CodexTurnStateCollectionTaskStagePreparing       = "preparing"
	CodexTurnStateCollectionTaskStageLoadingAccount  = "loading_account"
	CodexTurnStateCollectionTaskStageResolvingRoutes = "resolving_routes"
	CodexTurnStateCollectionTaskStageCollecting      = "collecting"
	CodexTurnStateCollectionTaskStageVerifying       = "verifying"
	CodexTurnStateCollectionTaskStagePersisting      = "persisting"
	CodexTurnStateCollectionTaskStageRetryWait       = "retry_wait"
	CodexTurnStateCollectionTaskStageCompleted       = "completed"
	CodexTurnStateCollectionTaskStageFailed          = "failed"
	CodexTurnStateCollectionTaskStageCanceled        = "canceled"

	codexTurnStateCollectionTaskLimit      = 500
	codexTurnStateCollectionTaskEventLimit = 100
)

type codexTurnStateCollectionContextKey uint8

const (
	codexTurnStateCollectionSourceContextKey codexTurnStateCollectionContextKey = iota
	codexTurnStateCollectionTaskIDContextKey
)

// CodexTurnStateCollectionTaskEvent is one redacted lifecycle transition. Error
// contains only a fixed safe code; upstream response text is never retained.
type CodexTurnStateCollectionTaskEvent struct {
	AtMS     int64  `json:"at_ms"`
	Status   string `json:"status"`
	Stage    string `json:"stage"`
	Progress int    `json:"progress"`
	Error    string `json:"error,omitempty"`
}

// CodexTurnStateCollectionTask is the admin-facing snapshot of one account and
// owner-model collection. It deliberately excludes credentials, proxy URLs,
// opaque Turn State values, and raw upstream errors.
type CodexTurnStateCollectionTask struct {
	ID              string                              `json:"id"`
	AccountID       int64                               `json:"account_id"`
	AccountName     string                              `json:"account_name"`
	RequestModel    string                              `json:"request_model"`
	OwnerModel      string                              `json:"owner_model"`
	Source          string                              `json:"source"`
	Status          string                              `json:"status"`
	Stage           string                              `json:"stage"`
	Progress        int                                 `json:"progress"`
	ProgressCurrent int                                 `json:"progress_current"`
	ProgressTotal   int                                 `json:"progress_total"`
	CreatedAtMS     int64                               `json:"created_at_ms"`
	StartedAtMS     int64                               `json:"started_at_ms,omitempty"`
	UpdatedAtMS     int64                               `json:"updated_at_ms"`
	FinishedAtMS    int64                               `json:"finished_at_ms,omitempty"`
	Error           string                              `json:"error,omitempty"`
	RetryOf         string                              `json:"retry_of,omitempty"`
	Events          []CodexTurnStateCollectionTaskEvent `json:"events,omitempty"`
	CanCancel       bool                                `json:"can_cancel"`
	CanRetry        bool                                `json:"can_retry"`
}

// CodexTurnStateCollectionTaskInput contains only immutable task identity.
// Source can be omitted when it is already attached to parent via
// WithCodexTurnStateCollectionSource.
type CodexTurnStateCollectionTaskInput struct {
	AccountID    int64
	AccountName  string
	RequestModel string
	OwnerModel   string
	Source       string
	RetryOf      string
}

// CodexTurnStateCollectionTaskRetryInfo is the immutable, read-only input for
// an administrator retry. Callers cannot mutate the original task snapshot.
type CodexTurnStateCollectionTaskRetryInfo struct {
	TaskID       string `json:"task_id"`
	AccountID    int64  `json:"account_id"`
	AccountName  string `json:"account_name"`
	RequestModel string `json:"request_model"`
	OwnerModel   string `json:"owner_model"`
	Source       string `json:"source"`
}

type codexTurnStateCollectionTaskRecord struct {
	task   CodexTurnStateCollectionTask
	ctx    context.Context
	cancel context.CancelFunc
}

type codexTurnStateCollectionTaskRegistry struct {
	mu      sync.RWMutex
	seq     uint64
	tasks   map[string]*codexTurnStateCollectionTaskRecord
	order   []string // oldest to newest
	maxSize int
	stopped bool
}

func newCodexTurnStateCollectionTaskRegistry(maxSize int) *codexTurnStateCollectionTaskRegistry {
	if maxSize <= 0 || maxSize > codexTurnStateCollectionTaskLimit {
		maxSize = codexTurnStateCollectionTaskLimit
	}
	return &codexTurnStateCollectionTaskRegistry{
		tasks:   make(map[string]*codexTurnStateCollectionTaskRecord, maxSize),
		order:   make([]string, 0, maxSize),
		maxSize: maxSize,
	}
}

// WithCodexTurnStateCollectionSource tags an administrator request as manual or
// bulk. Internal automatic and renewal callers may use the same helper. Invalid
// values are ignored and later fall back to the caller's explicit/default
// source instead of becoming a new unbounded label.
func WithCodexTurnStateCollectionSource(ctx context.Context, source string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if normalized, ok := normalizeCodexTurnStateCollectionSource(source); ok {
		return context.WithValue(ctx, codexTurnStateCollectionSourceContextKey, normalized)
	}
	return ctx
}

// CodexTurnStateCollectionSourceFromContext returns an empty string when no
// valid source was attached.
func CodexTurnStateCollectionSourceFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	source, _ := ctx.Value(codexTurnStateCollectionSourceContextKey).(string)
	normalized, _ := normalizeCodexTurnStateCollectionSource(source)
	return normalized
}

// CodexTurnStateCollectionTaskIDFromContext lets the manual scheduler bind a
// retry reservation to the exact worker instead of creating a duplicate task.
func CodexTurnStateCollectionTaskIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	taskID, _ := ctx.Value(codexTurnStateCollectionTaskIDContextKey).(string)
	return strings.TrimSpace(taskID)
}

func withCodexTurnStateCollectionTaskID(ctx context.Context, taskID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if taskID = strings.TrimSpace(taskID); taskID == "" {
		return ctx
	}
	return context.WithValue(ctx, codexTurnStateCollectionTaskIDContextKey, taskID)
}

func normalizeCodexTurnStateCollectionSource(source string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case CodexTurnStateCollectionSourceManual:
		return CodexTurnStateCollectionSourceManual, true
	case CodexTurnStateCollectionSourceBulk:
		return CodexTurnStateCollectionSourceBulk, true
	case CodexTurnStateCollectionSourceAutomatic:
		return CodexTurnStateCollectionSourceAutomatic, true
	case CodexTurnStateCollectionSourceRenewal:
		return CodexTurnStateCollectionSourceRenewal, true
	case CodexTurnStateCollectionSourceRetry:
		return CodexTurnStateCollectionSourceRetry, true
	default:
		return "", false
	}
}

func normalizeCodexTurnStateCollectionStage(stage, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case CodexTurnStateCollectionTaskStageQueued:
		return CodexTurnStateCollectionTaskStageQueued
	case CodexTurnStateCollectionTaskStagePreparing:
		return CodexTurnStateCollectionTaskStagePreparing
	case CodexTurnStateCollectionTaskStageLoadingAccount:
		return CodexTurnStateCollectionTaskStageLoadingAccount
	case CodexTurnStateCollectionTaskStageResolvingRoutes:
		return CodexTurnStateCollectionTaskStageResolvingRoutes
	case CodexTurnStateCollectionTaskStageCollecting:
		return CodexTurnStateCollectionTaskStageCollecting
	case CodexTurnStateCollectionTaskStageVerifying:
		return CodexTurnStateCollectionTaskStageVerifying
	case CodexTurnStateCollectionTaskStagePersisting:
		return CodexTurnStateCollectionTaskStagePersisting
	case CodexTurnStateCollectionTaskStageRetryWait:
		return CodexTurnStateCollectionTaskStageRetryWait
	case CodexTurnStateCollectionTaskStageCompleted:
		return CodexTurnStateCollectionTaskStageCompleted
	case CodexTurnStateCollectionTaskStageFailed:
		return CodexTurnStateCollectionTaskStageFailed
	case CodexTurnStateCollectionTaskStageCanceled:
		return CodexTurnStateCollectionTaskStageCanceled
	default:
		return fallback
	}
}

func safeCodexTurnStateCollectionTaskError(code string) string {
	if code = safeCodexTurnStateAutoError(strings.TrimSpace(code)); code != "" {
		return code
	}
	return "request_failed"
}

func codexTurnStateCollectionTaskTerminal(status string) bool {
	switch status {
	case CodexTurnStateCollectionTaskStatusSucceeded,
		CodexTurnStateCollectionTaskStatusFailed,
		CodexTurnStateCollectionTaskStatusCanceled:
		return true
	default:
		return false
	}
}

func setCodexTurnStateCollectionTaskCapabilities(task *CodexTurnStateCollectionTask) {
	if task == nil {
		return
	}
	task.CanCancel = task.Status == CodexTurnStateCollectionTaskStatusQueued || task.Status == CodexTurnStateCollectionTaskStatusRunning
	task.CanRetry = task.Status == CodexTurnStateCollectionTaskStatusFailed || task.Status == CodexTurnStateCollectionTaskStatusCanceled
}

func cloneCodexTurnStateCollectionTask(task *CodexTurnStateCollectionTask) *CodexTurnStateCollectionTask {
	if task == nil {
		return nil
	}
	clone := *task
	clone.Events = append([]CodexTurnStateCollectionTaskEvent(nil), task.Events...)
	setCodexTurnStateCollectionTaskCapabilities(&clone)
	return &clone
}

func appendCodexTurnStateCollectionTaskEvent(task *CodexTurnStateCollectionTask, nowMS int64) {
	if task == nil {
		return
	}
	event := CodexTurnStateCollectionTaskEvent{
		AtMS:     nowMS,
		Status:   task.Status,
		Stage:    task.Stage,
		Progress: task.Progress,
		Error:    task.Error,
	}
	if len(task.Events) >= codexTurnStateCollectionTaskEventLimit {
		copy(task.Events, task.Events[len(task.Events)-codexTurnStateCollectionTaskEventLimit+1:])
		task.Events = task.Events[:codexTurnStateCollectionTaskEventLimit-1]
	}
	task.Events = append(task.Events, event)
}

func (r *codexTurnStateCollectionTaskRegistry) nextIDLocked(now time.Time) string {
	r.seq++
	return fmt.Sprintf("cts_%s_%s", strconv.FormatInt(now.UnixMilli(), 36), strconv.FormatUint(r.seq, 36))
}

func (r *codexTurnStateCollectionTaskRegistry) makeRoomLocked() bool {
	if len(r.tasks) < r.maxSize {
		return true
	}
	for index, taskID := range r.order {
		record := r.tasks[taskID]
		if record == nil || codexTurnStateCollectionTaskTerminal(record.task.Status) {
			delete(r.tasks, taskID)
			copy(r.order[index:], r.order[index+1:])
			r.order = r.order[:len(r.order)-1]
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) codexTurnStateCollectionTasks() *codexTurnStateCollectionTaskRegistry {
	if s == nil {
		return nil
	}
	s.openaiTurnStateTasksOnce.Do(func() {
		if s.openaiTurnStateTasks == nil {
			s.openaiTurnStateTasks = newCodexTurnStateCollectionTaskRegistry(codexTurnStateCollectionTaskLimit)
		}
	})
	return s.openaiTurnStateTasks
}

// CreateCodexTurnStateCollectionTask registers one queued account/model task
// and returns its cancelable worker context. At capacity, completed history is
// evicted oldest-first; 500 simultaneously active tasks are rejected rather
// than silently losing their cancellation handles.
func (s *OpenAIGatewayService) CreateCodexTurnStateCollectionTask(parent context.Context, input CodexTurnStateCollectionTaskInput) (*CodexTurnStateCollectionTask, context.Context, error) {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return nil, nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_TASKS_UNAVAILABLE", "Codex Turn State task registry is unavailable")
	}
	if input.AccountID <= 0 {
		return nil, nil, infraerrors.New(http.StatusBadRequest, "INVALID_ACCOUNT", "invalid account")
	}
	input.RequestModel = strings.TrimSpace(input.RequestModel)
	input.OwnerModel = strings.TrimSpace(input.OwnerModel)
	if input.RequestModel == "" || input.OwnerModel == "" {
		return nil, nil, infraerrors.New(http.StatusBadRequest, "INVALID_CODEX_TURN_STATE_TASK", "invalid Codex Turn State task")
	}
	if parent == nil {
		parent = context.Background()
	}
	source, ok := normalizeCodexTurnStateCollectionSource(input.Source)
	if !ok {
		source = CodexTurnStateCollectionSourceFromContext(parent)
	}
	if source == "" {
		source = CodexTurnStateCollectionSourceAutomatic
	}

	now := time.Now()
	// Collection is asynchronous. HTTP disconnects and handler deadlines must
	// not abort it; only the registry cancel action or service shutdown should.
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	registry.mu.Lock()
	if registry.stopped {
		registry.mu.Unlock()
		cancel()
		return nil, nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_TASKS_STOPPED", "Codex Turn State task registry is stopped")
	}
	if !registry.makeRoomLocked() {
		registry.mu.Unlock()
		cancel()
		return nil, nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_TASK_CAPACITY_EXCEEDED", "Codex Turn State task registry is at capacity")
	}
	taskID := registry.nextIDLocked(now)
	ctx = withCodexTurnStateCollectionTaskID(ctx, taskID)
	task := CodexTurnStateCollectionTask{
		ID:           taskID,
		AccountID:    input.AccountID,
		AccountName:  boundedCodexTurnStateCollectionLabel(input.AccountName),
		RequestModel: input.RequestModel,
		OwnerModel:   input.OwnerModel,
		Source:       source,
		Status:       CodexTurnStateCollectionTaskStatusQueued,
		Stage:        CodexTurnStateCollectionTaskStageQueued,
		CreatedAtMS:  now.UnixMilli(),
		UpdatedAtMS:  now.UnixMilli(),
		RetryOf:      strings.TrimSpace(input.RetryOf),
		Events:       make([]CodexTurnStateCollectionTaskEvent, 0, 8),
	}
	setCodexTurnStateCollectionTaskCapabilities(&task)
	appendCodexTurnStateCollectionTaskEvent(&task, now.UnixMilli())
	registry.tasks[taskID] = &codexTurnStateCollectionTaskRecord{task: task, ctx: ctx, cancel: cancel}
	registry.order = append(registry.order, taskID)
	result := cloneCodexTurnStateCollectionTask(&task)
	registry.mu.Unlock()
	return result, ctx, nil
}

// StartCodexTurnStateCollectionTask marks a queued task as running.
func (s *OpenAIGatewayService) StartCodexTurnStateCollectionTask(taskID, stage string, total int) (*CodexTurnStateCollectionTask, bool) {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return nil, false
	}
	nowMS := time.Now().UnixMilli()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	record := registry.tasks[strings.TrimSpace(taskID)]
	if record == nil || codexTurnStateCollectionTaskTerminal(record.task.Status) {
		return nil, false
	}
	record.task.Status = CodexTurnStateCollectionTaskStatusRunning
	record.task.Stage = normalizeCodexTurnStateCollectionStage(stage, CodexTurnStateCollectionTaskStagePreparing)
	if record.task.StartedAtMS == 0 {
		record.task.StartedAtMS = nowMS
	}
	if total > 0 {
		record.task.ProgressTotal = total
	}
	record.task.UpdatedAtMS = nowMS
	setCodexTurnStateCollectionTaskCapabilities(&record.task)
	appendCodexTurnStateCollectionTaskEvent(&record.task, nowMS)
	return cloneCodexTurnStateCollectionTask(&record.task), true
}

// UpdateCodexTurnStateCollectionTask records bounded progress for a live task.
func (s *OpenAIGatewayService) UpdateCodexTurnStateCollectionTask(taskID, stage string, current, total int) (*CodexTurnStateCollectionTask, bool) {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return nil, false
	}
	nowMS := time.Now().UnixMilli()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	record := registry.tasks[strings.TrimSpace(taskID)]
	if record == nil || codexTurnStateCollectionTaskTerminal(record.task.Status) {
		return nil, false
	}
	if record.task.Status == CodexTurnStateCollectionTaskStatusQueued {
		record.task.Status = CodexTurnStateCollectionTaskStatusRunning
		if record.task.StartedAtMS == 0 {
			record.task.StartedAtMS = nowMS
		}
	}
	record.task.Stage = normalizeCodexTurnStateCollectionStage(stage, record.task.Stage)
	if total > 0 {
		record.task.ProgressTotal = total
		if current < 0 {
			current = 0
		}
		if current > total {
			current = total
		}
		record.task.ProgressCurrent = current
		record.task.Progress = current * 100 / total
	}
	if record.task.Progress > 99 {
		record.task.Progress = 99
	}
	record.task.UpdatedAtMS = nowMS
	setCodexTurnStateCollectionTaskCapabilities(&record.task)
	appendCodexTurnStateCollectionTaskEvent(&record.task, nowMS)
	return cloneCodexTurnStateCollectionTask(&record.task), true
}

func (s *OpenAIGatewayService) finishCodexTurnStateCollectionTask(taskID, status, stage, errorCode string) (*CodexTurnStateCollectionTask, bool) {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return nil, false
	}
	nowMS := time.Now().UnixMilli()
	registry.mu.Lock()
	record := registry.tasks[strings.TrimSpace(taskID)]
	if record == nil || codexTurnStateCollectionTaskTerminal(record.task.Status) {
		registry.mu.Unlock()
		return nil, false
	}
	record.task.Status = status
	record.task.Stage = stage
	record.task.UpdatedAtMS = nowMS
	record.task.FinishedAtMS = nowMS
	if status == CodexTurnStateCollectionTaskStatusSucceeded {
		record.task.Error = ""
		record.task.Progress = 100
		if record.task.ProgressTotal > 0 {
			record.task.ProgressCurrent = record.task.ProgressTotal
		}
	} else if status == CodexTurnStateCollectionTaskStatusFailed {
		record.task.Error = safeCodexTurnStateCollectionTaskError(errorCode)
	}
	setCodexTurnStateCollectionTaskCapabilities(&record.task)
	appendCodexTurnStateCollectionTaskEvent(&record.task, nowMS)
	cancel := record.cancel
	record.cancel = nil
	result := cloneCodexTurnStateCollectionTask(&record.task)
	registry.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return result, true
}

func (s *OpenAIGatewayService) CompleteCodexTurnStateCollectionTask(taskID string) (*CodexTurnStateCollectionTask, bool) {
	return s.finishCodexTurnStateCollectionTask(taskID, CodexTurnStateCollectionTaskStatusSucceeded, CodexTurnStateCollectionTaskStageCompleted, "")
}

func (s *OpenAIGatewayService) FailCodexTurnStateCollectionTask(taskID, errorCode string) (*CodexTurnStateCollectionTask, bool) {
	return s.finishCodexTurnStateCollectionTask(taskID, CodexTurnStateCollectionTaskStatusFailed, CodexTurnStateCollectionTaskStageFailed, errorCode)
}

// ListCodexTurnStateCollectionTasks returns detached snapshots newest-first.
func (s *OpenAIGatewayService) ListCodexTurnStateCollectionTasks() []*CodexTurnStateCollectionTask {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return []*CodexTurnStateCollectionTask{}
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	result := make([]*CodexTurnStateCollectionTask, 0, len(registry.order))
	for index := len(registry.order) - 1; index >= 0; index-- {
		if record := registry.tasks[registry.order[index]]; record != nil {
			summary := cloneCodexTurnStateCollectionTask(&record.task)
			summary.Events = nil
			result = append(result, summary)
		}
	}
	return result
}

// GetCodexTurnStateCollectionTask returns a detached task snapshot.
func (s *OpenAIGatewayService) GetCodexTurnStateCollectionTask(taskID string) (*CodexTurnStateCollectionTask, bool) {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return nil, false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	record := registry.tasks[strings.TrimSpace(taskID)]
	if record == nil {
		return nil, false
	}
	return cloneCodexTurnStateCollectionTask(&record.task), true
}

// CodexTurnStateCollectionTaskContext returns the cancelable context owned by a
// live task. Workers should use this context for every network and persistence
// operation so the admin cancel action takes effect promptly.
func (s *OpenAIGatewayService) CodexTurnStateCollectionTaskContext(taskID string) (context.Context, bool) {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return nil, false
	}
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	record := registry.tasks[strings.TrimSpace(taskID)]
	if record == nil || codexTurnStateCollectionTaskTerminal(record.task.Status) || record.ctx == nil {
		return nil, false
	}
	return record.ctx, true
}

// CancelCodexTurnStateCollectionTask atomically publishes cancellation before
// signaling the worker context. Late worker updates cannot overwrite it.
func (s *OpenAIGatewayService) CancelCodexTurnStateCollectionTask(taskID string) (*CodexTurnStateCollectionTask, error) {
	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_TASKS_UNAVAILABLE", "Codex Turn State task registry is unavailable")
	}
	taskID = strings.TrimSpace(taskID)
	nowMS := time.Now().UnixMilli()
	registry.mu.Lock()
	record := registry.tasks[taskID]
	if record == nil {
		registry.mu.Unlock()
		return nil, infraerrors.New(http.StatusNotFound, "CODEX_TURN_STATE_TASK_NOT_FOUND", "Codex Turn State task not found")
	}
	if codexTurnStateCollectionTaskTerminal(record.task.Status) {
		registry.mu.Unlock()
		return nil, infraerrors.New(http.StatusConflict, "CODEX_TURN_STATE_TASK_NOT_CANCELABLE", "Codex Turn State task is no longer cancelable")
	}
	record.task.Status = CodexTurnStateCollectionTaskStatusCanceled
	record.task.Stage = CodexTurnStateCollectionTaskStageCanceled
	record.task.Error = ""
	record.task.UpdatedAtMS = nowMS
	record.task.FinishedAtMS = nowMS
	setCodexTurnStateCollectionTaskCapabilities(&record.task)
	appendCodexTurnStateCollectionTaskEvent(&record.task, nowMS)
	cancel := record.cancel
	record.cancel = nil
	result := cloneCodexTurnStateCollectionTask(&record.task)
	registry.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return result, nil
}

// StopCodexTurnStateCollectionTasks cancels every queued/running task during
// gateway shutdown. It is idempotent and retains their terminal snapshots for
// the details page.
func (s *OpenAIGatewayService) StopCodexTurnStateCollectionTasks() {
	if s == nil {
		return
	}
	s.openaiTurnStateMu.Lock()
	s.openaiTurnStateStopping = true
	s.openaiTurnStateMu.Unlock()

	registry := s.codexTurnStateCollectionTasks()
	if registry == nil {
		return
	}
	nowMS := time.Now().UnixMilli()
	cancels := make([]context.CancelFunc, 0)
	registry.mu.Lock()
	registry.stopped = true
	for _, taskID := range registry.order {
		record := registry.tasks[taskID]
		if record == nil || codexTurnStateCollectionTaskTerminal(record.task.Status) {
			continue
		}
		record.task.Status = CodexTurnStateCollectionTaskStatusCanceled
		record.task.Stage = CodexTurnStateCollectionTaskStageCanceled
		record.task.Error = ""
		record.task.UpdatedAtMS = nowMS
		record.task.FinishedAtMS = nowMS
		setCodexTurnStateCollectionTaskCapabilities(&record.task)
		appendCodexTurnStateCollectionTaskEvent(&record.task, nowMS)
		if record.cancel != nil {
			cancels = append(cancels, record.cancel)
			record.cancel = nil
		}
	}
	registry.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// CodexTurnStateCollectionTaskRetryInfo returns immutable scheduling data only
// for terminal tasks that the UI is allowed to retry.
func (s *OpenAIGatewayService) CodexTurnStateCollectionTaskRetryInfo(taskID string) (*CodexTurnStateCollectionTaskRetryInfo, bool) {
	task, ok := s.GetCodexTurnStateCollectionTask(taskID)
	if !ok || task == nil || !task.CanRetry {
		return nil, false
	}
	return &CodexTurnStateCollectionTaskRetryInfo{
		TaskID:       task.ID,
		AccountID:    task.AccountID,
		AccountName:  task.AccountName,
		RequestModel: task.RequestModel,
		OwnerModel:   task.OwnerModel,
		Source:       task.Source,
	}, true
}

// RetryCodexTurnStateCollectionTask reserves a traceable retry task and submits
// it through the same administrator collection entry point. The manual
// scheduler binds the task ID from context to the matching owner-model worker.
func (s *OpenAIGatewayService) RetryCodexTurnStateCollectionTask(ctx context.Context, taskID string) (*CodexTurnStateCollectionTask, error) {
	retry, ok := s.CodexTurnStateCollectionTaskRetryInfo(taskID)
	if !ok {
		if _, exists := s.GetCodexTurnStateCollectionTask(taskID); !exists {
			return nil, infraerrors.New(http.StatusNotFound, "CODEX_TURN_STATE_TASK_NOT_FOUND", "Codex Turn State task not found")
		}
		return nil, infraerrors.New(http.StatusConflict, "CODEX_TURN_STATE_TASK_NOT_RETRYABLE", "Codex Turn State task cannot be retried")
	}
	if s == nil || s.accountRepo == nil {
		return nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_COLLECTION_UNAVAILABLE", "Codex Turn State collection service is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// A retry is an asynchronous administrator action. Detach it from the HTTP
	// request deadline; the registry-owned cancel function remains authoritative.
	ctx = WithCodexTurnStateCollectionSource(context.WithoutCancel(ctx), CodexTurnStateCollectionSourceRetry)
	reserved, taskCtx, err := s.CreateCodexTurnStateCollectionTask(ctx, CodexTurnStateCollectionTaskInput{
		AccountID:    retry.AccountID,
		AccountName:  retry.AccountName,
		RequestModel: retry.RequestModel,
		OwnerModel:   retry.OwnerModel,
		Source:       CodexTurnStateCollectionSourceRetry,
		RetryOf:      retry.TaskID,
	})
	if err != nil {
		return nil, err
	}
	account, err := s.accountRepo.GetByID(taskCtx, retry.AccountID)
	if err != nil || account == nil {
		s.FailCodexTurnStateCollectionTask(reserved.ID, "account_unavailable")
		return nil, infraerrors.New(http.StatusNotFound, "ACCOUNT_NOT_FOUND", "account not found")
	}
	result, err := s.RequestCodexTurnStateCollection(taskCtx, account, retry.RequestModel)
	if err != nil {
		s.FailCodexTurnStateCollectionTask(reserved.ID, "request_failed")
		return nil, err
	}
	if result == nil {
		s.FailCodexTurnStateCollectionTask(reserved.ID, "request_failed")
		return nil, infraerrors.New(http.StatusServiceUnavailable, "CODEX_TURN_STATE_COLLECTION_UNAVAILABLE", "Codex Turn State collection service is unavailable")
	}
	switch result.Status {
	case CodexTurnStateManualStatusQueued:
		if !containsCodexTurnStateCollectionOwner(result.QueuedModels, retry.OwnerModel) {
			s.FailCodexTurnStateCollectionTask(reserved.ID, "model_scope_changed")
			return nil, infraerrors.New(http.StatusConflict, "CODEX_TURN_STATE_TASK_RETRY_REJECTED", "Codex Turn State task retry was rejected")
		}
	case CodexTurnStateManualStatusAlreadyValid:
		s.CompleteCodexTurnStateCollectionTask(reserved.ID)
	default:
		s.FailCodexTurnStateCollectionTask(reserved.ID, result.Reason)
		return nil, infraerrors.New(http.StatusConflict, "CODEX_TURN_STATE_TASK_RETRY_REJECTED", "Codex Turn State task retry was rejected")
	}
	latest, _ := s.GetCodexTurnStateCollectionTask(reserved.ID)
	return latest, nil
}

func containsCodexTurnStateCollectionOwner(owners []string, owner string) bool {
	owner = strings.TrimSpace(owner)
	for _, candidate := range owners {
		if strings.EqualFold(strings.TrimSpace(candidate), owner) {
			return true
		}
	}
	return false
}

func boundedCodexTurnStateCollectionLabel(value string) string {
	const maxRunes = 128
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}
