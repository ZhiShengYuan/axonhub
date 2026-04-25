package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/streams"
)

func TestHedgeIntegration_PrimaryWinsFullLifecycle(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     false,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
		{Type: "data", Data: []byte("token5")},
		{Type: "data", Data: []byte("token6")},
		{Type: "done", Data: []byte("[DONE]")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "done", Data: []byte("[DONE]")},
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

	require.NotNil(t, result.WinnerBuffer)
	assert.Len(t, result.WinnerBuffer, 7)

	require.NotNil(t, result.WinnerStream)
	eventCount := 0
	for result.WinnerStream.Next() {
		event := result.WinnerStream.Current()
		if event != nil && len(event.Data) > 0 {
			eventCount++
		}
	}
	assert.Equal(t, 7, eventCount)
}

func TestHedgeIntegration_SecondaryWinsFullLifecycle(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "done", Data: []byte("[DONE]")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "data", Data: []byte("token4")},
		{Type: "data", Data: []byte("token5")},
		{Type: "data", Data: []byte("token6")},
		{Type: "done", Data: []byte("[DONE]")},
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

	require.NotNil(t, result.WinnerBuffer)
	assert.Len(t, result.WinnerBuffer, 7)
}

func TestHedgeIntegration_BothFailFallbackToRemaining(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryStream := &errorStream{err: errors.New("primary connection failed")}
	secondaryStream := &errorStream{err: errors.New("secondary connection failed")}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.BothFailed)
	assert.Nil(t, result.WinnerBuffer)
	assert.Nil(t, result.WinnerStream)
}

func TestHedgeIntegration_ProbingModeBothLaunchImmediately(t *testing.T) {
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

func TestHedgeIntegration_WinnerPassthroughStream(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("primary1")},
		{Type: "data", Data: []byte("primary2")},
		{Type: "done", Data: []byte("[DONE]")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("secondary1")},
		{Type: "data", Data: []byte("secondary2")},
		{Type: "data", Data: []byte("secondary3")},
		{Type: "data", Data: []byte("secondary4")},
		{Type: "data", Data: []byte("secondary5")},
		{Type: "data", Data: []byte("secondary6")},
		{Type: "done", Data: []byte("[DONE]")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 50 * time.Millisecond,
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
	require.NotNil(t, result.WinnerStream)

	collected := make([]string, 0)
	for result.WinnerStream.Next() {
		event := result.WinnerStream.Current()
		if event != nil && len(event.Data) > 0 {
			collected = append(collected, string(event.Data))
		}
	}

	require.True(t, len(collected) > 0, "Should have collected events from winner stream")
	assert.NotEmpty(t, result.WinnerBuffer)
}

func TestHedgeIntegration_ShadowDeadlineExceeded(t *testing.T) {
	config := DefaultShadowConsumerConfig()
	config.ShadowDeadline = 50 * time.Millisecond

	sc := NewShadowConsumer(config)

	events := make([]*StreamEvent, 100)
	for i := 0; i < 100; i++ {
		events[i] = &StreamEvent{Type: "data", Data: []byte("token")}
	}

	stream := &slowStream{
		events:    events,
		idx:       0,
		readDelay: 10 * time.Millisecond,
	}

	ctx := context.Background()
	result, err := sc.StartShadow(ctx, stream, "test-pair-id")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, ShadowCompletionDeadlineExceeded, result.CompletionReason)
}

func TestHedgeIntegration_ObservationWindowWithNoTokens(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 100 * time.Millisecond,
		ObservationWindow: 200 * time.Millisecond,
		IsProbingMode:     false,
	}

	primaryEvents := []*StreamEvent{}
	secondaryEvents := []*StreamEvent{}

	primaryStream := MockStreamForTesting(primaryEvents)
	secondaryStream := MockStreamForTesting(secondaryEvents)

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.BothFailed)
}

func TestHedgeIntegration_ContextCancellationDuringObservation(t *testing.T) {
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
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	hc := NewHedgeCoordinator(config)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, context.Canceled, err)
}

func TestHedgeIntegration_PrimaryProducesFirstSecondaryNeverStarts(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     false,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "done", Data: []byte("[DONE]")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 100 * time.Millisecond,
	}

	secondaryStream := &errorStream{err: errors.New("secondary never started")}

	hc := NewHedgeCoordinator(config)
	ctx := context.Background()

	result, err := hc.StartRace(ctx, primaryStream, secondaryStream)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.BothFailed)
	assert.Equal(t, 0, result.WinnerIndex)
	assert.Equal(t, 1, result.LoserIndex)
}

func TestHedgeIntegration_SecondaryProducesFirstBeforePrimary(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 3 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "done", Data: []byte("[DONE]")},
	}

	secondaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("token1")},
		{Type: "data", Data: []byte("token2")},
		{Type: "data", Data: []byte("token3")},
		{Type: "done", Data: []byte("[DONE]")},
	}

	primaryStream := &delayedMockStream{
		events:    primaryEvents,
		idx:       0,
		readDelay: 500 * time.Millisecond,
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
	assert.False(t, result.BothFailed)
	assert.Equal(t, 1, result.WinnerIndex)
}

func TestHedgeIntegration_BothStreamsProduceTokensSimultaneously(t *testing.T) {
	config := HedgeCoordinatorConfig{
		HedgeTriggerDelay: 12 * time.Second,
		ObservationWindow: 1 * time.Second,
		IsProbingMode:     true,
	}

	primaryEvents := []*StreamEvent{
		{Type: "data", Data: []byte("p1")},
		{Type: "data", Data: []byte("p2")},
		{Type: "data", Data: []byte("p3")},
		{Type: "data", Data: []byte("p4")},
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
	assert.False(t, result.BothFailed)
	assert.Equal(t, 0, result.WinnerIndex)
}

func TestHedgeIntegration_AllShadowCompletionReasons(t *testing.T) {
	tests := []struct {
		name            string
		events          []*StreamEvent
		shadowDeadline  time.Duration
		upstreamErr     error
		blocked         bool
		cancelAfter     time.Duration
		expectedReason  ShadowCompletionReason
	}{
		{
			name:           "normal_completion EOF",
			events:         []*StreamEvent{{Type: "data", Data: []byte("t1")}, {Type: "data", Data: []byte("t2")}},
			expectedReason: ShadowCompletionNormal,
		},
		{
			name:           "normal_completion DONE sentinel",
			events:         []*StreamEvent{{Type: "data", Data: []byte("t1")}, {Type: "done", Data: []byte("[DONE]")}},
			expectedReason: ShadowCompletionNormal,
		},
		{
			name:           "upstream_error",
			upstreamErr:    errors.New("connection reset"),
			expectedReason: ShadowCompletionUpstreamError,
		},
		{
			name:           "deadline_exceeded",
			events:         []*StreamEvent{{Type: "data", Data: []byte("t1")}},
			shadowDeadline: 1 * time.Millisecond,
			blocked:        true,
			expectedReason: ShadowCompletionDeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultShadowConsumerConfig()
			if tt.shadowDeadline > 0 {
				config.ShadowDeadline = tt.shadowDeadline
			}

			sc := NewShadowConsumer(config)

			var stream streams.Stream[*StreamEvent]
			if tt.upstreamErr != nil {
				stream = &errorStream{err: tt.upstreamErr}
			} else if tt.blocked {
				stream = &neverEndingStream{block: make(chan struct{})}
			} else {
				stream = MockStreamForTesting(tt.events)
			}

			ctx := context.Background()
			if tt.cancelAfter > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(context.Background())
				go func() {
					time.Sleep(tt.cancelAfter)
					cancel()
				}()
			}

			result, err := sc.StartShadow(ctx, stream, "test-pair-id")

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, tt.expectedReason, result.CompletionReason)
		})
	}
}

func TestHedgeIntegration_AllHedgeFallbackActions(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	tests := []struct {
		name           string
		raceResult     *HedgeRaceResult
		state          *PersistenceState
		expectedAction HedgeFallbackAction
	}{
		{
			name: "FallbackToRemaining when both failed",
			raceResult: &HedgeRaceResult{
				BothFailed:  true,
				WinnerIndex: -1,
				LoserIndex:  -1,
			},
			state:          &PersistenceState{HedgeState: &HedgeState{Phase: HedgeDisabled}},
			expectedAction: FallbackToRemaining,
		},
		{
			name: "ContinueWithSingleStream when primary fails",
			raceResult: &HedgeRaceResult{
				BothFailed:  false,
				WinnerIndex: 1,
				LoserIndex:  0,
			},
			state:          &PersistenceState{HedgeState: &HedgeState{Phase: HedgeObservationActive}},
			expectedAction: ContinueWithSingleStream,
		},
		{
			name: "ContinueWithSingleStream when secondary fails",
			raceResult: &HedgeRaceResult{
				BothFailed:  false,
				WinnerIndex: 0,
				LoserIndex:  1,
			},
			state:          &PersistenceState{HedgeState: &HedgeState{Phase: HedgeObservationActive}},
			expectedAction: ContinueWithSingleStream,
		},
		{
			name: "NoAction when winner already released",
			raceResult: &HedgeRaceResult{
				BothFailed:  false,
				WinnerIndex: 0,
				LoserIndex:  1,
			},
			state:          &PersistenceState{HedgeState: &HedgeState{Phase: HedgeWinnerReleased}},
			expectedAction: NoAction,
		},
		{
			name: "NoAction when loser is shadowing",
			raceResult: &HedgeRaceResult{
				BothFailed:  false,
				WinnerIndex: 0,
				LoserIndex:  1,
			},
			state:          &PersistenceState{HedgeState: &HedgeState{Phase: HedgeLoserShadowing}},
			expectedAction: NoAction,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action := m.HandleHedgeFailure(ctx, tt.raceResult, tt.state)
			assert.Equal(t, tt.expectedAction, action)
		})
	}
}

func TestHedgeIntegration_AllHedgePhaseTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialPhase  HedgePhase
		transitionFn  func(*HedgeState) error
		expectedPhase HedgePhase
		expectError   bool
	}{
		{
			name:          "TransitionToSecondaryLaunched from PrimaryOnly",
			initialPhase:  HedgePrimaryOnly,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToSecondaryLaunched() },
			expectedPhase: HedgeSecondaryLaunched,
		},
		{
			name:          "TransitionToObservationActive from SecondaryLaunched",
			initialPhase:  HedgeSecondaryLaunched,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToObservationActive() },
			expectedPhase: HedgeObservationActive,
		},
		{
			name:          "TransitionToWinnerReleased from ObservationActive primary wins",
			initialPhase:  HedgeObservationActive,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToWinnerReleased(0) },
			expectedPhase: HedgeWinnerReleased,
		},
		{
			name:          "TransitionToWinnerReleased from ObservationActive secondary wins",
			initialPhase:  HedgeObservationActive,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToWinnerReleased(1) },
			expectedPhase: HedgeWinnerReleased,
		},
		{
			name:          "TransitionToLoserShadowing from WinnerReleased",
			initialPhase:  HedgeWinnerReleased,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToLoserShadowing() },
			expectedPhase: HedgeLoserShadowing,
		},
		{
			name:          "TransitionToShadowCompleted from LoserShadowing normal",
			initialPhase:  HedgeLoserShadowing,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToShadowCompleted("completed") },
			expectedPhase: HedgeShadowCompleted,
		},
		{
			name:          "TransitionToShadowCompleted from LoserShadowing deadline_exceeded",
			initialPhase:  HedgeLoserShadowing,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToShadowCompleted("deadline_exceeded") },
			expectedPhase: HedgeShadowCompleted,
		},
		{
			name:          "TransitionToShadowDeadlineExceeded from LoserShadowing",
			initialPhase:  HedgeLoserShadowing,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToShadowDeadlineExceeded() },
			expectedPhase: HedgeShadowDeadlineExceeded,
		},
		{
			name:          "TransitionToFallbackResumed from ShadowCompleted",
			initialPhase:  HedgeShadowCompleted,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToFallbackResumed() },
			expectedPhase: HedgeFallbackResumed,
		},
		{
			name:          "TransitionToFallbackResumed from WinnerReleased",
			initialPhase:  HedgeWinnerReleased,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToFallbackResumed() },
			expectedPhase: HedgeFallbackResumed,
		},
		{
			name:          "Invalid transition SecondaryLaunched from Disabled",
			initialPhase:  HedgeDisabled,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToSecondaryLaunched() },
			expectError:   true,
		},
		{
			name:          "Invalid transition ObservationActive from Disabled",
			initialPhase:  HedgeDisabled,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToObservationActive() },
			expectError:   true,
		},
		{
			name:          "Invalid winner index 2",
			initialPhase:  HedgeObservationActive,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToWinnerReleased(2) },
			expectError:   true,
		},
		{
			name:          "Invalid winner index -1",
			initialPhase:  HedgeObservationActive,
			transitionFn:  func(h *HedgeState) error { return h.TransitionToWinnerReleased(-1) },
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &HedgeState{Phase: tt.initialPhase}
			err := tt.transitionFn(state)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedPhase, state.Phase)
			}
		})
	}
}

type slowStream struct {
	events    []*StreamEvent
	idx       int
	readDelay time.Duration
	mu        sync.Mutex
}

func (s *slowStream) Next() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.idx >= len(s.events) {
		return false
	}
	time.Sleep(s.readDelay)
	return true
}

func (s *slowStream) Current() *StreamEvent {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.idx >= len(s.events) {
		return nil
	}
	event := s.events[s.idx]
	s.idx++
	return event
}

func (s *slowStream) Err() error {
	return nil
}

func (s *slowStream) Close() error {
	return nil
}
