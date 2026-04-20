// Package mcp implements the Model Context Protocol (MCP) proxy subsystem for AxonHub.
//
// MCP is a bidirectional communication protocol that enables AI clients to interact
// with servers providing tools, resources, and prompts. AxonHub acts as an MCP proxy,
// aggregating capabilities from multiple upstream MCP servers into a unified endpoint.
//
// Architecture Overview:
//
//	MCP Proxy (AxonHub)
//	┌─────────────────────────────────────────────────────────────┐
//	│                     internal/mcp/                           │
//	├─────────────────────────────────────────────────────────────┤
//	│  protocol/     - JSON-RPC 2.0 message types                 │
//	│  transport/    - Streamable HTTP transport interfaces       │
//	│  session/      - Session management (ID, capabilities)      │
//	│  registry/     - Capability registry (tools, resources)     │
//	│  router/       - Operation routing to upstreams            │
//	│  health/       - Health probes for upstream reachability   │
//	│  metrics/      - MCP metrics (sessions, invocations, etc)  │
//	│  testutil/     - Mock MCP server for testing              │
//	└─────────────────────────────────────────────────────────────┘
//	         │                              │
//	         ▼                              ▼
//	┌─────────────────┐           ┌─────────────────────┐
//	│  Downstream     │           │  Upstream MCP       │
//	│  (MCP Clients)  │◄─────────►│  Servers           │
//	└─────────────────┘           └─────────────────────┘
//
// Key Design Decisions:
//
//  1. Streamable HTTP Only: No stdio support, no legacy HTTP+SSE.
//     Streamable HTTP is the current MCP standard (2025-03-26+).
//
//  2. Dedicated Subsystem: MCP sessions are fundamentally incompatible with
//     the stateless llm/pipeline request-scoped model. A separate subsystem
//     under internal/mcp/ maintains session state.
//
//  3. Session Affinity: Once initialized, an MCP session binds to one upstream
//     channel set for its lifetime. No mid-session failover.
//
//  4. Collision Policy: Configured alias wins > auto-prefix by channel namespace
//     > reject at config/build time.
//
//  5. Auth Model: AxonHub virtual API keys (ah- prefix) for downstream auth;
//     upstream secrets injected from channel credentials.
//
// Observability:
//
//  MCP observability is built around three pillars:
//
//  - health/: Upstream reachability validation via MCP initialize handshake.
//    ProbeUpstream sends a real MCP initialize request to validate protocol
//    compliance, not just TCP connectivity. Returns latency, protocol version,
//    and server capabilities.
//
//  - metrics/: Thread-safe in-memory counters using sync/atomic. Tracks:
//    * ActiveSessions - current session count
//    * TotalInitializations - initialization count with latency
//    * TotalInvocations - method invocation count (tools/call, resources/read, prompts/get)
//    * TotalErrors - error count with method attribution
//    * DiscoverySize - aggregated tools + resources + prompts count
//
//  Tracing correlation uses session ID + channel ID + JSON-RPC request ID
//  to correlate operations across the proxy. Session-bound operations never
//  cross-channel retry after bind, ensuring trace continuity.
//
// Protocol Reference:
//
//	Initialize:    client→server {jsonrpc:"2.0", method:"initialize", params:{...}, id:1}
//	Server Reply:  server→client {jsonrpc:"2.0", result:{protocolVersion, capabilities, serverInfo}, id:1}
//	Initialized:   client→server {jsonrpc:"2.0", method:"notifications/initialized"}
//	tools/list:    {jsonrpc:"2.0", method:"tools/list", params:{}, id:2} → tools array
//
// Streamable HTTP Details:
//
//	POST /mcp - Client→Server requests
//	GET  /mcp - Server→Client notifications (SSE)
//
//	Headers:
//	  Accept: application/json, text/event-stream
//	  MCP-Protocol-Version: <version> (after init)
//	  MCP-Session-Id: <id> (for session routing)
package mcp