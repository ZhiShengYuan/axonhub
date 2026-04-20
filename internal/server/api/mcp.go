package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/internal/mcp"
	"github.com/looplj/axonhub/internal/mcp/auth"
	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/transport"
	"github.com/looplj/axonhub/internal/server/biz"
)

type MCPHandler struct {
	proxy         *mcp.Proxy
	authService   *biz.AuthService
	channelService *biz.ChannelService
}

func NewMCPHandler(proxy *mcp.Proxy, authService *biz.AuthService, channelService *biz.ChannelService) *MCPHandler {
	return &MCPHandler{
		proxy:         proxy,
		authService:   authService,
		channelService: channelService,
	}
}

func (h *MCPHandler) HandleMCP(c *gin.Context) {
	if c.Request.Method == http.MethodGet {
		h.handleGET(c)
		return
	}
	h.handlePOST(c)
}

func (h *MCPHandler) handlePOST(c *gin.Context) {
	if h.authService == nil {
		c.JSON(http.StatusInternalServerError, auth.CreateJSONRPCError(nil, protocol.CodeInternalError, "auth service not configured"))
		return
	}

	_, err := auth.ValidateAPIKey(c, h.authService)
	if err != nil {
		c.JSON(http.StatusUnauthorized, auth.CreateJSONRPCError(nil, protocol.CodeInternalError, "unauthorized"))
		return
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, auth.CreateJSONRPCError(nil, protocol.CodeParseError, "failed to read request body"))
		return
	}

	var req protocol.Request
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, auth.CreateJSONRPCError(nil, protocol.CodeParseError, "invalid JSON-RPC request"))
		return
	}

	sessionID := c.GetHeader(transport.HeaderMCPSessionID)

	if req.Method == "initialize" {
		initResp, err := h.proxy.HandleInitialize(c.Request.Context(), &req)
		if err != nil {
			errMsg := "internal error"
			if mcpErr, ok := err.(*protocol.Error); ok {
				c.JSON(http.StatusBadRequest, auth.CreateJSONRPCError(req.ID, mcpErr.Code, mcpErr.Message))
				return
			}
			c.JSON(http.StatusInternalServerError, auth.CreateJSONRPCError(req.ID, protocol.CodeInternalError, errMsg))
			return
		}

		c.Header(transport.HeaderMCPSessionID, initResp.SessionID)
		c.JSON(http.StatusOK, initResp.Response)
		return
	}

	if sessionID == "" {
		c.JSON(http.StatusBadRequest, auth.CreateJSONRPCError(req.ID, protocol.CodeInvalidRequest, "session ID required"))
		return
	}

	if req.ID == nil {
		if err := h.proxy.HandleNotification(c.Request.Context(), sessionID, &protocol.Notification{
			JSONRPC: req.JSONRPC,
			Method:  req.Method,
			Params:  req.Params,
		}); err != nil {
			errMsg := "internal error"
			if mcpErr, ok := err.(*protocol.Error); ok {
				c.JSON(http.StatusBadRequest, auth.CreateJSONRPCError(req.ID, mcpErr.Code, mcpErr.Message))
				return
			}
			c.JSON(http.StatusInternalServerError, auth.CreateJSONRPCError(req.ID, protocol.CodeInternalError, errMsg))
			return
		}
		c.Status(http.StatusNoContent)
		return
	}

	resp, err := h.proxy.HandleRequest(c.Request.Context(), sessionID, &req)
	if err != nil {
		errMsg := "internal error"
		if mcpErr, ok := err.(*protocol.Error); ok {
			c.JSON(http.StatusBadRequest, auth.CreateJSONRPCError(req.ID, mcpErr.Code, mcpErr.Message))
			return
		}
		c.JSON(http.StatusInternalServerError, auth.CreateJSONRPCError(req.ID, protocol.CodeInternalError, errMsg))
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *MCPHandler) handleGET(c *gin.Context) {
	sessionID := c.GetHeader(transport.HeaderMCPSessionID)
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session ID required"})
		return
	}

	_, err := h.proxy.HandleRequest(c.Request.Context(), sessionID, &protocol.Request{
		Method: "ping",
	})
	if err != nil {
		if errors.Is(err, mcp.ErrSessionNotActive) || errors.Is(err, mcp.ErrMissingSessionID) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.Header("Content-Type", transport.ContentTypeEventStream)
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	c.Stream(func(w io.Writer) bool {
		return false
	})
}