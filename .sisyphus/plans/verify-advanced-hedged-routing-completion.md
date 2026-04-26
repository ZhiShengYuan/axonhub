# Verify Advanced Hedged Routing Completion and Correctness

## TL;DR
> **Summary**: Independently verify whether the latest completed hedge/shadow work was actually finished, whether the completion markers are truthful, and whether the implementation matches the original acceptance criteria without performing any fixes.
> **Deliverables**:
> - Completion-claim reconciliation across plan checkboxes, Boulder metadata, evidence files, and git history
> - Task-by-task verification matrix for the advanced hedged routing/shadow execution work
> - Independent source/test/evidence audit for hedge lifecycle, candidate selection, fallback, observability, and coverage claims
> - Final discrepancy report with hard verdicts: verified / partially verified / unverified / contradicted
> **Effort**: Medium
> **Parallel**: YES - 4 waves
> **Critical Path**: 1 → 3 → 5 → 7 → F1-F4

## Context
### Original Request
阅读最新的计划文件和完成情况，然后编写一个计划来验证是否真的完成并正确实现了。

### Interview Summary
- The latest and only plan file is `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md`.
- That plan marks tasks `1-12` and final review items `F1-F4` as complete.
- `.sisyphus/evidence/` contains 13 non-empty evidence files, but several filenames do not match the exact QA artifact names declared in the implementation plan.
- `.sisyphus/boulder.json` points to the same active plan and records task sessions for implementation work plus `F1`, but it does not currently substantiate `F2-F4` in the same way.
- Current git state shows the feature commits already exist on `origin/release/v0.9.x`, while local `HEAD` adds an unrelated CI-workflow removal commit; verification must therefore separate hedge/shadow implementation history from later unrelated changes.

### Metis Review (gaps addressed)
- Added an explicit truthfulness audit for checked tasks and final-review items instead of assuming completion markers are accurate.
- Added cross-source consistency checks between the plan file, `.sisyphus/boulder.json`, evidence artifacts, and git history.
- Added strict evidence-content validation so filename presence alone is insufficient proof.
- Added task verdict states (`verified`, `partially verified`, `unverified`, `contradicted`) and a discrete policy-violation check for `F1-F4` being checked despite the plan text forbidding that before explicit user approval.

## Work Objectives
### Core Objective
Produce a verification-only audit of the completed hedge/shadow work that determines, with explicit evidence, whether each claimed implementation task and final-review item was actually completed and whether the resulting code behavior matches the original plan’s acceptance criteria.

### Deliverables
- Verified inventory of planned tasks, claimed completion state, and expected evidence artifacts
- Git provenance audit isolating the actual hedge/shadow implementation commits from unrelated later commits
- Code-path audit covering configuration, state, candidate selection, coordinator, shadow handling, fallback, observability, activation, and tests
- Independent test/evidence audit covering orchestrator and integration behavior claims
- Final discrepancy matrix and overall verdict report

### Definition of Done (verifiable conditions with commands)
- Every task `1-12` and final item `F1-F4` from `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md` has one of exactly four verdicts: `verified`, `partially verified`, `unverified`, or `contradicted`.
- Expected-vs-actual evidence inventory is reconciled with exact artifact paths and content-based justification for every mismatch.
- Git inspection identifies the actual hedge/shadow commit range separately from unrelated later commits using `git log --oneline --decorate -12`, `git diff --stat origin/release/v0.9.x...HEAD`, and focused per-commit inspection.
- Source-code existence checks confirm that claimed hedge/shadow entry points and tests are present in the expected files.
- Verification commands are read-only except for test execution, and the plan produces no remediation steps or code modifications.
- Commands used by the executor include: `git status --short`, `git log --oneline --decorate -12`, `git diff --stat origin/release/v0.9.x...HEAD`, `go test ./internal/server/orchestrator/...`, `go test ./integration_test/openai/... ./integration_test/anthropic/...`, `go test ./...`, `cd llm && go test ./...`, and evidence/file inspections under `.sisyphus/`.

### Must Have
- Treat this as a forensic verification plan, not an implementation or repair plan.
- Verify completion truth across four sources: plan checkboxes, Boulder task-session metadata, evidence files, and repo/git state.
- Validate evidence by content, command scope, and plausibility — not by file existence only.
- Distinguish backend feature verification from unrelated repository state such as later CI-only commits.
- Use explicit verdict rules so incomplete proof cannot be reported as success.

### Must NOT Have (guardrails, AI slop patterns, scope boundaries)
- Must NOT edit source code, regenerate files, format files, create commits, or “fix while verifying.”
- Must NOT infer missing proof from adjacent artifacts or broad aggregate test passes.
- Must NOT broaden verification into unrelated frontend/UI work; Playwright is only allowed if a final-review claim explicitly depends on UI QA, which the current hedge/shadow scope does not require.
- Must NOT treat `HEAD` as the implementation baseline without separating the unrelated local CI-workflow commit.
- Must NOT mark any task `verified` if one of its required acceptance claims remains unsupported or contradicted.

## Verification Strategy
> ZERO HUMAN INTERVENTION - all verification is agent-executed.
- Test decision: **tests-after** using read-only repository audit plus targeted Go unit/integration verification
- QA policy: Every verification task includes both confirmation and contradiction-path checks
- Evidence: `.sisyphus/evidence/task-{N}-{slug}.{ext}` for newly executed verification outputs; existing evidence is treated as claimed artifacts to audit, not as automatically trusted proof

## Execution Strategy
### Parallel Execution Waves
> Target: 5-8 tasks per wave. <3 per wave (except final) = under-splitting.
> Extract shared dependencies as Wave-1 tasks for max parallelism.

Wave 1: claim inventory, evidence reconciliation, git baseline/provenance
Wave 2: code-path audit, acceptance-criteria mapping, review-wave substantiation
Wave 3: targeted orchestrator/integration verification, aggregate regression cross-check
Wave 4: verdict consolidation, discrepancy report, completion recommendation

### Dependency Matrix (full, all tasks)
- 1 blocks 4, 5, 7
- 2 blocks 5, 7
- 3 blocks 4, 6, 7
- 4 blocks 7
- 5 blocks 7
- 6 blocks 7
- 7 blocks 8

### Agent Dispatch Summary (wave → task count → categories)
- Wave 1 → 3 tasks → unspecified-high, deep
- Wave 2 → 3 tasks → deep, oracle
- Wave 3 → 1 task → deep
- Wave 4 → 1 task → writing

## TODOs
> Verification + proof collection = ONE task. Never separate.
> EVERY task MUST have: Agent Profile + Parallelization + QA Scenarios.

- [x] 1. Reconcile planned completion claims against recorded execution metadata

  **What to do**: Build a complete inventory of tasks `1-12` and `F1-F4` from `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md`, then compare that inventory against `.sisyphus/boulder.json` task-session records. Record for each item whether the checkbox state, task title, session record, agent category, and update timestamps are mutually consistent. Explicitly flag that the source plan text forbids checking `F1-F4` before explicit user approval, then test whether the recorded state violates that rule.
  **Must NOT do**: Must NOT assume checked boxes are truthful, and must NOT infer missing `F2-F4` session records from adjacent sessions or general repository state.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: straightforward but detail-sensitive metadata reconciliation across plan and Boulder state.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['git-master']` - this task is about `.sisyphus` metadata, not git history.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 4, 5, 7 | Blocked By: none

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md:110-653` - authoritative task list, acceptance claims, and checked completion markers.
  - Pattern: `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md:634-641` - explicit policy forbidding early completion of `F1-F4`.
  - Pattern: `.sisyphus/boulder.json:1-170` - active-plan metadata and per-task session records.
  - API/Type: verdict enum for this verification plan - `verified`, `partially verified`, `unverified`, `contradicted`.

  **Acceptance Criteria** (agent-executable only):
  - [ ] A matrix exists mapping every original task/final-review item to checkbox state, Boulder session record presence, timestamp data, and an initial verdict.
  - [ ] Any policy violation around `F1-F4` early completion is called out explicitly with source lines.
  - [ ] Missing Boulder support for checked items is classified as `unverified` or `contradicted`, never silently normalized.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Completion markers and Boulder metadata align where records exist
    Tool: Bash
    Steps: Read the source plan and `.sisyphus/boulder.json`, then generate a row-by-row reconciliation table for tasks `1-12` and `F1-F4`.
    Expected: Every item is classified with explicit support status; implementation tasks map to Boulder task sessions where present.
    Evidence: .sisyphus/evidence/task-1-claim-reconciliation.txt

  Scenario: Review-wave policy violation is detected if present
    Tool: Bash
    Steps: Compare `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md:634-641` against the current checked state of `F1-F4` and Boulder support for those items.
    Expected: Any mismatch is recorded as a policy violation instead of being treated as complete.
    Evidence: .sisyphus/evidence/task-1-claim-reconciliation-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-1-claim-reconciliation.txt`, `.sisyphus/evidence/task-1-claim-reconciliation-error.txt`

- [x] 2. Audit expected-vs-actual evidence artifacts and reconcile naming mismatches

  **What to do**: Extract every expected evidence artifact name from the original plan’s QA scenarios, list the actual files under `.sisyphus/evidence/`, and produce a reconciliation table showing exact matches, missing files, unexpected files, and content-reconciled aliases. For every alias candidate such as `task-11-coverage.txt` vs expected `task-11-tests.txt`, verify by content whether the file actually covers the intended scenario. Classify every mismatch as accepted alias, insufficient evidence, or contradiction.
  **Must NOT do**: Must NOT treat file presence as proof, and must NOT rename or rewrite existing evidence artifacts.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` - Reason: evidence inventory and content reconciliation is mechanical but must be exact.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['review-work']` - this is direct artifact auditing, not broad review orchestration.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 5, 7 | Blocked By: none

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md:136-629` - expected evidence paths for tasks `1-12`.
  - Pattern: `.sisyphus/evidence/` - actual artifact directory currently containing 13 files.
  - Pattern: `.sisyphus/evidence/task-11-coverage.txt` - example content that may map to planned test evidence.
  - Pattern: `.sisyphus/evidence/task-12-full-verification.txt` - example content that may map to planned final verification evidence.

  **Acceptance Criteria** (agent-executable only):
  - [ ] A complete expected-vs-actual evidence inventory is produced with no missing task omitted.
  - [ ] Every filename mismatch is either reconciled by content with justification or marked unsupported.
  - [ ] Missing error-path artifacts are explicitly classified and cannot inherit coverage from success-path outputs.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Evidence inventory is exhaustively reconciled
    Tool: Bash
    Steps: Enumerate expected evidence names from the original plan, list actual `.sisyphus/evidence/*` files, and compare both sets.
    Expected: Exact matches, missing files, unexpected files, and alias candidates are all listed explicitly.
    Evidence: .sisyphus/evidence/task-2-evidence-inventory.txt

  Scenario: Mismatched filenames fail unless content justifies equivalence
    Tool: Bash
    Steps: Read mismatched artifact contents and compare them to the original task QA scenario descriptions.
    Expected: Each mismatch is classified as accepted alias, insufficient evidence, or contradiction; no silent pass-through.
    Evidence: .sisyphus/evidence/task-2-evidence-inventory-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-2-evidence-inventory.txt`, `.sisyphus/evidence/task-2-evidence-inventory-error.txt`

- [x] 3. Establish git provenance and isolate the actual hedge/shadow implementation range

  **What to do**: Use read-only git inspection to separate the advanced hedge/shadow implementation commits from unrelated later branch changes. Identify the specific commits that introduced configuration/persistence, lifecycle state, coordinator/shadow logic, and test coverage; then record whether current `HEAD` contains unrelated changes that should not influence the correctness verdict of the feature itself. Treat remote `origin/release/v0.9.x` as the confirmed branch baseline for the feature commits, and explicitly note that local `HEAD` includes a later CI-workflow removal commit unrelated to hedge/shadow behavior.
  **Must NOT do**: Must NOT use a broad `HEAD` diff as proof of feature implementation, and must NOT mutate git state.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: provenance and baseline isolation affects all downstream verification conclusions.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['deployer']` - no commit or push work is allowed.

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: 4, 6, 7 | Blocked By: none

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `git log --oneline --decorate -12` - recent commit history already shows feature commits plus one later CI-only commit.
  - Pattern: `git diff --stat origin/release/v0.9.x...HEAD` - current local-only diff is unrelated CI-workflow removal.
  - Pattern: commits `79f7b93f`, `757df6c8`, `637c334a`, `c841a8d6` - observed feature commit chain for hedge/shadow work.
  - Pattern: commit `8fb51af2` - observed local-only CI workflow removal commit that should be excluded from feature correctness judgment.

  **Acceptance Criteria** (agent-executable only):
  - [ ] The feature commit range is explicitly identified and documented.
  - [ ] Unrelated local changes are separated from the hedge/shadow verification baseline.
  - [ ] The git working tree state and recent history are recorded so downstream verdicts cite the correct provenance.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Hedge/shadow commit provenance is isolated correctly
    Tool: Bash
    Steps: Run `git status --short`, `git log --oneline --decorate -12`, and `git diff --stat origin/release/v0.9.x...HEAD`, then map which commits belong to the hedge/shadow feature.
    Expected: The feature commit chain is separated from unrelated later commits and recorded as the verification baseline.
    Evidence: .sisyphus/evidence/task-3-git-provenance.txt

  Scenario: Local-only unrelated changes are not misattributed to the feature
    Tool: Bash
    Steps: Inspect the local-only diff against `origin/release/v0.9.x` and classify whether it intersects the hedge/shadow files.
    Expected: Unrelated local-only changes are excluded from feature correctness conclusions.
    Evidence: .sisyphus/evidence/task-3-git-provenance-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-3-git-provenance.txt`, `.sisyphus/evidence/task-3-git-provenance-error.txt`

- [x] 4. Verify source-code implementation coverage against the original task map

  **What to do**: Map the original plan’s implementation areas to actual source files and verify that claimed code paths exist. At minimum, inspect and reconcile configuration/persistence, state tracking, candidate selection, hedge activation, coordinator race handling, shadow consumption, fallback behavior, observability, and orchestrator wiring. For each area, record whether the code exists in the expected path, whether it appears wired into the main orchestrator flow, and whether any planned area is absent or only partially implemented.
  **Must NOT do**: Must NOT infer wiring from filenames alone, and must NOT rewrite or refactor any code during inspection.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: this is the main code-path existence and integration audit.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['tester']` - this task is source verification first; runtime checks happen later.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 7 | Blocked By: 1, 3

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `internal/server/orchestrator/state.go`
  - Pattern: `internal/server/orchestrator/candidates.go`
  - Pattern: `internal/server/orchestrator/select_candidates.go`
  - Pattern: `internal/server/orchestrator/orchestrator.go`
  - Pattern: `internal/server/orchestrator/hedge_activation.go`
  - Pattern: `internal/server/orchestrator/hedge_coordinator.go`
  - Pattern: `internal/server/orchestrator/shadow_consumer.go`
  - Pattern: `internal/server/orchestrator/hedge_fallback.go`
  - Pattern: `internal/server/orchestrator/hedge_observability.go`
  - Pattern: `internal/ent/schema/system.go`
  - Pattern: `internal/ent/schema/request_execution.go`

  **Acceptance Criteria** (agent-executable only):
  - [ ] Every major implementation area claimed by tasks `1-10` is mapped to an actual source location.
  - [ ] The audit records whether each mapped area is present, partially present, or absent.
  - [ ] Main orchestrator-path wiring is validated with code references rather than inferred from test names alone.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Claimed implementation areas exist in source and are mapped
    Tool: Bash
    Steps: Inspect the referenced orchestrator and schema files, then produce a task-to-file coverage map for tasks `1-10`.
    Expected: Each claimed implementation area is linked to concrete source files and labeled present / partial / absent.
    Evidence: .sisyphus/evidence/task-4-source-map.txt

  Scenario: Missing or unwired code paths are surfaced explicitly
    Tool: Bash
    Steps: Verify that hedge/shadow components are referenced from the orchestrator entry flow rather than existing as isolated files.
    Expected: Any orphaned or partially wired component is recorded as `partially verified` or `contradicted`.
    Evidence: .sisyphus/evidence/task-4-source-map-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-4-source-map.txt`, `.sisyphus/evidence/task-4-source-map-error.txt`

- [x] 5. Validate evidence contents against acceptance criteria and claimed QA scenarios

  **What to do**: For each original task, compare the claimed acceptance criteria and QA scenarios to the contents of any corresponding evidence artifacts. Verify whether the artifact contains the expected command output, pass/fail signal, scope, and scenario coverage. Distinguish aggregate outputs from task-specific proof, and explicitly call out cases where success-path evidence exists but failure-path evidence is missing, renamed, summarized, or unsupported.
  **Must NOT do**: Must NOT accept summarized prose inside evidence files as equivalent to command output unless it still unambiguously proves the planned scenario.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: content-vs-criterion matching is the heart of independent verification.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['frontend-ui-ux']` - no frontend design work is relevant.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 7 | Blocked By: 1, 2

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md:136-629` - original per-task acceptance criteria and QA scenarios.
  - Pattern: `.sisyphus/evidence/task-1-hedge-config.txt` through `.sisyphus/evidence/task-12-full-verification.txt` - claimed proof artifacts.
  - Pattern: `.sisyphus/evidence/task-pipeline-wiring.txt` - extra artifact that may or may not correspond to planned QA coverage.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Each task has an evidence verdict based on content, not filename existence.
  - [ ] Success-path and failure-path scenario coverage are evaluated separately.
  - [ ] Summary-only artifacts that do not prove the underlying command/scope are downgraded appropriately.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Evidence contents substantiate the original plan where valid
    Tool: Bash
    Steps: Read each claimed evidence artifact and compare its contents to the corresponding task acceptance criteria and QA scenario text.
    Expected: Each task receives an evidence verdict with exact justification.
    Evidence: .sisyphus/evidence/task-5-evidence-validation.txt

  Scenario: Missing failure-path or mismatched-scope proof is rejected
    Tool: Bash
    Steps: For each task, verify whether the planned error-path artifact or equivalent content exists and actually covers the stated failure condition.
    Expected: Unsupported or missing failure-path proof is recorded as insufficient, never silently inherited from happy-path output.
    Evidence: .sisyphus/evidence/task-5-evidence-validation-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-5-evidence-validation.txt`, `.sisyphus/evidence/task-5-evidence-validation-error.txt`

- [x] 6. Substantiate or reject the final review-wave claims (`F1-F4`)

  **What to do**: Independently verify whether `F1-F4` were actually executed and whether their scope matches the labels “Plan Compliance Audit,” “Code Quality Review,” “Real Manual QA,” and “Scope Fidelity Check.” Use the plan file, Boulder metadata, any available review artifacts, and session/evidence references to determine whether each final-review item is truly supported. Because the source plan forbids marking these complete before explicit user approval, separately assess whether the checked state itself is a policy contradiction even if some review evidence exists.
  **Must NOT do**: Must NOT infer `F2-F4` completion from a single `F1` record or from the general existence of feature tests.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: this task combines metadata, scope, and policy verification.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['playwright']` - no browser verification evidence has been discovered for this backend-only scope.

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: 7 | Blocked By: 3

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md:634-641` - final-review policy and current checked items.
  - Pattern: `.sisyphus/boulder.json:161-168` - only explicit `F1` final-wave task-session record currently observed.
  - Pattern: `.sisyphus/evidence/` - inspect whether any evidence files clearly support final-review items.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Each final-review item `F1-F4` receives an independent support verdict.
  - [ ] The plan-policy contradiction around early completion is evaluated separately from evidence of review activity.
  - [ ] Missing attributable review outputs prevent a `verified` verdict.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Supported final-review claims are attributed correctly
    Tool: Bash
    Steps: Inspect plan, Boulder, and any linked artifacts to determine whether each of `F1-F4` has attributable evidence matching its review label.
    Expected: Every final-review item is labeled verified / partially verified / unverified / contradicted with source citations.
    Evidence: .sisyphus/evidence/task-6-final-wave-review.txt

  Scenario: Premature or unsupported review completion is caught
    Tool: Bash
    Steps: Compare the checked state of `F1-F4` to the explicit policy requiring user approval and to the actual recorded support artifacts.
    Expected: Unsupported or premature completion is flagged as contradiction or unverified, not accepted.
    Evidence: .sisyphus/evidence/task-6-final-wave-review-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-6-final-wave-review.txt`, `.sisyphus/evidence/task-6-final-wave-review-error.txt`

- [x] 7. Execute targeted behavioral verification for hedge/shadow code paths and regression claims

  **What to do**: Run the targeted verification commands needed to prove or disprove the key behavioral claims of the original plan. This includes orchestrator tests covering hedge activation, candidate selection, coordinator behavior, fallback, shadow lifecycle, and observability, plus backend integration coverage for OpenAI and Anthropic streaming paths. Also run aggregate backend suites (`go test ./...`, `cd llm && go test ./...`, `make test-backend-all`) only to cross-check broader regression claims — not as a substitute for feature-specific proof. Record whether enabled-behavior tests, failure-path tests, and non-hedged regression tests actually run and pass.
  **Must NOT do**: Must NOT fix failing tests, regenerate code, or expand into unrelated frontend E2E verification.

  **Recommended Agent Profile**:
  - Category: `deep` - Reason: this task is the runtime proof layer for the source and evidence audit.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['deployer']` - no mutation or git operation is permitted.

  **Parallelization**: Can Parallel: NO | Wave 3 | Blocks: 8 | Blocked By: 1, 2, 3, 4, 5, 6

  **References** (executor has NO interview context - be exhaustive):
  - Test: `internal/server/orchestrator/hedge_activation_test.go`
  - Test: `internal/server/orchestrator/hedge_coordinator_test.go`
  - Test: `internal/server/orchestrator/hedge_fallback_test.go`
  - Test: `internal/server/orchestrator/hedge_integration_test.go`
  - Test: `internal/server/orchestrator/hedge_observability_test.go`
  - Test: `internal/server/orchestrator/shadow_consumer_test.go`
  - Test: `internal/server/orchestrator/state_test.go`
  - Test: `internal/server/orchestrator/candidates_loadbalance_test.go`
  - Test: `integration_test/openai/chat/streaming/streaming_test.go`
  - Test: `integration_test/openai/responses/streaming/streaming_test.go`
  - Test: `integration_test/anthropic/streaming/streaming_test.go`
  - Pattern: `Makefile` - `test-backend-all` target.

  **Acceptance Criteria** (agent-executable only):
  - [ ] Targeted orchestrator and integration suites are executed and their outcomes recorded.
  - [ ] Aggregate backend suites are recorded as regression cross-checks, not overclaimed as task-specific proof.
  - [ ] Any gap between passing aggregate tests and missing feature-specific proof is called out explicitly.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Targeted hedge/shadow suites prove enabled and failure-path behavior
    Tool: Bash
    Steps: Run `go test ./internal/server/orchestrator/...` and targeted integration packages under `./integration_test/openai/...` and `./integration_test/anthropic/...` that cover streaming hedge/shadow behavior.
    Expected: Results show whether core hedge/shadow behavior, fallback, and protocol-specific paths actually pass under targeted verification.
    Evidence: .sisyphus/evidence/task-7-targeted-verification.txt

  Scenario: Aggregate regression checks do not mask missing targeted proof
    Tool: Bash
    Steps: Run `go test ./...`, `cd llm && go test ./...`, and `make test-backend-all`, then compare those outcomes to the targeted suite coverage.
    Expected: Aggregate pass/fail status is recorded separately from feature-specific verification, and any mismatch in evidentiary strength is reported.
    Evidence: .sisyphus/evidence/task-7-targeted-verification-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-7-targeted-verification.txt`, `.sisyphus/evidence/task-7-targeted-verification-error.txt`

- [x] 8. Produce the final discrepancy matrix and completion verdict report

  **What to do**: Consolidate the outputs from tasks `1-7` into a single final report that lists, for every original task and final-review item, the claimed status, supporting artifacts, contradictory facts, and final verdict. The report must contain dedicated sections for: completion-claim mismatches, missing evidence, insufficient evidence, contradicted acceptance criteria, unsupported final-review claims, git-provenance caveats, and overall release readiness of the implementation as currently evidenced. End with a single overall verdict for the implementation: `fully verified`, `verified with material gaps`, `not verified`, or `contradicted by evidence`.
  **Must NOT do**: Must NOT prescribe repairs beyond identifying exact gaps, and must NOT collapse `partially verified` into success.

  **Recommended Agent Profile**:
  - Category: `writing` - Reason: synthesis-heavy reporting with strict factual traceability.
  - Skills: `[]` - no extra skills required.
  - Omitted: `['momus']` - Momus review is offered after the plan is generated, not embedded in the report task itself.

  **Parallelization**: Can Parallel: NO | Wave 4 | Blocks: F1, F2, F3, F4 | Blocked By: 7

  **References** (executor has NO interview context - be exhaustive):
  - Pattern: outputs from tasks `1-7` in this verification plan.
  - Pattern: verdict vocabulary from `## Definition of Done` and `## Success Criteria` in this plan.
  - Pattern: `.sisyphus/plans/advanced-hedged-routing-shadow-execution.md` - original claim source.

  **Acceptance Criteria** (agent-executable only):
  - [ ] The final report includes a verdict row for every original task `1-12` and `F1-F4`.
  - [ ] Discrepancies are grouped into named sections with source-backed explanations.
  - [ ] The overall verdict is derived from task-level evidence rather than narrative intuition.

  **QA Scenarios** (MANDATORY - task incomplete without these):
  ```
  Scenario: Final discrepancy report is complete and source-backed
    Tool: Bash
    Steps: Synthesize task outputs into a single matrix covering all original tasks and final-review items with verdicts and citations.
    Expected: No original task is omitted, and every verdict is justified by recorded evidence or contradiction.
    Evidence: .sisyphus/evidence/task-8-final-verdict.txt

  Scenario: Unsupported success claims remain downgraded in the final report
    Tool: Bash
    Steps: Verify that items marked `partially verified`, `unverified`, or `contradicted` are not summarized as complete in the report conclusion.
    Expected: The overall verdict preserves all material gaps and contradictions.
    Evidence: .sisyphus/evidence/task-8-final-verdict-error.txt
  ```

  **Commit**: NO | Message: `n/a` | Files: `.sisyphus/evidence/task-8-final-verdict.txt`, `.sisyphus/evidence/task-8-final-verdict-error.txt`

## Final Verification Wave (MANDATORY — after ALL implementation tasks)
> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
> **Do NOT auto-proceed after verification. Wait for user's explicit approval before marking work complete.**
> **Never mark F1-F4 as checked before getting user's okay.** Rejection or user feedback -> fix -> re-run -> present again -> wait for okay.
- [x] F1. Plan Compliance Audit — oracle
- [x] F2. Verification Quality Review — unspecified-high
- [x] F3. Evidence Integrity Review — unspecified-high
- [x] F4. Scope Fidelity Check — deep

## Commit Strategy
- No commit work in this plan. Verification is read-only except test execution and plan/evidence artifact writing.

## Success Criteria
- The executor can prove or disprove each completion claim from the latest plan without changing product code.
- Evidence mismatches, metadata gaps, and unsupported review claims are surfaced explicitly instead of being normalized away.
- The final report cleanly separates: what is truly verified, what is only partially supported, what cannot be verified, and what is contradicted by repository facts.
