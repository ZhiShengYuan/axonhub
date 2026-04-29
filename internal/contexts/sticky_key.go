package contexts

import (
	"context"
)

// WithStickyKey stores a sticky key string in the context.
func WithStickyKey(ctx context.Context, stickyKey string) context.Context {
	container := getContainer(ctx)
	container.StickyKey = &stickyKey

	return withContainer(ctx, container)
}

// GetStickyKey retrieves the sticky key string from the context.
// Returns (string, false) if not set.
func GetStickyKey(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.StickyKey != nil {
		return *container.StickyKey, true
	}

	return "", false
}