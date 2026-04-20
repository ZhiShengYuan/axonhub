package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/objects"
)

func TestProbeUpstreamHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req protocol.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		if req.Method != "initialize" {
			http.Error(w, "Expected initialize", http.StatusBadRequest)
			return
		}

		result := protocol.InitializeResult{
			ProtocolVersion: "2025-11-25",
			Capabilities: protocol.ServerCapabilities{
				Tools:    &protocol.ToolsCapability{ListChanged: true},
				Resources: &protocol.ResourcesCapability{Subscribe: true, ListChanged: true},
				Prompts:  &protocol.PromptsCapability{ListChanged: true},
			},
			ServerInfo: protocol.ServerInfo{
				Name:    "test-mcp-server",
				Version: "1.0.0",
			},
		}

		resp := protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      req.ID,
		}
		resp.Result, _ = json.Marshal(result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	result, err := ProbeUpstream(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Healthy {
		t.Fatalf("expected healthy, got unhealthy: %s", result.Error)
	}

	if result.ProtocolVersion != "2025-11-25" {
		t.Fatalf("expected protocol version 2025-11-25, got %s", result.ProtocolVersion)
	}

	if result.Latency <= 0 {
		t.Fatalf("expected positive latency, got %v", result.Latency)
	}

	if result.Capabilities.Tools == nil {
		t.Fatalf("expected tools capability")
	}
}

func TestProbeUpstreamUnreachable(t *testing.T) {
	result, err := ProbeUpstream(context.Background(), "http://localhost:9999", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Healthy {
		t.Fatalf("expected unhealthy for unreachable server")
	}

	if result.Error == "" {
		t.Fatalf("expected error message for unreachable server")
	}
}

func TestProbeUpstreamInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc": "2.0", "error": {"code": -32603, "message": "Internal error"}}`))
	}))
	defer server.Close()

	result, err := ProbeUpstream(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Healthy {
		t.Fatalf("expected unhealthy for invalid response")
	}

	if result.Error == "" {
		t.Fatalf("expected error message for invalid response")
	}
}

func TestProbeUpstreamTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      1,
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := ProbeUpstream(ctx, server.URL, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Healthy {
		t.Fatalf("expected unhealthy for timed out server")
	}
}

func TestProbeUpstreamWithCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		result := protocol.InitializeResult{
			ProtocolVersion: "2025-11-25",
			Capabilities:    protocol.ServerCapabilities{},
			ServerInfo: protocol.ServerInfo{
				Name:    "test-mcp-server",
				Version: "1.0.0",
			},
		}

		resp := protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      1,
		}
		resp.Result, _ = json.Marshal(result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	creds := &objects.MCPCredentials{
		UpstreamAPIKey: "test-api-key",
	}

	result, err := ProbeUpstream(context.Background(), server.URL, creds)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Healthy {
		t.Fatalf("expected healthy with valid credentials: %s", result.Error)
	}
}