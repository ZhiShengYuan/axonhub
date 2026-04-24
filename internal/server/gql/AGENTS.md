# GraphQL Layer — `internal/server/gql/`

## OVERVIEW
Dual gqlgen modules serving Admin GraphQL (`/admin/graphql`) and OpenAPI GraphQL (`/openapi/v1/graphql`) with shared Ent schema foundation.

## CONVENTIONS

- **Schema load order**: `axonhub.graphql` MUST load first. gqlgen processes schemas in order; dependencies must precede dependents.
- **Transaction middleware**: Wrap mutations with `entgql.TransactionMiddleware` from `ent/generated/entgql.go`. Never commit/rollback directly in resolvers.
- **GUID/ID conversion**: Custom scalar converters defined in `gqlgen.yml` under `models:`. Do not modify generated converter code.
- **Generate chain**: `generate.go` runs `entc` then `gqlgen`. Run `go generate ./internal/server/gql/...` from repo root.
- **Large resolver file**: `dashboard.resolvers.go` (54K lines) is intentionally monolithic. Edit with LSP; do not split without coordinating with the team.
- **OpenAPI nested module**: `gql/openapi/` has its own `gqlgen.yml`, `generated.go`, and resolvers. Treat as independent module.

## ANTI-PATTERNS

- **DO NOT** edit `ent.graphql` (139KB auto-generated). Edit Ent schemas in `internal/ent/schema/` and regenerate.
- **DO NOT** modify `generated.go` files directly. They are overwrite on generate.
- **DO NOT** run `gqlgen` directly in subdirectories without checking `gqlgen.yml` exists for that context.
- **DO NOT** add OpenAPI-specific logic to the main `gql/` module. Keep modules isolated.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Admin schema/resolvers | `gql/` | Main gqlgen module (104K generated.go) |
| OpenAPI schema/resolvers | `gql/openapi/` | Nested gqlgen, separate gqlgen.yml |
| Dashboard resolvers | `dashboard.resolvers.go` | 54K lines; largest file |
| Query builder | `qb/` | Mixed SQL builders; imported by resolvers |
| Transaction handling | `ent/generated/entgql.go` | `TransactionMiddleware` |
| Generate scripts | `generate.go` | `go:generate` chain |
| Schema definitions | `*.graphql` | `axonhub.graphql` loads first |
| Ent schema → GraphQL | `ent.graphql` | Auto-generated, do not edit |
