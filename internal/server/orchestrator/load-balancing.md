# Channel Load Balancing

This document describes the load balancing system for channel selection in AxonHub.

## Overview

After channels are selected based on model compatibility, they are sorted using a load balancing system that considers multiple factors to determine the optimal order for attempting requests.

### Animated Demonstration

The load balancing flow now has an accompanying SVG animation (`load-balancing.svg`) that visualizes how requests move between AxonHub and backend channels while their **Score** and progress bars fluctuate in real time. Open the file directly or embed it in documentation sites to see:

- Channel A degrading and recovering, with its score oscillating between 95 → 80 → 45 → 60 while the bar shrinks and regrows
- Channel B absorbing extra load when Channel A degrades, showing 65 → 33 → 65 score transitions
- Channel C remaining in error cooldown with a pulsing score between 20 → 18 → 10 → 12 → 15

![Load-balancing animation](./load-balancing.svg)

## Architecture

### Core Components

1. **LoadBalanceStrategy Interface** - Defines how strategies score channels
2. **LoadBalancer** - Orchestrates multiple strategies to sort channels
3. **Strategy Implementations** - Concrete implementations for different prioritization criteria

### Strategy Pattern

The load balancing system uses the Strategy pattern to make the prioritization logic extensible and composable. Each strategy independently scores channels, and the LoadBalancer combines these scores to produce a final ordering.

## Built-in Strategies

### 1. StickyRoutingStrategy (Priority: up to 900 points)

**Purpose**: Sticky routing for cache affinity using consistent hashing.

**Algorithm**:
1. Resolves a sticky key via cascading priority: trace entity → trace ID string → thread ID → API key + model → custom `X-Sticky-Key` header.
2. Uses a consistent hash ring with 150 virtual nodes per channel to map the key to a target channel.
3. If the current channel matches the target, assigns the full boost (`boostScore`, default 900); otherwise returns 0.
4. The hash ring is lazily rebuilt when the channel cache version changes (channels added/removed).

**Key Cascade** (highest to lowest priority):
| Priority | Key Source | Use Case |
|----------|-----------|----------|
| 1 | Trace entity (`X-Trace-ID` header) | Multi-turn chat cache affinity |
| 2 | Trace ID string | Same, without DB entity lookup |
| 3 | Thread entity (`X-Thread-ID` header) | Conversation-level stickiness |
| 4 | API key + model | Tenant+model-level stickiness |
| 5 | Custom `X-Sticky-Key` header | Application-defined affinity |

**Pros**:
- Zero I/O — pure computation, no database lookups (unlike the previous TraceAwareStrategy).
- Consistent hashing minimizes remapping when channels are added/removed (~1/N keys remapped).
- Covers all real use cases without configuration: multi-turn chat (trace), conversation continuity (thread), tenant isolation (apiKey+model), and custom control (header).
- Subsumes the previous TraceAwareStrategy — trace ID is the first key tried in the cascade.

**Cons**:
- With very few channels (2-3), distribution may show slight imbalance; other strategies (WeightRoundRobin, ErrorAware) compensate.
- Score of 900 dominates all other strategies combined, so error-aware override only occurs when the sticky channel is deeply unhealthy (ErrorAware drops to 0 vs 200 for healthy = 200-point swing).

### 2. ErrorAwareStrategy (Priority: 0-200 points)

**Purpose**: Deprioritizes channels based on their recent error history.

**Scoring Factors**:
- **Consecutive Failures**: -30 points per consecutive failure (decayed linearly over the cooldown period)
- **Recent Failure**: A base penalty of -40 (decayed linearly over the cooldown period)
- **Base Score**: 200 points (for healthy channels)

**Use Case**: Avoids channels experiencing issues and promotes reliable channels by penalizing failures without permanent stigmatization.

**Implementation**:
```go
NewErrorAwareStrategy(channelService)
```

### 3. WeightStrategy (Priority: 0-100 points)

**Purpose**: Respects admin-configured channel priorities.

**Algorithm**:
1. Reads the `OrderingWeight` (typically 0-100) from the channel configuration.
2. Normalizes the weight to the `maxScore` (default 100) range.
3. If weight is negative, it is clamped to 0.

**Pros**:
- Direct control over channel priority for administrators.
- Simple and predictable.

**Cons**:
- Static; does not react to live performance or load unless weights are manually updated.

### 3. WeightRoundRobinStrategy (Priority: 10-200 points)

**Purpose**: Blends historic request distribution with admin-defined weights.

**Algorithm**:
1. Fetches `AggregatedMetrics` for each channel.
2. Uses exponential decay on request counts to compute a round-robin component (0-150). Idle channels decay quickly back toward the maximum.
3. Normalizes `OrderingWeight` (0-100) to a weight component (0-50).
4. Sums both components, clamping to a minimum of 10 points to keep every healthy channel in contention.

**Pros**:
- Prevents hot channels from monopolizing requests while still honoring business priorities.
- Built-in inactivity decay means new channels warm up quickly even if they start empty.

**Cons**:
- Requires metrics storage similar to ErrorAwareStrategy.
- Heavily skewed manual weights can still override fairness, so administrators must tune carefully.

### 4. Connection Tracking and Concurrency Fallback

**Purpose**: Track in-flight requests and provide concurrency saturation signals for `RateLimitAwareStrategy`.

**Algorithm**:
1. The orchestrator increments and decrements active connections around each upstream request.
2. `RateLimitAwareStrategy` first respects explicit `MaxConcurrent` configuration.
3. If `MaxConcurrent` is not set, it falls back to the default `ConnectionTracker` capacity to penalize or exhaust saturated channels.

**Notes**:
- Connection tracking remains part of the runtime path even though `ConnectionAwareStrategy` is no longer in the default strategy chain.
- This makes concurrency protection part of rate-limit scoring instead of a standalone production strategy.

## Default Configuration

The `DefaultChannelSelector` uses these strategies in order:

```go
loadBalancer := NewLoadBalancer(
    NewStickyRoutingStrategy(channelService),                    // Priority 1: Cache affinity (consistent hash)
    NewErrorAwareStrategy(channelService),                         // Priority 2: Health
    NewWeightRoundRobinStrategy(channelService),                   // Priority 3: Fairness + admin weight
    NewLatencyAwareStrategy(channelService),                       // Priority 4: Streaming FTTL/TPS or non-streaming latency
    NewRateLimitAwareStrategy(rateLimitTracker, connectionTracker), // Priority 5: Rate limits + concurrency fallback
)
```

**Total Score Range**: ~-9790-1430 points per channel (StickyRouting 0-900 + Error 0-200 + WeightRoundRobin 10-150 + Latency 0-80 + RateLimit -10000-100)

### Default Strategy Mix Analysis

**Strengths**:
1. **Cache affinity first** – StickyRouting ensures requests with the same trace/thread/tenant consistently hit the same channel, maximizing upstream cache hit rates.
2. **Fair utilization** – WeightRoundRobin keeps new or idle channels active without ignoring business priorities.
3. **Real-time protection** – LatencyAware and RateLimitAware react to live first-token latency, throughput, end-to-end latency, concurrency, and cooldown state before a channel is fully overloaded.

**Trade-offs**:
1. Requires multiple runtime signals (trace, metrics, request history, connections); missing data downgrades overall accuracy.
2. StickyRouting score (900) dominates when a sticky key is available. When no key exists, the remaining strategies must differentiate channels with far smaller numbers, so tuning their ranges matters.
3. Concurrency protection depends on accurate connection tracking and sensible `MaxConcurrent` or tracker capacities.

## Scoring Example

Given 3 channels for a request with trace ID (sticky routing active):

| Channel | Sticky Match | Consecutive Failures | Request Load | Weight | Utilization | Total Score | Rank |
|---------|-------------|----------------------|--------------|--------|-------------|-------------|------|
| A       | Yes         | 0                    | Near 0       | 80     | 20%         | 1290        | 1    |
| C       | No          | 0                    | Low          | 50     | 20%         | 330         | 2    |
| B       | No          | 1                    | High         | 100    | 90%         | 210         | 3    |

**Calculation**:
- Channel A: 900 (sticky) + 200 (healthy) + 150 (round robin) + 40 (weight) ≈ **1290**
- Channel C: 0 (sticky) + 200 + 120 (round robin) + 25 (weight) + 40 (connection) ≈ **385** (rounded to 330)
- Channel B: 0 (sticky) + 150 (health, -50 failure penalty) + 30 (round robin due to high load) + 50 (weight) + 5 (connection) ≈ **235** (rounded to 210)

## Observability

### Structured Decision Logging

The load balancer provides comprehensive structured logging for debugging and monitoring:

**Decision Summary Log**:
```json
{
  "level": "debug",
  "timestamp": "2025-11-22T10:30:15Z",
  "msg": "Load balancing decision completed",
  "channel_count": 3,
  "duration_ms": 12.5,
  "top_channel_id": 1,
  "top_channel_name": "openai-us",
  "top_channel_score": 1280.0,
  "model": "gpt-4"
}
```

**Channel Details Log** (one per channel):
```json
{
  "level": "debug",
  "timestamp": "2025-11-22T10:30:15Z",
  "msg": "Channel load balancing details",
  "channel_id": 1,
  "channel_name": "openai-us",
  "total_score": 1280.0,
  "final_rank": 1,
  "strategy_breakdown": {
    "TraceAware": {
      "score": 1000.0,
      "duration_ms": 2.1
    },
    "ErrorAware": {
      "score": 200.0,
      "duration_ms": 5.3
    },
    "Weight": {
      "score": 80.0,
      "duration_ms": 0.1
    }
  },
  "model": "gpt-4"
}
```

**Strategy-Level Logging**:
- **TraceAwareStrategy**: Logs when boosting channels based on trace history
- ErrorAwareStrategy: Logs all penalties (consecutive failures, recent failures) and calculation details
- **WeightStrategy**: Logs weight calculation with clamping warnings

### Debug Mode

Debug mode provides enhanced observability for troubleshooting:

**Enable via Context**:
```go
opts := &chat.DebugOptions{
    Enabled:               true,
    RecordDecisionDetails: true,
    RecordStrategyDetails: true,
}
ctx = chat.EnableDebugMode(ctx, opts)
```

**DebugInfo Structure**:
```go
type DebugInfo struct {
    RequestID      string
    Timestamp      time.Time
    Model          string
    InputChannels  []ChannelDebugInfo   // Before sorting
    OutputChannels []ChannelDebugInfo   // After sorting
    TotalDuration  time.Duration
}
```

Each `ChannelDebugInfo` includes:
- Channel ID and name
- Total score
- Detailed scores from each strategy
- Strategy execution duration
- Final rank

**Retrieve Debug Info**:
```go
if info := chat.GetDebugInfo(ctx); info != nil {
    // Access detailed decision information
    for _, ch := range info.OutputChannels {
        log.Info(ctx, "Channel ranking",
            log.Int("channel_id", ch.ChannelID),
            log.Int("rank", ch.Rank),
            log.Float64("total_score", ch.TotalScore),
        )
    }
}
```

### Strategy-Specific Logs

**StickyRoutingStrategy** logs:
- Debug: When boosting a channel (score: 900, reason: "sticky_channel_matched")
- Debug: When no sticky key available (reason: "no_sticky_key")
- Debug: When channel doesn't match sticky target (reason: "sticky_channel_mismatch")

**ErrorAwareStrategy** logs:
- Debug: All penalty calculations with values and reasons
  - Consecutive failures penalty
  - Recent failure penalty (with time-based decay)
  - Low success rate penalty (< 50%)
- Debug: All boost calculations
  - Recent success boost (within 1 minute)
  - High success rate boost (> 90%)
- Warn: When metrics unavailable (uses neutral score)
- Debug: When score clamped to 0

**WeightStrategy** logs:
- Warn: When channel has negative weight (clamped to 0)
- Trace: Weight calculation details

### Viewing Logs

**Enable Debug Logging**:
```bash
export LOG_LEVEL=debug
# or for production
export LOG_LEVEL=info  # will see warnings and errors
```

**Filter Load Balancer Logs**:
```bash
# View all load balancer decisions
tail -f axonhub.log | grep "Load balancing decision"

# View specific channel details
tail -f axonhub.log | grep "Channel load balancing details"

# View StickyRouting strategy logs
tail -f axonhub.log | grep "StickyRoutingStrategy"

# View ErrorAware strategy logs
tail -f axonhub.log | grep "ErrorAwareStrategy"

# Use jq for structured JSON logs
 tail -f axonhub.log | jq 'select(.msg | contains("Load balancing"))'
 ```

**Production Log Analysis**:
```bash
# Find channels with low scores due to errors
grep "ErrorAwareStrategy.*penalty" axonhub.log | \
  jq '{channel: .channel_name, penalty_reason: .details} | select(.penalty_reason != null)'

# Analyze StickyRouting strategy effectiveness
grep "StickyRoutingStrategy: channel matched" axonhub.log | \
  jq '{channel: .channel_name, key_type: .key_type}' | \
  sort | uniq -c | sort -nr
```

### Performance Considerations

1. **Logging Overhead**: Debug-level logs have minimal performance impact when disabled (default log level is typically info or higher)
2. **Structured Logging**: Uses efficient JSON encoding with zap logger
3. **Context-Aware**: Helper functions safely extract request information from context
4. **Opt-in Debug Mode**: Debug mode is disabled by default; explicit opt-in required
5. **Graceful Degradation**: If context information is missing, logs use sensible defaults (e.g., "unknown" for model)

### Debugging Strategy Behavior

**Verify StickyRoutingStrategy**:
```bash
# Send request with trace ID for sticky routing
curl -X POST http://localhost:8090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Trace-ID: 12345" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "hello"}]
  }'

# Check logs for sticky routing
tail -f axonhub.log | grep "StickyRoutingStrategy"

# Send request with custom sticky key
curl -X POST http://localhost:8090/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Sticky-Key: my-session-id" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "hello"}]
  }'
```

**Verify ErrorAwareStrategy**:
```bash
# Force channel errors by using invalid API key# Then check logs for penalty application
tail -f axonhub.log | grep "consecutive_failures_penalty"
tail -f axonhub.log | grep "recent_failure_penalty"

# Monitor recovery after fixing errors
tail -f axonhub.log | grep "recent_success_boost"
tail -f axonhub.log | grep "high_success_rate_boost"
```

**Verify WeightStrategy**:
```bash
# Set channel weights in admin UI or database
# Channel A: weight 100
# Channel B: weight 50

# Send multiple requests and check rankings
tail -f axonhub.log | grep "WeightStrategy" | jq '{channel: .channel_name, score: .score}'
# Should see Channel A with double the score of Channel B
```

## Future Enhancements

1. **Connection Tracking**: Expose richer per-channel concurrency telemetry for observability and tuning
2. **A/B Testing**: Support for experimental channel routing
3. **Metrics Integration**: Prometheus metrics for load balancer decisions
4. **Decision Auditing**: Persistent storage of load balancing decisions for analysis
5. **Configurable Sticky Boost**: Make `boostScore` configurable per model or per API key profile

## Related Files

- `/internal/server/chat/load_balancer.go` - Core load balancer with decision logging
- `/internal/server/chat/strategies.go` - Strategy implementations with detailed logging
- `/internal/server/chat/debug.go` - Debug mode implementation and helper functions
- `/internal/server/chat/channels.go` - Channel selector integration
- `/internal/server/biz/channel.go` - Channel service with trace support
- `/internal/server/biz/channel_performance.go` - Performance metrics
