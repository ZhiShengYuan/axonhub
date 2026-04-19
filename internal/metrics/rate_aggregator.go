package metrics

import (
	"strconv"
	"sync"
	"time"
)

const (
	defaultRateWindowSeconds = int64(60)
	defaultRateBucketSeconds = int64(10)
)

type RateAggregatorConfig struct {
	WindowSeconds int64
	BucketSeconds int64
}

type bucketCounts struct {
	RequestCount int64
	TokenCount   int64
}

type rateSeries struct {
	mu      sync.Mutex
	buckets map[int64]*bucketCounts
}

type ModelRate struct {
	RequestedModel string
	RPS            float64
	TPS            float64
}

type ChannelRate struct {
	ChannelID   string
	ChannelName string
	RPS         float64
	TPS         float64
}

type RateAggregator struct {
	windowSeconds int64
	bucketSeconds int64

	modelSeries   sync.Map // map[string]*rateSeries
	channelSeries sync.Map // map[string]*rateSeries
}

func NewRateAggregator(config RateAggregatorConfig) *RateAggregator {
	windowSeconds := config.WindowSeconds
	if windowSeconds <= 0 {
		windowSeconds = defaultRateWindowSeconds
	}

	bucketSeconds := config.BucketSeconds
	if bucketSeconds <= 0 {
		bucketSeconds = defaultRateBucketSeconds
	}
	if bucketSeconds > windowSeconds {
		bucketSeconds = windowSeconds
	}

	return &RateAggregator{
		windowSeconds: windowSeconds,
		bucketSeconds: bucketSeconds,
	}
}

func (a *RateAggregator) Record(model string, channelID int, channelName string, tokens int64, ts time.Time) {
	if ts.IsZero() {
		ts = time.Now()
	}

	bucketTs := a.bucketTimestamp(ts.Unix())
	expireBefore := ts.Unix() - a.windowSeconds - a.bucketSeconds

	modelKey := model
	if modelKey == "" {
		modelKey = "unknown"
	}

	modelSeries := a.getOrCreateSeries(&a.modelSeries, modelKey)
	a.recordToSeries(modelSeries, bucketTs, tokens, expireBefore)

	channelKey := channelSeriesKey(channelID, channelName)
	channelSeries := a.getOrCreateSeries(&a.channelSeries, channelKey)
	a.recordToSeries(channelSeries, bucketTs, tokens, expireBefore)
}

func (a *RateAggregator) Snapshot(now time.Time) ([]ModelRate, []ChannelRate) {
	if now.IsZero() {
		now = time.Now()
	}

	cutoff := now.Unix() - a.windowSeconds
	windowSeconds := float64(a.windowSeconds)
	expireBefore := now.Unix() - a.windowSeconds - a.bucketSeconds

	models := make([]ModelRate, 0)
	a.modelSeries.Range(func(key, value any) bool {
		model, ok := key.(string)
		if !ok {
			return true
		}

		series, ok := value.(*rateSeries)
		if !ok {
			return true
		}

		requests, tokens := a.sumSeries(series, cutoff, expireBefore)
		models = append(models, ModelRate{
			RequestedModel: model,
			RPS:            float64(requests) / windowSeconds,
			TPS:            float64(tokens) / windowSeconds,
		})

		return true
	})

	channels := make([]ChannelRate, 0)
	a.channelSeries.Range(func(key, value any) bool {
		compositeKey, ok := key.(string)
		if !ok {
			return true
		}

		series, ok := value.(*rateSeries)
		if !ok {
			return true
		}

		channelID, channelName := splitChannelSeriesKey(compositeKey)
		requests, tokens := a.sumSeries(series, cutoff, expireBefore)
		channels = append(channels, ChannelRate{
			ChannelID:   channelID,
			ChannelName: channelName,
			RPS:         float64(requests) / windowSeconds,
			TPS:         float64(tokens) / windowSeconds,
		})

		return true
	})

	return models, channels
}

func (a *RateAggregator) getOrCreateSeries(store *sync.Map, key string) *rateSeries {
	if existing, ok := store.Load(key); ok {
		if series, ok := existing.(*rateSeries); ok {
			return series
		}
	}

	created := &rateSeries{
		buckets: make(map[int64]*bucketCounts),
	}

	actual, _ := store.LoadOrStore(key, created)
	return actual.(*rateSeries)
}

func (a *RateAggregator) recordToSeries(series *rateSeries, bucketTs int64, tokens int64, expireBefore int64) {
	series.mu.Lock()
	defer series.mu.Unlock()

	bucket, ok := series.buckets[bucketTs]
	if !ok {
		bucket = &bucketCounts{}
		series.buckets[bucketTs] = bucket
	}

	bucket.RequestCount++
	bucket.TokenCount += tokens

	for ts := range series.buckets {
		if ts <= expireBefore {
			delete(series.buckets, ts)
		}
	}
}

func (a *RateAggregator) sumSeries(series *rateSeries, cutoff int64, expireBefore int64) (int64, int64) {
	series.mu.Lock()
	defer series.mu.Unlock()

	var requests int64
	var tokens int64

	for ts, bucket := range series.buckets {
		if ts <= expireBefore {
			delete(series.buckets, ts)
			continue
		}
		if ts < cutoff {
			continue
		}

		requests += bucket.RequestCount
		tokens += bucket.TokenCount
	}

	return requests, tokens
}

func (a *RateAggregator) bucketTimestamp(unixTs int64) int64 {
	return unixTs - (unixTs % a.bucketSeconds)
}

func channelSeriesKey(channelID int, channelName string) string {
	return strconv.Itoa(channelID) + "\x00" + channelName
}

func splitChannelSeriesKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '\x00' {
			return key[:i], key[i+1:]
		}
	}

	return key, ""
}
