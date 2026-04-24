# AGENTS.md — llm Module

## OVERVIEW

Bidirectional transformer pipeline for AI provider APIs (18+ providers, unified Request/Response model).

## STRUCTURE

```
llm/
├── auth/           # APIKey provider, OAuth abstractions
├── bedrock/        # AWS Bedrock transformer
├── httpclient/     # HTTP client configuration
├── internal/       # Internal utilities
├── oauth/          # OAuth implementations
├── pipeline/       # Pipeline factory, retry, middleware
├── simulator/      # Testing simulator
├── streams/        # Stream handling
├── tools/          # Tool/function calling
├── transformer/    # 18+ provider transformers (openai, anthropic, gemini, etc.)
├── vertex/         # Google Cloud Vertex AI transformer
├── model.go        # Unified llm.Request / llm.Response
├── cache.go        # Caching interface
├── embedding.go    # Embedding operations
├── rerank.go       # Reranking operations
└── video.go        # Video operations
```

## WHERE TO LOOK

| Task | Location |
|------|----------|
| Add new provider transformer | `transformer/` (copy existing as template) |
| Implement pipeline middleware | `pipeline/` |
| Modify unified data model | `model.go` |
| Auth abstractions | `auth/`, `oauth/` |
| Fake transformer for testing | `transformer/fake/` (uses `//go:embed testdata/*`) |
| Test fixtures | `**/testdata/` (JSON/JSONL) |

## CONVENTIONS

- **Go module**: Commands run from `llm/` directory, NOT repo root
- **Pipeline usage**: `pipeline.NewFactory(executor).Pipeline(inbound, outbound, opts...)`
- **Transformer interfaces**: `transformer.Inbound` (external→internal), `transformer.Outbound` (internal→external)
- **Fake transformers**: Embed testdata via `//go:embed testdata/*`, JSON/JSONL fixtures
- **Test count**: 132 test files, run via `cd llm && go test ./...`
- **Module replace**: Root module consumes via `replace github.com/looplj/axonhub/llm => ./llm`

## ANTI-PATTERNS

- Do NOT run `go test ./llm/...` from repo root (module boundary errors)
- Do NOT modify `model.go` without updating all affected transformers
- Do NOT add provider logic outside `transformer/` (pipeline is provider-agnostic)
