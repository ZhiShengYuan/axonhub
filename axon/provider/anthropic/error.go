package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

func wrapAPIError(err error) error {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		if msg := extractErrorMessage(apiErr); msg != "" {
			return fmt.Errorf("anthropic: %s (status %d)", msg, apiErr.StatusCode)
		}
		return fmt.Errorf("anthropic: request failed (status %d)", apiErr.StatusCode)
	}
	return fmt.Errorf("anthropic: %w", err)
}

func extractErrorMessage(apiErr *anthropic.Error) string {
	raw := apiErr.RawJSON()
	if raw == "" {
		return ""
	}

	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &body) == nil && body.Error.Message != "" {
		return body.Error.Message
	}
	return ""
}
