package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestHedgeFallbackManager_HandleHedgeFailure_BothFailed(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  true,
		WinnerIndex: -1,
		LoserIndex:  -1,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, FallbackToRemaining, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_PrimaryFailsDuringObservation(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  false,
		WinnerIndex: 1,
		LoserIndex:  0,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{
			Phase: HedgeObservationActive,
		},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, ContinueWithSingleStream, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_SecondaryFailsDuringObservation(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  false,
		WinnerIndex: 0,
		LoserIndex:  1,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{
			Phase: HedgeObservationActive,
		},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, ContinueWithSingleStream, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_WinnerReleased_LoserShadowing(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  false,
		WinnerIndex: 0,
		LoserIndex:  1,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{
			Phase: HedgeWinnerReleased,
		},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, NoAction, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_LoserFailsDuringShadow(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  false,
		WinnerIndex: 0,
		LoserIndex:  1,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{
			Phase: HedgeLoserShadowing,
		},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, NoAction, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_NilRaceResult(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	state := &PersistenceState{}

	action := m.HandleHedgeFailure(ctx, nil, state)

	assert.Equal(t, FallbackToRemaining, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_NilState(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  true,
		WinnerIndex: -1,
		LoserIndex:  -1,
	}

	action := m.HandleHedgeFailure(ctx, raceResult, nil)

	assert.Equal(t, FallbackToRemaining, action)
}

func makeTestChannel(id int, name string) *biz.Channel {
	return &biz.Channel{
		Channel: &ent.Channel{
			ID:   id,
			Name: name,
		},
	}
}

func TestHedgeFallbackManager_GetFallbackCandidates_WithRemaining(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	primary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(1, "primary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	secondary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(2, "secondary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	remaining := &ChannelModelsCandidate{
		Channel:  makeTestChannel(3, "remaining"),
		Priority: 1,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{primary, secondary, remaining},
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   primary,
			Secondary: secondary,
			Remaining: []*ChannelModelsCandidate{remaining},
		},
	}

	candidates := m.GetFallbackCandidates(state)

	require.Len(t, candidates, 1)
	assert.Equal(t, 3, candidates[0].Channel.ID)
}

func TestHedgeFallbackManager_GetFallbackCandidates_ReconstructsFromChannelModels(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	primary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(1, "primary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	secondary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(2, "secondary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	third := &ChannelModelsCandidate{
		Channel:  makeTestChannel(3, "third"),
		Priority: 1,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	fourth := &ChannelModelsCandidate{
		Channel:  makeTestChannel(4, "fourth"),
		Priority: 2,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{primary, secondary, third, fourth},
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   primary,
			Secondary: secondary,
			Remaining: nil,
		},
	}

	candidates := m.GetFallbackCandidates(state)

	require.Len(t, candidates, 2)
	assert.Equal(t, 3, candidates[0].Channel.ID)
	assert.Equal(t, 4, candidates[1].Channel.ID)
}

func TestHedgeFallbackManager_GetFallbackCandidates_NilState(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	candidates := m.GetFallbackCandidates(nil)

	assert.Nil(t, candidates)
}

func TestHedgeFallbackManager_ShouldSkipHedge_NoHedgeCandidates(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	state := &PersistenceState{
		HedgeCandidates: nil,
	}

	assert.True(t, m.ShouldSkipHedge(state))
}

func TestHedgeFallbackManager_ShouldSkipHedge_MissingPrimary(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	secondary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(2, "secondary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   nil,
			Secondary: secondary,
		},
	}

	assert.True(t, m.ShouldSkipHedge(state))
}

func TestHedgeFallbackManager_ShouldSkipHedge_MissingSecondary(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	primary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(1, "primary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   primary,
			Secondary: nil,
		},
	}

	assert.True(t, m.ShouldSkipHedge(state))
}

func TestHedgeFallbackManager_ShouldSkipHedge_SameChannel(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	ch := makeTestChannel(1, "same")
	primary := &ChannelModelsCandidate{
		Channel:  ch,
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	secondary := &ChannelModelsCandidate{
		Channel:  ch,
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   primary,
			Secondary: secondary,
		},
	}

	assert.True(t, m.ShouldSkipHedge(state))
}

func TestHedgeFallbackManager_ShouldSkipHedge_ValidDistinct(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	primary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(1, "primary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	secondary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(2, "secondary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   primary,
			Secondary: secondary,
		},
	}

	assert.False(t, m.ShouldSkipHedge(state))
}

func TestHedgeFallbackManager_ShouldSkipHedge_NilState(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	assert.True(t, m.ShouldSkipHedge(nil))
}

func TestHedgeFallbackManager_CanRetrySameChannel(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	assert.True(t, m.CanRetrySameChannel(0))
	assert.True(t, m.CanRetrySameChannel(1))

	m.RecordRetry(0)
	assert.True(t, m.CanRetrySameChannel(0))

	m.RecordRetry(0)
	assert.False(t, m.CanRetrySameChannel(0))
	assert.True(t, m.CanRetrySameChannel(1))
}

func TestHedgeFallbackManager_RecordRetry(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	count := m.RecordRetry(0)
	assert.Equal(t, 1, count)

	count = m.RecordRetry(0)
	assert.Equal(t, 2, count)

	count = m.RecordRetry(1)
	assert.Equal(t, 1, count)
}

func TestHedgeFallbackManager_GetRetryCount(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	assert.Equal(t, 0, m.GetRetryCount(0))

	m.RecordRetry(0)
	m.RecordRetry(0)
	m.RecordRetry(1)

	assert.Equal(t, 2, m.GetRetryCount(0))
	assert.Equal(t, 1, m.GetRetryCount(1))
}

func TestHedgeFallbackManager_Reset(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	m.RecordRetry(0)
	m.RecordRetry(1)

	m.Reset()

	assert.Equal(t, 0, m.GetRetryCount(0))
	assert.Equal(t, 0, m.GetRetryCount(1))
}

func TestHedgeFallbackManager_AdvanceToNextFallbackCandidate(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	primary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(1, "primary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	secondary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(2, "secondary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	third := &ChannelModelsCandidate{
		Channel:  makeTestChannel(3, "third"),
		Priority: 1,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{primary, secondary, third},
		CurrentCandidateIndex:  0,
		CurrentCandidate:       primary,
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   primary,
			Secondary: secondary,
			Remaining: []*ChannelModelsCandidate{third},
		},
	}

	success := m.AdvanceToNextFallbackCandidate(state)

	assert.True(t, success)
	assert.Equal(t, 2, state.CurrentCandidateIndex)
	assert.Equal(t, 3, state.CurrentCandidate.Channel.ID)
	assert.Equal(t, 0, state.CurrentModelIndex)
	assert.Nil(t, state.RequestExec)
}

func TestHedgeFallbackManager_AdvanceToNextFallbackCandidate_NoRemaining(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	primary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(1, "primary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}
	secondary := &ChannelModelsCandidate{
		Channel:  makeTestChannel(2, "secondary"),
		Priority: 0,
		Models:   []biz.ChannelModelEntry{{ActualModel: "gpt-4"}},
	}

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{primary, secondary},
		CurrentCandidateIndex:  0,
		HedgeCandidates: &HedgeCandidateSet{
			Primary:   primary,
			Secondary: secondary,
			Remaining: nil,
		},
	}

	success := m.AdvanceToNextFallbackCandidate(state)

	assert.False(t, success)
}

func TestHedgeFallbackManager_AdvanceToNextFallbackCandidate_NilState(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	success := m.AdvanceToNextFallbackCandidate(nil)

	assert.False(t, success)
}

func TestHedgeFallbackAction_String(t *testing.T) {
	assert.Equal(t, "FallbackToRemaining", FallbackToRemaining.String())
	assert.Equal(t, "ContinueWithSingleStream", ContinueWithSingleStream.String())
	assert.Equal(t, "NoAction", NoAction.String())
	assert.Equal(t, "RetrySameChannel", RetrySameChannel.String())
	assert.Equal(t, "Unknown", HedgeFallbackAction(999).String())
}

func TestHedgeFallbackManager_HandleHedgeFailure_WinnerAlreadySelected(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  false,
		WinnerIndex: 0,
		LoserIndex:  1,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{
			Phase: HedgeShadowCompleted,
		},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, ContinueWithSingleStream, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_DefaultWithWinner(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  false,
		WinnerIndex: 0,
		LoserIndex:  1,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{
			Phase: HedgeDisabled,
		},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, ContinueWithSingleStream, action)
}

func TestHedgeFallbackManager_HandleHedgeFailure_DefaultWithNoWinner(t *testing.T) {
	m := NewHedgeFallbackManager(2)
	ctx := context.Background()

	raceResult := &HedgeRaceResult{
		BothFailed:  false,
		WinnerIndex: -1,
		LoserIndex:  -1,
	}

	state := &PersistenceState{
		HedgeState: &HedgeState{
			Phase: HedgeDisabled,
		},
	}

	action := m.HandleHedgeFailure(ctx, raceResult, state)

	assert.Equal(t, FallbackToRemaining, action)
}

func TestHedgeFallbackManager_GetFallbackCandidates_NilHedgeCandidates(t *testing.T) {
	m := NewHedgeFallbackManager(2)

	state := &PersistenceState{
		ChannelModelsCandidates: []*ChannelModelsCandidate{
			{Channel: makeTestChannel(1, "ch1")},
			{Channel: makeTestChannel(2, "ch2")},
		},
		HedgeCandidates: nil,
	}

	candidates := m.GetFallbackCandidates(state)

	assert.Nil(t, candidates)
}