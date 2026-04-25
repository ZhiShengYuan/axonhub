package orchestrator

import (
	"context"
	"hash/fnv"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
)

// HedgeSampler determines whether a request should be probed based on request ID and percentage.
// The sampler uses deterministic sampling so the same request ID always gets the same result.
type HedgeSampler interface {
	// ShouldProbe returns true if the request should be probed.
	// percentage is 0-100, where 0 means never probe and 100 means always probe.
	ShouldProbe(requestID string, percentage float64) bool
}

// DefaultHedgeSampler uses FNV-1a hash for deterministic sampling.
type DefaultHedgeSampler struct{}

// NewDeterministicHedgeSampler creates a new DefaultHedgeSampler.
func NewDeterministicHedgeSampler() *DefaultHedgeSampler {
	return &DefaultHedgeSampler{}
}

// ShouldProbe uses FNV-1a hash of the request ID for deterministic sampling.
// Returns true if (hash % 100) < percentage.
// This ensures:
// - Same request ID always returns the same result (deterministic)
// - percentage=0 → never probe (hash % 100 is always >= 0, never < 0)
// - percentage=100 → always probe (hash % 100 is always < 100)
func (s *DefaultHedgeSampler) ShouldProbe(requestID string, percentage float64) bool {
	if percentage <= 0 {
		return false
	}
	if percentage >= 100 {
		return true
	}

	hash := fnv.New64a()
	_, _ = hash.Write([]byte(requestID))
	hashValue := hash.Sum64()

	// Use modulo 100 to get a value 0-99
	remainder := hashValue % 100

	return float64(remainder) < percentage
}

// IsProbingRequest is a convenience function that uses the default sampler.
// For production use, inject a sampler via ShouldActivateHedgeWithSampler.
func IsProbingRequest(requestID string, percentage float64) bool {
	sampler := NewDeterministicHedgeSampler()
	return sampler.ShouldProbe(requestID, percentage)
}

// ShouldActivateHedgeWithSampler determines whether hedge should be activated for a request.
// It checks all 4 conditions:
//   - Request is streaming
//   - Endpoint is OpenAI/Anthropic-compatible
//   - Hedge feature is enabled
//   - At least 2 distinct candidates exist
//
// Returns (shouldHedge, isProbing):
//   - shouldHedge=true only when ALL conditions are met
//   - isProbing=true when probing mode is activated for this request
//
// The sampler is used to determine probing mode deterministically based on request ID.
func ShouldActivateHedgeWithSampler(
	ctx context.Context,
	request *httpclient.Request,
	hedgePolicy *biz.HedgePolicy,
	candidateSet *HedgeCandidateSet,
	sampler HedgeSampler,
) (bool, bool) {
	// Fast path: if hedge is disabled or policy is nil, return false immediately
	// This avoids any overhead for non-hedge requests
	if hedgePolicy == nil || !hedgePolicy.Enabled {
		return false, false
	}

	// Fast path: insufficient candidates - return false, falls back to single-channel routing
	if candidateSet == nil {
		return false, false
	}
	if candidateSet.Primary == nil || candidateSet.Secondary == nil {
		return false, false
	}

	// Check 1: Request must be streaming
	if !isStreamingRequest(request) {
		return false, false
	}

	// Check 2: Endpoint must be OpenAI or Anthropic compatible streaming
	protocol := GetHedgeProtocol(request)
	if protocol != HedgeProtocolOpenAI && protocol != HedgeProtocolAnthropic {
		return false, false
	}

	// All conditions met - determine probing mode
	shouldHedge := true
	isProbing := false

	// Use deterministic sampling to decide probing mode
	if sampler != nil && hedgePolicy.ProbingPercentage > 0 {
		requestID := request.RequestID
		if requestID == "" {
			// Fallback to empty string if no request ID - will use deterministic hash
			requestID = ""
		}
		isProbing = sampler.ShouldProbe(requestID, hedgePolicy.ProbingPercentage)
	}

	return shouldHedge, isProbing
}

// ShouldActivateHedge determines whether hedge should be activated for a request.
// Uses the default deterministic sampler.
//
// Returns (shouldHedge, isProbing):
//   - shouldHedge=true only when ALL conditions are met:
//     - Request is streaming
//     - Endpoint is OpenAI/Anthropic-compatible
//     - Hedge feature is enabled
//     - At least 2 distinct candidates exist
//   - isProbing=true when probing mode is activated for this request
//
// Config-disabled mode: If hedgePolicy is nil or Enabled=false, returns false immediately with no overhead.
// Insufficient-candidate mode: If candidateSet is nil or Primary/Secondary is nil, returns false, falls back to single-channel routing.
func ShouldActivateHedge(
	ctx context.Context,
	request *httpclient.Request,
	hedgePolicy *biz.HedgePolicy,
	candidateSet *HedgeCandidateSet,
) (bool, bool) {
	return ShouldActivateHedgeWithSampler(ctx, request, hedgePolicy, candidateSet, NewDeterministicHedgeSampler())
}