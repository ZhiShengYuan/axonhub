# Frontend AGENTS.md

## OVERVIEW

React 19 SPA with TanStack Router/Query, Zustand state, Tailwind CSS v4, custom GraphQL client.

## STRUCTURE

```
src/
├── features/          # 23 feature modules (index.tsx page + components/ + data/)
│   └── proejct-users/ # NOTE: typo, should be project-users
├── components/ui/     # shadcn/ui (Radix + CVA)
├── routes/            # TanStack Router file-based routes
├── gql/               # Custom GraphQL client (raw fetch)
├── hooks/             # Shared hooks
├── stores/            # Zustand stores (2)
├── locales/           # i18next (en/, zh-CN/ 20 files each)
├── lib/               # api-client.ts (REST)
└── context/           # React providers
```

## WHERE TO LOOK

| Task | Location |
|------|----------|
| Add feature page | `features/<name>/index.tsx` + `features/<name>/data/schema.ts` |
| API call (REST) | `lib/api-client.ts` |
| API call (GraphQL) | `gql/graphql.ts` |
| Auth guard | `routes/_authenticated/route.tsx` |
| Permission check | `hooks/usePermissions.ts` |
| i18n key | `locales/en/` or `locales/zh-CN/` |
| Global store | `stores/` (Zustand) |
| shadcn component | `components/ui/` |
| Route definition | `routes/` (TanStack file-based) |
| E2E test | `tests/*.ts` (Playwright) |

## CONVENTIONS

- Feature files: `index.tsx` (page), `components/` (sub-components), `data/` (schema + hooks)
- Zod schemas for all API inputs in `data/schema.ts`
- i18n: use `t('key')` with nested keys, 20 files per locale
- Route params: typed via TanStack Router inference
- Tailwind: v4 with shadcn/ui, CVA for variant styles
- No Apollo/urql; custom `graphql.ts` wraps raw fetch

## ANTI-PATTERNS

- Do NOT put business logic in `components/ui/`
- Do NOT use `useState` for server data; use TanStack Query
- Do NOT hardcode API URLs; use env vars from `src/config/`
- Do NOT skip route guards for authenticated pages
- Do NOT add i18n keys directly in components; use `data/schema.ts` constants
