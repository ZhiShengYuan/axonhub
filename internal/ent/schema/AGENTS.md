# Ent Schema Definitions

## OVERVIEW

Ent ORM schema definitions for AxonHub entities: users, channels, API keys, models, traces, roles, projects, prompts, and more.

## CONVENTIONS

**Mixins**: Always embed `TimeMixin{}` and `schematype.SoftDeleteMixin{}` via `Mixin()` method.

**Soft Delete**: Uses `deleted_at INT DEFAULT 0` pattern. 0 = active, unix timestamp = deleted. Use `schematype.SkipSoftDelete(ctx)` to bypass in queries.

**Enums**: Define inline with `field.Enum("name").Values("val1", "val2").Default("val1")`.

**Indexes**: Unique indexes include `deleted_at` to allow reuse of unique fields after soft delete (e.g., email, name).

**Edges**: Use `edge.From(...).Through(...)` for many-to-many. Use `Annotations(entgql.Skip(...))` on reverse edges to exclude from GraphQL input.

**GraphQL Annotations**: Use `entgql.QueryField()`, `entgql.RelayConnection()`, `entgql.Mutations(...)` on schema. Use `entgql.OrderField("FIELD")` for sortable fields. Use `entgql.Skip(entgql.SkipMutationCreateInput)` to exclude fields from input types.

**Privacy**: Define `Policy()` method returning `scopes.Policy{Query: ..., Mutation: ...}` using rules from `internal/scopes/`.

**Field Types**: Use `field.JSON("name", T{})` for structured data. Use `Sensitive()` on password/credential fields.

**Custom Directives**: Define in `directives.go` (e.g., `forceResolver()`).

## ANTI-PATTERNS

- Do NOT modify generated code in `internal/ent/generated.go` or `internal/ent/**/*.go` — edit schema files only.
- Do NOT make enum fields mutable after creation — use `Immutable()` on enum fields.
- Do NOT use nullable unique indexes without `deleted_at` — conflicts occur after soft delete.
- Do NOT skip `make generate` after schema changes — GraphQL schema and ent code will drift.
- Do NOT add business logic to schema files — use `internal/server/biz/` for logic.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Add entity | `user.go`, `channel.go`, etc. | Copy existing schema pattern |
| Add mixin | `mixin.go` (TimeMixin), `schematype/soft_delete.go` | Reuse existing mixins |
| Add enum | `channel.go` (type field), `user.go` (status field) | Use `field.Enum().Values()` |
| Add relationship | `user.go` (Edges method), `role.go` | Use `edge.From().Through()` or `edge.To()` |
| Add index | `user.go` (Indexes method) | Include `deleted_at` in unique indexes |
| Add privacy rule | `user.go` (Policy method), `internal/scopes/` | Use scopes from `internal/scopes/` package |
| GraphQL config | `directives.go`, schema Annotations | Custom directives + entgql annotations |
| Soft delete logic | `schematype/soft_delete.go` | Interceptors + hooks for soft delete |
