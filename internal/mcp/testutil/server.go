package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/looplj/axonhub/internal/mcp/protocol"
)

type MockMCPServer struct {
	Server *httptest.Server
	Config MockServerConfig
}

type MockServerConfig struct {
	ProtocolVersion string
	ServerInfo     protocol.ServerInfo
	Capabilities   protocol.ServerCapabilities
	Tools         []protocol.Tool
	Resources      []protocol.Resource
	Prompts       []protocol.Prompt
}

type ServerConfig struct {
	ProtocolVersion    string
	ServerInfo        protocol.ServerInfo
	Capabilities      protocol.ServerCapabilities
	Tools             []protocol.Tool
	Resources         []protocol.Resource
	Prompts           []protocol.Prompt
	HandleToolCall    func(name string, args json.RawMessage) (json.RawMessage, error)
	HandleResourceRead func(uri string) (json.RawMessage, error)
	HandlePromptGet   func(name string, args json.RawMessage) (json.RawMessage, error)
}

type Server struct {
	Server *httptest.Server
	Config ServerConfig
}

func NewMockMCPServer(cfg MockServerConfig) *MockMCPServer {
	if cfg.ProtocolVersion == "" {
		cfg.ProtocolVersion = "2025-11-25"
	}
	if cfg.ServerInfo.Name == "" {
		cfg.ServerInfo.Name = "mock-mcp-server"
		cfg.ServerInfo.Version = "1.0.0"
	}
	if cfg.Capabilities.Tools == nil {
		cfg.Capabilities.Tools = &protocol.ToolsCapability{ListChanged: true}
	}

	m := &MockMCPServer{Config: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", m.handleMCP)
	m.Server = httptest.NewServer(mux)
	return m
}

func NewServer(cfg ServerConfig) *Server {
	if cfg.ProtocolVersion == "" {
		cfg.ProtocolVersion = "2025-11-25"
	}
	if cfg.ServerInfo.Name == "" {
		cfg.ServerInfo.Name = "test-server"
		cfg.ServerInfo.Version = "1.0.0"
	}
	if cfg.Capabilities.Tools == nil {
		cfg.Capabilities.Tools = &protocol.ToolsCapability{ListChanged: true}
	}
	if cfg.Capabilities.Resources == nil {
		cfg.Capabilities.Resources = &protocol.ResourcesCapability{Subscribe: true, ListChanged: true}
	}
	if cfg.Capabilities.Prompts == nil {
		cfg.Capabilities.Prompts = &protocol.PromptsCapability{ListChanged: true}
	}

	s := &Server{Config: cfg}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	s.Server = httptest.NewServer(mux)
	return s
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePOST(w, r)
	case http.MethodGet:
		s.handleGET(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePOST(w http.ResponseWriter, r *http.Request) {
	var msg json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(msg, &req); err != nil {
		notif, parseErr := s.tryParseNotification(msg)
		if parseErr != nil {
			http.Error(w, "Invalid JSON-RPC", http.StatusBadRequest)
			return
		}
		s.handleNotification(w, notif)
		return
	}

	resp, err := s.handleRequest(r.Context(), &req)
	if err != nil {
		if pe, ok := err.(*protocol.Error); ok {
			s.sendError(w, req.ID, pe)
		} else {
			s.sendError(w, req.ID, &protocol.Error{Code: protocol.CodeInternalError, Message: err.Error()})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleGET(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
}

func (s *Server) handleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "tools/call":
		return s.handleToolCall(req)
	case "resources/read":
		return s.handleResourceRead(req)
	case "prompts/get":
		return s.handlePromptGet(req)
	default:
		return nil, &protocol.Error{Code: protocol.CodeMethodNotFound, Message: "Method not found"}
	}
}

func (s *Server) handleInitialize(req *protocol.Request) (*protocol.Response, error) {
	params := &protocol.InitializeParams{}
	if err := json.Unmarshal(req.Params, params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "Invalid params"}
	}

	result := protocol.InitializeResult{
		ProtocolVersion: s.Config.ProtocolVersion,
		Capabilities:   s.Config.Capabilities,
		ServerInfo:     s.Config.ServerInfo,
	}

	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *Server) handleToolsList(req *protocol.Request) (*protocol.Response, error) {
	result := protocol.ToolsListResult{
		Tools: s.Config.Tools,
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *Server) handleResourcesList(req *protocol.Request) (*protocol.Response, error) {
	result := protocol.ResourcesListResult{
		Resources: s.Config.Resources,
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *Server) handlePromptsList(req *protocol.Request) (*protocol.Response, error) {
	result := protocol.PromptsListResult{
		Prompts: s.Config.Prompts,
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *Server) handleToolCall(req *protocol.Request) (*protocol.Response, error) {
	if s.Config.HandleToolCall == nil {
		return nil, &protocol.Error{Code: protocol.CodeMethodNotFound, Message: "Method not found"}
	}

	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "Invalid params"}
	}

	result, err := s.Config.HandleToolCall(params.Name, params.Arguments)
	if err != nil {
		return nil, err
	}

	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  result,
		ID:      req.ID,
	}, nil
}

func (s *Server) handleResourceRead(req *protocol.Request) (*protocol.Response, error) {
	if s.Config.HandleResourceRead == nil {
		return nil, &protocol.Error{Code: protocol.CodeMethodNotFound, Message: "Method not found"}
	}

	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "Invalid params"}
	}

	result, err := s.Config.HandleResourceRead(params.URI)
	if err != nil {
		return nil, err
	}

	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  result,
		ID:      req.ID,
	}, nil
}

func (s *Server) handlePromptGet(req *protocol.Request) (*protocol.Response, error) {
	if s.Config.HandlePromptGet == nil {
		return nil, &protocol.Error{Code: protocol.CodeMethodNotFound, Message: "Method not found"}
	}

	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "Invalid params"}
	}

	result, err := s.Config.HandlePromptGet(params.Name, params.Arguments)
	if err != nil {
		return nil, err
	}

	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  result,
		ID:      req.ID,
	}, nil
}

func (s *Server) handleNotification(w http.ResponseWriter, notif *protocol.Notification) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) tryParseNotification(msg json.RawMessage) (*protocol.Notification, error) {
	notif := &protocol.Notification{}
	if err := json.Unmarshal(msg, notif); err != nil {
		return nil, err
	}
	if notif.JSONRPC != protocol.JSONRPCVersion || notif.Method == "" {
		return nil, fmt.Errorf("not a notification")
	}
	return notif, nil
}

func (s *Server) sendError(w http.ResponseWriter, id protocol.ID, err *protocol.Error) {
	resp := protocol.ErrorResponse{
		JSONRPC: protocol.JSONRPCVersion,
		Error:   err,
		ID:      id,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) URL() string {
	return s.Server.URL
}

func (s *Server) Close() {
	s.Server.Close()
}

func (s *MockMCPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handlePOST(w, r)
	case http.MethodGet:
		s.handleGET(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *MockMCPServer) handlePOST(w http.ResponseWriter, r *http.Request) {
	var msg json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(msg, &req); err != nil {
		notif, parseErr := s.tryParseNotification(msg)
		if parseErr != nil {
			http.Error(w, "Invalid JSON-RPC", http.StatusBadRequest)
			return
		}
		s.handleNotification(w, notif)
		return
	}

	resp, err := s.handleRequest(r.Context(), &req)
	if err != nil {
		if pe, ok := err.(*protocol.Error); ok {
			s.sendError(w, req.ID, pe)
		} else {
			s.sendError(w, req.ID, &protocol.Error{Code: protocol.CodeInternalError, Message: err.Error()})
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *MockMCPServer) handleGET(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	w.(http.Flusher).Flush()
}

func (s *MockMCPServer) handleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error) {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "prompts/list":
		return s.handlePromptsList(req)
	default:
		return nil, &protocol.Error{Code: protocol.CodeMethodNotFound, Message: "Method not found"}
	}
}

func (s *MockMCPServer) handleInitialize(req *protocol.Request) (*protocol.Response, error) {
	params := &protocol.InitializeParams{}
	if err := json.Unmarshal(req.Params, params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "Invalid params"}
	}

	result := protocol.InitializeResult{
		ProtocolVersion: s.Config.ProtocolVersion,
		Capabilities:   s.Config.Capabilities,
		ServerInfo:     s.Config.ServerInfo,
	}

	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *MockMCPServer) handleToolsList(req *protocol.Request) (*protocol.Response, error) {
	result := protocol.ToolsListResult{
		Tools: s.Config.Tools,
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *MockMCPServer) handleResourcesList(req *protocol.Request) (*protocol.Response, error) {
	result := protocol.ResourcesListResult{
		Resources: s.Config.Resources,
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *MockMCPServer) handlePromptsList(req *protocol.Request) (*protocol.Response, error) {
	result := protocol.PromptsListResult{
		Prompts: s.Config.Prompts,
	}
	resultBytes, _ := json.Marshal(result)
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (s *MockMCPServer) handleNotification(w http.ResponseWriter, notif *protocol.Notification) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *MockMCPServer) tryParseNotification(msg json.RawMessage) (*protocol.Notification, error) {
	notif := &protocol.Notification{}
	if err := json.Unmarshal(msg, notif); err != nil {
		return nil, err
	}
	if notif.JSONRPC != protocol.JSONRPCVersion || notif.Method == "" {
		return nil, fmt.Errorf("not a notification")
	}
	return notif, nil
}

func (s *MockMCPServer) sendError(w http.ResponseWriter, id protocol.ID, err *protocol.Error) {
	resp := protocol.ErrorResponse{
		JSONRPC: protocol.JSONRPCVersion,
		Error:   err,
		ID:      id,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *MockMCPServer) URL() string {
	return s.Server.URL
}

func (s *MockMCPServer) Close() {
	s.Server.Close()
}

func MakeTool(name, desc string, inputSchema map[string]any) protocol.Tool {
	schemaBytes, _ := json.Marshal(inputSchema)
	return protocol.Tool{
		Name:        name,
		Description: desc,
		InputSchema: schemaBytes,
	}
}

func MakeJSONRPCRequest(method string, params any, id any) string {
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      id,
	}
	if params != nil {
		req["params"] = params
	}
	buf, _ := json.Marshal(req)
	return string(buf)
}

func MakeJSONRPCNotification(method string, params any) string {
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		notif["params"] = params
	}
	buf, _ := json.Marshal(notif)
	return string(buf)
}

func ParseJSONRPCResponse(body []byte) (*protocol.Response, error) {
	if strings.Contains(string(body), `"error"`) {
		errResp := &protocol.ErrorResponse{}
		if err := json.Unmarshal(body, errResp); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("json-rpc error: %s", errResp.Error.Message)
	}
	resp := &protocol.Response{}
	if err := json.Unmarshal(body, resp); err != nil {
		return nil, err
	}
	return resp, nil
}
