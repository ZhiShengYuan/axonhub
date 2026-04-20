package health

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/mcp/transport"
	"github.com/looplj/axonhub/internal/objects"
)

const DefaultProbeTimeout = 5 * time.Second

type ProbeResult struct {
	Healthy          bool
	Latency         time.Duration
	ProtocolVersion string
	Capabilities    protocol.ServerCapabilities
	Error           string
}

func ProbeUpstream(ctx context.Context, baseURL string, creds *objects.MCPCredentials) (*ProbeResult, error) {
	probeCtx, cancel := context.WithTimeout(ctx, DefaultProbeTimeout)
	defer cancel()

	initParams := protocol.InitializeParams{
		ProtocolVersion: protocol.SupportedProtocolVersion,
		Capabilities: protocol.ClientCapabilities{
			Roots:    protocol.RootsCapability{},
			Sampling: protocol.SamplingCapability{},
		},
		ClientInfo: protocol.ClientInfo{
			Name:    "axonhub-mcp-probe",
			Version: "1.0.0",
		},
	}

	paramsBytes, err := json.Marshal(initParams)
	if err != nil {
		return &ProbeResult{
			Healthy: false,
			Error:   fmt.Sprintf("failed to marshal initialize params: %v", err),
		}, nil
	}

	req := &protocol.Request{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  "initialize",
		Params:  paramsBytes,
		ID:      1,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return &ProbeResult{
			Healthy: false,
			Error:   fmt.Sprintf("failed to marshal request: %v", err),
		}, nil
	}

	httpReq, err := http.NewRequestWithContext(probeCtx, http.MethodPost, baseURL+"/mcp", bytes.NewReader(reqBytes))
	if err != nil {
		return &ProbeResult{
			Healthy: false,
			Error:   fmt.Sprintf("failed to create HTTP request: %v", err),
		}, nil
	}

	httpReq.Header.Set("Content-Type", transport.ContentTypeJSON)
	httpReq.Header.Set(transport.HeaderMCPProtocolVersion, protocol.SupportedProtocolVersion)

	if creds != nil {
		if creds.UpstreamAPIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+creds.UpstreamAPIKey)
		} else if creds.UpstreamBearerToken != "" {
			httpReq.Header.Set("Authorization", "Bearer "+creds.UpstreamBearerToken)
		}
	}

	start := time.Now()

	client := &http.Client{
		Timeout: DefaultProbeTimeout,
	}

	resp, err := client.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		return &ProbeResult{
			Healthy: false,
			Latency: latency,
			Error:   fmt.Sprintf("upstream unreachable: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &ProbeResult{
			Healthy: false,
			Latency: latency,
			Error:   fmt.Sprintf("failed to read response: %v", err),
		}, nil
	}

	if containsError(respBody) {
		var errResp protocol.ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error != nil {
			return &ProbeResult{
				Healthy: false,
				Latency: latency,
				Error:   fmt.Sprintf("json-rpc error: %s (code: %d)", errResp.Error.Message, errResp.Error.Code),
			}, nil
		}
	}

	var jsonRPCResp protocol.Response
	if err := json.Unmarshal(respBody, &jsonRPCResp); err != nil {
		return &ProbeResult{
			Healthy: false,
			Latency: latency,
			Error:   fmt.Sprintf("invalid json-rpc response: %v", err),
		}, nil
	}

	if jsonRPCResp.Result == nil {
		return &ProbeResult{
			Healthy: false,
			Latency: latency,
			Error:   "no result in initialize response",
		}, nil
	}

	var initResult protocol.InitializeResult
	if err := json.Unmarshal(jsonRPCResp.Result, &initResult); err != nil {
		return &ProbeResult{
			Healthy: false,
			Latency: latency,
			Error:   fmt.Sprintf("invalid initialize result: %v", err),
		}, nil
	}

	return &ProbeResult{
		Healthy:         true,
		Latency:         latency,
		ProtocolVersion: initResult.ProtocolVersion,
		Capabilities:    initResult.Capabilities,
	}, nil
}

func containsError(body []byte) bool {
	return bytes.Contains(body, []byte(`"error"`))
}