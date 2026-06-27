package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

// sessionAffinityHeaders lists the supported session affinity headers in
// precedence order. The first non-empty, non-whitespace value wins.
var sessionAffinityHeaders = []string{
	"X-Session-ID",
	"X-Session-Affinity",
	"x-claude-code-session-id",
	"session-id",
	"x-conversation-id",
	"x-thread-id",
}

// WithSessionAffinityID is a gin middleware that extracts a session affinity ID
// from request headers and stores it in the request context via shared.WithSessionID.
// It runs independently of trace creation and does not overwrite an existing
// session ID already present in the context.
func WithSessionAffinityID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// If session ID is already set in context (e.g. by auth), don't overwrite.
		if _, ok := shared.GetSessionID(c.Request.Context()); ok {
			c.Next()
			return
		}

		for _, header := range sessionAffinityHeaders {
			val := strings.TrimSpace(c.GetHeader(header))
			if val != "" {
				ctx := shared.WithSessionID(c.Request.Context(), val)
				c.Request = c.Request.WithContext(ctx)
				break
			}
		}

		c.Next()
	}
}
