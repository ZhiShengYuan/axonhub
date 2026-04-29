package middleware

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/internal/contexts"
)

// WithStickyKey is a middleware that extracts a sticky key from the context cascade
// and stores it for use in request handling.
//
// Cascade order:
//  1. Trace entity ID (if trace exists)
//  2. Trace ID string (if trace ID exists)
//  3. Thread entity ID (if thread exists)
//  4. Empty string (no sticky key)
func WithStickyKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		var stickyKey string

		// 1. Try to get trace entity ID
		if trace, ok := contexts.GetTrace(ctx); ok && trace != nil {
			stickyKey = strconv.Itoa(trace.ID)
			ctx = contexts.WithStickyKey(ctx, stickyKey)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// 2. Try to get trace ID string
		if traceID, ok := contexts.GetTraceID(ctx); ok {
			stickyKey = traceID
			ctx = contexts.WithStickyKey(ctx, stickyKey)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// 3. Try to get thread entity ID
		if thread, ok := contexts.GetThread(ctx); ok && thread != nil {
			stickyKey = strconv.Itoa(thread.ID)
			ctx = contexts.WithStickyKey(ctx, stickyKey)
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}

		// 4. No sticky key available - store empty string
		ctx = contexts.WithStickyKey(ctx, "")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}