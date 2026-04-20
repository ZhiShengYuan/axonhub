package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/looplj/axonhub/internal/mcp/auth"
	"github.com/looplj/axonhub/internal/mcp/metrics"
	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/registry"
	"github.com/looplj/axonhub/internal/mcp/router"
	"github.com/looplj/axonhub/internal/mcp/session"
	"github.com/looplj/axonhub/internal/mcp/upstream"
	"github.com/looplj/axonhub/internal/objects"
)

var (
	ErrInvalidProtocolVersion = errors.New("invalid protocol version")
	ErrMissingSessionID       = errors.New("missing session ID")
	ErrSessionNotActive       = errors.New("session not active")
)



type InitializeResponse struct {
	Response   *protocol.Response
	SessionID  string
}

type Proxy struct {
	sm               *session.SessionManager
	channelURLs      map[string]string
	channelCreds     map[string]*objects.MCPCredentials
	registry         *registry.CapabilityRegistry
	operationRouter  *router.OperationRouter
	upstreamClient  *upstream.UpstreamClient
	metrics         *metrics.Metrics
}

func NewProxy(channelURLs map[string]string, channelCreds map[string]*objects.MCPCredentials, m *metrics.Metrics) *Proxy {
	reg := registry.NewCapabilityRegistry()
	sm := session.NewSessionManager()
	p := &Proxy{
		sm:              sm,
		channelURLs:     channelURLs,
		channelCreds:    channelCreds,
		registry:        reg,
		operationRouter: router.NewOperationRouter(reg),
		upstreamClient:  upstream.NewUpstreamClient(),
		metrics:        m,
	}
	if m != nil {
		sm.SetOnRemoveCallback(func(sessionID string) {
			m.DecrementActiveSessions()
		})
	}
	return p
}

func (p *Proxy) CloseSession(sessionID string) error {
	return p.sm.RemoveSession(sessionID)
}

func (p *Proxy) HandleInitialize(ctx context.Context, req *protocol.Request) (*InitializeResponse, error) {
	start := time.Now()

	var params protocol.InitializeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid initialize params"}
	}

	if params.ProtocolVersion != protocol.SupportedProtocolVersion {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "unsupported protocol version"}
	}

	capabilities := protocol.ServerCapabilities{
		Tools:    &protocol.ToolsCapability{ListChanged: true},
		Resources: &protocol.ResourcesCapability{Subscribe: true, ListChanged: true},
		Prompts:  &protocol.PromptsCapability{ListChanged: true},
	}

	channelID := p.selectChannel()
	sess, err := p.sm.CreateSession(channelID, capabilities, protocol.SupportedProtocolVersion)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to create session"}
	}

	baseURL := p.channelURLs[channelID]
	if baseURL != "" {
		upstreamReq := &protocol.Request{
			JSONRPC: protocol.JSONRPCVersion,
			Method:  "initialize",
			Params:  req.Params,
			ID:      req.ID,
		}
		creds := p.channelCreds[channelID]
		_, upstreamSessionID, err := p.upstreamClient.SendInitialize(ctx, baseURL, upstreamReq, creds)
		if err != nil {
			// Upstream initialize failed — remove the session we just created and return error
			_ = p.sm.RemoveSession(sess.ID)
			return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: fmt.Sprintf("upstream initialize failed: %v", err)}
		}
		if upstreamSessionID != "" {
			sess.UpstreamSessionID = upstreamSessionID
		}
	}

	if p.metrics != nil {
		p.metrics.IncrementActiveSessions()
		p.metrics.RecordInitialization(channelID, time.Since(start))
	}

	result := protocol.InitializeResult{
		ProtocolVersion: protocol.SupportedProtocolVersion,
		Capabilities:    capabilities,
		ServerInfo: protocol.ServerInfo{
			Name:    "axonhub-mcp-proxy",
			Version: "1.0.0",
		},
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal initialize result"}
	}
	return &InitializeResponse{
		Response: &protocol.Response{
			JSONRPC: protocol.JSONRPCVersion,
			Result:  resultBytes,
			ID:      req.ID,
		},
		SessionID: sess.ID,
	}, nil
}

func (p *Proxy) HandleRequest(ctx context.Context, sessionID string, req *protocol.Request) (*protocol.Response, error) {
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
		return p.handleToolsList(req, sess)
	case "resources/list":
		return p.handleResourcesList(req, sess)
	case "prompts/list":
		return p.handlePromptsList(req, sess)
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

func (p *Proxy) HandleNotification(ctx context.Context, sessionID string, notif *protocol.Notification) error {
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
	case "notifications/cancelled":
		return p.forwardNotificationToUpstream(ctx, sess, notif)
	default:
		return nil
	}
}

func (p *Proxy) selectChannel() string {
	for channelID := range p.channelURLs {
		return channelID
	}
	return "default"
}

func (p *Proxy) handleToolsList(req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	tools := p.registry.SortedTools()
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
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal tools list result"}
	}
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (p *Proxy) handleResourcesList(req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	resources := p.registry.SortedResources()
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
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal resources list result"}
	}
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (p *Proxy) handlePromptsList(req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	prompts := p.registry.SortedPrompts()
	result := protocol.PromptsListResult{
		Prompts: make([]protocol.Prompt, 0, len(prompts)),
	}
	for _, p := range prompts {
		args := make([]protocol.PromptArgument, 0, len(p.Arguments))
		for _, a := range p.Arguments {
			args = append(args, protocol.PromptArgument{
				Name:        a.Name,
				Description: a.Description,
				Required:    a.Required,
			})
		}
		result.Prompts = append(result.Prompts, protocol.Prompt{
			Name:        p.Name,
			Description: p.Description,
			Arguments:   args,
		})
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal prompts list result"}
	}
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

type ToolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ResourceReadParams struct {
	URI string `json:"uri"`
}

type PromptGetParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func (p *Proxy) handleToolCall(ctx context.Context, req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	start := time.Now()

	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid tool call params"}
	}

	channelID, upstreamName, err := p.operationRouter.RouteToolCall(params.Name)
	if err != nil {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "tools/call", err)
		}
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: fmt.Sprintf("tool not found: %s", params.Name)}
	}

	// Enforce session affinity: operation must route to the session's bound channel
	if channelID != sess.ChannelID {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "tools/call", errors.New("session affinity violation"))
		}
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: fmt.Sprintf("tool %s not available on session channel", params.Name)}
	}

	baseURL, ok := p.channelURLs[channelID]
	if !ok {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "tools/call", errors.New("channel URL not found"))
		}
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "channel URL not found"}
	}

	upstreamParams := map[string]any{"name": upstreamName}
	if params.Arguments != nil && len(params.Arguments) > 0 {
		upstreamParams["arguments"] = json.RawMessage(params.Arguments)
	}
	upstreamParamsBytes, err := json.Marshal(upstreamParams)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal upstream params"}
	}

	upstreamReq := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  "tools/call",
		Params:  upstreamParamsBytes,
		ID:      req.ID,
	}

	creds := p.channelCreds[channelID]
	upstreamResp, err := p.upstreamClient.SendRequest(ctx, baseURL, upstreamReq, sess.UpstreamSessionID, creds)
	if err != nil {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "tools/call", err)
		}
		if mcpErr, ok := err.(*protocol.Error); ok {
			return nil, mcpErr
		}
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: fmt.Sprintf("upstream request failed: %v", err)}
	}

	if upstreamResp.Result != nil {
		sanitized, err := auth.SanitizeResponse(upstreamResp.Result)
		if err != nil {
			if p.metrics != nil {
				p.metrics.RecordError(sess.ID, channelID, "tools/call", err)
			}
			return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to sanitize response"}
		}
		upstreamResp.Result = sanitized
	}

	if p.metrics != nil {
		p.metrics.RecordInvocation(sess.ID, channelID, "tools/call", time.Since(start))
	}

	return upstreamResp, nil
}

func (p *Proxy) handleResourceRead(ctx context.Context, req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	start := time.Now()

	var params ResourceReadParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid resource read params"}
	}

	channelID, upstreamURI, err := p.operationRouter.RouteResourceAccess(params.URI)
	if err != nil {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "resources/read", err)
		}
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: fmt.Sprintf("resource not found: %s", params.URI)}
	}

	// Enforce session affinity: operation must route to the session's bound channel
	if channelID != sess.ChannelID {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "resources/read", errors.New("session affinity violation"))
		}
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: fmt.Sprintf("resource %s not available on session channel", params.URI)}
	}

	baseURL, ok := p.channelURLs[channelID]
	if !ok {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "resources/read", errors.New("channel URL not found"))
		}
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "channel URL not found"}
	}

	upstreamParams := map[string]any{"uri": upstreamURI}
	upstreamParamsBytes, err := json.Marshal(upstreamParams)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal upstream params"}
	}

	upstreamReq := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  "resources/read",
		Params:  upstreamParamsBytes,
		ID:      req.ID,
	}

	creds := p.channelCreds[channelID]
	upstreamResp, err := p.upstreamClient.SendRequest(ctx, baseURL, upstreamReq, sess.UpstreamSessionID, creds)
	if err != nil {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "resources/read", err)
		}
		if mcpErr, ok := err.(*protocol.Error); ok {
			return nil, mcpErr
		}
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: fmt.Sprintf("upstream request failed: %v", err)}
	}

	if upstreamResp.Result != nil {
		sanitized, err := auth.SanitizeResponse(upstreamResp.Result)
		if err != nil {
			if p.metrics != nil {
				p.metrics.RecordError(sess.ID, channelID, "resources/read", err)
			}
			return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to sanitize response"}
		}
		upstreamResp.Result = sanitized
	}

	if p.metrics != nil {
		p.metrics.RecordInvocation(sess.ID, channelID, "resources/read", time.Since(start))
	}

	return upstreamResp, nil
}

func (p *Proxy) handlePromptGet(ctx context.Context, req *protocol.Request, sess *session.Session) (*protocol.Response, error) {
	start := time.Now()

	var params PromptGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: "invalid prompt get params"}
	}

	channelID, upstreamName, err := p.operationRouter.RoutePrompt(params.Name)
	if err != nil {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "prompts/get", err)
		}
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: fmt.Sprintf("prompt not found: %s", params.Name)}
	}

	// Enforce session affinity: operation must route to the session's bound channel
	if channelID != sess.ChannelID {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "prompts/get", errors.New("session affinity violation"))
		}
		return nil, &protocol.Error{Code: protocol.CodeInvalidParams, Message: fmt.Sprintf("prompt %s not available on session channel", params.Name)}
	}

	baseURL, ok := p.channelURLs[channelID]
	if !ok {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "prompts/get", errors.New("channel URL not found"))
		}
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "channel URL not found"}
	}

	upstreamParams := map[string]any{"name": upstreamName}
	if params.Arguments != nil && len(params.Arguments) > 0 {
		upstreamParams["arguments"] = json.RawMessage(params.Arguments)
	}
	upstreamParamsBytes, err := json.Marshal(upstreamParams)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal upstream params"}
	}

	upstreamReq := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  "prompts/get",
		Params:  upstreamParamsBytes,
		ID:      req.ID,
	}

	creds := p.channelCreds[channelID]
	upstreamResp, err := p.upstreamClient.SendRequest(ctx, baseURL, upstreamReq, sess.UpstreamSessionID, creds)
	if err != nil {
		if p.metrics != nil {
			p.metrics.RecordError(sess.ID, channelID, "prompts/get", err)
		}
		if mcpErr, ok := err.(*protocol.Error); ok {
			return nil, mcpErr
		}
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: fmt.Sprintf("upstream request failed: %v", err)}
	}

	if upstreamResp.Result != nil {
		sanitized, err := auth.SanitizeResponse(upstreamResp.Result)
		if err != nil {
			if p.metrics != nil {
				p.metrics.RecordError(sess.ID, channelID, "prompts/get", err)
			}
			return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to sanitize response"}
		}
		upstreamResp.Result = sanitized
	}

	if p.metrics != nil {
		p.metrics.RecordInvocation(sess.ID, channelID, "prompts/get", time.Since(start))
	}

	return upstreamResp, nil
}

func (p *Proxy) handlePing(req *protocol.Request) (*protocol.Response, error) {
	result := map[string]bool{"pong": true}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, &protocol.Error{Code: protocol.CodeInternalError, Message: "failed to marshal ping result"}
	}
	return &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultBytes,
		ID:      req.ID,
	}, nil
}

func (p *Proxy) forwardNotificationToUpstream(ctx context.Context, sess *session.Session, notif *protocol.Notification) error {
	if sess.ChannelID == "" || sess.UpstreamSessionID == "" {
		return nil
	}

	baseURL, ok := p.channelURLs[sess.ChannelID]
	if !ok {
		return nil
	}

	creds := p.channelCreds[sess.ChannelID]
	return p.upstreamClient.SendNotification(ctx, baseURL, notif, sess.UpstreamSessionID, creds)
}
