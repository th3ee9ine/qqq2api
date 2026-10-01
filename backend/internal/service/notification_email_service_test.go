package service

import (
	"bufio"
	"context"
	"errors"
	"io"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNotificationEmailPreviewEscapesHTMLAndSanitizesSubject(t *testing.T) {
	ctx := context.Background()
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)

	preview, err := previewNotificationTemplate(svc, ctx, notificationEmailTestPreviewInput{
		Event:   NotificationEmailEventAccountQuotaAlert,
		Locale:  "en-US,en;q=0.9",
		Subject: "Quota alert for {{recipient_name}}\r\nInjected",
		HTML:    `<p>{{recipient_name}}</p>`,
		Variables: map[string]string{
			"recipient_name": `<script>alert("x")</script>`,
		},
	})
	require.NoError(t, err)
	require.NotContains(t, preview.Subject, "\r")
	require.NotContains(t, preview.Subject, "\n")
	require.Contains(t, preview.Subject, `Quota alert for <script>alert("x")</script>Injected`)
	require.Contains(t, preview.HTML, `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`)
}

func TestNotificationEmailURLSafety(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "data:text/html,<script>alert(1)</script>", "file:///etc/passwd"} {
		require.False(t, isSafeNotificationEmailURL(raw), raw)
	}
	for _, raw := range []string{"https://example.com/ops", "http://example.com/ops", "mailto:ops@example.com", "/ops"} {
		require.True(t, isSafeNotificationEmailURL(raw), raw)
	}
}

func TestNotificationEmailAdditionalEventsAreListedAndPreviewable(t *testing.T) {
	ctx := context.Background()
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)

	infos := notificationEmailEventDefinitions
	events := make(map[string]NotificationEmailEventInfo, len(infos))
	for _, info := range infos {
		events[info.Event] = info
	}

	checks := []struct {
		event       string
		placeholder string
	}{
		{NotificationEmailEventAccountQuotaAlert, "account_name"},
		{NotificationEmailEventContentModerationViolation, "moderation_category"},
		{NotificationEmailEventContentModerationDisabled, "violation_count"},
		{NotificationEmailEventCyberPolicyNotice, "upstream_message"},
		{NotificationEmailEventOpsAlert, "rule_name"},
		{NotificationEmailEventOpsScheduledReport, "report_html"},
	}

	for _, check := range checks {
		info, ok := events[check.event]
		require.Truef(t, ok, "expected %s to be listed", check.event)
		require.False(t, info.Optional)
		require.Contains(t, info.Placeholders, check.placeholder)

		preview, err := previewNotificationTemplate(svc, ctx, notificationEmailTestPreviewInput{Event: check.event, Locale: "zh"})
		require.NoError(t, err)
		require.NotEmpty(t, preview.Subject)
		require.NotEmpty(t, preview.HTML)
	}
}

func TestCyberPolicyNoticeTemplateWrapsLongUpstreamMessages(t *testing.T) {
	ctx := context.Background()
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)
	longMessage := strings.Repeat("0123456789abcdef", 256)

	for _, locale := range []string{"en", "zh"} {
		preview, err := previewNotificationTemplate(svc, ctx, notificationEmailTestPreviewInput{
			Event:  NotificationEmailEventCyberPolicyNotice,
			Locale: locale,
			Variables: map[string]string{
				"upstream_message": longMessage,
			},
		})
		require.NoError(t, err)
		require.Contains(t, preview.HTML, "table-layout:fixed")
		require.Contains(t, preview.HTML, "overflow-wrap:anywhere")
		require.Contains(t, preview.HTML, "word-break:break-all")
		require.NotContains(t, preview.HTML, "{{upstream_message}}")
	}
}

func TestOpsScheduledReportTemplateExposesEditableSummaryMetrics(t *testing.T) {
	ctx := context.Background()
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)

	requiredPlaceholders := []string{
		"report_summary_display",
		"report_detail_display",
		"report_total_requests",
		"report_success_count",
		"report_sla_error_count",
		"report_business_limited_count",
		"report_sla",
		"report_error_rate",
		"report_upstream_error_rate",
		"report_upstream_error_count_excl_429_529",
		"report_upstream_429_count",
		"report_upstream_529_count",
		"report_latency_p50",
		"report_latency_p99",
		"report_ttft_p50",
		"report_ttft_p99",
		"report_tokens",
		"report_qps_current",
		"report_qps_peak",
		"report_qps_avg",
		"report_tps_current",
		"report_tps_peak",
		"report_tps_avg",
	}

	for _, locale := range []string{"en", "zh"} {
		tmpl, err := svc.GetTemplate(ctx, NotificationEmailEventOpsScheduledReport, locale)
		require.NoError(t, err)
		for _, placeholder := range requiredPlaceholders {
			require.Contains(t, tmpl.Placeholders, placeholder)
			require.Contains(t, tmpl.HTML, "{{"+placeholder+"}}")
		}

		preview, err := previewNotificationTemplate(svc, ctx, notificationEmailTestPreviewInput{
			Event:  NotificationEmailEventOpsScheduledReport,
			Locale: locale,
		})
		require.NoError(t, err)
		require.Contains(t, preview.HTML, "2,374")
		require.Contains(t, preview.HTML, "99.86%")
		require.Contains(t, preview.HTML, "151,260 ms")
		require.Contains(t, preview.HTML, `style="display: none;"`)
		require.NotContains(t, preview.HTML, "{{report_total_requests}}")
	}
}

func TestOpsScheduledReportRuntimeVariablesDoNotLeakPreviewSamples(t *testing.T) {
	ctx := context.Background()
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)

	variables := svc.runtimeVariables(ctx, NotificationEmailEventOpsScheduledReport, "en", NotificationEmailSendInput{})
	require.Equal(t, "none", variables["report_summary_display"])
	require.Equal(t, "block", variables["report_detail_display"])
	require.Empty(t, variables["report_html"])
	for _, placeholder := range notificationEmailOpsSummaryPlaceholders {
		if placeholder == "report_summary_display" {
			continue
		}
		require.Equal(t, "-", variables[placeholder])
	}

	rendered, err := renderNotificationEmail(
		NotificationEmailEventOpsScheduledReport,
		"Report",
		`<div style="display: {{report_detail_display}};">{{report_html}}</div>`,
		variables,
		nil,
	)
	require.NoError(t, err)
	require.NotContains(t, rendered.HTML, "<h2>Daily summary</h2>")
}

func TestNotificationEmailRawHTMLVariablesAreTrustedOnlyForHTMLPlaceholders(t *testing.T) {
	require.True(t, notificationEmailRawHTMLAllowed(NotificationEmailEventOpsScheduledReport, "report_html"))
	require.False(t, notificationEmailRawHTMLAllowed(NotificationEmailEventOpsScheduledReport, "recipient_name"))
	require.False(t, notificationEmailRawHTMLAllowed(NotificationEmailEventOpsAlert, "report_html"))

	preview, err := renderNotificationEmail(
		NotificationEmailEventOpsScheduledReport,
		"Report for {{recipient_name}}",
		`<section>{{report_html}}</section><p>{{recipient_name}}</p>`,
		map[string]string{
			"recipient_name": `<script>alert("x")</script>`,
			"report_html":    `<p>escaped report</p>`,
		},
		map[string]string{
			"report_html": `<table><tr><td>trusted report</td></tr></table>`,
		},
	)
	require.NoError(t, err)
	require.Contains(t, preview.HTML, `<table><tr><td>trusted report</td></tr></table>`)
	require.NotContains(t, preview.HTML, `escaped report`)
	require.Contains(t, preview.HTML, `&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`)
	require.Contains(t, preview.Subject, `<script>alert("x")</script>`)

	preview, err = renderNotificationEmail(
		NotificationEmailEventOpsScheduledReport,
		"Recipient {{recipient_name}}",
		`<p>{{recipient_name}}</p>`,
		map[string]string{"recipient_name": `<em>escaped</em>`},
		map[string]string{"recipient_name": `<strong>raw</strong>`},
	)
	require.NoError(t, err)
	require.Contains(t, preview.HTML, `&lt;em&gt;escaped&lt;/em&gt;`)
	require.NotContains(t, preview.HTML, `<strong>raw</strong>`)
}

func TestNotificationEmailFallbackClassification(t *testing.T) {
	templateErr := notificationEmailTemplateErr(errors.New("bad template"))
	configErr := notificationEmailConfigErr(errors.New("missing email service"))
	deliveryErr := notificationEmailDeliveryErr(errors.New("smtp timeout"))

	require.True(t, shouldFallbackNotificationEmail(templateErr))
	require.True(t, shouldFallbackNotificationEmail(configErr))
	require.False(t, shouldFallbackNotificationEmail(deliveryErr))
	require.True(t, isNotificationEmailDeliveryError(deliveryErr))
	require.False(t, isNotificationEmailDeliveryError(templateErr))
	require.False(t, shouldFallbackNotificationEmail(nil))
}

func TestOpsScheduledReportDeliverySourceIDIncludesReportIdentity(t *testing.T) {
	report := &opsScheduledReport{Name: "日报", ReportType: "daily_summary", Schedule: "0 9 * * *"}
	sourceID := opsScheduledReportDeliverySourceID(report)
	require.Contains(t, sourceID, "daily_summary")
	require.Contains(t, sourceID, "日报")
	require.Contains(t, sourceID, "0 9 * * *")
	require.NotEqual(t, sourceID, opsScheduledReportDeliverySourceID(&opsScheduledReport{Name: "周报", ReportType: "weekly_summary", Schedule: "0 9 * * 1"}))
	require.Equal(t, "scheduled_report", opsScheduledReportDeliverySourceID(nil))
}

func TestNotificationEmailUnsubscribeRejectsOperationalAndRetiredEvents(t *testing.T) {
	ctx := context.Background()
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)

	for event := range notificationEmailEventDefinitions {
		t.Run(event, func(t *testing.T) {
			token, err := svc.createUnsubscribeToken(ctx, "user@example.com", event)
			require.NoError(t, err)
			_, err = svc.Unsubscribe(ctx, token)
			require.ErrorContains(t, err, "transactional")
		})
	}
	token, err := svc.createUnsubscribeToken(ctx, "user@example.com", "balance.low")
	require.NoError(t, err)
	_, err = svc.Unsubscribe(ctx, token)
	require.ErrorContains(t, err, "unsupported email template event")
	_, err = svc.GetTemplate(ctx, "balance.low", "en")
	require.ErrorContains(t, err, "unsupported email template event")
	err = svc.Send(ctx, NotificationEmailSendInput{Event: "balance.low", RecipientEmail: "user@example.com"})
	require.ErrorContains(t, err, "unsupported email template event")
}

func TestNotificationEmailLocaleMemoryNormalizesAcceptLanguage(t *testing.T) {
	ctx := context.Background()
	svc := NewNotificationEmailService(newNotificationEmailMemorySettingRepo(), nil)

	svc.RememberRecipientLocale(ctx, 42, "User@Example.com", "zh-CN,zh;q=0.9,en;q=0.8")
	require.Equal(t, "zh", svc.ResolveRecipientLocale(ctx, 42, "user@example.com"))
	require.Equal(t, "zh", svc.ResolveRecipientLocale(ctx, 0, "user@example.com"))
}

func TestNotificationEmailDeliveryKeyUsesShortStableHash(t *testing.T) {
	key := notificationEmailDeliveryKey(
		NotificationEmailEventAccountQuotaAlert,
		"account_quota",
		"1234567890",
		"User@Example.com",
		"7d",
	)
	require.NotEmpty(t, key)
	require.LessOrEqual(t, len(key), 100)
	require.True(t, strings.HasPrefix(key, notificationEmailDeliveryKeyPrefix+"v2:"))
	require.Equal(t, key, notificationEmailDeliveryKey(
		NotificationEmailEventAccountQuotaAlert,
		"account_quota",
		"1234567890",
		"user@example.com",
		"7d",
	))
	require.NotEqual(t, key, notificationEmailDeliveryKey(
		NotificationEmailEventAccountQuotaAlert,
		"account_quota",
		"1234567890",
		"user@example.com",
		"3d",
	))

	legacyKey := legacyNotificationEmailDeliveryKey(
		NotificationEmailEventAccountQuotaAlert,
		"account_quota",
		"1234567890",
		"user@example.com",
		"7d",
	)
	require.NotEqual(t, key, legacyKey)
}

func TestNotificationEmailSendDeduplicatesQuotaNotification(t *testing.T) {
	ctx := context.Background()
	repo := newNotificationEmailMemorySettingRepo()
	smtpServer := startNotificationEmailTestSMTPServer(t)
	require.NoError(t, repo.SetMultiple(ctx, smtpServer.settings()))

	emailSvc := NewEmailService(repo)
	svc := NewNotificationEmailService(repo, emailSvc)
	input := NotificationEmailSendInput{
		Event:          NotificationEmailEventAccountQuotaAlert,
		RecipientEmail: "User@Example.com",
		RecipientName:  "User",
		UserID:         42,
		SourceType:     "account_quota",
		SourceID:       "1234567890",
		ReminderKey:    "7d",
		Variables:      map[string]string{"quota_remaining": "1.00", "quota_threshold": "10.00", "account_name": "openai-main"},
	}

	require.NoError(t, svc.Send(ctx, input))
	require.Equal(t, int64(1), smtpServer.messageCount())

	key := notificationEmailDeliveryKey(input.Event, input.SourceType, input.SourceID, input.RecipientEmail, input.ReminderKey)
	require.LessOrEqual(t, len(key), 100)
	_, err := repo.GetValue(ctx, key)
	require.NoError(t, err)

	require.NoError(t, svc.Send(ctx, input))
	require.Equal(t, int64(1), smtpServer.messageCount())
}

func TestNotificationEmailSendRespectsLegacyDeliveryKey(t *testing.T) {
	ctx := context.Background()
	repo := newNotificationEmailMemorySettingRepo()
	svc := NewNotificationEmailService(repo, nil)
	input := NotificationEmailSendInput{
		Event:          NotificationEmailEventAccountQuotaAlert,
		RecipientEmail: "user@example.com",
		SourceType:     "account_quota",
		SourceID:       "1234567890",
		ReminderKey:    "7d",
	}
	legacyKey := legacyNotificationEmailDeliveryKey(input.Event, input.SourceType, input.SourceID, input.RecipientEmail, input.ReminderKey)
	require.NoError(t, repo.Set(ctx, legacyKey, "sent"))

	require.NoError(t, svc.Send(ctx, input))
}

type notificationEmailMemorySettingRepo struct {
	mu     sync.RWMutex
	values map[string]string
}

func newNotificationEmailMemorySettingRepo() *notificationEmailMemorySettingRepo {
	return &notificationEmailMemorySettingRepo{values: make(map[string]string)}
}

func (r *notificationEmailMemorySettingRepo) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value}, nil
}

func (r *notificationEmailMemorySettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *notificationEmailMemorySettingRepo) Set(_ context.Context, key, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[key] = value
	return nil
}

func (r *notificationEmailMemorySettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}

func (r *notificationEmailMemorySettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, value := range settings {
		r.values[key] = value
	}
	return nil
}

func (r *notificationEmailMemorySettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.values))
	for key, value := range r.values {
		out[key] = value
	}
	return out, nil
}

func (r *notificationEmailMemorySettingRepo) Delete(_ context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.values[key]; !ok {
		return ErrSettingNotFound
	}
	delete(r.values, key)
	return nil
}

func TestNotificationEmailMemorySettingRepoSatisfiesInterface(t *testing.T) {
	var _ SettingRepository = (*notificationEmailMemorySettingRepo)(nil)
	require.False(t, strings.Contains(notificationEmailPreferenceKey(NotificationEmailEventAccountQuotaAlert, "User@Example.com"), "User@Example.com"))
}

type notificationEmailTestSMTPServer struct {
	listener      net.Listener
	wg            sync.WaitGroup
	messages      atomic.Int64
	messageMu     sync.Mutex
	messageBodies []string
}

func startNotificationEmailTestSMTPServer(t *testing.T) *notificationEmailTestSMTPServer {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	server := &notificationEmailTestSMTPServer{listener: listener}
	server.wg.Add(1)
	go server.serve()
	t.Cleanup(server.close)
	return server
}

func (s *notificationEmailTestSMTPServer) settings() map[string]string {
	host, port, _ := net.SplitHostPort(s.listener.Addr().String())
	return map[string]string{
		SettingKeySMTPHost:     host,
		SettingKeySMTPPort:     port,
		SettingKeySMTPUsername: "user",
		SettingKeySMTPPassword: "password",
		SettingKeySMTPFrom:     "noreply@example.com",
		SettingKeySMTPFromName: "Sub2API",
		SettingKeySMTPUseTLS:   "false",
	}
}

func (s *notificationEmailTestSMTPServer) messageCount() int64 {
	return s.messages.Load()
}

func (s *notificationEmailTestSMTPServer) lastMessage() string {
	s.messageMu.Lock()
	defer s.messageMu.Unlock()
	if len(s.messageBodies) == 0 {
		return ""
	}
	return s.messageBodies[len(s.messageBodies)-1]
}

func (s *notificationEmailTestSMTPServer) lastMessageBody(t *testing.T) string {
	t.Helper()

	message, err := mail.ReadMessage(strings.NewReader(s.lastMessage()))
	require.NoError(t, err)

	bodyReader := io.Reader(message.Body)
	if strings.EqualFold(message.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
		bodyReader = quotedprintable.NewReader(message.Body)
	}
	body, err := io.ReadAll(bodyReader)
	require.NoError(t, err)
	return string(body)
}

func (s *notificationEmailTestSMTPServer) close() {
	_ = s.listener.Close()
	s.wg.Wait()
}

func (s *notificationEmailTestSMTPServer) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.handleConn(conn)
	}
}

func (s *notificationEmailTestSMTPServer) handleConn(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	rw := bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn))
	writeLine := func(line string) bool {
		if _, err := rw.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return rw.Flush() == nil
	}
	if !writeLine("220 localhost ESMTP") {
		return
	}
	for {
		line, err := rw.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.ToUpper(strings.TrimRight(line, "\r\n"))
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			if _, err := rw.WriteString("250-localhost\r\n250 AUTH PLAIN\r\n"); err != nil {
				return
			}
			if err := rw.Flush(); err != nil {
				return
			}
		case strings.HasPrefix(cmd, "AUTH"):
			if !writeLine("235 2.7.0 Authentication successful") {
				return
			}
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			if !writeLine("250 2.1.0 OK") {
				return
			}
		case strings.HasPrefix(cmd, "RCPT TO:"):
			if !writeLine("250 2.1.5 OK") {
				return
			}
		case strings.HasPrefix(cmd, "DATA"):
			if !writeLine("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
			var message strings.Builder
			for {
				dataLine, err := rw.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				_, _ = message.WriteString(dataLine)
			}
			s.messageMu.Lock()
			s.messageBodies = append(s.messageBodies, message.String())
			s.messageMu.Unlock()
			s.messages.Add(1)
			if !writeLine("250 2.0.0 OK") {
				return
			}
		case strings.HasPrefix(cmd, "QUIT"):
			_ = writeLine("221 2.0.0 Bye")
			return
		default:
			if !writeLine("250 OK") {
				return
			}
		}
	}
}
