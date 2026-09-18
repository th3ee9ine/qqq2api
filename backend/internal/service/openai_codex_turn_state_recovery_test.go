package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func recoveryTestToken(now time.Time, blocks int, marker byte) string {
	raw, _ := base64.RawURLEncoding.DecodeString(testGlobalTurnStateToken(now, blocks))
	raw[len(raw)-1] = marker
	return base64.URLEncoding.EncodeToString(raw)
}
func seedRecoveryTestAccount(repo *turnStateAutoRepo, a *Account, token string) {
	now := time.Now().UnixMilli()
	a.Extra = map[string]any{codexTurnStateModelExtraKey("gpt-5"): map[string]any{
		CodexTurnStateAutoExtraKey: token, CodexTurnStateAutoSetAtExtraKey: now, CodexTurnStateAutoProbeAtExtraKey: now,
		CodexTurnStateAutoVerifiedAtExtraKey: now, CodexTurnStateAutoVerifiedModelExtraKey: "gpt-5",
	}}
	repo.mu.Lock()
	repo.accounts[a.ID].Extra = mergeMap(nil, a.Extra)
	repo.mu.Unlock()
}
func TestCodexTurnStateRecoveryRecognizesEnvelopeNotHTTPCodeOrStringLength(t *testing.T) {
	now := time.Now()
	normal, signal := recoveryTestToken(now, 10, 1), recoveryTestToken(now, 11, 2)
	teamNormal := recoveryTestToken(now, 12, 4)
	teamSignal := recoveryTestToken(now, 13, 3)
	require.Len(t, normal, 292)
	require.Len(t, signal, 312)
	require.Len(t, teamNormal, 332)
	require.Len(t, teamSignal, 356)
	require.False(t, codexTurnStateIs312(normal))
	require.True(t, codexTurnStateIs312(signal))
	require.True(t, codexTurnStateIs312(strings.TrimRight(signal, "=")))
	require.True(t, codexTurnStateIsNormal(teamNormal))
	require.False(t, codexTurnStateIsRecoverySignal(teamNormal))
	require.False(t, codexTurnStateIs312(teamSignal))
	require.True(t, codexTurnStateIs356(teamSignal))
	require.True(t, codexTurnStateIsRecoverySignal(teamSignal))
	require.True(t, codexTurnStateIsRecoverySignal(strings.TrimRight(teamSignal, "=")))
	require.False(t, codexTurnStateIs312(strings.Repeat("x", 312)))
	require.False(t, codexTurnStateIs312("312"))
}

func TestCodexTurnStateRecovery356HeaderOnlyDoesNotRevokeVerifiedState(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	teamSignal := recoveryTestToken(now, 13, 3)
	seedRecoveryTestAccount(repo, a, old)
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		calls.Add(1)
		return turnStateResponse("unexpected-probe"), nil
	}}

	s.collectOpenAICodexTurnState(context.Background(), a, teamSignal, old)
	waitTurnStateAutoIdle(t, s)

	require.Zero(t, calls.Load(), "a 356-byte header is diagnostic only and must not trigger a probe")
	stored, err := repo.GetByID(context.Background(), a.ID)
	require.NoError(t, err)
	require.Equal(t, old, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
	require.False(t, codexTurnStateRecoveryFromAccount(codexTurnStateModelAccount(stored, "gpt-5")).Pending)
	h := http.Header{}
	require.NoError(t, s.applyOpenAICodexTurnState(context.Background(), stored, h, "gpt-5"))
	require.Equal(t, old, h.Get(openAICodexTurnStateHeader))
}
func TestCodexTurnStateRecovery312HeaderOnlyPreservesNativeAndAutomaticState(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	signal := recoveryTestToken(now, 11, 3)
	seedRecoveryTestAccount(repo, a, old)
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		return turnStateResponse("unexpected-probe"), nil
	}}
	// A 312-byte header is not evidence of invalidity. It must not revoke the
	// local cache or cause a route-rotating maintenance request.
	s.collectOpenAICodexTurnState(context.Background(), a, signal, old)
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, old)
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, old, h.Get(openAICodexTurnStateHeader))
	waitTurnStateAutoIdle(t, s)
	require.Zero(t, calls.Load())
	require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Equal(t, old, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
	info := CodexTurnStateAutoInfoForAccount(stored, time.Now())
	require.False(t, info.RecoveryPending)
	require.Empty(t, info.LastError)
}
func TestCodexTurnStateRecoveryHeaderOnlyLengthsNeverReplaceVerifiedState(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name  string
		state string
	}{
		{"personal-292", recoveryTestToken(now, 10, 2)},
		{"personal-312", recoveryTestToken(now, 11, 2)},
		{"team-332", recoveryTestToken(now, 12, 2)},
		{"team-356", recoveryTestToken(now, 13, 2)},
		{"observed-376", strings.Repeat("x", 376)},
		{"opaque-arbitrary", "opaque-state"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, a := newTurnStateAutoService(t)
			old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
			seedRecoveryTestAccount(repo, a, old)

			for range 5 {
				s.collectOpenAICodexTurnState(context.Background(), a, tc.state, old)
			}
			waitTurnStateAutoIdle(t, s)

			require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
			stored, err := repo.GetByID(context.Background(), a.ID)
			require.NoError(t, err)
			require.Equal(t, old, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")))
			require.False(t, CodexTurnStateAutoInfoForAccount(stored, time.Now()).RecoveryPending)
			require.Zero(t, repo.writes, "header-only observations must not persist or schedule recovery work")
		})
	}
}
func TestCodexTurnStateRecoveryHeaderOnlySignalCannotSupersedeInflightVerifiedProbe(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	oldProbeResult := recoveryTestToken(now, 10, 1)
	signal := recoveryTestToken(now, 11, 3)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return turnStateResponse(oldProbeResult), nil
		}
		return turnStateResponse("unexpected-second-probe"), nil
	}}
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("probe did not start")
	}
	s.collectOpenAICodexTurnState(context.Background(), a, signal)
	close(release)
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 1, calls.Load(), "a header-only 312 must not restart or supersede a verified probe")
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Empty(t, codexTurnStateAutoToken(codexTurnStateModelAccount(stored, "gpt-5")), "a replayed maintenance result must not publish without a usage row")
	s.openaiTurnStateMu.Lock()
	entry := s.openaiTurnStates[codexTurnStateKey{a.ID, "gpt-5"}]
	require.NotNil(t, entry)
	require.Equal(t, oldProbeResult, entry.candidate.state, "the verified probe remains the pending candidate")
	require.NotEqual(t, signal, entry.candidate.state, "the header-only signal cannot supersede the in-flight probe")
	require.Empty(t, entry.candidate.requestID)
	s.openaiTurnStateMu.Unlock()
}
func TestCodexTurnStateRecoveryHTTPResponseEvidenceStillNeedsReplayBeforePublication(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	signal := recoveryTestToken(now, 11, 2)
	next := recoveryTestToken(now, 10, 3)
	seedRecoveryTestAccount(repo, a, old)
	oldReq := s.stampCodexTurnStateRequest(httptest.NewRequest(http.MethodPost, "/responses", nil), a)
	s.httpUpstream = &turnStateRawUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		if req.URL.Path == "/responses" {
			return turnStateResponse(signal), nil
		}
		return nil, errors.New("probe failed")
	}}
	req := httptest.NewRequest(http.MethodPost, "/responses", nil)
	req.Header.Set(openAICodexTurnStateHeader, old)
	resp, err := s.doOpenAIUpstream(req, "", a)
	require.NoError(t, err)
	defer resp.Body.Close()
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"), "a response header is not model evidence")
	// A header-only collection attempt, even with a newer-looking blob, cannot
	// replace the verified state.
	s.collectCodexTurnStateHTTP(context.Background(), a, next, oldReq)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	// Matching lifecycle evidence from an ordinary client response is still not
	// a same-route and daily-route replay, so it cannot publish the candidate.
	current := s.stampCodexTurnStateRequest(httptest.NewRequest(http.MethodPost, "/responses", nil), a)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	observer := beginUpstreamResponseModelObservation(c)
	observer.ObserveOpenAI([]byte(`{"type":"response.created","response":{"model":"gpt-5"}}`), "response.created")
	observer.ObserveOpenAI([]byte(`{"type":"response.completed","response":{"model":"gpt-5"}}`), "response.completed")
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, next)
	var staged http.Header
	stageOpenAICodexTurnState(&staged, h)
	s.noteStagedOpenAICodexTurnStateCommitted(c, a, staged, current)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
}
func TestCodexTurnStateRecoveryWSHeaderOnlyKeepsNativeContinuation(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	signal := recoveryTestToken(now, 11, 2)
	seedRecoveryTestAccount(repo, a, old)
	req := openAIWSAcquireRequest{Account: a, Headers: http.Header{}, TurnState: openAIWSTurnStatePolicy{Settings: s.settingService, Gateway: s, Models: []string{"gpt-5"}, NativeState: old}}
	before := req.withCurrentTurnState(context.Background())
	conn := newOpenAIWSConn("old", a.ID, &openAIWSFakeConn{}, nil)
	conn.handshakeCompatibility = normalizeOpenAIWSHandshakeCompatibility(a, before.Headers, before.turnStateFingerprint, before.turnStateRecoveryEpoch)
	s.collectOpenAICodexTurnState(context.Background(), a, signal, old)
	waitTurnStateAutoIdle(t, s)
	after := before.withCurrentTurnState(context.Background())
	require.Equal(t, old, after.Headers.Get(openAICodexTurnStateHeader), "a WS/header-only state remains an official native continuation")
	key := normalizeOpenAIWSHandshakeCompatibility(a, after.Headers, after.turnStateFingerprint, after.turnStateRecoveryEpoch)
	require.True(t, conn.matchesHandshakeCompatibility(key))
	require.True(t, conn.matchesContinuationHandshakeCompatibility(key), "without verified model evidence there is no recovery epoch to invalidate")
}
func TestCodexTurnStateRecoveryDisabledAndUnrelatedAccounts(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now, 10, 1)
	signal := recoveryTestToken(now, 11, 2)
	seedRecoveryTestAccount(repo, a, old)
	other := *a
	other.ID = 20
	repo.mu.Lock()
	repo.accounts[other.ID] = &other
	repo.mu.Unlock()
	s.collectOpenAICodexTurnState(context.Background(), a, signal)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), &other, "gpt-5"))
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnStateAutoEnabled] = "false"
	s.settingService.InvalidateOpenAICodexTurnStateCache()
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, old)
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, old, h.Get(openAICodexTurnStateHeader))
}
func TestCodexTurnStateRecoveryMetadataDoesNotExposeOrExportHashes(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now, 10, 1)
	signal := recoveryTestToken(now, 11, 2)
	seedRecoveryTestAccount(repo, a, old)
	// Simulate metadata left by an older length-based build. It is ignored.
	repo.accounts[a.ID].Extra[CodexTurnStateAutoRecoveryExtraKey] = map[string]any{
		"invalidated_at_ms": now.UnixMilli(), "pending": true,
	}
	s.collectOpenAICodexTurnState(context.Background(), a, signal)
	waitTurnStateAutoIdle(t, s)
	stored, _ := repo.GetByID(context.Background(), a.ID)
	encoded, err := json.Marshal(CodexTurnStateAutoInfoForAccount(stored, time.Now()))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), old)
	require.NotContains(t, string(encoded), signal)
	require.NotContains(t, string(encoded), codexTurnStateCanonicalDigest(old))
	require.NotContains(t, StripCodexTurnStateAutoExtra(stored.Extra), CodexTurnStateAutoRecoveryExtraKey)
	info := CodexTurnStateAutoInfoForAccount(stored, time.Now())
	require.False(t, info.RecoveryPending)
	require.False(t, info.Due)
	require.Equal(t, len(old), info.Models["gpt-5"].StateLength)
}

type recoverySignalDialer struct{ state string }

func (d *recoverySignalDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	return nil, 403, turnStateResponse(d.state).Header, errors.New("handshake failed")
}
func TestCodexTurnStateRecoveryFailedWSHandshakeDoesNotPromoteHeader(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	signal := recoveryTestToken(now, 11, 2)
	seedRecoveryTestAccount(repo, a, old)
	pool := newOpenAIWSConnPool(nil)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(&recoverySignalDialer{state: signal})
	_, err := pool.dialConn(context.Background(), openAIWSAcquireRequest{Account: a, Headers: http.Header{}, TurnState: openAIWSTurnStatePolicy{Settings: s.settingService, Gateway: s, Models: []string{"gpt-5"}, NativeState: old}})
	require.Error(t, err)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, old, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.False(t, CodexTurnStateAutoInfoForAccount(stored, time.Now()).RecoveryPending)
}
