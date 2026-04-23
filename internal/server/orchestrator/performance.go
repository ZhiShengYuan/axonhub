package orchestrator

import (
	"context"
	"errors"
	"time"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/metrics"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

// withPerformanceRecording creates a unified middleware that handles all performance tracking.
// It initializes metrics, tracks first token in streams, and records final metrics.
func withPerformanceRecording(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &performanceRecording{
		outbound: outbound,
	}
}

// performanceRecording is a unified middleware that handles all performance tracking.
type performanceRecording struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
}

func (m *performanceRecording) Name() string {
	return "record-performance"
}

func (m *performanceRecording) OnInboundLlmRequest(ctx context.Context, request *llm.Request) (*llm.Request, error) {
	if m.outbound.state.Perf == nil {
		m.outbound.state.Perf = &biz.PerformanceRecord{}
	}

	// Capture the raw requested model before any mapping/transformation.
	// This ensures metrics are labeled by what the user requested, not the actual model.
	m.outbound.state.RequestedModelRaw = request.Model

	if request.Stream != nil {
		m.outbound.state.Perf.Stream = *request.Stream
	} else {
		m.outbound.state.Perf.Stream = false
	}

	return request, nil
}

func (m *performanceRecording) OnOutboundRawRequest(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	// Initialize performance metrics at the start of request
	channel := m.outbound.GetCurrentChannel()
	if channel == nil {
		return request, nil
	}

	// Preserve Stream flag from existing PerformanceRecord (set in OnInboundLlmRequest)
	var streamFlag bool
	if m.outbound.state.Perf != nil {
		streamFlag = m.outbound.state.Perf.Stream
	}

	// Create a new PerformanceRecord instance for each request.
	perf := biz.PerformanceRecord{}
	perf.StartTime = time.Now()
	perf.ChannelID = channel.ID
	perf.Success = false
	perf.RequestCompleted = false
	perf.Stream = streamFlag

	// Get the API key used for this request from context (set by TraceStickyKeyProvider)
	if apiKey, ok := contexts.GetChannelAPIKey(ctx); ok {
		perf.APIKey = apiKey
	}

	// Get IncludeTTFTInSpeed from system general settings
	if m.outbound.state.ChannelService != nil && m.outbound.state.ChannelService.SystemService != nil {
		settings := m.outbound.state.ChannelService.SystemService.GeneralSettingsOrDefault(ctx)
		perf.IncludeTTFTInSpeed = settings.IncludeTTFTInSpeed
	}

	m.outbound.state.Perf = &perf

	log.Debug(ctx, "Started performance tracking",
		log.Int("channel_id", channel.ID),
		log.String("channel_name", channel.Name),
	)

	return request, nil
}

func (m *performanceRecording) OnOutboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	return response, nil
}

func (m *performanceRecording) OnOutboundLlmResponse(ctx context.Context, response *llm.Response) (*llm.Response, error) {
	if m.outbound.state.Perf == nil {
		return response, nil
	}

	if response != nil && response.Usage != nil {
		if tokenCount := response.Usage.GetCompletionTokens(); tokenCount != nil && *tokenCount > 0 {
			m.outbound.state.Perf.CompletionTokens = *tokenCount
		}
	}

	m.outbound.state.Perf.MarkSuccess()
	m.outbound.state.ChannelService.AsyncRecordPerformance(ctx, m.outbound.state.Perf)

	channel := m.outbound.GetCurrentChannel()
	if channel != nil {
		metrics.RecordLLMRequestAsync(ctx, &metrics.RequestMetricsData{
			RequestedModel:   m.outbound.state.RequestedModelRaw,
			ChannelID:        channel.ID,
			ChannelName:      channel.Name,
			Stream:           m.outbound.state.Perf.Stream,
			Success:          true,
			Canceled:         false,
			CompletionTokens: m.outbound.state.Perf.CompletionTokens,
			StartTime:       m.outbound.state.Perf.StartTime,
			FirstTokenTime:  m.outbound.state.Perf.FirstTokenTime,
			EndTime:         m.outbound.state.Perf.EndTime,
			IncludeTTFTInSpeed: m.outbound.state.Perf.IncludeTTFTInSpeed,
		})
	}

	return response, nil
}

func (m *performanceRecording) OnOutboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	return stream, nil
}

func (m *performanceRecording) OnOutboundLlmStream(ctx context.Context, stream streams.Stream[*llm.Response]) (streams.Stream[*llm.Response], error) {
	return &recordPerformanceStream{
		ctx:    ctx,
		stream: stream,
		state:  m.outbound.state,
	}, nil
}

func (m *performanceRecording) OnOutboundRawError(ctx context.Context, err error) {
	if m.outbound.state.Perf == nil {
		return
	}

	perf := m.outbound.state.Perf
	if errors.Is(err, context.Canceled) {
		perf.MarkCanceled()
	} else {
		errorCode := ExtractErrorCode(err)
		perf.MarkFailed(errorCode)
	}

	m.outbound.state.ChannelService.AsyncRecordPerformance(ctx, perf)

	channel := m.outbound.GetCurrentChannel()
	if channel != nil {
		metrics.RecordLLMRequestAsync(ctx, &metrics.RequestMetricsData{
			RequestedModel:   m.outbound.state.RequestedModelRaw,
			ChannelID:        channel.ID,
			ChannelName:      channel.Name,
			Stream:           perf.Stream,
			Success:          false,
			Canceled:         perf.Canceled,
			CompletionTokens: perf.CompletionTokens,
			StartTime:       perf.StartTime,
			FirstTokenTime:  perf.FirstTokenTime,
			EndTime:         perf.EndTime,
			IncludeTTFTInSpeed: perf.IncludeTTFTInSpeed,
		})
	}
}

// recordPerformanceStream records performance metrics for a stream of responses.
//
//nolint:containedctx // ctx is used for logging.
type recordPerformanceStream struct {
	ctx    context.Context
	stream streams.Stream[*llm.Response]
	state  *PersistenceState

	firstTokenSet     bool
	reasoningStartSet bool
	reasoningEndSet   bool
	metricsRecorded   bool
}

func (s *recordPerformanceStream) Current() *llm.Response {
	event := s.stream.Current()
	if event == nil {
		return event
	}

	if !s.firstTokenSet && s.state.Perf != nil {
		s.state.Perf.MarkFirstToken()
		s.firstTokenSet = true
	}

	if s.state.Perf != nil && len(event.Choices) > 0 {
		delta := event.Choices[0].Delta
		if delta != nil {
			if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
				if !s.reasoningStartSet {
					s.state.Perf.MarkReasoningStart()
					s.reasoningStartSet = true
				}
			} else if (delta.Content.Content != nil && *delta.Content.Content != "") || len(delta.Content.MultipleContent) > 0 || len(delta.ToolCalls) > 0 {
				if s.reasoningStartSet && !s.reasoningEndSet {
					s.state.Perf.MarkReasoningEnd()
					s.reasoningEndSet = true
				}
			}
		}
	}

	if tokenCount := event.Usage.GetCompletionTokens(); tokenCount != nil && *tokenCount > 0 && !s.metricsRecorded {
		s.state.Perf.CompletionTokens = *tokenCount
		s.state.Perf.MarkSuccess()
		s.state.ChannelService.AsyncRecordPerformance(s.ctx, s.state.Perf)

		channel := s.state.CurrentCandidate.Channel
		if channel != nil {
			metrics.RecordLLMRequestAsync(s.ctx, &metrics.RequestMetricsData{
				RequestedModel:   s.state.RequestedModelRaw,
				ChannelID:        channel.ID,
				ChannelName:      channel.Name,
				Stream:           s.state.Perf.Stream,
				Success:          true,
				Canceled:         false,
				CompletionTokens: s.state.Perf.CompletionTokens,
				StartTime:       s.state.Perf.StartTime,
				FirstTokenTime:  s.state.Perf.FirstTokenTime,
				EndTime:         s.state.Perf.EndTime,
				IncludeTTFTInSpeed: s.state.Perf.IncludeTTFTInSpeed,
			})
			s.metricsRecorded = true
		}
	}

	return event
}

func (s *recordPerformanceStream) Next() bool {
	return s.stream.Next()
}

func (s *recordPerformanceStream) Close() error {
	if !s.metricsRecorded && s.firstTokenSet && s.state.Perf != nil {
		channel := s.state.CurrentCandidate.Channel
		if channel != nil {
			metrics.RecordLLMRequestAsync(s.ctx, &metrics.RequestMetricsData{
				RequestedModel:   s.state.RequestedModelRaw,
				ChannelID:        channel.ID,
				ChannelName:      channel.Name,
				Stream:           s.state.Perf.Stream,
				Success:          s.state.Perf.Success,
				Canceled:         s.state.Perf.Canceled,
				CompletionTokens: s.state.Perf.CompletionTokens,
				StartTime:       s.state.Perf.StartTime,
				FirstTokenTime:  s.state.Perf.FirstTokenTime,
				EndTime:         s.state.Perf.EndTime,
				IncludeTTFTInSpeed: s.state.Perf.IncludeTTFTInSpeed,
			})
		}
	}
	return s.stream.Close()
}

func (s *recordPerformanceStream) Err() error {
	return s.stream.Err()
}

// ExtractErrorCode extracts HTTP error code from error.
func ExtractErrorCode(err error) int {
	// Check if error is an HTTP error
	httpErr := &httpclient.Error{}
	if errors.As(err, &httpErr) {
		code := httpErr.StatusCode
		return code
	}

	// Default to 500
	return 500
}

type NoopPerformanceRecording struct {
	pipeline.DummyMiddleware
}

func (m *NoopPerformanceRecording) Name() string {
	return "noop-performance"
}
