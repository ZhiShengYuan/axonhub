package session

import (
	"sync"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
)

type Session struct {
	ID              string
	ChannelID       string
	Capabilities    protocol.ServerCapabilities
	CreatedAt       time.Time
	LastPing        time.Time
	ProtocolVersion string
	UpstreamBaseURL string
	UpstreamSessionID string
	isInitialized   bool

	mu        sync.RWMutex
	closed    bool
}

func NewSession(id, channelID string, caps protocol.ServerCapabilities, protoVersion string) *Session {
	now := time.Now()
	return &Session{
		ID:              id,
		ChannelID:       channelID,
		Capabilities:    caps,
		CreatedAt:       now,
		LastPing:        now,
		ProtocolVersion: protoVersion,
	}
}

func (s *Session) Touch() {
	s.mu.Lock()
	s.LastPing = time.Now()
	s.mu.Unlock()
}

func (s *Session) IsClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closed
}

func (s *Session) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func (s *Session) SetInitialized() {
	s.mu.Lock()
	s.isInitialized = true
	s.mu.Unlock()
}

func (s *Session) IsInitialized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isInitialized
}
