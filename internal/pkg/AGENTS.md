# internal/pkg/ AGENTS.md

## OVERVIEW

Shared utility packages with x-prefixed names for the root module only; `llm/` maintains its own copies in `llm/internal/pkg/`.

## CONVENTIONS

- Packages are side-effect free unless noted; import freely.
- `xerrors` uses panic for error propagation; never recover in calling code except at API boundaries.
- Two-level cache (`xcache`) requires Redis; graceful degradation to memory-only when Redis unavailable.
- `ringbuffer/` provides O(1) timestamp-indexed lookup; do not use for ordered iteration.
- All packages in this directory belong to the root module `github.com/looplj/axonhub`.

## ANTI-PATTERNS

- Do not import `llm/internal/pkg/` packages into root module code; they diverge over time.
- Do not use `xerrors.NoErr`/`NoErr2` where errors must be inspected mid-call; they collapse error chains.
- Do not replace `ringbuffer/` with a plain slice assuming insertion order; timestamp indexing is the point.
- Do not use `xcache` two-level cache without a cache key strategy; stampedes cause hot failover.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Caching (memory+Redis) | `xcache/cache.go` | Two-tier; `NewTwoLevel`, `NewMemory` |
| Error handling with type guard | `xerrors/as.go` | `As[T](err)` pattern, panic-based |
| Collapse errors silently | `xerrors/noerr.go` | `NoErr`, `NoErr2` when caller must not see error |
| JSON with unknown fields | `xjson/rawmessage.go` | `JSONRawMessage`, schema stripping |
| Context value propagation | `xcontext/ctx.go` | `FromCtx`, `ToCtx` helpers |
| Fixed circular buffer | `ringbuffer/ring.go` | O(1) lookup by timestamp key |
| Cache invalidation pub/sub | `watcher/watcher.go` | Best-effort cross-instance |
| Redis wrapper with retry | `xredis/client.go` | Wraps `go-redis`; idempotent ops |
| Regexp with benchmarks | `xregexp/regexp.go` | Compiled and cached |
