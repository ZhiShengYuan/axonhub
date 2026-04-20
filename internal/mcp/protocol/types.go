// Package protocol provides JSON-RPC 2.0 message types for the Model Context Protocol.
// MCP uses JSON-RPC 2.0 as its wire protocol, with specific message shapes for
// requests, responses, notifications, and errors.
package protocol

import "encoding/json"

// JSONRPCVersion is the required JSON-RPC 2.0 version string.
const JSONRPCVersion = "2.0"

// SupportedProtocolVersion is the MCP protocol version supported by this implementation.
const SupportedProtocolVersion = "2025-11-25"

// ID represents a JSON-RPC 2.0 request/response identifier.
// Per spec, it can be a string, number, or null (null not used in practice).
type ID interface{}

// IntID is a numeric JSON-RPC ID.
type IntID int64

// StringID is a string JSON-RPC ID.
type StringID string

// Request is a JSON-RPC 2.0 request message.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      ID              `json:"id"`
}

// Response is a JSON-RPC 2.0 success response message.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result json.RawMessage `json:"result,omitempty"`
	ID     ID              `json:"id"`
}

// ErrorResponse is a JSON-RPC 2.0 error response message.
type ErrorResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Error  *Error      `json:"error,omitempty"`
	ID     ID          `json:"id"`
}

// Error is a JSON-RPC 2.0 error object.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string {
	return e.Message
}

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError       = -32700
	CodeInvalidRequest   = -32600
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternalError    = -32603
	CodeServerError      = -32000 // Reserved for implementation-defined server errors
)

// Notification is a JSON-RPC 2.0 notification message (no response expected).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// InitializeParams is the params for the "initialize" method.
type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo     `json:"clientInfo"`
}

// ClientCapabilities describes capabilities of an MCP client.
type ClientCapabilities struct {
	Roots    RootsCapability    `json:"roots,omitempty"`
	Sampling SamplingCapability `json:"sampling,omitempty"`
}

// RootsCapability describes client roots support.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability describes client sampling support.
type SamplingCapability struct{}

// ClientInfo describes the client application.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the result of the "initialize" method.
type InitializeResult struct {
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    ServerCapabilities     `json:"capabilities"`
	ServerInfo      ServerInfo            `json:"serverInfo"`
}

// ServerCapabilities describes capabilities of an MCP server.
type ServerCapabilities struct {
	Tools    *ToolsCapability    `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts  *PromptsCapability   `json:"prompts,omitempty"`
}

// ToolsCapability describes tool capabilities.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability describes resource capabilities.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability describes prompt capabilities.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ServerInfo describes the server application.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolsListParams is the params for the "tools/list" method.
type ToolsListParams struct{}

// ToolsListResult is the result of the "tools/list" method.
type ToolsListResult struct {
	Tools []Tool `json:"tools"`
}

// Tool describes an MCP tool.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ResourcesListParams is the params for the "resources/list" method.
type ResourcesListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// ResourcesListResult is the result of the "resources/list" method.
type ResourcesListResult struct {
	Resources []Resource `json:"resources"`
	Cursor    string    `json:"cursor,omitempty"`
}

// Resource describes an MCP resource.
type Resource struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
}

// PromptsListParams is the params for the "prompts/list" method.
type PromptsListParams struct {
	Cursor string `json:"cursor,omitempty"`
}

// PromptsListResult is the result of the "prompts/list" method.
type PromptsListResult struct {
	Prompts []Prompt `json:"prompts"`
	Cursor  string   `json:"cursor,omitempty"`
}

// Prompt describes an MCP prompt.
type Prompt struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument describes a prompt argument.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}