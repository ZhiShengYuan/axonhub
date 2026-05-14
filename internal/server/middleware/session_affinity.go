package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/internal/contexts"
)

// SessionAffinityHeader is the header name for session affinity.
const SessionAffinityHeader = "X-Session-Affinity"

// MaxAffinityHeaderLength is the maximum allowed length for the affinity header value.
// Values exceeding this limit are ignored to prevent memory exhaustion and routing abuse.
const MaxAffinityHeaderLength = 256

// WithSessionAffinity is a middleware that extracts the X-Session-Affinity header
// and stores it in the request context.
func WithSessionAffinity() gin.HandlerFunc {
	return func(c *gin.Context) {
		affinity := c.GetHeader(SessionAffinityHeader)
		if affinity == "" {
			c.Next()
			return
		}

		// Normalize: trim whitespace
		affinity = strings.TrimSpace(affinity)

		// Reject obviously invalid values (e.g., only whitespace)
		if affinity == "" {
			c.Next()
			return
		}

		// Reject overlong values to prevent memory abuse and routing anomalies.
		// Raw affinity values are never logged or forwarded.
		if len(affinity) > MaxAffinityHeaderLength {
			c.Next()
			return
		}

		// Store in context
		ctx := contexts.WithSessionAffinity(c.Request.Context(), affinity)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}