package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLogHedgePrimaryLaunched(t *testing.T) {
	ctx := context.Background()
	channelID := 123
	model := "gpt-4"
	hedgePairID := "hp-12345"

	LogHedgePrimaryLaunched(ctx, channelID, model, hedgePairID)
}

func TestLogHedgeSecondaryLaunched(t *testing.T) {
	ctx := context.Background()
	channelID := 124
	model := "claude-3"
	hedgePairID := "hp-12345"
	isProbing := true

	LogHedgeSecondaryLaunched(ctx, channelID, model, hedgePairID, isProbing)

	isProbing = false
	LogHedgeSecondaryLaunched(ctx, channelID, model, hedgePairID, isProbing)
}

func TestLogHedgeObservationStarted(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-12345"
	firstTokenFrom := "primary"

	LogHedgeObservationStarted(ctx, hedgePairID, firstTokenFrom)

	firstTokenFrom = "secondary"
	LogHedgeObservationStarted(ctx, hedgePairID, firstTokenFrom)
}

func TestLogHedgeObservationEnded(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-12345"
	primaryTPS := 50.5
	secondaryTPS := 45.2
	winnerIndex := 0

	LogHedgeObservationEnded(ctx, hedgePairID, primaryTPS, secondaryTPS, winnerIndex)

	winnerIndex = 1
	LogHedgeObservationEnded(ctx, hedgePairID, secondaryTPS, primaryTPS, winnerIndex)
}

func TestLogHedgeWinnerChosen(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-12345"
	winnerChannelID := 123
	loserChannelID := 124
	winnerTPS := 50.5
	loserTPS := 45.2

	LogHedgeWinnerChosen(ctx, hedgePairID, winnerChannelID, loserChannelID, winnerTPS, loserTPS)
}

func TestLogHedgeLoserShadowed(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-12345"
	loserChannelID := 124

	LogHedgeLoserShadowed(ctx, hedgePairID, loserChannelID)
}

func TestLogHedgeShadowCompleted(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-12345"
	completionReason := "normal_completion"
	tokensConsumed := 100
	duration := 5 * time.Second

	LogHedgeShadowCompleted(ctx, hedgePairID, completionReason, tokensConsumed, duration)
}

func TestLogHedgeShadowDeadlineExceeded(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-12345"
	loserChannelID := 124

	LogHedgeShadowDeadlineExceeded(ctx, hedgePairID, loserChannelID)
}

func TestLogHedgeFallbackResumed(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-12345"
	reason := "both_streams_failed"

	LogHedgeFallbackResumed(ctx, hedgePairID, reason)
}

func TestHedgeObservabilityRecorder(t *testing.T) {
	ctx := context.Background()
	hedgePairID := "hp-rec-test"

	recorder := NewHedgeObservabilityRecorder(hedgePairID)
	assert.Equal(t, hedgePairID, recorder.HedgePairID)
	assert.False(t, recorder.StartTime.IsZero())

	time.Sleep(time.Millisecond)
	duration := recorder.GetDuration()
	assert.True(t, duration > 0)

	recorder.RecordPrimaryLaunched(ctx, 100, "gpt-4")
	assert.Equal(t, 100, recorder.PrimaryChannelID)
	assert.Equal(t, "gpt-4", recorder.Model)

	recorder.RecordSecondaryLaunched(ctx, 101, "claude-3", false)
	assert.Equal(t, 101, recorder.SecondaryChannelID)

	recorder.RecordObservationStarted(ctx, "primary")
	assert.Equal(t, "primary", recorder.FirstTokenFrom)

	recorder.RecordObservationEnded(ctx, 50.0, 45.0, 0)

	recorder.RecordWinnerChosen(ctx, 100, 101, 50.0, 45.0)

	recorder.RecordLoserShadowed(ctx, 101)

	recorder.RecordShadowCompleted(ctx, ShadowCompletionNormal, 100, 5*time.Second)

	recorder.RecordShadowDeadlineExceeded(ctx, 101)

	recorder.RecordFallbackResumed(ctx, "winner_released")
}

func TestRecordHedgeRaceCompletion(t *testing.T) {
	RecordHedgeRaceCompletion(100, 101, HedgeOutcomeWinnerReleased)
	RecordHedgeRaceCompletion(100, 101, HedgeOutcomeBothFailed)
	RecordHedgeRaceCompletion(100, 101, HedgeOutcomeFallback)
}

func TestRecordShadowCompletion(t *testing.T) {
	RecordShadowCompletion(101, ShadowCompletionNormal)
	RecordShadowCompletion(101, ShadowCompletionUpstreamError)
	RecordShadowCompletion(101, ShadowCompletionClientDisconnected)
	RecordShadowCompletion(101, ShadowCompletionDeadlineExceeded)
	RecordShadowCompletion(101, ShadowCompletionServerShutdown)
}

func TestRecordObservationWindowDuration(t *testing.T) {
	RecordObservationWindowDuration(3 * time.Second)
	RecordObservationWindowDuration(1 * time.Second)
}

func TestHedgeOutcomeValues(t *testing.T) {
	assert.Equal(t, HedgeOutcome("winner_released"), HedgeOutcomeWinnerReleased)
	assert.Equal(t, HedgeOutcome("both_failed"), HedgeOutcomeBothFailed)
	assert.Equal(t, HedgeOutcome("fallback"), HedgeOutcomeFallback)
}

func TestShadowCompletionReasonIsValid(t *testing.T) {
	assert.True(t, ShadowCompletionNormal.IsValid())
	assert.True(t, ShadowCompletionUpstreamError.IsValid())
	assert.True(t, ShadowCompletionClientDisconnected.IsValid())
	assert.True(t, ShadowCompletionDeadlineExceeded.IsValid())
	assert.True(t, ShadowCompletionServerShutdown.IsValid())
	assert.False(t, ShadowCompletionReason("invalid").IsValid())
}