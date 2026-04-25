package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/streams"
)

type mockLlmStream struct {
	chunks []*llm.Response
	index  int
	closed bool
}

func (s *mockLlmStream) Next() bool {
	return s.index < len(s.chunks)
}

func (s *mockLlmStream) Current() *llm.Response {
	if s.index < len(s.chunks) {
		item := s.chunks[s.index]
		s.index++
		return item
	}
	return nil
}

func (s *mockLlmStream) Err() error {
	return nil
}

func (s *mockLlmStream) Close() error {
	s.closed = true
	return nil
}

func TestStreamBuffering_DisabledByDefault(t *testing.T) {
	state := &PersistenceState{
		Perf:                  &biz.PerformanceRecord{},
		StreamBufferingConfig: DisabledStreamBufferingConfig(),
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	originalStream := &mockLlmStream{
		chunks: []*llm.Response{
			{ID: "1"},
			{ID: "2"},
		},
	}

	result, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	assert.Same(t, originalStream, result)
	assert.Equal(t, ReleaseNone, state.StreamReleaseState)
}

func TestStreamBuffering_Enabled_PassesThroughWhenNotStreaming(t *testing.T) {
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 16,
			TimerDuration:  3 * time.Second,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	originalStream := &mockLlmStream{
		chunks: []*llm.Response{
			{ID: "1"},
		},
	}

	result, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	assert.Same(t, originalStream, result)
}

func TestStreamBuffering_Enabled_WrapsStreamWithBuffering(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream: streamTrue,
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 3,
			TimerDuration:  100 * time.Millisecond,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	originalStream := &mockLlmStream{
		chunks: []*llm.Response{
			{ID: "1"},
			{ID: "2"},
			{ID: "3"},
		},
	}

	result, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	assert.NotSame(t, originalStream, result)
	assert.Equal(t, ReleaseBuffering, state.StreamReleaseState)
}

func TestStreamBuffering_TtftReadyCallback(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		perf       *biz.PerformanceRecord
		expectTrue bool
	}{
		{
			name:       "nil Perf returns false",
			perf:       nil,
			expectTrue: false,
		},
		{
			name:       "Perf with nil FirstTokenTime returns false",
			perf:       &biz.PerformanceRecord{},
			expectTrue: false,
		},
		{
			name: "Perf with set FirstTokenTime returns true",
			perf: &biz.PerformanceRecord{
				FirstTokenTime: &now,
			},
			expectTrue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &PersistenceState{
				Perf: tt.perf,
				StreamBufferingConfig: StreamBufferingConfig{
					Enabled:        true,
					ChunkThreshold: 16,
					TimerDuration:  3 * time.Second,
				},
			}
			outbound := &PersistentOutboundTransformer{state: state}
			middleware := &streamBuffering{outbound: outbound}

			originalStream := &mockLlmStream{chunks: []*llm.Response{{ID: "1"}}}

			_, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
			require.NoError(t, err)
		})
	}
}

func TestStreamBuffering_Name(t *testing.T) {
	outbound := &PersistentOutboundTransformer{
		state: &PersistenceState{
			StreamBufferingConfig: DisabledStreamBufferingConfig(),
		},
	}
	middleware := &streamBuffering{outbound: outbound}

	assert.Equal(t, "stream-buffer", middleware.Name())
}

func TestAnyStreamWrapper(t *testing.T) {
	chunks := []*llm.Response{
		{ID: "1"},
		{ID: "2"},
	}
	source := &mockLlmStream{chunks: chunks}
	wrapper := &anyStreamWrapper{source: source}

	require.True(t, wrapper.Next())
	assert.Equal(t, chunks[0], wrapper.Current())

	require.True(t, wrapper.Next())
	assert.Equal(t, chunks[1], wrapper.Current())

	assert.False(t, wrapper.Next())
	assert.Nil(t, wrapper.Current())
	assert.NoError(t, wrapper.Err())
	assert.NoError(t, wrapper.Close())
}

func TestLlmResponseStreamWrapper(t *testing.T) {
	chunks := []any{
		&llm.Response{ID: "1"},
		&llm.Response{ID: "2"},
	}
	source := streams.SliceStream(chunks)
	wrapper := &llmResponseStreamWrapper{source: source}

	require.True(t, wrapper.Next())
	assert.Equal(t, "1", wrapper.Current().ID)

	require.True(t, wrapper.Next())
	assert.Equal(t, "2", wrapper.Current().ID)

	assert.False(t, wrapper.Next())
	assert.NoError(t, wrapper.Err())
	assert.NoError(t, wrapper.Close())
}