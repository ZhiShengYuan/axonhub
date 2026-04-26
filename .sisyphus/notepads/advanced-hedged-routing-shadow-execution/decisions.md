## Decisions

### 2025-04-25 Architecture
- Hedge coordinator lives in orchestrator layer, NOT in provider transformers
- System-level config only (no API key/profile scope for hedge settings)
- Streaming-only rollout; non-streaming routing unchanged
- Full loser-text harvesting is configuration-gated; hedge/shadow metrics always persisted
- CRITICAL invariant: no retry after client-visible bytes (StreamReleaseState gate preserved)
