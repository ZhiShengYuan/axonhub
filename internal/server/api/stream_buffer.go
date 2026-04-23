package api

import (
	"sync"
	"time"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Default thresholds for StreamBuffer
const (
	DefaultMaxChunks       = 8
	DefaultTimeout         = 3 * time.Second
	DefaultOverflowLimit   = 64
)

// StreamBufferOptions configures a StreamBuffer.
type StreamBufferOptions struct {
	// Writer is the callback for writing committed events.
	Writer func(*httpclient.StreamEvent)

	// MaxChunks is the number of chunks that triggers a buffer release.
	// Defaults to DefaultMaxChunks (8).
	MaxChunks int

	// Timeout is the duration before automatic buffer release.
	// Defaults to DefaultTimeout (3 seconds).
	Timeout time.Duration

	// OverflowLimit is the maximum number of chunks before forced release.
	// Defaults to DefaultOverflowLimit (64).
	OverflowLimit int

	// OnRelease is an optional callback invoked when the buffer releases.
	OnRelease func()
}

// StreamBuffer buffers SSE events before the first downstream commit.
// It provides a gate that holds events until a release condition is met:
//   - Timeout elapsed
//   - MaxChunks accumulated
//   - Upstream stream completed (via SetUpstreamDone)
//   - OverflowLimit reached
//
// After the first Flush (commit), subsequent Append calls pass through directly
// to the writer.
//
// StreamBuffer is safe for concurrent use: timer goroutine, Append, Flush, Close,
// and SetUpstreamDone can all be called simultaneously.
type StreamBuffer struct {
	mu sync.Mutex

	// writer is the callback for writing committed events.
	writer func(*httpclient.StreamEvent)

	// opts holds the configuration thresholds.
	opts StreamBufferOptions

	// buffer holds events before commit.
	buffer []*httpclient.StreamEvent

	// committed indicates whether Flush has been called (downstream commit).
	committed bool

	// closed indicates whether Close has been called.
	closed bool

	// upstreamDone indicates whether SetUpstreamDone has been called.
	upstreamDone bool

	// timer is the release timer.
	timer *time.Timer

	// released tracks whether release has been triggered to prevent double-release.
	released bool

	// suppressRelease prevents event release on Close. Used when we need to discard
	// buffered events without emitting them (e.g., pre-commit error handling).
	suppressRelease bool
}

// NewStreamBuffer creates a new StreamBuffer with the given options.
// If opts.Writer is nil, no events will be written until a writer is set via Flush.
// If opts.MaxChunks is 0, DefaultMaxChunks is used.
// If opts.Timeout is 0, DefaultTimeout is used.
// If opts.OverflowLimit is 0, DefaultOverflowLimit is used.
func NewStreamBuffer(opts StreamBufferOptions) *StreamBuffer {
	if opts.MaxChunks <= 0 {
		opts.MaxChunks = DefaultMaxChunks
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.OverflowLimit <= 0 {
		opts.OverflowLimit = DefaultOverflowLimit
	}

	sb := &StreamBuffer{
		writer: opts.Writer,
		opts:   opts,
		buffer: make([]*httpclient.StreamEvent, 0, opts.OverflowLimit),
	}
	sb.timer = time.AfterFunc(opts.Timeout, sb.releaseByTimer)
	sb.timer.Stop()
	return sb
}

// Writer returns the current writer function.
func (sb *StreamBuffer) Writer() func(*httpclient.StreamEvent) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.writer
}

// SetWriter sets the writer function. Not thread-safe; should only be called
// before any events are appended or after Close.
func (sb *StreamBuffer) SetWriter(writer func(*httpclient.StreamEvent)) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.writer = writer
}

// Append adds an event to the buffer. If the buffer has already been committed,
// the event is passed directly to the writer. Returns false if the buffer is closed.
func (sb *StreamBuffer) Append(event *httpclient.StreamEvent) bool {
	if event == nil {
		return false
	}

	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.closed {
		return false
	}

	// Passthrough after commit
	if sb.committed {
		if sb.writer != nil {
			sb.writer(event)
		}
		return true
	}

	// Add to buffer
	sb.buffer = append(sb.buffer, event)

	// Check overflow guard - immediate flush
	if len(sb.buffer) >= sb.opts.OverflowLimit {
		sb.releaseLocked()
		return true
	}

	// Check chunk threshold
	if len(sb.buffer) >= sb.opts.MaxChunks {
		sb.releaseLocked()
		return true
	}

	// Start timer on first append if not already done
	if len(sb.buffer) == 1 && !sb.upstreamDone {
		sb.timer.Reset(sb.opts.Timeout)
	}

	return true
}

// Committed returns true if Flush has been called (downstream commit occurred).
func (sb *StreamBuffer) Committed() bool {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.committed
}

// Flush releases all buffered events to the writer and marks the buffer as committed.
// Subsequent Append calls will pass through directly.
// It is safe to call Flush multiple times; subsequent calls are no-ops.
func (sb *StreamBuffer) Flush() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.releaseLocked()
}

func (sb *StreamBuffer) SuppressRelease() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.suppressRelease = true
}

// Close finalizes the buffer: stops the timer, releases any remaining buffered events,
// and marks the buffer as committed. After Close, Append returns false and no further
// events are accepted.
func (sb *StreamBuffer) Close() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.closed {
		return
	}

	sb.closed = true
	sb.timer.Stop()

	if sb.suppressRelease {
		sb.buffer = nil
		return
	}

	if !sb.committed && len(sb.buffer) > 0 {
		sb.releaseLocked()
	} else if sb.committed {
		sb.buffer = nil
	}
}

// SetUpstreamDone signals that the upstream stream has completed.
// If not already committed, this triggers a release.
func (sb *StreamBuffer) SetUpstreamDone() {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	if sb.upstreamDone {
		return
	}

	sb.upstreamDone = true

	// If already committed, nothing to do
	if sb.committed {
		return
	}

	// Release buffered events
	sb.releaseLocked()
}

// releaseByTimer is called by the timer when it fires.
// It must not hold the mutex when called.
func (sb *StreamBuffer) releaseByTimer() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.releaseLocked()
}

// releaseLocked releases buffered events. Caller must hold the mutex.
func (sb *StreamBuffer) releaseLocked() {
	if sb.released {
		return
	}
	sb.released = true

	// Stop timer if running
	if sb.timer != nil {
		sb.timer.Stop()
	}

	// Mark as committed before writing
	sb.committed = true

	// Invoke OnRelease callback before writing events
	if sb.opts.OnRelease != nil {
		sb.opts.OnRelease()
	}

	// Write all buffered events
	for _, event := range sb.buffer {
		if sb.writer != nil {
			sb.writer(event)
		}
	}
	sb.buffer = nil
}