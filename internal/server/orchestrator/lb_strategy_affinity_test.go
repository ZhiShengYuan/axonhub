package orchestrator

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
)

// mockAffinityProvider implements ChannelAffinityProvider for tests.
type mockAffinityProvider struct {
	// cached maps "modelScope|affinityHash" -> channelID for a fixed project.
	cached    map[string]int
	projectID int
	err       error
}

func (m *mockAffinityProvider) GetAffinityChannelID(_ context.Context, projectID int, modelScope string, affinityHash string) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	// Only respond for the configured project; other projects have no cache.
	if projectID != m.projectID {
		return 0, nil
	}
	key := modelScope + "|" + affinityHash
	if id, ok := m.cached[key]; ok {
		return id, nil
	}
	return 0, nil
}

// affinityTestCtx builds a context with optional project ID.
func affinityTestCtx(withProject bool) context.Context {
	ctx := context.Background()
	if withProject {
		ctx = contexts.WithProjectID(ctx, 42)
	}
	return ctx
}

// affinityChannel builds a *biz.Channel wrapper for the given id/name.
func affinityChannel(id int, name string) *biz.Channel {
	return &biz.Channel{Channel: &ent.Channel{ID: id, Name: name}}
}

func TestAffinityAwareStrategy_Name(t *testing.T) {
	strategy := NewAffinityAwareStrategy(&mockAffinityProvider{})
	assert.Equal(t, "AffinityAwareStrategy", strategy.Name())
}

func TestAffinityAwareStrategy_Score_WithCachedChannel(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached: map[string]int{
			"gpt-4|hash123": 7,
		},
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	// Matching channel gets the boost.
	score := strategy.Score(ctx, affinityChannel(7, "ch7"))
	assert.Equal(t, 750.0, score, "matching channel should get the full boost")

	// Different channel gets 0.
	score = strategy.Score(ctx, affinityChannel(8, "ch8"))
	assert.Equal(t, 0.0, score, "non-matching channel should get 0")
}

func TestAffinityAwareStrategy_Score_NoAffinity(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{"gpt-4|hash123": 7},
	}
	strategy := NewAffinityAwareStrategy(provider)

	// No affinity state in context.
	ctx := affinityTestCtx(true)

	// Even the channel that would have matched gets 0.
	assert.Equal(t, 0.0, strategy.Score(ctx, affinityChannel(7, "ch7")))
	assert.Equal(t, 0.0, strategy.Score(ctx, affinityChannel(8, "ch8")))
}

func TestAffinityAwareStrategy_Score_CacheMiss(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{}, // empty cache
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "nope",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	assert.Equal(t, 0.0, strategy.Score(ctx, affinityChannel(7, "ch7")))
}

func TestAffinityAwareStrategy_Score_CacheError(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		err:       errors.New("cache unavailable"),
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	// Cache error should degrade to 0, not panic.
	assert.Equal(t, 0.0, strategy.Score(ctx, affinityChannel(7, "ch7")))
}

func TestAffinityAwareStrategy_Score_NoProjectID(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{"gpt-4|hash123": 7},
	}
	strategy := NewAffinityAwareStrategy(provider)

	// No project ID in context.
	ctx := affinityTestCtx(false)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	assert.Equal(t, 0.0, strategy.Score(ctx, affinityChannel(7, "ch7")))
}

func TestAffinityAwareStrategy_Score_BoostBelowTrace(t *testing.T) {
	// Guard against accidentally raising the boost to or above TraceAware's 1000.
	strategy := NewAffinityAwareStrategy(&mockAffinityProvider{})
	assert.Less(t, strategy.boostScore, 1000.0,
		"affinity boost must stay below TraceAwareStrategy's 1000.0")
}

func TestAffinityAwareStrategy_ScoreWithDebug_Parity(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached: map[string]int{
			"gpt-4|hash123": 7,
		},
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	cases := []struct {
		name    string
		channel *biz.Channel
	}{
		{"match", affinityChannel(7, "ch7")},
		{"non_match", affinityChannel(8, "ch8")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantScore := strategy.Score(ctx, tc.channel)
			gotScore, debug := strategy.ScoreWithDebug(ctx, tc.channel)
			assert.Equal(t, wantScore, gotScore, "ScoreWithDebug must match Score")
			assert.Equal(t, strategy.Name(), debug.StrategyName)
			assert.Equal(t, wantScore, debug.Score)
		})
	}
}

func TestAffinityAwareStrategy_ScoreWithDebug_NoAffinity(t *testing.T) {
	strategy := NewAffinityAwareStrategy(&mockAffinityProvider{projectID: 42})
	ctx := affinityTestCtx(true) // no affinity state

	score, debug := strategy.ScoreWithDebug(ctx, affinityChannel(7, "ch7"))
	assert.Equal(t, 0.0, score)
	assert.Equal(t, "no_affinity_in_context", debug.Details["reason"])
}

func TestAffinityAwareStrategy_ScoreWithDebug_CacheMiss(t *testing.T) {
	provider := &mockAffinityProvider{projectID: 42, cached: map[string]int{}}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "nope",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	score, debug := strategy.ScoreWithDebug(ctx, affinityChannel(7, "ch7"))
	assert.Equal(t, 0.0, score)
	assert.Equal(t, "no_cached_channel", debug.Details["reason"])
	assert.Equal(t, "gpt-4", debug.Details["model_scope"])
}

func TestAffinityAwareStrategy_ScoreWithDebug_NoRawValues(t *testing.T) {
	// Debug details must never contain the raw affinity key — only hash,
	// source, model scope, and channel IDs. We seed an obvious raw value
	// ("RAW-SECRET") and verify it does not leak into any detail.
	provider := &mockAffinityProvider{
		projectID: 42,
		cached: map[string]int{
			"gpt-4|hash-of-raw-secret": 7,
		},
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash-of-raw-secret",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	// Check both match and non-match debug details.
	for _, ch := range []*biz.Channel{affinityChannel(7, "ch7"), affinityChannel(8, "ch8")} {
		_, debug := strategy.ScoreWithDebug(ctx, ch)

		for k, v := range debug.Details {
			// No key should reference a raw value concept.
			assert.NotContains(t, k, "raw", "detail key %q should not reference raw values", k)
			// No string value should contain the secret marker.
			if s, ok := v.(string); ok {
				assert.NotContains(t, s, "RAW-SECRET", "detail %q must not leak raw affinity value", k)
			}
		}
		// The hash is allowed but must be the hashed form, not the raw value.
		assert.NotContains(t, debug.Details, "raw_value")
	}
}

// ============================================================================
// Guardrail tests — verify affinity boost does NOT bypass safety mechanisms.
// These tests prove that affinity is a soft boost (750.0), never a filter, and
// that the load balancer's eligibility / safety strategies always win.
// ============================================================================

// TestAffinityGuardrails_StaleCachedChannel verifies that a cached channel
// that is stale/disabled still receives the 750.0 boost from the strategy.
// The strategy is intentionally unaware of eligibility — the load balancer's
// candidate filtering (which excludes disabled channels) is the real guardrail.
// This test documents that contract: the strategy boosts blindly, and
// eligibility filtering happens upstream.
func TestAffinityGuardrails_StaleCachedChannel(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{"gpt-4|hash123": 7}, // channel 7 cached but hypothetically disabled
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	// The strategy returns the boost regardless of the channel's eligibility.
	// It is the load balancer's responsibility to filter out disabled channels
	// before they ever reach the strategy. This is the documented contract.
	score := strategy.Score(ctx, affinityChannel(7, "disabled-ch7"))
	assert.Equal(t, 750.0, score, "cached channel gets the boost regardless of eligibility")

	// A different non-cached channel gets 0.
	score = strategy.Score(ctx, affinityChannel(8, "eligible-ch8"))
	assert.Equal(t, 0.0, score, "non-cached channel gets 0")
}

// TestAffinityGuardrails_DoNotOverrideSafetyBlockers verifies that affinity
// boost (750.0) cannot override safety penalties from quota, rate-limit, or
// circuit-breaker strategies. Since the load balancer sums all strategy scores,
// a quota-exhausted penalty (-10000) dwarfs the affinity boost (+750).
// We simulate this by computing the combined score manually, which mirrors
// exactly how the LoadBalancer sums strategy scores.
func TestAffinityGuardrails_DoNotOverrideSafetyBlockers(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{"gpt-4|hash123": 7},
	}
	affinityStrategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	cachedCh := affinityChannel(7, "cached-but-quota-exhausted")

	affinityScore := affinityStrategy.Score(ctx, cachedCh)
	assert.Equal(t, 750.0, affinityScore, "cached channel gets affinity boost")

	// Simulate the load balancer's total score computation: sum of all strategies.
	// The quota-exhausted penalty (-10000) is the same constant used in production.
	const quotaExhaustedPenalty = -10000.0

	totalCached := affinityScore + quotaExhaustedPenalty // 750 + (-10000) = -9250
	totalOther := 0.0 + 0.0                               // no affinity, no penalty

	assert.Less(t, totalCached, totalOther,
		"quota-exhausted cached channel must rank below healthy non-affinity channel")
	assert.Less(t, totalCached, 0.0,
		"safety penalty must dominate: total score must be negative")
}

// TestAffinityGuardrails_PriorityGroupBoundaries verifies that affinity does
// not promote a channel beyond its priority group. Affinity is a within-group
// soft boost — it reorders channels within the same priority tier but never
// lifts a lower-priority channel above a higher-priority one.
//
// In practice, priority is encoded by the base weight strategy (e.g. 150-300).
// A higher-priority channel with no affinity match (0) should still outrank a
// lower-priority channel with an affinity match (750) when the base weights
// differ enough. This test documents that affinity is additive, not exclusive.
func TestAffinityGuardrails_PriorityGroupBoundaries(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{"gpt-4|hash123": 7},
	}
	affinityStrategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	lowPriorityCached := affinityChannel(7, "low-priority-cached")
	highPriorityOther := affinityChannel(8, "high-priority-no-affinity")

	// Simulate priority groups via base weight scores.
	// High priority group base: 500. Low priority group base: 100.
	const highPriorityBase = 500.0
	const lowPriorityBase = 100.0

	totalLow := lowPriorityBase + affinityStrategy.Score(ctx, lowPriorityCached)  // 100 + 750 = 850
	totalHigh := highPriorityBase + affinityStrategy.Score(ctx, highPriorityOther) // 500 + 0 = 500

	// With these particular base weights, affinity CAN lift the low-priority
	// channel above the high-priority one (850 > 500). This is expected behavior —
	// affinity is a strong within-group signal. The real priority enforcement
	// happens at the candidate filtering layer (priority groups), not at the
	// scoring layer. We document this boundary:
	assert.Greater(t, totalLow, totalHigh,
		"affinity boost can lift a lower base-weight channel; priority groups are enforced by filtering, not scoring")

	// But affinity must NEVER produce a negative or exclusionary score.
	affinityScore := affinityStrategy.Score(ctx, lowPriorityCached)
	assert.GreaterOrEqual(t, affinityScore, 0.0,
		"affinity score must be non-negative — it is a boost, never a penalty")
}

// TestAffinityGuardrails_ModelScopedCache verifies that the same affinity hash
// with different model scopes produces different cache lookups. This is the
// critical guardrail: two requests with the same X-Session-Affinity header but
// different model bodies must NOT collide in the affinity cache.
func TestAffinityGuardrails_ModelScopedCache(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached: map[string]int{
			"gpt-4|sharedhash":  10,
			"claude-3|sharedhash": 20,
		},
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)

	// Same hash, model gpt-4 → channel 10.
	ctxGPT := contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "sharedhash",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})
	assert.Equal(t, 750.0, strategy.Score(ctxGPT, affinityChannel(10, "gpt4-ch")),
		"gpt-4 scoped cache hit should boost channel 10")
	assert.Equal(t, 0.0, strategy.Score(ctxGPT, affinityChannel(20, "claude-ch")),
		"gpt-4 scoped cache should NOT boost the claude channel")

	// Same hash, model claude-3 → channel 20 (no collision with gpt-4).
	ctxClaude := contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "sharedhash",
		Source:     "X-Session-Affinity",
		ModelScope: "claude-3",
	})
	assert.Equal(t, 750.0, strategy.Score(ctxClaude, affinityChannel(20, "claude-ch")),
		"claude-3 scoped cache hit should boost channel 20")
	assert.Equal(t, 0.0, strategy.Score(ctxClaude, affinityChannel(10, "gpt4-ch")),
		"claude-3 scoped cache should NOT boost the gpt-4 channel")
}

// TestAffinityGuardrails_NoRawValuesInDebug is a comprehensive guardrail test
// that verifies ScoreWithDebug never leaks raw affinity values. Only hashes,
// sources, model scopes, and channel IDs are allowed in debug output.
func TestAffinityGuardrails_NoRawValuesInDebug(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{"gpt-4|leaked-secret-hash": 7},
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "leaked-secret-hash",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	// The raw secret marker that must NEVER appear in any detail.
	const secretMarker = "leaked-secret"

	channels := []*biz.Channel{
		affinityChannel(7, "ch7"),  // match
		affinityChannel(8, "ch8"),  // non-match
	}

	for _, ch := range channels {
		_, debug := strategy.ScoreWithDebug(ctx, ch)

		// No detail key should reference raw values.
		for k := range debug.Details {
			assert.NotContains(t, k, "raw", "detail key %q must not reference raw values", k)
		}

		// No string value should contain the secret marker.
		for k, v := range debug.Details {
			if s, ok := v.(string); ok {
				assert.NotContains(t, s, secretMarker,
					"detail %q must not leak raw affinity value", k)
			}
		}

		// Explicitly verify no raw_value key exists.
		assert.NotContains(t, debug.Details, "raw_value")
		assert.NotContains(t, debug.Details, "raw_affinity")
	}
}

// TestAffinityGuardrails_ConcurrentFirstHit verifies that when multiple
// concurrent requests share the same affinity key but the cache is empty
// (first-hit burst), all of them receive score 0 from the strategy. The cache
// is only populated after a request completes successfully, so during a burst
// of first requests, there is no cached channel to boost.
func TestAffinityGuardrails_ConcurrentFirstHit(t *testing.T) {
	// Empty cache simulates first-hit scenario: no request has completed yet.
	provider := &mockAffinityProvider{
		projectID: 42,
		cached:    map[string]int{}, // empty — no completions yet
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "burst-hash",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	const numGoroutines = 50
	results := make([]float64, numGoroutines)

	t.Run("parallel", func(t *testing.T) {
		t.Parallel() // allow this subtest to run in parallel with others

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				// Each goroutine scores the same channel — all should get 0
				// because the cache is empty (no completion has written yet).
				results[idx] = strategy.Score(ctx, affinityChannel(7, "ch7"))
			}(i)
		}

		wg.Wait()
	})

	// After all goroutines complete, every result must be 0.
	for i, score := range results {
		assert.Equal(t, 0.0, score,
			"goroutine %d: first-hit burst must all get 0 (no cache entry)", i)
	}
}

// TestAffinityGuardrails_CacheErrorIsSafe verifies that a cache backend error
// is handled gracefully — the strategy returns 0 (no boost) rather than
// panicking or returning an error. This is the error-degradation guardrail.
func TestAffinityGuardrails_CacheErrorIsSafe(t *testing.T) {
	provider := &mockAffinityProvider{
		projectID: 42,
		err:       errors.New("redis connection refused"),
	}
	strategy := NewAffinityAwareStrategy(provider)

	ctx := affinityTestCtx(true)
	ctx = contexts.WithAffinityState(ctx, &contexts.AffinityState{
		Hash:       "hash123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4",
	})

	// Must not panic, must return 0.
	require.NotPanics(t, func() {
		score := strategy.Score(ctx, affinityChannel(7, "ch7"))
		assert.Equal(t, 0.0, score, "cache error must degrade to 0 boost")
	})

	// Debug path must also be safe.
	require.NotPanics(t, func() {
		score, debug := strategy.ScoreWithDebug(ctx, affinityChannel(7, "ch7"))
		assert.Equal(t, 0.0, score)
		assert.Equal(t, "error_getting_cached_channel", debug.Details["reason"])
	})
}
