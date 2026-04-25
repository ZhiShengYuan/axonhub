package orchestrator

import (
	"context"
	"time"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/metrics"
)

type HedgeOutcome string

const (
	HedgeOutcomeWinnerReleased HedgeOutcome = "winner_released"
	HedgeOutcomeBothFailed     HedgeOutcome = "both_failed"
	HedgeOutcomeFallback       HedgeOutcome = "fallback"
)

func LogHedgePrimaryLaunched(ctx context.Context, channelID int, model, hedgePairID string) {
	log.Debug(ctx, "hedge.primary_launched",
		log.Int("channel_id", channelID),
		log.String("model", model),
		log.String("hedge_pair_id", hedgePairID),
	)
}

func LogHedgeSecondaryLaunched(ctx context.Context, channelID int, model, hedgePairID string, isProbing bool) {
	log.Debug(ctx, "hedge.secondary_launched",
		log.Int("channel_id", channelID),
		log.String("model", model),
		log.String("hedge_pair_id", hedgePairID),
		log.Bool("is_probing", isProbing),
	)
}

func LogHedgeObservationStarted(ctx context.Context, hedgePairID string, firstTokenFrom string) {
	log.Info(ctx, "hedge.observation_started",
		log.String("hedge_pair_id", hedgePairID),
		log.String("first_token_from", firstTokenFrom),
	)
}

func LogHedgeObservationEnded(ctx context.Context, hedgePairID string, primaryTPS, secondaryTPS float64, winnerIndex int) {
	log.Info(ctx, "hedge.observation_ended",
		log.String("hedge_pair_id", hedgePairID),
		log.Float64("primary_tps", primaryTPS),
		log.Float64("secondary_tps", secondaryTPS),
		log.Int("winner_index", winnerIndex),
	)
}

func LogHedgeWinnerChosen(ctx context.Context, hedgePairID string, winnerChannelID, loserChannelID int, winnerTPS, loserTPS float64) {
	log.Info(ctx, "hedge.winner_chosen",
		log.String("hedge_pair_id", hedgePairID),
		log.Int("winner_channel_id", winnerChannelID),
		log.Float64("winner_tps", winnerTPS),
		log.Int("loser_channel_id", loserChannelID),
		log.Float64("loser_tps", loserTPS),
	)
}

func LogHedgeLoserShadowed(ctx context.Context, hedgePairID string, loserChannelID int) {
	log.Debug(ctx, "hedge.loser_shadowed",
		log.String("hedge_pair_id", hedgePairID),
		log.Int("loser_channel_id", loserChannelID),
	)
}

func LogHedgeShadowCompleted(ctx context.Context, hedgePairID string, completionReason string, tokensConsumed int, duration time.Duration) {
	log.Info(ctx, "hedge.shadow_completed",
		log.String("hedge_pair_id", hedgePairID),
		log.String("completion_reason", completionReason),
		log.Int("tokens_consumed", tokensConsumed),
		log.Float64("duration_seconds", duration.Seconds()),
	)
}

func LogHedgeShadowDeadlineExceeded(ctx context.Context, hedgePairID string, loserChannelID int) {
	log.Warn(ctx, "hedge.shadow_deadline_exceeded",
		log.String("hedge_pair_id", hedgePairID),
		log.Int("loser_channel_id", loserChannelID),
	)
}

func LogHedgeFallbackResumed(ctx context.Context, hedgePairID string, reason string) {
	log.Info(ctx, "hedge.fallback_resumed",
		log.String("hedge_pair_id", hedgePairID),
		log.String("reason", reason),
	)
}

func RecordHedgeRaceCompletion(winnerChannelID, loserChannelID int, outcome HedgeOutcome) {
	metrics.RecordHedgeRaceCompletion(winnerChannelID, loserChannelID, string(outcome))
}

func RecordShadowCompletion(channelID int, reason ShadowCompletionReason) {
	metrics.RecordShadowCompletion(channelID, string(reason))
}

func RecordObservationWindowDuration(duration time.Duration) {
	metrics.RecordObservationWindowDuration(duration)
}

type HedgeObservabilityRecorder struct {
	HedgePairID         string
	PrimaryChannelID    int
	SecondaryChannelID  int
	Model               string
	FirstTokenFrom      string
	StartTime           time.Time
}

func NewHedgeObservabilityRecorder(hedgePairID string) *HedgeObservabilityRecorder {
	return &HedgeObservabilityRecorder{
		HedgePairID: hedgePairID,
		StartTime:   time.Now(),
	}
}

func (r *HedgeObservabilityRecorder) RecordPrimaryLaunched(ctx context.Context, channelID int, model string) {
	r.PrimaryChannelID = channelID
	r.Model = model
	LogHedgePrimaryLaunched(ctx, channelID, model, r.HedgePairID)
}

func (r *HedgeObservabilityRecorder) RecordSecondaryLaunched(ctx context.Context, channelID int, model string, isProbing bool) {
	r.SecondaryChannelID = channelID
	LogHedgeSecondaryLaunched(ctx, channelID, model, r.HedgePairID, isProbing)
}

func (r *HedgeObservabilityRecorder) RecordObservationStarted(ctx context.Context, firstTokenFrom string) {
	r.FirstTokenFrom = firstTokenFrom
	LogHedgeObservationStarted(ctx, r.HedgePairID, firstTokenFrom)
}

func (r *HedgeObservabilityRecorder) RecordObservationEnded(ctx context.Context, primaryTPS, secondaryTPS float64, winnerIndex int) {
	LogHedgeObservationEnded(ctx, r.HedgePairID, primaryTPS, secondaryTPS, winnerIndex)
}

func (r *HedgeObservabilityRecorder) RecordWinnerChosen(ctx context.Context, winnerChannelID, loserChannelID int, winnerTPS, loserTPS float64) {
	LogHedgeWinnerChosen(ctx, r.HedgePairID, winnerChannelID, loserChannelID, winnerTPS, loserTPS)
}

func (r *HedgeObservabilityRecorder) RecordLoserShadowed(ctx context.Context, loserChannelID int) {
	LogHedgeLoserShadowed(ctx, r.HedgePairID, loserChannelID)
}

func (r *HedgeObservabilityRecorder) RecordShadowCompleted(ctx context.Context, reason ShadowCompletionReason, tokensConsumed int, duration time.Duration) {
	LogHedgeShadowCompleted(ctx, r.HedgePairID, string(reason), tokensConsumed, duration)
	RecordShadowCompletion(r.SecondaryChannelID, reason)
}

func (r *HedgeObservabilityRecorder) RecordShadowDeadlineExceeded(ctx context.Context, loserChannelID int) {
	LogHedgeShadowDeadlineExceeded(ctx, r.HedgePairID, loserChannelID)
}

func (r *HedgeObservabilityRecorder) RecordFallbackResumed(ctx context.Context, reason string) {
	LogHedgeFallbackResumed(ctx, r.HedgePairID, reason)
}

func (r *HedgeObservabilityRecorder) GetDuration() time.Duration {
	return time.Since(r.StartTime)
}