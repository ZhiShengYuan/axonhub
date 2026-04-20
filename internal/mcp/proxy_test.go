package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/registry"
	"github.com/looplj/axonhub/internal/mcp/session"
	"github.com/looplj/axonhub/internal/mcp/testutil"
)

func TestInitializeLifecycle(t *testing.T) {
	proxy := NewProxy(nil, nil, nil)

	reqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)

	var req protocol.Request
	if err := json.Unmarshal([]byte(reqBody), &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &req)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	resp := initResp.Response
	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc '2.0', got '%s'", resp.JSONRPC)
	}

	var result protocol.InitializeResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result.ProtocolVersion != "2025-11-25" {
		t.Errorf("expected protocol version '2025-11-25', got '%s'", result.ProtocolVersion)
	}

	if result.ServerInfo.Name != "axonhub-mcp-proxy" {
		t.Errorf("expected server name 'axonhub-mcp-proxy', got '%s'", result.ServerInfo.Name)
	}

	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability to be set")
	}

	if initResp.SessionID == "" {
		t.Error("expected non-empty session ID")
	}
}

func TestInitializeRejectsInvalidVersion(t *testing.T) {
	proxy := NewProxy(map[string]string{"channel-1": "http://localhost:8080"}, nil, nil)

	reqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2024-01-01",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)

	var req protocol.Request
	if err := json.Unmarshal([]byte(reqBody), &req); err != nil {
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

func TestRequestWithoutSessionRejected(t *testing.T) {
	proxy := NewProxy(map[string]string{"channel-1": "http://localhost:8080"}, nil, nil)

	reqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 1)
	var req protocol.Request
	if err := json.Unmarshal([]byte(reqBody), &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	_, err := proxy.HandleRequest(context.Background(), "", &req)
	if err == nil {
		t.Fatal("expected error for missing session ID")
	}

	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}
	if protoErr.Code != protocol.CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", protocol.CodeInvalidRequest, protoErr.Code)
	}
}

func TestSessionAffinityOnSubsequentRequests(t *testing.T) {
	proxy := NewProxy(nil, nil, nil)

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
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	sessionID := initResp.SessionID

	toolsReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 2)
	var toolsReq protocol.Request
	if err := json.Unmarshal([]byte(toolsReqBody), &toolsReq); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolsReq)
	if err != nil {
		t.Fatalf("HandleRequest failed: %v", err)
	}
}

func TestAggregatedToolsList(t *testing.T) {
	mockServerA := testutil.NewMockMCPServer(testutil.MockServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "server-a",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("tool-a1", "First tool from server A", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"input": map[string]any{"type": "string"},
				},
			}),
			testutil.MakeTool("tool-a2", "Second tool from server A", map[string]any{
				"type": "object",
			}),
		},
	})
	defer mockServerA.Close()

	mockServerB := testutil.NewMockMCPServer(testutil.MockServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "server-b",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolsCapability{ListChanged: true},
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("tool-b1", "First tool from server B", map[string]any{
				"type": "object",
			}),
		},
	})
	defer mockServerB.Close()

	proxy := NewProxy(map[string]string{
		"channel-a": mockServerA.URL(),
		"channel-b": mockServerB.URL(),
	}, nil, nil)

	reg := proxy.registry
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
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

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
		t.Fatalf("HandleRequest failed: %v", err)
	}

	var toolsResult protocol.ToolsListResult
	if err := json.Unmarshal(toolsResp.Result, &toolsResult); err != nil {
		t.Fatalf("failed to unmarshal tools result: %v", err)
	}

	if len(toolsResult.Tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(toolsResult.Tools))
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
	for name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("expected tool %q in result", name)
		}
	}
}

func TestPingWithValidSession(t *testing.T) {
	proxy := NewProxy(nil, nil, nil)

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
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	initResp, err := proxy.HandleInitialize(context.Background(), &initReq)
	if err != nil {
		t.Fatalf("HandleInitialize failed: %v", err)
	}

	sessionID := initResp.SessionID

	pingReqBody := testutil.MakeJSONRPCRequest("ping", nil, 2)
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

	var result map[string]bool
	if err := json.Unmarshal(pingResp.Result, &result); err != nil {
		t.Fatalf("failed to unmarshal ping result: %v", err)
	}

	if !result["pong"] {
		t.Error("expected pong to be true")
	}
}

func TestPingWithInvalidSession(t *testing.T) {
	proxy := NewProxy(map[string]string{"channel-1": "http://localhost:8080"}, nil, nil)

	pingReqBody := testutil.MakeJSONRPCRequest("ping", nil, 1)
	var pingReq protocol.Request
	if err := json.Unmarshal([]byte(pingReqBody), &pingReq); err != nil {
		t.Fatalf("failed to unmarshal ping request: %v", err)
	}

	_, err := proxy.HandleRequest(context.Background(), "invalid-session", &pingReq)
	if err == nil {
		t.Fatal("expected error for invalid session")
	}

	protoErr, ok := err.(*protocol.Error)
	if !ok {
		t.Fatalf("expected protocol.Error, got %T", err)
	}
	if protoErr.Code != protocol.CodeInvalidRequest {
		t.Errorf("expected code %d, got %d", protocol.CodeInvalidRequest, protoErr.Code)
	}
}

func TestRoutedToolCall(t *testing.T) {
	var receivedName string
	var toolCallCount int

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
			testutil.MakeTool("original-tool", "Test tool", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			toolCallCount++
			receivedName = name
			return json.RawMessage(`{"result": "success"}`), nil
		},
	})
	defer mockServer.Close()

	proxy := NewProxy(map[string]string{
		"test-channel": mockServer.URL(),
	}, nil, nil)

	reg := proxy.registry
	channels := []registry.ChannelConfig{
		{
			ChannelID: "test-channel",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("original-tool", "Test tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

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
		"name": "ns/original-tool",
		"arguments": map[string]any{"input": "test"},
	}, 2)
	var toolCallReq protocol.Request
	if err := json.Unmarshal([]byte(toolCallReqBody), &toolCallReq); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &toolCallReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/call failed: %v", err)
	}

	if toolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", toolCallCount)
	}

	if receivedName != "original-tool" {
		t.Errorf("expected upstream tool name 'original-tool', got '%s'", receivedName)
	}
}

func TestRoutedResourceRead(t *testing.T) {
	var receivedURI string

	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Resources: &protocol.ResourcesCapability{Subscribe: true, ListChanged: true},
		},
		Resources: []protocol.Resource{
			{URI: "file://test.txt", Name: "Test File", MimeType: "text/plain"},
		},
		HandleResourceRead: func(uri string) (json.RawMessage, error) {
			receivedURI = uri
			return json.RawMessage(`{"content": "test content"}`), nil
		},
	})
	defer mockServer.Close()

	proxy := NewProxy(map[string]string{
		"test-channel": mockServer.URL(),
	}, nil, nil)

	reg := proxy.registry
	channels := []registry.ChannelConfig{
		{
			ChannelID: "test-channel",
			Namespace: "ns",
			Resources: []protocol.Resource{
				{URI: "file://test.txt", Name: "Test File", MimeType: "text/plain"},
			},
		},
	}
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

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

	resourceReadReqBody := testutil.MakeJSONRPCRequest("resources/read", map[string]any{
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

	if receivedURI != "file://test.txt" {
		t.Errorf("expected upstream URI 'file://test.txt', got '%s'", receivedURI)
	}
}

func TestRoutedPromptGet(t *testing.T) {
	var receivedName string

	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Capabilities: protocol.ServerCapabilities{
			Prompts: &protocol.PromptsCapability{ListChanged: true},
		},
		Prompts: []protocol.Prompt{
			{Name: "test-prompt", Description: "Test prompt"},
		},
		HandlePromptGet: func(name string, args json.RawMessage) (json.RawMessage, error) {
			receivedName = name
			return json.RawMessage(`{"messages": []}`), nil
		},
	})
	defer mockServer.Close()

	proxy := NewProxy(map[string]string{
		"test-channel": mockServer.URL(),
	}, nil, nil)

	reg := proxy.registry
	channels := []registry.ChannelConfig{
		{
			ChannelID: "test-channel",
			Namespace: "ns",
			Prompts: []protocol.Prompt{
				{Name: "test-prompt", Description: "Test prompt"},
			},
		},
	}
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

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

	promptGetReqBody := testutil.MakeJSONRPCRequest("prompts/get", map[string]any{
		"name": "ns/test-prompt",
	}, 2)
	var promptGetReq protocol.Request
	if err := json.Unmarshal([]byte(promptGetReqBody), &promptGetReq); err != nil {
		t.Fatalf("failed to unmarshal prompts/get request: %v", err)
	}

	_, err = proxy.HandleRequest(context.Background(), sessionID, &promptGetReq)
	if err != nil {
		t.Fatalf("HandleRequest prompts/get failed: %v", err)
	}

	if receivedName != "test-prompt" {
		t.Errorf("expected upstream prompt name 'test-prompt', got '%s'", receivedName)
	}
}

func TestNonRoutableNameRejected(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
	})
	defer mockServer.Close()

	proxy := NewProxy(map[string]string{
		"test-channel": mockServer.URL(),
	}, nil, nil)

	reg := proxy.registry
	channels := []registry.ChannelConfig{
		{
			ChannelID: "test-channel",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("real-tool", "Real tool", map[string]any{"type": "object"}),
			},
		},
	}
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

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

	nonExistentReqBody := testutil.MakeJSONRPCRequest("tools/call", map[string]any{
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

func TestOperationErrorNoLeak(t *testing.T) {
	mockServer := testutil.NewServer(testutil.ServerConfig{
		ProtocolVersion: "2025-11-25",
		ServerInfo: protocol.ServerInfo{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Tools: []protocol.Tool{
			testutil.MakeTool("secret-tool", "Tool with secrets", map[string]any{"type": "object"}),
		},
		HandleToolCall: func(name string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{
				"api_key": "super-secret-upstream-key",
				"result": "success",
				"upstreamBearerToken": "bearer-secret"
			}`), nil
		},
	})
	defer mockServer.Close()

	proxy := NewProxy(map[string]string{
		"test-channel": mockServer.URL(),
	}, nil, nil)

	reg := proxy.registry
	channels := []registry.ChannelConfig{
		{
			ChannelID: "test-channel",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("secret-tool", "Tool with secrets", map[string]any{"type": "object"}),
			},
		},
	}
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

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
		"name":      "ns/secret-tool",
		"arguments": map[string]any{},
	}, 2)
	var toolCallReq protocol.Request
	if err := json.Unmarshal([]byte(toolCallReqBody), &toolCallReq); err != nil {
		t.Fatalf("failed to unmarshal tools/call request: %v", err)
	}

	resp, err := proxy.HandleRequest(context.Background(), sessionID, &toolCallReq)
	if err != nil {
		t.Fatalf("HandleRequest tools/call failed: %v", err)
	}

	respStr := string(resp.Result)
	if strings.Contains(respStr, "super-secret-upstream-key") {
		t.Error("response should not contain upstream API key")
	}
	if strings.Contains(respStr, "bearer-secret") {
		t.Error("response should not contain upstream bearer token")
	}
}

func TestSessionManagement(t *testing.T) {
	sm := NewProxy(nil, nil, nil).sm

	sess, err := sm.CreateSession("channel-1", protocol.ServerCapabilities{}, protocol.SupportedProtocolVersion)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if sess.ID == "" {
		t.Error("expected non-empty session ID")
	}

	if sess.ChannelID != "channel-1" {
		t.Errorf("expected channel 'channel-1', got '%s'", sess.ChannelID)
	}

	retrieved, err := sm.GetSession(sess.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if retrieved.ID != sess.ID {
		t.Errorf("expected session ID '%s', got '%s'", sess.ID, retrieved.ID)
	}

	err = sm.RemoveSession(sess.ID)
	if err != nil {
		t.Fatalf("RemoveSession failed: %v", err)
	}

	_, err = sm.GetSession(sess.ID)
	if err != session.ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}

	count := sm.CleanupExpired(time.Hour)
	if count != 0 {
		t.Errorf("expected 0 expired, got %d", count)
	}
}

func TestNotificationRouting(t *testing.T) {
	proxy := NewProxy(nil, nil, nil)

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

	initializedNotif := testutil.MakeJSONRPCNotification("notifications/initialized", nil)
	var notif protocol.Notification
	if err := json.Unmarshal([]byte(initializedNotif), &notif); err != nil {
		t.Fatalf("failed to unmarshal notification: %v", err)
	}

	err = proxy.HandleNotification(context.Background(), sessionID, &notif)
	if err != nil {
		t.Fatalf("HandleNotification failed: %v", err)
	}

	sess, _ := proxy.sm.GetSession(sessionID)
	if !sess.IsInitialized() {
		t.Error("expected session to be initialized after notifications/initialized")
	}

	err = proxy.HandleNotification(context.Background(), "", &notif)
	if err == nil {
		t.Error("expected error for empty session ID")
	}

	err = proxy.HandleNotification(context.Background(), "invalid-session", &notif)
	if err == nil {
		t.Error("expected error for invalid session")
	}
}

func TestCapabilityRegistry(t *testing.T) {
	reg := registry.NewCapabilityRegistry()

	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-a",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("tool-a", "Tool A", map[string]any{"type": "object"}),
			},
			Resources: []protocol.Resource{
				{URI: "file://test.txt", Name: "Test File", MimeType: "text/plain"},
			},
			Prompts: []protocol.Prompt{
				{Name: "test-prompt", Description: "Test prompt"},
			},
		},
	}

	err := reg.BuildFromChannels(channels)
	if err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	tools := reg.ListTools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "ns/tool-a" {
		t.Errorf("expected tool name 'ns/tool-a', got '%s'", tools[0].Name)
	}

	resources := reg.ListResources()
	if len(resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].URI != "ns/file://test.txt" {
		t.Errorf("expected resource URI 'ns/file://test.txt', got '%s'", resources[0].URI)
	}

	prompts := reg.ListPrompts()
	if len(prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(prompts))
	}
	if prompts[0].Name != "ns/test-prompt" {
		t.Errorf("expected prompt name 'ns/test-prompt', got '%s'", prompts[0].Name)
	}

	channelID, upstreamName, err := reg.ResolveTool("ns/tool-a")
	if err != nil {
		t.Fatalf("ResolveTool failed: %v", err)
	}
	if channelID != "channel-a" {
		t.Errorf("expected channel 'channel-a', got '%s'", channelID)
	}
	if upstreamName != "tool-a" {
		t.Errorf("expected upstream name 'tool-a', got '%s'", upstreamName)
	}

	_, _, err = reg.ResolveTool("nonexistent")
	if err != registry.ErrToolNotFound {
		t.Errorf("expected ErrToolNotFound, got %v", err)
	}
}

func TestOperationRouter(t *testing.T) {
	proxy := NewProxy(map[string]string{"channel-1": "http://localhost:8080"}, nil, nil)

	reg := proxy.registry
	channels := []registry.ChannelConfig{
		{
			ChannelID: "channel-1",
			Namespace: "ns",
			Tools: []protocol.Tool{
				testutil.MakeTool("test-tool", "Test tool", map[string]any{"type": "object"}),
			},
			Resources: []protocol.Resource{
				{URI: "file://test.txt", Name: "Test File", MimeType: "text/plain"},
			},
			Prompts: []protocol.Prompt{
				{Name: "test-prompt", Description: "Test prompt"},
			},
		},
	}
	if err := reg.BuildFromChannels(channels); err != nil {
		t.Fatalf("BuildFromChannels failed: %v", err)
	}

	channelID, upstreamName, err := proxy.operationRouter.RouteToolCall("ns/test-tool")
	if err != nil {
		t.Fatalf("RouteToolCall failed: %v", err)
	}
	if channelID != "channel-1" {
		t.Errorf("expected channel 'channel-1', got '%s'", channelID)
	}
	if upstreamName != "test-tool" {
		t.Errorf("expected upstream name 'test-tool', got '%s'", upstreamName)
	}

	_, _, err = proxy.operationRouter.RouteToolCall("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent tool")
	}

	_, _, err = proxy.operationRouter.RouteResourceAccess("ns/file://test.txt")
	if err != nil {
		t.Fatalf("RouteResourceAccess failed: %v", err)
	}

	_, _, err = proxy.operationRouter.RouteResourceAccess("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent resource")
	}

	_, _, err = proxy.operationRouter.RoutePrompt("ns/test-prompt")
	if err != nil {
		t.Fatalf("RoutePrompt failed: %v", err)
	}

	_, _, err = proxy.operationRouter.RoutePrompt("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent prompt")
	}
}

func TestMockMCPServerInitialize(t *testing.T) {
	mockServer := testutil.NewMockMCPServer(testutil.MockServerConfig{
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

func TestMockMCPServerToolsList(t *testing.T) {
	mockServer := testutil.NewMockMCPServer(testutil.MockServerConfig{
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
	})
	defer mockServer.Close()

	initReqBody := testutil.MakeJSONRPCRequest("initialize", protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}, 1)

	initReq, err := http.NewRequest(http.MethodPost, mockServer.URL()+"/mcp", strings.NewReader(initReqBody))
	if err != nil {
		t.Fatalf("failed to create initialize request: %v", err)
	}
	initReq.Header.Set("Content-Type", "application/json")

	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		t.Fatalf("failed to send initialize request: %v", err)
	}
	defer initResp.Body.Close()

	if initResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", initResp.StatusCode)
	}

	toolsListReqBody := testutil.MakeJSONRPCRequest("tools/list", nil, 2)
	toolsListReq, err := http.NewRequest(http.MethodPost, mockServer.URL()+"/mcp", strings.NewReader(toolsListReqBody))
	if err != nil {
		t.Fatalf("failed to create tools/list request: %v", err)
	}
	toolsListReq.Header.Set("Content-Type", "application/json")

	toolsListResp, err := http.DefaultClient.Do(toolsListReq)
	if err != nil {
		t.Fatalf("failed to send tools/list request: %v", err)
	}
	defer toolsListResp.Body.Close()

	if toolsListResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", toolsListResp.StatusCode)
	}

	var jsonResp map[string]any
	if err := json.NewDecoder(toolsListResp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if jsonResp["jsonrpc"] != "2.0" {
		t.Error("expected jsonrpc 2.0")
	}

	result, ok := jsonResp["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result object")
	}

	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array")
	}

	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
}