package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHedgeCoordinator_PrimaryWinsWithHigherTPS(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
		{Type: "data", Data: []byte("token5")},
		{Type: "data", Data: []byte("token6")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 200 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 500 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.BothFailed)
	assert.Equal(t, 0, result.WinnerIndex)
	assert.Equal(t, 1, result.LoserIndex)
	assert.NotNil(t, result.WinnerBuffer)
	assert.Len(t, result.WinnerBuffer, 6)
}

func TestHedgeCoordinator_SecondaryWinsWithHigherTPS(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
		{Type: "data", Data: []byte("token5")},
		{Type: "data", Data: []byte("token6")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 500 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 200 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.BothFailed)
	assert.Equal(t, 1, result.WinnerIndex)
	assert.Equal(t, 0, result.LoserIndex)
	assert.Len(t, result.WinnerBuffer, 6)
}

func TestHedgeCoordinator_SingleActiveStreamWins(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     false,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 200 * time.Millisecond,
	}

	secondaryStream := &errorStream{err: errors.New("stream failed")}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.BothFailed)
	assert.Equal(t, 0, result.WinnerIndex)
	assert.Equal(t, 1, result.LoserIndex)
	assert.Len(t, result.WinnerBuffer, 2)
}

func TestHedgeCoordinator_DualFailureReturnsBothFailed(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryStream := &errorStream{err: errors.New("primary failed")}
	secondaryStream := &errorStream{err: errors.New("secondary failed")}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.BothFailed)
	assert.Nil(t, result.WinnerBuffer)
	assert.Nil(t, result.WinnerStream)
}

func TestHedgeCoordinator_ProbingModeLaunchesBothImmediately(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("p1")},
		{Type: "data", Data: []byte("p2")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("s1")},
		{Type: "data", Data: []byte("s2")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	start := time.Now()
	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.BothFailed)
	assert.Less(t, elapsed, 5*time.Second)
}

func TestHedgeCoordinator_ComputeObservationTPS(t *testing.T) {
	config := HedgeCoordinatorConfig{
		ObservationWindow: 3 * time.Second,
	}

	hc := NewHedgeCoordinator(config)

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
		{Type: "data", Data: []byte("token5")},
		{Type: "data", Data: []byte("token6")},
	}

	tps := hc.ComputeObservationTPS(events, 3*time.Second)

	assert.InDelta(t, 2.0, tps, 0.1)
}

func TestHedgeCoordinator_ComputeObservationTPS_EmptyEvents(t *testing.T) {
	config := HedgeCoordinatorConfig{
		ObservationWindow: 3 * time.Second,
	}

	hc := NewHedgeCoordinator(config)

	events := []*StreamEvent{}

	tps := hc.ComputeObservationTPS(events, 3*time.Second)

	assert.Equal(t, 0.0, tps)
}

func TestHedgeCoordinator_ComputeObservationTPS_ZeroDuration(t *testing.T) {
	config := HedgeCoordinatorConfig{
		ObservationWindow: 3 * time.Second,
	}

	hc := NewHedgeCoordinator(config)

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
	}

	tps := hc.ComputeObservationTPS(events, 0)

	assert.Equal(t, 0.0, tps)
}

func TestHedgeCoordinator_ContextCancellation_CleansUp(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 10 * time.Second,
		ObservationWindow: 10 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 5 * time.Second,
	}

	secondaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 5 * time.Second,
	}

	hc := NewHedgeCoordinator(config)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	assert.Error(t, err)
	assert.Nil(t, result)

	_ = cancel
}

func TestHedgeCoordinator_BufferFlushExactlyOnce(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 1 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("p1")},
		{Type: "data", Data: []byte("p2")},
		{Type: "data", Data: []byte("p3")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("s1")},
		{Type: "data", Data: []byte("s2")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)

	flushedBuffer := result.WinnerBuffer
	require.NotNil(t, flushedBuffer)

	for _, event := range flushedBuffer {
		require.NotNil(t, event)
	}

	assert.Len(t, flushedBuffer, 3)
}

func TestHedgeCoordinator_DefaultConfig(t *testing.T) {
	config := DefaultHedgeCoordinatorConfig()

	assert.Equal(t, 12*time.Second, config.HedgeTriggerDelay)
	assert.Equal(t, 3*time.Second, config.ObservationWindow)
	assert.False(t, config.IsProbingMode)
}

func TestHedgeCoordinator_IsObservationActive(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)

	assert.False(t, hc.IsObservationActive())

	ctx := context.Background()
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = hc.IsObservationActive()
	}()

	_, _ = hc.StartRace(ctx, primaryStream, secondaryStream)
}

func TestHedgeCoordinator_GetPrimaryBuffer(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("p1")},
		{Type: "data", Data: []byte("p2")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("s1")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	_, _ = hc.StartRace(ctx, primaryStream, secondaryStream)

	buffer := hc.GetPrimaryBuffer()
	require.Len(t, buffer, 2)
}

func TestHedgeCoordinator_GetSecondaryBuffer(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("p1")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("s1")},
		{Type: "data", Data: []byte("s2")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	_, _ = hc.StartRace(ctx, primaryStream, secondaryStream)

	buffer := hc.GetSecondaryBuffer()
	require.Len(t, buffer, 2)
}

func TestHedgeCoordinator_Cancel(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 10 * time.Second,
		ObservationWindow: 10 * time.Second,
		IsProbingMode:     true,
	}

	primaryStream := &delayedMockStream{
		events:    []*StreamEvent{{Type: "data", Data: []byte("token1")}},
		idx:       0,
		readDelay: 5 * time.Second,
	}

	secondaryStream := &delayedMockStream{
		events:    []*StreamEvent{{Type: "data", Data: []byte("token1")}},
		idx:       0,
		readDelay: 5 * time.Second,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	go func() {
		time.Sleep(50 * time.Millisecond)
		hc.Cancel()
	}()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestHedgeCoordinator_Err(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryStream := &errorStream{err: errors.New("primary error")}
	secondaryStream := &errorStream{err: errors.New("secondary error")}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	_, _ = hc.StartRace(ctx, primaryStream, secondaryStream)

	err := hc.Err()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "primary error")
}

func TestHedgeCoordinator_TPSCalculation_Correct(t *testing.T) {
	config := HedgeCoordinatorConfig{
		ObservationWindow: 2 * time.Second,
	}

	hc := NewHedgeCoordinator(config)

	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
		{Type: "data", Data: []byte("token5")},
		{Type: "data", Data: []byte("token6")},
		{Type: "data", Data: []byte("token7")},
		{Type: "data", Data: []byte("token8")},
		{Type: "data", Data: []byte("token9")},
		{Type: "data", Data: []byte("token10")},
	}

	tps := hc.ComputeObservationTPS(events, 2*time.Second)

	assert.InDelta(t, 5.0, tps, 0.1)
}

func TestHedgeCoordinator_BothActiveSameTPS_PrimaryWins(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    secondaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, 0, result.WinnerIndex)
}

func TestBufferedStreamReader_NextAndCurrent(t *testing.T) {
	events := []*StreamEvent{
		{Type: "data", Data: []byte("1")},
		{Type: "data", Data: []byte("2")},
		{Type: "data", Data: []byte("3")},
	}

	reader := NewBufferedStreamReader(events, nil)

	assert.True(t, reader.Next())
	assert.Equal(t, []byte("1"), reader.Current().Data)

	assert.True(t, reader.Next())
	assert.Equal(t, []byte("2"), reader.Current().Data)

	assert.True(t, reader.Next())
	assert.Equal(t, []byte("3"), reader.Current().Data)

	assert.False(t, reader.Next())
}

func TestBufferedStreamReader_Close(t *testing.T) {
	events := []*StreamEvent{
		{Type: "data", Data: []byte("1")},
	}

	reader := NewBufferedStreamReader(events, nil)

	err := reader.Close()
	assert.NoError(t, err)

	err = reader.Close()
	assert.NoError(t, err)
}

func TestMockStreamForTesting(t *testing.T) {
	events := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
	}

	stream := MockStreamForTesting(events)

	assert.True(t, stream.Next())
	assert.Equal(t, []byte("token1"), stream.Current().Data)

	assert.True(t, stream.Next())
	assert.Equal(t, []byte("token2"), stream.Current().Data)

	assert.False(t, stream.Next())
	assert.NoError(t, stream.Err())
	assert.NoError(t, stream.Close())
}

func TestCreateTestStreamEvent(t *testing.T) {
	event := CreateTestStreamEvent("data", []byte("test"))

	assert.Equal(t, "data", event.Type)
	assert.Equal(t, []byte("test"), event.Data)
}

func TestIsHedgeError(t *testing.T) {
	assert.True(t, IsHedgeError(ErrBothStreamsFailed))
	assert.True(t, IsHedgeError(ErrObservationTimeout))
	assert.True(t, IsHedgeError(ErrInvalidWinnerIndex))
	assert.False(t, IsHedgeError(errors.New("other error")))
}

type delayedMockStream struct {
	events    []*StreamEvent
	idx       int
	readDelay time.Duration
	closed    bool
	mu        struct{}
}

func (s *delayedMockStream) Next() bool {
	time.Sleep(s.readDelay)
	return s.idx < len(s.events)
}

func (s *delayedMockStream) Current() *StreamEvent {
	if s.idx < len(s.events) {
		event := s.events[s.idx]
		s.idx++
		return event
	}
	return nil
}

func (s *delayedMockStream) Err() error {
	return nil
}

func (s *delayedMockStream) Close() error {
	s.closed = true
	return nil
}

type blockingMockStream struct {
	events []*StreamEvent
	idx    int
	block  chan struct{}
	closed bool
}

func (s *blockingMockStream) Next() bool {
	<-s.block
	return s.idx < len(s.events)
}

func (s *blockingMockStream) Current() *StreamEvent {
	if s.idx < len(s.events) {
		event := s.events[s.idx]
		s.idx++
		return event
	}
	return nil
}

func (s *blockingMockStream) Err() error {
	return nil
}

func (s *blockingMockStream) Close() error {
	s.closed = true
	return nil
}

type errorStream struct {
	err    error
	closed bool
}

func (s *errorStream) Next() bool {
	return false
}

func (s *errorStream) Current() *StreamEvent {
	return nil
}

func (s *errorStream) Err() error {
	return s.err
}

func (s *errorStream) Close() error {
	s.closed = true
	return nil
}