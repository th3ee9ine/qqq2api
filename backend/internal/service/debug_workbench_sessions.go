package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/http/httpguts"
)

const (
	debugWorkbenchSessionTTL        = 30 * time.Minute
	debugWorkbenchMaxSessions       = 1024
	debugWorkbenchMaxTurnStateBytes = 16 * 1024
)

var (
	ErrDebugSessionInvalidScope  = errors.New("debug session requires a valid owner and account")
	ErrDebugSessionInvalidAction = errors.New("invalid debug session action")
	ErrDebugSessionNotFound      = errors.New("debug session not found or expired")
	ErrDebugSessionBusy          = errors.New("debug session already has an in-flight request")
	ErrDebugSessionCapacity      = errors.New("debug session capacity reached")
)

// DebugWorkbenchSessionStore holds only local debugging context, never account
// credentials. Sessions are private to an administrator/account pair. It is
// deliberately process-local: a restart invalidates IDs rather than replaying
// opaque upstream state through another process or account.
type DebugWorkbenchSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*debugWorkbenchSession
	now      func() time.Time
}

type debugWorkbenchSession struct {
	ownerID   int64
	accountID int64
	view      DebugSessionView
	turnState string
	expiresAt time.Time
	lease     uint64
	inflight  bool
}

// DebugWorkbenchSessionLease is a single request's immutable context. Finishing
// an old lease is a no-op: it cannot release or overwrite a newer request.
type DebugWorkbenchSessionLease struct {
	store      *DebugWorkbenchSessionStore
	session    *debugWorkbenchSession
	generation uint64
	headers    http.Header
	view       DebugSessionView
	finished   bool
}

func NewDebugWorkbenchSessionStore() *DebugWorkbenchSessionStore {
	return &DebugWorkbenchSessionStore{
		sessions: make(map[string]*debugWorkbenchSession),
		now:      time.Now,
	}
}

// Acquire starts one request. new_turn without an ID creates the first turn;
// new_session may replace an idle session belonging to this same owner/account.
// continue_turn never creates a missing session or silently changes a turn.
func (s *DebugWorkbenchSessionStore) Acquire(ownerID, accountID int64, input DebugSessionInput) (*DebugWorkbenchSessionLease, error) {
	if ownerID <= 0 || accountID <= 0 || s == nil {
		return nil, ErrDebugSessionInvalidScope
	}
	action := strings.TrimSpace(input.Action)
	if action == "" {
		action = "new_turn"
	}
	if action != "new_session" && action != "new_turn" && action != "continue_turn" {
		return nil, ErrDebugSessionInvalidAction
	}
	id := strings.TrimSpace(input.ID)
	if id == "" && action == "continue_turn" {
		return nil, ErrDebugSessionNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.currentTimeLocked()
	s.cleanupLocked(now)
	if s.sessions == nil {
		s.sessions = make(map[string]*debugWorkbenchSession)
	}
	var session *debugWorkbenchSession
	if id != "" {
		session = s.sessions[id]
		// An unknown ID and an ID owned by someone else deliberately have the
		// same result so the store cannot be used to enumerate another user.
		if session == nil || session.ownerID != ownerID || session.accountID != accountID {
			return nil, ErrDebugSessionNotFound
		}
		if session.inflight {
			return nil, ErrDebugSessionBusy
		}
	}
	if session == nil || action == "new_session" {
		if session == nil && len(s.sessions) >= debugWorkbenchMaxSessions {
			return nil, ErrDebugSessionCapacity
		}
		replacement, err := newDebugWorkbenchSession(ownerID, accountID)
		if err != nil {
			return nil, err
		}
		// Allocate all random IDs before replacing the old session, so an
		// entropy-source failure leaves its existing context intact.
		if session != nil {
			delete(s.sessions, session.view.ID)
		}
		session = replacement
		s.sessions[session.view.ID] = session
	} else if action == "new_turn" {
		turnID, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("generate debug turn ID: %w", err)
		}
		session.view.TurnID = turnID.String()
		session.view.TurnIndex++
		session.turnState = ""
		session.view.TurnStateAvailable = false
	}
	session.lease++
	session.inflight = true
	session.expiresAt = now.Add(debugWorkbenchSessionTTL)
	return &DebugWorkbenchSessionLease{
		store: s, session: session, generation: session.lease,
		headers: debugWorkbenchSessionHeaders(session), view: session.view,
	}, nil
}

func newDebugWorkbenchSession(ownerID, accountID int64) (*debugWorkbenchSession, error) {
	var ids [5]string
	for i := range ids {
		id, err := uuid.NewRandom()
		if err != nil {
			return nil, fmt.Errorf("generate debug session ID: %w", err)
		}
		ids[i] = id.String()
	}
	return &debugWorkbenchSession{
		ownerID: ownerID, accountID: accountID,
		view: DebugSessionView{
			ID: ids[0], SessionID: ids[1], ThreadID: ids[2], TurnID: ids[3], WindowID: ids[4], TurnIndex: 1,
		},
	}, nil
}

func debugWorkbenchSessionHeaders(session *debugWorkbenchSession) http.Header {
	headers := make(http.Header)
	headers.Set("session_id", session.view.SessionID)
	headers.Set("conversation_id", session.view.SessionID)
	headers.Set("thread-id", session.view.ThreadID)
	headers.Set("turn-id", session.view.TurnID)
	headers.Set("x-codex-window-id", session.view.WindowID)
	if session.turnState != "" {
		headers.Set(openAICodexTurnStateHeader, session.turnState)
	}
	return headers
}

// Headers returns a copy so request builders may rewrite account-scoped IDs
// without mutating the store or another request's context.
func (l *DebugWorkbenchSessionLease) Headers() http.Header {
	if l == nil {
		return make(http.Header)
	}
	return l.headers.Clone()
}

func (l *DebugWorkbenchSessionLease) View() DebugSessionView {
	if l == nil || l.store == nil {
		return DebugSessionView{}
	}
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	return l.view
}

// Finish releases the request and accepts opaque state only from the actual
// successful upstream response. Failed requests retain the previous state.
// Duplicate/stale completion and completion after expiry never mutate storage.
// Callers should defer Finish("", false) to release a lease on every error path.
func (l *DebugWorkbenchSessionLease) Finish(turnState string, success bool) DebugSessionView {
	if l == nil || l.store == nil {
		return DebugSessionView{}
	}
	s := l.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.finished {
		return l.view
	}
	l.finished = true
	now := s.currentTimeLocked()
	session := s.sessions[l.view.ID]
	if session != l.session || session == nil || !session.inflight || session.lease != l.generation {
		return l.view
	}
	if !now.Before(session.expiresAt) {
		delete(s.sessions, session.view.ID)
		l.view.TurnStateAvailable = false
		return l.view
	}
	if success {
		// Do not cache a value that is too large or invalid for an HTTP header.
		// A successful response without a state explicitly clears an older one.
		if len(turnState) > debugWorkbenchMaxTurnStateBytes || !httpguts.ValidHeaderFieldValue(turnState) || strings.TrimSpace(turnState) == "" {
			turnState = ""
		}
		session.turnState = turnState
	}
	session.view.TurnStateAvailable = session.turnState != ""
	session.inflight = false
	session.expiresAt = now.Add(debugWorkbenchSessionTTL)
	l.view = session.view
	return l.view
}

func (s *DebugWorkbenchSessionStore) currentTimeLocked() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *DebugWorkbenchSessionStore) cleanupLocked(now time.Time) {
	for id, session := range s.sessions {
		if !now.Before(session.expiresAt) {
			delete(s.sessions, id)
		}
	}
}
