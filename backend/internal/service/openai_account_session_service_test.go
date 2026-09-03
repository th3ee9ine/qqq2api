//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
)

func TestDecodeOpenAIAccountSessionsAcceptsEnvelopeAliases(t *testing.T) {
	payload := []byte(`{
		"data": {
			"activeSessions": [{
				"sessionId": "sess-1",
				"device": {"name": "MacBook Pro", "type": "desktop", "os": "macOS", "browser": "Chrome"},
				"app": {"name": "ChatGPT"},
				"location": {"city": "Shanghai", "country": "China"},
				"signedInAt": "2026-08-20T10:00:00Z",
				"lastActiveAt": 1787900000,
				"isCurrent": false,
				"isTrusted": true,
				"canLogout": true
			}]
		}
	}`)

	result, err := decodeOpenAIAccountSessions(payload)

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	session := result.Sessions[0]
	require.Equal(t, "sess-1", session.ID)
	require.Equal(t, "MacBook Pro", session.DeviceName)
	require.Equal(t, "desktop", session.DeviceType)
	require.Equal(t, "macOS", session.OS)
	require.Equal(t, "Chrome", session.Browser)
	require.Equal(t, "ChatGPT", session.AppName)
	require.Equal(t, "Shanghai, China", session.Location)
	require.Equal(t, "1787900000", session.LastActiveAt)
	require.True(t, session.Trusted)
	require.True(t, session.StatusAvailable)
	require.True(t, session.CanRevoke)
}

func TestDecodeOpenAIAccountSessionsPrefersNonEmptyNestedCollection(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"sessions": [],
		"data": {"active_sessions": [{"id":"sess-nested","current":true}]}
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	require.Equal(t, "sess-nested", result.Sessions[0].ID)
	require.True(t, result.Sessions[0].Current)
	require.True(t, result.CurrentKnown)
}

func TestDecodeOpenAIAccountSessionsNeverAllowsCurrentOrUnavailableSessionRevoke(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`[
		{"id":"current","current":true,"can_revoke":true},
		{"id":"unknown","status":"session_status_unavailable","can_revoke":true}
	]`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 2)
	require.False(t, result.Sessions[0].CanRevoke)
	require.False(t, result.Sessions[1].StatusAvailable)
	require.False(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsHonorsEnvelopeCurrentSessionID(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"current_session_id":"sess-current",
		"sessions":[{"id":"sess-current","can_revoke":true}]
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	require.True(t, result.Sessions[0].Current)
	require.False(t, result.Sessions[0].CanRevoke)
	require.True(t, result.CurrentKnown)
}

func TestDecodeOpenAIAccountSessionsHonorsCurrentDeviceIDAliases(t *testing.T) {
	for _, payload := range []string{
		`{"current":{"id":"sess-current"},"sessions":[{"id":"sess-current"},{"id":"sess-other","can_revoke":true}]}`,
		`{"current_device_id":"sess-current","sessions":[{"id":"sess-current"},{"id":"sess-other","can_revoke":true}]}`,
		`{"currentDevice":{"session_id":"sess-current"},"sessions":[{"id":"sess-current"},{"id":"sess-other","can_revoke":true}]}`,
		`{"current_session":{"id":"sess-current"},"sessions":[{"id":"sess-current"},{"id":"sess-other","can_revoke":true}]}`,
		`{"current_session":"sess-current","sessions":[{"id":"sess-current"},{"id":"sess-other","can_revoke":true}]}`,
		`{"current_session_id":"sess-current","sessions":[{"unified_session_id":"sess-current"},{"unifiedSessionId":"sess-other","can_revoke":true}]}`,
	} {
		result, err := decodeOpenAIAccountSessions([]byte(payload))
		require.NoError(t, err)
		require.True(t, result.CurrentKnown)
		require.True(t, result.Sessions[0].Current)
		require.False(t, result.Sessions[0].CanRevoke)
		require.True(t, result.Sessions[1].CanRevoke)
	}
}

func TestDecodeOpenAIAccountSessionsHonorsActiveSessionAliases(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{
			name: "snake case active session id",
			payload: `{"active_session_id":"sess-current","sessions":[
				{"session_id":"sess-current"},
				{"session_id":"sess-old","can_revoke":true}
			]}`,
		},
		{
			name: "camel case active session id",
			payload: `{"activeSessionId":"sess-current","sessions":[
				{"session_id":"sess-current"},
				{"session_id":"sess-old","can_revoke":true}
			]}`,
		},
		{
			name: "acronym active session id matches device id",
			payload: `{"activeSessionID":"render-current","sessions":[
				{"session_id":"sess-current","device":{"render_id":"render-current"}},
				{"session_id":"sess-old","device":{"render_id":"render-old"},"can_revoke":true}
			]}`,
		},
		{
			name: "active session descriptor",
			payload: `{"active_session":{"session_id":"sess-current"},"sessions":[
				{"session_id":"sess-current"},
				{"session_id":"sess-old","can_revoke":true}
			]}`,
		},
		{
			name: "camel active session descriptor matches nested device",
			payload: `{"activeSession":{"id":"render-current"},"sessions":[
				{"session_id":"sess-current","device":{"render_id":"render-current"}},
				{"session_id":"sess-old","device":{"render_id":"render-old"},"can_revoke":true}
			]}`,
		},
		{
			name: "nested envelope active session descriptor",
			payload: `{"data":{"activeSession":{"sessionId":"sess-current"},"items":[
				{"session_id":"sess-current"},
				{"session_id":"sess-old","can_revoke":true}
			]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeOpenAIAccountSessions([]byte(tt.payload))
			require.NoError(t, err)
			require.True(t, result.CurrentKnown)
			require.Len(t, result.Sessions, 2)
			require.True(t, result.Sessions[0].Current)
			require.False(t, result.Sessions[0].CanRevoke)
			require.False(t, result.Sessions[1].Current)
			require.True(t, result.Sessions[1].CanRevoke)
		})
	}
}

func TestDecodeOpenAIAccountSessionsHonorsRowActiveSessionDescriptor(t *testing.T) {
	for _, row := range []string{
		`{"id":"sess-current","active_session":"sess-current"}`,
		`{"id":"sess-current","activeSession":{"session_id":"sess-current"}}`,
	} {
		result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[` + row + `,{"id":"sess-old","can_revoke":true}]}`))
		require.NoError(t, err)
		require.True(t, result.CurrentKnown)
		require.True(t, result.Sessions[0].Current)
		require.False(t, result.Sessions[0].CanRevoke)
		require.True(t, result.Sessions[1].CanRevoke)
	}
}

func TestDecodeOpenAIAccountSessionsHonorsRowCurrentDescriptor(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[
		{"id":"sess-current","current":{"session_id":"sess-current"}},
		{"id":"sess-old","can_revoke":true}
	]}`))
	require.NoError(t, err)
	require.True(t, result.CurrentKnown)
	require.True(t, result.Sessions[0].Current)
	require.False(t, result.Sessions[0].CanRevoke)
	require.True(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsMatchesCurrentDeviceIdentifiersAcrossNamespaces(t *testing.T) {
	for _, test := range []struct {
		name    string
		payload string
	}{
		{
			name: "acronym current device id matches render id",
			payload: `{"currentDeviceID":"render-current","sessions":[
				{"id":"render-current","session_id":"sess-current"},
				{"id":"render-old","session_id":"sess-old","can_revoke":true}
			]}`,
		},
		{
			name: "current device session id matches nested device id",
			payload: `{"currentDeviceSessionID":"hashed-current","sessions":[
				{"session_id":"sess-current","device":{"hashed_device_id":"hashed-current"}},
				{"session_id":"sess-old","device":{"hashed_device_id":"hashed-old"},"can_revoke":true}
			]}`,
		},
		{
			name: "current device descriptor render id",
			payload: `{"currentDevice":{"render_id":"render-current"},"sessions":[
				{"session_id":"sess-current","device":{"render_id":"render-current"}},
				{"session_id":"sess-old","device":{"render_id":"render-old"},"can_revoke":true}
			]}`,
		},
		{
			name: "acronym descriptor id object",
			payload: `{"currentDeviceSessionID":{"id":"render-current"},"sessions":[
				{"session_id":"sess-current","device":{"render_id":"render-current"}},
				{"session_id":"sess-old","device":{"render_id":"render-old"},"can_revoke":true}
			]}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := decodeOpenAIAccountSessions([]byte(test.payload))
			require.NoError(t, err)
			require.True(t, result.CurrentKnown)
			require.Len(t, result.Sessions, 2)
			require.True(t, result.Sessions[0].Current)
			require.False(t, result.Sessions[0].CanRevoke)
			require.False(t, result.Sessions[1].Current)
			require.True(t, result.Sessions[1].CanRevoke)
		})
	}
}

func TestDecodeOpenAIAccountSessionsPositiveCurrentMarkerWinsConflict(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[
		{"id":"sess-current","current":false,"is_current":true},
		{"id":"sess-old","can_revoke":true}
	]}`))

	require.NoError(t, err)
	require.True(t, result.CurrentKnown)
	require.True(t, result.Sessions[0].Current)
	require.False(t, result.Sessions[0].CanRevoke)
	require.True(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsMalformedCapabilityFailsClosedAcrossAliases(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[
		{"id":"sess-old","can_revoke":"malformed","canLogout":true},
		{"id":"sess-available","status_available":"malformed","statusAvailable":true}
	]}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 2)
	require.False(t, result.Sessions[0].CanRevoke)
	require.False(t, result.Sessions[1].StatusAvailable)
	require.False(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsNestedDeviceCurrentDescriptorMarksRow(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[
		{"session_id":"sess-current","device":{"current_session":{"id":"render-current"},"render_id":"render-current"}},
		{"session_id":"sess-old","device":{"render_id":"render-old"},"can_revoke":true}
	]}`))

	require.NoError(t, err)
	require.True(t, result.CurrentKnown)
	require.True(t, result.Sessions[0].Current)
	require.False(t, result.Sessions[0].CanRevoke)
	require.True(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsMatchesNestedCurrentDeviceDescriptor(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[
		{"session_id":"sess-current","device":{"current_device":{"render_id":"render-current"},"render_id":"render-current"}},
		{"session_id":"sess-old","device":{"render_id":"render-old"},"can_revoke":true}
	],"current_device_id":"render-current"}`))

	require.NoError(t, err)
	require.True(t, result.CurrentKnown)
	require.True(t, result.Sessions[0].Current)
	require.False(t, result.Sessions[0].CanRevoke)
	require.True(t, result.Sessions[1].CanRevoke)
}

func TestSessionScalarStringSupportsCommonNumericTypes(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{value: int(-7), want: "-7"},
		{value: int8(8), want: "8"},
		{value: int16(16), want: "16"},
		{value: int32(32), want: "32"},
		{value: uint(9), want: "9"},
		{value: uint16(16), want: "16"},
		{value: uint64(64), want: "64"},
		{value: float32(1.5), want: "1.5"},
	}
	for _, test := range tests {
		require.Equal(t, test.want, sessionScalarString(test.value))
	}
	require.Empty(t, sessionScalarString(math.NaN()))
}

func TestParseSessionBoolAcceptsStrictNumericRepresentations(t *testing.T) {
	for _, value := range []any{"0", "1", json.Number("0"), json.Number("1"), uintptr(0), uintptr(1)} {
		parsed, ok := parseSessionBool(value)
		require.True(t, ok, "expected %v to parse", value)
		require.Equal(t, strings.HasSuffix(fmt.Sprint(value), "1"), parsed)
	}
	for _, value := range []any{"2", "yes", json.Number("2"), uintptr(2)} {
		_, ok := parseSessionBool(value)
		require.False(t, ok, "expected %v to remain malformed", value)
	}
}

func TestDecodeOpenAIAccountSessionsTracksCurrentMarkerPresence(t *testing.T) {
	tests := []struct {
		name         string
		payload      string
		current      bool
		currentKnown bool
	}{
		{
			name:         "missing marker is unknown",
			payload:      `{"sessions":[{"id":"sess-unknown"}]}`,
			currentKnown: false,
		},
		{
			name:         "explicit false marker is still known",
			payload:      `{"sessions":[{"id":"sess-other","current":false}]}`,
			currentKnown: true,
		},
		{
			name:         "nested device marker",
			payload:      `{"devices":[{"session_id":"sess-device","device":{"is_current_device":true}}]}`,
			current:      true,
			currentKnown: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := decodeOpenAIAccountSessions([]byte(tt.payload))
			require.NoError(t, err)
			require.Equal(t, tt.currentKnown, result.CurrentKnown)
			require.Len(t, result.Sessions, 1)
			require.Equal(t, tt.current, result.Sessions[0].Current)
		})
	}
}

func TestDecodeOpenAIAccountSessionsReadsNestedSessionIDAlias(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[
		{"device":{"session_id":"sess-current","is_current":true}},
		{"device":{"sessionId":"sess-old"},"can_revoke":true}
	]}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 2)
	require.Equal(t, "sess-current", result.Sessions[0].ID)
	require.True(t, result.Sessions[0].Current)
	require.Equal(t, "sess-old", result.Sessions[1].ID)
	require.True(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsPrefersExplicitSessionIDOverDeviceID(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{"current_session_id":"sess-current","sessions":[
		{"id":"device-render-id","session_id":"sess-current"},
		{"id":"device-render-old","session_id":"sess-old","can_revoke":true}
	]}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 2)
	require.Equal(t, "sess-current", result.Sessions[0].ID)
	require.True(t, result.Sessions[0].Current)
	require.False(t, result.Sessions[0].CanRevoke)
	require.Equal(t, "sess-old", result.Sessions[1].ID)
	require.True(t, result.Sessions[1].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsRecognizesRowCurrentSessionDescriptor(t *testing.T) {
	for _, row := range []string{
		`{"id":"sess-current","current_session":"sess-current"}`,
		`{"id":"sess-current","currentSession":{"session_id":"sess-current"}}`,
	} {
		result, err := decodeOpenAIAccountSessions([]byte(`{"sessions":[` + row + `,{"id":"sess-old","can_revoke":true}]}`))
		require.NoError(t, err)
		require.Len(t, result.Sessions, 2)
		require.True(t, result.CurrentKnown)
		require.True(t, result.Sessions[0].Current)
		require.False(t, result.Sessions[0].CanRevoke)
		require.True(t, result.Sessions[1].CanRevoke)
	}
}

func TestDecodeOpenAIAccountSessionsAcceptsCurrentDevicesContract(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"show_session_manager": true,
		"devices": [{
			"render_id": "us-render-id",
			"display_name": "Mac",
			"human_readable_description": "Mac",
			"platform": "macos",
			"os_version": "10.15.7",
			"device_model": "Mac",
			"is_trusted_device": false,
			"is_current_device": true,
			"can_untrust": false,
			"hashed_device_id": "hashed-device-id",
			"session_id": "us_session-test",
			"last_signed_in_timestamp_second": 1787891587,
			"last_signed_in_city": "Reus",
			"last_signed_in_region_code": "CT",
			"last_signed_in_country": "ES",
			"app_sessions": [
				{"client_name": "ChatGPT Web"},
				{"client_name": "Codex"}
			]
		}]
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	session := result.Sessions[0]
	require.Equal(t, "us_session-test", session.ID)
	require.Equal(t, "Mac", session.DeviceName)
	require.Equal(t, "macos", session.DeviceType)
	require.Equal(t, "macos 10.15.7", session.OS)
	require.Equal(t, "ChatGPT Web, Codex", session.AppName)
	require.Equal(t, "Reus, CT, ES", session.Location)
	require.Equal(t, "1787891587", session.SignedInAt)
	require.True(t, session.Current)
	require.False(t, session.Trusted)
	require.True(t, session.StatusAvailable)
	require.False(t, session.CanRevoke)
}

func TestDecodeOpenAIAccountSessionsExpandsNestedDeviceSessionsAndInheritsCurrentMarker(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"devices": [
			{
				"render_id": "render-current",
				"display_name": "Current Mac",
				"is_current_device": true,
				"sessions": [
					{"session_id": "sess-current-web", "client_name": "ChatGPT Web"},
					{"session_id": "sess-current-codex", "client_name": "Codex"}
				]
			},
			{
				"render_id": "render-old",
				"display_name": "Old Mac",
				"is_current_device": false,
				"app_sessions": [
					{"client_name": "ChatGPT Web"},
					{"session_id": "sess-old-codex", "client_name": "Codex"}
				]
			}
		]
	}`))

	require.NoError(t, err)
	// The two device rows remain visible, while concrete nested session rows
	// are appended.  Metadata-only app descriptors do not become rows.
	require.Len(t, result.Sessions, 5)
	require.True(t, result.CurrentKnown)

	byID := make(map[string]OpenAIAccountSession, len(result.Sessions))
	for _, session := range result.Sessions {
		byID[session.ID] = session
	}
	require.True(t, byID["sess-current-web"].Current)
	require.False(t, byID["sess-current-web"].CanRevoke)
	require.True(t, byID["sess-current-codex"].Current)
	require.False(t, byID["sess-current-codex"].CanRevoke)
	require.False(t, byID["sess-old-codex"].Current)
	require.True(t, byID["sess-old-codex"].CanRevoke)
	require.Equal(t, "ChatGPT Web", byID["sess-current-web"].AppName)
	require.Equal(t, "Codex", byID["sess-current-codex"].AppName)
}

func TestDecodeOpenAIAccountSessionsDoesNotTreatAppMetadataIDsAsSessionIDs(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"devices": [{
			"session_id": "device-session",
			"display_name": "Mac",
			"is_current_device": false,
			"app_sessions": [
				{"id": "chatgpt-installation-id", "client_name": "ChatGPT"},
				{"name": "Codex", "id": "codex-installation-id"}
			]
		}]
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	require.Equal(t, "device-session", result.Sessions[0].ID)
	require.NotEqual(t, "chatgpt-installation-id", result.Sessions[0].ID)
	require.NotEqual(t, "codex-installation-id", result.Sessions[0].ID)
}

func TestDecodeOpenAIAccountSessionsNestedWrappersAndCurrentSessionID(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"current_session_id": "nested-current",
		"devices": [{
			"render_id": "render-current",
			"sessions": {"data": [{"session_id": "nested-current"}]}
		}, {
			"render_id": "render-old",
			"sessions": {"payload": [{"session_id": "nested-old", "can_revoke": true}]}
		}]
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 4)
	byID := make(map[string]OpenAIAccountSession, len(result.Sessions))
	for _, session := range result.Sessions {
		if session.ID != "" {
			byID[session.ID] = session
		}
	}
	require.True(t, byID["nested-current"].Current)
	require.False(t, byID["nested-current"].CanRevoke)
	require.True(t, byID["nested-old"].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsProtectsParentTokenWhenNestedCurrentChildHasNoParentMarker(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"devices": [{
			"session_id": "current-device-token",
			"render_id": "current-render",
			"sessions": [{"session_id": "current-child", "current": true}]
		}, {
			"session_id": "old-device-token",
			"render_id": "old-render",
			"sessions": [{"session_id": "old-child", "current": false, "can_revoke": true}]
		}]
	}`))

	require.NoError(t, err)
	byID := make(map[string]OpenAIAccountSession, len(result.Sessions))
	for _, session := range result.Sessions {
		byID[session.ID] = session
	}
	// The current child protects both its own token and the canonical parent
	// device token.  Without this invariant cleanup could revoke the current
	// device through the parent row.
	require.True(t, byID["current-child"].Current)
	require.False(t, byID["current-child"].CanRevoke)
	require.True(t, byID["current-device-token"].Current)
	require.False(t, byID["current-device-token"].CanRevoke)
	require.True(t, byID["old-child"].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsProtectsParentForEnvelopeCurrentID(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"current_session_id": "current-child",
		"devices": [{
			"session_id": "current-device-token",
			"render_id": "current-render",
			"sessions": [{"session_id": "current-child"}]
		}, {
			"session_id": "old-device-token",
			"sessions": [{"session_id": "old-child", "can_revoke": true}]
		}]
	}`))

	require.NoError(t, err)
	byID := make(map[string]OpenAIAccountSession, len(result.Sessions))
	for _, session := range result.Sessions {
		byID[session.ID] = session
	}
	require.True(t, byID["current-child"].Current)
	require.False(t, byID["current-child"].CanRevoke)
	require.True(t, byID["current-device-token"].Current)
	require.False(t, byID["current-device-token"].CanRevoke)
	require.True(t, byID["old-child"].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsProtectsParentForNestedSelfCurrentDescriptor(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"devices": [{
			"session_id": "current-device-token",
			"sessions": [{"session_id": "current-child", "current_session_id": "current-child"}]
		}, {
			"session_id": "old-device-token",
			"sessions": [{"session_id": "old-child", "current": false, "can_revoke": true}]
		}]
	}`))

	require.NoError(t, err)
	byID := make(map[string]OpenAIAccountSession, len(result.Sessions))
	for _, session := range result.Sessions {
		byID[session.ID] = session
	}
	require.True(t, result.CurrentKnown)
	require.True(t, byID["current-child"].Current)
	require.False(t, byID["current-child"].CanRevoke)
	// The parent row carries a canonical revoke token for the same device.  A
	// child self-descriptor must protect it just like an explicit boolean marker.
	require.True(t, byID["current-device-token"].Current)
	require.False(t, byID["current-device-token"].CanRevoke)
	require.True(t, byID["old-child"].CanRevoke)
}

func TestDecodeOpenAIAccountSessionsDoesNotPromoteGenericNestedIDs(t *testing.T) {
	result, err := decodeOpenAIAccountSessions([]byte(`{
		"devices": [{
			"session_id": "device-session",
			"sessions": [{"id": "render-id", "current": false}],
			"app_sessions": [{"id": "app-id", "current": false}]
		}]
	}`))

	require.NoError(t, err)
	require.Len(t, result.Sessions, 1)
	require.Equal(t, "device-session", result.Sessions[0].ID)
}

func TestOpenAIAccountSessionServiceListsAndRevokes(t *testing.T) {
	account := &Account{
		ID:       71,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "acct-session-test",
		},
	}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "access-session-test"}}
	tokenProvider := NewOpenAITokenProvider(repo, cache, nil)

	var methods []string
	var paths []string
	var revokeBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		require.Equal(t, "Bearer access-session-test", r.Header.Get("authorization"))
		require.Equal(t, "acct-session-test", r.Header.Get("chatgpt-account-id"))
		require.Empty(t, r.Header.Get("cookie"), "Codex access tokens must not depend on a browser session cookie")
		w.Header().Set("content-type", "application/json")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"sessions":[{"id":"sess-a","device_name":"Firefox","can_revoke":true}]}`))
			return
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&revokeBody))
		_, _ = w.Write([]byte(`{"revoked_unified_sessions":1,"revoked_app_sessions":2}`))
	}))
	defer srv.Close()

	svc := NewOpenAIQuotaService(repo, nil, tokenProvider, newQuotaRedirectingFactory(srv))
	list, err := svc.ListSessions(context.Background(), account.ID)
	require.NoError(t, err)
	require.Len(t, list.Sessions, 1)
	require.Equal(t, "sess-a", list.Sessions[0].ID)
	require.NotZero(t, list.FetchedAt)

	err = svc.RevokeSession(context.Background(), account.ID, "us_session-test")
	require.NoError(t, err)
	require.Equal(t, []string{http.MethodGet, http.MethodPost}, methods)
	require.Equal(t, []string{"/backend-api/accounts/sessions", "/backend-api/accounts/sessions/revoke"}, paths)
	require.Equal(t, map[string]string{"session_id": "us_session-test"}, revokeBody)
}

func TestOpenAIAccountSessionServiceBatchRevokeReportsPartialSuccess(t *testing.T) {
	account := &Account{
		ID:       72,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"chatgpt_account_id": "acct-batch-test",
		},
	}
	repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{account.ID: account}}
	cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "access-batch-test"}}
	tokenProvider := NewOpenAITokenProvider(repo, cache, nil)

	var revokedIDs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		revokedIDs = append(revokedIDs, body["session_id"])
		w.Header().Set("content-type", "application/json")
		if body["session_id"] == "sess-b" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"revoked_unified_sessions":1}`))
	}))
	defer srv.Close()

	svc := NewOpenAIQuotaService(repo, nil, tokenProvider, newQuotaRedirectingFactory(srv))
	result, err := svc.RevokeSessions(context.Background(), account.ID, []string{"sess-a", "sess-b", "sess-a", " sess-c "})

	require.NoError(t, err)
	require.Equal(t, []string{"sess-a", "sess-b", "sess-c"}, revokedIDs)
	require.Equal(t, 3, result.RequestedCount)
	require.Equal(t, 2, result.SuccessCount)
	require.Equal(t, 1, result.FailedCount)
	require.Equal(t, []string{"sess-a", "sess-c"}, result.RevokedSessionIDs)
	require.Equal(t, []OpenAIAccountSessionRevokeFailure{{SessionID: "sess-b", Code: "OPENAI_SESSION_NOT_FOUND"}}, result.Failures)
}

func TestNormalizeOpenAIAccountSessionIDsRejectsInvalidBatch(t *testing.T) {
	_, err := normalizeOpenAIAccountSessionIDs(nil)
	require.Equal(t, "OPENAI_SESSIONS_BATCH_EMPTY", infraerrors.Reason(err))

	tooMany := make([]string, maxOpenAIAccountSessionBatchSize+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("sess-%d", index)
	}
	_, err = normalizeOpenAIAccountSessionIDs(tooMany)
	require.Equal(t, "OPENAI_SESSIONS_BATCH_TOO_LARGE", infraerrors.Reason(err))
}

func TestDecodeOpenAIAccountSessionsRejectsMissingCollection(t *testing.T) {
	_, err := decodeOpenAIAccountSessions([]byte(`{"result":"ok"}`))
	require.ErrorContains(t, err, "sessions collection is missing")
}
