package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
)

const defaultVirtualNodes = 150
const defaultStickyBoostScore = 900.0

// ringEntry represents a single point on the consistent hash ring.
type ringEntry struct {
	hash      uint32
	channelID int
}

// ConsistentHashRing implements a consistent hash ring with virtual nodes.
// It provides O(log N) lookup for channel selection.
type ConsistentHashRing struct {
	mu      sync.RWMutex
	sorted  []ringEntry
	nodeMap map[int]struct{} // track which channel IDs are on the ring
}

// NewConsistentHashRing creates a new empty consistent hash ring.
func NewConsistentHashRing() *ConsistentHashRing {
	return &ConsistentHashRing{
		sorted:  make([]ringEntry, 0),
		nodeMap: make(map[int]struct{}),
	}
}

// Rebuild rebuilds the ring with the given channel IDs.
// Creates virtualNodes entries per channel ID using FNV-1a hash.
// Sorts all entries by hash for binary search.
func (r *ConsistentHashRing) Rebuild(channelIDs []int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.sorted = make([]ringEntry, 0, len(channelIDs)*defaultVirtualNodes)
	r.nodeMap = make(map[int]struct{})

	for _, channelID := range channelIDs {
		r.nodeMap[channelID] = struct{}{}
		for vnode := 0; vnode < defaultVirtualNodes; vnode++ {
			key := fmt.Sprintf("%d#%d", channelID, vnode)
			hash := fnv32(key)
			r.sorted = append(r.sorted, ringEntry{
				hash:      hash,
				channelID: channelID,
			})
		}
	}

	sort.Slice(r.sorted, func(i, j int) bool {
		return r.sorted[i].hash < r.sorted[j].hash
	})
}

// Get returns the channel ID that the given key maps to.
// Uses binary search on the sorted ring entries.
// Wraps around to the first entry if hash exceeds all entries.
// Returns 0 if the ring is empty.
func (r *ConsistentHashRing) Get(key string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.sorted) == 0 {
		return 0
	}

	hash := fnv32(key)

	// Binary search for the first entry with hash >= key hash
	idx := sort.Search(len(r.sorted), func(i int) bool {
		return r.sorted[i].hash >= hash
	})

	// If we exceeded all entries, wrap around to the first
	if idx >= len(r.sorted) {
		idx = 0
	}

	return r.sorted[idx].channelID
}

// fnv32 computes FNV-1a 32-bit hash of the given key.
func fnv32(key string) uint32 {
	h := uint32(2166136261) // FNV offset basis
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619 // FNV prime
	}
	return h
}

// StickyChannelProvider provides channel information for sticky routing.
type StickyChannelProvider interface {
	GetEnabledChannels() []*biz.Channel
	GetCacheVersion() int64
}

// StickyRoutingStrategy uses consistent hashing to route requests with the same
// identifier to the same upstream channel for cache affinity.
type StickyRoutingStrategy struct {
	channelProvider StickyChannelProvider
	ring            *ConsistentHashRing
	lastVersion     int64
	boostScore      float64
}

// NewStickyRoutingStrategy creates a new sticky routing strategy.
func NewStickyRoutingStrategy(channelProvider StickyChannelProvider) *StickyRoutingStrategy {
	return &StickyRoutingStrategy{
		channelProvider: channelProvider,
		ring:            NewConsistentHashRing(),
		boostScore:      defaultStickyBoostScore,
	}
}

// resolveStickyKey resolves the sticky key using cascading resolution:
// 1. Trace ID (multi-turn chat cache affinity - highest priority)
// 2. Trace ID string (from X-Trace-ID header, without DB entity)
// 3. Thread ID (conversation-level stickiness)
// 4. Custom sticky key (from X-Sticky-Key header)
//
// Note: API key + model is intentionally NOT used as a sticky key.
// Tenant-level stickiness would force all requests with the same API key
// and model to one upstream, killing parallel throughput for independent
// conversations. Only conversation-level identifiers (trace/thread) get
// sticky routing for cache affinity; everything else is distributed by
// other strategies (WeightRoundRobin, ErrorAware, LatencyAware) for
// maximum throughput.
func (s *StickyRoutingStrategy) resolveStickyKey(ctx context.Context, model string) string {
	if trace, ok := contexts.GetTrace(ctx); ok && trace != nil && trace.TraceID != "" {
		return "trace:" + trace.TraceID
	}
	if traceID, ok := contexts.GetTraceID(ctx); ok && traceID != "" {
		return "trace:" + traceID
	}
	if thread, ok := contexts.GetThread(ctx); ok && thread != nil && thread.ThreadID != "" {
		return "thread:" + thread.ThreadID
	}
	if stickyKey, ok := contexts.GetStickyKey(ctx); ok && stickyKey != "" {
		return "custom:" + stickyKey
	}
	return ""
}

// ensureRing rebuilds the ring if the channel cache version has changed.
func (s *StickyRoutingStrategy) ensureRing(ctx context.Context) {
	version := s.channelProvider.GetCacheVersion()
	if version == s.lastVersion {
		return
	}
	channels := s.channelProvider.GetEnabledChannels()
	ids := make([]int, 0, len(channels))
	for _, ch := range channels {
		ids = append(ids, ch.ID)
	}
	s.ring.Rebuild(ids)
	s.lastVersion = version
}

// Score calculates the score for the channel using sticky routing.
// Production path with minimal overhead.
// Returns boostScore (900) if the channel matches the sticky target, 0 otherwise.
func (s *StickyRoutingStrategy) Score(ctx context.Context, channel *biz.Channel) float64 {
	model := requestedModelFromContext(ctx)
	key := s.resolveStickyKey(ctx, model)
	if key == "" {
		return 0
	}
	s.ensureRing(ctx)
	targetID := s.ring.Get(key)
	if targetID == channel.ID {
		return s.boostScore
	}
	return 0
}

// ScoreWithDebug calculates the score with detailed debug information.
// Debug path with comprehensive logging.
func (s *StickyRoutingStrategy) ScoreWithDebug(ctx context.Context, channel *biz.Channel) (float64, StrategyScore) {
	model := requestedModelFromContext(ctx)
	key := s.resolveStickyKey(ctx, model)

	// No sticky key available
	if key == "" {
		log.Info(ctx, "StickyRoutingStrategy: no sticky key available",
			log.String("reason", "no_sticky_key"),
		)
		return 0, StrategyScore{
			StrategyName: s.Name(),
			Score:        0,
			Details: map[string]any{
				"reason": "no_sticky_key",
			},
		}
	}

	var keyType string
	switch {
	case len(key) > 6 && key[:6] == "trace:":
		keyType = "trace"
	case len(key) > 7 && key[:7] == "thread:":
		keyType = "thread"
	case len(key) > 7 && key[:7] == "custom:":
		keyType = "custom"
	default:
		keyType = "unknown"
	}

	s.ensureRing(ctx)
	targetID := s.ring.Get(key)

	log.Info(ctx, "StickyRoutingStrategy: sticky key resolved",
		log.String("key_type", keyType),
		log.String("sticky_key", key),
		log.Int("target_channel_id", targetID),
	)

	// Check if this channel matches the sticky target
	if targetID == channel.ID {
		log.Info(ctx, "StickyRoutingStrategy: channel matched sticky target",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.Float64("score", s.boostScore),
			log.String("reason", "sticky_channel_matched"),
		)
		return s.boostScore, StrategyScore{
			StrategyName: s.Name(),
			Score:        s.boostScore,
			Details: map[string]any{
				"reason":            "sticky_channel_matched",
				"key_type":          keyType,
				"sticky_key":        key,
				"target_channel_id": targetID,
			},
		}
	}

	log.Info(ctx, "StickyRoutingStrategy: channel does not match sticky target",
		log.Int("channel_id", channel.ID),
		log.String("channel_name", channel.Name),
		log.Int("target_channel_id", targetID),
		log.String("reason", "sticky_channel_mismatch"),
	)
	return 0, StrategyScore{
		StrategyName: s.Name(),
		Score:        0,
		Details: map[string]any{
			"reason":            "sticky_channel_mismatch",
			"key_type":          keyType,
			"sticky_key":        key,
			"target_channel_id": targetID,
		},
	}
}

// Name returns the strategy name.
func (s *StickyRoutingStrategy) Name() string {
	return "StickyRouting"
}
