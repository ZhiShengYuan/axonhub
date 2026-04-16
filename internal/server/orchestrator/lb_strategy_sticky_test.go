package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestConsistentHashRing_Empty(t *testing.T) {
	ring := NewConsistentHashRing()
	assert.Equal(t, 0, ring.Get("any-key"), "Empty ring should return 0")
}

func TestConsistentHashRing_SingleChannel(t *testing.T) {
	ring := NewConsistentHashRing()
	ring.Rebuild([]int{1})

	for _, key := range []string{"a", "b", "c", "trace:123", "tenant:key:model"} {
		assert.Equal(t, 1, ring.Get(key), "Single channel ring should always return channel 1")
	}
}

func TestConsistentHashRing_MultipleChannels(t *testing.T) {
	ring := NewConsistentHashRing()
	ring.Rebuild([]int{1, 2, 3})

	channelHits := make(map[int]int)
	for i := 0; i < 1000; i++ {
		key := string(rune(i))
		ch := ring.Get(key)
		assert.Contains(t, []int{1, 2, 3}, ch, "Channel should be one of the configured channels")
		channelHits[ch]++
	}

	for _, chID := range []int{1, 2, 3} {
		assert.Greater(t, channelHits[chID], 100, "Channel %d should get a reasonable share of keys", chID)
	}
}

func TestConsistentHashRing_Consistency(t *testing.T) {
	ring := NewConsistentHashRing()
	ring.Rebuild([]int{1, 2, 3})

	key := "trace:abc-123"
	expected := ring.Get(key)
	for i := 0; i < 100; i++ {
		assert.Equal(t, expected, ring.Get(key), "Same key should consistently map to the same channel")
	}
}

func TestConsistentHashRing_MinimalRemapping(t *testing.T) {
	ring := NewConsistentHashRing()
	ring.Rebuild([]int{1, 2, 3})

	before := make(map[string]int)
	for i := 0; i < 500; i++ {
		key := string(rune(i))
		before[key] = ring.Get(key)
	}

	ring.Rebuild([]int{1, 2, 3, 4})

	remapped := 0
	for i := 0; i < 500; i++ {
		key := string(rune(i))
		if ring.Get(key) != before[key] {
			remapped++
		}
	}

	assert.Less(t, remapped, 200, "Adding a channel should remap at most ~25%% of keys, got %d/500", remapped)
}

func TestConsistentHashRing_RemoveChannel(t *testing.T) {
	ring := NewConsistentHashRing()
	ring.Rebuild([]int{1, 2, 3})

	before := make(map[string]int)
	for i := 0; i < 500; i++ {
		key := string(rune(i))
		before[key] = ring.Get(key)
	}

	ring.Rebuild([]int{1, 3})

	remapped := 0
	for i := 0; i < 500; i++ {
		key := string(rune(i))
		ch := ring.Get(key)
		assert.Contains(t, []int{1, 3}, ch, "After removing channel 2, all keys should map to 1 or 3")
		if ch != before[key] {
			remapped++
		}
	}

	assert.Less(t, remapped, 250, "Removing a channel should remap at most ~33%% of keys, got %d/500", remapped)
}

func TestConsistentHashRing_RebuildIdempotent(t *testing.T) {
	ring := NewConsistentHashRing()
	ring.Rebuild([]int{1, 2, 3})

	before := make(map[string]int)
	for i := 0; i < 100; i++ {
		key := string(rune(i))
		before[key] = ring.Get(key)
	}

	ring.Rebuild([]int{1, 2, 3})

	for i := 0; i < 100; i++ {
		key := string(rune(i))
		assert.Equal(t, before[key], ring.Get(key), "Rebuilding with same channels should not change mappings")
	}
}

func TestConsistentHashRing_Distribution(t *testing.T) {
	ring := NewConsistentHashRing()
	channelIDs := []int{1, 2, 3, 4, 5}
	ring.Rebuild(channelIDs)

	channelHits := make(map[int]int)
	for i := 0; i < 10000; i++ {
		key := string(rune(i))
		ch := ring.Get(key)
		channelHits[ch]++
	}

	for _, chID := range channelIDs {
		share := float64(channelHits[chID]) / 10000.0
		assert.Greater(t, share, 0.10, "Channel %d share should be > 10%%, got %.2f%%", chID, share*100)
		assert.Less(t, share, 0.30, "Channel %d share should be < 30%%, got %.2f%%", chID, share*100)
	}
}

func TestStickyRoutingStrategy_TraceEntityKey(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}
	strategy := NewStickyRoutingStrategy(mockProvider)

	ctx = contexts.WithTrace(ctx, &ent.Trace{ID: 123, TraceID: "trace-abc"})

	channel1 := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}
	channel2 := &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}

	score1 := strategy.Score(ctx, channel1)
	score2 := strategy.Score(ctx, channel2)

	assert.Equal(t, defaultStickyBoostScore, score1+score2, "Exactly one channel should get the boost score")

	score1Again := strategy.Score(ctx, channel1)
	assert.Equal(t, score1, score1Again, "Same trace should consistently route to the same channel")
}

func TestStickyRoutingStrategy_TraceIDStringKey(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}
	strategy := NewStickyRoutingStrategy(mockProvider)

	ctx = contexts.WithTraceID(ctx, "trace-xyz-789")

	channel1 := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}
	channel2 := &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}

	score1 := strategy.Score(ctx, channel1)
	score2 := strategy.Score(ctx, channel2)

	assert.Equal(t, defaultStickyBoostScore, score1+score2, "Exactly one channel should get the boost score")
}

func TestStickyRoutingStrategy_ThreadKey(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}
	strategy := NewStickyRoutingStrategy(mockProvider)

	ctx = contexts.WithThread(ctx, &ent.Thread{ID: 456, ThreadID: "thread-def"})

	channel1 := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}
	channel2 := &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}

	score1 := strategy.Score(ctx, channel1)
	score2 := strategy.Score(ctx, channel2)

	assert.Equal(t, defaultStickyBoostScore, score1+score2, "Exactly one channel should get the boost score")
}

func TestStickyRoutingStrategy_TenantKeyNotSticky(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}
	strategy := NewStickyRoutingStrategy(mockProvider)

	ctx = contexts.WithAPIKey(ctx, &ent.APIKey{Key: "sk-test-key-123"})
	ctx = contextWithRequestedModel(ctx, "gpt-4")

	channel1 := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}

	score := strategy.Score(ctx, channel1)
	assert.Equal(t, 0.0, score, "API key + model should NOT create stickiness (kills parallel throughput)")
}

func TestStickyRoutingStrategy_CustomStickyKey(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}
	strategy := NewStickyRoutingStrategy(mockProvider)

	ctx = contexts.WithStickyKey(ctx, "user-session-abc")

	channel1 := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}
	channel2 := &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "ch2"}}

	score1 := strategy.Score(ctx, channel1)
	score2 := strategy.Score(ctx, channel2)

	assert.Equal(t, defaultStickyBoostScore, score1+score2, "Exactly one channel should get the boost score")
}

func TestStickyRoutingStrategy_KeyCascadePriority(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}

	ctx = contexts.WithTrace(ctx, &ent.Trace{ID: 1, TraceID: "trace-priority"})
	ctx = contexts.WithThread(ctx, &ent.Thread{ID: 1, ThreadID: "thread-priority"})
	ctx = contexts.WithStickyKey(ctx, "custom-priority")
	ctx = contextWithRequestedModel(ctx, "gpt-4")

	strategy := NewStickyRoutingStrategy(mockProvider)

	key := strategy.resolveStickyKey(ctx, "gpt-4")
	assert.Equal(t, "trace:trace-priority", key, "Trace key should take priority in cascade")
}

func TestStickyRoutingStrategy_ThreadOverCustomKey(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}

	ctx = contexts.WithThread(ctx, &ent.Thread{ID: 1, ThreadID: "thread-priority"})
	ctx = contexts.WithStickyKey(ctx, "custom-priority")

	strategy := NewStickyRoutingStrategy(mockProvider)

	key := strategy.resolveStickyKey(ctx, "gpt-4")
	assert.Equal(t, "thread:thread-priority", key, "Thread key should take priority over custom key")
}

func TestStickyRoutingStrategy_RingRebuildOnCacheVersionChange(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}
	strategy := NewStickyRoutingStrategy(mockProvider)

	ctx = contexts.WithTraceID(ctx, "trace-rebuild")
	channel1 := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}

	strategy.Score(ctx, channel1)

	mockProvider.cacheVersion = 2
	mockProvider.channels = []*biz.Channel{
		{Channel: &ent.Channel{ID: 3, Name: "ch3"}},
		{Channel: &ent.Channel{ID: 4, Name: "ch4"}},
	}

	channel3 := &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "ch3"}}
	channel4 := &biz.Channel{Channel: &ent.Channel{ID: 4, Name: "ch4"}}

	score1After := strategy.Score(ctx, channel1)
	assert.Equal(t, 0.0, score1After, "Old channel should not get boost after ring rebuild with new channels")

	score3 := strategy.Score(ctx, channel3)
	score4 := strategy.Score(ctx, channel4)
	assert.Equal(t, defaultStickyBoostScore, score3+score4, "New channels should be scored correctly after rebuild")
}

func TestStickyRoutingStrategy_ScoreWithDebugConsistency(t *testing.T) {
	ctx := context.Background()

	mockProvider := &mockStickyChannelProvider{
		channels: []*biz.Channel{
			{Channel: &ent.Channel{ID: 1, Name: "ch1"}},
			{Channel: &ent.Channel{ID: 2, Name: "ch2"}},
		},
		cacheVersion: 1,
	}
	strategy := NewStickyRoutingStrategy(mockProvider)

	testCases := []struct {
		name   string
		ctx    context.Context
		chID   int
		chName string
	}{
		{"no_sticky_key", ctx, 1, "ch1"},
		{"with_trace", contexts.WithTraceID(ctx, "trace-debug"), 1, "ch1"},
		{"with_custom_key", contexts.WithStickyKey(ctx, "custom-debug"), 2, "ch2"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			channel := &biz.Channel{Channel: &ent.Channel{ID: tc.chID, Name: tc.chName}}

			score := strategy.Score(tc.ctx, channel)
			debugScore, debugInfo := strategy.ScoreWithDebug(tc.ctx, channel)

			assert.Equal(t, score, debugScore, "Score and ScoreWithDebug must return identical scores")
			assert.Equal(t, "StickyRouting", debugInfo.StrategyName, "Strategy name should be StickyRouting")
			assert.Equal(t, score, debugInfo.Score, "StrategyScore.Score should match")
		})
	}
}

func TestStickyRoutingStrategy_BoostScoreValue(t *testing.T) {
	assert.Equal(t, 900.0, defaultStickyBoostScore, "Boost score should be 900 to dominate other strategies")
}

type mockStickyChannelProvider struct {
	channels     []*biz.Channel
	cacheVersion int64
}

func (m *mockStickyChannelProvider) GetEnabledChannels() []*biz.Channel {
	return m.channels
}

func (m *mockStickyChannelProvider) GetCacheVersion() int64 {
	return m.cacheVersion
}
