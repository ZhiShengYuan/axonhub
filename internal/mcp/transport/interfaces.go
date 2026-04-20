package transport

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/looplj/axonhub/internal/mcp/protocol"
)

// ClientHandler handles MCP client requests to an upstream server.
type ClientHandler interface {
	// HandleRequest processes a JSON-RPC request and returns the response.
	HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error)

	// HandleNotification processes a JSON-RPC notification (no response expected).
	HandleNotification(ctx context.Context, notif *protocol.Notification) error
}

// Client is an MCP Streamable HTTP client that can send requests to an upstream server.
type Client interface {
	// ProtocolVersion returns the MCP protocol version this client supports.
	ProtocolVersion() string

	// SendRequest sends a JSON-RPC request to the server.
	SendRequest(ctx context.Context, method string, params json.RawMessage) (*protocol.Response, error)

	// SendNotification sends a JSON-RPC notification to the server.
	SendNotification(ctx context.Context, method string, params json.RawMessage) error

	// SendServerMessage sends an SSE event to the server (for GET-based server-initiated messages).
	SendServerMessage(ctx context.Context, event []byte) error
}

// HTTPClient is the interface for making HTTP requests.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ServerHandler handles MCP server-side requests from a client.
type ServerHandler interface {
	// HandleRequest processes an incoming JSON-RPC request.
	HandleRequest(ctx context.Context, req *protocol.Request) (*protocol.Response, error)

	// HandleNotification processes an incoming JSON-RPC notification.
	HandleNotification(ctx context.Context, notif *protocol.Notification) error
}

// Server is an MCP Streamable HTTP server that receives requests from clients.
type Server interface {
	// HandlePOST handles client→server requests via POST.
	HandlePOST(ctx context.Context, body []byte) ([]byte, error)

	// HandleGET handles server→client notifications via GET+SSE.
	// Returns a channel that yields SSE events until the context is cancelled.
	HandleGET(ctx context.Context) (<-chan []byte, error)

	// Sessions returns the session manager.
	Sessions() SessionManager
}

// SessionManager manages MCP sessions.
type SessionManager interface {
	// CreateSession creates a new MCP session.
	CreateSession(sessionID, channelID string, caps protocol.ServerCapabilities) (*Session, error)

	// GetSession retrieves a session by ID.
	GetSession(sessionID string) (*Session, error)

	// RemoveSession removes a session by ID.
	RemoveSession(sessionID string) error

	// ListSessions lists all active sessions.
	ListSessions() []*Session
}

// Session represents an MCP session.
type Session struct {
	ID                string
	ChannelID         string
	Capabilities      protocol.ServerCapabilities
	CreatedAt         int64
	LastPing          int64
	ProtocolVersion   string
}

// HTTPHeaders represents HTTP headers for MCP transport.
type HTTPHeaders struct {
	MCPProtocolVersion string
	MCPSessionID      string
	Accept            string
	ContentType       string
}

// ContentType constants.
const (
	ContentTypeJSON      = "application/json"
	ContentTypeEventStream = "text/event-stream"
)

// Header constants.
const (
	HeaderMCPProtocolVersion = "MCP-Protocol-Version"
	HeaderMCPSessionID      = "MCP-Session-Id"
)
