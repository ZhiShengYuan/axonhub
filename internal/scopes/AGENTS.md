# Scopes Agent Guide

## OVERVIEW

Policy-based RBAC with default-deny; QueryPolicy/MutationPolicy evaluate rules sequentially, Skip/nil continues, absence of Allow triggers denial.

## CONVENTIONS

- Rules return `privacy.Allow`, `privacy.Skip`, or error. Never return nil from EvalQuery/EvalMutation.
- `privacy.Skip` and `nil` both mean "continue to next rule" in policy evaluation.
- `userHasSystemScope` checks direct user scopes first, then role scopes. Owner users bypass all checks.
- Project-level rules use `contexts.GetProjectID(ctx)` to extract project context.
- API key rules use `contexts.GetAPIKey(ctx)` and typically return `privacy.Deny` directly on scope mismatch (no skip).
- Mutations implement `ProjectOwnedMutation` interface to participate in project-scope filtering via `WhereP`.
- Scope slugs are defined as string constants in `scopes.go` (e.g., `ScopeReadChannels`).

## ANTI-PATTERNS

- Do not return `nil` from rule evaluation methods; return `privacy.Skip` explicitly for "not applicable".
- Do not assume user is always in context; `getUserFromContext` returns `privacy.Denyf(ErrNoUser)` if missing.
- API key context is optional; `getAPIKeyFromContext` returns `privacy.Skipf(ErrNoAPIKey)` if missing, not denial.
- Do not use `userHasSystemScope` for project-level checks; use `userHasProjectScope` instead.
- Do not bypass policy evaluation for owners without `OwnerRule()` in the policy chain; owner access is granted through rules.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Permission evaluation logic | `policy.go` | QueryPolicy/MutationPolicy with sequential rule evaluation |
| Scope constants and levels | `scopes.go` | ScopeSlug definitions, ScopeLevelSystem vs ScopeLevelProject |
| User scope checking helpers | `rule.go` | `userHasSystemScope`, `getUserFromContext`, `getAPIKeyFromContext` |
| System-level read/write rules | `rule_user_scope.go` | `UserReadScopeRule`, `UserWriteScopeRule`, `UserScopeQueryMutationRule` |
| Project-level rules | `rule_user_project_scope.go` | `UserProjectScopeReadRule`, `UserProjectScopeWriteRule`, project filtering |
| Owner bypass rule | `rule_owner.go` | `OwnerRule` grants all permissions to owner users |
| API key permission rules | `rule_apikey_scope.go` | `APIKeyScopeQueryRule`, `APIKeyScopeMutationRule`, `APIKeyProjectScopeWriteRule` |
| Privacy rule interface | Ent `privacy` package | Implements `privacy.QueryRule`, `privacy.MutationRule`, `privacy.QueryMutationRule` |
