package orchestrator

import (
	"context"

	"go.uber.org/zap"

	"github.com/looplj/axonhub/internal/log"
)

// HedgeFallbackAction defines the action to take after a hedge race failure.
type HedgeFallbackAction int

const (
	// FallbackToRemaining indicates that the hedge race failed and we should
	// fall back to remaining candidates for the original slot.
	FallbackToRemaining HedgeFallbackAction = iota
	// ContinueWithSingleStream indicates that one stream succeeded and we should
	// continue with only that stream (the other failed during observation).
	ContinueWithSingleStream
	// NoAction indicates no fallback action is needed (e.g., winner already released,
	// loser failure during shadow mode has no impact on client).
	NoAction
	// RetrySameChannel indicates that a candidate failed but same-channel retry
	// is still available before declaring the candidate failed.
	RetrySameChannel
)

func (a HedgeFallbackAction) String() string {
	switch a {
	case FallbackToRemaining:
		return "FallbackToRemaining"
	case ContinueWithSingleStream:
		return "ContinueWithSingleStream"
	case NoAction:
		return "NoAction"
	case RetrySameChannel:
		return "RetrySameChannel"
	default:
		return "Unknown"
	}
}

// HedgeFallbackManager handles hedge failure scenarios and integrates with the
// existing retry and fallback machinery.
type HedgeFallbackManager struct {
	// MaxSingleChannelRetries is the maximum number of same-channel retries allowed
	// before declaring a candidate failed in the hedge race.
	MaxSingleChannelRetries int
	// retryCount tracks same-channel retry attempts per candidate in the current hedge race
	retryCount map[int]int // key: candidate index (0=primary, 1=secondary)
}

// NewHedgeFallbackManager creates a new HedgeFallbackManager.
func NewHedgeFallbackManager(maxSingleChannelRetries int) *HedgeFallbackManager {
	return &HedgeFallbackManager{
		MaxSingleChannelRetries: maxSingleChannelRetries,
		retryCount:              make(map[int]int),
	}
}

// Reset clears the retry count for a new hedge race.
func (m *HedgeFallbackManager) Reset() {
	m.retryCount = make(map[int]int)
}

// RecordRetry records a same-channel retry attempt for the given candidate index.
// Returns the total retry count after incrementing.
func (m *HedgeFallbackManager) RecordRetry(candidateIndex int) int {
	m.retryCount[candidateIndex]++
	return m.retryCount[candidateIndex]
}

// GetRetryCount returns the current retry count for the given candidate index.
func (m *HedgeFallbackManager) GetRetryCount(candidateIndex int) int {
	return m.retryCount[candidateIndex]
}

// CanRetrySameChannel returns true if the given candidate can be retried on the same channel.
func (m *HedgeFallbackManager) CanRetrySameChannel(candidateIndex int) bool {
	return m.retryCount[candidateIndex] < m.MaxSingleChannelRetries
}

// HandleHedgeFailure handles all hedge failure scenarios and returns the appropriate action.
// It integrates with PersistenceState to respect CanRetryStream() invariant.
func (m *HedgeFallbackManager) HandleHedgeFailure(
	ctx context.Context,
	raceResult *HedgeRaceResult,
	state *PersistenceState,
) HedgeFallbackAction {
	if raceResult == nil {
		log.Warn(ctx, "HandleHedgeFailure called with nil raceResult")
		return FallbackToRemaining
	}

	// Case 7: BothFailed - return FallbackToRemaining
	if raceResult.BothFailed {
		log.Debug(ctx, "Hedge failure: both streams failed during observation",
			zap.Bool("both_failed", raceResult.BothFailed))
		return FallbackToRemaining
	}

	// Case 4 & 5: Winner already released (post-release phase) - NoAction
	// At this point, the winner has been released to the client.
	// If the loser fails during shadow, it's recorded but has no impact on client.
	if state != nil && state.HedgeState != nil {
		if state.HedgeState.Phase == HedgeWinnerReleased || state.HedgeState.Phase == HedgeLoserShadowing {
			log.Debug(ctx, "Hedge failure: winner already released, shadow handles loser failure",
				zap.String("phase", state.HedgeState.Phase.String()))
			return NoAction
		}
	}

	// Determine which candidate failed and which succeeded
	// WinnerIndex = 0 means primary won, 1 means secondary won
	primaryFailed := raceResult.LoserIndex == 0
	secondaryFailed := raceResult.LoserIndex == 1

	// Case 2: Primary (A) fails during observation while Secondary (B) is active
	// Winner is secondary, continue with single stream
	if primaryFailed && !secondaryFailed {
		log.Debug(ctx, "Hedge failure: primary failed during observation, continuing with secondary",
			zap.Int("winner_index", raceResult.WinnerIndex))
		return ContinueWithSingleStream
	}

	// Case 3: Secondary (B) fails during observation while Primary (A) is active
	// Winner is primary, continue with single stream
	if secondaryFailed && !primaryFailed {
		log.Debug(ctx, "Hedge failure: secondary failed during observation, continuing with primary",
			zap.Int("winner_index", raceResult.WinnerIndex))
		return ContinueWithSingleStream
	}

	// Case 1: A fails before B starts - this is handled by the race result
	// where both streams didn't produce tokens, but the hedge coordinator
	// already returned BothFailed in that case. If we get here with one
	// winner, it means one stream succeeded before the other failed.

	// Case 6: No distinct secondary candidate - Skip hedge, proceed with single-channel
	// This is handled at candidate selection time (HedgeCandidateSet would be nil)

	// Default: if we have a winner, continue with it
	if raceResult.WinnerIndex >= 0 {
		return ContinueWithSingleStream
	}

	return FallbackToRemaining
}

// GetFallbackCandidates returns the remaining candidates for fallback after hedge race fails.
// It uses HedgeCandidateSet.Remaining if available, otherwise falls back to normal candidate
// selection from ChannelModelsCandidates starting at the hedge candidates' positions.
func (m *HedgeFallbackManager) GetFallbackCandidates(state *PersistenceState) []*ChannelModelsCandidate {
	if state == nil {
		return nil
	}

	// If we have hedge candidates with remaining, use those
	if state.HedgeCandidates != nil && len(state.HedgeCandidates.Remaining) > 0 {
		log.Debug(context.Background(), "Using remaining candidates from hedge set",
			zap.Int("remaining_count", len(state.HedgeCandidates.Remaining)))
		return state.HedgeCandidates.Remaining
	}

	// Fallback: reconstruct remaining from ChannelModelsCandidates
	// Skip indices that were used for hedge (primary and secondary)
	if state.HedgeCandidates != nil {
		var remaining []*ChannelModelsCandidate

		// Find the indices of primary and secondary in ChannelModelsCandidates
		primaryIdx := -1
		secondaryIdx := -1

		for i, c := range state.ChannelModelsCandidates {
			if c == nil {
				continue
			}
			if state.HedgeCandidates.Primary != nil && c.Channel.ID == state.HedgeCandidates.Primary.Channel.ID {
				if primaryIdx == -1 {
					primaryIdx = i
				}
			}
			if state.HedgeCandidates.Secondary != nil && c.Channel.ID == state.HedgeCandidates.Secondary.Channel.ID {
				if secondaryIdx == -1 {
					secondaryIdx = i
				}
			}
		}

		// Collect candidates after primary and secondary
		for i, c := range state.ChannelModelsCandidates {
			if c == nil {
				continue
			}
			// Skip primary and secondary indices
			if i == primaryIdx || i == secondaryIdx {
				continue
			}
			remaining = append(remaining, c)
		}

		if len(remaining) > 0 {
			log.Debug(context.Background(), "Reconstructed remaining candidates from ChannelModelsCandidates",
				zap.Int("remaining_count", len(remaining)))
			return remaining
		}
	}

	// No hedge candidates - return nil (no fallback available)
	return nil
}

// ShouldSkipHedge returns true if hedge should be skipped due to insufficient candidates.
func (m *HedgeFallbackManager) ShouldSkipHedge(state *PersistenceState) bool {
	if state == nil {
		return true
	}

	// No hedge candidates means hedge should be skipped
	if state.HedgeCandidates == nil {
		return true
	}

	// Need at least primary and secondary for hedge
	if state.HedgeCandidates.Primary == nil || state.HedgeCandidates.Secondary == nil {
		return true
	}

	// Check if we have at least 2 distinct candidates
	if state.HedgeCandidates.Primary.Channel.ID == state.HedgeCandidates.Secondary.Channel.ID {
		return true
	}

	return false
}

// AdvanceToNextFallbackCandidate advances the CurrentCandidateIndex to use the next
// remaining candidate from the hedge fallback set.
// Returns true if advancement succeeded, false if no more fallback candidates.
func (m *HedgeFallbackManager) AdvanceToNextFallbackCandidate(state *PersistenceState) bool {
	if state == nil {
		return false
	}

	remaining := m.GetFallbackCandidates(state)
	if len(remaining) == 0 {
		return false
	}

	// Find the next remaining candidate after current position
	currentIdx := state.CurrentCandidateIndex

	for _, candidate := range remaining {
		// Find this candidate's index in ChannelModelsCandidates
		for i, c := range state.ChannelModelsCandidates {
			if c != nil && c.Channel.ID == candidate.Channel.ID {
				if i > currentIdx {
					state.CurrentCandidateIndex = i
					state.CurrentCandidate = c
					state.CurrentModelIndex = 0
					state.RequestExec = nil // Reset for new candidate
					log.Debug(context.Background(), "Advanced to fallback candidate",
						zap.Int("new_candidate_index", i),
						zap.String("channel", c.Channel.Name))
					return true
				}
			}
		}
	}

	return false
}