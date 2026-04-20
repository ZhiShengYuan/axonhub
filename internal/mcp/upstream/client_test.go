package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/objects"
)

func TestUpstreamClientSendRequest(t *testing.T) {
	var receivedAuth, receivedSessionID string
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedSessionID = r.Header.Get("MCP-Session-Id")
		receivedMethod = r.URL.Path

		var req protocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		result := map[string]any{"status": "ok"}
		resultBytes, _ := json.Marshal(result)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol.Response{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      req.ID,
		})
	}))
	defer server.Close()

	client := NewUpstreamClient()

	req := &protocol.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		Params:  nil,
		ID:      1,
	}

	resp, err := client.SendRequest(context.Background(), server.URL, req, "test-session", nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected JSONRPC '2.0', got '%s'", resp.JSONRPC)
	}

	if receivedSessionID != "test-session" {
		t.Errorf("expected session ID 'test-session', got '%s'", receivedSessionID)
	}

	if receivedMethod != "/mcp" {
		t.Errorf("expected path '/mcp', got '%s'", receivedMethod)
	}

	_ = receivedAuth
}

func TestUpstreamClientAuthInjection(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		result := map[string]any{"status": "ok"}
		resultBytes, _ := json.Marshal(result)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol.Response{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      1,
		})
	}))
	defer server.Close()

	client := NewUpstreamClient()

	creds := &objects.MCPCredentials{
		UpstreamAPIKey:      "test-api-key",
		UpstreamBearerToken: "",
	}

	req := &protocol.Request{
		JSONRPC: "2.0",
		Method:  "ping",
		ID:      1,
	}

	_, err := client.SendRequest(context.Background(), server.URL, req, "", creds)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	if receivedAuth != "Bearer test-api-key" {
		t.Errorf("expected auth 'Bearer test-api-key', got '%s'", receivedAuth)
	}
}

func TestUpstreamClientSessionHeader(t *testing.T) {
	var receivedSessionID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSessionID = r.Header.Get("MCP-Session-Id")

		result := map[string]any{"status": "ok"}
		resultBytes, _ := json.Marshal(result)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol.Response{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      1,
		})
	}))
	defer server.Close()

	client := NewUpstreamClient()

	req := &protocol.Request{
		JSONRPC: "2.0",
		Method:  "ping",
		ID:      1,
	}

	_, err := client.SendRequest(context.Background(), server.URL, req, "upstream-session-123", nil)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	if receivedSessionID != "upstream-session-123" {
		t.Errorf("expected MCP-Session-Id 'upstream-session-123', got '%s'", receivedSessionID)
	}
}

func TestUpstreamClientBearerTokenInjection(t *testing.T) {
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		result := map[string]any{"status": "ok"}
		resultBytes, _ := json.Marshal(result)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol.Response{
			JSONRPC: "2.0",
			Result:  resultBytes,
			ID:      1,
		})
	}))
	defer server.Close()

	client := NewUpstreamClient()

	creds := &objects.MCPCredentials{
		UpstreamAPIKey:      "",
		UpstreamBearerToken: "bearer-token-xyz",
	}

	req := &protocol.Request{
		JSONRPC: "2.0",
		Method:  "ping",
		ID:      1,
	}

	_, err := client.SendRequest(context.Background(), server.URL, req, "", creds)
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	if receivedAuth != "Bearer bearer-token-xyz" {
		t.Errorf("expected auth 'Bearer bearer-token-xyz', got '%s'", receivedAuth)
	}
}

func TestUpstreamClientErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol.ErrorResponse{
			JSONRPC: "2.0",
			Error: &protocol.Error{
				Code:    -32601,
				Message: "Method not found",
			},
			ID: 1,
		})
	}))
	defer server.Close()

	client := NewUpstreamClient()

	req := &protocol.Request{
		JSONRPC: "2.0",
		Method:  "unknown_method",
		ID:      1,
	}

	_, err := client.SendRequest(context.Background(), server.URL, req, "", nil)
	if err == nil {
		t.Fatal("expected error for error response")
	}

	if !strings.Contains(err.Error(), "Method not found") {
		t.Errorf("expected error message to contain 'Method not found', got: %v", err)
	}
}

func TestUpstreamClientNotification(t *testing.T) {
	var receivedNotification bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var notif protocol.Notification
		if err := json.NewDecoder(r.Body).Decode(&notif); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if notif.Method == "notifications/cancelled" {
			receivedNotification = true
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewUpstreamClient()

	notif := &protocol.Notification{
		JSONRPC: "2.0",
		Method:  "notifications/cancelled",
		Params:  json.RawMessage(`{"requestId": "123"}`),
	}

	err := client.SendNotification(context.Background(), server.URL, notif, "session-abc", nil)
	if err != nil {
		t.Fatalf("SendNotification failed: %v", err)
	}

	if !receivedNotification {
		t.Error("expected to receive notifications/cancelled notification")
	}
}