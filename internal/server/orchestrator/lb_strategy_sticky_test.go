package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestStickyRoutingStrategy_Name(t *testing.T) {
	strategy := NewStickyRoutingStrategy()
	assert.Equal(t, "StickyRouting", strategy.Name())
}

func TestStickyRoutingStrategy_Score_NoStickyKey(t *testing.T) {
	ctx := context.Background()
	strategy := NewStickyRoutingStrategy()

	channel := &biz.Channel{
		Channel: &ent.Channel{ID: 1, Name: "test"},
	}

	score := strategy.Score(ctx, channel)
	assert.Equal(t, 0.0, score, "Should return 0 when no sticky key in context")
}

func TestStickyRoutingStrategy_Score_EmptyStickyKey(t *testing.T) {
	ctx := contexts.WithStickyKey(context.Background(), "")
	strategy := NewStickyRoutingStrategy()

	channel := &biz.Channel{
		Channel: &ent.Channel{ID: 1, Name: "test"},
	}

	score := strategy.Score(ctx, channel)
	assert.Equal(t, 0.0, score, "Should return 0 when empty sticky key in context")
}

func TestStickyRoutingStrategy_Score_NoCandidates(t *testing.T) {
	ctx := contexts.WithStickyKey(context.Background(), "test-key")
	strategy := NewStickyRoutingStrategy()

	channel := &biz.Channel{
		Channel: &ent.Channel{ID: 1, Name: "test"},
	}

	score := strategy.Score(ctx, channel)
	assert.Equal(t, 0.0, score, "Should return 0 when no candidates prepared")
}

func TestStickyRoutingStrategy_Score_MatchingChannel(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}
	strategy.PrepareCandidates(candidates)

	// Find which channel "test-key" maps to
	targetChannelID := strategy.ring.getChannelID("test-key")

	// Create context with sticky key
	ctx := contexts.WithStickyKey(context.Background(), "test-key")

	// Test the target channel - should get 900
	targetChannel := &biz.Channel{Channel: &ent.Channel{ID: targetChannelID, Name: fmt.Sprintf("ch%d", targetChannelID)}}
	score := strategy.Score(ctx, targetChannel)
	assert.Equal(t, 900.0, score, "Should return 900 for matching channel")
}

func TestStickyRoutingStrategy_Score_NonMatchingChannel(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}
	strategy.PrepareCandidates(candidates)

	// Find which channel "test-key" maps to
	targetChannelID := strategy.ring.getChannelID("test-key")

	// Create context with sticky key
	ctx := contexts.WithStickyKey(context.Background(), "test-key")

	// Test a non-target channel - should get 0
	nonTargetID := 1
	if nonTargetID == targetChannelID {
		nonTargetID = 2
	}
	nonTargetChannel := &biz.Channel{Channel: &ent.Channel{ID: nonTargetID, Name: fmt.Sprintf("ch%d", nonTargetID)}}
	score := strategy.Score(ctx, nonTargetChannel)
	assert.Equal(t, 0.0, score, "Should return 0 for non-matching channel")
}

func TestStickyRoutingStrategy_ScoreWithDebug_NoStickyKey(t *testing.T) {
	ctx := context.Background()
	strategy := NewStickyRoutingStrategy()

	channel := &biz.Channel{
		Channel: &ent.Channel{ID: 1, Name: "test"},
	}

	score, strategyScore := strategy.ScoreWithDebug(ctx, channel)
	assert.Equal(t, 0.0, score)
	assert.Equal(t, "StickyRouting", strategyScore.StrategyName)
	assert.Equal(t, 0.0, strategyScore.Score)
	assert.Equal(t, "no_sticky_key_in_context", strategyScore.Details["reason"])
}

func TestStickyRoutingStrategy_ScoreWithDebug_EmptyStickyKey(t *testing.T) {
	ctx := contexts.WithStickyKey(context.Background(), "")
	strategy := NewStickyRoutingStrategy()

	channel := &biz.Channel{
		Channel: &ent.Channel{ID: 1, Name: "test"},
	}

	score, strategyScore := strategy.ScoreWithDebug(ctx, channel)
	assert.Equal(t, 0.0, score)
	assert.Equal(t, "no_sticky_key_in_context", strategyScore.Details["reason"])
}

func TestStickyRoutingStrategy_ScoreWithDebug_NoCandidates(t *testing.T) {
	ctx := contexts.WithStickyKey(context.Background(), "test-key")
	strategy := NewStickyRoutingStrategy()

	channel := &biz.Channel{
		Channel: &ent.Channel{ID: 1, Name: "test"},
	}

	score, strategyScore := strategy.ScoreWithDebug(ctx, channel)
	assert.Equal(t, 0.0, score)
	assert.Equal(t, "ring_not_initialized", strategyScore.Details["reason"])
	assert.Equal(t, "test-key", strategyScore.Details["sticky_key"])
}

func TestStickyRoutingStrategy_ScoreWithDebug_MatchingChannel(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}
	strategy.PrepareCandidates(candidates)

	// Find which channel "test-key" maps to
	targetChannelID := strategy.ring.getChannelID("test-key")

	ctx := contexts.WithStickyKey(context.Background(), "test-key")
	targetChannel := &biz.Channel{Channel: &ent.Channel{ID: targetChannelID, Name: fmt.Sprintf("ch%d", targetChannelID)}}

	score, strategyScore := strategy.ScoreWithDebug(ctx, targetChannel)
	assert.Equal(t, 900.0, score)
	assert.Equal(t, 900.0, strategyScore.Score)
	assert.Equal(t, true, strategyScore.Details["is_target"])
	assert.Equal(t, "sticky_key_matches_channel", strategyScore.Details["reason"])
	assert.NotNil(t, strategyScore.Details["sticky_key"])
	assert.NotNil(t, strategyScore.Details["target_channel_id"])
	assert.NotNil(t, strategyScore.Details["current_channel_id"])
}

func TestStickyRoutingStrategy_ScoreWithDebug_NonMatchingChannel(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}
	strategy.PrepareCandidates(candidates)

	// Find which channel "test-key" maps to
	targetChannelID := strategy.ring.getChannelID("test-key")

	ctx := contexts.WithStickyKey(context.Background(), "test-key")

	// Use a non-matching channel
	nonTargetID := 1
	if nonTargetID == targetChannelID {
		nonTargetID = 2
	}
	nonTargetChannel := &biz.Channel{Channel: &ent.Channel{ID: nonTargetID, Name: fmt.Sprintf("ch%d", nonTargetID)}}

	score, strategyScore := strategy.ScoreWithDebug(ctx, nonTargetChannel)
	assert.Equal(t, 0.0, score)
	assert.Equal(t, 0.0, strategyScore.Score)
	assert.Equal(t, false, strategyScore.Details["is_target"])
	assert.Equal(t, "sticky_key_does_not_match_channel", strategyScore.Details["reason"])
}

func TestStickyRoutingStrategy_ScoreConsistency(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}
	strategy.PrepareCandidates(candidates)

	testCases := []struct {
		name      string
		stickyKey string
		channelID int
	}{
		{
			name:      "no sticky key",
			stickyKey: "",
			channelID: 1,
		},
		{
			name:      "matching channel",
			stickyKey: "consistent-key-123",
			channelID: 1,
		},
		{
			name:      "non-matching channel",
			stickyKey: "consistent-key-456",
			channelID: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var ctx context.Context
			if tc.stickyKey != "" {
				ctx = contexts.WithStickyKey(context.Background(), tc.stickyKey)
			} else {
				ctx = context.Background()
			}

			channel := &biz.Channel{
				Channel: &ent.Channel{ID: tc.channelID, Name: "test"},
			}

			score := strategy.Score(ctx, channel)
			debugScore, _ := strategy.ScoreWithDebug(ctx, channel)

			assert.Equal(t, score, debugScore,
				"Score and ScoreWithDebug must return identical scores for %s", tc.name)
		})
	}
}

func TestStickyRoutingStrategy_PrepareCandidates_RebuildsOnChange(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	// Initial candidates
	candidates1 := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
	}
	strategy.PrepareCandidates(candidates1)

	// Get the hash after first preparation
	hash1 := strategy.lastCandidatesHash
	require.NotZero(t, hash1, "Hash should be set after first preparation")

	// Same candidates should not rebuild (same hash)
	candidates1Again := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
	}
	strategy.PrepareCandidates(candidates1Again)
	assert.Equal(t, hash1, strategy.lastCandidatesHash, "Hash should not change for same candidates")

	// Different candidates should rebuild (different hash)
	candidates2 := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 4, Name: "ch4"}}},
	}
	strategy.PrepareCandidates(candidates2)
	hash2 := strategy.lastCandidatesHash
	require.NotEqual(t, hash1, hash2, "Hash should change for different candidates")

	// Verify the ring was rebuilt - it should have 300 nodes (2 channels * 150)
	assert.Equal(t, 300, len(strategy.ring.nodes), "Ring should have 2 channels * 150 virtual nodes")
}

func TestStickyRoutingStrategy_PrepareCandidates_NoRebuildWhenSame(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	// Prepare with same candidates twice
	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}

	strategy.PrepareCandidates(candidates)
	hash1 := strategy.lastCandidatesHash
	nodeCount1 := len(strategy.ring.nodes)

	strategy.PrepareCandidates(candidates)
	hash2 := strategy.lastCandidatesHash
	nodeCount2 := len(strategy.ring.nodes)

	assert.Equal(t, hash1, hash2, "Hash should be equal for same candidates")
	assert.Equal(t, nodeCount1, nodeCount2, "Ring node count should be equal for same candidates")
	assert.Equal(t, 450, nodeCount1, "Should have 150 virtual nodes per channel (3 channels = 450 nodes)")
}

func TestStickyRoutingStrategy_Distribution(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}
	strategy.PrepareCandidates(candidates)

	counts := map[int]int{1: 0, 2: 0, 3: 0}
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("%016x", i*1234567)
		ctx := contexts.WithStickyKey(context.Background(), key)

		for _, c := range candidates {
			score := strategy.Score(ctx, c.Channel)
			if score > 0 {
				counts[c.Channel.ID]++
				break
			}
		}
	}

	// Verify distribution sanity:
	// 1. All keys must be assigned
	total := counts[1] + counts[2] + counts[3]
	assert.Equal(t, 1000, total, "All keys should be assigned to a channel")

	// 2. Each channel must get at least some keys (sanity check, not all in one)
	for id, count := range counts {
		assert.Greater(t, count, 0, "Channel %d should get at least some keys", id)
		assert.Less(t, count, 900, "Channel %d should not get almost all keys", id)
	}
}

func TestStickyRoutingStrategy_Distribution_LargeKeySpace(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 4, Name: "ch4"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 5, Name: "ch5"}}},
	}
	strategy.PrepareCandidates(candidates)

	counts := map[int]int{1: 0, 2: 0, 3: 0, 4: 0, 5: 0}
	for i := 0; i < 5000; i++ {
		key := fmt.Sprintf("%016x", i*7890123)
		ctx := contexts.WithStickyKey(context.Background(), key)

		for _, c := range candidates {
			score := strategy.Score(ctx, c.Channel)
			if score > 0 {
				counts[c.Channel.ID]++
				break
			}
		}
	}

	// Verify distribution sanity:
	// 1. All keys must be assigned
	total := counts[1] + counts[2] + counts[3] + counts[4] + counts[5]
	assert.Equal(t, 5000, total, "All keys should be assigned")

	// 2. Each channel must get at least some keys
	for id, count := range counts {
		assert.Greater(t, count, 0, "Channel %d should get at least some keys", id)
		assert.Less(t, count, 4500, "Channel %d should not get almost all keys", id)
	}
}

func TestStickyRoutingStrategy_SameKeySameChannel(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
	}
	strategy.PrepareCandidates(candidates)

	// Same sticky key should always map to same channel
	key := "consistent-user-session"
	ctx := contexts.WithStickyKey(context.Background(), key)

	var targetChannelID int
	for _, c := range candidates {
		score := strategy.Score(ctx, c.Channel)
		if score > 0 {
			targetChannelID = c.Channel.ID
			break
		}
	}

	require.NotZero(t, targetChannelID, "Should find a target channel for the key")

	// Repeat 100 times - should always get the same channel
	for i := 0; i < 100; i++ {
		ctx := contexts.WithStickyKey(context.Background(), key)
		found := false
		for _, c := range candidates {
			score := strategy.Score(ctx, c.Channel)
			if score > 0 {
				assert.Equal(t, targetChannelID, c.Channel.ID, "Same key should always map to same channel")
				found = true
				break
			}
		}
		assert.True(t, found, "Should always find a target channel")
	}
}

func TestStickyRoutingStrategy_WithTraceContext(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
	}
	strategy.PrepareCandidates(candidates)

	// Create context with trace and sticky key
	ctx := context.Background()
	ctx = contexts.WithTraceID(ctx, "trace-123")
	ctx = contexts.WithStickyKey(ctx, "sticky-key-456")

	// Verify both are accessible
	traceID, ok := contexts.GetTraceID(ctx)
	assert.True(t, ok)
	assert.Equal(t, "trace-123", traceID)

	stickyKey, ok := contexts.GetStickyKey(ctx)
	assert.True(t, ok)
	assert.Equal(t, "sticky-key-456", stickyKey)

	// Score should work with combined context
	channel := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}
	score := strategy.Score(ctx, channel)
	// Score depends on whether channel 1 is the target for "sticky-key-456"
	assert.True(t, score == 0.0 || score == 900.0, "Score should be either 0 or 900")
}

func TestStickyRoutingStrategy_VirtualNodeCount(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	// Single channel
	candidates1 := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
	}
	strategy.PrepareCandidates(candidates1)
	assert.Equal(t, 150, len(strategy.ring.nodes), "Single channel should have 150 virtual nodes")

	// Two channels
	candidates2 := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
	}
	strategy.PrepareCandidates(candidates2)
	assert.Equal(t, 300, len(strategy.ring.nodes), "Two channels should have 300 virtual nodes")

	// Ten channels
	candidates10 := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 4, Name: "ch4"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 5, Name: "ch5"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 6, Name: "ch6"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 7, Name: "ch7"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 8, Name: "ch8"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 9, Name: "ch9"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 10, Name: "ch10"}}},
	}
	strategy.PrepareCandidates(candidates10)
	assert.Equal(t, 1500, len(strategy.ring.nodes), "Ten channels should have 1500 virtual nodes")
}

func TestStickyRoutingStrategy_EmptyCandidates(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{}
	strategy.PrepareCandidates(candidates)

	assert.Equal(t, 0, len(strategy.ring.nodes), "Empty candidates should result in empty ring")

	ctx := contexts.WithStickyKey(context.Background(), "test-key")
	channel := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}

	score := strategy.Score(ctx, channel)
	assert.Equal(t, 0.0, score, "Should return 0 with empty ring")
}

func TestStickyRoutingStrategy_RingSorted(t *testing.T) {
	strategy := NewStickyRoutingStrategy()

	candidates := []*ChannelModelsCandidate{
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}},
	}
	strategy.PrepareCandidates(candidates)

	// Verify nodes are sorted by hash
	for i := 1; i < len(strategy.ring.nodes); i++ {
		assert.Less(t, strategy.ring.nodes[i-1].hash, strategy.ring.nodes[i].hash,
			"Ring nodes should be sorted by hash")
	}
}