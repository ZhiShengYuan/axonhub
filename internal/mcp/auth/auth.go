// Package auth provides authentication utilities for MCP endpoint.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/mcp/protocol"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
)

// APIKeyPrefix is the required prefix for AxonHub API keys.
const APIKeyPrefix = "ah-"

// ErrInvalidAPIKey is returned when the API key is invalid.
var ErrInvalidAPIKey = errors.New("invalid API key")

// ErrMissingAPIKey is returned when no API key is provided.
var ErrMissingAPIKey = errors.New("missing API key")

// ErrUnauthorized is returned when authentication fails.
var ErrUnauthorized = errors.New("unauthorized")

// ValidateAPIKey validates the AxonHub API key from the request context.
// It extracts the Bearer token from Authorization header and validates it via authService.
func ValidateAPIKey(c *gin.Context, authService *biz.AuthService) (*ent.APIKey, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return nil, ErrMissingAPIKey
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, ErrInvalidAPIKey
	}

	key := strings.TrimPrefix(authHeader, "Bearer ")
	if key == "" {
		return nil, ErrInvalidAPIKey
	}

	if !strings.HasPrefix(key, APIKeyPrefix) {
		return nil, ErrInvalidAPIKey
	}

	apiKey, err := authService.AuthenticateAPIKey(c.Request.Context(), key)
	if err != nil {
		if ent.IsNotFound(err) || errors.Is(err, biz.ErrInvalidAPIKey) {
			return nil, ErrInvalidAPIKey
		}
		return nil, fmt.Errorf("failed to validate API key: %w", err)
	}

	return apiKey, nil
}

// InjectUpstreamAuth injects upstream authentication from channel credentials into the request.
// It modifies the request headers to include upstream API keys or bearer tokens.
func InjectUpstreamAuth(req *http.Request, credentials *objects.MCPCredentials) error {
	if req == nil || credentials == nil {
		return nil
	}

	if credentials.UpstreamAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+credentials.UpstreamAPIKey)
	}

	if credentials.UpstreamBearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+credentials.UpstreamBearerToken)
	}

	return nil
}

// SanitizeResponse removes any upstream secrets from the response before sending to client.
// It recursively walks the response data and removes sensitive fields.
func SanitizeResponse(data json.RawMessage) (json.RawMessage, error) {
	if data == nil {
		return nil, nil
	}

	var sanitized any
	if err := json.Unmarshal(data, &sanitized); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	sanitized = removeSensitiveData(sanitized)

	return json.Marshal(sanitized)
}

// removeSensitiveData recursively removes sensitive fields from data.
func removeSensitiveData(data any) any {
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any)
		for key, value := range v {
			if isSensitiveField(key) {
				continue
			}
			result[key] = removeSensitiveData(value)
		}
		return result

	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = removeSensitiveData(item)
		}
		return result

	default:
		return v
	}
}

// isSensitiveField returns true if the field name suggests it contains sensitive data.
func isSensitiveField(name string) bool {
	sensitiveFields := []string{
		"api_key",
		"apiKey",
		"api-key",
		"secret",
		"token",
		"password",
		"credential",
		"upstream_api_key",
		"upstreamAPIKey",
		"upstream_bearer_token",
		"upstreamBearerToken",
	}

	lower := strings.ToLower(name)
	for _, sensitive := range sensitiveFields {
		if strings.Contains(lower, sensitive) {
			return true
		}
	}
	return false
}

// CreateJSONRPCError creates a JSON-RPC error response.
func CreateJSONRPCError(id protocol.ID, code int, message string) *protocol.ErrorResponse {
	return &protocol.ErrorResponse{
		JSONRPC: protocol.JSONRPCVersion,
		Error: &protocol.Error{
			Code:    code,
			Message: message,
		},
		ID: id,
	}
}

// SendError sends a JSON-RPC error response.
func SendError(c *gin.Context, statusCode int, errResp *protocol.ErrorResponse) {
	c.JSON(statusCode, errResp)
}

// SendSuccess sends a JSON-RPC success response.
func SendSuccess(c *gin.Context, statusCode int, result protocol.ID, resultData json.RawMessage) {
	c.JSON(statusCode, &protocol.Response{
		JSONRPC: protocol.JSONRPCVersion,
		Result:  resultData,
		ID:      result,
	})
}