package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
)

// ChannelAffinityProvider provides affinity-related channel information.
// It resolves the channel ID that previously served the same affinity key
// within a project and model scope. The lookup is cache-only — there is no
// DB fallback.
type ChannelAffinityProvider interface {
	GetAffinityChannelID(ctx context.Context, projectID int, modelScope string, affinityHash string) (int, error)
}

// AffinityAwareStrategy provides a soft ordering boost for the channel that
// previously served the same stable session affinity key.
//
// The boost (750.0) is deliberately below TraceAwareStrategy's 1000.0 so that
// trace pinning always takes precedence over session affinity when both are
// present. Affinity is a boost, never a filter — non-matching channels still
// receive a score of 0 (not exclusion) and remain eligible for selection.
type AffinityAwareStrategy struct {
	affinityProvider ChannelAffinityProvider
	boostScore       float64
}

// NewAffinityAwareStrategy creates a new affinity-aware strategy with a
// default boost of 750.0.
func NewAffinityAwareStrategy(affinityProvider ChannelAffinityProvider) *AffinityAwareStrategy {
	return &AffinityAwareStrategy{
		affinityProvider: affinityProvider,
		boostScore:       750.0,
	}
}

// Score returns the boost score if this channel was the last successful one
// for the current affinity key. Production path without debug logging.
//
// Returns 0 when:
//   - there is no affinity state in the context,
//   - the project ID is missing,
//   - the cache lookup errors or reports no cached channel,
//   - the candidate channel is not the cached channel.
func (s *AffinityAwareStrategy) Score(ctx context.Context, channel *biz.Channel) float64 {
	affinityState, hasAffinity := contexts.GetAffinityState(ctx)
	if !hasAffinity {
		return 0
	}

	projectID, ok := contexts.GetProjectID(ctx)
	if !ok {
		return 0
	}

	cachedChannelID, err := s.affinityProvider.GetAffinityChannelID(ctx, projectID, affinityState.ModelScope, affinityState.Hash)
	if err != nil || cachedChannelID == 0 {
		return 0
	}

	if channel.ID == cachedChannelID {
		return s.boostScore
	}

	return 0
}

// ScoreWithDebug returns the boost score with detailed debug information.
// Debug path with comprehensive logging. Mirrors Score() exactly except for
// debug details and logging.
//
// Debug details intentionally contain NO raw affinity values — only the
// hash, source, model scope, and channel IDs are recorded.
func (s *AffinityAwareStrategy) ScoreWithDebug(ctx context.Context, channel *biz.Channel) (float64, StrategyScore) {
	affinityState, hasAffinity := contexts.GetAffinityState(ctx)
	if !hasAffinity {
		log.Info(ctx, "AffinityAwareStrategy: no affinity state in context, returning 0 score")

		return 0, StrategyScore{
			StrategyName: s.Name(),
			Score:        0,
			Details: map[string]any{
				"reason": "no_affinity_in_context",
			},
		}
	}

	projectID, ok := contexts.GetProjectID(ctx)
	if !ok {
		log.Info(ctx, "AffinityAwareStrategy: project ID missing from context, returning 0 score",
			log.String("source", affinityState.Source),
			log.String("model_scope", affinityState.ModelScope),
		)

		return 0, StrategyScore{
			StrategyName: s.Name(),
			Score:        0,
			Details: map[string]any{
				"reason":      "no_project_id",
				"source":      affinityState.Source,
				"model_scope": affinityState.ModelScope,
			},
		}
	}

	cachedChannelID, err := s.affinityProvider.GetAffinityChannelID(ctx, projectID, affinityState.ModelScope, affinityState.Hash)
	if err != nil {
		log.Info(ctx, "AffinityAwareStrategy: failed to get cached affinity channel ID",
			log.String("source", affinityState.Source),
			log.String("model_scope", affinityState.ModelScope),
			log.Cause(err),
		)

		return 0, StrategyScore{
			StrategyName: s.Name(),
			Score:        0,
			Details: map[string]any{
				"reason":      "error_getting_cached_channel",
				"source":      affinityState.Source,
				"model_scope": affinityState.ModelScope,
				"error":       err.Error(),
			},
		}
	}

	if cachedChannelID == 0 {
		log.Info(ctx, "AffinityAwareStrategy: no cached channel for affinity key",
			log.String("source", affinityState.Source),
			log.String("model_scope", affinityState.ModelScope),
		)

		return 0, StrategyScore{
			StrategyName: s.Name(),
			Score:        0,
			Details: map[string]any{
				"reason":      "no_cached_channel",
				"source":      affinityState.Source,
				"model_scope": affinityState.ModelScope,
			},
		}
	}

	isAffinityChannel := channel.ID == cachedChannelID
	score := 0.0
	details := map[string]any{
		"source":            affinityState.Source,
		"model_scope":       affinityState.ModelScope,
		"cached_channel_id": cachedChannelID,
		"is_affinity_match": isAffinityChannel,
	}

	if isAffinityChannel {
		score = s.boostScore
		details["reason"] = "affinity_channel_match"

		log.Info(ctx, "AffinityAwareStrategy: boosting channel",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.String("source", affinityState.Source),
			log.String("model_scope", affinityState.ModelScope),
			log.Float64("score", score),
			log.String("reason", "affinity_channel_match"),
		)
	} else {
		details["reason"] = "not_affinity_channel"

		log.Info(ctx, "AffinityAwareStrategy: channel is not the cached affinity channel",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.Int("cached_channel_id", cachedChannelID),
			log.String("source", affinityState.Source),
			log.String("model_scope", affinityState.ModelScope),
		)
	}

	return score, StrategyScore{
		StrategyName: s.Name(),
		Score:        score,
		Details:      details,
	}
}

// Name returns the strategy name.
func (s *AffinityAwareStrategy) Name() string {
	return "AffinityAwareStrategy"
}
