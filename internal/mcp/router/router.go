package router

import (
	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/registry"
)

type OperationRouter struct {
	registry *registry.CapabilityRegistry
}

func NewOperationRouter(reg *registry.CapabilityRegistry) *OperationRouter {
	return &OperationRouter{
		registry: reg,
	}
}

func (r *OperationRouter) RouteToolCall(toolName string) (channelID, upstreamName string, err error) {
	channelID, upstreamName, err = r.registry.ResolveTool(toolName)
	if err != nil {
		return "", "", ErrToolNotFound
	}
	return channelID, upstreamName, nil
}

func (r *OperationRouter) RouteResourceAccess(uri string) (channelID, upstreamURI string, err error) {
	channelID, upstreamURI, err = r.registry.ResolveResource(uri)
	if err != nil {
		return "", "", ErrResourceNotFound
	}
	return channelID, upstreamURI, nil
}

func (r *OperationRouter) RoutePrompt(promptName string) (channelID, upstreamName string, err error) {
	channelID, upstreamName, err = r.registry.ResolvePrompt(promptName)
	if err != nil {
		return "", "", ErrPromptNotFound
	}
	return channelID, upstreamName, nil
}

var (
	ErrToolNotFound     = &RouterError{Message: "tool not found"}
	ErrResourceNotFound = &RouterError{Message: "resource not found"}
	ErrPromptNotFound   = &RouterError{Message: "prompt not found"}
	ErrNotImplemented   = &RouterError{Message: "operation not yet implemented"}
)

type RouterError struct {
	Message string
}

func (e *RouterError) Error() string {
	return e.Message
}

type InitializeHandler interface {
	HandleInitialize(req *protocol.Request) (*protocol.Response, error)
}
