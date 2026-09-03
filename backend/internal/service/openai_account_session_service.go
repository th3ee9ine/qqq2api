package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imroc/req/v3"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

// Kept as variables so upstream-contract tests can redirect only these calls.
var (
	chatGPTAccountSessionsURL      = "https://chatgpt.com/backend-api/accounts/sessions"
	chatGPTAccountSessionRevokeURL = "https://chatgpt.com/backend-api/accounts/sessions/revoke"
	chatGPTAccountSessionTrustURL  = "https://chatgpt.com/backend-api/accounts/sessions/trust"
)

const maxOpenAIAccountSessionBatchSize = 50

// The active-sessions endpoint normally returns one row per device.  Some
// response versions additionally nest concrete session records below a
// device's `sessions`/`app_sessions` field.  Keep recursive expansion tightly
// bounded: this parser consumes an upstream response and must not let a
// malformed payload turn into unbounded CPU or memory use.
const (
	maxOpenAIAccountSessionNestedDepth = 8
	maxOpenAIAccountSessionRows        = 4096
)

var openAIAccountSessionNestedCollectionKeys = []string{
	"sessions",
	"active_sessions",
	"activeSessions",
	"app_sessions",
	"appSessions",
	"session",
}

var openAIAccountSessionNestedWrapperKeys = []string{
	"data",
	"result",
	"payload",
	"items",
}

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
	Sessions     []OpenAIAccountSession `json:"sessions"`
	FetchedAt    int64                  `json:"fetched_at"`
	CurrentKnown bool                   `json:"current_known"`
}

type OpenAIAccountSessionRevokeFailure struct {
	SessionID string `json:"session_id"`
	Code      string `json:"code"`
}

// OpenAIAccountSessionBatchRevokeResult reports partial success instead of
// hiding successfully revoked sessions when one upstream request fails.
type OpenAIAccountSessionBatchRevokeResult struct {
	RequestedCount    int                                 `json:"requested_count"`
	SuccessCount      int                                 `json:"success_count"`
	FailedCount       int                                 `json:"failed_count"`
	RevokedSessionIDs []string                            `json:"revoked_session_ids"`
	Failures          []OpenAIAccountSessionRevokeFailure `json:"failures"`
}

// ListSessions queries ChatGPT's Active sessions control-plane endpoint using
// the same refreshed OAuth token, proxy, and browser-compatible client as quota
// requests.
func (s *OpenAIQuotaService) ListSessions(ctx context.Context, accountID int64) (*OpenAIAccountSessionList, error) {
	if ctx == nil {
		ctx = context.Background()
	}
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
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 512 {
		return infraerrors.New(http.StatusBadRequest, "OPENAI_SESSION_INVALID_ID", "session id is invalid")
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	client, headers, proxyURL, err := s.prepareSessionRevoke(callCtx, accountID)
	if err != nil {
		return err
	}
	return revokeOpenAIAccountSession(callCtx, client, headers, proxyURL, accountID, sessionID)
}

// RevokeSessions ends multiple ChatGPT sessions while reusing one refreshed
// credential and one upstream client. Individual failures are returned in the
// result so the UI can remove successful rows and keep failed rows selected.
func (s *OpenAIQuotaService) RevokeSessions(ctx context.Context, accountID int64, sessionIDs []string) (*OpenAIAccountSessionBatchRevokeResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedIDs, err := normalizeOpenAIAccountSessionIDs(sessionIDs)
	if err != nil {
		return nil, err
	}

	prepareCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	client, headers, proxyURL, err := s.prepareSessionRevoke(prepareCtx, accountID)
	cancel()
	if err != nil {
		return nil, err
	}

	result := &OpenAIAccountSessionBatchRevokeResult{
		RequestedCount:    len(normalizedIDs),
		RevokedSessionIDs: make([]string, 0, len(normalizedIDs)),
		Failures:          make([]OpenAIAccountSessionRevokeFailure, 0),
	}
	for _, sessionID := range normalizedIDs {
		callCtx, callCancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
		revokeErr := revokeOpenAIAccountSession(callCtx, client, headers, proxyURL, accountID, sessionID)
		callCancel()
		if revokeErr != nil {
			code := infraerrors.Reason(revokeErr)
			if code == "" {
				code = "OPENAI_SESSION_REVOKE_FAILED"
			}
			result.Failures = append(result.Failures, OpenAIAccountSessionRevokeFailure{
				SessionID: sessionID,
				Code:      code,
			})
			continue
		}
		result.RevokedSessionIDs = append(result.RevokedSessionIDs, sessionID)
	}
	result.SuccessCount = len(result.RevokedSessionIDs)
	result.FailedCount = len(result.Failures)
	slog.Info("openai_sessions_batch_revoke_complete",
		"account_id", accountID,
		"requested_count", result.RequestedCount,
		"success_count", result.SuccessCount,
		"failed_count", result.FailedCount,
	)
	return result, nil
}

// TrustSession marks a ChatGPT device session as trusted.  The admin UI only
// exposes this action for the current/local device; the upstream control-plane
// endpoint still accepts the concrete session identifier so the operation is
// idempotent and remains safe when a device contains multiple app sessions.
func (s *OpenAIQuotaService) TrustSession(ctx context.Context, accountID int64, sessionID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || len(sessionID) > 512 {
		return infraerrors.New(http.StatusBadRequest, "OPENAI_SESSION_INVALID_ID", "session id is invalid")
	}

	callCtx, cancel := context.WithTimeout(ctx, openaiQuotaUpstreamTimeout)
	defer cancel()
	client, headers, proxyURL, err := s.prepareSessionRevoke(callCtx, accountID)
	if err != nil {
		return err
	}
	return trustOpenAIAccountSession(callCtx, client, headers, proxyURL, accountID, sessionID)
}

// TrustCurrentSession resolves the authoritative current device first, then
// delegates to TrustSession.  This variant is useful for integrations that do
// not want to expose a session identifier in their request body and guarantees
// that only the local/current device can be marked trusted.
func (s *OpenAIQuotaService) TrustCurrentSession(ctx context.Context, accountID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	sessions, err := s.ListSessions(ctx, accountID)
	if err != nil {
		return err
	}
	if sessions == nil || !sessions.CurrentKnown {
		return infraerrors.New(http.StatusBadGateway, "OPENAI_CURRENT_SESSION_UNKNOWN", "the current device could not be identified")
	}
	for _, session := range sessions.Sessions {
		if !session.Current || strings.TrimSpace(session.ID) == "" {
			continue
		}
		if session.Trusted {
			return nil
		}
		return s.TrustSession(ctx, accountID, session.ID)
	}
	return infraerrors.New(http.StatusBadGateway, "OPENAI_CURRENT_SESSION_UNKNOWN", "the current device could not be identified")
}

func (s *OpenAIQuotaService) prepareSessionRevoke(ctx context.Context, accountID int64) (*req.Client, map[string]string, string, error) {
	accessToken, chatGPTAccountID, proxyURL, fedRAMP, err := s.prepareUpstreamCall(ctx, accountID)
	if err != nil {
		return nil, nil, "", err
	}
	client, err := s.privacyClientFactory(proxyURL)
	if err != nil {
		return nil, nil, "", infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_CLIENT_ERROR", "failed to build upstream client: %v", err)
	}
	headers, _, err := s.buildCodexQuotaHeaders(ctx, accountID, accessToken, chatGPTAccountID, fedRAMP)
	if err != nil {
		return nil, nil, "", infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSIONS_AUTH_FAILED", "failed to build upstream authentication: %v", err)
	}
	prepareChatGPTSessionHeaders(headers)
	return client, headers, proxyURL, nil
}

func revokeOpenAIAccountSession(
	ctx context.Context,
	client *req.Client,
	headers map[string]string,
	proxyURL string,
	accountID int64,
	sessionID string,
) error {
	resp, err := client.R().
		SetContext(ctx).
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

func trustOpenAIAccountSession(
	ctx context.Context,
	client *req.Client,
	headers map[string]string,
	proxyURL string,
	accountID int64,
	sessionID string,
) error {
	resp, err := client.R().
		SetContext(ctx).
		SetHeaders(headers).
		SetBody(map[string]string{"session_id": sessionID}).
		Post(chatGPTAccountSessionTrustURL)
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" {
			slog.Warn("openai_session_trust_proxy_unavailable", "account_id", accountID)
			return infraerrors.New(http.StatusBadGateway, "OPENAI_SESSION_TRUST_PROXY_UNAVAILABLE", "the account proxy could not connect to ChatGPT")
		}
		return infraerrors.Newf(http.StatusBadGateway, "OPENAI_SESSION_TRUST_REQUEST_FAILED", "upstream request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		slog.Warn("openai_session_trust_failed", "account_id", accountID, "status", resp.StatusCode)
		if resp.StatusCode == http.StatusNotFound {
			return infraerrors.New(http.StatusNotFound, "OPENAI_SESSION_NOT_FOUND", "session no longer exists")
		}
		return infraerrors.Newf(mapUpstreamStatus(resp.StatusCode), "OPENAI_SESSION_TRUST_UPSTREAM_ERROR", "upstream returned %d", resp.StatusCode)
	}
	slog.Info("openai_session_trust_success", "account_id", accountID)
	return nil
}

func normalizeOpenAIAccountSessionIDs(sessionIDs []string) ([]string, error) {
	if len(sessionIDs) == 0 {
		return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_SESSIONS_BATCH_EMPTY", "at least one session id is required")
	}
	if len(sessionIDs) > maxOpenAIAccountSessionBatchSize {
		return nil, infraerrors.Newf(http.StatusBadRequest, "OPENAI_SESSIONS_BATCH_TOO_LARGE", "at most %d sessions can be revoked at once", maxOpenAIAccountSessionBatchSize)
	}

	seen := make(map[string]struct{}, len(sessionIDs))
	normalized := make([]string, 0, len(sessionIDs))
	for _, rawID := range sessionIDs {
		sessionID := strings.TrimSpace(rawID)
		if sessionID == "" || len(sessionID) > 512 {
			return nil, infraerrors.New(http.StatusBadRequest, "OPENAI_SESSION_INVALID_ID", "session id is invalid")
		}
		if _, exists := seen[sessionID]; exists {
			continue
		}
		seen[sessionID] = struct{}{}
		normalized = append(normalized, sessionID)
	}
	return normalized, nil
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
	// A device row can contain concrete session records in a nested
	// `sessions`/`app_sessions` collection.  Expand only records carrying an
	// explicit session identifier; metadata-only app descriptors remain attached to
	// their parent row.  The parent row is retained for compatibility with the
	// flat `devices` contract and to avoid dropping a canonical device-level
	// revoke token.
	// Pass the envelope marker into expansion as well: a child can be identified
	// as current solely by `current_session_id`, without carrying its own boolean
	// marker.  In that shape the parent device token must still be protected.
	items = expandOpenAIAccountSessionItems(items, currentSessionID)

	// Keep track of whether upstream gave us an authoritative current-device
	// marker.  A missing marker is deliberately distinguishable from a row
	// whose Current value is simply false: callers that perform destructive
	// operations (such as scheduled session cleanup) can fail closed rather
	// than treating every row as a non-current device.
	currentKnown := currentSessionID != ""
	result := &OpenAIAccountSessionList{Sessions: make([]OpenAIAccountSession, 0, len(items))}
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if openAIAccountSessionCurrentMarkerPresent(row) {
			currentKnown = true
		}
		session := decodeOpenAIAccountSession(row)
		// `current_device_id` is not consistently the same identifier used by
		// the revoke endpoint.  Some response versions point at a render/device
		// id while each row carries the revocation token in `session_id`.
		// Compare the envelope marker with every stable row/device identifier so
		// we preserve the current device instead of silently treating it as an
		// old session.  This helper only adds preservation candidates; it never
		// broadens the set of sessions eligible for revocation.
		if currentSessionID != "" && openAIAccountSessionMatchesCurrentID(row, currentSessionID) {
			session.Current = true
			session.CanRevoke = false
		}
		result.Sessions = append(result.Sessions, session)
	}
	result.CurrentKnown = currentKnown
	return result, nil
}

type openAIAccountNestedSessionRow struct {
	row  map[string]any
	kind string
}

// expandOpenAIAccountSessionItems recursively discovers concrete child
// session rows below device/session rows.  It intentionally leaves scalar
// values and metadata-only objects alone; decodeOpenAIAccountSession already
// ignores non-map items just as it did before nested expansion was added.
func expandOpenAIAccountSessionItems(items []any, currentIDs ...string) []any {
	if len(items) == 0 {
		return items
	}
	currentID := ""
	if len(currentIDs) > 0 {
		currentID = strings.TrimSpace(currentIDs[0])
	}
	expanded := make([]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, nested := range expandOpenAIAccountSessionRow(row, 0, currentID) {
			expanded = append(expanded, nested)
			if len(expanded) >= maxOpenAIAccountSessionRows {
				return expanded
			}
		}
	}
	return expanded
}

// expandOpenAIAccountSessionRow keeps the device row and appends any concrete
// nested session rows.  A child inherits the parent's device metadata and
// current marker so a current device can never become revokable merely because
// the upstream response represented its sessions at a deeper level.
func expandOpenAIAccountSessionRow(row map[string]any, depth int, currentID string) []map[string]any {
	if row == nil {
		return nil
	}
	if depth >= maxOpenAIAccountSessionNestedDepth {
		return []map[string]any{row}
	}
	children := collectOpenAIAccountNestedSessionRows(row, depth+1)
	if len(children) == 0 {
		return []map[string]any{row}
	}

	result := make([]map[string]any, 0, 1+len(children))
	result = append(result, row)
	parentID := openAIAccountSessionNestedID(row, "session")
	parentMustBeProtected := false
	for _, child := range children {
		childID := openAIAccountSessionNestedID(child.row, child.kind)
		// A nested representation of the same canonical device token should not
		// create a duplicate row or duplicate revoke request.
		if parentID != "" && childID != "" && parentID == childID {
			continue
		}
		merged := mergeOpenAIAccountSessionParent(row, child.row, child.kind)
		expandedChildren := expandOpenAIAccountSessionRow(merged, depth+1, currentID)
		for _, expanded := range expandedChildren {
			// A concrete nested session can identify the current device even when
			// the parent device row has no marker of its own.  The parent may still
			// carry the canonical device-level revoke token; preserving only the
			// child would therefore let cleanup revoke the current device through
			// the parent row.  Protect the parent whenever any descendant is
			// positively identified as current, including envelope-ID matches.
			if descendantCurrent, descendantMarked := openAIAccountSessionEffectiveCurrentMarker(expanded); descendantMarked && descendantCurrent {
				parentMustBeProtected = true
			}
			if currentID != "" && openAIAccountSessionMatchesCurrentID(expanded, currentID) {
				parentMustBeProtected = true
			}
			result = append(result, expanded)
			if len(result) >= maxOpenAIAccountSessionRows {
				return result
			}
		}
	}
	if parentMustBeProtected {
		// Do not mutate the decoded root map: callers may reuse the raw payload
		// in diagnostics/tests.  A shallow marker overlay is enough because the
		// decoder only reads scalar current/capability fields from this row.
		protectedParent := cloneOpenAIAccountSessionValue(row).(map[string]any)
		protectedParent["current"] = true
		protectedParent["can_revoke"] = false
		result[0] = protectedParent
	}
	return result
}

// collectOpenAIAccountNestedSessionRows walks only the known nested
// collection/wrapper keys.  It never treats arbitrary map keys as session
// identifiers, which prevents a device/app metadata id from reaching the
// revocation path accidentally.
func collectOpenAIAccountNestedSessionRows(row map[string]any, depth int) []openAIAccountNestedSessionRow {
	if row == nil || depth > maxOpenAIAccountSessionNestedDepth {
		return nil
	}
	children := make([]openAIAccountNestedSessionRow, 0)
	for _, key := range openAIAccountSessionNestedCollectionKeys {
		raw, exists := row[key]
		if !exists {
			continue
		}
		children = append(children, collectOpenAIAccountNestedValue(raw, key, depth+1)...)
	}
	return children
}

func collectOpenAIAccountNestedValue(value any, kind string, depth int) []openAIAccountNestedSessionRow {
	if depth > maxOpenAIAccountSessionNestedDepth {
		return nil
	}
	children := make([]openAIAccountNestedSessionRow, 0)
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			children = append(children, collectOpenAIAccountNestedValue(item, kind, depth+1)...)
			if len(children) >= maxOpenAIAccountSessionRows {
				return children[:maxOpenAIAccountSessionRows]
			}
		}
	case map[string]any:
		if openAIAccountSessionNestedID(typed, kind) != "" {
			children = append(children, openAIAccountNestedSessionRow{row: typed, kind: kind})
			return children
		}
		// A wrapper may sit between the device and its child records.  Follow
		// only explicit collection keys; arbitrary metadata objects are ignored.
		for _, key := range openAIAccountSessionNestedWrapperKeys {
			if nested, exists := typed[key]; exists {
				children = append(children, collectOpenAIAccountNestedValue(nested, kind, depth+1)...)
			}
		}
		for _, key := range openAIAccountSessionNestedCollectionKeys {
			if nested, exists := typed[key]; exists {
				children = append(children, collectOpenAIAccountNestedValue(nested, key, depth+1)...)
			}
		}
	}
	return children
}

// openAIAccountSessionNestedID deliberately uses explicit session aliases for
// every nested collection.  A generic `id` can identify a device, product
// installation, or render record rather than a revocable account session, so
// it is never promoted into a revoke token during recursive expansion.
func openAIAccountSessionNestedID(row map[string]any, kind string) string {
	if row == nil {
		return ""
	}
	if id := sessionString(row,
		"session_id", "sessionId",
		"unified_session_id", "unifiedSessionId",
		"session_uuid", "sessionUuid",
	); id != "" {
		return id
	}
	return ""
}

func mergeOpenAIAccountSessionParent(parent, child map[string]any, kind string) map[string]any {
	merged := make(map[string]any, len(parent)+len(child))
	for key, value := range parent {
		if isOpenAIAccountSessionNestedCollectionKey(key) {
			continue
		}
		merged[key] = cloneOpenAIAccountSessionValue(value)
	}

	childID := openAIAccountSessionNestedID(child, kind)
	if childID != "" {
		// Remove inherited explicit ids before overlaying the child.  Otherwise a
		// parent session_id would win over a child's unified_session_id in
		// decodeOpenAIAccountSession's precedence order.
		for _, key := range []string{
			"session_id", "sessionId",
			"unified_session_id", "unifiedSessionId",
			"session_uuid", "sessionUuid",
		} {
			delete(merged, key)
		}
	}
	for key, value := range child {
		if isOpenAIAccountSessionNestedCollectionKey(key) {
			// Keep child collections so the next recursion level can inspect them;
			// parent collections were removed above to prevent re-expansion loops.
			merged[key] = cloneOpenAIAccountSessionValue(value)
			continue
		}
		if isOpenAIAccountSessionDeviceMapKey(key) {
			if parentMap, ok := merged[key].(map[string]any); ok {
				if childMap, ok := value.(map[string]any); ok {
					merged[key] = mergeOpenAIAccountSessionMaps(parentMap, childMap)
					continue
				}
			}
		}
		merged[key] = cloneOpenAIAccountSessionValue(value)
	}

	parentCurrent, parentMarked := openAIAccountSessionEffectiveCurrentMarker(parent)
	childCurrent, childMarked := openAIAccountSessionEffectiveCurrentMarker(child)
	if parentMarked {
		// A positive parent marker covers every nested app/session on that device.
		// This intentionally wins over a stale child false marker.
		if parentCurrent {
			merged["current"] = true
		} else if !childMarked {
			merged["current"] = false
		}
	} else if childMarked && childCurrent {
		// Keep an explicit positive child marker visible even when the parent did
		// not expose device-level provenance.
		merged["current"] = true
	}

	// Preserve useful parent app labels without carrying the parent's nested
	// collection back into recursion.  Child-specific client names take
	// precedence when available.
	if sessionString(child, "app_name", "appName", "application_name", "applicationName", "product", "client_name", "clientName") == "" {
		if names := sessionJoinedNames(parent, "apps", "applications", "products", "app_sessions", "appSessions"); names != "" {
			merged["app_name"] = names
		}
	}
	if clientName := sessionString(child, "client_name", "clientName"); clientName != "" && sessionString(child, "app_name", "appName", "application_name", "applicationName", "product") == "" {
		merged["app_name"] = clientName
	}
	return merged
}

func isOpenAIAccountSessionNestedCollectionKey(key string) bool {
	for _, candidate := range openAIAccountSessionNestedCollectionKeys {
		if key == candidate {
			return true
		}
	}
	return false
}

func isOpenAIAccountSessionDeviceMapKey(key string) bool {
	return key == "device" || key == "device_info" || key == "deviceInfo"
}

func mergeOpenAIAccountSessionMaps(parent, child map[string]any) map[string]any {
	merged := make(map[string]any, len(parent)+len(child))
	for key, value := range parent {
		merged[key] = cloneOpenAIAccountSessionValue(value)
	}
	for key, value := range child {
		merged[key] = cloneOpenAIAccountSessionValue(value)
	}
	return merged
}

func cloneOpenAIAccountSessionValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for key, nested := range typed {
			cloned[key] = cloneOpenAIAccountSessionValue(nested)
		}
		return cloned
	case []any:
		cloned := make([]any, len(typed))
		for index, nested := range typed {
			cloned[index] = cloneOpenAIAccountSessionValue(nested)
		}
		return cloned
	default:
		return value
	}
}

func openAIAccountSessionEffectiveCurrentMarker(row map[string]any) (bool, bool) {
	current, marked := openAIAccountSessionCurrentMarker(row)
	// A row-level descriptor can identify itself as the current session without
	// carrying a boolean marker (for example
	// {"session_id":"child", "current_session_id":"child"}).  The final
	// decoder promotes that self-match to Current=true; mirror the same
	// precedence during nested expansion so the parent device token is protected
	// before cleanup derives revocation candidates.
	if !current {
		if markerID := openAIAccountSessionRowCurrentSessionID(row); markerID != "" && openAIAccountSessionMatchesCurrentID(row, markerID) {
			current = true
			marked = true
		}
	}
	device := sessionMap(row, "device", "device_info", "deviceInfo")
	if nestedCurrent, nestedMarked := openAIAccountSessionCurrentMarker(device); nestedMarked {
		if nestedCurrent || !marked {
			current = nestedCurrent
		}
		marked = true
	}
	return current, marked
}

func openAIAccountSessionItems(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case map[string]any:
		emptyCollection := false
		for _, key := range []string{"sessions", "active_sessions", "activeSessions", "items", "devices", "result", "payload"} {
			if raw, exists := typed[key]; exists {
				if raw == nil {
					emptyCollection = true
					continue
				}
				if items, ok := raw.([]any); ok {
					if len(items) > 0 {
						return items, true
					}
					emptyCollection = true
					continue
				}
				if nested, ok := raw.(map[string]any); ok {
					if items, found := openAIAccountSessionItems(nested); found {
						if len(items) > 0 {
							return items, true
						}
						emptyCollection = true
					}
				}
			}
		}
		for _, key := range []string{"data", "result", "payload"} {
			if nested, exists := typed[key]; exists {
				if items, found := openAIAccountSessionItems(nested); found {
					if len(items) > 0 {
						return items, true
					}
					emptyCollection = true
				}
			}
		}
		if emptyCollection {
			return []any{}, true
		}
	}
	return nil, false
}

func decodeOpenAIAccountSession(row map[string]any) OpenAIAccountSession {
	device := sessionMap(row, "device", "device_info", "deviceInfo")
	client := sessionMap(row, "client", "app", "application")
	location := sessionMap(row, "location", "approximate_location", "approximateLocation")
	actions := sessionMap(row, "actions", "capabilities")

	// `session_id` is the common spelling, while a few session-manager
	// responses call the same revocation token a unified session id.  Accept
	// both forms (and the UUID aliases) so scheduled cleanup does not silently
	// skip a valid non-current device just because the response version changed.
	id := sessionString(row,
		// Explicit session-token fields take precedence over a generic `id`.
		// Current-device payloads may expose both a render/device id and the
		// unified session id used by the revoke endpoint.
		"session_id", "sessionId",
		"unified_session_id", "unifiedSessionId",
		"session_uuid", "sessionUuid",
		"id",
	)
	// Some session-manager response versions wrap the revocation token on the
	// nested device object.  Only accept fields whose names explicitly identify
	// a session; a generic device `id` may be a hardware identifier and must not
	// be sent to the logout endpoint by accident.
	if id == "" {
		id = sessionString(device,
			"session_id", "sessionId",
			"unified_session_id", "unifiedSessionId",
			"session_uuid", "sessionUuid",
		)
	}
	current, currentMarked := openAIAccountSessionCurrentMarker(row)
	if !current {
		if rowCurrentID := openAIAccountSessionRowCurrentSessionID(row); rowCurrentID != "" && openAIAccountSessionMatchesCurrentID(row, rowCurrentID) {
			current = true
			currentMarked = true
		}
	}
	// Some versions of the device-session endpoint put the marker on the
	// nested device object.  Honor it for both projection and CurrentKnown
	// accounting while keeping the accepted aliases in one place.  A positive
	// nested marker must not be masked by a stale/duplicated row-level `false`;
	// preserving the current device is the safe precedence rule.
	if nestedCurrent, nestedMarked := openAIAccountSessionCurrentMarker(device); nestedMarked {
		if nestedCurrent || !currentMarked {
			current = nestedCurrent
		}
		currentMarked = true
	}
	trusted := sessionBool(row, "trusted", "is_trusted", "isTrusted", "trusted_device", "trustedDevice", "is_trusted_device", "isTrustedDevice")
	if !trusted {
		trusted = sessionBool(device, "trusted", "is_trusted", "isTrusted", "trusted_device", "trustedDevice", "is_trusted_device", "isTrustedDevice")
	}
	if !trusted {
		trusted = strings.EqualFold(sessionString(row, "trust_status", "trustStatus", "device_trust_status", "deviceTrustStatus"), "trusted")
	}
	if !trusted {
		trusted = strings.EqualFold(sessionString(device, "trust_status", "trustStatus", "device_trust_status", "deviceTrustStatus"), "trusted")
	}
	status := sessionString(row, "status", "session_status", "sessionStatus")
	statusAvailable := true
	if value, present, valid := sessionOptionalBoolDetailed(row, "status_available", "statusAvailable", "is_status_available"); present {
		// An explicitly supplied but malformed availability flag is treated as
		// unavailable.  This keeps both the UI and the scheduled destructive
		// worker fail-closed when an upstream contract changes shape.
		statusAvailable = valid && value
	} else if normalized := strings.ToLower(strings.TrimSpace(status)); strings.Contains(normalized, "unavailable") || normalized == "unknown" || normalized == "unsupported" {
		statusAvailable = false
	}
	// Keep the projection aligned with the revoke endpoint's input bound.  A
	// malformed/oversized upstream identifier should not render an actionable
	// logout button in the admin UI (and is ignored by scheduled cleanup).
	validID := id != "" && len(id) <= 512
	canRevoke := validID && !current && statusAvailable
	if value, present, valid := sessionOptionalBoolDetailed(row, "can_revoke", "canRevoke", "can_logout", "canLogout", "can_sign_out", "canSignOut", "can_terminate", "canTerminate"); present {
		// A malformed explicit capability must never widen the default
		// (non-current + available) capability into a revocation action.
		canRevoke = valid && value && validID && !current && statusAvailable
	} else if value, present, valid := sessionOptionalBoolDetailed(actions, "can_revoke", "canRevoke", "can_logout", "canLogout", "can_sign_out", "canSignOut"); present {
		canRevoke = valid && value && validID && !current && statusAvailable
	}

	deviceName := sessionString(row, "device_name", "deviceName", "display_name", "displayName")
	if deviceName == "" {
		deviceName = sessionString(row, "human_readable_description", "humanReadableDescription", "device_model", "deviceModel")
	}
	if deviceName == "" {
		deviceName = sessionString(device, "display_name", "displayName", "name", "model", "human_readable_description", "humanReadableDescription", "device_model", "deviceModel")
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

// openAIAccountSessionCurrentMarker returns the value and whether upstream
// supplied a parseable explicit current-device flag.  The second result is
// intentionally separate from the value: an omitted marker and an explicit
// false marker have different safety implications for callers that may revoke
// sessions.
func openAIAccountSessionCurrentMarker(row map[string]any) (bool, bool) {
	if row == nil {
		return false, false
	}
	if current, marked := sessionCurrentMarkerBool(row,
		"current",
		"is_current",
		"isCurrent",
		"current_session",
		"currentSession",
		"active_session",
		"activeSession",
		"is_current_session",
		"isCurrentSession",
		"current_device",
		"currentDevice",
		"is_current_device",
		"isCurrentDevice",
	); marked {
		return current, true
	}
	// A few envelope/row variants use `current` as a descriptor object (for
	// example `{\"current\":{\"session_id\":\"...\"}}`) instead of a boolean.
	// Treat an explicitly identified descriptor as authoritative marker
	// provenance; the decoder later matches its identifier to the row before
	// protecting that session.  A match can only reduce the revoke set, so this
	// remains fail-closed if the descriptor uses a different identifier space.
	if raw, exists := row["current"]; exists {
		if nested, ok := raw.(map[string]any); ok {
			if current, marked := sessionCurrentMarkerBool(nested,
				"current",
				"is_current",
				"isCurrent",
				"is_current_session",
				"isCurrentSession",
				"current_device",
				"currentDevice",
				"is_current_device",
				"isCurrentDevice",
				"active_session",
				"activeSession",
			); marked {
				return current, true
			}
			if sessionString(nested,
				"id", "session_id", "sessionId",
				"unified_session_id", "unifiedSessionId",
				"session_uuid", "sessionUuid",
				"render_id", "renderId", "device_id", "deviceId",
				"hashed_device_id", "hashedDeviceId",
				"current_session_id", "currentSessionId", "currentSessionID",
				"current_device_id", "currentDeviceId", "currentDeviceID",
				"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
				"active_session_id", "activeSessionId", "activeSessionID",
			) != "" {
				return false, true
			}
		}
	}
	// `current_session` may be an ID (scalar) or a descriptor object rather
	// than a boolean. It is still authoritative marker presence, but the row is
	// marked current only when its ID matches that descriptor (see the decoder).
	for _, key := range []string{"current_session", "currentSession", "active_session", "activeSession"} {
		raw, exists := row[key]
		if !exists {
			continue
		}
		if nested, ok := raw.(map[string]any); ok {
			if sessionString(nested,
				"id", "session_id", "sessionId",
				"unified_session_id", "unifiedSessionId",
				"session_uuid", "sessionUuid",
				"render_id", "renderId", "device_id", "deviceId",
				"hashed_device_id", "hashedDeviceId",
				"current_session_id", "currentSessionId", "currentSessionID",
				"current_device_id", "currentDeviceId", "currentDeviceID",
				"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
				"active_session_id", "activeSessionId", "activeSessionID",
			) != "" {
				return false, true
			}
			continue
		}
		if id := sessionScalarString(raw); id != "" && !strings.EqualFold(id, "true") && !strings.EqualFold(id, "false") {
			return false, true
		}
	}
	// The marker can also be exposed as an *_id field or under a nested device
	// descriptor.  Reuse the identifier extractor so those variants contribute
	// to CurrentKnown and can be matched to the row's revoke token later.
	if descriptorID := openAIAccountSessionRowCurrentSessionID(row); descriptorID != "" {
		return false, true
	}
	// Some response variants represent current_session/current_device as an
	// object carrying its own marker. Inspect only that explicitly named object
	// so arbitrary device metadata is not mistaken for an authoritative flag.
	for _, key := range []string{"current_session", "currentSession", "active_session", "activeSession", "current_device", "currentDevice"} {
		nested, ok := row[key].(map[string]any)
		if !ok {
			continue
		}
		if current, marked := sessionCurrentMarkerBool(nested,
			"current",
			"is_current",
			"isCurrent",
			"is_current_session",
			"isCurrentSession",
			"current_device",
			"currentDevice",
			"is_current_device",
			"isCurrentDevice",
			"active_session",
			"activeSession",
		); marked {
			return current, true
		}
	}
	return false, false
}

// sessionCurrentMarkerBool parses a group of current-device aliases while
// giving any positive marker precedence over explicit false values.  Upstream
// payloads can contain both a stale row-level `current:false` and a newer
// `is_current:true`; preserving the device is the only safe resolution for
// that conflict.  A present-but-malformed marker is reported as unknown unless
// another alias supplies a parseable value.
func sessionCurrentMarkerBool(row map[string]any, keys ...string) (value bool, marked bool) {
	if row == nil {
		return false, false
	}
	present := false
	for _, key := range keys {
		raw, exists := row[key]
		if !exists {
			continue
		}
		present = true
		parsed, ok := parseSessionBool(raw)
		if !ok {
			continue
		}
		if parsed {
			return true, true
		}
		// Keep scanning: a later positive alias wins over this false marker.
		marked = true
	}
	if present && marked {
		return false, true
	}
	return false, false
}

func openAIAccountSessionRowCurrentSessionID(row map[string]any) string {
	if row == nil {
		return ""
	}
	for _, key := range []string{"current", "current_session", "currentSession", "active_session", "activeSession", "current_session_id", "currentSessionId", "currentSessionID", "active_session_id", "activeSessionId", "activeSessionID", "current_device_id", "currentDeviceId", "currentDeviceID", "current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID", "current_device", "currentDevice"} {
		raw, exists := row[key]
		if !exists {
			continue
		}
		if nested, ok := raw.(map[string]any); ok {
			if id := sessionString(nested,
				"id", "session_id", "sessionId",
				"unified_session_id", "unifiedSessionId",
				"session_uuid", "sessionUuid",
				"render_id", "renderId", "device_id", "deviceId",
				"hashed_device_id", "hashedDeviceId",
				"current_session_id", "currentSessionId", "currentSessionID",
				"current_device_id", "currentDeviceId", "currentDeviceID",
				"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
				"active_session_id", "activeSessionId", "activeSessionID",
			); id != "" {
				return id
			}
			continue
		}
		if key == "current_device" || key == "currentDevice" {
			// A scalar current_device is commonly a display label; only an
			// object descriptor carries a stable identifier.
			continue
		}
		if id := sessionScalarString(raw); id != "" && !strings.EqualFold(id, "true") && !strings.EqualFold(id, "false") {
			return id
		}
	}
	// Device descriptors may carry the marker while the row's revocation token
	// remains in `session_id`.  Inspect only explicitly named nested device
	// objects; a generic device `id` by itself is not treated as a marker.
	for _, containerKey := range []string{"device", "device_info", "deviceInfo"} {
		nested, ok := row[containerKey].(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"current_session", "currentSession", "active_session", "activeSession", "current_session_id", "currentSessionId", "currentSessionID", "active_session_id", "activeSessionId", "activeSessionID", "current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID", "current_device", "currentDevice"} {
			raw, exists := nested[key]
			if !exists {
				continue
			}
			if descriptor, ok := raw.(map[string]any); ok {
				if id := sessionString(descriptor,
					"session_id", "sessionId", "unified_session_id", "unifiedSessionId",
					"session_uuid", "sessionUuid", "id", "render_id", "renderId",
					"device_id", "deviceId", "hashed_device_id", "hashedDeviceId",
					"current_session_id", "currentSessionId", "currentSessionID",
					"current_device_id", "currentDeviceId", "currentDeviceID",
					"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
					"active_session_id", "activeSessionId", "activeSessionID",
				); id != "" {
					return id
				}
				continue
			}
			// A scalar current_session/currentSession is explicitly a session
			// identifier; current_device is often a display label and is skipped.
			if key == "current_session" || key == "currentSession" || key == "active_session" || key == "activeSession" || strings.Contains(strings.ToLower(key), "session_id") || strings.Contains(strings.ToLower(key), "sessionid") {
				if id := sessionScalarString(raw); id != "" && !strings.EqualFold(id, "true") && !strings.EqualFold(id, "false") {
					return id
				}
			}
		}
	}
	return ""
}

// openAIAccountSessionMatchesCurrentID reports whether an envelope/descriptor
// current marker identifies this row.  The sessions endpoint has used several
// identifier namespaces over time: `session_id` is the revoke token, while
// `id`, `render_id`, `device_id`, or `hashed_device_id` can identify the same
// device.  Matching all explicitly named identifiers is safe for cleanup: a
// match only marks a row as current (and therefore protected), never as
// revokable.
func openAIAccountSessionMatchesCurrentID(row map[string]any, currentID string) bool {
	currentID = strings.TrimSpace(currentID)
	if row == nil || currentID == "" || len(currentID) > 512 {
		return false
	}
	if sessionIdentifierMatches(row, currentID) {
		return true
	}
	for _, key := range []string{"device", "device_info", "deviceInfo"} {
		if nested, ok := row[key].(map[string]any); ok && sessionIdentifierMatches(nested, currentID) {
			return true
		}
	}
	return false
}

func sessionIdentifierMatches(row map[string]any, wanted string) bool {
	if row == nil {
		return false
	}
	for _, key := range []string{
		"id", "render_id", "renderId", "device_id", "deviceId",
		"hashed_device_id", "hashedDeviceId",
		"session_id", "sessionId",
		"unified_session_id", "unifiedSessionId",
		"session_uuid", "sessionUuid",
		"current_session_id", "currentSessionId", "currentSessionID",
		"current_device_id", "currentDeviceId", "currentDeviceID",
		"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
		"active_session_id", "activeSessionId", "activeSessionID",
	} {
		if value, exists := row[key]; exists {
			if candidate := strings.TrimSpace(sessionScalarString(value)); candidate != "" && candidate == wanted {
				return true
			}
		}
	}
	return false
}

func openAIAccountSessionCurrentMarkerPresent(row map[string]any) bool {
	if _, marked := openAIAccountSessionCurrentMarker(row); marked {
		return true
	}
	device := sessionMap(row, "device", "device_info", "deviceInfo")
	_, marked := openAIAccountSessionCurrentMarker(device)
	return marked
}

func openAIAccountCurrentSessionID(value any) string {
	row, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if id := sessionString(row,
		"current_session_id",
		"currentSessionId",
		"currentSessionID",
		"current_device_id",
		"currentDeviceId",
		"currentDeviceID",
		"current_device_session_id",
		"currentDeviceSessionId",
		"currentDeviceSessionID",
		"active_session_id",
		"activeSessionId",
		"activeSessionID",
	); id != "" {
		return id
	}
	// Although these fields are usually scalar strings, a few envelopes wrap
	// the marker in a descriptor object.  Accept the same explicit identifier
	// aliases there without guessing from arbitrary metadata.
	for _, key := range []string{
		"current_session_id", "currentSessionId", "currentSessionID",
		"current_device_id", "currentDeviceId", "currentDeviceID",
		"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
		"active_session_id", "activeSessionId", "activeSessionID",
	} {
		nested, ok := row[key].(map[string]any)
		if !ok {
			continue
		}
		if id := sessionString(nested,
			"session_id", "sessionId", "unified_session_id", "unifiedSessionId",
			"session_uuid", "sessionUuid", "id", "render_id", "renderId",
			"device_id", "deviceId", "hashed_device_id", "hashedDeviceId",
			"current_session_id", "currentSessionId", "currentSessionID",
			"current_device_id", "currentDeviceId", "currentDeviceID",
			"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
			"active_session_id", "activeSessionId", "activeSessionID",
		); id != "" {
			return id
		}
	}
	// `current` itself may be a descriptor object or a scalar session token.
	// Scalar boolean values are deliberately ignored; they are handled by the
	// row-level marker parser above and do not identify a session to preserve.
	if raw, exists := row["current"]; exists {
		if nested, ok := raw.(map[string]any); ok {
			if id := sessionString(nested,
				"session_id", "sessionId", "unified_session_id", "unifiedSessionId",
				"session_uuid", "sessionUuid", "id", "render_id", "renderId",
				"device_id", "deviceId", "hashed_device_id", "hashedDeviceId",
				"current_session_id", "currentSessionId", "currentSessionID",
				"current_device_id", "currentDeviceId", "currentDeviceID",
				"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
				"active_session_id", "activeSessionId", "activeSessionID",
			); id != "" {
				return id
			}
		} else if id := sessionScalarString(raw); id != "" && !strings.EqualFold(id, "true") && !strings.EqualFold(id, "false") {
			return id
		}
	}
	// A few response versions return the current device/session as a small
	// object rather than a dedicated *_id field.  Do not coerce arbitrary
	// scalar current_device values (often a display name) into a session id.
	for _, key := range []string{"current_session", "currentSession", "active_session", "activeSession", "current_device", "currentDevice"} {
		raw, exists := row[key]
		if !exists {
			continue
		}
		if nested, ok := raw.(map[string]any); ok {
			if id := sessionString(nested,
				"session_id", "sessionId",
				"unified_session_id", "unifiedSessionId",
				"session_uuid", "sessionUuid",
				"id", "render_id", "renderId", "device_id", "deviceId",
				"hashed_device_id", "hashedDeviceId",
				"current_session_id", "currentSessionId", "currentSessionID",
				"current_device_id", "currentDeviceId", "currentDeviceID",
				"current_device_session_id", "currentDeviceSessionId", "currentDeviceSessionID",
				"active_session_id", "activeSessionId", "activeSessionID",
			); id != "" {
				return id
			}
			continue
		}
		// `current_session` has also appeared as a scalar session identifier.
		// Treat only that explicitly session-named field as an ID; a scalar
		// `current_device` is commonly a human-readable device label and must
		// not be guessed into a revocation token.
		if key == "current_session" || key == "currentSession" || key == "active_session" || key == "activeSession" {
			if id := sessionScalarString(raw); id != "" && !strings.EqualFold(id, "true") && !strings.EqualFold(id, "false") {
				return id
			}
		}
	}
	for _, key := range []string{"data", "result", "payload", "sessions", "active_sessions", "activeSessions", "app_sessions", "appSessions", "session", "items", "devices"} {
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
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return ""
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		if math.IsNaN(float64(typed)) || math.IsInf(float64(typed), 0) {
			return ""
		}
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int8:
		return strconv.FormatInt(int64(typed), 10)
	case int16:
		return strconv.FormatInt(int64(typed), 10)
	case int32:
		return strconv.FormatInt(int64(typed), 10)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint8:
		return strconv.FormatUint(uint64(typed), 10)
	case uint16:
		return strconv.FormatUint(uint64(typed), 10)
	case uint32:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case uintptr:
		return strconv.FormatUint(uint64(typed), 10)
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
	value, _, valid := sessionOptionalBoolDetailed(row, keys...)
	return value, valid
}

// sessionOptionalBoolDetailed distinguishes an omitted field from a field
// that was present but malformed.  The distinction is important for
// destructive actions: omitted capability flags retain the compatibility
// default, while malformed explicit flags fail closed.
func sessionOptionalBoolDetailed(row map[string]any, keys ...string) (value bool, present bool, valid bool) {
	for _, key := range keys {
		raw, exists := row[key]
		if !exists {
			continue
		}
		present = true
		if parsed, ok := parseSessionBool(raw); ok {
			return parsed, true, true
		}
		// Once an explicitly named capability is malformed, fail closed rather
		// than falling through to a legacy alias that could accidentally widen
		// the destructive action surface.
		return false, true, false
	}
	return false, present, false
}

func parseSessionBool(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		text := strings.TrimSpace(typed)
		parsed, err := strconv.ParseBool(text)
		if err == nil {
			return parsed, true
		}
		// A few JSON bridges serialize boolean flags as the numeric strings
		// "0"/"1".  Accept only those two exact values; arbitrary numeric
		// strings remain malformed and therefore fail closed.
		if text == "0" || text == "1" {
			return text == "1", true
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			if parsed == 0 || parsed == 1 {
				return parsed == 1, true
			}
		} else if parsed, err := typed.Float64(); err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0) {
			if parsed == 0 || parsed == 1 {
				return parsed == 1, true
			}
		}
	case float64:
		if !math.IsNaN(typed) && !math.IsInf(typed, 0) && (typed == 0 || typed == 1) {
			return typed == 1, true
		}
	case float32:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case int:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case int8:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case int16:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case int32:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case int64:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case uint:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case uint8:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case uint16:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case uint32:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case uint64:
		if typed == 0 || typed == 1 {
			return typed == 1, true
		}
	case uintptr:
		if typed == 0 || typed == 1 {
			return typed == 1, true
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
