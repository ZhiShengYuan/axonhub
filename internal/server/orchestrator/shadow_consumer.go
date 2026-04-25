package orchestrator

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/looplj/axonhub/llm/streams"
)

// ShadowCompletionReason represents the reason why shadow consumption ended.
type ShadowCompletionReason string

const (
	// ShadowCompletionNormal means the shadow stream completed normally (EOF or [DONE] sentinel).
	ShadowCompletionNormal ShadowCompletionReason = "normal_completion"
	// ShadowCompletionUpstreamError means the shadow stream encountered an upstream error.
	ShadowCompletionUpstreamError ShadowCompletionReason = "upstream_error"
	// ShadowCompletionClientDisconnected means the client disconnected before shadow completed.
	ShadowCompletionClientDisconnected ShadowCompletionReason = "client_disconnected"
	// ShadowCompletionDeadlineExceeded means the hard deadline was exceeded.
	ShadowCompletionDeadlineExceeded ShadowCompletionReason = "deadline_exceeded"
	// ShadowCompletionServerShutdown means the server was shutting down.
	ShadowCompletionServerShutdown ShadowCompletionReason = "server_shutdown"
)

// IsValid checks if the reason is a valid ShadowCompletionReason.
func (r ShadowCompletionReason) IsValid() bool {
	switch r {
	case ShadowCompletionNormal,
		ShadowCompletionUpstreamError,
		ShadowCompletionClientDisconnected,
		ShadowCompletionDeadlineExceeded,
		ShadowCompletionServerShutdown:
		return true
	}
	return false
}

// ShadowResult holds the result of shadow consumption.
type ShadowResult struct {
	// CompletionReason describes why shadow completed.
	CompletionReason ShadowCompletionReason
	// TotalTokensConsumed is the total number of tokens consumed from the loser stream.
	TotalTokensConsumed int
	// Duration is how long the shadow consumption took.
	Duration time.Duration
	// FullText is the accumulated full text from the loser stream.
	// This is only populated if FullTextRetentionEnabled was true in config.
	// If FullTextRetentionEnabled was false, this will be an empty string.
	FullText string
	// Error contains any error that occurred during shadow consumption.
	Error error
}

// ShadowConsumerConfig holds configuration for the ShadowConsumer.
type ShadowConsumerConfig struct {
	// ShadowDeadline is the hard deadline for shadow consumption.
	// Default is 30 minutes.
	ShadowDeadline time.Duration
	// FullTextRetentionEnabled controls whether full shadow response text is stored.
	// Default is false.
	FullTextRetentionEnabled bool
	// HedgePairID is an identifier for linking winner/loser in persistence.
	HedgePairID string
	// TimeNow returns current time (injectable for testing).
	TimeNow func() time.Time
}

// DefaultShadowConsumerConfig returns the default shadow consumer configuration.
func DefaultShadowConsumerConfig() ShadowConsumerConfig {
	return ShadowConsumerConfig{
		ShadowDeadline:          30 * time.Minute,
		FullTextRetentionEnabled: false,
		TimeNow:                 time.Now,
	}
}

// ShadowConsumer manages the shadow lifecycle of a loser stream.
// It detaches the loser from client delivery and continues consuming
// the loser stream in background until EOF, [DONE], explicit cancellation,
// hard deadline, or server shutdown.
type ShadowConsumer struct {
	config ShadowConsumerConfig

	// Internal state
	mu          sync.Mutex
	result      *ShadowResult
	isRunning   bool
	hasStarted  bool
	cancelCh    chan struct{}
}

// NewShadowConsumer creates a new ShadowConsumer with the given config.
func NewShadowConsumer(config ShadowConsumerConfig) *ShadowConsumer {
	if config.ShadowDeadline <= 0 {
		config.ShadowDeadline = 30 * time.Minute
	}
	if config.TimeNow == nil {
		config.TimeNow = time.Now
	}

	return &ShadowConsumer{
		config:   config,
		cancelCh: make(chan struct{}),
	}
}

// StartShadow begins consuming the loser stream in shadow mode.
// It detaches the loser from client delivery and continues consuming
// in a background goroutine until termination.
// Returns immediately with a result channel that will receive the final result.
//
// The shadow work outlives client disconnect using context.WithoutCancel,
// but is bounded by:
// - EOF from the loser stream (normal completion)
// - [DONE] sentinel in the stream
// - Hard deadline (config.ShadowDeadline)
// - Server shutdown (parentCtx cancellation)
// - Explicit Cancel() call
func (sc *ShadowConsumer) StartShadow(
	parentCtx context.Context,
	loserStream streams.Stream[*StreamEvent],
	hedgePairID string,
) (*ShadowResult, error) {
	sc.mu.Lock()
	if sc.hasStarted {
		sc.mu.Unlock()
		return nil, errors.New("shadow consumer has already been started")
	}
	sc.hasStarted = true
	if sc.isRunning {
		sc.mu.Unlock()
		return nil, errors.New("shadow consumer is already running")
	}
	sc.isRunning = true
	sc.cancelCh = make(chan struct{})
	sc.mu.Unlock()

	shadowCtx := context.WithoutCancel(parentCtx)
	shadowCtx, cancel := context.WithTimeout(shadowCtx, sc.config.ShadowDeadline)
	defer cancel()

	resultCh := make(chan *ShadowResult, 1)

	go func() {
		result := sc.consumeShadow(shadowCtx, loserStream, hedgePairID)
		resultCh <- result
	}()

	select {
	case result := <-resultCh:
		sc.mu.Lock()
		sc.result = result
		sc.isRunning = false
		sc.mu.Unlock()
		return result, nil
	case <-shadowCtx.Done():
		sc.mu.Lock()
		sc.result = &ShadowResult{
			CompletionReason: ShadowCompletionDeadlineExceeded,
		}
		sc.isRunning = false
		sc.mu.Unlock()
		return sc.result, nil
	}
}

// consumeShadow performs the actual shadow consumption loop.
func (sc *ShadowConsumer) consumeShadow(
	ctx context.Context,
	loserStream streams.Stream[*StreamEvent],
	hedgePairID string,
) *ShadowResult {
	// Defensive nil check - stream should never be nil if called correctly
	if loserStream == nil {
		return &ShadowResult{
			CompletionReason:   ShadowCompletionUpstreamError,
			TotalTokensConsumed: 0,
			Duration:           0,
			FullText:           "",
			Error:              errors.New("nil loser stream"),
		}
	}

	startTime := sc.config.TimeNow()

	var totalTokens int
	var fullText string

	for {
		// Capture cancel channel under mutex to avoid data race with Cancel()
		sc.mu.Lock()
		cancelCh := sc.cancelCh
		sc.mu.Unlock()

		nextCh := make(chan struct{})
		var ok bool
		go func() {
			ok = loserStream.Next()
			close(nextCh)
		}()

		select {
		case <-cancelCh:
			_ = loserStream.Close()
			return &ShadowResult{
				CompletionReason:   ShadowCompletionServerShutdown,
				TotalTokensConsumed: totalTokens,
				Duration:           sc.config.TimeNow().Sub(startTime),
				FullText:           fullTextIfEnabled(&sc.config, fullText),
			}
		case <-ctx.Done():
			_ = loserStream.Close()
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return &ShadowResult{
					CompletionReason:   ShadowCompletionDeadlineExceeded,
					TotalTokensConsumed: totalTokens,
					Duration:           sc.config.TimeNow().Sub(startTime),
					FullText:           fullTextIfEnabled(&sc.config, fullText),
				}
			}
			return &ShadowResult{
				CompletionReason:   ShadowCompletionServerShutdown,
				TotalTokensConsumed: totalTokens,
				Duration:           sc.config.TimeNow().Sub(startTime),
				FullText:           fullTextIfEnabled(&sc.config, fullText),
			}
		case <-nextCh:
		}

		if !ok {
			streamErr := loserStream.Err()
			if streamErr != nil {
				_ = loserStream.Close()
				return &ShadowResult{
					CompletionReason:   ShadowCompletionUpstreamError,
					TotalTokensConsumed: totalTokens,
					Duration:           sc.config.TimeNow().Sub(startTime),
					FullText:           fullTextIfEnabled(&sc.config, fullText),
					Error:              streamErr,
				}
			}

			_ = loserStream.Close()
			return &ShadowResult{
				CompletionReason:   ShadowCompletionNormal,
				TotalTokensConsumed: totalTokens,
				Duration:           sc.config.TimeNow().Sub(startTime),
				FullText:           fullTextIfEnabled(&sc.config, fullText),
			}
		}

		event := loserStream.Current()
		if event == nil {
			continue
		}

		isDone := event.Type == "done" || string(event.Data) == "[DONE]"
		if isDone {
			_ = loserStream.Close()
			return &ShadowResult{
				CompletionReason:   ShadowCompletionNormal,
				TotalTokensConsumed: totalTokens,
				Duration:           sc.config.TimeNow().Sub(startTime),
				FullText:           fullTextIfEnabled(&sc.config, fullText),
			}
		}

		if len(event.Data) > 0 {
			totalTokens++
			if sc.config.FullTextRetentionEnabled {
				fullText += string(event.Data)
			}
		}
	}
}

// Cancel explicitly cancels the shadow consumption with the given reason.
// This is typically called when the hedge coordinator is being torn down.
// If the reason is invalid, a default reason is used.
func (sc *ShadowConsumer) Cancel(reason ShadowCompletionReason) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if sc.result != nil {
		return
	}

	if !reason.IsValid() {
		reason = ShadowCompletionServerShutdown
	}

	sc.result = &ShadowResult{
		CompletionReason: reason,
	}

	if sc.cancelCh != nil {
		close(sc.cancelCh)
		sc.cancelCh = nil
	}
}

// IsRunning returns whether the shadow consumer is currently running.
func (sc *ShadowConsumer) IsRunning() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.isRunning
}

// Result returns the current result if available.
func (sc *ShadowConsumer) Result() *ShadowResult {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.result
}

// fullTextIfEnabled returns fullText only if retention is enabled, otherwise empty string.
func fullTextIfEnabled(config *ShadowConsumerConfig, fullText string) string {
	if config.FullTextRetentionEnabled {
		return fullText
	}
	return ""
}