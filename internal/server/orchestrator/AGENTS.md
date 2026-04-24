# Orchestrator Package

## OVERVIEW
LLM request orchestration with load balancing, candidate selection, token budgeting, and response transformation.

## CONVENTIONS
- `LoadBalanceStrategy` implementations MUST implement `Score()` and `ScoreWithDebug()`
- Candidate selection uses decorator pattern; wrap with `WithAnthropicNativeToolsSelector(wrapped)`
- `PersistentInboundTransformer` injects `PersistenceState` into transformer chain
- Association cache: 5-min TTL, invalidated when channel/model `update_time` changes
- Token estimation: CJK chars /1.5, other chars /4.0, images/audio +128 tokens per unit
- Weighted strategy composition in `lb_strategy_composite.go`
- Use `partial` package for top-k partial sorting (not full sort)

## ANTI-PATTERNS
- Do not bypass decorator chain for candidate selection
- Do not cache associations without tracking `update_time` for invalidation
- Do not use full sort where partial sort suffices
- Do not inject transformer state outside `PersistentInboundTransformer`

## WHERE TO LOOK
| Task | File | Notes |
|------|------|-------|
| Main orchestration flow | orchestrator.go | entry point, request routing |
| Request overrides | override.go | override logic |
| Load balancing | load_balancer.go | candidate scoring/selection |
| Candidate management | candidates.go | token budgeting, filtering |
| Inbound/outbound transform | transformer.go | PersistentInboundTransformer |
| Response handling | outbound.go | final response processing |
| Condition evaluation | candidates_condition.go | token estimation in conditions |
| Weighted strategies | lb_strategy_composite.go | strategy composition |
| Partial sorting | partial/ | top-k sorting utilities |
