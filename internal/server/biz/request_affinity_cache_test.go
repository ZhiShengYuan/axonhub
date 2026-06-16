package biz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/pkg/xcache"
)

// newAffinityTestService builds a RequestService with an in-memory channel cache
// suitable for affinity cache unit tests.
func newAffinityTestService() *RequestService {
	return &RequestService{
		channelCache: xcache.NewFromConfig[int](xcache.Config{
			Mode: xcache.ModeMemory,
			Memory: xcache.MemoryConfig{
				Expiration: 30 * time.Minute,
			},
		}),
	}
}

func TestRequestService_AffinityCacheScopedHit(t *testing.T) {
	svc := newAffinityTestService()
	ctx := context.Background()

	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "hash123", 42)

	got, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "hash123")
	require.NoError(t, err)
	require.Equal(t, 42, got)
}

func TestRequestService_AffinityCacheMiss(t *testing.T) {
	svc := newAffinityTestService()
	ctx := context.Background()

	got, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "unset")
	require.NoError(t, err)
	require.Equal(t, 0, got)
}

func TestRequestService_AffinityCacheProjectIsolation(t *testing.T) {
	svc := newAffinityTestService()
	ctx := context.Background()

	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "hash", 11)
	svc.SetAffinityChannelID(ctx, 2, "gpt-4", "hash", 22)

	got1, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "hash")
	require.NoError(t, err)
	require.Equal(t, 11, got1)

	got2, err := svc.GetAffinityChannelID(ctx, 2, "gpt-4", "hash")
	require.NoError(t, err)
	require.Equal(t, 22, got2)
}

func TestRequestService_AffinityCacheModelIsolation(t *testing.T) {
	svc := newAffinityTestService()
	ctx := context.Background()

	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "hash", 11)
	svc.SetAffinityChannelID(ctx, 1, "claude-3", "hash", 22)

	got1, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "hash")
	require.NoError(t, err)
	require.Equal(t, 11, got1)

	got2, err := svc.GetAffinityChannelID(ctx, 1, "claude-3", "hash")
	require.NoError(t, err)
	require.Equal(t, 22, got2)
}

func TestRequestService_AffinityCacheHashIsolation(t *testing.T) {
	svc := newAffinityTestService()
	ctx := context.Background()

	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "hashA", 11)
	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "hashB", 22)

	gotA, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "hashA")
	require.NoError(t, err)
	require.Equal(t, 11, gotA)

	gotB, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "hashB")
	require.NoError(t, err)
	require.Equal(t, 22, gotB)
}

func TestRequestService_AffinityCacheTTL(t *testing.T) {
	// Verify set-then-get works immediately (positive write-read confirmation).
	// A real TTL expiry test would require time manipulation; here we confirm the
	// stored value is retrievable right after writing, which exercises the Set/Get path.
	svc := newAffinityTestService()
	ctx := context.Background()

	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "ttl-hash", 99)

	got, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "ttl-hash")
	require.NoError(t, err)
	require.Equal(t, 99, got)
}

func TestRequestService_AffinityCacheOverwrite(t *testing.T) {
	// Setting the same key twice should overwrite the previous value.
	svc := newAffinityTestService()
	ctx := context.Background()

	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "hash", 10)
	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "hash", 20)

	got, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "hash")
	require.NoError(t, err)
	require.Equal(t, 20, got)
}

func TestBuildAffinityChannelCacheKey(t *testing.T) {
	got := buildAffinityChannelCacheKey(1, "gpt-4", "abc123")
	require.Equal(t, "affinity_channel:1:gpt-4:abc123", got)
}

// ============================================================================
// Guardrail tests — verify affinity cache key isolation.
// ============================================================================

// TestAffinityGuardrails_ModelScopedCache verifies that the same affinity
// hash with a different model scope does NOT collide in the cache. This is
// the critical guardrail: two requests with the same X-Session-Affinity
// header but different model bodies must map to different cache keys.
//
// Without model scoping, a gpt-4 affinity hit would incorrectly route a
// subsequent claude-3 request to the gpt-4 channel. This test ensures the
// model scope dimension is part of the cache key.
func TestAffinityGuardrails_ModelScopedCache(t *testing.T) {
	svc := newAffinityTestService()
	ctx := context.Background()

	// Write affinity for gpt-4 model scope.
	svc.SetAffinityChannelID(ctx, 1, "gpt-4", "sharedhash", 10)

	// Same affinity hash but claude-3 model scope must NOT collide.
	got, err := svc.GetAffinityChannelID(ctx, 1, "claude-3", "sharedhash")
	require.NoError(t, err)
	require.Equal(t, 0, got, "different model scope must not collide with cached entry")

	// Write affinity for claude-3 model scope with a different channel.
	svc.SetAffinityChannelID(ctx, 1, "claude-3", "sharedhash", 20)

	// Both entries coexist independently.
	gotGPT, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "sharedhash")
	require.NoError(t, err)
	require.Equal(t, 10, gotGPT, "gpt-4 entry must be preserved")

	gotClaude, err := svc.GetAffinityChannelID(ctx, 1, "claude-3", "sharedhash")
	require.NoError(t, err)
	require.Equal(t, 20, gotClaude, "claude-3 entry must be independent")
}

// TestAffinityGuardrails_UnknownModelScopeIsolation verifies that the
// placeholder "unknown" model scope (used when the body has no parseable
// model field) is treated as a distinct scope, not a wildcard that matches
// all models.
func TestAffinityGuardrails_UnknownModelScopeIsolation(t *testing.T) {
	svc := newAffinityTestService()
	ctx := context.Background()

	// Write affinity with model scope "unknown".
	svc.SetAffinityChannelID(ctx, 1, "unknown", "hash", 10)

	// A lookup with model "gpt-4" must NOT match the "unknown" entry.
	got, err := svc.GetAffinityChannelID(ctx, 1, "gpt-4", "hash")
	require.NoError(t, err)
	require.Equal(t, 0, got, "gpt-4 scope must not collide with unknown scope")

	// The "unknown" entry itself must be retrievable.
	gotUnknown, err := svc.GetAffinityChannelID(ctx, 1, "unknown", "hash")
	require.NoError(t, err)
	require.Equal(t, 10, gotUnknown)
}
