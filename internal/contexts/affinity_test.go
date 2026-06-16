package contexts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAffinityContext_StoreAndRetrieve(t *testing.T) {
	state := &AffinityState{
		Hash:       "abc123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4o",
	}

	ctx := WithAffinityState(t.Context(), state)
	got, ok := GetAffinityState(ctx)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "abc123", got.Hash)
	assert.Equal(t, "X-Session-Affinity", got.Source)
	assert.Equal(t, "gpt-4o", got.ModelScope)
}

func TestAffinityContext_RetrieveMissing(t *testing.T) {
	got, ok := GetAffinityState(t.Context())
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestAffinityContext_NilStateIsNoOp(t *testing.T) {
	// Passing nil must NOT store anything in the context.
	ctx := WithAffinityState(t.Context(), nil)

	got, ok := GetAffinityState(ctx)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestAffinityContext_OverwritePrevious(t *testing.T) {
	first := &AffinityState{Hash: "first", Source: "X-Session-Affinity", ModelScope: "gpt-4o"}
	second := &AffinityState{Hash: "second", Source: "metadata.session_id", ModelScope: "claude-3-5-sonnet"}

	ctx := WithAffinityState(t.Context(), first)
	ctx = WithAffinityState(ctx, second)

	got, ok := GetAffinityState(ctx)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "second", got.Hash)
	assert.Equal(t, "metadata.session_id", got.Source)
	assert.Equal(t, "claude-3-5-sonnet", got.ModelScope)
}

func TestAffinityContext_CoexistsWithOtherValues(t *testing.T) {
	state := &AffinityState{Hash: "h1", Source: "X-Session-Id", ModelScope: "gpt-4o"}

	ctx := t.Context()
	ctx = WithTraceID(ctx, "trace-1")
	ctx = WithRequestID(ctx, "req-1")
	ctx = WithAffinityState(ctx, state)

	// TraceID still readable.
	traceID, ok := GetTraceID(ctx)
	require.True(t, ok)
	assert.Equal(t, "trace-1", traceID)

	// Affinity state still readable.
	got, ok := GetAffinityState(ctx)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "h1", got.Hash)
}
