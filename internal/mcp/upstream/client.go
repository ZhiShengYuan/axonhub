package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/looplj/axonhub/internal/mcp/auth"
	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/transport"
	"github.com/looplj/axonhub/internal/objects"
)

type UpstreamClient struct {
	httpClient *http.Client
}

func NewUpstreamClient() *UpstreamClient {
	return &UpstreamClient{
		httpClient: &http.Client{},
	}
}

func (c *UpstreamClient) SendInitialize(
	ctx context.Context,
	baseURL string,
	req *protocol.Request,
	creds *objects.MCPCredentials,
) (*protocol.Response, string, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mcp", bytes.NewReader(reqBody))
	if err != nil {
		return nil, "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", transport.ContentTypeJSON)
	httpReq.Header.Set(transport.HeaderMCPProtocolVersion, protocol.SupportedProtocolVersion)

	if err := auth.InjectUpstreamAuth(httpReq, creds); err != nil {
		return nil, "", fmt.Errorf("failed to inject upstream auth: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	upstreamSessionID := resp.Header.Get(transport.HeaderMCPSessionID)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, upstreamSessionID, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, upstreamSessionID, fmt.Errorf("upstream initialize failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var jsonRPCResp protocol.Response
	if err := json.Unmarshal(respBody, &jsonRPCResp); err != nil {
		return nil, upstreamSessionID, fmt.Errorf("failed to parse response: %w", err)
	}

	return &jsonRPCResp, upstreamSessionID, nil
}

func (c *UpstreamClient) SendRequest(
	ctx context.Context,
	baseURL string,
	req *protocol.Request,
	sessionID string,
	creds *objects.MCPCredentials,
) (*protocol.Response, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mcp", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", transport.ContentTypeJSON)
	httpReq.Header.Set(transport.HeaderMCPProtocolVersion, protocol.SupportedProtocolVersion)

	if sessionID != "" {
		httpReq.Header.Set(transport.HeaderMCPSessionID, sessionID)
	}

	if err := auth.InjectUpstreamAuth(httpReq, creds); err != nil {
		return nil, fmt.Errorf("failed to inject upstream auth: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errResp protocol.ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error != nil {
			return nil, fmt.Errorf("upstream error: %s (code: %d)", errResp.Error.Message, errResp.Error.Code)
		}
		return nil, fmt.Errorf("upstream request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	if bytes.Contains(respBody, []byte(`"error"`)) {
		var errResp protocol.ErrorResponse
		if err := json.Unmarshal(respBody, &errResp); err != nil {
			return nil, fmt.Errorf("failed to parse error response: %w", err)
		}
		return nil, &protocol.Error{
			Code:    errResp.Error.Code,
			Message: errResp.Error.Message,
			Data:    errResp.Error.Data,
		}
	}

	var jsonRPCResp protocol.Response
	if err := json.Unmarshal(respBody, &jsonRPCResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &jsonRPCResp, nil
}

func (c *UpstreamClient) SendNotification(
	ctx context.Context,
	baseURL string,
	notif *protocol.Notification,
	sessionID string,
	creds *objects.MCPCredentials,
) error {
	reqBody, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/mcp", bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", transport.ContentTypeJSON)
	httpReq.Header.Set(transport.HeaderMCPProtocolVersion, protocol.SupportedProtocolVersion)

	if sessionID != "" {
		httpReq.Header.Set(transport.HeaderMCPSessionID, sessionID)
	}

	if err := auth.InjectUpstreamAuth(httpReq, creds); err != nil {
		return fmt.Errorf("failed to inject upstream auth: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upstream notification failed with status %d", resp.StatusCode)
	}

	return nil
}