# API Handlers

## OVERVIEW
REST handlers wrapping the LLM orchestrator pipeline for provider-specific API formats.

## CONVENTIONS

- Request parsing → inbound transformer construction → `orchestrator.Process()` → response writing
- Streaming: use SSE helpers, set `X-Accel-Buffering: no` header
- Error responses follow provider-specific formats (OpenAI vs Anthropic vs Gemini)
- Handler functions are named `handle<Provider><Operation>` (e.g., `handleOpenAIChat`)
- Context values extracted via `contextx` helpers, not raw `c.Request().Context()`
- Panic recovery wraps handler entry points

## ANTI-PATTERNS

- Do NOT call `c.JSON()` after streaming has started
- Do NOT block in handlers; offload to goroutines only for background responses
- Do NOT parse request bodies more than once; use middleware for body caching if needed
- Do NOT ignore `c.Request().Close` for connection cleanup

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| OpenAI-compatible endpoints | `openai.go` | `/v1/chat/completions`, `/v1/embeddings`, `/v1/images/generations` |
| Anthropic endpoints | `anthropic.go` | `/v1/messages`, streaming support |
| Gemini endpoints | `gemini.go` | `/v1beta/models/...` format |
| Image generation | `image_generation.go` | DALL-E, Imagen, etc. |
| Embeddings | `embedding.go` | Text and code embeddings |
| Re-ranking | `rerank.go` | Cohere-compatible rerank API |
| Provider handlers | `aisdk.go`, `doubao.go`, `codex.go`, `claudecode.go`, `antigravity.go`, `copilot.go`, `jina.go` | Each wraps provider-specific quirks |
| Dependency injection | `fx_module.go` | Provides all handlers via FX |
