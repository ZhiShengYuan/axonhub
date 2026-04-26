# AxonHub Advanced Hedged Routing & Shadow Execution

## TL;DR
> **Summary**: Add orchestrator-level hedged routing for OpenAI-compatible and Anthropic-compatible streaming requests by racing the top two ranked channel-model candidates, holding both streams in a 3-second observation buffer, selecting the winner by activity/TPS, and continuing the loser as a bounded shadow run for metrics and optional corpus capture.
> **Deliverables**:
> - Hedge coordinator integrated into the existing streaming orchestration path
> - eTPS-based ranking inputs and persistence for hedge outcomes
> - Config surfaces for hedge timing, probing, retention, and shadow deadline
> - Unit and integration coverage for race, failure, fallback, and shadow cases
> - Observability distinguishing client-delivered vs shadow-consumed output
> **Effort**: XL
> **Parallel**: YES - 6 waves
> **Critical Path**: 1 → 2 → 3 → 4 → 8 → 12 → F1-F4

## Context
### Original Request
Implement the PRD for advanced hedged routing and shadow execution with a 12-second hedge trigger, 3-second observation window, eTPS-based channel ranking, loser shadow consumption, 30-minute shadow deadline, fallback reintegration, and probing mode.

### Interview Summary
- Rollout is limited to **OpenAI-compatible streaming** and **Anthropic-compatible streaming**.
- **Streaming only** is in scope; non-streaming routing remains unchanged.
- Full loser-text harvesting is **configuration-gated**; exact hedge/shadow performance metrics are always persisted.
- Existing load-balancing, retry, buffering, and streaming infrastructure must be reused rather than replaced.

### Metis Review (gaps addressed)
- Added explicit guardrails to preserve the existing **no retry after client-visible bytes** invariant.
- Added explicit persistence/observability tasks so hedge/shadow metrics do not bypass the current recording path.
- Added fallback rules for all edge states: A fails before B starts, both fail during observation, winner fails after release, and shadow deadline expiry.
- Added acceptance criteria for client cancellation, server shutdown, probing mode, and config-disabled behavior to prevent scope drift.

## Work Objectives
### Core Objective
Add a decision-safe hedge coordinator to the orchestrator-layer streaming path that can concurrently evaluate the top two ranked channel-model candidates, release exactly one client-visible stream, continue the loser in shadow, and feed completed results back into ranking and observability without regressing the current retry/fallback guarantees.

### Deliverables
- Hedge/shadow configuration model and defaults
- Orchestrator hedge coordinator and state machine
- Candidate-selection extension for top-2 ranked stream candidates
- Observation-window buffering and winner-selection logic
- Shadow-consumption lifecycle with explicit hard deadline
- Hedge-aware performance recording, eTPS calculation, and persistence
- Hedge-aware fallback integration with the existing retry stack
- Unit/integration verification and evidence capture

### Definition of Done (verifiable conditions with commands)
- Hedged routing is executed only for enabled OpenAI/Anthropic streaming requests and disabled paths continue to use existing single-channel behavior.
- Winner selection respects: single active stream wins; if both active, higher observation-window TPS wins.
- No client-visible bytes are emitted before winner selection completes.
- Loser stream is detached from client delivery but continues in shadow until completion, forced deadline, or internal shutdown.
- Dual failure correctly falls back into the existing remaining-channel retry flow.
- Probing mode launches top-2 candidates at T=0 according to configured percentage.
- Hedge and shadow metrics persist and feed ranking without corrupting existing latency/error metrics.
- Commands: `go test ./...`, `cd llm && go test ./...`, `make test-backend-all`, and targeted integration suites for OpenAI/Anthropic streaming all pass.

### Must Have
- Hedge coordinator lives in orchestrator/pipeline integration, not provider transformers.
- Reuse `internal/server/orchestrator/candidates.go`, `select_candidates.go`, `load_balancer.go`, `performance.go`, and `llm/pipeline/pipeline.go` patterns where possible.
- Preserve `PersistenceState` release semantics from `internal/server/orchestrator/state.go`.
- Persist exact hedge outcome fields needed to compute per-request eTPS and winner/loser analytics.
- Keep loser full-text capture configurable and retention-bounded.

### Must NOT Have (guardrails, AI slop patterns, scope boundaries)
- Must NOT broaden this rollout to non-streaming requests.
- Must NOT implement hedging inside `llm/transformer/*` provider-specific transformers.
- Must NOT bypass the existing retry/fallback machinery with a separate routing stack.
- Must NOT allow both streams to write to the client response.
- Must NOT allow shadow work to outlive explicit deadline/shutdown controls.
- Must NOT record duplicate client-delivery metrics for both winner and loser.
- Must NOT add generic “manager” abstractions without anchoring them to existing orchestrator state and middleware patterns.

## Verification Strategy
> ZERO HUMAN INTERVENTION - all verification is agent-executed.
- Test decision: **tests-after** using existing Go unit, integration, and streaming test suites
- QA policy: Every task has agent-executed scenarios
- Evidence: `.sisyphus/evidence/task-{N}-{slug}.{ext}`

## Execution Strategy
### Parallel Execution Waves
> Target: 5-8 tasks per wave. <3 per wave (except final) = under-splitting.
> Extract shared dependencies as Wave-1 tasks for max parallelism.

Wave 1: config/persistence foundation, state model, candidate/ranking design, observability schema
Wave 2: hedge coordinator core, observation buffering, shadow lifecycle
Wave 3: retry/fallback integration, performance recording, probing mode
Wave 4: OpenAI wiring, Anthropic wiring, config plumbing
Wave 5: unit/integration verification, failure-path hardening, evidence generation
Wave 6: documentation-in-plan evidence updates, consolidated verification fixes

### Dependency Matrix (full, all tasks)
- 1 blocks 2, 3, 4, 8, 9
- 2 blocks 4, 5, 8
- 3 blocks 4, 5, 6
- 4 blocks 5, 6, 8, 10
- 5 blocks 6, 10
- 6 blocks 10
- 7 blocks 8, 9
- 8 blocks 10, 11
- 9 blocks 11
- 10 blocks 12
- 11 blocks 12
- 12 blocks F1-F4

### Agent Dispatch Summary (wave → task count → categories)
- Wave 1 → 3 tasks → deep, unspecified-high
- Wave 2 → 3 tasks → deep, ultrabrain
- Wave 3 → 3 tasks → deep, unspecified-high
- Wave 4 → 2 tasks → unspecified-high
- Wave 5 → 1 task → unspecified-high

## TODOs
> Implementation + Test = ONE task. Never separate.
> EVERY task MUST have: Agent Profile + Parallelization + QA Scenarios.

- [x] 1. Add hedge/shadow configuration and persistence schema

  **What to do**: Extend the existing routing/config persistence model so hedged routing can be enabled and tuned without introducing an unrelated config stack. Add system-level settings for: `enabled`, `hedge_trigger_seconds` default 12, `observation_window_seconds` default 3, `probing_percentage` default 5, `shadow_hard_deadline_minutes` default 30, `full_shadow_text_enabled` default false, and optional retention controls for stored loser text. Persist per-request hedge outcome data needed for winner/loser analysis and exact eTPS calculation by extending the existing request execution persistence path rather than creating a disconnected table unless schema constraints make extension impossible.
  **Must NOT do**: Must NOT add non-streaming config knobs, provider-specific duplicate settings, or a second persistence pipeline outside `request_execution`/related existing metrics records.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: touches ent schema, system settings, and persistence contracts that later tasks depend on.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['review-work']` - review happens in the final verification wave, not during implementation.

  **Parallelization**: Can Parallel: NO | Wave 1 | Blocks: 2, 3, 4, 8, 9 | Blocked By: none

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/ent/schema/system.go` - existing retry/storage policy fields and system-scoped settings pattern.
  - Pattern: `internal/ent/schema/apikey.go` - profile override pattern if any hedge settings are later allowed at key scope; do not add key scope unless explicitly needed by existing settings model.
  - Pattern: `internal/ent/schema/request_execution.go` - existing request execution persistence anchor for timing/stream metrics.
  - Pattern: `internal/ent/schema/usage_log.go` - token/cost persistence model; preserve consistency if hedge/shadow attribution needs usage linkage.
  - Pattern: `internal/server/biz/system.go` - `RetryPolicyOrDefault()` pattern for config retrieval/defaulting.
  - Pattern: `internal/server/orchestrator/performance.go` - current performance record lifecycle that should consume new persisted fields.
  - API/Type: `internal/server/orchestrator/state.go:45` - `StreamBufferingConfig` shape and defaulting pattern.
  - Test: `internal/server/orchestrator/stream_buffering_protocol_test.go` - verification style for timing/config-dependent streaming behavior.

  **Acceptance Criteria** (agent-executable only):
  - [ ] System config exposes hedge/shadow settings with defaults matching the PRD and disabled-safe behavior.
  - [ ] Persistence model can represent primary/secondary role, winner/loser outcome, hedge start times, first-token times, observation-window stats, eTPS inputs, shadow completion state, and optional loser-text retention metadata.
  - [ ] Existing non-hedged requests still persist successfully with zero-value/default hedge fields.
  - [ ] Generated schema/code compiles and backend tests touching config/persistence continue to pass.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Hedge config defaults load correctly
    Tool: Bash
    Steps: Run `go test ./internal/server/biz ./internal/server/orchestrator ./internal/ent/...`
    Expected: Tests pass and confirm default hedge settings do not break existing config loading.
    Evidence: .sisyphus/evidence/task-1-hedge-config.txt

  Scenario: Legacy persistence remains valid
    Tool: Bash
    Steps: Run targeted request execution persistence tests covering non-hedged records after schema changes.
    Expected: Non-hedged request persistence succeeds without requiring hedge-only fields.
    Evidence: .sisyphus/evidence/task-1-hedge-config-error.txt
  ```

  **Commit**: NO | Message: `feat(routing): add hedge configuration schema` | Files: `internal/ent/schema/system.go`, `internal/ent/schema/request_execution.go`, related generated artifacts, `internal/server/biz/system.go`

- [x] 2. Extend orchestrator state for hedge lifecycle tracking

  **What to do**: Add explicit hedge state structures to `PersistenceState` or closely adjacent orchestrator state so the runtime can track primary candidate, secondary candidate, hedge trigger time, observation-window start/end, winner identity, loser identity, client-release state, shadow deadline, cancellation causes, and whether fallback remains allowed. Model these states as explicit enums/fields rather than implicit booleans. Preserve the current release-state invariant: once client-visible bytes are emitted, normal retries cannot replay another candidate into the client response.
  **Must NOT do**: Must NOT overload existing single-stream fields in ways that make winner/loser attribution ambiguous, and must NOT store shadow-only state inside provider transformer structs.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: central state model affects all orchestration, retry, and recording tasks.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['frontend-ui-ux']` - no UI work involved.

  **Parallelization**: Can Parallel: NO | Wave 1 | Blocks: 4, 5, 8 | Blocked By: 1

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/orchestrator/state.go` - current `PersistenceState`, `StreamReleaseState`, `MarkStreamReleased()`, and `CanRetryStream()` invariants.
  - Pattern: `internal/server/orchestrator/outbound.go` - current outbound transformer dependence on `PersistenceState` for retry and persistence transitions.
  - Pattern: `internal/server/orchestrator/performance.go` - current `Perf` lifecycle embedded in persistence state.
  - Pattern: `llm/pipeline/pipeline.go:222` - retry loop expectations about retryable state.
  - External: `https://pkg.go.dev/context` - cancellation and `WithCancelCause`/`WithoutCancel` semantics to encode winner/shadow transitions safely.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Orchestrator state can represent all hedge lifecycle phases deterministically: disabled, primary-only pending, secondary launched, observation active, winner released, loser shadowing, shadow completed, shadow deadline exceeded, and fallback resumed.
  - [ ] Release-state checks still prevent client-path retry after first visible byte.
  - [ ] State transitions are covered by unit tests for all major branches, including dual failure before release and winner failure after release.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Release invariant preserved after hedge state added
    Tool: Bash
    Steps: Run `go test ./internal/server/orchestrator -run 'Test.*(Release|Retry|HedgeState)'`
    Expected: Tests confirm retries are allowed before client release and blocked after release even with hedge fields populated.
    Evidence: .sisyphus/evidence/task-2-hedge-state.txt

  Scenario: Invalid state transitions rejected
    Tool: Bash
    Steps: Run unit tests that attempt winner release before observation completion and shadow completion before loser detachment.
    Expected: Tests fail the invalid transition paths and only accept the intended state machine.
    Evidence: .sisyphus/evidence/task-2-hedge-state-error.txt
  ```

  **Commit**: NO | Message: `feat(orchestrator): add hedge lifecycle state` | Files: `internal/server/orchestrator/state.go`, related orchestrator tests

- [x] 3. Extend candidate selection and ranking for top-2 hedge dispatch

  **What to do**: Reuse the current candidate selection and load balancer path to produce a deterministic ordered top-2 set for eligible streaming requests. Keep primary selection based on highest historical streaming eTPS-equivalent score and secondary selection as the next-ranked distinct candidate. Ensure probing mode can request top-2 immediately at T=0 without re-running a separate selector path. Preserve existing priority grouping, channel/profile filtering, duplicate elimination, and top-K logic. If necessary, adapt `LoadBalancer.Sort()` output handling so hedging can consume rank-1 and rank-2 while normal retries still see the remaining ordered candidates.
  **Must NOT do**: Must NOT create a second ranking system detached from `LoadBalanceStrategy`, and must NOT allow the same channel-model pair to occupy both primary and secondary slots.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: affects core routing/ranking logic and must stay consistent with existing load balancer semantics.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['git-master']` - no git operation required during planning.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 4, 5, 6 | Blocked By: 1

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/orchestrator/candidates.go:38` - `CandidateSelector` contract.
  - Pattern: `internal/server/orchestrator/candidates.go:79` - `DefaultSelector.Select()`.
  - Pattern: `internal/server/orchestrator/candidates.go:330` - `aggregateChannelModelCandidates()` duplicate handling.
  - Pattern: `internal/server/orchestrator/select_candidates.go:18` - middleware composition and profile/tag filtering.
  - Pattern: `internal/server/orchestrator/load_balancer.go:30` - `LoadBalanceStrategy` interface.
  - Pattern: `internal/server/orchestrator/load_balancer.go:144` - `LoadBalancer.Sort()` and top-K behavior.
  - Pattern: `internal/server/orchestrator/lb_strategy_latency.go` - existing streaming latency/throughput scoring inputs.
  - Pattern: `internal/server/biz/channel_metrics.go` - in-memory metrics currently backing latency/TPS scoring.
  - Test: `internal/server/orchestrator/candidates_loadbalance_test.go`
  - Test: `internal/server/orchestrator/lb_strategy_latency_test.go`

  **Acceptance Criteria** (agent-executable only):
  - [ ] Eligible streaming requests receive a stable ordered top-2 hedge candidate set using existing filters and ranking inputs.
  - [ ] Ineligible requests or disabled hedge config still receive existing single-path behavior.
  - [ ] Duplicate candidate elimination prevents primary/secondary duplication.
  - [ ] Probing mode can force immediate top-2 launch without corrupting remaining fallback ordering.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Top-2 candidate ordering is stable
    Tool: Bash
    Steps: Run `go test ./internal/server/orchestrator -run 'Test.*(Candidate|LoadBalance|Top2|Probe)'`
    Expected: Tests prove the highest-ranked distinct candidate is primary, the second highest distinct candidate is secondary, and remaining candidates preserve retry order.
    Evidence: .sisyphus/evidence/task-3-candidates.txt

  Scenario: Duplicate and single-candidate pools degrade safely
    Tool: Bash
    Steps: Run unit tests with one candidate, duplicate channel-model matches, and profile filters leaving only one eligible channel.
    Expected: Hedges are skipped when fewer than two distinct candidates remain; no duplicate dispatch occurs.
    Evidence: .sisyphus/evidence/task-3-candidates-error.txt
  ```

  **Commit**: NO | Message: `feat(routing): add top-two hedge candidate selection` | Files: `internal/server/orchestrator/candidates.go`, `select_candidates.go`, `load_balancer.go`, related tests

- [x] 4. Implement orchestrator hedge coordinator and observation-window race

  **What to do**: Add a hedge coordinator at the orchestrator layer that owns the full race lifecycle for eligible streaming requests. Start primary A immediately. If probing mode is selected, start B immediately too; otherwise schedule B at T=12s only if A has not produced a first token. As soon as either stream produces first token, open a fixed 3-second observation window, buffer both streams in memory, and suppress all client output during that window. At observation end, select the winner: single active stream wins automatically; if both active, compute observation-window TPS and choose the higher rate. Flush the winner buffer to the client exactly once, then switch the winner to passthrough streaming. If both streams fail before release, return control to the existing fallback pipeline with remaining candidates.
  **Must NOT do**: Must NOT emit any client-visible bytes during the observation window, must NOT let loser events reach the client, and must NOT hide dual-failure errors instead of re-entering fallback.

  **Recommended Agent Profile**:
  - Category: `ultrabrain` - Reason: this is the core state machine with timing, cancellation, and race-condition sensitivity.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['playwright']` - backend orchestration task, not browser work.

  **Parallelization**: Can Parallel: NO | Wave 2 | Blocks: 5, 6, 8, 10 | Blocked By: 1, 2, 3

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/orchestrator/orchestrator.go:20` - `NewChatCompletionOrchestrator()` wiring.
  - Pattern: `internal/server/orchestrator/orchestrator.go` - `ChatCompletionOrchestrator.Process()` entry and middleware composition.
  - Pattern: `internal/server/orchestrator/stream_buffering.go:14` - existing buffer middleware and timing-based release model.
  - Pattern: `llm/streams/buffer.go` - `BufferedStream` implementation concepts; reuse only where semantics fit the observation window.
  - Pattern: `internal/server/api/chat.go:135` - `WriteSSEStreamWithErrorFormatter()` client release path.
  - Pattern: `internal/server/orchestrator/state.go` - `MarkStreamReleased()` / `CanRetryStream()` invariants.
  - Pattern: `llm/httpclient/client.go` and `llm/httpclient/decoder.go` - current streaming request/decoder behavior.
  - External: `https://go.dev/blog/pipelines` - fan-out/fan-in cancellation and merge patterns.
  - External: `https://pkg.go.dev/net/http#Flusher` - streaming flush semantics.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Primary launch, delayed secondary launch, immediate-probe launch, observation-window buffering, winner selection, and passthrough release all execute according to the PRD timing rules.
  - [ ] Client receives exactly one released stream and never sees mixed winner/loser output.
  - [ ] If both streams fail before release, fallback is invoked against remaining candidates.
  - [ ] If only one stream becomes active during the observation window, it wins without TPS comparison.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Delayed hedge selects higher-TPS winner
    Tool: Bash
    Steps: Run orchestrator integration tests with stub streams where A misses the 12s TTFT threshold, B starts at T=12s, both stream during observation, and B has higher TPS.
    Expected: No bytes are released before observation ends; B wins; buffered B tokens flush once; loser A detaches from client delivery.
    Evidence: .sisyphus/evidence/task-4-hedge-coordinator.txt

  Scenario: Dual failure before release re-enters fallback
    Tool: Bash
    Steps: Run tests where A fails before TTFT, B launches and also fails before observation completes, while a third candidate remains available.
    Expected: Hedge race ends without client release and control returns to remaining-channel fallback instead of terminating early.
    Evidence: .sisyphus/evidence/task-4-hedge-coordinator-error.txt
  ```

  **Commit**: NO | Message: `feat(orchestrator): add hedge race coordinator` | Files: `internal/server/orchestrator/orchestrator.go`, new hedge coordination files/tests, possible stream helpers

- [x] 5. Implement loser detachment, shadow consumption, and hard deadline controls

  **What to do**: Once the winner is selected and released, detach the loser from the client path and continue consuming its stream in shadow until EOF, `[DONE]`, explicit internal cancellation, or the 30-minute hard deadline. Use cancellation structure that allows shadow work to outlive client disconnect while remaining bounded by service shutdown and explicit deadline. Record completion reason: normal completion, upstream error, client disconnected before shadow completion, deadline exceeded, or server shutdown. Gate full loser-text persistence behind configuration; metrics persistence is always on.
  **Must NOT do**: Must NOT keep the loser tied to `ResponseWriter`, must NOT allow unlimited shadow lifetime, and must NOT discard loser completion reason metadata.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: requires careful cancellation-tree design and persistence semantics.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['dev-browser']` - no browser interaction involved.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 6, 10 | Blocked By: 2, 3, 4

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/orchestrator/performance.go` - stream wrappers and completion recording hooks.
  - Pattern: `internal/server/orchestrator/outbound.go:413` - `TransformStream()` wrapping/persistence style.
  - Pattern: `llm/streams/stream.go` - stream interface to consume independently from the client path.
  - Pattern: `internal/server/orchestrator/state.go` - release-state and persistence-state coordination.
  - Pattern: `internal/server/biz/channel_metrics.go` - asynchronous performance recording patterns.
  - External: `https://pkg.go.dev/context` - `WithoutCancel`, `WithTimeout`, `WithCancelCause`.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Loser stream continues after winner release without writing to the client.
  - [ ] Shadow deadline forcibly terminates long-running loser streams at 30 minutes or configured override.
  - [ ] Full loser text is retained only when the gate is enabled; completion metrics are retained regardless.
  - [ ] Client disconnect does not automatically cancel shadow unless shutdown/deadline/internal policy requires it.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Shadow continues after client-visible winner release
    Tool: Bash
    Steps: Run tests where winner releases quickly and loser continues streaming in background to completion.
    Expected: Client output ends from winner only; loser completion is still observed and persisted in shadow metrics.
    Evidence: .sisyphus/evidence/task-5-shadow.txt

  Scenario: Shadow hard deadline terminates zombie stream
    Tool: Bash
    Steps: Run tests with a loser stream that never finishes and a shortened deadline injected via config.
    Expected: Shadow consumer terminates at deadline, records timeout/deadline completion reason, and does not leak goroutines.
    Evidence: .sisyphus/evidence/task-5-shadow-error.txt
  ```

  **Commit**: NO | Message: `feat(orchestrator): add shadow stream lifecycle` | Files: hedge/shadow coordination files, `internal/server/orchestrator/performance.go`, related tests

- [x] 6. Compute hedge-aware metrics and update ranking inputs with exact eTPS

  **What to do**: Extend the current performance-recording path so both winner and loser requests capture the exact timings needed for per-request eTPS: total generated tokens, request start, first-token time, final completion time, observation-window TPS, winner/loser designation, and shadow completion reason. Feed completed results into the existing in-memory metrics/ranking system so future routing favors stronger historical eTPS at the model×channel level. Ensure only the winner contributes client-delivery metrics, while loser shadow metrics feed hedge analytics and the ranking update path deliberately defined for this feature.
  **Must NOT do**: Must NOT double-count completion tokens as client-delivered usage, and must NOT overwrite legacy latency metrics with shadow-only values without explicit field separation.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: affects ranking behavior, persistence correctness, and observability semantics.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['ai-slop-remover']` - implementation task, not cleanup.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 10 | Blocked By: 3, 4, 5

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/orchestrator/performance.go:18` - `withPerformanceRecording()` and `PerformanceRecord` lifecycle.
  - Pattern: `internal/server/orchestrator/performance.go:132` - stream wrapper that marks first token/success.
  - Pattern: `internal/server/biz/channel_metrics.go` - EWMA aggregation, startup replay, async recording.
  - Pattern: `internal/server/orchestrator/lb_strategy_latency.go` - current streaming scoring formula.
  - Pattern: `internal/metrics/prometheus.go` - exporter naming and metrics style.
  - Pattern: `internal/ent/schema/request_execution.go` - persisted timing fields.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Hedge requests persist sufficient timing/token data to compute exact eTPS for both winner and loser.
  - [ ] Ranking updates use completed hedge metrics without breaking existing non-hedged scoring.
  - [ ] Client-delivered metrics remain attributed only to the winner.
  - [ ] Prometheus/internal metrics distinguish winner delivery from shadow consumption.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Completed hedge updates eTPS inputs correctly
    Tool: Bash
    Steps: Run tests that complete both winner and loser with known timings/token counts, then inspect persisted records and in-memory metric updates.
    Expected: eTPS inputs match expected wall-clock calculations and ranking state updates accordingly.
    Evidence: .sisyphus/evidence/task-6-etps.txt

  Scenario: Winner-only delivery metrics avoid double counting
    Tool: Bash
    Steps: Run tests asserting usage/delivery counters after a hedge where both streams complete.
    Expected: Delivered-token metrics count only the winner while shadow metrics count loser consumption separately.
    Evidence: .sisyphus/evidence/task-6-etps-error.txt
  ```

  **Commit**: NO | Message: `feat(metrics): record hedge etps outcomes` | Files: `internal/server/orchestrator/performance.go`, `internal/server/biz/channel_metrics.go`, `internal/metrics/prometheus.go`, related tests

- [x] 7. Add explicit observability and audit surfaces for winner vs shadow paths

  **What to do**: Add structured logs, counters, histograms, and trace attributes that clearly distinguish: primary launch, secondary launch, observation start/end, winner chosen, loser shadowed, shadow deadline exceeded, and fallback resumed. Include per-request identifiers linking winner and loser attempts so audit/billing analysis can separate client-visible delivery from shadow-consumed output. Keep names aligned with current observability conventions.
  **Must NOT do**: Must NOT log full loser text unless the retention gate is enabled, and must NOT emit ambiguous “request completed” logs that hide whether completion refers to winner delivery or loser shadow completion.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: cross-cutting instrumentation changes with moderate complexity.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['writing']` - this is instrumentation, not prose-heavy documentation.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 8, 9 | Blocked By: 1

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/metrics/prometheus.go` - current metrics naming/export conventions.
  - Pattern: `internal/server/orchestrator/performance.go` - best insertion points for hedge/shadow instrumentation.
  - Pattern: `internal/server/biz/channel_probe.go` - periodic metric/persistence style.
  - Pattern: `internal/server/api/chat.go` - release timing hook via `ReleaseMarkCallback`.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Logs and metrics distinguish client-delivered winner output from shadow-consumed loser output.
  - [ ] Hedge lifecycle phases are observable in a single request trace/log sequence.
  - [ ] Full loser text is absent from logs when retention is disabled.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Hedge observability shows winner and shadow separately
    Tool: Bash
    Steps: Run targeted tests or integration harnesses and capture emitted metrics/logs for a successful hedge race.
    Expected: Observability output includes primary launch, secondary launch, winner selection, and shadow completion as distinct events/metrics.
    Evidence: .sisyphus/evidence/task-7-observability.txt

  Scenario: Retention-disabled mode suppresses loser text logging
    Tool: Bash
    Steps: Run a hedge flow with retention disabled and inspect logs/structured output.
    Expected: No loser response body/full text appears, while summary metrics and completion reasons still appear.
    Evidence: .sisyphus/evidence/task-7-observability-error.txt
  ```

  **Commit**: NO | Message: `feat(observability): add hedge shadow telemetry` | Files: `internal/metrics/prometheus.go`, orchestrator logging/metrics hooks, related tests

- [x] 8. Integrate hedge coordinator with existing retry and fallback machinery

  **What to do**: Thread the hedge coordinator through the existing `llm/pipeline/pipeline.go` and orchestrator retry model so pre-release failures can still use same-channel retry and remaining-channel fallback, while post-release failures remain constrained by the existing release invariant. Define the exact behavior for these cases: A fails before B starts, A fails during observation while B is active, B fails during observation while A is active, both fail during observation, winner fails after release, loser fails during shadow, and no distinct secondary candidate exists. Keep same-channel retries local to a candidate before declaring that candidate failed in the hedge race, and preserve remaining-channel fallback order after the top-2 hedge set is exhausted.
  **Must NOT do**: Must NOT reopen client-path retries after release, and must NOT lose remaining candidates because top-2 hedge handling consumed the ordered list incorrectly.

  **Recommended Agent Profile**:
  - Category: `ultrabrain` - Reason: hardest interaction boundary in the design; multiple failure dimensions must be made deterministic.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['reviewer']` - review belongs in final verification, not implementation.

  **Parallelization**: Can Parallel: NO | Wave 3 | Blocks: 10, 11 | Blocked By: 1, 2, 4, 7

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `llm/pipeline/pipeline.go:222` - main retry loop.
  - Pattern: `internal/server/orchestrator/outbound.go:476` - `HasMoreChannels()`.
  - Pattern: `internal/server/orchestrator/outbound.go:499` - `NextChannel()`.
  - Pattern: `internal/server/orchestrator/outbound.go:529` - `CanRetry()` logic.
  - Pattern: `internal/server/orchestrator/outbound.go:591` - `PrepareForRetry()`.
  - Pattern: `internal/server/orchestrator/retry.go` - retryable error classification and load balancer strategy derivation.
  - Pattern: `internal/server/orchestrator/state.go` - `CanRetryStream()` guard.
  - Test: `internal/server/orchestrator/orchestrator_streaming_test.go`
  - Test: `integration_test/openai/chat/load_balance/load_balance_test.go`

  **Acceptance Criteria** (agent-executable only):
  - [ ] Pre-release hedge failures correctly reuse same-channel retry and remaining-channel fallback logic.
  - [ ] Post-release winner failures do not trigger a second client-visible stream.
  - [ ] Remaining candidate order after the hedged top-2 set is deterministic and preserved.
  - [ ] Loser shadow failure does not corrupt winner completion or client response semantics.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Hedge race reuses fallback ordering correctly
    Tool: Bash
    Steps: Run tests where top-2 hedge candidates both fail before release and a third candidate succeeds via existing fallback order.
    Expected: Third candidate is tried through the normal retry path and the final response succeeds without mixed outputs.
    Evidence: .sisyphus/evidence/task-8-fallback.txt

  Scenario: Winner failure after release does not leak second stream
    Tool: Bash
    Steps: Run tests where the winner releases buffered output and then errors mid-stream while loser remains active in shadow.
    Expected: No loser takeover occurs on the client path; failure follows existing released-stream semantics.
    Evidence: .sisyphus/evidence/task-8-fallback-error.txt
  ```

  **Commit**: NO | Message: `feat(retry): integrate hedge fallback semantics` | Files: `llm/pipeline/pipeline.go`, `internal/server/orchestrator/outbound.go`, `internal/server/orchestrator/retry.go`, related tests

- [x] 9. Plumb hedge eligibility and protocol-specific streaming entry points

  **What to do**: Wire the hedge-capable orchestration path through the eligible API handlers and protocol transforms for OpenAI-compatible and Anthropic-compatible streaming only. Ensure handler entry points opt into hedging based on request stream mode plus config, while all non-streaming handlers and unsupported endpoints continue unchanged. Confirm that the released winner stream still emits protocol-correct SSE/event payloads through the existing writer path for each protocol.
  **Must NOT do**: Must NOT change response formats, non-streaming handlers, or provider transformer semantics beyond what is necessary to preserve protocol-correct released winner output.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: protocol wiring with moderate complexity and strong backward-compatibility requirements.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['frontend-design:frontend-design']` - no frontend changes.

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: 11 | Blocked By: 1, 7

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/api/chat.go:135` - OpenAI-compatible SSE writing path.
  - Pattern: `internal/server/api/anthropic.go` - Anthropic streaming handler entry points.
  - Pattern: `llm/transformer/openai/*stream*.go` - OpenAI stream transform boundaries.
  - Pattern: `llm/transformer/anthropic/*stream*.go` - Anthropic stream transform boundaries.
  - Pattern: `internal/server/routes.go` - route registration.
  - Test: `integration_test/openai/chat/streaming/streaming_test.go`
  - Test: `integration_test/anthropic/streaming/streaming_test.go`

  **Acceptance Criteria** (agent-executable only):
  - [ ] OpenAI-compatible and Anthropic-compatible streaming handlers can invoke hedge-capable orchestration when enabled.
  - [ ] Non-streaming handlers and unsupported protocols retain existing behavior.
  - [ ] Released winner output remains protocol-correct for both OpenAI and Anthropic streaming clients.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: OpenAI streaming path releases protocol-correct winner output
    Tool: Bash
    Steps: Run targeted OpenAI streaming integration tests through the hedged path.
    Expected: Client receives valid OpenAI-compatible SSE events only from the selected winner.
    Evidence: .sisyphus/evidence/task-9-openai.txt

  Scenario: Anthropic streaming path remains protocol-correct and scoped
    Tool: Bash
    Steps: Run targeted Anthropic streaming integration tests and a non-streaming control test.
    Expected: Anthropic streaming succeeds with hedge enabled; non-streaming behavior is unchanged.
    Evidence: .sisyphus/evidence/task-9-anthropic-error.txt
  ```

  **Commit**: NO | Message: `feat(api): wire hedge streaming entry points` | Files: `internal/server/api/chat.go`, `internal/server/api/anthropic.go`, route/orchestrator wiring, related tests

- [x] 10. Implement probing mode and config-driven hedge activation rules

  **What to do**: Add deterministic config-driven activation rules so hedging runs only when all of these are true: request is streaming, endpoint is OpenAI/Anthropic-compatible, hedge feature is enabled, and at least two distinct candidates exist. Add probing mode sampling that launches A and B at T=0 for the configured percentage of eligible requests. Define and test a stable sampling method so requests do not behave nondeterministically in tests. Ensure config-disabled mode and insufficient-candidate mode fall back to existing single-channel routing without extra overhead.
  **Must NOT do**: Must NOT apply probing to ineligible requests, and must NOT use nondeterministic randomness in tests without injectable seeding/control.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: bounded feature-flag and sampling logic with broad routing impact.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['web-searcher']` - no external current-info dependency.

  **Parallelization**: Can Parallel: NO | Wave 3 | Blocks: 12 | Blocked By: 1, 4, 5, 6, 8

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/biz/system.go` - runtime config retrieval/defaulting.
  - Pattern: `internal/server/orchestrator/orchestrator.go` - request-level strategy selection/wiring.
  - Pattern: `internal/server/orchestrator/select_candidates.go` - request eligibility and filtering.
  - Test: `internal/server/orchestrator/candidates_stream_policy_test.go`

  **Acceptance Criteria** (agent-executable only):
  - [ ] Hedge activation is limited to eligible streaming requests with feature enabled and two distinct candidates.
  - [ ] Probing mode launches both top candidates immediately at the configured percentage.
  - [ ] Sampling is deterministic under test control.
  - [ ] Disabled or ineligible requests take the existing single-channel route with no hedge artifacts.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Probing mode launches immediate dual requests
    Tool: Bash
    Steps: Run tests with injected sampling control forcing probe=true for an eligible streaming request.
    Expected: Both top candidates start at T=0 and observation begins at the first token event.
    Evidence: .sisyphus/evidence/task-10-probing.txt

  Scenario: Ineligible requests skip hedge path completely
    Tool: Bash
    Steps: Run tests for non-streaming, disabled-feature, and single-candidate requests.
    Expected: Existing routing path executes with no hedge coordinator activation or shadow records.
    Evidence: .sisyphus/evidence/task-10-probing-error.txt
  ```

  **Commit**: YES | Message: `feat(routing): add hedge activation and probing` | Files: orchestrator config plumbing, selection gates, related tests

- [x] 11. Add comprehensive unit and integration coverage for hedge, shadow, and fallback paths

  **What to do**: Add focused unit tests in `internal/server/orchestrator/` for state transitions, top-2 candidate selection, observation buffering, winner/loser selection, retry interaction, probing mode, and shadow deadline handling. Add integration coverage in existing OpenAI and Anthropic streaming test suites for successful hedge races, delayed secondary launch, immediate probing, dual failure fallback, post-release winner failure, client disconnect, and retention-gated loser capture. Reuse current streaming/load-balance integration harnesses instead of creating a new test subsystem.
  **Must NOT do**: Must NOT leave critical branches covered only by manual reasoning, and must NOT create flaky time-based tests without controllable clocks/timers.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: broad but straightforward test implementation across existing backend harnesses.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['tester']` - direct execution is enough here because the plan already defines exact test surfaces.

  **Parallelization**: Can Parallel: NO | Wave 4 | Blocks: 12 | Blocked By: 8, 9

  **References** (executor has NO interview context - be exhaustive):
  - Test: `internal/server/orchestrator/orchestrator_streaming_test.go`
  - Test: `internal/server/orchestrator/stream_buffering_protocol_test.go`
  - Test: `internal/server/orchestrator/load_balancer_test.go`
  - Test: `integration_test/openai/chat/streaming/streaming_test.go`
  - Test: `integration_test/openai/chat/load_balance/load_balance_test.go`
  - Test: `integration_test/openai/responses/streaming/client_disconnect_test.go`
  - Test: `integration_test/anthropic/streaming/streaming_test.go`
  - Pattern: `frontend/playwright.config.ts` - exists but is out of scope; do not add UI E2E unless backend verification proves impossible.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Unit tests cover hedge state machine branches and timing/selection rules.
  - [ ] Integration tests cover OpenAI and Anthropic hedged streaming paths plus key failure cases.
  - [ ] Time-sensitive tests use deterministic clocks/timers or injected durations.
  - [ ] Existing non-hedged streaming tests remain green.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Full backend test wave passes with hedge coverage
    Tool: Bash
    Steps: Run `go test ./...`, `cd llm && go test ./...`, and targeted integration suites for OpenAI and Anthropic streaming/load-balance paths.
    Expected: New hedge/shadow tests and existing streaming tests all pass.
    Evidence: .sisyphus/evidence/task-11-tests.txt

  Scenario: Client disconnect path remains safe with shadow behavior
    Tool: Bash
    Steps: Run disconnect-focused streaming tests through a hedged path with retention both on and off.
    Expected: Client disconnect does not corrupt server state; shadow follows configured cancellation/deadline rules.
    Evidence: .sisyphus/evidence/task-11-tests-error.txt
  ```

  **Commit**: YES | Message: `test(routing): cover hedge and shadow flows` | Files: orchestrator tests, integration tests under `integration_test/openai` and `integration_test/anthropic`

- [x] 12. Run full backend verification, capture evidence, and harden any failing edge paths

  **What to do**: Execute the complete backend verification wave after all feature work lands. Run root-module tests, `llm` module tests, and targeted integration suites relevant to hedged streaming. If failures expose edge-path bugs, fix only the failing hedge/shadow implementation paths without expanding scope. Store command output and any targeted rerun output as evidence files referenced by this plan.
  **Must NOT do**: Must NOT introduce unrelated refactors while fixing verification failures, and must NOT mark the feature complete until evidence files exist for the full verification run.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: verification-first stabilization with possible focused bug fixes.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['deployer']` - not a git/push step.

  **Parallelization**: Can Parallel: NO | Wave 5 | Blocks: F1, F2, F3, F4 | Blocked By: 10, 11

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `Makefile: test-backend-all` - combined root + `llm` backend test target.
  - Pattern: `.github/workflows/test.yml` - CI expectations for backend test execution.
  - Test: all files introduced/updated in Tasks 1-11.

  **Acceptance Criteria** (agent-executable only):
  - [ ] `go test ./...` passes from repo root.
  - [ ] `cd llm && go test ./...` passes from the `llm/` module.
  - [ ] `make test-backend-all` passes or is documented as redundant-equivalent with preserved evidence if already covered.
  - [ ] Evidence files are saved under `.sisyphus/evidence/` and referenced in completion notes.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Full verification succeeds
    Tool: Bash
    Steps: Run the complete backend verification command set and save outputs.
    Expected: All targeted tests pass and evidence artifacts are present under `.sisyphus/evidence/`.
    Evidence: .sisyphus/evidence/task-12-verification.txt

  Scenario: Failing edge path is fixed without scope creep
    Tool: Bash
    Steps: If a verification failure occurs, add a regression test for that exact failure and rerun the minimal failing suite plus the full verification wave.
    Expected: Regression is fixed, new test passes, and no unrelated modules are modified.
    Evidence: .sisyphus/evidence/task-12-verification-error.txt
  ```

  **Commit**: YES | Message: `fix(routing): stabilize hedged streaming verification` | Files: only failing hedge/shadow implementation and tests, evidence references

## Final Verification Wave (MANDATORY — after ALL implementation tasks)
> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
> **Do NOT auto-proceed after verification. Wait for user's explicit approval before marking work complete.**
> **Never mark F1-F4 as checked before getting user's okay.** Rejection or user feedback -> fix -> re-run -> present again -> wait for okay.
- [x] F1. Plan Compliance Audit — oracle
- [x] F2. Code Quality Review — unspecified-high
- [x] F3. Real Manual QA — unspecified-high (+ playwright if UI)
- [x] F4. Scope Fidelity Check — deep

## Commit Strategy
- Commit after Wave 2 core orchestration is stable.
- Commit after Wave 4 protocol wiring/config plumbing is complete.
- Final commit after Wave 5 verification fixes and evidence references are complete.

## Success Criteria
- Streaming requests on eligible endpoints can hedge top-2 candidates without leaking partial output.
- Winner/loser outcomes are persisted with exact per-request eTPS inputs.
- Existing retry semantics remain correct before release and blocked after release.
- Shadow work is bounded, observable, and non-blocking to the client path.
- OpenAI and Anthropic streaming integration tests cover success, failure, fallback, and probing scenarios.
