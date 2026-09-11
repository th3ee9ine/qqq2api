package service

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDebugWorkbenchSessionLifecycle(t *testing.T) {
	store := NewDebugWorkbenchSessionStore()
	first, err := store.Acquire(1, 7, DebugSessionInput{Action: "new_session"})
	require.NoError(t, err)
	initial := first.View()
	require.Equal(t, 1, initial.TurnIndex)
	require.False(t, initial.TurnStateAvailable)
	seen := map[string]bool{}
	for _, value := range []string{initial.ID, initial.SessionID, initial.ThreadID, initial.TurnID, initial.WindowID} {
		id, err := uuid.Parse(value)
		require.NoError(t, err)
		require.Equal(t, uuid.Version(4), id.Version())
		require.False(t, seen[value])
		seen[value] = true
	}
	headers := first.Headers()
	require.Equal(t, initial.SessionID, headers.Get("session_id"))
	require.Equal(t, initial.SessionID, headers.Get("conversation_id"))
	require.Equal(t, initial.ThreadID, headers.Get("thread-id"))
	require.Equal(t, initial.TurnID, headers.Get("turn-id"))
	require.Equal(t, initial.WindowID, headers.Get("x-codex-window-id"))
	require.Empty(t, headers.Get(openAICodexTurnStateHeader))
	headers.Set("thread-id", "caller-mutation")
	require.Equal(t, initial.ThreadID, first.Headers().Get("thread-id"))

	finished := first.Finish("opaque-upstream-state", true)
	require.True(t, finished.TurnStateAvailable)
	encoded, err := json.Marshal(finished)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "opaque-upstream-state")

	continued, err := store.Acquire(1, 7, DebugSessionInput{ID: initial.ID, Action: "continue_turn"})
	require.NoError(t, err)
	require.Equal(t, finished, continued.View())
	require.Equal(t, "opaque-upstream-state", continued.Headers().Get(openAICodexTurnStateHeader))
	continued.Finish("failed-response-state", false)
	retried, err := store.Acquire(1, 7, DebugSessionInput{ID: initial.ID, Action: "continue_turn"})
	require.NoError(t, err)
	require.Equal(t, "opaque-upstream-state", retried.Headers().Get(openAICodexTurnStateHeader))
	retried.Finish("", false)

	next, err := store.Acquire(1, 7, DebugSessionInput{ID: initial.ID, Action: "new_turn"})
	require.NoError(t, err)
	nextView := next.View()
	require.Equal(t, initial.ID, nextView.ID)
	require.Equal(t, initial.SessionID, nextView.SessionID)
	require.Equal(t, initial.ThreadID, nextView.ThreadID)
	require.Equal(t, initial.WindowID, nextView.WindowID)
	require.NotEqual(t, initial.TurnID, nextView.TurnID)
	require.Equal(t, 2, nextView.TurnIndex)
	require.False(t, nextView.TurnStateAvailable)
	require.Empty(t, next.Headers().Get(openAICodexTurnStateHeader))
	next.Finish("", false)
	final, err := store.Acquire(1, 7, DebugSessionInput{ID: initial.ID, Action: "continue_turn"})
	require.NoError(t, err)
	require.Empty(t, final.Headers().Get(openAICodexTurnStateHeader), "a failed new turn does not restore the previous turn's state")
	final.Finish("", true)
}

func TestDebugWorkbenchSessionScopeAndActions(t *testing.T) {
	store := NewDebugWorkbenchSessionStore()
	for _, input := range []DebugSessionInput{{}, {Action: "new_turn"}, {Action: "new_session"}} {
		lease, err := store.Acquire(1, 2, input)
		require.NoError(t, err)
		require.Equal(t, 1, lease.View().TurnIndex)
		lease.Finish("", false)
	}
	_, err := store.Acquire(0, 2, DebugSessionInput{})
	require.ErrorIs(t, err, ErrDebugSessionInvalidScope)
	_, err = store.Acquire(1, 0, DebugSessionInput{})
	require.ErrorIs(t, err, ErrDebugSessionInvalidScope)
	_, err = store.Acquire(1, 2, DebugSessionInput{Action: "continue_turn"})
	require.ErrorIs(t, err, ErrDebugSessionNotFound)
	_, err = store.Acquire(1, 2, DebugSessionInput{Action: "invented-action"})
	require.ErrorIs(t, err, ErrDebugSessionInvalidAction)
	first, err := store.Acquire(1, 2, DebugSessionInput{})
	require.NoError(t, err)
	id := first.View().ID
	first.Finish("private-state", true)
	for _, action := range []string{"new_turn", "continue_turn", "new_session"} {
		for _, scope := range [][2]int64{{3, 2}, {1, 3}} {
			_, err := store.Acquire(scope[0], scope[1], DebugSessionInput{ID: id, Action: action})
			require.ErrorIs(t, err, ErrDebugSessionNotFound)
		}
		_, err := store.Acquire(1, 2, DebugSessionInput{ID: "unknown-id", Action: action})
		require.ErrorIs(t, err, ErrDebugSessionNotFound)
	}
	original, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
	require.NoError(t, err)
	require.Equal(t, "private-state", original.Headers().Get(openAICodexTurnStateHeader))
	original.Finish("", false)
}

func TestDebugWorkbenchSessionLeaseRejectsConcurrentAndStaleCompletion(t *testing.T) {
	store := NewDebugWorkbenchSessionStore()
	first, err := store.Acquire(1, 2, DebugSessionInput{})
	require.NoError(t, err)
	id := first.View().ID
	for _, action := range []string{"continue_turn", "new_turn", "new_session"} {
		_, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: action})
		require.ErrorIs(t, err, ErrDebugSessionBusy)
	}
	stale := *first
	first.Finish("first-state", true)
	second, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
	require.NoError(t, err)
	stale.Finish("stale-state", true)
	first.Finish("duplicate-state", true)
	_, err = store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
	require.ErrorIs(t, err, ErrDebugSessionBusy, "stale completion must not release a newer request")
	second.Finish("second-state", true)
	third, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
	require.NoError(t, err)
	require.Equal(t, "second-state", third.Headers().Get(openAICodexTurnStateHeader))
	third.Finish("", false)
}

func TestDebugWorkbenchSessionConcurrentAcquireHasOneWinner(t *testing.T) {
	store := NewDebugWorkbenchSessionStore()
	first, err := store.Acquire(1, 2, DebugSessionInput{})
	require.NoError(t, err)
	id := first.View().ID
	first.Finish("", true)
	var wg sync.WaitGroup
	winners := make(chan *DebugWorkbenchSessionLease, 32)
	errorsCh := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
			if err != nil {
				errorsCh <- err
				return
			}
			winners <- lease
		}()
	}
	wg.Wait()
	close(winners)
	close(errorsCh)
	require.Len(t, winners, 1)
	require.Len(t, errorsCh, 31)
	for err := range errorsCh {
		require.True(t, errors.Is(err, ErrDebugSessionBusy))
	}
	for lease := range winners {
		lease.Finish("", false)
	}
}

func TestDebugWorkbenchSessionExpiryAndLateFinish(t *testing.T) {
	store := NewDebugWorkbenchSessionStore()
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	first, err := store.Acquire(1, 2, DebugSessionInput{})
	require.NoError(t, err)
	id := first.View().ID
	first.Finish("old-state", true)
	now = now.Add(debugWorkbenchSessionTTL - time.Second)
	continued, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
	require.NoError(t, err)
	now = now.Add(debugWorkbenchSessionTTL)
	view := continued.Finish("late-state", true)
	require.False(t, view.TurnStateAvailable)
	_, err = store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
	require.ErrorIs(t, err, ErrDebugSessionNotFound)
	require.Empty(t, store.sessions)

	lease, err := store.Acquire(1, 2, DebugSessionInput{})
	require.NoError(t, err)
	lease.Finish("", true)
	now = now.Add(debugWorkbenchSessionTTL)
	_, err = store.Acquire(1, 2, DebugSessionInput{ID: lease.View().ID, Action: "new_turn"})
	require.ErrorIs(t, err, ErrDebugSessionNotFound)
	require.Empty(t, store.sessions)
}

func TestDebugWorkbenchSessionBoundedCapacityAndReplacement(t *testing.T) {
	store := NewDebugWorkbenchSessionStore()
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	var last *DebugWorkbenchSessionLease
	for i := 0; i < debugWorkbenchMaxSessions; i++ {
		lease, err := store.Acquire(1, 2, DebugSessionInput{})
		require.NoError(t, err)
		lease.Finish("", false)
		last = lease
	}
	require.Len(t, store.sessions, 1024)
	_, err := store.Acquire(1, 2, DebugSessionInput{})
	require.ErrorIs(t, err, ErrDebugSessionCapacity)
	previous := last.View()
	replacement, err := store.Acquire(1, 2, DebugSessionInput{ID: previous.ID, Action: "new_session"})
	require.NoError(t, err)
	require.NotEqual(t, previous.ID, replacement.View().ID)
	require.NotEqual(t, previous.SessionID, replacement.View().SessionID)
	require.NotEqual(t, previous.ThreadID, replacement.View().ThreadID)
	require.NotEqual(t, previous.WindowID, replacement.View().WindowID)
	require.Equal(t, 1, replacement.View().TurnIndex)
	require.Len(t, store.sessions, 1024)
	_, err = store.Acquire(1, 2, DebugSessionInput{ID: previous.ID, Action: "continue_turn"})
	require.ErrorIs(t, err, ErrDebugSessionNotFound)
	now = now.Add(debugWorkbenchSessionTTL)
	fresh, err := store.Acquire(1, 2, DebugSessionInput{})
	require.NoError(t, err)
	require.Len(t, store.sessions, 1)
	replacement.Finish("expired-inflight-state", true)
	require.Len(t, store.sessions, 1, "late completion must not resurrect an expired session")
	fresh.Finish("", false)
}

func TestDebugWorkbenchSessionOnlyCachesValidActualResponseState(t *testing.T) {
	for _, responseState := range []string{"", "  ", "invalid\r\nvalue", strings.Repeat("a", debugWorkbenchMaxTurnStateBytes+1)} {
		store := NewDebugWorkbenchSessionStore()
		first, err := store.Acquire(1, 2, DebugSessionInput{})
		require.NoError(t, err)
		id := first.View().ID
		first.Finish("first-state", true)
		next, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
		require.NoError(t, err)
		require.False(t, next.Finish(responseState, true).TurnStateAvailable)
		continued, err := store.Acquire(1, 2, DebugSessionInput{ID: id, Action: "continue_turn"})
		require.NoError(t, err)
		require.Empty(t, continued.Headers().Get(openAICodexTurnStateHeader))
		continued.Finish("", false)
	}
}

func TestDebugWorkbenchSessionMissingNativeHeaderDocumentation(t *testing.T) {
	details := buildOpenAITestHeaderDetails(nil)
	for _, name := range []string{"Accept-Language", "X-Codex-Parent-Thread-Id"} {
		found := false
		for _, detail := range details {
			if detail.Name == name {
				found = true
				require.Equal(t, "conditional", detail.Requirement)
				require.False(t, detail.DefaultIncluded)
				require.NotEmpty(t, detail.Purpose)
				require.NotEmpty(t, detail.Condition)
			}
		}
		require.True(t, found, "missing %s", name)
	}
}
