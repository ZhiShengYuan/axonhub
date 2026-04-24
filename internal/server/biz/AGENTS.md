# Biz Package

## OVERVIEW

Monolithic god-package (~100 files, 43K lines) with all core business logic; only `provider_quota/` is a sub-package.

## CONVENTIONS

- `*_internal.go` suffix marks package-private helpers (non-standard Go convention used here)
- All services embed `*AbstractService` for `RunInTransaction` and `entFromContext`
- `authz.WithTestBypass(ctx)` skips auth checks in tests; used in 300+ test locations
- `contexts.GetUser(ctx)` returns current user (never use `ctx.Value` for this)
- `xcache.NewFromConfig[T](xcache.Config{Mode: xcache.ModeMemory})` creates in-memory cache for tests
- Test setup: `enttest.Open` + SQLite in-memory (see `test_helper.go`)
- Quota checkers live in `provider_quota/` sub-package; implement `QuotaChecker` interface

## ANTI-PATTERNS

- Using `ctx.Value` for user context: always use `contexts.GetUser(ctx)`
- Creating service without in-memory cache in tests: use `NewChannelServiceForTest` or equivalent
- Running transactions manually: always use `RunInTransaction` from `AbstractService`
- Bypassing `authz.WithTestBypass` in unit tests when testing auth-required operations

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Channel CRUD + LLM routing | `channel.go` | Main service |
| Channel internals | `channel_internal.go` | Background sync, performance recording |
| API key management | `api_key.go` | Key creation, quota validation |
| User management | `user.go` | Registration, roles, projects |
| Model associations | `model.go` | Abstract model to channel mapping |
| Request tracing | `trace.go` | Trace recording + retrieval |
| RBAC + roles | `role.go` | Role definitions, permissions |
| Project management | `project.go` | Project CRUD, member roles |
| System settings | `system.go` | Onboarding, defaults, proxy |
| Prompt management | `prompt.go` | Prompt templates, protection rules |
| Quota enforcement | `quota.go` | Usage limits, soft/hard caps |
| Cost calculation | `cost.go` | Per-request cost breakdown |
| Webhook delivery | `webhook_notifier.go` | Async event delivery |
| Test helpers | `test_helper.go` | `NewChannelServiceForTest` with memory cache |
| Base service | `abstract.go` | `RunInTransaction`, `entFromContext` |
| Sub-package quota checkers | `provider_quota/` | Per-provider quota implementations |
