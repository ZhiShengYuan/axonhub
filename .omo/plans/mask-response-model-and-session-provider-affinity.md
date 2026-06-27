# mask-response-model-and-session-provider-affinity - Work Plan

## TL;DR (For humans)
**What you'll get:** redirected OpenAI-compatible responses will always show the model name the user asked for instead of the hidden execution model, and multi-provider model configs will stay sticky to the last successful provider for a session until that provider fails and another one succeeds.

**Why this approach:** it fixes the leak at the exact client-visible return paths instead of refactoring the whole transformer stack, and it adds provider stickiness at the candidate-ordering layer so existing retry, priority, and per-provider load balancing rules keep working.

**What it will NOT do:** it will not add Redis/DB-backed affinity.
it will not rewrite stored provider-native payloads beyond the bytes/events being returned to the client.
it will not change API-key sticky key logic or flatten existing channel priority rules.

**Effort:** Medium
**Risk:** Medium - the main risk is preserving failover reachability while adding session preference to existing retry truncation logic.
**Decisions to sanity-check:** header precedence is first-non-empty in the user-provided order; affinity is keyed by trusted scope + session ID; rebinding happens only after a fully successful response/terminal stream completion.

Your next move: choose one - start work now, or ask for the optional high-accuracy review first. Full execution detail follows below.

---

> TL;DR (machine): Medium effort, medium risk; add client-visible response-model masking for pass-through + transformed flows, and process-local scoped provider affinity with success-only rebinding.

## Scope
### Must have
- Always rewrite client-visible execution-model fields to the original request model for redirected OpenAI-compatible responses, regardless of pass-through vs non-pass-through delivery.
- Preserve the existing transformed-response masking behavior in `internal/server/orchestrator/model_mapper.go` and extend equivalent protection to raw pass-through HTTP responses and raw pass-through SSE events.
- Extract session affinity IDs case-insensitively from `X-Session-ID`, `X-Session-Affinity`, `x-claude-code-session-id`, `session-id`, `x-conversation-id`, `x-thread-id`, using the first non-empty value in that order.
- Propagate the extracted session ID into shared request context independently of trace creation, while keeping trusted session scope from auth middleware.
- Add process-local, bounded, synchronized provider affinity keyed by `sessionScope + sessionID`.
- Define provider identity by channel provider type / channel type, not by channel ID, so multiple channels from the same provider still benefit from existing load balancing.
- Prefer the last successful provider only within the same candidate priority group, and preserve at least one non-preferred retry slot when alternatives exist.
- Rebind affinity only after a full successful non-stream response or successful terminal stream completion from the fallback provider.
- Add automated regression coverage for precedence, scope isolation, pass-through masking, preferred ordering, failover reachability, and success-only rebinding.
### Must NOT have (guardrails, anti-slop, scope boundaries)
- Must NOT add Redis/database/shared-state persistence for affinity.
- Must NOT modify `TraceStickyKeyProvider` or API-key sticky key behavior.
- Must NOT rewrite stored provider-native payloads or request-execution audit blobs beyond the exact bytes/events returned to the client in pass-through mode.
- Must NOT break existing cross-priority ordering semantics when promoting the preferred provider.
- Must NOT let affinity consume every retry slot and block fallback provider selection.
- Must NOT introduce a broad transformer refactor outside middleware/session plumbing, candidate ordering, outbound success tracking, and pass-through response masking.

## Verification strategy
> Zero human intervention - all verification is agent-executed.
- Test decision: tests-after + Go package tests in `internal/server/middleware` and `internal/server/orchestrator`
- Evidence: .omo/evidence/task-<N>-mask-response-model-and-session-provider-affinity.<ext>
- Required command gates:
  - `go test ./internal/server/middleware ./internal/server/orchestrator`
- Required behavioral assertions:
  - transformed non-pass-through responses keep returning the request model;
  - raw pass-through JSON rewrites top-level `model` when present and leaves bodies unchanged when absent/invalid;
  - raw pass-through SSE never emits the execution model in top-level `model` or `response.model` fields;
  - same scoped session reuses the last successful provider;
  - fallback success rebinds later same-session requests;
  - different session scopes with the same client session ID do not share affinity;
  - no session ID => original candidate/load-balancer order stays intact.

## Execution strategy
### Parallel execution waves
> Target 5-8 todos per wave. Fewer than 3 (except the final) means you under-split.

**Wave 1 - context + masking foundation**
- Todo 1 establishes session propagation independent of trace state.
- Todo 2 patches raw pass-through JSON responses.
- Todo 3 patches raw pass-through SSE events.

**Wave 2 - provider affinity behavior**
- Todo 4 introduces the scoped in-memory affinity store and provider identity helper.
- Todo 5 applies preferred-provider ordering without starving fallback retries.
- Todo 6 records successful provider outcomes and rebinds only on completed success.

### Dependency matrix
| Todo | Depends on | Blocks | Can parallelize with |
| --- | --- | --- | --- |
| 1 | none | 4, 5, 6 | 2, 3 |
| 2 | none | final verification | 1, 3, 4 |
| 3 | none | final verification | 1, 2, 4 |
| 4 | 1 | 5, 6 | 2, 3 |
| 5 | 1, 4 | 6 | 2, 3 |
| 6 | 1, 4, 5 | final verification | 2, 3 |

## Todos
> Implementation + Test = ONE todo. Never separate.
<!-- APPEND TASK BATCHES BELOW THIS LINE WITH edit/apply_patch - never rewrite the headers above. -->
- [x] 1. Propagate a scoped session affinity ID into request context before candidate selection and independent of trace creation.
  What to do / Must NOT do: add a reusable request-session extractor in middleware, set `shared.WithSessionID(...)` from the supported header list in deterministic precedence order, and wire it into the API route middleware chain before candidate selection/trace-dependent downstream work; keep `shared.WithSessionScope(...)` from auth as the trust namespace and do not remove existing Codex/Claude trace helpers.
  Parallelization: Wave 1 | Blocked by: none | Blocks: 4, 5, 6
  References (executor has NO interview context - be exhaustive): internal/server/routes.go:159-166; internal/server/middleware/auth.go:54-68,145-158,189-206,219-225; internal/server/middleware/trace.go:76-149,195-221; llm/transformer/shared/session.go:13-36; llm/transformer/openai/codex/headers.go:28-52; llm/transformer/anthropic/claudecode/userid.go:95-104
  Acceptance criteria (agent-executable): `go test ./internal/server/middleware` passes with new cases proving header precedence, blank-header skipping, trace-disabled propagation, and scoped isolation of the shared session ID.
  QA scenarios (name the exact tool + invocation): happy - add/request tests in `internal/server/middleware` and run `go test ./internal/server/middleware -run 'Test.*Session.*'`, proving `shared.GetSessionID(ctx)` returns the chosen header value even when trace creation is disabled; failure - run the same command with cases covering all-empty headers and conflicting headers, proving the context stays unset when nothing valid exists and that the first non-empty header wins. Evidence .omo/evidence/task-1-mask-response-model-and-session-provider-affinity.txt
  Commit: Y | feat(middleware): propagate scoped session affinity ids independent of trace

- [x] 2. Patch raw pass-through JSON responses so top-level `model` never leaks the execution model.
  What to do / Must NOT do: in the raw pass-through HTTP response path, rewrite only the top-level `model` field of client-visible JSON responses to the original request model when that field exists; leave bodies unchanged when JSON is invalid, when `model` is absent, or when pass-through is disabled; do not deep-rewrite nested payload data or persisted provider-native artifacts.
  Parallelization: Wave 1 | Blocked by: none | Blocks: final verification
  References (executor has NO interview context - be exhaustive): internal/server/orchestrator/model_mapper.go:78-97,166-172; internal/server/orchestrator/pass_through.go:80-134,215-245; internal/server/orchestrator/state.go:62-83; internal/server/biz/channel.go:29-39
  Acceptance criteria (agent-executable): `go test ./internal/server/orchestrator -run 'Test.*PassThrough.*Response|Test.*ModelMapper.*'` passes with new cases proving raw JSON pass-through emits the request model and preserves bodies without a `model` field.
  QA scenarios (name the exact tool + invocation): happy - extend `internal/server/orchestrator/pass_through_test.go` and run `go test ./internal/server/orchestrator -run 'Test.*PassThrough.*Response'`, asserting a raw JSON response with `{"model":"actual-model"}` is returned as `{"model":"request-model"}`; failure - include invalid JSON / missing-field cases in the same command, asserting the helper leaves the payload untouched rather than fabricating fields or failing the response. Evidence .omo/evidence/task-2-mask-response-model-and-session-provider-affinity.txt
  Commit: N | fix(orchestrator): mask pass-through response model bytes

- [x] 3. Patch raw pass-through SSE events so streaming payloads never expose the execution model.
  What to do / Must NOT do: rewrite raw pass-through stream events before client delivery by patching top-level `model` and nested `response.model` when present; leave `[DONE]`, invalid JSON, and unrelated events untouched; do not alter event ordering, event framing, or events without model fields.
  Parallelization: Wave 1 | Blocked by: none | Blocks: final verification
  References (executor has NO interview context - be exhaustive): internal/server/orchestrator/pass_through.go:248-380; internal/server/orchestrator/pass_through_test.go:741-824; llm/transformer/openai/responses/outbound_stream.go:156-176,194-205,480-520; llm/transformer/openai/responses/aggregator.go:183-228
  Acceptance criteria (agent-executable): `go test ./internal/server/orchestrator -run 'Test.*PassThrough.*Stream'` passes with new cases proving raw SSE events rewrite `model` / `response.model` and preserve `[DONE]` plus non-model events byte-for-byte.
  QA scenarios (name the exact tool + invocation): happy - extend `internal/server/orchestrator/pass_through_test.go` and run `go test ./internal/server/orchestrator -run 'Test.*PassThrough.*Stream'`, asserting streamed chat-completion chunks and Responses API events return the request model; failure - include `[DONE]`, invalid JSON, and no-model events in the same command, asserting they remain unchanged and stream completion still works. Evidence .omo/evidence/task-3-mask-response-model-and-session-provider-affinity.txt
  Commit: Y | fix(orchestrator): mask pass-through streaming model fields

- [x] 4. Introduce a bounded process-local provider-affinity store keyed by trusted scope + session ID.
  What to do / Must NOT do: add an orchestrator-local, synchronized, bounded in-memory affinity store (LRU-style is acceptable) keyed by `sessionScope + sessionID`; store provider identity as the candidate channel provider type / channel type, not channel ID; keep this separate from `TraceStickyKeyProvider` and from any DB/Redis persistence.
  Parallelization: Wave 2 | Blocked by: 1 | Blocks: 5, 6
  References (executor has NO interview context - be exhaustive): internal/server/biz/channel_apikey_provider.go:14-38,40-98; internal/server/middleware/auth.go:219-225; llm/transformer/shared/session.go:21-36; internal/server/orchestrator/candidates.go:25-31; internal/server/orchestrator/load_balancer.go:126-176
  Acceptance criteria (agent-executable): `go test ./internal/server/orchestrator -run 'Test.*ProviderAffinity.*Store|Test.*ScopedSession.*'` passes with new cases proving same scope+session resolves to the same provider preference and different scopes do not share entries.
  QA scenarios (name the exact tool + invocation): happy - add targeted store tests and run `go test ./internal/server/orchestrator -run 'Test.*ProviderAffinity.*Store'`, asserting repeated lookups reuse the same provider identity; failure - include scope-collision cases in the same command, asserting `session-123` under two different scopes does not share affinity. Evidence .omo/evidence/task-4-mask-response-model-and-session-provider-affinity.txt
  Commit: N | feat(orchestrator): add scoped in-memory provider affinity store

- [x] 5. Apply preferred-provider ordering within each priority group without starving fallback retries.
  What to do / Must NOT do: after existing candidate filtering/load-balancer sorting, partition each priority group into preferred-provider vs non-preferred-provider slices while preserving internal order; if retries are enabled and alternatives exist, keep at least one highest-ranked non-preferred candidate in the retained retry set; if there is no affinity match, preserve the original order exactly; do not promote a lower-priority provider ahead of a higher-priority group.
  Parallelization: Wave 2 | Blocked by: 1, 4 | Blocks: 6
  References (executor has NO interview context - be exhaustive): internal/server/orchestrator/select_candidates.go:20-121; internal/server/orchestrator/candidates.go:622-704; internal/server/orchestrator/load_balancer.go:153-228,230-307; internal/server/orchestrator/state.go:44-52; internal/server/orchestrator/outbound.go:524-557
  Acceptance criteria (agent-executable): `go test ./internal/server/orchestrator -run 'Test.*LoadBalanced.*|Test.*ProviderAffinity.*Ordering'` passes with new cases proving preferred-provider promotion, unchanged ordering without affinity, and preserved fallback slots when retries are enabled.
  QA scenarios (name the exact tool + invocation): happy - extend `internal/server/orchestrator/candidates_loadbalance_test.go` and run `go test ./internal/server/orchestrator -run 'Test.*ProviderAffinity.*Ordering'`, asserting same-session requests put the preferred provider first within its priority group; failure - add cases where retries are enabled and alternate providers exist, asserting at least one non-preferred provider remains reachable and lower-priority groups are not improperly promoted. Evidence .omo/evidence/task-5-mask-response-model-and-session-provider-affinity.txt
  Commit: N | feat(orchestrator): prefer scoped session providers without breaking failover

- [x] 6. Rebind affinity only after a fully successful response/terminal stream completion from the provider that actually succeeded.
  What to do / Must NOT do: update the success path so a completed non-stream response or completed outbound stream records the successful provider into the affinity store; do not update affinity on failed attempts, incomplete/cancelled streams, or before fallback succeeds; use the final winning candidate/provider after `PrepareForRetry` / `NextChannel` decisions settle.
  Parallelization: Wave 2 | Blocked by: 1, 4, 5 | Blocks: final verification
  References (executor has NO interview context - be exhaustive): internal/server/orchestrator/outbound.go:100-211,444-461,524-660; internal/server/orchestrator/state.go:44-83; internal/server/orchestrator/model_mapper.go:78-97; internal/server/orchestrator/pass_through.go:248-380
  Acceptance criteria (agent-executable): `go test ./internal/server/orchestrator -run 'Test.*ProviderAffinity.*Rebind|Test.*NextChannel.*|Test.*PrepareForRetry.*'` passes with new cases proving A-success => prefer A, A-fail+B-success => next same-session request prefers B, and partial/failed streams do not overwrite affinity.
  QA scenarios (name the exact tool + invocation): happy - add orchestrator retry/rebind tests and run `go test ./internal/server/orchestrator -run 'Test.*ProviderAffinity.*Rebind'`, asserting a successful fallback provider becomes the next-session preference; failure - include failed-attempt / incomplete-stream cases in the same command, asserting affinity is unchanged until a clean success path is reached. Evidence .omo/evidence/task-6-mask-response-model-and-session-provider-affinity.txt
  Commit: Y | feat(orchestrator): rebind scoped provider affinity on successful fallback

## Final verification wave
> Runs in parallel after ALL todos. ALL must APPROVE. Surface results and wait for the user's explicit okay before declaring complete.
- [x] F1. Plan compliance audit
  - Verify the implemented diff touches only the planned surfaces for middleware/session plumbing, pass-through masking, candidate ordering, and outbound success tracking.
  - Evidence command: `git diff --name-only -- internal/server/middleware internal/server/orchestrator llm/transformer/shared`
- [x] F2. Code quality review
  - Run `aft_inspect` scoped to `internal/server/middleware` and `internal/server/orchestrator`, then run `go test ./internal/server/middleware ./internal/server/orchestrator`.
  - Approve only if no new diagnostics or failing tests remain.
- [x] F3. Real manual QA
  - Re-run the targeted behavior suites for pass-through JSON/SSE masking and provider rebinding, capturing exact outputs in `.omo/evidence/`.
  - Required commands: `go test ./internal/server/orchestrator -run 'Test.*PassThrough.*|Test.*ProviderAffinity.*'` and `go test ./internal/server/middleware -run 'Test.*Session.*'`.
- [x] F4. Scope fidelity
  - Confirm there is no added persistence layer, no edits to `TraceStickyKeyProvider`, and no cross-priority reorder behavior.
  - Evidence commands: `git diff -- internal/server/biz/channel_apikey_provider.go` and `git diff -- internal/server/orchestrator internal/server/middleware`

## Commit strategy
- Commit after Todo 1: session propagation foundation.
- Commit after Todo 3: complete the response-model masking component.
- Commit after Todo 6: complete the provider-affinity component.
- If the worker prefers a cleaner history, use fixup commits during execution and squash to these three logical commits before handoff.

## Success criteria
- A redirected client request never receives the backend execution model in client-visible OpenAI-compatible response bodies or stream events when the response exposes a model field.
- Existing non-pass-through masking behavior continues to pass unchanged regression coverage.
- Raw pass-through JSON rewrites only the intended top-level `model` field and does not fabricate missing fields.
- Raw pass-through SSE rewrites only `model` / `response.model` fields and leaves `[DONE]`, invalid JSON, and unrelated events untouched.
- With a valid scoped session ID, the next request prefers the last successful provider when that provider is still a candidate in the same priority group.
- When the preferred provider fails and another provider succeeds, later same-session requests prefer the successful fallback provider instead.
- Different trusted scopes using the same client session ID do not share affinity.
- When no supported session ID is present, candidate ordering and retry behavior remain equivalent to the current implementation.
