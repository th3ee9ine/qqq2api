package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type auditServiceRepoStub struct {
	mu sync.Mutex

	logs []*AuditLog

	countOverride *int64
	countErr      error
	truncateErr   error
	insertErr     error
	batchErr      error

	batchStarted chan struct{}
	batchRelease chan struct{}
	batchOnce    sync.Once
}

func (r *auditServiceRepoStub) BatchInsert(ctx context.Context, logs []*AuditLog) (int64, error) {
	if r.batchStarted != nil {
		r.batchOnce.Do(func() { close(r.batchStarted) })
	}
	if r.batchRelease != nil {
		select {
		case <-r.batchRelease:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	if r.batchErr != nil {
		return 0, r.batchErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, logs...)
	return int64(len(logs)), nil
}

func (r *auditServiceRepoStub) Insert(_ context.Context, log *AuditLog) error {
	if r.insertErr != nil {
		return r.insertErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, log)
	return nil
}

func (r *auditServiceRepoStub) List(context.Context, *AuditLogFilter) (*AuditLogList, error) {
	return &AuditLogList{}, nil
}

func (r *auditServiceRepoStub) GetByID(context.Context, int64) (*AuditLog, error) {
	return nil, ErrAuditLogNotFound
}

func (r *auditServiceRepoStub) Count(context.Context) (int64, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	if r.countOverride != nil {
		return *r.countOverride, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.logs)), nil
}

func (r *auditServiceRepoStub) TruncateAll(context.Context) error {
	if r.truncateErr != nil {
		return r.truncateErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = nil
	return nil
}

func (r *auditServiceRepoStub) DeleteBefore(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func (r *auditServiceRepoStub) snapshotLogs() []*AuditLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*AuditLog(nil), r.logs...)
}

var _ AuditLogRepository = (*auditServiceRepoStub)(nil)

func TestMaskAuditCredential(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"short", "abc", "****"},
		{"boundary_14", "12345678901234", "****"},
		{"long", "sk-ant-api03-abcdefghijklmnop1234", "sk-ant****1234"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MaskAuditCredential(tc.in)
			if got != tc.want {
				t.Fatalf("MaskAuditCredential(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// 掩码结果绝不能包含原始凭证的中间部分。
			if len(tc.in) > 14 && strings.Contains(got, tc.in) {
				t.Fatalf("masked value leaks full credential: %q", got)
			}
		})
	}
}

func TestRedactAuditBody_JSONRedactsSecrets(t *testing.T) {
	raw := []byte(`{
		"name": "acc1",
		"base_url": "https://evil.example.com",
		"credentials": {"api_key": "sk-secret-123", "base_url": "https://evil.example.com"},
		"new_password": "hunter2",
		"totp_code": "123456",
		"nested": [{"access_token": "tok_abc"}]
	}`)
	out := RedactAuditBody(raw, "application/json")

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	// 敏感字段被擦除。
	for _, secret := range []string{"sk-secret-123", "hunter2", "123456", "tok_abc"} {
		if strings.Contains(out, secret) {
			t.Fatalf("redacted body still contains secret %q: %s", secret, out)
		}
	}
	// 非敏感字段（base_url、name）保留以便追责。
	if !strings.Contains(out, "evil.example.com") {
		t.Fatalf("base_url should be preserved for accountability: %s", out)
	}
	if !strings.Contains(out, "acc1") {
		t.Fatalf("name should be preserved: %s", out)
	}
}

// 裸键 "session"（Ollama Cloud 会话保存的请求体字段）值整体就是浏览器 Cookie 明文，
// 必须命中键级脱敏；session_id 等运行态标识不受影响，保留以便追责。
func TestRedactAuditBody_BareSessionKeyRedacted(t *testing.T) {
	raw := []byte(`{"session": "wos-session=cookie-canary", "session_id": "sid-visible"}`)
	out := RedactAuditBody(raw, "application/json")

	if strings.Contains(out, "cookie-canary") {
		t.Fatalf("redacted body still contains the session cookie: %s", out)
	}
	if !strings.Contains(out, "sid-visible") {
		t.Fatalf("session_id should be preserved for accountability: %s", out)
	}
}

// TestRedactAuditBody_AuthoritativeTablesSynced 覆盖曾经漏网的凭证字段：
// 账号 credentials 敏感子键、支付渠道无分隔符密钥、字符串值内嵌凭证的 proxy_key / custom_key，
// 以及 camelCase 等命名变体（归一化比对）。
func TestRedactAuditBody_AuthoritativeTablesSynced(t *testing.T) {
	raw := []byte(`{
		"credentials": {
			"session_key": "sk-session-aaa",
			"service_account_json": "{\"private_key\":\"pem-body-bbb\"}",
			"service_account": "sa-blob-ccc"
		},
		"proxy_key": "socks5|1.2.3.4|1080|proxyuser|proxypass-ddd",
		"custom_key": "sk-custom-eee",
		"config": {
			"pkey": "easypay-merchant-fff",
			"privateKey": "alipay-pem-ggg",
			"apiv3key": "wxpay-v3-hhh",
			"SecretKey": "stripe-sk-iii",
			"webhookSecret": "whsec-jjj"
		},
		"provider_key": "stripe",
		"name": "instance-1"
	}`)
	out := RedactAuditBody(raw, "application/json")

	for _, secret := range []string{
		"sk-session-aaa", "pem-body-bbb", "sa-blob-ccc",
		"proxypass-ddd", "sk-custom-eee",
		"easypay-merchant-fff", "alipay-pem-ggg", "wxpay-v3-hhh",
		"stripe-sk-iii", "whsec-jjj",
	} {
		if strings.Contains(out, secret) {
			t.Fatalf("redacted body still contains secret %q: %s", secret, out)
		}
	}
	// provider_key 是渠道标识而非密钥，必须保留以便追责。
	if !strings.Contains(out, `"provider_key":"stripe"`) {
		t.Fatalf("provider_key should be preserved for accountability: %s", out)
	}
	if !strings.Contains(out, "instance-1") {
		t.Fatalf("name should be preserved: %s", out)
	}
}

// SensitiveCredentialKeys 中的每个键都必须被审计脱敏判定命中（防两表漂移的守卫）。
func TestAuditSensitiveKeys_CoverCredentialTable(t *testing.T) {
	for _, k := range SensitiveCredentialKeys {
		if !isAuditSensitiveBodyKey(k) {
			t.Fatalf("credential key %q is not covered by audit redaction", k)
		}
	}
	for provider, fields := range providerSensitiveConfigFields {
		for k := range fields {
			if !isAuditSensitiveBodyKey(k) {
				t.Fatalf("payment provider %q sensitive field %q is not covered by audit redaction", provider, k)
			}
		}
	}
}

func TestRedactAuditBody_NonJSONOmitted(t *testing.T) {
	out := RedactAuditBody([]byte("username=admin&password=secret"), "application/x-www-form-urlencoded")
	if strings.Contains(out, "secret") {
		t.Fatalf("non-json body must not leak content: %s", out)
	}
	if !strings.Contains(out, "omitted") {
		t.Fatalf("expected omission marker, got: %s", out)
	}
}

func TestRedactAuditBody_Empty(t *testing.T) {
	if got := RedactAuditBody(nil, "application/json"); got != "" {
		t.Fatalf("expected empty for nil body, got %q", got)
	}
}

func TestSessionBindingHash(t *testing.T) {
	a := &SessionBinding{IP: "1.2.3.4", UserAgent: "Mozilla/5.0"}
	b := &SessionBinding{IP: "1.2.3.4", UserAgent: "Mozilla/5.0"}
	if a.Hash() != b.Hash() {
		t.Fatalf("identical bindings must hash equal")
	}
	if a.Hash() == "" {
		t.Fatalf("non-empty binding must produce non-empty hash")
	}

	// IP 变化 → 哈希变化。
	c := &SessionBinding{IP: "5.6.7.8", UserAgent: "Mozilla/5.0"}
	if a.Hash() == c.Hash() {
		t.Fatalf("changing IP must change hash")
	}
	// UA 变化 → 哈希变化。
	d := &SessionBinding{IP: "1.2.3.4", UserAgent: "curl/8.0"}
	if a.Hash() == d.Hash() {
		t.Fatalf("changing UA must change hash")
	}

	// 空指纹 → 空哈希（旧 token 兼容）。
	empty := &SessionBinding{}
	if empty.Hash() != "" {
		t.Fatalf("empty binding must hash to empty string")
	}
	var nilBinding *SessionBinding
	if nilBinding.Hash() != "" {
		t.Fatalf("nil binding must hash to empty string")
	}
}

func TestParseAuditLogRetentionDays(t *testing.T) {
	cases := map[string]int{
		"":       defaultAuditLogRetentionDays,
		"abc":    defaultAuditLogRetentionDays,
		"90":     90,
		"0":      0,
		"-1":     0,
		"  30  ": 30,
	}
	for in, want := range cases {
		if got := parseAuditLogRetentionDays(in); got != want {
			t.Fatalf("parseAuditLogRetentionDays(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestAuditLogServiceClearAllDropsQueuedPreClearEntries(t *testing.T) {
	repo := &auditServiceRepoStub{}
	svc := NewAuditLogService(repo, nil)

	svc.Record(&AuditLog{Action: "before-clear"})
	deleted, err := svc.ClearAll(context.Background(), &AuditLog{})
	require.NoError(t, err)
	require.Zero(t, deleted)

	// Start after clearing so the test exercises the queue drain performed by
	// ClearAll rather than relying on a running writer to consume the item.
	svc.Start()
	svc.Stop()

	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, AuditActionAuditLogClear, logs[0].Action)
}

func TestAuditLogServiceClearAllAllowsOnlyPostClearEntries(t *testing.T) {
	repo := &auditServiceRepoStub{}
	svc := NewAuditLogService(repo, nil)

	svc.Record(&AuditLog{Action: "before-clear"})
	deleted, err := svc.ClearAll(context.Background(), &AuditLog{})
	require.NoError(t, err)
	require.Zero(t, deleted)

	svc.Start()
	svc.Record(&AuditLog{Action: "after-clear"})
	svc.Stop()

	logs := repo.snapshotLogs()
	require.Len(t, logs, 2)
	require.Equal(t, AuditActionAuditLogClear, logs[0].Action)
	require.Equal(t, "after-clear", logs[1].Action)
}

func TestAuditLogServiceClearAllFailureLeavesQueueIntact(t *testing.T) {
	repo := &auditServiceRepoStub{countErr: errors.New("count failed")}
	svc := NewAuditLogService(repo, nil)
	svc.Record(&AuditLog{Action: "before-clear"})

	_, err := svc.ClearAll(context.Background(), &AuditLog{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "count audit logs")

	svc.Start()
	svc.Stop()
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, "before-clear", logs[0].Action)
}

func TestAuditLogServiceClearAllSerializesWithInFlightBatch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	repo := &auditServiceRepoStub{
		batchStarted: started,
		batchRelease: release,
	}
	svc := NewAuditLogService(repo, nil)
	svc.Start()
	defer svc.Stop()

	// Fill a complete batch so the writer hands it to the repository
	// immediately, where the stub deliberately blocks.
	for i := 0; i < auditLogBatchSize; i++ {
		svc.Record(&AuditLog{Action: "before-clear"})
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("audit writer did not start the batch")
	}

	clearDone := make(chan struct{})
	var deleted int64
	var clearErr error
	go func() {
		deleted, clearErr = svc.ClearAll(context.Background(), &AuditLog{})
		close(clearDone)
	}()

	// ClearAll must wait for the in-flight repository write instead of
	// truncating concurrently with it.
	select {
	case <-clearDone:
		t.Fatal("ClearAll returned while a batch write was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)

	select {
	case <-clearDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ClearAll did not finish after releasing the batch")
	}
	require.NoError(t, clearErr)
	require.Equal(t, int64(auditLogBatchSize), deleted)

	// Stop flushes only the clear trace; the pre-clear batch was removed by
	// TruncateAll while the write lock was held.
	svc.Stop()
	logs := repo.snapshotLogs()
	require.Len(t, logs, 1)
	require.Equal(t, AuditActionAuditLogClear, logs[0].Action)
}
