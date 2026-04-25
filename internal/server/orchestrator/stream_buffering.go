package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

// withStreamBuffering creates a middleware that wraps the LLM stream with buffering logic.
// It must be registered AFTER withPerformanceRecording so that TTFT is recorded before
// the buffering timer starts.
func withStreamBuffering(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &streamBuffering{
		outbound: outbound,
	}
}

type streamBuffering struct {
	pipeline.DummyMiddleware
	outbound *PersistentOutboundTransformer
}

func (m *streamBuffering) Name() string {
	return "stream-buffer"
}

// OnOutboundLlmStream wraps the LLM stream with buffering when enabled.
func (m *streamBuffering) OnOutboundLlmStream(ctx context.Context, stream streams.Stream[*llm.Response]) (streams.Stream[*llm.Response], error) {
	enabled := m.outbound.state.StreamBufferingConfig.Enabled
	isStreaming := m.outbound.state.Perf != nil && m.outbound.state.Perf.Stream

	if !enabled || !isStreaming {
		return stream, nil
	}

	// ttftReady callback: checks if TTFT has been recorded by performance middleware.
	// GUARDRAIL (TTFT-before-timer): Timer in bufferedStream starts ONLY after this
	// returns true. This ensures the timer doesn't fire before meaningful data arrives,
	// preventing premature buffer flush when upstream is just starting to respond.
	ttftReady := func() bool {
		return m.outbound.state.Perf != nil && m.outbound.state.Perf.FirstTokenTime != nil
	}

	// Wrap Stream[*llm.Response] as Stream[any] for bufferedStream
	anyStream := &anyStreamWrapper{source: stream}

	// Convert orchestrator StreamBufferingConfig to streams.StreamBufferingConfig
	streamsConfig := streams.StreamBufferingConfig{
		Enabled:        m.outbound.state.StreamBufferingConfig.Enabled,
		ChunkThreshold: m.outbound.state.StreamBufferingConfig.ChunkThreshold,
		TimerDuration:  m.outbound.state.StreamBufferingConfig.TimerDuration,
	}

	// Wrap with buffering. The bufferedStream implements "pre-release only" buffering:
	// - Before first flush: buffers chunks up to ChunkThreshold or TimerDuration
	// - After first flush: passes chunks directly through (no more buffering)
	// GUARDRAIL (pre-release only): Once data is released to client (first flush),
	// the stream enters permanent passthrough mode and retry becomes forbidden.
	bufferedAnyStream := streams.NewBufferedStream(anyStream, streamsConfig, ttftReady)

	// Wrap back as Stream[*llm.Response]
	wrappedStream := &llmResponseStreamWrapper{source: bufferedAnyStream}

	// Mark that buffering has started - transitions state from ReleaseNone to ReleaseBuffering
	// This enables the CanRetryStream() gate in retry logic.
	m.outbound.state.MarkStreamBuffering()

	return wrappedStream, nil
}

// anyStreamWrapper wraps a Stream[*llm.Response] as Stream[any].
type anyStreamWrapper struct {
	source streams.Stream[*llm.Response]
}

func (s *anyStreamWrapper) Next() bool {
	return s.source.Next()
}

func (s *anyStreamWrapper) Current() any {
	return s.source.Current()
}

func (s *anyStreamWrapper) Err() error {
	return s.source.Err()
}

func (s *anyStreamWrapper) Close() error {
	return s.source.Close()
}

// llmResponseStreamWrapper wraps a Stream[any] as Stream[*llm.Response].
type llmResponseStreamWrapper struct {
	source streams.Stream[any]
}

func (s *llmResponseStreamWrapper) Next() bool {
	return s.source.Next()
}

func (s *llmResponseStreamWrapper) Current() *llm.Response {
	return s.source.Current().(*llm.Response)
}

func (s *llmResponseStreamWrapper) Err() error {
	return s.source.Err()
}

func (s *llmResponseStreamWrapper) Close() error {
	return s.source.Close()
}