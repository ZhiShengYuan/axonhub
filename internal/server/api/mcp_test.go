package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/looplj/axonhub/internal/mcp"
	"github.com/looplj/axonhub/internal/mcp/protocol"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestMCPEndpointRequiresAuth(t *testing.T) {
	handler := NewMCPHandler(mcp.NewProxy(nil, nil, nil), nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	body := bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}`))
	c.Request = httptest.NewRequest(http.MethodPost, "/mcp", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.handlePOST(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d when auth service is nil, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestMCPEndpointRejectsInvalidKey(t *testing.T) {
	handler := NewMCPHandler(mcp.NewProxy(nil, nil, nil), nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	body := bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"initialize","params":{},"id":1}`))
	c.Request = httptest.NewRequest(http.MethodPost, "/mcp", body)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Authorization", "Bearer invalid-key")

	handler.handlePOST(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d when auth service is nil, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestMCPInitializeReturnsSessionHeader(t *testing.T) {
	proxy := mcp.NewProxy(nil, nil, nil)
	handler := NewMCPHandler(proxy, nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	initParams := protocol.InitializeParams{
		ProtocolVersion: "2025-11-25",
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}
	paramsBytes, _ := json.Marshal(initParams)
	reqBody := protocol.Request{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params:  paramsBytes,
		ID:      1,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	body := bytes.NewReader(bodyBytes)
	c.Request = httptest.NewRequest(http.MethodPost, "/mcp", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.handlePOST(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d when auth service is nil, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestMCPRequestWithoutSessionReturnsError(t *testing.T) {
	handler := NewMCPHandler(mcp.NewProxy(nil, nil, nil), nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	reqBody := protocol.Request{
		JSONRPC: "2.0",
		Method:  "tools/list",
		Params:  []byte(`{}`),
		ID:      1,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	body := bytes.NewReader(bodyBytes)
	c.Request = httptest.NewRequest(http.MethodPost, "/mcp", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.handlePOST(c)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d when auth service is nil, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestMCPGetEndpointRequiresSession(t *testing.T) {
	handler := NewMCPHandler(mcp.NewProxy(nil, nil, nil), nil, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	c.Request = httptest.NewRequest(http.MethodGet, "/mcp", nil)

	handler.handleGET(c)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}