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
	a.Extra = map[string]any{CodexTurnStateAutoExtraKey: token, CodexTurnStateAutoSetAtExtraKey: time.Now().UnixMilli(), CodexTurnStateAutoProbeAtExtraKey: time.Now().UnixMilli()}
	repo.mu.Lock()
	repo.accounts[a.ID].Extra = mergeMap(nil, a.Extra)
	repo.mu.Unlock()
}
func TestCodexTurnStateRecoveryRecognizesEnvelopeNotHTTPCodeOrStringLength(t *testing.T) {
	now := time.Now()
	normal, signal := recoveryTestToken(now, 10, 1), recoveryTestToken(now, 11, 2)
	require.Len(t, normal, 292)
	require.Len(t, signal, 312)
	require.False(t, codexTurnStateIs312(normal))
	require.True(t, codexTurnStateIs312(signal))
	require.True(t, codexTurnStateIs312(strings.TrimRight(signal, "=")))
	require.False(t, codexTurnStateIs312(strings.Repeat("x", 312)))
	require.False(t, codexTurnStateIs312("312"))
}
func TestCodexTurnStateRecoveryImmediatelyRevokesAndFetchesNew292(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	next := recoveryTestToken(now, 10, 2)
	signal := recoveryTestToken(now, 11, 3)
	seedRecoveryTestAccount(repo, a, old)
	settings := s.settingService.settingRepo.(*codexHeaderSettingRepoStub)
	settings.values[SettingKeyOpenAICodexTurnState] = old
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return turnStateResponse(next), nil
	}}
	// A global override and the one-hour TTL cannot hide the invalidation. The
	// persisted probe time is 'now', proving the first recovery bypasses cooldown.
	s.collectOpenAICodexTurnState(context.Background(), a, signal, old)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("recovery delayed by regular probe cooldown")
	}
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, old)
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Empty(t, h.Get(openAICodexTurnStateHeader))
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	for n := 0; n < 20; n++ {
		s.collectOpenAICodexTurnState(context.Background(), a, signal, old)
	}
	close(release)
	waitTurnStateAutoIdle(t, s)
	require.EqualValues(t, 1, calls.Load())
	h.Set(openAICodexTurnStateHeader, old)
	s.applyOpenAICodexTurnState(context.Background(), a, h, "gpt-5")
	require.Equal(t, next, h.Get(openAICodexTurnStateHeader), "old global/native state must not override recovery")
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Equal(t, next, codexTurnStateAutoToken(stored))
	info := CodexTurnStateAutoInfoForAccount(stored, time.Now())
	require.False(t, info.RecoveryPending)
	require.Positive(t, info.InvalidatedAtMS)
	require.Empty(t, info.LastError)
	// Another instance loading persisted revocation cannot resurrect the old
	// manual value or an old native continuation, even after recovery succeeds.
	second := &OpenAIGatewayService{settingService: s.settingService, accountRepo: repo}
	h.Set(openAICodexTurnStateHeader, old)
	second.applyOpenAICodexTurnState(context.Background(), stored, h, "gpt-5")
	require.Equal(t, next, h.Get(openAICodexTurnStateHeader))
	waitTurnStateAutoIdle(t, second)
	// Late duplicate signals do not invalidate the newly recovered value.
	s.collectOpenAICodexTurnState(context.Background(), a, signal)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, next, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
}
func TestCodexTurnStateRecoveryOnlyAcceptsDifferentFresh292(t *testing.T) {
	for _, kind := range []string{"same", "unpad-same", "opaque", "312", "expired", "future", "nine-blocks"} {
		t.Run(kind, func(t *testing.T) {
			s, repo, a := newTurnStateAutoService(t)
			now := time.Now()
			old := recoveryTestToken(now, 10, 1)
			signal := recoveryTestToken(now, 11, 2)
			seedRecoveryTestAccount(repo, a, old)
			candidate := old
			switch kind {
			case "unpad-same":
				candidate = strings.TrimRight(old, "=")
			case "opaque":
				candidate = "opaque-state"
			case "312":
				candidate = recoveryTestToken(now, 11, 3)
			case "expired":
				candidate = recoveryTestToken(now.Add(-2*time.Hour), 10, 3)
			case "future":
				candidate = recoveryTestToken(now.Add(2*time.Hour), 10, 3)
			case "nine-blocks":
				candidate = recoveryTestToken(now, 9, 3)
			}
			var calls atomic.Int32
			s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
				calls.Add(1)
				return turnStateResponse(candidate), nil
			}}
			s.collectOpenAICodexTurnState(context.Background(), a, signal)
			waitTurnStateAutoIdle(t, s)
			require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
			// Repeated signals and requests must not start a tight probe loop.
			for n := 0; n < 5; n++ {
				s.collectOpenAICodexTurnState(context.Background(), a, signal)
			}
			waitTurnStateAutoIdle(t, s)
			require.EqualValues(t, 1, calls.Load())
			stored, _ := repo.GetByID(context.Background(), a.ID)
			require.Empty(t, codexTurnStateAutoToken(stored))
			require.True(t, CodexTurnStateAutoInfoForAccount(stored, time.Now()).RecoveryPending)
			// In-flight/old account snapshots and normal collection cannot resurrect it.
			s.collectOpenAICodexTurnState(context.Background(), a, old)
			waitTurnStateAutoIdle(t, s)
			require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
		})
	}
}
func TestCodexTurnStateRecoveryOldProbeCannotUndoSignal(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	oldProbeResult := recoveryTestToken(now, 10, 1)
	next := recoveryTestToken(now, 10, 2)
	signal := recoveryTestToken(now, 11, 3)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.httpUpstream = &turnStateAutoUpstream{call: func(*http.Request, string, int64) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return turnStateResponse(oldProbeResult), nil
		}
		return turnStateResponse(next), nil
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
	require.EqualValues(t, 2, calls.Load(), "312 must schedule a new probe after discarding the older in-flight probe")
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.Equal(t, next, codexTurnStateAutoToken(stored))
}
func TestCodexTurnStateRecoveryHTTPObservationPrecedesSSECommitAndRejectsOldResponses(t *testing.T) {
	s, repo, a := newTurnStateAutoService(t)
	now := time.Now()
	old := recoveryTestToken(now.Add(-time.Minute), 10, 1)
	signal := recoveryTestToken(now, 11, 2)
	next := recoveryTestToken(now, 10, 3)
	seedRecoveryTestAccount(repo, a, old)
	oldReq := s.stampCodexTurnStateRequest(httptest.NewRequest(http.MethodPost, "/responses", nil), a)
	s.httpUpstream = &turnStateAutoUpstream{call: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
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
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"), "must revoke before parsing/committing SSE body")
	// This response started before revocation, even if its token has the same
	// public second timestamp as the new generation. It must not recover state.
	s.collectCodexTurnStateHTTP(context.Background(), a, next, oldReq)
	waitTurnStateAutoIdle(t, s)
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	// A newly started request may recover with a fresh 292.
	current := s.stampCodexTurnStateRequest(httptest.NewRequest(http.MethodPost, "/responses", nil), a)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	h := http.Header{}
	h.Set(openAICodexTurnStateHeader, next)
	var staged http.Header
	stageOpenAICodexTurnState(&staged, h)
	s.noteStagedOpenAICodexTurnStateCommitted(c, a, staged, current)
	waitTurnStateAutoIdle(t, s)
	require.Equal(t, next, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
}
func TestCodexTurnStateRecoveryWSConnectionsCannotContinueOldEpoch(t *testing.T) {
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
	require.Empty(t, after.Headers.Get(openAICodexTurnStateHeader))
	key := normalizeOpenAIWSHandshakeCompatibility(a, after.Headers, after.turnStateFingerprint, after.turnStateRecoveryEpoch)
	require.False(t, conn.matchesHandshakeCompatibility(key))
	require.False(t, conn.matchesContinuationHandshakeCompatibility(key), "pinned continuations must not ignore revocation")
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
	s.collectOpenAICodexTurnState(context.Background(), a, signal)
	waitTurnStateAutoIdle(t, s)
	stored, _ := repo.GetByID(context.Background(), a.ID)
	encoded, err := json.Marshal(CodexTurnStateAutoInfoForAccount(stored, time.Now()))
	require.NoError(t, err)
	require.NotContains(t, string(encoded), old)
	require.NotContains(t, string(encoded), signal)
	require.NotContains(t, string(encoded), codexTurnStateCanonicalDigest(old))
	require.NotContains(t, StripCodexTurnStateAutoExtra(stored.Extra), CodexTurnStateAutoRecoveryExtraKey)
	require.True(t, CodexTurnStateAutoInfoForAccount(stored, time.Now()).Due)
}

type recoverySignalDialer struct{ state string }

func (d *recoverySignalDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	return nil, 403, turnStateResponse(d.state).Header, errors.New("handshake failed")
}
func TestCodexTurnStateRecoveryFailedWSHandshakeStillRevokes(t *testing.T) {
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
	require.Empty(t, s.autoTurnStateForAccount(context.Background(), a, "gpt-5"))
	stored, _ := repo.GetByID(context.Background(), a.ID)
	require.True(t, CodexTurnStateAutoInfoForAccount(stored, time.Now()).RecoveryPending)
}
