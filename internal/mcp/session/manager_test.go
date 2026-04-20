package session

import (
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
)

func TestCreateAndGetSession(t *testing.T) {
	sm := NewSessionManager()

	sess, err := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if sess.ID == "" {
		t.Error("session ID should not be empty")
	}
	if sess.ChannelID != "channel-1" {
		t.Errorf("expected ChannelID 'channel-1', got '%s'", sess.ChannelID)
	}
	if sess.ProtocolVersion != "2025-11-25" {
		t.Errorf("expected ProtocolVersion '2025-11-25', got '%s'", sess.ProtocolVersion)
	}

	retrieved, err := sm.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.ID != sess.ID {
		t.Errorf("expected session ID '%s', got '%s'", sess.ID, retrieved.ID)
	}
}

func TestSessionNotFound(t *testing.T) {
	sm := NewSessionManager()

	_, err := sm.GetSession("non-existent")
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestRemoveSession(t *testing.T) {
	sm := NewSessionManager()

	sess, err := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = sm.RemoveSession(sess.ID)
	if err != nil {
		t.Fatalf("RemoveSession failed: %v", err)
	}

	_, err = sm.GetSession(sess.ID)
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound after removal, got %v", err)
	}
}

func TestSessionAffinity(t *testing.T) {
	sm := NewSessionManager()

	sess, err := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if sess.ChannelID != "channel-1" {
		t.Errorf("expected ChannelID 'channel-1', got '%s'", sess.ChannelID)
	}
}

func TestCleanupExpired(t *testing.T) {
	sm := NewSessionManager()

	_, err := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	_, err = sm.CreateSession("channel-2", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	count := sm.CleanupExpired(time.Hour)
	if count != 0 {
		t.Errorf("expected 0 expired sessions, got %d", count)
	}

	if sm.SessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", sm.SessionCount())
	}
}

func TestConcurrentSessionAccess(t *testing.T) {
	sm := NewSessionManager()

	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				sess, _ := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, "2025-11-25")
				sm.GetSession(sess.ID)
				sm.TouchSession(sess.ID)
				sm.SessionCount()
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestTouchSession(t *testing.T) {
	sm := NewSessionManager()

	sess, err := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	originalPing := sess.LastPing

	time.Sleep(time.Millisecond)

	err = sm.TouchSession(sess.ID)
	if err != nil {
		t.Fatalf("TouchSession failed: %v", err)
	}

	sm.mu.RLock()
	updatedSess := sm.sessions[sess.ID]
	sm.mu.RUnlock()

	if !updatedSess.LastPing.After(originalPing) {
		t.Error("LastPing should have been updated")
	}
}

func TestSessionCount(t *testing.T) {
	sm := NewSessionManager()

	if sm.SessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", sm.SessionCount())
	}

	_, err := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	_, err = sm.CreateSession("channel-2", protocol.ServerCapabilities{}, "2025-11-25")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if sm.SessionCount() != 2 {
		t.Errorf("expected 2 sessions, got %d", sm.SessionCount())
	}
}
