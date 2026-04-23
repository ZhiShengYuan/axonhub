package api

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEvent(data string) *httpclient.StreamEvent {
	return &httpclient.StreamEvent{Type: "", Data: []byte(data)}
}

func TestStreamBuffer_HoldsEventsBelowThreshold(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	for i := 0; i < 3; i++ {
		ok := sb.Append(makeEvent("event"))
		require.True(t, ok)
	}

	assert.False(t, sb.Committed())
	assert.Len(t, written, 0)
}

func TestStreamBuffer_ReleasesOnChunkThreshold(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	for i := 0; i < 8; i++ {
		sb.Append(makeEvent("event"))
	}

	assert.True(t, sb.Committed())
	assert.Len(t, written, 8)
}

func TestStreamBuffer_ReleasesOnTimer(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       50 * time.Millisecond,
		OverflowLimit: 64,
	})

	sb.Append(makeEvent("event"))
	assert.False(t, sb.Committed())

	time.Sleep(100 * time.Millisecond)
	assert.True(t, sb.Committed())
	assert.Len(t, written, 1)
}

func TestStreamBuffer_ReleasesOnUpstreamEndSignal(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	sb.Append(makeEvent("event1"))
	sb.Append(makeEvent("event2"))
	assert.False(t, sb.Committed())

	sb.SetUpstreamDone()

	assert.True(t, sb.Committed())
	assert.Len(t, written, 2)
}

func TestStreamBuffer_ReleasesOnOverflowGuard(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	for i := 0; i < 64; i++ {
		sb.Append(makeEvent("event"))
	}

	assert.True(t, sb.Committed())
	assert.Len(t, written, 64)
}

func TestStreamBuffer_PassthroughAfterCommit(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	for i := 0; i < 8; i++ {
		sb.Append(makeEvent("event"))
	}

	assert.True(t, sb.Committed())
	assert.Len(t, written, 8)

	sb.Append(makeEvent("aftercommit"))
	assert.Len(t, written, 9)
	assert.Equal(t, "aftercommit", string(written[8].Data))
}

func TestStreamBuffer_CloseStopsTimerAndReleases(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	sb.Append(makeEvent("event1"))
	sb.Append(makeEvent("event2"))
	assert.False(t, sb.Committed())

	sb.Close()

	assert.True(t, sb.Committed())
	assert.Len(t, written, 2)
	assert.False(t, sb.Append(makeEvent("afterclose")))
}

func TestStreamBuffer_ConcurrentTimerAppendSafety(t *testing.T) {
	var written []*httpclient.StreamEvent
	var writeMu sync.Mutex
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			writeMu.Lock()
			written = append(written, e)
			writeMu.Unlock()
		},
		MaxChunks:     8,
		Timeout:       20 * time.Millisecond,
		OverflowLimit: 64,
	})

	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; !stop.Load() && i < 100; i++ {
			sb.Append(makeEvent("event"))
			time.Sleep(time.Microsecond)
		}
	}()

	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		stop.Store(true)
	}()

	wg.Wait()
	sb.Close()

	t.Logf("Total events written: %d", len(written))
}

func TestStreamBuffer_DoubleReleaseNoOp(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	for i := 0; i < 8; i++ {
		sb.Append(makeEvent("event"))
	}

	assert.True(t, sb.Committed())
	assert.Len(t, written, 8)

	sb.SetUpstreamDone()
	assert.Len(t, written, 8)

	sb.Flush()
	assert.Len(t, written, 8)
}

func TestStreamBuffer_CloseAfterAlreadyClosed(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	sb.Close()
	sb.Close()

	assert.False(t, sb.Append(makeEvent("afterclose")))
}

func TestStreamBuffer_SetUpstreamDoneIdempotent(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	sb.SetUpstreamDone()
	sb.SetUpstreamDone()

	assert.Len(t, written, 0)
}

func TestStreamBuffer_NilAppendReturnsFalse(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     8,
		Timeout:       3 * time.Second,
		OverflowLimit: 64,
	})

	assert.False(t, sb.Append(nil))
	assert.Len(t, written, 0)
}

func TestStreamBuffer_DefaultsApplied(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
	})

	assert.Equal(t, DefaultMaxChunks, sb.opts.MaxChunks)
	assert.Equal(t, DefaultTimeout, sb.opts.Timeout)
	assert.Equal(t, DefaultOverflowLimit, sb.opts.OverflowLimit)
}

func TestStreamBuffer_OnReleaseCallback(t *testing.T) {
	var written []*httpclient.StreamEvent
	var releaseCalled bool
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks: 8,
		Timeout:   3 * time.Second,
		OnRelease: func() {
			releaseCalled = true
		},
	})

	for i := 0; i < 8; i++ {
		sb.Append(makeEvent("event"))
	}

	assert.True(t, releaseCalled)
	assert.Len(t, written, 8)
}

func TestStreamBuffer_CustomThresholds(t *testing.T) {
	var written []*httpclient.StreamEvent
	sb := NewStreamBuffer(StreamBufferOptions{
		Writer: func(e *httpclient.StreamEvent) {
			written = append(written, e)
		},
		MaxChunks:     3,
		Timeout:       100 * time.Millisecond,
		OverflowLimit: 10,
	})

	for i := 0; i < 3; i++ {
		sb.Append(makeEvent("event"))
	}
	assert.True(t, sb.Committed())
	assert.Len(t, written, 3)

	sb.Append(makeEvent("aftercommit"))
	assert.Len(t, written, 4)
}