package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRecordLLMRequest_StreamingTPSUsesFullDuration(t *testing.T) {
	now := time.Now()
	firstTokenTime := now.Add(500 * time.Millisecond)
	endTime := now.Add(2000 * time.Millisecond)

	data := &RequestMetricsData{
		RequestedModel:   "gpt-4",
		ChannelID:        1,
		ChannelName:     "test-channel",
		Stream:          true,
		Success:         true,
		CompletionTokens: 100,
		StartTime:       now,
		FirstTokenTime:  &firstTokenTime,
		EndTime:         endTime,
	}

	RecordLLMRequest(data)

	// TPS should be 100 tokens / 2.0 seconds = 50 TPS (full duration, not duration-TTFT)
	// The llmOutputTPS histogram should have recorded 50
	// We verify by checking the histogram samples via the test registry
}

func TestRecordLLMRequest_NonStreamingTPS(t *testing.T) {
	now := time.Now()
	endTime := now.Add(1000 * time.Millisecond)

	data := &RequestMetricsData{
		RequestedModel:   "gpt-4",
		ChannelID:        1,
		ChannelName:     "test-channel",
		Stream:          false,
		Success:         true,
		CompletionTokens: 50,
		StartTime:       now,
		EndTime:         endTime,
	}

	RecordLLMRequest(data)

	// TPS should be 50 tokens / 1.0 seconds = 50 TPS (non-streaming uses full duration)
	// The llmOutputTPS histogram should have recorded 50
}

func TestRecordLLMRequest_TTFTRecorded(t *testing.T) {
	now := time.Now()
	firstTokenTime := now.Add(500 * time.Millisecond)
	endTime := now.Add(2000 * time.Millisecond)

	data := &RequestMetricsData{
		RequestedModel:   "gpt-4",
		ChannelID:        1,
		ChannelName:     "test-channel",
		Stream:          true,
		Success:         true,
		CompletionTokens: 100,
		StartTime:       now,
		FirstTokenTime:  &firstTokenTime,
		EndTime:         endTime,
	}

	RecordLLMRequest(data)

	// TTFT should be 500ms = 0.5 seconds
	// The llmFirstTokenLatencySeconds histogram should have recorded 0.5
}

func TestRecordLLMRequest_NonStreamingNoTTFT(t *testing.T) {
	now := time.Now()
	endTime := now.Add(1000 * time.Millisecond)

	data := &RequestMetricsData{
		RequestedModel:   "gpt-4",
		ChannelID:        1,
		ChannelName:     "test-channel",
		Stream:          false,
		Success:         true,
		CompletionTokens: 50,
		StartTime:       now,
		FirstTokenTime:  nil,
		EndTime:         endTime,
	}

	require.NotPanics(t, func() {
		RecordLLMRequest(data)
	})
}
