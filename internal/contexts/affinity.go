package contexts

import (
	"context"
)

// AffinityState represents the request affinity context derived from
// session/thread headers and body fields. It is used to bias subsequent
// channel selection toward the channel that previously served the same
// affinity key (soft ordering boost, never a hard pin).
//
// AffinityState stores only a SHA256 hash of the raw affinity key — the
// raw value is intentionally never persisted or logged.
type AffinityState struct {
	// Hash is the SHA256 hash of the normalized raw affinity key.
	Hash string
	// Source is the header name or body field path that produced the
	// affinity key (e.g. "X-Session-Affinity" or "metadata.session_id").
	Source string
	// ModelScope is the model name extracted from the request body JSON,
	// or "unknown" when the field is absent.
	ModelScope string
}

// WithAffinityState stores the affinity state in the context.
func WithAffinityState(ctx context.Context, state *AffinityState) context.Context {
	if state == nil {
		return ctx
	}

	container := getContainer(ctx)
	container.AffinityState = state

	return withContainer(ctx, container)
}

// GetAffinityState retrieves the affinity state from the context.
// The second return value is true only when a non-nil AffinityState is present.
func GetAffinityState(ctx context.Context) (*AffinityState, bool) {
	container := getContainer(ctx)
	if container.AffinityState == nil {
		return nil, false
	}

	return container.AffinityState, true
}
