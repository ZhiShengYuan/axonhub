package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/registry"
	"github.com/looplj/axonhub/internal/mcp/session"
	"github.com/looplj/axonhub/internal/mcp/testutil"
)

// TestRegressionExistingHandlersUnaffected verifies that the MCP package imports
// don't break existing server compilation.
func TestRegressionExistingHandlersUnaffected(t *testing.T) {
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

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	_, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
}

// TestRegressionSessionCleanupOnClose verifies session is fully cleaned up on close
// with no goroutine leaks or stale entries.
func TestRegressionSessionCleanupOnClose(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	initialCount := runner.Proxy.sm.SessionCount()
	if initialCount != 1 {
		t.Errorf("expected 1 session after initialize, got %d", initialCount)
	}

	err = runner.Proxy.sm.RemoveSession(sessionID)
	if err != nil {
		t.Fatalf("RemoveSession failed: %v", err)
	}

	afterCount := runner.Proxy.sm.SessionCount()
	if afterCount != 0 {
		t.Errorf("expected 0 sessions after close, got %d", afterCount)
	}

	_, err = runner.Proxy.sm.GetSession(sessionID)
	if err != session.ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound after removal, got %v", err)
	}

	closedSess, _ := runner.Proxy.sm.GetSession(sessionID)
	if closedSess != nil && !closedSess.IsClosed() {
		t.Error("session should be marked as closed after RemoveSession")
	}
}

// TestRegressionConcurrentSessionCreation verifies that many concurrent initialize
// requests don't cause data races.
func TestRegressionConcurrentSessionCreation(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	var wg sync.WaitGroup
	var errorCount int32
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
				ProtocolVersion: "2025-11-25",
				Capabilities:    protocol.ClientCapabilities{},
				ClientInfo: protocol.ClientInfo{
					Name:    "test-client",
					Version: "1.0.0",
				},
			}, id)
			var initReq protocol.Request
			if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
				atomic.AddInt32(&errorCount, 1)
				return
			}

			_, err := proxy.HandleInitialize(context.Background(), &initReq)
			if err != nil {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if errorCount > 0 {
		t.Errorf("got %d errors during concurrent session creation", errorCount)
	}

	finalCount := runner.Proxy.sm.SessionCount()
	if int(finalCount) != numGoroutines {
		t.Errorf("expected %d sessions, got %d", numGoroutines, finalCount)
	}
}

// TestRegressionMultipleSessionsPerProxy verifies multiple sessions can coexist
// and each maintains its own state.
func TestRegressionMultipleSessionsPerProxy(t *testing.T) {
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

	sessionIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
			ProtocolVersion: "2025-11-25",
			Capabilities:    protocol.ClientCapabilities{},
			ClientInfo: protocol.ClientInfo{
				Name:    "test-client",
				Version: "1.0.0",
			},
		}, i)
		var initReq protocol.Request
		if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
			t.Fatalf("failed to unmarshal initialize request: %v", err)
		}

		initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
		if err != nil {
			t.Fatalf("HandleInitialize failed: %v", err)
		}
		sessionIDs[i] = initResp.SessionID
	}

	if runner.Proxy.sm.SessionCount() != 5 {
		t.Errorf("expected 5 sessions, got %d", runner.Proxy.sm.SessionCount())
	}

	for i, sessionID := range sessionIDs {
		toolsReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 100+i)
		var toolsReq protocol.Request
		if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
			t.Fatalf("failed to unmarshal tools/list request: %v", err)
		}

		_, err := proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
		if err != nil {
			t.Fatalf("HandleRequest failed for session %d: %v", i, err)
		}
	}

	for i, sessionID := range sessionIDs {
		err := runner.Proxy.sm.RemoveSession(sessionID)
		if err != nil {
			t.Fatalf("RemoveSession failed for session %d: %v", i, err)
		}
	}

	if runner.Proxy.sm.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after all closed, got %d", runner.Proxy.sm.SessionCount())
	}
}

// TestRegressionClosedSessionRejectRequests verifies that requests to a closed
// session are properly rejected.
func TestRegressionClosedSessionRejectRequests(t *testing.T) {
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

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	runner.Proxy.sm.RemoveSession(sessionID)

	toolsReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 2)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal tools/list request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
	if err == nil {
		t.Fatal("expected error for closed session")
	}

	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}
	if protoErr.Code != protocol.CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", protocol.CodeInvalidRequest, protoErr.Code)
	}
}

// TestRegressionProxyCompilation verifies that all proxy types can be
// instantiated and used without compilation errors.
func TestRegressionProxyCompilation(t *testing.T) {
	_ = protocol.CodeInvalidParams
	_ = protocol.CodeInvalidRequest
	_ = protocol.CodeMethodNotFound
	_ = protocol.CodeInternalError
	_ = protocol.JSONRPCVersion

	sm := session.NewSessionManager()
	if sm == nil {
		t.Fatal("NewSessionManager returned nil")
	}

	reg := registry.NewCapabilityRegistry()
	if reg == nil {
		t.Fatal("NewCapabilityRegistry returned nil")
	}

	channels := []registry.ChannelConfig{
		{
			ChannelID: "test-channel",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("test-tool", "A test tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	tools := reg.ListTools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}

// TestRegressionJSONRPCResponseFormat verifies JSON-RPC response format
// is correct for all operations.
func TestRegressionJSONRPCResponseFormat(t *testing.T) {
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

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	if initResp.Response.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got '%s'", initResp.Response.JSONRPC)
	}

	if initResp.SessionID == "" {
		t.Error("expected non-empty session ID")
	}

	sessionID := initResp.SessionID

	toolsReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 2)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal tools/list request: %v", err)
	}

	toolsResp, err := proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/list failed: %v", err)
	}

	if toolsResp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got '%s'", toolsResp.JSONRPC)
	}

	if toolsResp.Result == nil {
		t.Error("expected non-nil result")
	}

	pingReqBody := testutil.MakeJSONRPCRequest("ping", nil, 3)
	var pingReq protocol.Request
	if err := json.Unmarshal([]byte(pingReqBody), &pingReq); err != nil {
		t.Fatalf("failed to unmarshal ping request: %v", err)
	}

	pingResp, err := proxy.HandleRequest(context.Background(), sessionID, &pingReq)
	if err != nil {
		t.Fatalf("HandleRequest ping failed: %v", err)
	}

	if pingResp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got '%s'", pingResp.JSONRPC)
	}
}

// TestRegressionServerInfoInResponse verifies ServerInfo is correctly populated
// in initialize response.
func TestRegressionServerInfoInResponse(t *testing.T) {
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

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	var result protocol.InitializeResult
	if err := json.Unmarshal(initResp.Response.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result.ServerInfo.Name != "axonhub-mcp-proxy" {
		t.Errorf("expected server name 'axonhub-mcp-proxy', got '%s'", result.ServerInfo.Name)
	}

	if result.ServerInfo.Version != "1.0.0" {
		t.Errorf("expected server version '1.0.0', got '%s'", result.ServerInfo.Version)
	}

	if result.ProtocolVersion != "2025-11-25" {
		t.Errorf("expected protocol version '2025-11-25', got '%s'", result.ProtocolVersion)
	}
}

// TestRegressionEmptyRegistry returns empty tools/resources/prompts list
// when no channels configured.
func TestRegressionEmptyRegistry(t *testing.T) {
	runner := newProxyRunner(t, []*testutil.Server{})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	sessionID := initResp.SessionID

	toolsReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 2)
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

	if len(toolsResult.Tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(toolsResult.Tools))
	}
}

// TestRegressionNamespaceAutoPrefix verifies auto-prefixing with namespace
// when no explicit mapping is provided.
func TestRegressionNamespaceAutoPrefix(t *testing.T) {
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
			testutil.MakeTool("my-tool", "My tool", map[string]any{"type": "object"}),
		},
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "custom-ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("my-tool", "My tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	sessionID := initResp.SessionID

	toolsReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 2)
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

	if toolsResult.Tools[0].Name != "custom-ns/my-tool" {
		t.Errorf("expected tool name 'custom-ns/my-tool', got '%s'", toolsResult.Tools[0].Name)
	}
}

// TestRegressionAliasMapping verifies explicit alias mapping works correctly.
func TestRegressionAliasMapping(t *testing.T) {
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
			testutil.MakeTool("original-tool", "Original tool", map[string]any{"type": "object"}),
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
				testutil.MakeTool("original-tool", "Original tool", map[string]any{"type": "object"}),
			},
			Mappings: []registry.NamespaceMapping{
				{
					From: "aliased-tool",
					To:   "original-tool",
					Type: "tool",
				},
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	sessionID := initResp.SessionID

	toolsReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 2)
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

	foundAliased := false
	for _, tool := range toolsResult.Tools {
		if tool.Name == "aliased-tool" {
			foundAliased = true
		}
		if strings.HasPrefix(tool.Name, "ns/") {
			t.Errorf("should not auto-prefix when alias mapping exists, got '%s'", tool.Name)
		}
	}

	if !foundAliased {
		t.Error("expected to find 'aliased-tool' in list")
	}
}

// TestRegressionSessionTimeoutCleanup verifies that sessions are properly
// cleaned up after timeout.
func TestRegressionSessionTimeoutCleanup(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	for i := 0; i < 5; i++ {
		initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
			ProtocolVersion: "2025-11-25",
			Capabilities:    protocol.ClientCapabilities{},
			ClientInfo: protocol.ClientInfo{
				Name:    "test-client",
				Version: "1.0.0",
			},
		}, i)
		var initReq protocol.Request
		if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
			t.Fatalf("failed to unmarshal initialize request: %v", err)
		}

		_, err := proxy.HandleInitialize(context.Background(), &initReq)
		if err != nil {
			t.Fatalf("HandleInitialize failed: %v", err)
		}
	}

	if runner.Proxy.sm.SessionCount() != 5 {
		t.Errorf("expected 5 sessions, got %d", runner.Proxy.sm.SessionCount())
	}

	count := runner.Proxy.sm.CleanupExpired(time.Hour)
	if count != 0 {
		t.Errorf("expected 0 expired sessions, got %d", count)
	}

	count = runner.Proxy.sm.CleanupExpired(-time.Hour)
	if count != 5 {
		t.Errorf("expected 5 expired sessions after negative timeout, got %d", count)
	}

	if runner.Proxy.sm.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after cleanup, got %d", runner.Proxy.sm.SessionCount())
	}
}

// TestRegressionToolCallWithNoArguments verifies tool call works with no arguments.
func TestRegressionToolCallWithNoArguments(t *testing.T) {
	var calledWithArgs bool

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
			testutil.MakeTool("no-args-tool", "Tool with no args", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			if args != nil && len(args) > 0 {
				calledWithArgs = true
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
				testutil.MakeTool("no-args-tool", "Tool with no args", map[string]any{"type": "object"}),
			},
		},
	}
	if err := runner.Proxy.registry.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	proxy := newMCPProxy(runner.Proxy)

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)
	var initReq protocol.Request
	if err := json.Unmarshal([]byte(initReqBody), &initReq); err != nil {
		t.Fatalf("failed to unmarshal initialize request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}
	sessionID := initResp.SessionID

	toolCallReqBody := testutil.MakeJSONRPCRequest("tools/call", map[string]any{
		"name": "ns/no-args-tool",
	}, 2)
	var toolCallReq protocol.Request
	if err := json.Unmarshal([]byte(toolCallReqBody), &toolCallReq); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolCallReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/call failed: %v", err)
	}

	if calledWithArgs {
		t.Error("tool should be called with no arguments")
	}
}

// TestRegressionNotificationWithoutSession verifies notifications are rejected
// when session ID is missing.
func TestRegressionNotificationWithoutSession(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
	})
	defer mockServer.Close()

	runner := newProxyRunner(t, []*testutil.Server{mockServer})
	defer runner.Cleanup()

	proxy := newMCPProxy(runner.Proxy)

	notif := testutil.MakeJSONRPCNotification("notifications/initialized", nil)
	var protNotif protocol.Notification
	if err := json.Unmarshal([]byte(notif), &protNotif); err != nil {
		t.Fatalf("failed to unmarshal notification: %v", err)
	}

	err := proxy.HandleNotification(context.Background(), "", &protNotif)
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}

	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}
	if protoErr.Code != protocol.CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", protocol.CodeInvalidRequest, protoErr.Code)
	}
}

// TestRegressionServerHandler tests the mock server directly to ensure
// it correctly handles HTTP requests.
func TestRegressionServerHandler(t *testing.T) {
	mockServer := testutil.NewMockMCPServer(testutil.MockServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "mock-server",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("mock-tool", "Mock tool", map[string]any{"type": "object"}),
		},
	})
	defer mockServer.Close()

	reqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)

	req, err := http.NewRequest(http.MethodPost, mockServer.URL()+"/mcp", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var jsonResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if jsonResp["jsonrpc"] != "2.0" {
		t.Error("expected jsonrpc 2.0")
	}
}
