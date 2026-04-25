package metrics

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	llmRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "axonhub_llm_requests_total",
			Help: "Total number of LLM requests",
		},
		[]string{"requested_model", "channel_id", "channel_name", "stream", "status"},
	)

	llmCompletionTokensTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "axonhub_llm_completion_tokens_total",
			Help: "Total number of completion tokens",
		},
		[]string{"requested_model", "channel_id", "channel_name", "stream"},
	)

	llmRequestDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "axonhub_llm_request_duration_seconds",
			Help:    "LLM request duration in seconds",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"requested_model", "channel_id", "channel_name", "stream"},
	)

	llmFirstTokenLatencySeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "axonhub_llm_first_token_latency_seconds",
			Help:    "First token latency in seconds (streaming only)",
			Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"requested_model", "channel_id", "channel_name"},
	)

	llmGenerationDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "axonhub_llm_generation_duration_seconds",
			Help:    "Token generation duration in seconds (EndTime - FirstTokenTime for streaming, EndTime - StartTime for non-streaming)",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		},
		[]string{"requested_model", "channel_id", "channel_name", "stream"},
	)

	llmOutputTPS = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "axonhub_llm_output_tps",
			Help:    "Output tokens per second (completion_tokens / generation_duration)",
			Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
		},
		[]string{"requested_model", "channel_id", "channel_name"},
	)

	modelRPS = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "axonhub_model_rps",
			Help: "Current requests per second in the sliding window per requested model",
		},
		[]string{"requested_model"},
	)

	modelTPS = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "axonhub_model_tps",
			Help: "Current tokens per second in the sliding window per requested model",
		},
		[]string{"requested_model"},
	)

	channelRPS = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "axonhub_channel_rps",
			Help: "Current requests per second in the sliding window per channel",
		},
		[]string{"channel_id", "channel_name"},
	)

	channelTPS = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "axonhub_channel_tps",
			Help: "Current tokens per second in the sliding window per channel",
		},
		[]string{"channel_id", "channel_name"},
	)

	axonhubHedgeRacesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "axonhub_hedge_races_total",
			Help: "Total number of hedge races by winner_channel, loser_channel, and outcome",
		},
		[]string{"winner_channel", "loser_channel", "outcome"},
	)

	axonhubHedgeShadowCompletionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "axonhub_hedge_shadow_completions_total",
			Help: "Total number of hedge shadow completions by channel and completion_reason",
		},
		[]string{"channel", "completion_reason"},
	)

	axonhubHedgeObservationWindowSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "axonhub_hedge_observation_window_seconds",
			Help:    "Hedge observation window duration in seconds",
			Buckets: []float64{0.5, 1, 2, 3, 5, 10},
		},
		[]string{},
	)
)

var llmRateAggregator = NewRateAggregator(RateAggregatorConfig{})

func init() {
	go startRateGaugeUpdater(context.Background(), 10*time.Second)
}

type RequestMetricsData struct {
	RequestedModel   string
	ChannelID        int
	ChannelName      string
	Stream           bool
	Success          bool
	Canceled         bool
	CompletionTokens int64
	StartTime        time.Time
	FirstTokenTime   *time.Time
	EndTime          time.Time
}

type requestMetricsRecorder struct {
	recorded bool
	mu       sync.Mutex
}

var requestRecorders = sync.Map{}

func getRecorder(key string) *requestMetricsRecorder {
	if v, ok := requestRecorders.Load(key); ok {
		return v.(*requestMetricsRecorder)
	}
	recorder := &requestMetricsRecorder{}
	requestRecorders.Store(key, recorder)
	return recorder
}

func RecordLLMRequest(data *RequestMetricsData) {
	key := data.RequestedModel + ":" + data.ChannelName + ":" + strconv.FormatBool(data.Stream)
	recorder := getRecorder(key)

	recorder.mu.Lock()
	if recorder.recorded {
		recorder.mu.Unlock()
		return
	}
	recorder.recorded = true
	recorder.mu.Unlock()

	channelID := strconv.Itoa(data.ChannelID)
	streamStr := strconv.FormatBool(data.Stream)
	status := "success"
	if data.Canceled {
		status = "canceled"
	} else if !data.Success {
		status = "error"
	}

	llmRequestsTotal.WithLabelValues(data.RequestedModel, channelID, data.ChannelName, streamStr, status).Inc()

	if data.CompletionTokens > 0 && data.Success {
		llmCompletionTokensTotal.WithLabelValues(data.RequestedModel, channelID, data.ChannelName, streamStr).Add(float64(data.CompletionTokens))
	}

	duration := data.EndTime.Sub(data.StartTime).Seconds()
	llmRequestDurationSeconds.WithLabelValues(data.RequestedModel, channelID, data.ChannelName, streamStr).Observe(duration)

	if data.Stream && data.FirstTokenTime != nil {
		firstTokenLatency := data.FirstTokenTime.Sub(data.StartTime).Seconds()
		llmFirstTokenLatencySeconds.WithLabelValues(data.RequestedModel, channelID, data.ChannelName).Observe(firstTokenLatency)

		generationDuration := data.EndTime.Sub(*data.FirstTokenTime).Seconds()
		llmGenerationDurationSeconds.WithLabelValues(data.RequestedModel, channelID, data.ChannelName, streamStr).Observe(generationDuration)

		if generationDuration > 0 && data.CompletionTokens > 0 {
			tps := float64(data.CompletionTokens) / generationDuration
			llmOutputTPS.WithLabelValues(data.RequestedModel, channelID, data.ChannelName).Observe(tps)
		}
	} else {
		llmGenerationDurationSeconds.WithLabelValues(data.RequestedModel, channelID, data.ChannelName, streamStr).Observe(duration)

		if duration > 0 && data.CompletionTokens > 0 {
			tps := float64(data.CompletionTokens) / duration
			llmOutputTPS.WithLabelValues(data.RequestedModel, channelID, data.ChannelName).Observe(tps)
		}
	}

	llmRateAggregator.Record(
		data.RequestedModel,
		data.ChannelID,
		data.ChannelName,
		data.CompletionTokens,
		data.EndTime,
	)
}

func RecordLLMRequestAsync(ctx context.Context, data *RequestMetricsData) {
	go func() {
		RecordLLMRequest(data)
	}()
}

func startRateGaugeUpdater(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			updateRateGauges(now)
		}
	}
}

func updateRateGauges(now time.Time) {
	models, channels := llmRateAggregator.Snapshot(now)

	for _, modelRate := range models {
		modelRPS.WithLabelValues(modelRate.RequestedModel).Set(modelRate.RPS)
		modelTPS.WithLabelValues(modelRate.RequestedModel).Set(modelRate.TPS)
	}

	for _, channelRate := range channels {
		channelRPS.WithLabelValues(channelRate.ChannelID, channelRate.ChannelName).Set(channelRate.RPS)
		channelTPS.WithLabelValues(channelRate.ChannelID, channelRate.ChannelName).Set(channelRate.TPS)
	}
}

func RecordHedgeRaceCompletion(winnerChannelID, loserChannelID int, outcome string) {
	winnerCh := strconv.Itoa(winnerChannelID)
	loserCh := strconv.Itoa(loserChannelID)
	axonhubHedgeRacesTotal.WithLabelValues(winnerCh, loserCh, outcome).Inc()
}

func RecordShadowCompletion(channelID int, reason string) {
	ch := strconv.Itoa(channelID)
	axonhubHedgeShadowCompletionsTotal.WithLabelValues(ch, reason).Inc()
}

func RecordObservationWindowDuration(duration time.Duration) {
	axonhubHedgeObservationWindowSeconds.WithLabelValues().Observe(duration.Seconds())
}
