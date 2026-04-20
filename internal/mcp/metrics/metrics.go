package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	activeSessions    atomic.Int64
	totalInitializations atomic.Int64
	totalInvocations  atomic.Int64
	totalErrors       atomic.Int64
	discoverySize     atomic.Int64

	mu          sync.RWMutex
	initLatencies   []time.Duration
	invokeLatencies  []time.Duration
}

type MetricsSnapshot struct {
	ActiveSessions     int64
	TotalInitializations int64
	TotalInvocations   int64
	TotalErrors        int64
	DiscoverySize      int64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) RecordInitialization(channelID string, latency time.Duration) {
	m.totalInitializations.Add(1)
	m.mu.Lock()
	m.initLatencies = append(m.initLatencies, latency)
	m.mu.Unlock()
}

func (m *Metrics) RecordInvocation(sessionID, channelID, method string, latency time.Duration) {
	m.totalInvocations.Add(1)
	m.mu.Lock()
	m.invokeLatencies = append(m.invokeLatencies, latency)
	m.mu.Unlock()
}

func (m *Metrics) RecordError(sessionID, channelID, method string, err error) {
	m.totalErrors.Add(1)
}

func (m *Metrics) RecordDiscoverySize(tools, resources, prompts int) {
	size := int64(tools + resources + prompts)
	m.discoverySize.Store(size)
}

func (m *Metrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		ActiveSessions:       m.activeSessions.Load(),
		TotalInitializations: m.totalInitializations.Load(),
		TotalInvocations:    m.totalInvocations.Load(),
		TotalErrors:         m.totalErrors.Load(),
		DiscoverySize:       m.discoverySize.Load(),
	}
}

func (m *Metrics) IncrementActiveSessions() {
	m.activeSessions.Add(1)
}

func (m *Metrics) DecrementActiveSessions() {
	m.activeSessions.Add(-1)
}