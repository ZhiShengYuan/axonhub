package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionClosed   = errors.New("session is closed")
)

// SessionManager manages MCP sessions with thread-safe operations.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	onRemove func(sessionID string)
}

// NewSessionManager creates a new SessionManager instance.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (sm *SessionManager) SetOnRemoveCallback(cb func(sessionID string)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onRemove = cb
}

// generateSessionID generates a new UUID session ID.
func generateSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// CreateSession creates a new MCP session and returns it.
// The session ID is automatically generated as a UUID.
func (sm *SessionManager) CreateSession(channelID string, caps protocol.ServerCapabilities, protoVersion string) (*Session, error) {
	sessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	s := NewSession(sessionID, channelID, caps, protoVersion)
	sm.sessions[sessionID] = s
	return s, nil
}

// GetSession retrieves a session by its ID.
// Returns ErrSessionNotFound if the session does not exist.
func (sm *SessionManager) GetSession(sessionID string) (*Session, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	s, ok := sm.sessions[sessionID]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return s, nil
}

// RemoveSession removes a session by its ID.
// The session is marked as closed before removal.
func (sm *SessionManager) RemoveSession(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.sessions[sessionID]
	if !ok {
		return ErrSessionNotFound
	}
	s.Close()
	if sm.onRemove != nil {
		sm.onRemove(sessionID)
	}
	delete(sm.sessions, sessionID)
	return nil
}

// TouchSession updates the LastPing timestamp of a session.
// Returns ErrSessionNotFound if the session does not exist.
func (sm *SessionManager) TouchSession(sessionID string) error {
	sm.mu.RLock()
	s, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}
	s.Touch()
	return nil
}

// ListSessions returns all active (non-closed) sessions.
func (sm *SessionManager) ListSessions() []*Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessions := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		if !s.IsClosed() {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

// CleanupExpired removes sessions that have been idle beyond maxIdle duration.
// Returns the count of removed sessions.
func (sm *SessionManager) CleanupExpired(maxIdle time.Duration) int {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cutoff := time.Now().Add(-maxIdle)
	count := 0

	for id, s := range sm.sessions {
		s.mu.RLock()
		isClosed := s.closed
		lastPing := s.LastPing
		s.mu.RUnlock()

		if !isClosed && lastPing.Before(cutoff) {
			s.Close()
			if sm.onRemove != nil {
				sm.onRemove(id)
			}
			delete(sm.sessions, id)
			count++
		}
	}
	return count
}

// SessionCount returns the number of active (non-closed) sessions.
func (sm *SessionManager) SessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	count := 0
	for _, s := range sm.sessions {
		if !s.IsClosed() {
			count++
		}
	}
	return count
}
