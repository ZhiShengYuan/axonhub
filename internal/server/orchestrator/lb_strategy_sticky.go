package orchestrator

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"sort"
	"sync"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
)

const (
	// virtualNodesPerChannel is the number of virtual nodes per channel in the consistent hash ring.
	virtualNodesPerChannel = 150
)

// virtualNode represents a single node in the consistent hash ring.
type virtualNode struct {
	hash      uint64
	channelID int
}

// consistentHashRing implements a consistent hash ring with sorted nodes.
type consistentHashRing struct {
	nodes []virtualNode // sorted by hash
}

// build creates a new consistent hash ring from the given channels.
// Each channel gets virtualNodesPerChannel virtual nodes for better distribution.
func (r *consistentHashRing) build(channels []*biz.Channel) {
	r.nodes = make([]virtualNode, 0, len(channels)*virtualNodesPerChannel)

	for _, channel := range channels {
		for i := 0; i < virtualNodesPerChannel; i++ {
			// Virtual node key: "channelID-replicaIndex"
			vnodeKey := fmt.Sprintf("%d-%d", channel.ID, i)
			h := fnv.New64a()
			h.Write([]byte(vnodeKey))

			r.nodes = append(r.nodes, virtualNode{
				hash:      h.Sum64(),
				channelID: channel.ID,
			})
		}
	}

	// Sort nodes by hash for binary search
	sort.Slice(r.nodes, func(i, j int) bool {
		return r.nodes[i].hash < r.nodes[j].hash
	})
}

// getChannelID finds the channel ID for a given key using consistent hashing.
// Uses binary search to find the first node with hash >= keyHash, wrapping around if needed.
func (r *consistentHashRing) getChannelID(key string) int {
	if len(r.nodes) == 0 {
		return 0
	}

	h := fnv.New64a()
	h.Write([]byte(key))
	keyHash := h.Sum64()

	// Binary search for first node with hash >= keyHash
	idx := sort.Search(len(r.nodes), func(i int) bool {
		return r.nodes[i].hash >= keyHash
	})

	// Wrap around if we reached the end
	if idx >= len(r.nodes) {
		idx = 0
	}

	return r.nodes[idx].channelID
}

// hashCandidates computes a hash of all candidate channel IDs for change detection.
func hashCandidates(channels []*biz.Channel) uint64 {
	h := fnv.New64a()
	for _, c := range channels {
		binary.Write(h, binary.BigEndian, int64(c.ID))
	}
	return h.Sum64()
}

// StickyRoutingStrategy implements consistent hashing for sticky sessions.
// It ensures requests with the same sticky key are routed to the same channel.
type StickyRoutingStrategy struct {
	ring               *consistentHashRing
	boostScore         float64
	lastCandidatesHash uint64
	mu                 sync.RWMutex
}

// NewStickyRoutingStrategy creates a new sticky routing strategy.
func NewStickyRoutingStrategy() *StickyRoutingStrategy {
	return &StickyRoutingStrategy{
		ring:       &consistentHashRing{},
		boostScore: 900.0,
	}
}

// Name returns the strategy name.
func (s *StickyRoutingStrategy) Name() string {
	return "StickyRouting"
}

// Score returns 900 if this channel matches the sticky key's hash target, 0 otherwise.
// Production path without debug logging.
func (s *StickyRoutingStrategy) Score(ctx context.Context, channel *biz.Channel) float64 {
	stickyKey, ok := contexts.GetStickyKey(ctx)
	if !ok || stickyKey == "" {
		return 0
	}

	s.mu.RLock()
	ring := s.ring
	s.mu.RUnlock()

	// If ring is empty (no candidates prepared), can't do sticky routing
	if len(ring.nodes) == 0 {
		return 0
	}

	// Find the target channel for this sticky key
	targetChannelID := ring.getChannelID(stickyKey)

	if channel.ID == targetChannelID {
		return s.boostScore
	}

	return 0
}

// ScoreWithDebug returns the score with detailed debug information.
// Debug path with comprehensive logging.
func (s *StickyRoutingStrategy) ScoreWithDebug(ctx context.Context, channel *biz.Channel) (float64, StrategyScore) {
	stickyKey, ok := contexts.GetStickyKey(ctx)
	if !ok || stickyKey == "" {
		log.Info(ctx, "StickyRoutingStrategy: no sticky key in context, returning 0 score")

		return 0, StrategyScore{
			StrategyName: s.Name(),
			Score:        0,
			Details: map[string]any{
				"reason": "no_sticky_key_in_context",
			},
		}
	}

	s.mu.RLock()
	ring := s.ring
	lastCandidatesHash := s.lastCandidatesHash
	s.mu.RUnlock()

	// If ring is empty (no candidates prepared), can't do sticky routing
	if len(ring.nodes) == 0 {
		log.Info(ctx, "StickyRoutingStrategy: ring not initialized, returning 0 score",
			log.String("sticky_key", stickyKey),
		)

		return 0, StrategyScore{
			StrategyName: s.Name(),
			Score:        0,
			Details: map[string]any{
				"reason":     "ring_not_initialized",
				"sticky_key": stickyKey,
			},
		}
	}

	// Find the target channel for this sticky key
	targetChannelID := ring.getChannelID(stickyKey)
	isTarget := channel.ID == targetChannelID
	score := 0.0

	if isTarget {
		score = s.boostScore
	}

	details := map[string]any{
		"sticky_key":          stickyKey,
		"target_channel_id":   targetChannelID,
		"current_channel_id":  channel.ID,
		"is_target":           isTarget,
		"ring_node_count":     len(ring.nodes),
		"last_candidates_hash": lastCandidatesHash,
	}

	if isTarget {
		details["reason"] = "sticky_key_matches_channel"

		log.Info(ctx, "StickyRoutingStrategy: boosting channel",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.String("sticky_key", stickyKey),
			log.Int("target_channel_id", targetChannelID),
			log.Float64("score", score),
			log.String("reason", "sticky_key_matches_channel"),
		)
	} else {
		details["reason"] = "sticky_key_does_not_match_channel"

		log.Info(ctx, "StickyRoutingStrategy: channel not target for sticky key",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.String("sticky_key", stickyKey),
			log.Int("target_channel_id", targetChannelID),
		)
	}

	return score, StrategyScore{
		StrategyName: s.Name(),
		Score:        score,
		Details:      details,
	}
}

// PrepareCandidates rebuilds the consistent hash ring if the candidate set has changed.
func (s *StickyRoutingStrategy) PrepareCandidates(candidates []*ChannelModelsCandidate) {
	channels := make([]*biz.Channel, len(candidates))
	for i, c := range candidates {
		channels[i] = c.Channel
	}

	candidatesHash := hashCandidates(channels)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Only rebuild if candidates have changed
	if candidatesHash != s.lastCandidatesHash {
		s.ring.build(channels)
		s.lastCandidatesHash = candidatesHash
	}
}