## Learnings

### 2025-04-25 Task 1: Hedge Config & Persistence Schema
- System config uses key-value store pattern (never modify System ent schema itself)
- Follow exact 7-step pattern from RetryPolicy: struct, SystemKey const, getter, setter, orDefault, normalize, default
- `make generate` must be run after any ent schema changes
- Ent schema fields for hedge must be Optional/Nillable for backward compat
- `HedgePolicy` defaults: enabled=false, trigger=12s, observation=3s, probing=5%, deadline=30min, full_text=false

### 2025-04-25 Task 2: Hedge State Lifecycle
- HedgePhase as int iota enum with String() method (matches StreamReleaseState pattern)
- Transition methods validate current phase before allowing transition
- HedgeState stored in PersistenceState alongside existing state fields
- Thread safety: HedgeState uses mutex for concurrent access

### 2025-04-25 Task 3: Top-2 Candidate Selection
- HedgeCandidateSet struct: Primary, Secondary, Remaining fields
- SelectHedgeCandidates() reuses LoadBalancer.SortWithRest() for top-k + remaining
- Duplicate elimination: same channel ID cannot be both primary and secondary
- Single-candidate pools gracefully degrade (return nil, skip hedge)
- selectHedgeCandidatesIfEnabled() checks hedge enabled + streaming before calling selection

### 2025-04-25 Task 4: Fix runHedgeRace Implementation
- `HedgeCoordinator.StartRace()` requires BOTH streams to be ready before calling (consumes them in parallel goroutines)
- Secondary stream MUST be obtained via `executeSecondaryCandidate()` BEFORE calling `StartRace`
- Winner stream from `HedgeRaceResult.WinnerStream` is returned to client
- Loser stream goes to `ShadowConsumer.StartShadow()` in background goroutine with `context.WithoutCancel`
- Real TPS computed via `ComputeHedgeMetrics(primaryBuffer, secondaryBuffer, observationWindow)`
- Graceful degradation: fall back to primary on secondary exec failure, race error, or BothFailed
- Passthrough streams created via `coordinator.newPassthroughStream(buffer)` for winner/loser when needed
