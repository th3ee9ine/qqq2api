package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

// Kept as variables so upstream-contract tests can redirect only these calls.
var (
	chatGPTAccountSessionsURL      = "https://chatgpt.com/backend-api/accounts/sessions"
	chatGPTAccountSessionRevokeURL = "https://chatgpt.com/backend-api/accounts/sessions/revoke"
)

// OpenAIAccountSession is the stable, privacy-conscious projection exposed to
// the admin UI. Upstream has used both snake_case and camelCase names, so the
// decoder below accepts both without leaking the full upstream payload.
type OpenAIAccountSession struct {
	ID              string `json:"id"`
	DeviceName      string `json:"device_name,omitempty"`
	DeviceType      string `json:"device_type,omitempty"`
	OS              string `json:"os,omitempty"`
	Browser         string `json:"browser,omitempty"`
	AppName         string `json:"app_name,omitempty"`
	Location        string `json:"location,omitempty"`
	SignedInAt      string `json:"signed_in_at,omitempty"`
	LastActiveAt    string `json:"last_active_at,omitempty"`
	Current         bool   `json:"current"`
	Trusted         bool   `json:"trusted"`
	Status          string `json:"status,omitempty"`
	StatusAvailable bool   `json:"status_available"`
	CanRevoke       bool   `json:"can_revoke"`
}

type OpenAIAccountSessionList struct {
	Sessions  []OpenAIAccountSession `json:"sessions"`
	FetchedAt int64                  `json:"fetched_at"`
}

// ListSessions queries ChatGPT's Active sessions control-plane endpoint using
// the same refreshed OAuth token, proxy, and browser-compatible client as quota
// requests.
func (s *OpenAIQuotaService) ListSessions(ctx context.Context, accountID int64) (*OpenAIAccountSessionList, error) {
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, err
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	headers, _, err := s.buildCodexQuotaHeaders(callCtx, accountID, accessToken, chatGPTAccountID, fedRAMP)
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_AUTH_FAILED", "failed to build upstream authentication: %v", err)
	}
	prepareChatGPTSessionHeaders(headers)
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		Get(chatGPTAccountSessionsURL)
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" {
			slog.Warn("openai_sessions_proxy_unavailable", "account_id", accountID)
			return nil, infraerrors.New(http.StatusBadGateway, "OPENAI_SESSIONS_PROXY_UNAVAILABLE", "the account proxy could not connect to ChatGPT")
		}
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		slog.Warn("openai_sessions_query_failed", "account_id", accountID, "status", resp.StatusCode)
		return nil, infraerrors.Newf(mapUpstreamStatus(resp.StatusCode), "OPENAI_SESSIONS_UPSTREAM_ERROR", "upstream returned %d", resp.StatusCode)
	}

	result, err := decodeOpenAIAccountSessions(resp.Bytes())
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_INVALID_RESPONSE", "failed to decode upstream response: %v", err)
	}
	result.FetchedAt = time.Now().Unix()
	return result, nil
}

// RevokeSession ends one non-current ChatGPT session. ChatGPT's control-plane
// contract uses POST /accounts/sessions/revoke with the unified session id in a
// JSON body (rather than DELETE /accounts/sessions/{id}).
func (s *OpenAIQuotaService) RevokeSession(ctx context.Context, accountID int64, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 512 {
		return infraerrors.New(http.StatusBadRequest, "OPENAI_SESSION_INVALID_ID", "session id is invalid")
	}
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return err
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	headers, _, err := s.buildCodexQuotaHeaders(callCtx, accountID, accessToken, chatGPTAccountID, fedRAMP)
	if err != nil {
		return infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_AUTH_FAILED", "failed to build upstream authentication: %v", err)
	}
	prepareChatGPTSessionHeaders(headers)
	resp, err := client.R().
		SetContext(callCtx).
		SetHeaders(headers).
		SetBody(map[string]string{"session_id": sessionID}).
		Post(chatGPTAccountSessionRevokeURL)
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" {
			slog.Warn("openai_session_revoke_proxy_unavailable", "account_id", accountID)
			return infraerrors.New(http.StatusBadGateway, "OPENAI_SESSION_REVOKE_PROXY_UNAVAILABLE", "the account proxy could not connect to ChatGPT")
		}
		return infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSION_REVOKE_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		slog.Warn("openai_session_revoke_failed", "account_id", accountID, "status", resp.StatusCode)
		if resp.StatusCode == http.StatusNotFound {
			return infraerrors.New(http.StatusNotFound, "OPENAI_SESSION_NOT_FOUND", "session no longer exists")
		}
		return infraerrors.Newf(mapUpstreamStatus(resp.StatusCode), "OPENAI_SESSION_REVOKE_UPSTREAM_ERROR", "upstream returned %d", resp.StatusCode)
	}
	slog.Info("openai_session_revoke_success", "account_id", accountID)
	return nil
}

func decodeOpenAIAccountSessions(body []byte) (*OpenAIAccountSessionList, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	items, ok := openAIAccountSessionItems(root)
	if !ok {
		return nil, fmt.Errorf("sessions collection is missing")
	}

	currentSessionID := openAIAccountCurrentSessionID(root)
	result := &OpenAIAccountSessionList{Sessions: make([]OpenAIAccountSession, 0, len(items))}
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		session := decodeOpenAIAccountSession(row)
		if currentSessionID != "" && session.ID == currentSessionID {
			session.Current = true
			session.CanRevoke = false
		}
		result.Sessions = append(result.Sessions, session)
	}
	return result, nil
}

func openAIAccountSessionItems(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case map[string]any:
		for _, key := range []string{"sessions", "active_sessions", "activeSessions", "items", "devices"} {
			if raw, exists := typed[key]; exists {
				if raw == nil {
					return []any{}, true
				}
				if items, ok := raw.([]any); ok {
					return items, true
				}
				if nested, ok := raw.(map[string]any); ok {
					if items, found := openAIAccountSessionItems(nested); found {
						return items, true
					}
				}
			}
		}
		if nested, exists := typed["data"]; exists {
			return openAIAccountSessionItems(nested)
		}
	}
	return nil, false
}

func decodeOpenAIAccountSession(row map[string]any) OpenAIAccountSession {
	device := sessionMap(row, "device", "device_info", "deviceInfo")
	client := sessionMap(row, "client", "app", "application")
	location := sessionMap(row, "location", "approximate_location", "approximateLocation")
	actions := sessionMap(row, "actions", "capabilities")

	id := sessionString(row, "id", "session_id", "sessionId", "session_uuid", "sessionUuid")
	current := sessionBool(row, "current", "is_current", "isCurrent", "is_current_session", "isCurrentSession", "is_current_device", "isCurrentDevice")
	trusted := sessionBool(row, "trusted", "is_trusted", "isTrusted", "trusted_device", "trustedDevice", "is_trusted_device", "isTrustedDevice")
	if !trusted {
		trusted = strings.EqualFold(sessionString(row, "trust_status", "trustStatus", "device_trust_status", "deviceTrustStatus"), "trusted")
	}
	status := sessionString(row, "status", "session_status", "sessionStatus")
	statusAvailable := true
	if value, ok := sessionOptionalBool(row, "status_available", "statusAvailable", "is_status_available"); ok {
		statusAvailable = value
	} else if normalized := strings.ToLower(strings.TrimSpace(status)); strings.Contains(normalized, "unavailable") || normalized == "unknown" || normalized == "unsupported" {
		statusAvailable = false
	}
	canRevoke := id != "" && !current && statusAvailable
	if value, ok := sessionOptionalBool(row, "can_revoke", "canRevoke", "can_logout", "canLogout", "can_sign_out", "canSignOut", "can_terminate", "canTerminate"); ok {
		canRevoke = value && id != "" && !current && statusAvailable
	} else if value, ok := sessionOptionalBool(actions, "can_revoke", "canRevoke", "can_logout", "canLogout", "can_sign_out", "canSignOut"); ok {
		canRevoke = value && id != "" && !current && statusAvailable
	}

	deviceName := sessionString(row, "device_name", "deviceName", "display_name", "displayName")
	if deviceName == "" {
		deviceName = sessionString(device, "display_name", "displayName", "name", "model")
	}
	if deviceName == "" {
		deviceName = sessionScalarString(row["device"])
	}
	locationText := sessionString(row, "location_name", "locationName")
	if locationText == "" {
		locationText = firstNonEmptySession(
			sessionScalarString(row["location"]),
			sessionScalarString(row["approximate_location"]),
			sessionScalarString(row["approximateLocation"]),
		)
	}
	if locationText == "" {
		locationText = sessionString(location, "display_name", "displayName", "name", "label")
	}
	if locationText == "" {
		locationText = sessionLocation(location)
	}

	appName := sessionString(row, "app_name", "appName", "application_name", "applicationName", "product")
	if appName == "" {
		appName = sessionString(client, "display_name", "displayName", "name", "product")
	}
	if appName == "" {
		appName = sessionScalarString(row["app"])
	}
	if appName == "" {
		appName = sessionJoinedNames(row, "apps", "applications", "products", "app_sessions", "appSessions")
	}
	deviceType := firstNonEmptySession(
		sessionString(row, "device_type", "deviceType"),
		sessionString(device, "type", "device_type", "deviceType"),
		sessionString(row, "platform"),
	)
	osName := firstNonEmptySession(
		sessionString(row, "os", "operating_system", "operatingSystem"),
		sessionString(device, "os", "operating_system", "operatingSystem"),
		sessionString(row, "platform"),
		sessionString(device, "platform"),
	)
	osVersion := firstNonEmptySession(
		sessionString(row, "os_version", "osVersion"),
		sessionString(device, "os_version", "osVersion"),
	)
	if osVersion != "" && !strings.Contains(strings.ToLower(osName), strings.ToLower(osVersion)) {
		osName = strings.TrimSpace(strings.Join([]string{osName, osVersion}, " "))
	}
	if locationText == "" {
		locationText = sessionLocationFields(row,
			"last_signed_in_city",
			"last_signed_in_region_code",
			"last_signed_in_country",
		)
	}

	return OpenAIAccountSession{
		ID:              id,
		DeviceName:      deviceName,
		DeviceType:      deviceType,
		OS:              osName,
		Browser:         firstNonEmptySession(sessionString(row, "browser", "browser_name", "browserName"), sessionString(device, "browser", "browser_name", "browserName"), sessionString(client, "browser")),
		AppName:         appName,
		Location:        locationText,
		SignedInAt:      sessionString(row, "signed_in_at", "signedInAt", "created_at", "createdAt", "created", "login_at", "loginAt", "last_signed_in_timestamp_second", "lastSignedInTimestampSecond"),
		LastActiveAt:    sessionString(row, "last_active_at", "lastActiveAt", "last_active", "lastActive", "last_seen_at", "lastSeenAt", "updated_at", "updatedAt"),
		Current:         current,
		Trusted:         trusted,
		Status:          status,
		StatusAvailable: statusAvailable,
		CanRevoke:       canRevoke,
	}
}

func openAIAccountCurrentSessionID(value any) string {
	row, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if id := sessionString(row, "current_session_id", "currentSessionId"); id != "" {
		return id
	}
	for _, key := range []string{"data", "active_sessions", "activeSessions"} {
		if id := openAIAccountCurrentSessionID(row[key]); id != "" {
			return id
		}
	}
	return ""
}

func prepareChatGPTSessionHeaders(headers map[string]string) {
	if headers == nil {
		return
	}
	headers["origin"] = "https://chatgpt.com"
	headers["referer"] = "https://chatgpt.com/"
	headers["sec-fetch-site"] = "same-origin"
	headers["sec-fetch-mode"] = "cors"
}

func sessionMap(row map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := row[key].(map[string]any); ok {
			return value
		}
	}
	return nil
}

func sessionString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, exists := row[key]; exists {
			if text := sessionScalarString(value); text != "" {
				return text
			}
		}
	}
	return ""
}

func sessionScalarString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(typed, 10)
	}
	return ""
}

func sessionJoinedNames(row map[string]any, keys ...string) string {
	for _, key := range keys {
		values, ok := row[key].([]any)
		if !ok {
			continue
		}
		names := make([]string, 0, len(values))
		for _, value := range values {
			name := sessionScalarString(value)
			if item, ok := value.(map[string]any); ok {
				name = sessionString(item, "display_name", "displayName", "name", "product", "client_name", "clientName")
			}
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			return strings.Join(names, ", ")
		}
	}
	return ""
}

func sessionBool(row map[string]any, keys ...string) bool {
	value, _ := sessionOptionalBool(row, keys...)
	return value
}

func sessionOptionalBool(row map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, exists := row[key]
		if !exists {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err == nil {
				return parsed, true
			}
		case json.Number:
			parsed, err := typed.Int64()
			if err == nil {
				return parsed != 0, true
			}
		}
	}
	return false, false
}

func sessionLocation(location map[string]any) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{
		sessionString(location, "city"),
		sessionString(location, "region", "state"),
		sessionString(location, "country", "country_name", "countryName"),
	} {
		if value == "" || (len(parts) > 0 && parts[len(parts)-1] == value) {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ", ")
}

func sessionLocationFields(row map[string]any, keys ...string) string {
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := sessionString(row, key)
		if value == "" || (len(parts) > 0 && parts[len(parts)-1] == value) {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ", ")
}

func firstNonEmptySession(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
