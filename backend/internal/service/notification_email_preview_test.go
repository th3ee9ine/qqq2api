package service

import (
	"context"
	"strings"
)

type notificationEmailTestPreviewInput struct {
	Event     string            `json:"event"`
	Locale    string            `json:"locale"`
	Subject   string            `json:"subject"`
	HTML      string            `json:"html"`
	Variables map[string]string `json:"variables,omitempty"`
}

func previewNotificationTemplate(s *NotificationEmailService, ctx context.Context, input notificationEmailTestPreviewInput) (NotificationEmailPreview, error) {
	_, normalizedEvent, err := s.eventInfo(input.Event)
	if err != nil {
		return NotificationEmailPreview{}, err
	}
	normalizedLocale := normalizeNotificationLocale(input.Locale)
	subject := input.Subject
	htmlBody := input.HTML
	if strings.TrimSpace(subject) == "" || strings.TrimSpace(htmlBody) == "" {
		tmpl, err := s.GetTemplate(ctx, normalizedEvent, normalizedLocale)
		if err != nil {
			return NotificationEmailPreview{}, err
		}
		if strings.TrimSpace(subject) == "" {
			subject = tmpl.Subject
		}
		if strings.TrimSpace(htmlBody) == "" {
			htmlBody = tmpl.HTML
		}
	}
	if err := validateNotificationEmailTemplate(normalizedEvent, subject, htmlBody); err != nil {
		return NotificationEmailPreview{}, err
	}
	variables := s.sampleVariables(ctx, normalizedEvent, normalizedLocale)
	for key, value := range input.Variables {
		variables[key] = value
	}
	return renderNotificationEmail(normalizedEvent, subject, htmlBody, variables, nil)
}
