package orchestrator

import (
	"context"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/log"
)

type HedgeMetricsResult struct {
	PrimaryTPS   float64
	SecondaryTPS float64
	WinnerIndex  int
	LoserIndex   int
	ObservationWindowDuration time.Duration
}

func ComputeHedgeMetrics(primaryEvents, secondaryEvents []*StreamEvent, windowDuration time.Duration) *HedgeMetricsResult {
	result := &HedgeMetricsResult{
		ObservationWindowDuration: windowDuration,
	}

	result.PrimaryTPS = computeObservationTPS(primaryEvents, windowDuration)
	result.SecondaryTPS = computeObservationTPS(secondaryEvents, windowDuration)

	if result.PrimaryTPS >= result.SecondaryTPS {
		result.WinnerIndex = 0
		result.LoserIndex = 1
	} else {
		result.WinnerIndex = 1
		result.LoserIndex = 0
	}

	return result
}

func computeObservationTPS(events []*StreamEvent, windowDuration time.Duration) float64 {
	if len(events) == 0 || windowDuration <= 0 {
		return 0
	}

	tokenCount := 0
	for _, e := range events {
		if len(e.Data) > 0 && string(e.Data) != "[DONE]" && e.Type != "done" {
			tokenCount++
		}
	}

	if tokenCount == 0 {
		return 0
	}

	windowSeconds := windowDuration.Seconds()
	if windowSeconds <= 0 {
		return 0
	}

	return float64(tokenCount) / windowSeconds
}

func RecordHedgeMetrics(
	ctx context.Context,
	result *HedgeMetricsResult,
	primaryCandidate *ChannelModelsCandidate,
	secondaryCandidate *ChannelModelsCandidate,
	latencyStrategy *LatencyAwareStrategy,
	entClient *ent.Client,
) {
	if result == nil {
		return
	}

	var winner, loser *ChannelModelsCandidate
	var winnerTPS, loserTPS float64

	if result.WinnerIndex == 0 {
		winner = primaryCandidate
		loser = secondaryCandidate
		winnerTPS = result.PrimaryTPS
		loserTPS = result.SecondaryTPS
	} else {
		winner = secondaryCandidate
		loser = primaryCandidate
		winnerTPS = result.SecondaryTPS
		loserTPS = result.PrimaryTPS
	}

	if latencyStrategy != nil {
		if winner != nil && winner.Channel != nil && winnerTPS > 0 {
			latencyStrategy.UpdateStreamingTPS(ctx, winner.Channel.ID, winnerTPS)
		}
		if loser != nil && loser.Channel != nil && loserTPS > 0 {
			penalizedTPS := loserTPS * 0.8
			latencyStrategy.UpdateStreamingTPS(ctx, loser.Channel.ID, penalizedTPS)
		}
	}

	recordTPSOnExecution := func(candidate *ChannelModelsCandidate, tps float64) {
		if candidate == nil || candidate.Channel == nil {
			return
		}

		executionID := findPendingExecutionForChannel(ctx, entClient, candidate.Channel.ID)
		if executionID == 0 {
			log.Debug(ctx, "RecordHedgeMetrics: no pending execution found for channel",
				log.Int("channel_id", candidate.Channel.ID))
			return
		}

		if _, err := entClient.RequestExecution.UpdateOneID(executionID).
			SetMetricsObservationWindowTps(tps).
			Save(ctx); err != nil {
			log.Warn(ctx, "RecordHedgeMetrics: failed to update observation window TPS",
				log.Int("execution_id", executionID),
				log.Float64("tps", tps),
				log.Cause(err))
		} else {
			log.Debug(ctx, "RecordHedgeMetrics: recorded observation window TPS",
				log.Int("execution_id", executionID),
				log.Int("channel_id", candidate.Channel.ID),
				log.Float64("tps", tps))
		}
	}

	if primaryCandidate != nil {
		recordTPSOnExecution(primaryCandidate, result.PrimaryTPS)
	}
	if secondaryCandidate != nil {
		recordTPSOnExecution(secondaryCandidate, result.SecondaryTPS)
	}
}

func findPendingExecutionForChannel(ctx context.Context, entClient *ent.Client, channelID int) int {
	if entClient == nil {
		return 0
	}
	exec, err := entClient.RequestExecution.Query().
		Where(
			requestexecution.ChannelIDEQ(channelID),
			requestexecution.StatusIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
		).
		Order(ent.Desc(requestexecution.FieldCreatedAt)).
		Only(ctx)

	if err != nil {
		return 0
	}

	return exec.ID
}