package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShadowConsumer_NormalCompletion_EOF(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionNormal, result.CompletionReason)
	assert.Equal(t, 3, result.TotalTokensConsumed)
	assert.Empty(t, result.FullText)
	assert.Nil(t, result.Error)
	assert.Greater(t, result.Duration, 0*time.Second)
}

func TestShadowConsumer_NormalCompletion_DONESentinel(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "done", Data: []byte("[DONE]")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionNormal, result.CompletionReason)
	assert.Equal(t, 2, result.TotalTokensConsumed)
	assert.Empty(t, result.FullText)
}

func TestShadowConsumer_NormalCompletion_DONEType(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "done", Data: []byte("")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionNormal, result.CompletionReason)
	assert.Equal(t, 1, result.TotalTokensConsumed)
}

func TestShadowConsumer_UpstreamError(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	upstreamErr := errors.New("upstream connection reset")
	stream := &errorStream{err: upstreamErr}

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionUpstreamError, result.CompletionReason)
	assert.Equal(t, 0, result.TotalTokensConsumed)
	assert.Equal(t, upstreamErr, result.Error)
}

func TestShadowConsumer_DeadlineExceeded(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.ShadowDeadline = 50 * time.Millisecond
	config.TimeNow = time.Now

	sc := NewShadowConsumer(config)

	// Use a stream that blocks forever (never returns from Next) so the deadline fires.
	// The stream must not have its own timeout — the ShadowConsumer's deadline should be the one that fires.
	stream := &neverEndingStream{
		block: make(chan struct{}),
	}

	result, err := sc.StartShadow(context.Background(), stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionDeadlineExceeded, result.CompletionReason)
}

func TestShadowConsumer_ServerShutdown(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.ShadowDeadline = 30 * time.Minute

	sc := NewShadowConsumer(config)

	// Use a stream that blocks forever so only Cancel() can stop it.
	stream := &neverEndingStream{
		block: make(chan struct{}),
	}

	var shadowResult *ShadowResult
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		result, _ := sc.StartShadow(context.Background(), stream, "test-pair-id")
		shadowResult = result
	}()

	time.Sleep(20 * time.Millisecond)

	sc.Cancel(ShadowCompletionServerShutdown)

	wg.Wait()

	require.NotNil(t, shadowResult)
	assert.Equal(t, ShadowCompletionServerShutdown, shadowResult.CompletionReason)
}

func TestShadowConsumer_ClientDisconnect_ShadowContinues(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.ShadowDeadline = 1 * time.Second

	sc := NewShadowConsumer(config)

	blockCh := make(chan struct{})
	unblockCh := make(chan struct{})

	stream := &clientDisconnectStream{
		block:    blockCh,
		unblock:  unblockCh,
		closed:   false,
	}

	clientCtx, cancel := context.WithCancel(context.Background())

	var shadowResult *ShadowResult
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		result, _ := sc.StartShadow(clientCtx, stream, "test-pair-id")
		shadowResult = result
	}()

	time.Sleep(20 * time.Millisecond)

	cancel()

	time.Sleep(20 * time.Millisecond)
	close(blockCh)
	close(unblockCh)

	wg.Wait()

	require.NotNil(t, shadowResult)
	assert.Equal(t, ShadowCompletionNormal, shadowResult.CompletionReason)
}

func TestShadowConsumer_FullTextRetention_Enabled(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.FullTextRetentionEnabled = true
	config.TimeNow = time.Now

	sc := NewShadowConsumer(config)

	events := []*StreamEvent{
		{Type: "data", Data: []byte("Hello ")},
		{Type: "data", Data: []byte("World")},
		{Type: "done", Data: []byte("[DONE]")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionNormal, result.CompletionReason)
	assert.Equal(t, 2, result.TotalTokensConsumed)
	assert.Equal(t, "Hello World", result.FullText)
}

func TestShadowConsumer_FullTextRetention_Disabled(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.FullTextRetentionEnabled = false
	config.TimeNow = time.Now

	sc := NewShadowConsumer(config)

	events := []*StreamEvent{
		{Type: "data", Data: []byte("Hello ")},
		{Type: "data", Data: []byte("World")},
		{Type: "done", Data: []byte("[DONE]")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionNormal, result.CompletionReason)
	assert.Equal(t, 2, result.TotalTokensConsumed)
	assert.Empty(t, result.FullText)
}

func TestShadowConsumer_TokenCounting(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.TimeNow = time.Now

	sc := NewShadowConsumer(config)

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: nil},
		{Type: "data", Data: []byte("token3")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 3, result.TotalTokensConsumed)
}

func TestShadowConsumer_DurationTracking(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.ShadowDeadline = 1 * time.Hour

	sc := NewShadowConsumer(config)

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Greater(t, result.Duration, 0*time.Second)
	assert.Less(t, result.Duration, 100*time.Millisecond)
}

func TestShadowConsumer_DefaultDeadline(t *testing.T) {
	sc := NewShadowConsumer(ShadowConsumerConfig{})

	assert.Equal(t, 30*time.Minute, sc.config.ShadowDeadline)
}

func TestShadowConsumer_AlreadyRunning(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()

	result1, err1 := sc.StartShadow(ctx, stream, "test-pair-id")
	require.NoError(t, err1)
	require.NotNil(t, result1)

	result2, err2 := sc.StartShadow(ctx, stream, "test-pair-id")
	assert.Error(t, err2)
	assert.Nil(t, result2)
	assert.Equal(t, "shadow consumer has already been started", err2.Error())
}

func TestShadowConsumer_Cancel(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	sc.Cancel(ShadowCompletionServerShutdown)

	result := sc.Result()
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionServerShutdown, result.CompletionReason)
}

func TestShadowConsumer_Cancel_InvalidReason(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	sc.Cancel(ShadowCompletionReason("invalid"))

	result := sc.Result()
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionServerShutdown, result.CompletionReason)
}

func TestShadowConsumer_Cancel_AlreadyCanceled(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	sc.Cancel(ShadowCompletionServerShutdown)
	sc.Cancel(ShadowCompletionUpstreamError)

	result := sc.Result()
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionServerShutdown, result.CompletionReason)
}

func TestShadowCompletionReason_IsValid(t *testing.T) {
	validReasons := []ShadowCompletionReason{
		ShadowCompletionNormal,
		ShadowCompletionUpstreamError,
		ShadowCompletionClientDisconnected,
		ShadowCompletionDeadlineExceeded,
		ShadowCompletionServerShutdown,
	}

	for _, reason := range validReasons {
		assert.True(t, reason.IsValid(), "expected %s to be valid", reason)
	}

	invalidReasons := []ShadowCompletionReason{
		ShadowCompletionReason(""),
		ShadowCompletionReason("invalid"),
		ShadowCompletionReason("COMPLETED"),
	}

	for _, reason := range invalidReasons {
		assert.False(t, reason.IsValid(), "expected %s to be invalid", reason)
	}
}

func TestShadowConsumer_EmptyStream(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	stream := MockStreamForTesting([]*StreamEvent{})

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionNormal, result.CompletionReason)
	assert.Equal(t, 0, result.TotalTokensConsumed)
}

func TestShadowConsumer_NilEvent(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	stream := &nilEventStream{}

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionNormal, result.CompletionReason)
	assert.Equal(t, 0, result.TotalTokensConsumed)
}

func TestShadowConsumer_IsRunning(t *testing.T) {
	sc := NewShadowConsumer(DefaultShadowConsumerConfig())

	assert.False(t, sc.IsRunning())

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
	}
	stream := MockStreamForTesting(events)

	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		for sc.IsRunning() {
			time.Sleep(5 * time.Millisecond)
		}
	}()

	_, _ = sc.StartShadow(ctx, stream, "test-pair-id")

	wg.Wait()
	assert.False(t, sc.IsRunning())
}

type blockingStream struct {
	block chan struct{}
}

func (s *blockingStream) Next() bool {
	<-s.block
	return false
}

func (s *blockingStream) Current() *StreamEvent {
	return nil
}

func (s *blockingStream) Err() error {
	return nil
}

func (s *blockingStream) Close() error {
	return nil
}

type clientDisconnectStream struct {
	block   chan struct{}
	unblock chan struct{}
	closed  bool
	mu      sync.Mutex
}

func (s *clientDisconnectStream) Next() bool {
	select {
	case <-s.block:
		return false
	case <-s.unblock:
		return false
	}
}

func (s *clientDisconnectStream) Current() *StreamEvent {
	return &StreamEvent{Type: "data", Data: []byte("token")}
}

func (s *clientDisconnectStream) Err() error {
	return nil
}

func (s *clientDisconnectStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

type nilEventStream struct {
	called bool
}

func (s *nilEventStream) Next() bool {
	if !s.called {
		s.called = true
		return true
	}
	return false
}

func (s *nilEventStream) Current() *StreamEvent {
	return nil
}

func (s *nilEventStream) Err() error {
	return nil
}

func (s *nilEventStream) Close() error {
	return nil
}

type sleepingStream struct {
	sleepDuration time.Duration
	called        bool
}

func (s *sleepingStream) Next() bool {
	if !s.called {
		s.called = true
		time.Sleep(s.sleepDuration)
		return true
	}
	return false
}

func (s *sleepingStream) Current() *StreamEvent {
	return &StreamEvent{Type: "data", Data: []byte("token")}
}

func (s *sleepingStream) Err() error {
	return nil
}

func (s *sleepingStream) Close() error {
	return nil
}

type neverEndingStream struct {
	block chan struct{}
}

func (s *neverEndingStream) Next() bool {
	<-s.block
	return false
}

func (s *neverEndingStream) Current() *StreamEvent {
	return nil
}

func (s *neverEndingStream) Err() error {
	return nil
}

func (s *neverEndingStream) Close() error {
	return nil
}

type contextAwareBlockingStream struct {
	block chan struct{}
	ctx   context.Context
}

func (s *contextAwareBlockingStream) Next() bool {
	select {
	case <-s.block:
		return false
	case <-s.ctx.Done():
		return false
	}
}

func (s *contextAwareBlockingStream) Current() *StreamEvent {
	return nil
}

func (s *contextAwareBlockingStream) Err() error {
	return nil
}

func (s *contextAwareBlockingStream) Close() error {
	return nil
}

type deadlineAwareStream struct {
	block chan struct{}
	ctx   context.Context
}

func (s *deadlineAwareStream) Next() bool {
	select {
	case <-s.block:
		return false
	case <-s.ctx.Done():
		return false
	}
}

func (s *deadlineAwareStream) Current() *StreamEvent {
	return nil
}

func (s *deadlineAwareStream) Err() error {
	return nil
}

func (s *deadlineAwareStream) Close() error {
	return nil
}