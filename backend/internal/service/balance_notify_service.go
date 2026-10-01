package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const (
	emailSendTimeout = 30 * time.Second

	// Threshold type values
	thresholdTypeFixed      = "fixed"
	thresholdTypePercentage = "percentage"

	// Quota dimension labels
	quotaDimDaily  = "daily"
	quotaDimWeekly = "weekly"
	quotaDimTotal  = "total"

	defaultSiteName = "QQQ2API"
)

// quotaDimLabels maps dimension names to display labels.
var quotaDimLabels = map[string]string{
	quotaDimDaily:  "日限额 / Daily",
	quotaDimWeekly: "周限额 / Weekly",
	quotaDimTotal:  "总限额 / Total",
}

// AccountQuotaReader provides read access to account quota data.
type AccountQuotaReader interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
}

// BalanceNotifyService handles upstream account quota threshold notifications.
type BalanceNotifyService struct {
	emailService             *EmailService
	settingRepo              SettingRepository
	accountRepo              AccountQuotaReader
	notificationEmailService *NotificationEmailService
}

// NewBalanceNotifyService creates a new BalanceNotifyService.
func NewBalanceNotifyService(emailService *EmailService, settingRepo SettingRepository, accountRepo AccountQuotaReader) *BalanceNotifyService {
	return &BalanceNotifyService{
		emailService: emailService,
		settingRepo:  settingRepo,
		accountRepo:  accountRepo,
	}
}

func (s *BalanceNotifyService) SetNotificationEmailService(notificationEmailService *NotificationEmailService) {
	s.notificationEmailService = notificationEmailService
}

// quotaDim describes one quota dimension for notification checking.
type quotaDim struct {
	name          string
	enabled       bool
	threshold     float64
	thresholdType string // "fixed" (default) or "percentage"
	currentUsed   float64
	limit         float64
}

// resolvedThreshold converts the user-facing "remaining" threshold into a usage-based trigger point.
// The threshold represents how much quota REMAINS when the alert fires:
//   - Fixed ($): threshold=400, limit=1000 → fires when usage reaches 600 (remaining drops to 400)
//   - Percentage (%): threshold=30, limit=1000 → fires when usage reaches 700 (remaining drops to 30%)
func (d quotaDim) resolvedThreshold() float64 {
	if d.limit <= 0 {
		return 0
	}
	if d.thresholdType == thresholdTypePercentage {
		return d.limit * (1 - d.threshold/100)
	}
	return d.limit - d.threshold
}

// buildQuotaDims returns the three quota dimensions for notification checking.
func buildQuotaDims(account *Account) []quotaDim {
	return []quotaDim{
		{quotaDimDaily, account.GetQuotaNotifyDailyEnabled(), account.GetQuotaNotifyDailyThreshold(), account.GetQuotaNotifyDailyThresholdType(), account.GetQuotaDailyUsed(), account.GetQuotaDailyLimit()},
		{quotaDimWeekly, account.GetQuotaNotifyWeeklyEnabled(), account.GetQuotaNotifyWeeklyThreshold(), account.GetQuotaNotifyWeeklyThresholdType(), account.GetQuotaWeeklyUsed(), account.GetQuotaWeeklyLimit()},
		{quotaDimTotal, account.GetQuotaNotifyTotalEnabled(), account.GetQuotaNotifyTotalThreshold(), account.GetQuotaNotifyTotalThresholdType(), account.GetQuotaUsed(), account.GetQuotaLimit()},
	}
}

// buildQuotaDimsFromState builds quota dimensions using DB transaction state instead of account snapshot.
// Notification settings (enabled, threshold, thresholdType) come from the account; usage values from quotaState.
func buildQuotaDimsFromState(account *Account, state *AccountQuotaState) []quotaDim {
	return []quotaDim{
		{quotaDimDaily, account.GetQuotaNotifyDailyEnabled(), account.GetQuotaNotifyDailyThreshold(), account.GetQuotaNotifyDailyThresholdType(), state.DailyUsed, state.DailyLimit},
		{quotaDimWeekly, account.GetQuotaNotifyWeeklyEnabled(), account.GetQuotaNotifyWeeklyThreshold(), account.GetQuotaNotifyWeeklyThresholdType(), state.WeeklyUsed, state.WeeklyLimit},
		{quotaDimTotal, account.GetQuotaNotifyTotalEnabled(), account.GetQuotaNotifyTotalThreshold(), account.GetQuotaNotifyTotalThresholdType(), state.TotalUsed, state.TotalLimit},
	}
}

// CheckAccountQuotaAfterIncrement checks if any quota dimension crossed above its notify threshold.
// When quotaState is non-nil (from DB transaction RETURNING), it is used directly for threshold
// checking, avoiding a separate DB read. Otherwise it falls back to fetching fresh account data.
func (s *BalanceNotifyService) CheckAccountQuotaAfterIncrement(ctx context.Context, account *Account, cost float64, quotaState *AccountQuotaState) {
	if account == nil || s.emailService == nil || s.settingRepo == nil || cost <= 0 {
		return
	}
	if !s.isAccountQuotaNotifyEnabled(ctx) {
		return
	}
	adminEmails := s.getAccountQuotaNotifyEmails(ctx)
	if len(adminEmails) == 0 {
		return
	}

	siteName := s.getSiteName(ctx)
	var dims []quotaDim
	if quotaState != nil {
		dims = buildQuotaDimsFromState(account, quotaState)
	} else {
		freshAccount := s.fetchFreshAccount(ctx, account)
		dims = buildQuotaDims(freshAccount)
		account = freshAccount // use fresh data for alert metadata
	}
	s.checkQuotaDimCrossings(account, dims, cost, adminEmails, siteName)
}

// fetchFreshAccount loads the latest account from DB; falls back to the snapshot on error.
func (s *BalanceNotifyService) fetchFreshAccount(ctx context.Context, snapshot *Account) *Account {
	if s.accountRepo == nil {
		return snapshot
	}
	fresh, err := s.accountRepo.GetByID(ctx, snapshot.ID)
	if err != nil {
		slog.Warn("failed to fetch fresh account for quota notify, using snapshot",
			"account_id", snapshot.ID, "error", err)
		return snapshot
	}
	return fresh
}

// checkQuotaDimCrossings iterates pre-built quota dimensions and sends alerts for threshold crossings.
// Pre-increment value is reconstructed as currentUsed - cost to detect the crossing moment.
func (s *BalanceNotifyService) checkQuotaDimCrossings(account *Account, dims []quotaDim, cost float64, adminEmails []string, siteName string) {
	for _, dim := range dims {
		if !dim.enabled || dim.threshold <= 0 {
			continue
		}
		effectiveThreshold := dim.resolvedThreshold()
		if effectiveThreshold <= 0 {
			continue
		}
		newUsed := dim.currentUsed
		oldUsed := dim.currentUsed - cost
		if oldUsed < effectiveThreshold && newUsed >= effectiveThreshold {
			s.asyncSendQuotaAlert(adminEmails, account.ID, account.Name, account.Platform, dim, newUsed, effectiveThreshold, siteName)
		}
	}
}

// asyncSendQuotaAlert sends quota alert email in a goroutine with panic recovery.
func (s *BalanceNotifyService) asyncSendQuotaAlert(adminEmails []string, accountID int64, accountName, platform string, dim quotaDim, newUsed, effectiveThreshold float64, siteName string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("panic in quota notification", "recover", r)
			}
		}()
		s.sendQuotaAlertEmails(adminEmails, accountID, accountName, platform, dim, newUsed, siteName)
	}()
}

// isAccountQuotaNotifyEnabled checks the global account quota notification toggle.
func (s *BalanceNotifyService) isAccountQuotaNotifyEnabled(ctx context.Context) bool {
	val, err := s.settingRepo.GetValue(ctx, SettingKeyAccountQuotaNotifyEnabled)
	if err != nil {
		return false
	}
	return val == "true"
}

// getAccountQuotaNotifyEmails reads admin notification emails from settings,
// filtering out disabled and unverified entries.
func (s *BalanceNotifyService) getAccountQuotaNotifyEmails(ctx context.Context) []string {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyAccountQuotaNotifyEmails)
	if err != nil || strings.TrimSpace(raw) == "" || raw == "[]" {
		return nil
	}

	entries := ParseNotifyEmails(raw)
	if len(entries) == 0 {
		return nil
	}

	return filterVerifiedEmails(entries)
}

// getSiteName reads site name from settings with fallback.
func (s *BalanceNotifyService) getSiteName(ctx context.Context) string {
	name, err := s.settingRepo.GetValue(ctx, SettingKeySiteName)
	if err != nil || name == "" {
		return defaultSiteName
	}
	return name
}

// filterVerifiedEmails returns deduplicated, non-disabled, verified emails.
func filterVerifiedEmails(entries []NotifyEmailEntry) []string {
	var recipients []string
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.Disabled || !entry.Verified {
			continue
		}
		email := strings.TrimSpace(entry.Email)
		if email == "" {
			continue
		}
		lower := strings.ToLower(email)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		recipients = append(recipients, email)
	}
	return recipients
}

// sendEmails sends an email to all recipients with shared timeout and error logging.
func (s *BalanceNotifyService) sendEmails(recipients []string, subject, body string, logAttrs ...any) {
	if len(recipients) == 0 {
		slog.Warn("sendEmails: no recipients", "subject", subject)
		return
	}
	for _, to := range recipients {
		ctx, cancel := context.WithTimeout(context.Background(), emailSendTimeout)
		if err := s.emailService.SendEmail(ctx, to, subject, body); err != nil {
			attrs := append([]any{"to", to, "error", err}, logAttrs...)
			slog.Error("failed to send notification", attrs...)
		} else {
			slog.Info("notification email sent successfully", "to", to, "subject", subject)
		}
		cancel()
	}
}

// sendQuotaAlertEmails sends quota alert notification to admin emails.
func (s *BalanceNotifyService) sendQuotaAlertEmails(adminEmails []string, accountID int64, accountName, platform string, dim quotaDim, used float64, siteName string) {
	dimLabel := quotaDimLabels[dim.name]
	if dimLabel == "" {
		dimLabel = dim.name
	}

	// Format the remaining-based threshold for display
	thresholdDisplay := fmt.Sprintf("$%.2f", dim.threshold)
	if dim.thresholdType == thresholdTypePercentage {
		thresholdDisplay = fmt.Sprintf("%.0f%%", dim.threshold)
	}
	remaining := dim.limit - used
	if remaining < 0 {
		remaining = 0
	}

	if s.notificationEmailService != nil {
		fallbackRecipients := make([]string, 0, len(adminEmails))
		for _, to := range adminEmails {
			ctx, cancel := context.WithTimeout(context.Background(), emailSendTimeout)
			err := s.notificationEmailService.Send(ctx, NotificationEmailSendInput{
				Event:          NotificationEmailEventAccountQuotaAlert,
				RecipientEmail: to,
				RecipientName:  emailRecipientName(to),
				SourceType:     "account_quota",
				SourceID:       fmt.Sprintf("%d-%s", accountID, dim.name),
				ReminderKey:    time.Now().UTC().Format("2006-01-02"),
				Variables: map[string]string{
					"account_id":      strconv.FormatInt(accountID, 10),
					"account_name":    accountName,
					"platform":        platform,
					"quota_dimension": dimLabel,
					"quota_used":      fmt.Sprintf("%.2f", used),
					"quota_limit":     fmt.Sprintf("%.2f", dim.limit),
					"quota_remaining": fmt.Sprintf("%.2f", remaining),
					"quota_threshold": thresholdDisplay,
				},
			})
			cancel()
			if err != nil {
				if shouldFallbackNotificationEmail(err) {
					slog.Warn("template account quota alert failed; falling back to built-in body", "to", to, "account_id", accountID, "dimension", dim.name, "err", err.Error())
					fallbackRecipients = append(fallbackRecipients, to)
				} else {
					slog.Warn("template account quota alert delivery failed; not sending fallback to avoid duplicates", "to", to, "account_id", accountID, "dimension", dim.name, "err", err.Error())
				}
			}
		}
		if len(fallbackRecipients) == 0 {
			return
		}
		adminEmails = fallbackRecipients
	}

	subject := fmt.Sprintf("[%s] 账号限额告警 / Account Quota Alert - %s", sanitizeEmailHeader(siteName), sanitizeEmailHeader(accountName))
	body := s.buildQuotaAlertEmailBody(accountID, html.EscapeString(accountName), html.EscapeString(platform), html.EscapeString(dimLabel), used, dim.limit, remaining, thresholdDisplay, html.EscapeString(siteName))
	s.sendEmails(adminEmails, subject, body, "account", accountName, "dimension", dim.name)
}

// sanitizeEmailHeader removes CR/LF characters to prevent SMTP header injection.
func sanitizeEmailHeader(s string) string {
	return strings.NewReplacer("\r", "", "\n", "").Replace(s)
}

// quotaAlertEmailTemplate is the HTML template for account quota alert notifications.
// Format args: siteName, accountID, accountName, platform, dimLabel, used, limitStr, remaining, thresholdDisplay.
const quotaAlertEmailTemplate = `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background-color: #f5f5f5; margin: 0; padding: 20px; }
        .container { max-width: 600px; margin: 0 auto; background-color: #fff; border-radius: 8px; overflow: hidden; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
        .header { background: linear-gradient(135deg, #ef4444 0%%, #dc2626 100%%); color: white; padding: 30px; text-align: center; }
        .header h1 { margin: 0; font-size: 24px; }
        .content { padding: 40px 30px; }
        .metric { display: flex; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid #eee; }
        .metric-label { color: #666; }
        .metric-value { font-weight: bold; color: #333; }
        .info { color: #666; font-size: 14px; line-height: 1.6; margin-top: 20px; text-align: center; }
        .footer { background-color: #f8f9fa; padding: 20px; text-align: center; color: #999; font-size: 12px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header"><h1>%s</h1></div>
        <div class="content">
            <p style="font-size: 18px; color: #333; text-align: center;">账号限额告警 / Account Quota Alert</p>
            <div class="metric"><span class="metric-label">账号 ID / Account ID</span><span class="metric-value">#%d</span></div>
            <div class="metric"><span class="metric-label">账号 / Account</span><span class="metric-value">%s</span></div>
            <div class="metric"><span class="metric-label">平台 / Platform</span><span class="metric-value">%s</span></div>
            <div class="metric"><span class="metric-label">维度 / Dimension</span><span class="metric-value">%s</span></div>
            <div class="metric"><span class="metric-label">已使用 / Used</span><span class="metric-value">$%.2f</span></div>
            <div class="metric"><span class="metric-label">限额 / Limit</span><span class="metric-value">%s</span></div>
            <div class="metric"><span class="metric-label">剩余额度 / Remaining</span><span class="metric-value">$%.2f</span></div>
            <div class="metric"><span class="metric-label">提醒阈值 / Alert Threshold</span><span class="metric-value">%s</span></div>
            <div class="info">
                <p>账号剩余额度已低于提醒阈值，请及时关注。</p>
                <p>Account remaining quota has fallen below the alert threshold.</p>
            </div>
        </div>
        <div class="footer"><p>此邮件由系统自动发送，请勿回复。</p></div>
    </div>
</body>
</html>`

// buildQuotaAlertEmailBody builds HTML email for account quota alert.
func (s *BalanceNotifyService) buildQuotaAlertEmailBody(accountID int64, accountName, platform, dimLabel string, used, limit, remaining float64, thresholdDisplay, siteName string) string {
	limitStr := fmt.Sprintf("$%.2f", limit)
	if limit <= 0 {
		limitStr = "无限制 / Unlimited"
	}
	return fmt.Sprintf(quotaAlertEmailTemplate, siteName, accountID, accountName, platform, dimLabel, used, limitStr, remaining, thresholdDisplay)
}
