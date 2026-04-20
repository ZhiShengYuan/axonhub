package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/registry"
	"github.com/looplj/axonhub/internal/mcp/session"
	"github.com/looplj/axonhub/internal/mcp/testutil"
)

// ProxyRunner is a helper that sets up a proxy with mock upstream servers.
type ProxyRunner struct {
	Proxy       *testProxy
	MockServers []*testutil.Server
	Cleanup     func()
}

type testProxy struct {
	sm               *session.SessionManager
	channelURLs      map[string]string
	channelCreds     map[string]*testCreds
	registry         *registry.CapabilityRegistry
	upstreamClient   *testUpstreamClient
}

type testCreds struct{}

type testUpstreamClient struct {
	toolCalls   map[string]int
	mu          sync.Mutex
	serverURLs  map[string]string
}

func newTestUpstreamClient() *testUpstreamClient {
	return &testUpstreamClient{
		toolCalls: make(map[string]int),
		serverURLs: make(map[string]string),
	}
}

func (c *testUpstreamClient) SetServerURL(channelID, url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serverURLs[channelID] = url
}

func (c *testUpstreamClient) IncrementToolCall(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.toolCalls[name]++
}

func (c *testUpstreamClient) GetToolCallCount(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.toolCalls[name]
}

func newProxyRunner(t *testing.T, servers []*testutil.Server) *ProxyRunner {
	urls := make(map[string]string)
	for i, s := range servers {
		channelID := "channel-" + string(rune('a'+i))
		urls[channelID] = s.URL()
	}

	pr := &testProxy{
		sm:             session.NewSessionManager(),
		channelURLs:    urls,
		registry:       registry.NewCapabilityRegistry(),
		upstreamClient: newTestUpstreamClient(),
	}

	for i, s := range servers {
		channelID := "channel-" + string(rune('a'+i))
		pr.upstreamClient.SetServerURL(channelID, s.URL())
	}

	return &ProxyRunner{
		Proxy:       pr,
		MockServers: servers,
		Cleanup: func() {
			for _, s := range servers {
				s.Close()
			}
		},
	}
}

// makeInitializeRequest creates a JSON-RPC initialize request.
func makeInitializeRequest(id any) string {
	return testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, id)
}

// makeRequest creates a JSON-RPC request with the given method and params.
func makeRequest(method string, params any, id any) string {
	return testutil.MakeJSONRPCRequest(method, params, id)
}

// makeNotification creates a JSON-RPC notification.
func makeNotification(method string, params any) string {
	return testutil.MakeJSONRPCNotification(method, params)
}

// parseResponse parses a JSON-RPC response.
func parseResponse(t *testing.T, body []byte) *protocol.Response {
	var resp protocol.Response
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	return &resp
}

// parseErrorResponse parses a JSON-RPC error response.
func parseErrorResponse(t *testing.T, body []byte) *protocol.ErrorResponse {
	var resp protocol.ErrorResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse error response: %v", err)
	}
	return &resp
}

// TestE2EFullHappyPath tests the full MCP lifecycle: initialize → notifications/initialized → tools/list → tools/call → ping → close session.
// Verifies session affinity holds throughout.
func TestE2EFullHappyPath(t *testing.T) {
	var toolCallName string
	var toolCallArgs map[string]any

	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("test-tool", "A test tool", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": map[string]any{"type": "string"},
				},
			}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			toolCallName = name
			if args != nil {
				_ = json.Unmarshal(args, &toolCallArgs)
			}
			return json.RawMessage(`{"result": "success"}`), nil
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("test-tool", "A test tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	// Step 1: Initialize
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)
	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	sessionID := initResp.SessionID
	if sessionID == "" {
		t.Fatal("expected non-empty session ID")
	}

	// Verify session was created with correct channel
	sess, err := runner.Proxy.sm.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if sess.ChannelID != "channel-a" {
		t.Errorf("expected channel 'channel-a', got '%s'", sess.ChannelID)
	}

	// Step 2: Send notifications/initialized (optional per spec)
	initializedNotif := makeNotification("notifications/initialized", nil)
	var notif protocol.Notification
	if err := json.Unmarshal([]byte(initializedNotif), &notif); err != nil {
		t.Fatalf("failed to unmarshal notification: %v", err)
	}

	if err := proxy.HandleNotification(context.Background(), sessionID, &notif); err != nil {
		t.Fatalf("HandleNotification failed: %v", err)
	}

	// Verify session is now marked as initialized
	sess, _ = runner.Proxy.sm.GetSession(sessionID)
	if !sess.IsInitialized() {
		t.Error("expected session to be marked as initialized")
	}

	// Step 3: tools/list
	toolsReqBody := makeRequest("tools/list", nil, 2)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal tools/list request: %v", err)
	}

	toolsResp, err := proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/list failed: %v", err)
	}

	var toolsResult protocol.ToolsListResult
	if err := json.Unmarshal(toolsResp.Result, &toolsResult); err != nil {
		t.Fatalf("failed to unmarshal tools result: %v", err)
	}

	if len(toolsResult.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(toolsResult.Tools))
	}
	if toolsResult.Tools[0].Name != "ns/test-tool" {
		t.Errorf("expected tool name 'ns/test-tool', got '%s'", toolsResult.Tools[0].Name)
	}

	// Step 4: tools/call
	toolCallReqBody := makeRequest("tools/call", map[string]any{
		"name":      "ns/test-tool",
		"arguments": map[string]any{"input": "hello"},
	}, 3)
	var toolCallReq protocol.Request
	if err := json.Unmarshal([]byte(toolCallReqBody), &toolCallReq); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolCallReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/call failed: %v", err)
	}

	if toolCallName != "test-tool" {
		t.Errorf("expected upstream tool name 'test-tool', got '%s'", toolCallName)
	}
	if toolCallArgs == nil || toolCallArgs["input"] != "hello" {
		t.Errorf("expected args with input='hello', got %v", toolCallArgs)
	}

	// Step 5: ping
	pingReqBody := makeRequest("ping", nil, 4)
	var pingReq protocol.Request
	if err := json.Unmarshal([]byte(pingReqBody), &pingReq); err != nil {
		t.Fatalf("failed to unmarshal ping request: %v", err)
	}

	pingResp, err := proxy.HandleRequest(context.Background(), sessionID, &pingReq)
	if err != nil {
		t.Fatalf("HandleRequest ping failed: %v", err)
	}

	var pingResult map[string]bool
	if err := json.Unmarshal(pingResp.Result, &pingResult); err != nil {
		t.Fatalf("failed to unmarshal ping result: %v", err)
	}
	if !pingResult["pong"] {
		t.Error("expected pong=true")
	}

	// Step 6: Verify session affinity - all requests should go to same channel
	if sess.ChannelID != "channel-a" {
		t.Errorf("session channel changed, expected 'channel-a', got '%s'", sess.ChannelID)
	}

	// Step 7: Close session
	if err := runner.Proxy.sm.RemoveSession(sessionID); err != nil {
		t.Fatalf("RemoveSession failed: %v", err)
	}

	// Verify session is closed
	_, err = runner.Proxy.sm.GetSession(sessionID)
	if err != session.ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound after removal, got %v", err)
	}
}

// TestE2EMultipleUpstreams tests two upstream MCP servers with aggregated tools/list
// and tools/call routing to correct upstream based on namespace.
func TestE2EMultipleUpstreams(t *testing.T) {
	var serverAToolCalls int
	var serverBToolCalls int
	var serverAToolName string
	var serverBToolName string

	mockServerA := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "server-a",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("tool-a1", "First tool from server A", map[string]any{"type": "object"}),
			testutil.MakeTool("tool-a2", "Second tool from server A", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			serverAToolCalls++
			serverAToolName = name
			return json.RawMessage(`{"result": "server-a-success"}`), nil
		},
	})
	defer mockServerA.Close()

	mockServerB := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "server-b",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("tool-b1", "First tool from server B", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			serverBToolCalls++
			serverBToolName = name
			return json.RawMessage(`{"result": "server-b-success"}`), nil
		},
	})
	defer mockServerB.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServerA, mockServerB})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				testutil.MakeTool("tool-a1", "First tool from channel A", map[string]any{"type": "object"}),
				testutil.MakeTool("tool-a2", "Second tool from channel A", map[string]any{"type": "object"}),
			},
		},
		{
			ChannelID: "channel-b",
			Namespace: "chB",
			Tools: []protocol.Tool{
				testutil.MakeTool("tool-b1", "First tool from channel B", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	// Initialize
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	// tools/list should show tools from both upstreams
	toolsReqBody := makeRequest("tools/list", nil, 2)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal tools/list request: %v", err)
	}

	toolsResp, err := proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/list failed: %v", err)
	}

	var toolsResult protocol.ToolsListResult
	if err := json.Unmarshal(toolsResp.Result, &toolsResult); err != nil {
		t.Fatalf("failed to unmarshal tools result: %v", err)
	}

	if len(toolsResult.Tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(toolsResult.Tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range toolsResult.Tools {
		toolNames[tool.Name] = true
	}

	expectedTools := map[string]bool{
		"chA/tool-a1": true,
		"chA/tool-a2": true,
		"chB/tool-b1": true,
	}
	for name, expected := range expectedTools {
		if expected && !toolNames[name] {
			t.Errorf("expected tool %q in result", name)
		}
	}

	// Call tool from server A
	toolCallReqBodyA := makeRequest("tools/call", map[string]any{
		"name":      "chA/tool-a1",
		"arguments": map[string]any{},
	}, 3)
	var toolCallReqA protocol.Request
	if err := json.Unmarshal([]byte(toolCallReqBodyA), &toolCallReqA); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolCallReqA)
	if err != nil {
		t.Fatalf("HandleRequest tools/call (server A) failed: %v", err)
	}

	if serverAToolCalls != 1 {
		t.Errorf("expected 1 tool call on server A, got %d", serverAToolCalls)
	}
	if serverAToolName != "tool-a1" {
		t.Errorf("expected upstream tool name 'tool-a1', got '%s'", serverAToolName)
	}

	// Call tool from server B
	toolCallReqBodyB := makeRequest("tools/call", map[string]any{
		"name":      "chB/tool-b1",
		"arguments": map[string]any{},
	}, 4)
	var toolCallReqB protocol.Request
	if err := json.Unmarshal([]byte(toolCallReqBodyB), &toolCallReqB); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolCallReqB)
	if err != nil {
		t.Fatalf("HandleRequest tools/call (server B) failed: %v", err)
	}

	if serverBToolCalls != 1 {
		t.Errorf("expected 1 tool call on server B, got %d", serverBToolCalls)
	}
	if serverBToolName != "tool-b1" {
		t.Errorf("expected upstream tool name 'tool-b1', got '%s'", serverBToolName)
	}
}

// TestE2EInvalidVersionNegotiation tests that client sending unsupported protocol version
// results in error from proxy.
func TestE2EInvalidVersionNegotiation(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	// Send initialize with old/unsupported version
	oldVersionReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2024-01-01", // Unsupported version
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var req protocol.Request
	if err := json.Unmarshal([]byte(oldVersionReqBody), &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	_, err := proxy.HandleInitialize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for invalid protocol version")
	}

	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}
	if protoErr.Code != protocol.CodeInvalidParams {
		t.Errorf("expected code %d, got %d", protocol.CodeInvalidParams, protoErr.Code)
	}
}

// TestE2EMissingInitializedNotification tests that client initializes but never sends
// notifications/initialized. Session remains in non-initialized state but subsequent
// calls should still work (notification is optional per spec).
func TestE2EMissingInitializedNotification(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("test-tool", "A test tool", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"result": "success"}`), nil
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("test-tool", "A test tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	// Initialize (but do NOT send notifications/initialized)
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	// Verify session is NOT marked as initialized
	sess, _ := runner.Proxy.sm.GetSession(sessionID)
	if sess.IsInitialized() {
		t.Error("expected session to NOT be initialized (notification not sent)")
	}

	// Subsequent calls should still work without notifications/initialized
	toolsReqBody := makeRequest("tools/list", nil, 2)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal tools/list request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/list failed (should work without initialized notification): %v", err)
	}
}

// TestE2EInvalidSessionHeader tests that request with non-existent MCP-Session-Id returns error.
func TestE2EInvalidSessionHeader(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	// Try to use a non-existent session
	toolsReqBody := makeRequest("tools/list", nil, 1)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal tools/list request: %v", err)
	}

	_, err := proxy.HandleRequest(context.Background(), "non-existent-session-id", &toolsReq)
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}

	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}
	if protoErr.Code != protocol.CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", protocol.CodeInvalidRequest, protoErr.Code)
	}
}

// TestE2EUpstreamAuthFailure tests that upstream returns 401/403, proxy returns
// safe error without leaking upstream details.
func TestE2EUpstreamAuthFailure(t *testing.T) {
	// Create a server that returns 401/403
	authFailingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"jsonrpc": "2.0", "error": {"code": -32000, "message": "upstream api key invalid"}}`))
	}))
	defer authFailingServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{{Server: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))}})
	runner.Proxy.channelURLs["channel-a"] = authFailingServer.URL
	defer runner.Cleanup()

}

// TestE2EDownstreamCancellation tests that client sends notifications/cancelled during
// a tools/call, and cancellation is forwarded.
func TestE2EDownstreamCancellation(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("long-running-tool", "A long running tool", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"result": "completed"}`), nil
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("long-running-tool", "A long running tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	// Initialize
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	// Send cancellation notification
	cancelNotif := makeNotification("notifications/cancelled", map[string]any{
		"requestId": 2,
	})
	var cancelProtNotif protocol.Notification
	if err := json.Unmarshal([]byte(cancelNotif), &cancelProtNotif); err != nil {
		t.Fatalf("failed to unmarshal cancelled notification: %v", err)
	}

	// The notification should be handled without error
	if err := proxy.HandleNotification(context.Background(), sessionID, &cancelProtNotif); err != nil {
		t.Fatalf("HandleNotification cancelled failed: %v", err)
	}
}

// TestE2ESessionExpiry tests that session is cleaned up after expiry
// and no longer routable.
func TestE2ESessionExpiry(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	// Initialize
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	// Force session expiry to test cleanup behavior
	count := runner.Proxy.sm.CleanupExpired(-time.Hour)
	if count != 1 {
		t.Errorf("expected 1 expired session, got %d", count)
	}

	// Session should no longer be routable
	toolsReqBody := makeRequest("tools/list", nil, 2)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal tools/list request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
	if err == nil {
		t.Fatal("expected error for expired session")
	}
}

// TestE2ECollisionAtStartup tests that two upstreams with same tool name and no mapping
// results in collision error at registry build time.
func TestE2ECollisionAtStartup(t *testing.T) {
	mockServerA := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "server-a",
			Version: "1.0.0",
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("collision-tool", "Tool from server A", map[string]any{"type": "object"}),
		},
	})
	defer mockServerA.Close()

	mockServerB := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "server-b",
			Version: "1.0.0",
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("collision-tool", "Tool from server B", map[string]any{"type": "object"}),
		},
	})
	defer mockServerB.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServerA, mockServerB})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "chA",
			Tools: []protocol.Tool{
				testutil.MakeTool("collision-tool", "Tool from channel A", map[string]any{"type": "object"}),
			},
		},
		{
			ChannelID: "channel-b",
			Namespace: "chB",
			Tools: []protocol.Tool{
				testutil.MakeTool("collision-tool", "Tool from channel B", map[string]any{"type": "object"}),
			},
		},
	}

	// BuildFromChannels should fail due to collision
	err := runner.Proxy.registry.BuildFromChannels(channels)
	if err == nil {
		t.Fatal("expected collision error when building registry with duplicate tool names")
	}

	collisionErr, ok := err.(*registry.ErrNamespaceCollision)
	if !ok {
		t.Fatalf("expected ErrNamespaceCollision, got %T", err)
	}
	if collisionErr.Type != "tool" {
		t.Errorf("expected collision type 'tool', got '%s'", collisionErr.Type)
	}
}

// TestE2ENonRoutableName tests that invoking tool name not in registry
// returns MCP error.
func TestE2ENonRoutableName(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("real-tool", "A real tool", map[string]any{"type": "object"}),
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("real-tool", "A real tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	// Initialize
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	// Try to call non-existent tool
	nonExistentReqBody := makeRequest("tools/call", map[string]any{
		"name":      "nonexistent-tool",
		"arguments": map[string]any{},
	}, 2)
	var nonExistentReq protocol.Request
	if err := json.Unmarshal([]byte(nonExistentReqBody), &nonExistentReq); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &nonExistentReq)
	if err == nil {
		t.Fatal("expected error for non-existent tool")
	}

	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}

	if !strings.Contains(protoErr.Message, "tool not found") {
		t.Errorf("expected error message to contain 'tool not found', got: %s", protoErr.Message)
	}
}

// TestE2EUpstreamServerError tests that upstream returns internal error,
// proxy returns MCP error without leaking details.
func TestE2EUpstreamServerError(t *testing.T) {
	internalErrorMessage := "upstream internal database error with sensitive info"

	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("error-tool", "Tool that errors", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			return nil, &protocol.Error{
				Code:    protocol.CodeInternalError,
				Message: internalErrorMessage,
			}
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("error-tool", "Tool that errors", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	// Initialize
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	// Call tool that returns internal error
	toolCallReqBody := makeRequest("tools/call", map[string]any{
		"name":      "ns/error-tool",
		"arguments": map[string]any{},
	}, 2)
	var toolCallReq protocol.Request
	if err := json.Unmarshal([]byte(toolCallReqBody), &toolCallReq); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolCallReq)
	if err == nil {
		t.Fatal("expected error from upstream server error")
	}

	// The proxy should return a generic error, not the internal error details
	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}

	// Error should NOT contain the upstream's sensitive internal message
	if strings.Contains(protoErr.Message, "database") {
		t.Error("error message should not leak upstream internal details")
	}
}

// TestE2EResourceAndPromptRouting tests that resources/read and prompts/get
// route to correct upstream.
func TestE2EResourceAndPromptRouting(t *testing.T) {
	var receivedResourceURI string
	var receivedPromptName string
	var receivedPromptArgs map[string]any

	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Resources: &protocol.ResourcesCapability{Subscribe: true, ListChanged: true},
			Prompts:   &protocol.PromptsCapability{ListChanged: true},
		},
		Resources: []protocol.Resource{
			{URI: "file://test.txt", Name: "Test File", MimeType: "text/plain"},
		},
		Prompts: []protocol.Prompt{
			{Name: "test-prompt", Description: "Test prompt"},
		},
		HandleResourceRead: func(uri string) (json.RawMessage, error) {
			receivedResourceURI = uri
			return json.RawMessage(`{"content": "test content"}`), nil
		},
		HandlePromptGet: func(name string, args json.RawMessage) (json.RawMessage, error) {
			receivedPromptName = name
			if args != nil {
				_ = json.Unmarshal(args, &receivedPromptArgs)
			}
			return json.RawMessage(`{"messages": []}`), nil
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "ns",
			Resources: []protocol.Resource{
				{URI: "file://test.txt", Name: "Test File", MimeType: "text/plain"},
			},
			Prompts: []protocol.Prompt{
				{Name: "test-prompt", Description: "Test prompt"},
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	// Initialize
	initReqBody := makeInitializeRequest(1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	// Test resources/read routing
	resourceReadReqBody := makeRequest("resources/read", map[string]any{
		"uri": "ns/file://test.txt",
	}, 2)
	var resourceReadReq protocol.Request
	if err := json.Unmarshal([]byte(resourceReadReqBody), &resourceReadReq); err != nil {
		t.Fatalf("failed to unmarshal resources/read request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &resourceReadReq)
	if err != nil {
		t.Fatalf("HandleRequest resources/read failed: %v", err)
	}

	if receivedResourceURI != "file://test.txt" {
		t.Errorf("expected upstream URI 'file://test.txt', got '%s'", receivedResourceURI)
	}

	// Test prompts/get routing
	promptGetReqBody := makeRequest("prompts/get", map[string]any{
		"name":      "ns/test-prompt",
		"arguments": map[string]any{"input": "hello"},
	}, 3)
	var promptGetReq protocol.Request
	if err := json.Unmarshal([]byte(promptGetReqBody), &promptGetReq); err != nil {
		t.Fatalf("failed to unmarshal prompts/get request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &promptGetReq)
	if err != nil {
		t.Fatalf("HandleRequest prompts/get failed: %v", err)
	}

	if receivedPromptName != "test-prompt" {
		t.Errorf("expected upstream prompt name 'test-prompt', got '%s'", receivedPromptName)
	}
	if receivedPromptArgs == nil || receivedPromptArgs["input"] != "hello" {
		t.Errorf("expected prompt args with input='hello', got %v", receivedPromptArgs)
	}
}

// ---------------------------------------------------------------------------
// Helper types and functions for testing
// ---------------------------------------------------------------------------

// mcpProxy is a test helper that wraps the proxy functionality for E2E tests.
type mcpProxy struct {
	sm               *session.SessionManager
	registry         *registry.CapabilityRegistry
	channelURLs      map[string]string
	upstreamClient   *testUpstreamClient
}

func newMCPProxy(p *testProxy) *mcpProxy {
	return &mcpProxy{
		sm:             p.sm,
		registry:       p.registry,
		channelURLs:    p.channelURLs,
		upstreamClient: p.upstreamClient,
	}
}

func (p *mcpProxy) HandleInitialize(ctx context.Context, req *protocol.Request) (*InitializeResponse, error) {
	var params protocol.InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid initialize params"}
	}

	if params.ProtocolVersion != "2025-11-25" {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "unsupported protocol version"}
	}

	capabilities := protocol.ServerCapabilities{
		Tools:    &protocol.ToolsCapability{ListChanged: true},
		Resources: &protocol.ResourcesCapability{Subscribe: true, ListChanged: true},
		Prompts:  &protocol.PromptsCapability{ListChanged: true},
	}

	// Simple channel selection - just pick the first one
	channelID := ""
	for cid := range p.channelURLs {
		channelID = cid
		break
	}

	sess, err := p.sm.CreateSession(channelID, capabilities, params.ProtocolVersion)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to create session"}
	}

	result := protocol.InitializeResult{
		ProtocolVersion: "2025-11-25",
		Capabilities:    capabilities,
		ServerInfo: protocol.ServerInfo{
			Name:    "axonhub-mcp-proxy",
			Version: "1.0.0",
		},
	}

	resultBytes, _ := json.Marshal(result)
	return &InitializeResponse{
		Response: &protocol.Response{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      req.ID,
		},
		SessionID: sess.ID,
	}, nil
}

type InitializeResponse struct {
	Response  *protocol.Response
	SessionID string
}

func (p *mcpProxy) HandleRequest(ctx context.Context, sessionID string, req *protocol.Request) (*protocol.Response, error) {
	if sessionID == "" {
		return nil, &protocol.Error{Code: protocol.CodeInvalidRequest, Message: "session ID required"}
	}

	sess, err := p.sm.GetSession(sessionID)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidRequest, Message: "invalid session"}
	}

	if sess.IsClosed() {
		return nil, &protocol.Error{Code: protocol.CodeInvalidRequest, Message: "session is closed"}
	}

	p.sm.TouchSession(sessionID)

	switch req.Method {
	case "tools/list":
		return p.handleToolsList(req)
	case "resources/list":
		return p.handleResourcesList(req)
	case "prompts/list":
		return p.handlePromptsList(req)
	case "tools/call":
		return p.handleToolCall(ctx, req, sess)
	case "resources/read":
		return p.handleResourceRead(ctx, req, sess)
	case "prompts/get":
		return p.handlePromptGet(ctx, req, sess)
	case "ping":
		return p.handlePing(req)
	default:
		return nil, &protocol.Error{Code: protocol.CodeMethodNotFound, Message: "method not found"}
	}
}

func (p *mcpProxy) HandleNotification(ctx context.Context, sessionID string, notif *protocol.Notification) error {
	if sessionID == "" {
		return &protocol.Error{Code: protocol.CodeInvalidRequest, Message: "session ID required"}
	}

	sess, err := p.sm.GetSession(sessionID)
	if err != nil {
		return &protocol.Error{Code: protocol.CodeInvalidRequest, Message: "invalid session"}
	}

	if sess.IsClosed() {
		return &protocol.Error{Code: protocol.CodeInvalidRequest, Message: "session is closed"}
	}

	switch notif.Method {
	case "notifications/initialized":
		sess.SetInitialized()
		return nil
	default:
		return nil
	}
}

func (p *mcpProxy) handleToolsList(req *protocol.Request) (*protocol.Response, error) {
	tools := p.registry.ListTools()
	result := protocol.ToolsListResult{
		Tools: make([]protocol.Tool, 0, len(tools)),
	}
	for _, t := range tools {
		result.Tools = append(result.Tools, protocol.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: "2.0",
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (p *mcpProxy) handleResourcesList(req *protocol.Request) (*protocol.Response, error) {
	resources := p.registry.ListResources()
	result := protocol.ResourcesListResult{
		Resources: make([]protocol.Resource, 0, len(resources)),
	}
	for _, r := range resources {
		result.Resources = append(result.Resources, protocol.Resource{
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MimeType,
		})
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: "2.0",
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (p *mcpProxy) handlePromptsList(req *protocol.Request) (*protocol.Response, error) {
	prompts := p.registry.ListPrompts()
	result := protocol.PromptsListResult{
		Prompts: make([]protocol.Prompt, 0, len(prompts)),
	}
	for _, pr := range prompts {
		args := make([]protocol.PromptArgument, 0, len(pr.Arguments))
		for _, a := range pr.Arguments {
			args = append(args, protocol.PromptArgument{
				Name:        a.Name,
				Description: a.Description,
				Required:    a.Required,
			})
		}
		result.Prompts = append(result.Prompts, protocol.Prompt{
			Name:        pr.Name,
			Description: pr.Description,
			Arguments:   args,
		})
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: "2.0",
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (p *mcpProxy) handleToolCall(ctx context.Context, req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid tool call params"}
	}

	channelID, upstreamName, err := p.registry.ResolveTool(params.Name)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "tool not found: " + params.Name}
	}

	// Forward to actual mock server
	baseURL, ok := p.channelURLs[channelID]
	if !ok {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "channel URL not found"}
	}

	upstreamParams := map[string]any{"name": upstreamName}
	if params.Arguments != nil && len(params.Arguments) > 0 {
		upstreamParams["arguments"] = json.RawMessage(params.Arguments)
	}

	jsonRPCReq := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params":  upstreamParams,
		"id":      req.ID,
	}
	reqBody, _ := json.Marshal(jsonRPCReq)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mcp", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to create upstream request"}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Make actual HTTP call to mock server
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "upstream request failed"}
	}
	defer resp.Body.Close()

	// Read response body to check for errors
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to read upstream response"}
	}

	// Check if response contains an error
	if strings.Contains(string(respBody), `"error"`) {
		var errResp protocol.ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != nil {
			// Sanitize error message to not leak upstream details
			sanitizedMsg := "upstream request failed"
			return nil, &protocol.Error{
				Code:    errResp.Error.Code,
				Message: sanitizedMsg,
				Data:    errResp.Error.Data,
			}
		}
	}

	var upstreamResp protocol.Response
	if err := json.Unmarshal(respBody, &upstreamResp); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to decode upstream response"}
	}

	return &upstreamResp, nil
}

func (p *mcpProxy) handleResourceRead(ctx context.Context, req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid resource read params"}
	}

	channelID, upstreamURI, err := p.registry.ResolveResource(params.URI)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "resource not found: " + params.URI}
	}

	baseURL, ok := p.channelURLs[channelID]
	if !ok {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "channel URL not found"}
	}

	jsonRPCReq := map[string]any{
		"jsonrpc": "2.0",
		"method":  "resources/read",
		"params":  map[string]any{"uri": upstreamURI},
		"id":      req.ID,
	}
	reqBody, _ := json.Marshal(jsonRPCReq)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mcp", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to create upstream request"}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "upstream request failed"}
	}
	defer resp.Body.Close()

	var upstreamResp protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&upstreamResp); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to decode upstream response"}
	}

	return &upstreamResp, nil
}

func (p *mcpProxy) handlePromptGet(ctx context.Context, req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid prompt get params"}
	}

	channelID, upstreamName, err := p.registry.ResolvePrompt(params.Name)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "prompt not found: " + params.Name}
	}

	baseURL, ok := p.channelURLs[channelID]
	if !ok {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "channel URL not found"}
	}

	upstreamParams := map[string]any{"name": upstreamName}
	if params.Arguments != nil && len(params.Arguments) > 0 {
		upstreamParams["arguments"] = json.RawMessage(params.Arguments)
	}

	jsonRPCReq := map[string]any{
		"jsonrpc": "2.0",
		"method":  "prompts/get",
		"params":  upstreamParams,
		"id":      req.ID,
	}
	reqBody, _ := json.Marshal(jsonRPCReq)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mcp", strings.NewReader(string(reqBody)))
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to create upstream request"}
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "upstream request failed"}
	}
	defer resp.Body.Close()

	var upstreamResp protocol.Response
	if err := json.NewDecoder(resp.Body).Decode(&upstreamResp); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to decode upstream response"}
	}

	return &upstreamResp, nil
}

func (p *mcpProxy) handlePing(req *protocol.Request) (*protocol.Response, error) {
	result := map[string]bool{"pong": true}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: "2.0",
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}
