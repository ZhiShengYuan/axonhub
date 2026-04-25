package orchestrator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

// HedgeCoordinatorConfig holds configuration for the hedge coordinator.
type HedgeCoordinatorConfig struct {
	// HedgeTriggerDelay is the time to wait before launching secondary if primary hasn't produced first token.
	// Default is 12 seconds.
	HedgeTriggerDelay time.Duration
	// ObservationWindow is the fixed time window to observe both streams before selecting winner.
	// Default is 3 seconds.
	ObservationWindow time.Duration
	// IsProbingMode if true, launches both primary and secondary immediately at T=0.
	// If false, secondary is launched only if primary hasn't produced first token after HedgeTriggerDelay.
	IsProbingMode bool
	// HedgePairID is an identifier for linking winner/loser in persistence.
	HedgePairID string
	// TimeNow returns current time (injectable for testing).
	TimeNow func() time.Time
}

// DefaultHedgeCoordinatorConfig returns the default hedge coordinator configuration.
func DefaultHedgeCoordinatorConfig() HedgeCoordinatorConfig {
	return HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     false,
	}
}

// HedgeRaceResult holds the result of a hedge race.
type HedgeRaceResult struct {
	// WinnerIndex is 0 for primary, 1 for secondary.
	WinnerIndex int
	// WinnerBuffer contains the buffered events from the winner stream.
	WinnerBuffer []*StreamEvent
	// LoserIndex is 0 for primary, 1 for secondary.
	LoserIndex int
	// BothFailed is true if both streams failed before observation window ended.
	BothFailed bool
	// WinnerStream is the winning stream for passthrough streaming.
	// After result is returned, caller should use WinnerStream for streaming to client.
	WinnerStream streams.Stream[*StreamEvent]
	// PrimaryStream is the original primary stream (still active after observation window).
	PrimaryStream streams.Stream[*StreamEvent]
	// SecondaryStream is the original secondary stream (still active after observation window).
	SecondaryStream streams.Stream[*StreamEvent]
}

// StreamEvent is a thin wrapper around httpclient.StreamEvent for this package.
// It represents a single event in a stream.
type StreamEvent = httpclient.StreamEvent

// HedgeCoordinator manages the full race lifecycle for dual-stream hedging.
// It launches primary immediately, optionally launches secondary after delay,
// observes both streams for a fixed window, selects winner based on TPS,
// and provides buffered winner stream for passthrough.
type HedgeCoordinator struct {
	config HedgeCoordinatorConfig

	// Internal state
	mu sync.Mutex

	// Track which streams have produced first token
	primaryGotFirstToken   bool
	secondaryGotFirstToken bool

	// Observation window state
	observationStarted bool
	observationEnded   bool

	// Buffered events from each stream
	primaryBuffer   []*StreamEvent
	secondaryBuffer []*StreamEvent

	// Original streams - kept so we can return them for passthrough/shadow
	primaryStream   streams.Stream[*StreamEvent]
	secondaryStream streams.Stream[*StreamEvent]

	// Stream completion tracking
	primaryDone   bool
	secondaryDone bool

	// Stream errors
	primaryErr   error
	secondaryErr error

	// Winner selection
	winnerIndex    int
	winnerSelected bool

	// For cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// NewHedgeCoordinator creates a new HedgeCoordinator with the given config.
func NewHedgeCoordinator(config HedgeCoordinatorConfig) *HedgeCoordinator {
	if config.HedgeTriggerDelay <= 0 {
		config.HedgeTriggerDelay = 12 * time.Second
	}
	if config.ObservationWindow <= 0 {
		config.ObservationWindow = 3 * time.Second
	}
	if config.TimeNow == nil {
		config.TimeNow = time.Now
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &HedgeCoordinator{
		config:          config,
		primaryBuffer:   make([]*StreamEvent, 0),
		secondaryBuffer: make([]*StreamEvent, 0),
		ctx:             ctx,
		cancel:          cancel,
	}
}

// StartRace begins the hedge race with primary and secondary streams.
// It launches primary immediately. If IsProbingMode is true, secondary is also launched immediately.
// Otherwise, secondary is launched after HedgeTriggerDelay if primary hasn't produced first token.
// Returns when observation window ends and winner is selected.
// The caller should use result.WinnerStream to continue streaming to client.
func (hc *HedgeCoordinator) StartRace(
	ctx context.Context,
	primaryStream streams.Stream[*StreamEvent],
	secondaryStream streams.Stream[*StreamEvent],
) (*HedgeRaceResult, error) {
	hc.mu.Lock()
	hc.primaryStream = primaryStream
	hc.secondaryStream = secondaryStream
	hc.mu.Unlock()

	firstTokenCh := make(chan int, 1)
	primaryFirstTokenCh := make(chan struct{}, 1) // signals only for primary's first token (for trigger decision)
	observationTimerCh := make(chan struct{}, 1)
	primaryDoneCh := make(chan struct{}, 1)
	secondaryDoneCh := make(chan struct{}, 1)

	var wg sync.WaitGroup

	// Always start primary consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		hc.consumeStream(primaryStream, 0, firstTokenCh, primaryDoneCh, primaryFirstTokenCh)
	}()

	// Launch trigger-delay goroutine that decides when to launch secondary (non-probing mode only)
	// or launches immediately (probing mode)
	if hc.config.IsProbingMode {
		// Probing mode: launch secondary immediately
		wg.Add(1)
		go func() {
			defer wg.Done()
			hc.consumeStream(secondaryStream, 1, firstTokenCh, secondaryDoneCh, nil)
		}()
	} else {
		// Non-probing mode: launch secondary only after trigger delay if primary hasn't produced first token
		go func() {
			timer := time.NewTimer(hc.config.HedgeTriggerDelay)
			defer timer.Stop()
			select {
			case <-primaryFirstTokenCh:
				// Primary got first token before trigger delay - don't launch secondary
				return
			case <-timer.C:
				// Trigger delay expired without primary first token - launch secondary
				wg.Add(1)
				go func() {
					defer wg.Done()
					hc.consumeStream(secondaryStream, 1, firstTokenCh, secondaryDoneCh, nil)
				}()
				return
			case <-ctx.Done():
				return
			case <-hc.ctx.Done():
				return
			}
		}()
	}

	select {
	case streamIdx := <-firstTokenCh:
		hc.mu.Lock()
		if streamIdx == 0 {
			hc.primaryGotFirstToken = true
		} else {
			hc.secondaryGotFirstToken = true
		}
		hc.observationStarted = true
		hc.mu.Unlock()

		go func() {
			timer := time.NewTimer(hc.config.ObservationWindow)
			defer timer.Stop()
			select {
			case <-timer.C:
				observationTimerCh <- struct{}{}
			case <-ctx.Done():
			}
		}()
	case <-primaryDoneCh:
		drainChan(secondaryDoneCh)
		wg.Wait()
		hc.mu.Lock()
		bothProducedNoToken := !hc.primaryGotFirstToken && !hc.secondaryGotFirstToken
		hc.mu.Unlock()
		if bothProducedNoToken {
			return &HedgeRaceResult{BothFailed: true}, nil
		}
		return hc.determineWinner(), nil
	case <-secondaryDoneCh:
		drainChan(primaryDoneCh)
		wg.Wait()
		hc.mu.Lock()
		bothProducedNoToken := !hc.primaryGotFirstToken && !hc.secondaryGotFirstToken
		hc.mu.Unlock()
		if bothProducedNoToken {
			return &HedgeRaceResult{BothFailed: true}, nil
		}
		return hc.determineWinner(), nil
	case <-ctx.Done():
		hc.cancel()
		wg.Wait()
		return nil, ctx.Err()
	case <-hc.ctx.Done():
		wg.Wait()
		return nil, hc.ctx.Err()
	}

	select {
	case <-observationTimerCh:
	case <-primaryDoneCh:
		drainChan(secondaryDoneCh)
		wg.Wait()
		return hc.determineWinner(), nil
	case <-secondaryDoneCh:
		drainChan(primaryDoneCh)
		wg.Wait()
		return hc.determineWinner(), nil
	case <-ctx.Done():
		hc.cancel()
		wg.Wait()
		return nil, ctx.Err()
	case <-hc.ctx.Done():
		wg.Wait()
		return nil, hc.ctx.Err()
	}

	wg.Wait()

	return hc.determineWinner(), nil
}

func drainChan(ch <-chan struct{}) {
	select {
	case <-ch:
	default:
	}
}

// consumeStream consumes events from a stream and buffers them.
// It signals via firstTokenCh when any stream produces a first token.
// It signals via primaryFirstTokenCh (if non-nil) when the primary stream (streamIdx==0) produces its first token.
func (hc *HedgeCoordinator) consumeStream(
	stream streams.Stream[*StreamEvent],
	streamIdx int,
	firstTokenCh chan<- int,
	doneCh chan<- struct{},
	primaryFirstTokenCh chan<- struct{},
) {
	// Defensive nil check - stream should never be nil if called correctly
	if stream == nil {
		hc.mu.Lock()
		if streamIdx == 0 {
			hc.primaryDone = true
			hc.primaryErr = errors.New("nil primary stream")
		} else {
			hc.secondaryDone = true
			hc.secondaryErr = errors.New("nil secondary stream")
		}
		hc.mu.Unlock()
		select {
		case doneCh <- struct{}{}:
		default:
		}
		return
	}

	for {
		ok := stream.Next()
		if !ok {
			err := stream.Err()
			hc.mu.Lock()
			if streamIdx == 0 {
				hc.primaryDone = true
				hc.primaryErr = err
			} else {
				hc.secondaryDone = true
				hc.secondaryErr = err
			}
			hc.mu.Unlock()
			_ = stream.Close()
			select {
			case doneCh <- struct{}{}:
			default:
			}
			return
		}

		event := stream.Current()
		if event == nil {
			continue
		}

		hc.mu.Lock()
		sendFirstToken := false
		if streamIdx == 0 && !hc.primaryGotFirstToken {
			hc.primaryGotFirstToken = true
			sendFirstToken = true
		} else if streamIdx == 1 && !hc.secondaryGotFirstToken {
			hc.secondaryGotFirstToken = true
			sendFirstToken = true
		}

		if streamIdx == 0 {
			hc.primaryBuffer = append(hc.primaryBuffer, event)
		} else {
			hc.secondaryBuffer = append(hc.secondaryBuffer, event)
		}
		hc.mu.Unlock()

		if sendFirstToken {
			select {
			case firstTokenCh <- streamIdx:
			default:
			}
			if streamIdx == 0 && primaryFirstTokenCh != nil {
				select {
				case primaryFirstTokenCh <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (hc *HedgeCoordinator) determineWinner() *HedgeRaceResult {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.observationEnded = true

	primaryFailed := hc.primaryDone && hc.primaryErr != nil
	secondaryFailed := hc.secondaryDone && hc.secondaryErr != nil

	if primaryFailed && secondaryFailed {
		return &HedgeRaceResult{
			BothFailed:      true,
			PrimaryStream:   hc.primaryStream,
			SecondaryStream: hc.secondaryStream,
		}
	}

	if primaryFailed && !secondaryFailed {
		hc.winnerSelected = true
		hc.winnerIndex = 1
		return &HedgeRaceResult{
			WinnerIndex:    1,
			WinnerBuffer:   hc.copyBuffer(hc.secondaryBuffer),
			LoserIndex:     0,
			BothFailed:     false,
			WinnerStream:   hc.newPassthroughStream(hc.copyBuffer(hc.secondaryBuffer), hc.secondaryStream),
			PrimaryStream:  hc.primaryStream,
			SecondaryStream: hc.secondaryStream,
		}
	}

	if secondaryFailed && !primaryFailed {
		hc.winnerSelected = true
		hc.winnerIndex = 0
		return &HedgeRaceResult{
			WinnerIndex:    0,
			WinnerBuffer:   hc.copyBuffer(hc.primaryBuffer),
			LoserIndex:     1,
			BothFailed:     false,
			WinnerStream:   hc.newPassthroughStream(hc.copyBuffer(hc.primaryBuffer), hc.primaryStream),
			PrimaryStream:  hc.primaryStream,
			SecondaryStream: hc.secondaryStream,
		}
	}

	primaryTPS := hc.computeObservationTPSLocked(hc.primaryBuffer)
	secondaryTPS := hc.computeObservationTPSLocked(hc.secondaryBuffer)

	if primaryTPS >= secondaryTPS {
		hc.winnerSelected = true
		hc.winnerIndex = 0
		return &HedgeRaceResult{
			WinnerIndex:    0,
			WinnerBuffer:   hc.copyBuffer(hc.primaryBuffer),
			LoserIndex:     1,
			BothFailed:     false,
			WinnerStream:   hc.newPassthroughStream(hc.copyBuffer(hc.primaryBuffer), hc.primaryStream),
			PrimaryStream:  hc.primaryStream,
			SecondaryStream: hc.secondaryStream,
		}
	}

	hc.winnerSelected = true
	hc.winnerIndex = 1
	return &HedgeRaceResult{
		WinnerIndex:    1,
		WinnerBuffer:   hc.copyBuffer(hc.secondaryBuffer),
		LoserIndex:     0,
		BothFailed:     false,
		WinnerStream:   hc.newPassthroughStream(hc.copyBuffer(hc.secondaryBuffer), hc.secondaryStream),
		PrimaryStream:  hc.primaryStream,
		SecondaryStream: hc.secondaryStream,
	}
}

// computeObservationTPSLocked computes tokens per second during observation window.
// Caller must hold hc.mu.
func (hc *HedgeCoordinator) computeObservationTPSLocked(events []*StreamEvent) float64 {
	if len(events) == 0 {
		return 0
	}

	// Count actual content tokens (non-empty data events)
	tokenCount := 0
	for _, e := range events {
		if len(e.Data) > 0 {
			tokenCount++
		}
	}

	if tokenCount == 0 {
		return 0
	}

	// Use observation window duration for TPS calculation
	windowSeconds := hc.config.ObservationWindow.Seconds()
	if windowSeconds <= 0 {
		return 0
	}

	return float64(tokenCount) / windowSeconds
}

// computeObservationTPS computes TPS given events and window duration.
// Public method for testing.
func (hc *HedgeCoordinator) ComputeObservationTPS(events []*StreamEvent, windowDuration time.Duration) float64 {
	if len(events) == 0 || windowDuration <= 0 {
		return 0
	}

	tokenCount := 0
	for _, e := range events {
		if len(e.Data) > 0 {
			tokenCount++
		}
	}

	if tokenCount == 0 {
		return 0
	}

	return float64(tokenCount) / windowDuration.Seconds()
}

// copyBuffer creates a copy of the event buffer.
func (hc *HedgeCoordinator) copyBuffer(events []*StreamEvent) []*StreamEvent {
	if events == nil {
		return nil
	}
	result := make([]*StreamEvent, len(events))
	copy(result, events)
	return result
}

// passthroughStream implements streams.Stream for winner passthrough.
// It reads from the winner's buffer first, then continues with the original stream.
type passthroughStream struct {
	stream    streams.Stream[*StreamEvent]
	buffer    []*StreamEvent
	bufferIdx int
	closed    bool
	mu        sync.Mutex
}

func (hc *HedgeCoordinator) newPassthroughStream(buffer []*StreamEvent, stream streams.Stream[*StreamEvent]) streams.Stream[*StreamEvent] {
	return NewBufferedStreamReader(buffer, stream)
}

func (s *passthroughStream) Next() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bufferIdx < len(s.buffer) {
		return true
	}

	if s.stream != nil {
		return s.stream.Next()
	}

	return false
}

func (s *passthroughStream) Current() *StreamEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bufferIdx < len(s.buffer) {
		event := s.buffer[s.bufferIdx]
		s.bufferIdx++
		return event
	}

	if s.stream != nil {
		return s.stream.Current()
	}

	return nil
}

func (s *passthroughStream) Err() error {
	if s.stream != nil {
		return s.stream.Err()
	}
	return nil
}

func (s *passthroughStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	if s.stream != nil {
		return s.stream.Close()
	}
	return nil
}

// Cancel aborts the hedge race.
func (hc *HedgeCoordinator) Cancel() {
	hc.cancel()
}

// Err returns any error that occurred during the race.
func (hc *HedgeCoordinator) Err() error {
	hc.mu.Lock()
	defer hc.mu.Unlock()

	if hc.primaryErr != nil {
		return hc.primaryErr
	}
	if hc.secondaryErr != nil {
		return hc.secondaryErr
	}
	return nil
}

// IsObservationActive returns true if the observation window is currently active.
func (hc *HedgeCoordinator) IsObservationActive() bool {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	return hc.observationStarted && !hc.observationEnded
}

// GetPrimaryBuffer returns a copy of the primary stream's buffered events.
func (hc *HedgeCoordinator) GetPrimaryBuffer() []*StreamEvent {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	return hc.copyBuffer(hc.primaryBuffer)
}

// GetSecondaryBuffer returns a copy of the secondary stream's buffered events.
func (hc *HedgeCoordinator) GetSecondaryBuffer() []*StreamEvent {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	return hc.copyBuffer(hc.secondaryBuffer)
}

// Helper to check if event has content (is a data token)
func hasContent(event *StreamEvent) bool {
	return event != nil && len(event.Data) > 0
}

// CreateTestStreamEvent creates a StreamEvent for testing.
func CreateTestStreamEvent(eventType string, data []byte) *StreamEvent {
	return &StreamEvent{
		Type: eventType,
		Data: data,
	}
}

// MockStreamForTesting creates a mock stream from a slice of events for testing.
func MockStreamForTesting(events []*StreamEvent) streams.Stream[*StreamEvent] {
	return &hedgeTestStream{events: events, idx: 0}
}

type hedgeTestStream struct {
	events []*StreamEvent
	idx    int
	closed bool
}

func (s *hedgeTestStream) Next() bool {
	return s.idx < len(s.events)
}

func (s *hedgeTestStream) Current() *StreamEvent {
	if s.idx < len(s.events) {
		event := s.events[s.idx]
		s.idx++
		return event
	}
	return nil
}

func (s *hedgeTestStream) Err() error {
	return nil
}

func (s *hedgeTestStream) Close() error {
	s.closed = true
	return nil
}

// BufferedStreamReader reads from both buffers and the original stream.
// This is used for the passthrough phase after winner selection.
type BufferedStreamReader struct {
	mu         sync.Mutex
	buffer     []*StreamEvent
	bufferIdx  int
	stream     streams.Stream[*StreamEvent]
	flushed    bool
	closed     bool
}

func NewBufferedStreamReader(buffer []*StreamEvent, stream streams.Stream[*StreamEvent]) *BufferedStreamReader {
	return &BufferedStreamReader{
		buffer:    buffer,
		bufferIdx: 0,
		stream:    stream,
	}
}

// Next implements streams.Stream.
func (b *BufferedStreamReader) Next() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.bufferIdx < len(b.buffer) {
		return true
	}

	if b.stream == nil {
		return false
	}

	if !b.flushed {
		b.flushed = true
		return b.stream.Next()
	}

	return b.stream.Next()
}

// Current implements streams.Stream.
func (b *BufferedStreamReader) Current() *StreamEvent {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.bufferIdx < len(b.buffer) {
		event := b.buffer[b.bufferIdx]
		b.bufferIdx++
		return event
	}

	if b.stream != nil && b.flushed {
		return b.stream.Current()
	}

	return nil
}

// Err implements streams.Stream.
func (b *BufferedStreamReader) Err() error {
	if b.stream != nil {
		return b.stream.Err()
	}
	return nil
}

// Close implements streams.Stream.
func (b *BufferedStreamReader) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return nil
	}
	b.closed = true

	if b.stream != nil {
		return b.stream.Close()
	}
	return nil
}

// HedgeError represents an error in the hedge coordinator.
type HedgeError struct {
	Msg string
}

func (e *HedgeError) Error() string {
	return e.Msg
}

func (e *HedgeError) HedgeError() string {
	return e.Msg
}

var (
	// ErrBothStreamsFailed is returned when both streams fail before observation window ends.
	ErrBothStreamsFailed = &HedgeError{Msg: "both streams failed"}
	// ErrObservationTimeout is returned when observation window times out.
	ErrObservationTimeout = &HedgeError{Msg: "observation window timeout"}
	// ErrInvalidWinnerIndex is returned when winner index is invalid.
	ErrInvalidWinnerIndex = &HedgeError{Msg: "invalid winner index"}
)

// IsHedgeError checks if err is a HedgeError.
func IsHedgeError(err error) bool {
	var hedgeErr *HedgeError
	return errors.As(err, &hedgeErr)
}