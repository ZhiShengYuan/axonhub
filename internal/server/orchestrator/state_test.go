package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStreamReleaseState_InitialState(t *testing.T) {
	s := &PersistenceState{}
	assert.Equal(t, ReleaseNone, s.StreamReleaseState)
	assert.False(t, s.IsStreamReleased())
	assert.True(t, s.CanRetryStream())
}

func TestStreamReleaseState_NoneToBufferingToEmitted(t *testing.T) {
	s := &PersistenceState{}

	assert.Equal(t, ReleaseNone, s.StreamReleaseState)
	assert.True(t, s.CanRetryStream())
	assert.False(t, s.IsStreamReleased())

	s.StreamReleaseState = ReleaseBuffering
	assert.Equal(t, ReleaseBuffering, s.StreamReleaseState)
	assert.True(t, s.CanRetryStream())
	assert.False(t, s.IsStreamReleased())

	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState)
	assert.False(t, s.CanRetryStream())
	assert.True(t, s.IsStreamReleased())
}

func TestStreamReleaseState_MarkStreamReleasedIdempotent(t *testing.T) {
	s := &PersistenceState{}
	s.StreamReleaseState = ReleaseBuffering

	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState)

	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState, "calling MarkStreamReleased twice should be idempotent")

	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState, "calling MarkStreamReleased third time should still be ReleaseEmitted")
}

func TestStreamReleaseState_MarkReleaseForbidden(t *testing.T) {
	s := &PersistenceState{}

	s.MarkReleaseForbidden()
	assert.Equal(t, ReleaseForbidden, s.StreamReleaseState)
	assert.True(t, s.IsStreamReleased())
	assert.False(t, s.CanRetryStream())
}

func TestStreamReleaseState_ForbiddenFromBuffering(t *testing.T) {
	s := &PersistenceState{}
	s.StreamReleaseState = ReleaseBuffering

	s.MarkReleaseForbidden()
	assert.Equal(t, ReleaseForbidden, s.StreamReleaseState)
	assert.True(t, s.IsStreamReleased())
	assert.False(t, s.CanRetryStream())
}

func TestStreamReleaseState_ForbiddenFromEmitted(t *testing.T) {
	s := &PersistenceState{}
	s.StreamReleaseState = ReleaseBuffering
	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState)

	s.MarkReleaseForbidden()
	assert.Equal(t, ReleaseForbidden, s.StreamReleaseState)
	assert.True(t, s.IsStreamReleased())
	assert.False(t, s.CanRetryStream())
}

func TestStreamReleaseState_FullLifecycle(t *testing.T) {
	s := &PersistenceState{}

	assert.Equal(t, ReleaseNone, s.StreamReleaseState)
	assert.True(t, s.CanRetryStream())
	assert.False(t, s.IsStreamReleased())

	s.StreamReleaseState = ReleaseBuffering
	assert.Equal(t, ReleaseBuffering, s.StreamReleaseState)
	assert.True(t, s.CanRetryStream())
	assert.False(t, s.IsStreamReleased())

	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState)
	assert.False(t, s.CanRetryStream())
	assert.True(t, s.IsStreamReleased())

	s.MarkReleaseForbidden()
	assert.Equal(t, ReleaseForbidden, s.StreamReleaseState)
	assert.False(t, s.CanRetryStream())
	assert.True(t, s.IsStreamReleased())
}

func TestStreamReleaseState_CanRetryStream(t *testing.T) {
	tests := []struct {
		name     string
		state    StreamReleaseState
		expected bool
	}{
		{"ReleaseNone allows retry", ReleaseNone, true},
		{"ReleaseBuffering allows retry", ReleaseBuffering, true},
		{"ReleaseEmitted forbids retry", ReleaseEmitted, false},
		{"ReleaseForbidden forbids retry", ReleaseForbidden, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PersistenceState{StreamReleaseState: tt.state}
			assert.Equal(t, tt.expected, s.CanRetryStream())
		})
	}
}

func TestStreamReleaseState_IsStreamReleased(t *testing.T) {
	tests := []struct {
		name     string
		state    StreamReleaseState
		expected bool
	}{
		{"ReleaseNone is not released", ReleaseNone, false},
		{"ReleaseBuffering is not released", ReleaseBuffering, false},
		{"ReleaseEmitted is released", ReleaseEmitted, true},
		{"ReleaseForbidden is released", ReleaseForbidden, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &PersistenceState{StreamReleaseState: tt.state}
			assert.Equal(t, tt.expected, s.IsStreamReleased())
		})
	}
}

func TestStreamBufferingConfig_DefaultValues(t *testing.T) {
	cfg := DefaultStreamBufferingConfig()

	assert.True(t, cfg.Enabled)
	assert.Equal(t, 16, cfg.ChunkThreshold)
	assert.Equal(t, 3*time.Second, cfg.TimerDuration)
}

func TestStreamBufferingConfig_Disabled(t *testing.T) {
	cfg := DisabledStreamBufferingConfig()

	assert.False(t, cfg.Enabled)
	assert.Equal(t, 0, cfg.ChunkThreshold)
	assert.Equal(t, time.Duration(0), cfg.TimerDuration)
}

func TestStreamReleaseState_ThreadSafety(t *testing.T) {
	s := &PersistenceState{}
	s.StreamReleaseState = ReleaseBuffering

	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			s.MarkStreamReleased()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			s.MarkReleaseForbidden()
		}
		done <- true
	}()

	<-done
	<-done

	assert.True(t, s.IsStreamReleased())
	assert.False(t, s.CanRetryStream())
}

func TestMarkStreamReleased_FromReleaseNone(t *testing.T) {
	s := &PersistenceState{}
	assert.Equal(t, ReleaseNone, s.StreamReleaseState)

	s.MarkStreamReleased()
	assert.Equal(t, ReleaseNone, s.StreamReleaseState, "MarkStreamReleased should be no-op when state is ReleaseNone")
}

func TestMarkStreamReleased_FromReleaseForbidden(t *testing.T) {
	s := &PersistenceState{}
	s.StreamReleaseState = ReleaseForbidden
	assert.Equal(t, ReleaseForbidden, s.StreamReleaseState)

	s.MarkStreamReleased()
	assert.Equal(t, ReleaseForbidden, s.StreamReleaseState, "MarkStreamReleased should be no-op when state is ReleaseForbidden")
}

func TestHedgePhase_String(t *testing.T) {
	tests := []struct {
		phase    HedgePhase
		expected string
	}{
		{HedgeDisabled, "HedgeDisabled"},
		{HedgePrimaryOnly, "HedgePrimaryOnly"},
		{HedgeSecondaryLaunched, "HedgeSecondaryLaunched"},
		{HedgeObservationActive, "HedgeObservationActive"},
		{HedgeWinnerReleased, "HedgeWinnerReleased"},
		{HedgeLoserShadowing, "HedgeLoserShadowing"},
		{HedgeShadowCompleted, "HedgeShadowCompleted"},
		{HedgeShadowDeadlineExceeded, "HedgeShadowDeadlineExceeded"},
		{HedgeFallbackResumed, "HedgeFallbackResumed"},
		{HedgePhase(100), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.phase.String())
		})
	}
}

func TestHedgeState_InitialState(t *testing.T) {
	h := &HedgeState{}
	assert.Equal(t, HedgeDisabled, h.Phase)
	assert.False(t, h.IsHedgeActive())
	assert.False(t, h.IsObservationActive())
	assert.False(t, h.IsShadowActive())
	assert.Equal(t, 0, h.PrimaryCandidateIndex)
	assert.Equal(t, 0, h.SecondaryCandidateIndex)
	assert.Equal(t, 0, h.WinnerIndex, "WinnerIndex uses Go zero value (0), not -1")
	assert.Equal(t, 0, h.LoserIndex)
	assert.False(t, h.FallbackAllowed)
}

func TestHedgeState_IsHedgeActive(t *testing.T) {
	h := &HedgeState{}
	assert.False(t, h.IsHedgeActive())

	h.Phase = HedgePrimaryOnly
	assert.True(t, h.IsHedgeActive())

	h.Phase = HedgeSecondaryLaunched
	assert.True(t, h.IsHedgeActive())

	h.Phase = HedgeObservationActive
	assert.True(t, h.IsHedgeActive())

	h.Phase = HedgeWinnerReleased
	assert.True(t, h.IsHedgeActive())

	h.Phase = HedgeLoserShadowing
	assert.True(t, h.IsHedgeActive())

	h.Phase = HedgeShadowCompleted
	assert.True(t, h.IsHedgeActive())

	h.Phase = HedgeShadowDeadlineExceeded
	assert.True(t, h.IsHedgeActive())

	h.Phase = HedgeFallbackResumed
	assert.True(t, h.IsHedgeActive())
}

func TestHedgeState_IsObservationActive(t *testing.T) {
	h := &HedgeState{}
	assert.False(t, h.IsObservationActive())

	h.Phase = HedgePrimaryOnly
	assert.False(t, h.IsObservationActive())

	h.Phase = HedgeSecondaryLaunched
	assert.False(t, h.IsObservationActive())

	h.Phase = HedgeObservationActive
	assert.True(t, h.IsObservationActive())

	h.Phase = HedgeWinnerReleased
	assert.False(t, h.IsObservationActive())
}

func TestHedgeState_IsShadowActive(t *testing.T) {
	h := &HedgeState{}
	assert.False(t, h.IsShadowActive())

	h.Phase = HedgeWinnerReleased
	assert.False(t, h.IsShadowActive())

	h.Phase = HedgeLoserShadowing
	assert.True(t, h.IsShadowActive())

	h.Phase = HedgeShadowCompleted
	assert.False(t, h.IsShadowActive())
}

func TestHedgeState_TransitionToSecondaryLaunched(t *testing.T) {
	h := &HedgeState{Phase: HedgePrimaryOnly}
	assert.NoError(t, h.TransitionToSecondaryLaunched())
	assert.Equal(t, HedgeSecondaryLaunched, h.Phase)
}

func TestHedgeState_TransitionToSecondaryLaunched_InvalidPhase(t *testing.T) {
	h := &HedgeState{Phase: HedgeDisabled}
	err := h.TransitionToSecondaryLaunched()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")
	assert.Equal(t, HedgeDisabled, h.Phase)
}

func TestHedgeState_TransitionToObservationActive(t *testing.T) {
	h := &HedgeState{Phase: HedgeSecondaryLaunched}
	assert.NoError(t, h.TransitionToObservationActive())
	assert.Equal(t, HedgeObservationActive, h.Phase)
}

func TestHedgeState_TransitionToObservationActive_InvalidPhase(t *testing.T) {
	h := &HedgeState{Phase: HedgePrimaryOnly}
	err := h.TransitionToObservationActive()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")
	assert.Equal(t, HedgePrimaryOnly, h.Phase)
}

func TestHedgeState_TransitionToWinnerReleased(t *testing.T) {
	h := &HedgeState{Phase: HedgeObservationActive}
	assert.NoError(t, h.TransitionToWinnerReleased(0))
	assert.Equal(t, HedgeWinnerReleased, h.Phase)
	assert.Equal(t, 0, h.WinnerIndex)
	assert.Equal(t, 1, h.LoserIndex)

	h2 := &HedgeState{Phase: HedgeObservationActive}
	assert.NoError(t, h2.TransitionToWinnerReleased(1))
	assert.Equal(t, HedgeWinnerReleased, h2.Phase)
	assert.Equal(t, 1, h2.WinnerIndex)
	assert.Equal(t, 0, h2.LoserIndex)
}

func TestHedgeState_TransitionToWinnerReleased_InvalidPhase(t *testing.T) {
	h := &HedgeState{Phase: HedgeSecondaryLaunched}
	err := h.TransitionToWinnerReleased(0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")
	assert.Equal(t, HedgeSecondaryLaunched, h.Phase)
}

func TestHedgeState_TransitionToWinnerReleased_InvalidWinnerIndex(t *testing.T) {
	h := &HedgeState{Phase: HedgeObservationActive}
	err := h.TransitionToWinnerReleased(2)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid winner index")

	err = h.TransitionToWinnerReleased(-1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid winner index")
}

func TestHedgeState_TransitionToLoserShadowing(t *testing.T) {
	h := &HedgeState{Phase: HedgeWinnerReleased}
	assert.NoError(t, h.TransitionToLoserShadowing())
	assert.Equal(t, HedgeLoserShadowing, h.Phase)
}

func TestHedgeState_TransitionToLoserShadowing_InvalidPhase(t *testing.T) {
	h := &HedgeState{Phase: HedgeObservationActive}
	err := h.TransitionToLoserShadowing()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")
	assert.Equal(t, HedgeObservationActive, h.Phase)
}

func TestHedgeState_TransitionToShadowCompleted(t *testing.T) {
	h := &HedgeState{Phase: HedgeLoserShadowing}
	validReasons := []string{"completed", "deadline_exceeded", "upstream_error", "server_shutdown", "client_disconnected"}

	for _, reason := range validReasons {
		h.Phase = HedgeLoserShadowing
		assert.NoError(t, h.TransitionToShadowCompleted(reason), "reason: %s", reason)
		assert.Equal(t, HedgeShadowCompleted, h.Phase)
		assert.Equal(t, reason, h.ShadowCompletionReason)
	}
}

func TestHedgeState_TransitionToShadowCompleted_InvalidPhase(t *testing.T) {
	h := &HedgeState{Phase: HedgeWinnerReleased}
	err := h.TransitionToShadowCompleted("completed")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")
}

func TestHedgeState_TransitionToShadowCompleted_InvalidReason(t *testing.T) {
	h := &HedgeState{Phase: HedgeLoserShadowing}
	err := h.TransitionToShadowCompleted("invalid_reason")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid shadow completion reason")
}

func TestHedgeState_TransitionToShadowDeadlineExceeded(t *testing.T) {
	h := &HedgeState{Phase: HedgeLoserShadowing}
	assert.NoError(t, h.TransitionToShadowDeadlineExceeded())
	assert.Equal(t, HedgeShadowDeadlineExceeded, h.Phase)
	assert.Equal(t, "deadline_exceeded", h.ShadowCompletionReason)
}

func TestHedgeState_TransitionToShadowDeadlineExceeded_InvalidPhase(t *testing.T) {
	h := &HedgeState{Phase: HedgeWinnerReleased}
	err := h.TransitionToShadowDeadlineExceeded()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")
}

func TestHedgeState_TransitionToFallbackResumed(t *testing.T) {
	h := &HedgeState{Phase: HedgeShadowCompleted}
	assert.NoError(t, h.TransitionToFallbackResumed())
	assert.Equal(t, HedgeFallbackResumed, h.Phase)
	assert.True(t, h.FallbackAllowed)

	h2 := &HedgeState{Phase: HedgeShadowDeadlineExceeded}
	assert.NoError(t, h2.TransitionToFallbackResumed())
	assert.Equal(t, HedgeFallbackResumed, h2.Phase)

	h3 := &HedgeState{Phase: HedgeWinnerReleased}
	assert.NoError(t, h3.TransitionToFallbackResumed())
	assert.Equal(t, HedgeFallbackResumed, h3.Phase)
}

func TestHedgeState_TransitionToFallbackResumed_InvalidPhase(t *testing.T) {
	h := &HedgeState{Phase: HedgeObservationActive}
	err := h.TransitionToFallbackResumed()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")

	h2 := &HedgeState{Phase: HedgeSecondaryLaunched}
	err = h2.TransitionToFallbackResumed()
	assert.Error(t, err)

	h3 := &HedgeState{Phase: HedgePrimaryOnly}
	err = h3.TransitionToFallbackResumed()
	assert.Error(t, err)
}

func TestHedgeState_FullValidLifecycle(t *testing.T) {
	h := &HedgeState{
		Phase:                 HedgePrimaryOnly,
		PrimaryCandidateIndex: 0,
		SecondaryCandidateIndex: 1,
	}

	assert.NoError(t, h.TransitionToSecondaryLaunched())
	assert.Equal(t, HedgeSecondaryLaunched, h.Phase)
	assert.True(t, h.IsHedgeActive())

	assert.NoError(t, h.TransitionToObservationActive())
	assert.Equal(t, HedgeObservationActive, h.Phase)
	assert.True(t, h.IsObservationActive())

	assert.NoError(t, h.TransitionToWinnerReleased(0))
	assert.Equal(t, HedgeWinnerReleased, h.Phase)
	assert.Equal(t, 0, h.WinnerIndex)
	assert.Equal(t, 1, h.LoserIndex)

	assert.NoError(t, h.TransitionToLoserShadowing())
	assert.Equal(t, HedgeLoserShadowing, h.Phase)
	assert.True(t, h.IsShadowActive())

	assert.NoError(t, h.TransitionToShadowCompleted("completed"))
	assert.Equal(t, HedgeShadowCompleted, h.Phase)
	assert.Equal(t, "completed", h.ShadowCompletionReason)

	assert.NoError(t, h.TransitionToFallbackResumed())
	assert.Equal(t, HedgeFallbackResumed, h.Phase)
	assert.True(t, h.FallbackAllowed)
}

func TestPersistenceState_CanRetryStream_WithHedgeState(t *testing.T) {
	s := &PersistenceState{}
	assert.True(t, s.CanRetryStream())

	s.HedgeState = &HedgeState{Phase: HedgePrimaryOnly}
	assert.True(t, s.CanRetryStream())

	s.HedgeState.Phase = HedgeObservationActive
	assert.True(t, s.CanRetryStream())

	s.StreamReleaseState = ReleaseBuffering
	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState)
	assert.False(t, s.CanRetryStream())

	s.HedgeState.Phase = HedgeWinnerReleased
	assert.False(t, s.CanRetryStream(), "CanRetryStream should return false after MarkStreamReleased regardless of hedge state")
}

func TestPersistenceState_ReleaseInvariant_PreserveWithHedge(t *testing.T) {
	s := &PersistenceState{}
	s.HedgeState = &HedgeState{
		Phase:                 HedgeObservationActive,
		PrimaryCandidateIndex: 0,
		SecondaryCandidateIndex: 1,
	}

	s.StreamReleaseState = ReleaseBuffering
	s.MarkStreamReleased()
	assert.Equal(t, ReleaseEmitted, s.StreamReleaseState)
	assert.False(t, s.CanRetryStream())

	err := s.HedgeState.TransitionToWinnerReleased(0)
	assert.NoError(t, err)
	assert.Equal(t, HedgeWinnerReleased, s.HedgeState.Phase)
	assert.Equal(t, 0, s.HedgeState.WinnerIndex)

	assert.False(t, s.CanRetryStream(), "Release invariant: once released, retry must be forbidden even with hedge state")
}

func TestHedgeState_ThreadSafety(t *testing.T) {
	h := &HedgeState{Phase: HedgePrimaryOnly}
	done := make(chan bool)

	go func() {
		for i := 0; i < 100; i++ {
			h.TransitionToSecondaryLaunched()
			h.hedgeMu.Lock()
			h.Phase = HedgePrimaryOnly
			h.hedgeMu.Unlock()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = h.IsHedgeActive()
			_ = h.IsObservationActive()
			_ = h.IsShadowActive()
		}
		done <- true
	}()

	<-done
	<-done
	assert.Equal(t, HedgePrimaryOnly, h.Phase)
}