package anthropic

import (
	"errors"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
)

func TestWrapAPIError(t *testing.T) {
	t.Run("non-API error", func(t *testing.T) {
		originalErr := errors.New("some error")
		wrapped := wrapAPIError(originalErr)

		assert.Contains(t, wrapped.Error(), "some error")
		assert.Contains(t, wrapped.Error(), "anthropic:")
	})

	t.Run("API error with status code", func(t *testing.T) {
		apiErr := &anthropic.Error{
			StatusCode: 500,
		}

		wrapped := wrapAPIError(apiErr)

		assert.Contains(t, wrapped.Error(), "status 500")
	})

	t.Run("API error with 400 status", func(t *testing.T) {
		apiErr := &anthropic.Error{
			StatusCode: 400,
		}

		wrapped := wrapAPIError(apiErr)

		assert.Contains(t, wrapped.Error(), "status 400")
	})
}

func TestExtractErrorMessage(t *testing.T) {
	t.Run("empty raw JSON", func(t *testing.T) {
		apiErr := &anthropic.Error{}
		msg := extractErrorMessage(apiErr)

		assert.Empty(t, msg)
	})
}
