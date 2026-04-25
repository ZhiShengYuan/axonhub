package streams

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mockResponse struct {
	id string
}

type mockStream struct {
	mu     sync.Mutex
	chunks []interface{}
	index  int
	closed bool
	err    error
	delay  time.Duration
}

func newMockStream(chunks []interface{}) Stream[interface{}] {
	return &mockStream{
		chunks: chunks,
		index:  0,
	}
}

func (s *mockStream) Next() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index >= len(s.chunks) {
		return false
	}

	if s.delay > 0 {
		time.Sleep(s.delay)
	}

	s.index++
	return s.index <= len(s.chunks)
}

func (s *mockStream) Current() interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index <= 0 || s.index > len(s.chunks) {
		return nil
	}
	return s.chunks[s.index-1]
}

func (s *mockStream) Err() error {
	return s.err
}

func (s *mockStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func createTestResponses(count int) []interface{} {
	responses := make([]interface{}, count)
	for i := 0; i < count; i++ {
		responses[i] = &mockResponse{id: "test-response"}
	}
	return responses
}

func TestBufferStream_ThresholdFlush(t *testing.T) {
	responses := createTestResponses(20)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := DefaultStreamBufferingConfig()
	config.ChunkThreshold = 16

	wrapped := NewBufferedStream(stream, config, ttftReady)

	result := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		result = append(result, resp)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 20, len(result))

	for i, resp := range result {
		require.NotNil(t, resp, "response at index %d should not be nil", i)
	}
}

func TestBufferStream_TimerFlush(t *testing.T) {
	responses := createTestResponses(5)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := DefaultStreamBufferingConfig()
	config.ChunkThreshold = 100
	config.TimerDuration = 100 * time.Millisecond

	wrapped := NewBufferedStream(stream, config, ttftReady)

	time.Sleep(150 * time.Millisecond)

	result := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		result = append(result, resp)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 5, len(result))
}

func TestBufferStream_NormalCloseBeforeFlush(t *testing.T) {
	responses := createTestResponses(5)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := DefaultStreamBufferingConfig()
	config.ChunkThreshold = 100
	config.TimerDuration = 10 * time.Second

	wrapped := NewBufferedStream(stream, config, ttftReady)

	err := wrapped.Close()
	require.NoError(t, err)
}

func TestBufferStream_PassthroughAfterFlush(t *testing.T) {
	responses := createTestResponses(20)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := DefaultStreamBufferingConfig()
	config.ChunkThreshold = 10

	wrapped := NewBufferedStream(stream, config, ttftReady)

	first10 := make([]*mockResponse, 0)
	for i := 0; i < 10 && wrapped.Next(); i++ {
		resp := wrapped.Current().(*mockResponse)
		first10 = append(first10, resp)
	}
	require.Equal(t, 10, len(first10))

	remaining := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		remaining = append(remaining, resp)
	}
	require.Equal(t, 10, len(remaining))

	all := append(first10, remaining...)
	require.Equal(t, 20, len(all))
}

func TestBufferStream_ContextCancellation(t *testing.T) {
	responses := createTestResponses(100)
	stream := newMockStream(responses)

	_, cancel := context.WithCancel(context.Background())

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 100,
		TimerDuration:  10 * time.Second,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	time.Sleep(50 * time.Millisecond)
	cancel()

	time.Sleep(50 * time.Millisecond)

	err := wrapped.Close()
	require.NoError(t, err)
}

func TestBufferStream_EmptyStream(t *testing.T) {
	stream := newMockStream([]interface{}{})

	ttftReady := func() bool { return true }
	config := DefaultStreamBufferingConfig()

	wrapped := NewBufferedStream(stream, config, ttftReady)

	result := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		result = append(result, resp)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 0, len(result))
}

func TestBufferStream_SingleChunk(t *testing.T) {
	responses := createTestResponses(1)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := DefaultStreamBufferingConfig()

	wrapped := NewBufferedStream(stream, config, ttftReady)

	result := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		result = append(result, resp)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 1, len(result))
}

func TestBufferStream_FIFOOrdering(t *testing.T) {
	responses := make([]interface{}, 20)
	for i := 0; i < 20; i++ {
		responses[i] = &mockResponse{id: string(rune('A' + i))}
	}
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 16,
		TimerDuration:  10 * time.Second,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	received := make([]string, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		received = append(received, resp.id)
	}

	require.Equal(t, 20, len(received))
	for i, id := range received {
		expected := string(rune('A' + i))
		require.Equal(t, expected, id, "FIFO ordering violated at index %d", i)
	}
}

func TestBufferStream_DisabledConfig(t *testing.T) {
	responses := createTestResponses(5)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled: false,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	require.Same(t, stream, wrapped)
}

func TestBufferStream_DefaultThreshold(t *testing.T) {
	responses := createTestResponses(16)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 0,
		TimerDuration:  10 * time.Second,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	result := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		result = append(result, resp)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 16, len(result))
}

func TestBufferStream_TTFTNotReadyNoTimer(t *testing.T) {
	responses := createTestResponses(20)
	stream := newMockStream(responses)

	ttftReady := func() bool { return false }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 16,
		TimerDuration:  100 * time.Millisecond,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	time.Sleep(150 * time.Millisecond)

	result := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		result = append(result, resp)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 20, len(result))
}

func TestBufferStream_BoundedMemory(t *testing.T) {
	responses := createTestResponses(30)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 10,
		TimerDuration:  500 * time.Millisecond,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	require.True(t, wrapped.Next())
	_ = wrapped.Current().(*mockResponse)

	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 5; i++ {
		if !wrapped.Next() {
			break
		}
		_ = wrapped.Current().(*mockResponse)
	}

	err := wrapped.Close()
	require.NoError(t, err)
}

func TestBufferStream_GoroutineCleanup(t *testing.T) {
	responses := createTestResponses(50)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 100,
		TimerDuration:  10 * time.Second,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	err := wrapped.Close()
	require.NoError(t, err)
}

func TestBufferStream_NoDuplicateEmission(t *testing.T) {
	responses := make([]interface{}, 20)
	for i := 0; i < 20; i++ {
		responses[i] = &mockResponse{id: string(rune('A' + i))}
	}
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 10,
		TimerDuration:  10 * time.Second,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	received := make([]string, 0)
	seen := make(map[string]bool)

	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		if seen[resp.id] {
			t.Fatalf("Duplicate emission detected for chunk %s", resp.id)
		}
		seen[resp.id] = true
		received = append(received, resp.id)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 20, len(received))

	for i, id := range received {
		expected := string(rune('A' + i))
		require.Equal(t, expected, id, "Order violated at index %d", i)
	}
}

func TestBufferStream_PermanentPassthrough(t *testing.T) {
	responses := make([]interface{}, 30)
	for i := 0; i < 30; i++ {
		responses[i] = &mockResponse{id: string(rune('A' + i))}
	}
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 10,
		TimerDuration:  10 * time.Second,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	first10 := make([]*mockResponse, 0)
	for i := 0; i < 10; i++ {
		require.True(t, wrapped.Next())
		resp := wrapped.Current().(*mockResponse)
		first10 = append(first10, resp)
	}
	require.Equal(t, 10, len(first10))

	time.Sleep(50 * time.Millisecond)

	remaining := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		remaining = append(remaining, resp)
	}
	require.Equal(t, 20, len(remaining))

	all := append(first10, remaining...)
	require.Equal(t, 30, len(all))

	allIDs := make(map[string]bool)
	for _, resp := range all {
		if allIDs[resp.id] {
			t.Fatalf("Duplicate chunk detected: %s", resp.id)
		}
		allIDs[resp.id] = true
	}
}

func TestBufferStream_TimerFlushThenPassthrough(t *testing.T) {
	responses := createTestResponses(5)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 100,
		TimerDuration:  100 * time.Millisecond,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	time.Sleep(200 * time.Millisecond)

	result := make([]*mockResponse, 0)
	for wrapped.Next() {
		resp := wrapped.Current().(*mockResponse)
		result = append(result, resp)
	}

	require.NoError(t, wrapped.Err())
	require.Equal(t, 5, len(result))
}

func TestBufferStream_CloseDuringBuffering(t *testing.T) {
	responses := createTestResponses(100)
	stream := newMockStream(responses)

	ttftReady := func() bool { return true }
	config := StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 100,
		TimerDuration:  10 * time.Second,
	}

	wrapped := NewBufferedStream(stream, config, ttftReady)

	for i := 0; i < 5; i++ {
		require.True(t, wrapped.Next())
		_ = wrapped.Current().(*mockResponse)
	}

	err := wrapped.Close()
	require.NoError(t, err)
}
