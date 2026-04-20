package metrics

import (
	"errors"
	"testing"
	"time"
)

func TestMetricsInitialization(t *testing.T) {
	m := NewMetrics()

	m.RecordInitialization("channel-1", 100*time.Millisecond)
	m.RecordInitialization("channel-2", 200*time.Millisecond)

	snapshot := m.Snapshot()
	if snapshot.TotalInitializations != 2 {
		t.Fatalf("expected 2 initializations, got %d", snapshot.TotalInitializations)
	}
}

func TestMetricsInvocation(t *testing.T) {
	m := NewMetrics()

	m.RecordInvocation("session-1", "channel-1", "tools/call", 50*time.Millisecond)
	m.RecordInvocation("session-2", "channel-1", "resources/read", 30*time.Millisecond)
	m.RecordInvocation("session-1", "channel-2", "prompts/get", 40*time.Millisecond)

	snapshot := m.Snapshot()
	if snapshot.TotalInvocations != 3 {
		t.Fatalf("expected 3 invocations, got %d", snapshot.TotalInvocations)
	}
}

func TestMetricsError(t *testing.T) {
	m := NewMetrics()

	m.RecordError("session-1", "channel-1", "tools/call", errors.New("upstream error"))
	m.RecordError("session-2", "channel-2", "resources/read", errors.New("connection refused"))

	snapshot := m.Snapshot()
	if snapshot.TotalErrors != 2 {
		t.Fatalf("expected 2 errors, got %d", snapshot.TotalErrors)
	}
}

func TestMetricsSnapshot(t *testing.T) {
	m := NewMetrics()

	m.IncrementActiveSessions()
	m.IncrementActiveSessions()
	m.IncrementActiveSessions()

	m.RecordInitialization("channel-1", 100*time.Millisecond)
	m.RecordInitialization("channel-2", 150*time.Millisecond)

	m.RecordInvocation("session-1", "channel-1", "tools/call", 50*time.Millisecond)

	m.RecordError("session-1", "channel-1", "tools/call", errors.New("test error"))

	m.RecordDiscoverySize(10, 5, 3)

	snapshot := m.Snapshot()

	if snapshot.ActiveSessions != 3 {
		t.Fatalf("expected 3 active sessions, got %d", snapshot.ActiveSessions)
	}

	if snapshot.TotalInitializations != 2 {
		t.Fatalf("expected 2 initializations, got %d", snapshot.TotalInitializations)
	}

	if snapshot.TotalInvocations != 1 {
		t.Fatalf("expected 1 invocation, got %d", snapshot.TotalInvocations)
	}

	if snapshot.TotalErrors != 1 {
		t.Fatalf("expected 1 error, got %d", snapshot.TotalErrors)
	}

	if snapshot.DiscoverySize != 18 {
		t.Fatalf("expected discovery size 18, got %d", snapshot.DiscoverySize)
	}
}

func TestMetricsActiveSessions(t *testing.T) {
	m := NewMetrics()

	m.IncrementActiveSessions()
	m.IncrementActiveSessions()
	m.DecrementActiveSessions()

	snapshot := m.Snapshot()
	if snapshot.ActiveSessions != 1 {
		t.Fatalf("expected 1 active session, got %d", snapshot.ActiveSessions)
	}
}

func TestMetricsDiscoverySize(t *testing.T) {
	m := NewMetrics()

	m.RecordDiscoverySize(5, 10, 15)
	snapshot := m.Snapshot()
	if snapshot.DiscoverySize != 30 {
		t.Fatalf("expected discovery size 30, got %d", snapshot.DiscoverySize)
	}

	m.RecordDiscoverySize(0, 0, 0)
	snapshot = m.Snapshot()
	if snapshot.DiscoverySize != 0 {
		t.Fatalf("expected discovery size 0, got %d", snapshot.DiscoverySize)
	}
}