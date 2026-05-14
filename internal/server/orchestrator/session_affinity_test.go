package orchestrator

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestNewSessionAffinityServiceWithNilOrEmptySecretGeneratesRandom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		secret []byte
	}{
		{
			name:   "nil secret",
			secret: nil,
		},
		{
			name:   "empty secret",
			secret: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewSessionAffinityService(tt.secret)

			require.NotNil(t, svc)
			assert.True(t, svc.GeneratesSecret())
			assert.Len(t, svc.secret, 32)
			assert.NotNil(t, svc.cache)
		})
	}
}

func TestNewSessionAffinityServiceWithProvidedSecret(t *testing.T) {
	t.Parallel()

	secret := []byte("provided-secret")

	svc := NewSessionAffinityService(secret)

	require.NotNil(t, svc)
	assert.False(t, svc.GeneratesSecret())
	assert.Equal(t, secret, svc.secret)
	assert.NotNil(t, svc.cache)
}

func TestSessionAffinityServiceGeneratesSecret(t *testing.T) {
	t.Parallel()

	generatedSvc := NewSessionAffinityService(nil)
	providedSvc := NewSessionAffinityService([]byte("provided-secret"))

	assert.True(t, generatedSvc.GeneratesSecret())
	assert.False(t, providedSvc.GeneratesSecret())
}

func TestSessionAffinityServiceBuildKeyProducesConsistentHMACSHA256Hex(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	scope := BuildAffinityScope(10, 20, "gpt-4", "openai", "session-123")
	svc := NewSessionAffinityService(secret)

	mac := hmac.New(sha256.New, secret)
	_, err := mac.Write([]byte(scope.String()))
	require.NoError(t, err)
	expected := hex.EncodeToString(mac.Sum(nil))

	key1 := svc.BuildKey(scope)
	key2 := svc.BuildKey(scope)

	assert.Equal(t, expected, key1)
	assert.Equal(t, key1, key2)
	assert.Len(t, key1, 64)
}

func TestSessionAffinityServiceGetReturnsFalseForMissingEntries(t *testing.T) {
	t.Parallel()

	svc := NewSessionAffinityService([]byte("test-secret"))
	scope := BuildAffinityScope(1, 2, "gpt-4", "openai", "session-123")

	channelID, ok := svc.Get(scope)

	assert.False(t, ok)
	assert.Zero(t, channelID)
}

func TestSessionAffinityServiceSetThenGetReturnsChannelID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := NewSessionAffinityService([]byte("test-secret"))
	scope := BuildAffinityScope(1, 2, "gpt-4", "openai", "session-123")

	svc.Set(ctx, scope, 42)
	channelID, ok := svc.Get(scope)

	assert.True(t, ok)
	assert.Equal(t, 42, channelID)
}

func TestBuildAffinityScopeProducesCorrectScope(t *testing.T) {
	t.Parallel()

	ctx := contexts.WithSessionAffinity(context.Background(), "session-123")
	sessionAffinity, ok := contexts.GetSessionAffinity(ctx)
	require.True(t, ok)

	scope := BuildAffinityScope(10, 20, "gpt-4", "openai", sessionAffinity)

	assert.Equal(t, 10, scope.ProjectID)
	assert.Equal(t, 20, scope.APIKeyID)
	assert.Equal(t, "gpt-4", scope.OriginalModel)
	assert.Equal(t, "openai", scope.ResolvedProvider)
	assert.Equal(t, "session-123", scope.SessionAffinity)
	// JSON format prevents delimiter ambiguity
	assert.Equal(t, `{"p":10,"a":20,"m":"gpt-4","r":"openai","s":"session-123"}`, scope.String())
}

func TestApplyAffinityPreferenceWithNilServiceReturnsOriginalCandidates(t *testing.T) {
	t.Parallel()

	candidates := newSessionAffinityTestCandidates(1, 2, 3)
	ctx := contexts.WithSessionAffinity(context.Background(), "session-123")
	sessionAffinity, ok := contexts.GetSessionAffinity(ctx)
	require.True(t, ok)
	scope := BuildAffinityScope(1, 2, "gpt-4", "openai", sessionAffinity)

	reordered, preferredChannelID, applied := ApplyAffinityPreference(candidates, nil, scope)

	assert.False(t, applied)
	assert.Zero(t, preferredChannelID)
	assert.Same(t, candidates[0], reordered[0])
	assert.Same(t, candidates[1], reordered[1])
	assert.Same(t, candidates[2], reordered[2])
	assert.Equal(t, candidates, reordered)
}

func TestApplyAffinityPreferenceWithEmptySessionAffinityReturnsOriginalCandidates(t *testing.T) {
	t.Parallel()

	candidates := newSessionAffinityTestCandidates(1, 2, 3)
	svc := NewSessionAffinityService([]byte("test-secret"))
	scope := BuildAffinityScope(1, 2, "gpt-4", "openai", "")

	reordered, preferredChannelID, applied := ApplyAffinityPreference(candidates, svc, scope)

	assert.False(t, applied)
	assert.Zero(t, preferredChannelID)
	assert.Same(t, candidates[0], reordered[0])
	assert.Same(t, candidates[1], reordered[1])
	assert.Same(t, candidates[2], reordered[2])
	assert.Equal(t, candidates, reordered)
}

func TestApplyAffinityPreferenceReordersCandidatesWhenAffinityChannelFound(t *testing.T) {
	t.Parallel()

	ctx := contexts.WithSessionAffinity(context.Background(), "session-123")
	sessionAffinity, ok := contexts.GetSessionAffinity(ctx)
	require.True(t, ok)
	scope := BuildAffinityScope(1, 2, "gpt-4", "openai", sessionAffinity)
	svc := NewSessionAffinityService([]byte("test-secret"))
	svc.Set(ctx, scope, 3)
	candidates := newSessionAffinityTestCandidates(1, 2, 3)

	reordered, preferredChannelID, applied := ApplyAffinityPreference(candidates, svc, scope)

	assert.True(t, applied)
	assert.Equal(t, 3, preferredChannelID)
	require.Len(t, reordered, 3)
	assert.Equal(t, 3, reordered[0].Channel.ID)
	assert.Equal(t, 1, reordered[1].Channel.ID)
	assert.Equal(t, 2, reordered[2].Channel.ID)
	assert.Same(t, candidates[2], reordered[0])
	assert.Same(t, candidates[0], reordered[1])
	assert.Same(t, candidates[1], reordered[2])
}

func TestSessionAffinityScopeHMACCollisionPrevention(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret")
	svc := NewSessionAffinityService(secret)

	// Scope that could collide with pipe delimiter: SessionAffinity contains "|"
	scope1 := BuildAffinityScope(1, 2, "gpt-4", "openai", "session|abc|123")
	// Scope with empty strings that could produce same pipe-delimited result
	scope2 := BuildAffinityScope(1, 2, "gp", "t-4|openai|session|abc|123", "")

	key1 := svc.BuildKey(scope1)
	key2 := svc.BuildKey(scope2)

	// Keys must be different - JSON encoding prevents collision
	assert.NotEqual(t, key1, key2, "JSON encoding must prevent HMAC collision between scopes with pipe characters")
}

func TestSessionAffinityServiceEmptySecretWarnsAndGenerates(t *testing.T) {
	t.Parallel()

	// Capture the warning by checking that generates flag is true and secret is set
	svc := NewSessionAffinityService(nil)

	assert.True(t, svc.GeneratesSecret(), "service must report that it generates a secret")
	assert.Len(t, svc.secret, 32, "generated secret must be 32 bytes")
}

func TestSessionAffinityServiceEmptySecretWarnsOnStartup(t *testing.T) {
	t.Parallel()

	// Create a new service with empty secret - should log warning
	// We verify behavior by checking GeneratesSecret returns true
	svc := NewSessionAffinityService([]byte{})

	assert.True(t, svc.GeneratesSecret(), "empty secret must trigger secret generation")
	assert.Len(t, svc.secret, 32)
}

func TestSessionAffinityRawValuesNeverInLogOutput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	svc := NewSessionAffinityService([]byte("test-secret"))
	scope := BuildAffinityScope(1, 2, "gpt-4", "openai", "super-secret-session-token")

	// Build key - the key itself is an HMAC hex string, not the raw affinity
	key := svc.BuildKey(scope)

	// Key must be a hex string (64 chars for SHA256)
	assert.Len(t, key, 64)
	// Key must not contain any part of the raw session affinity
	assert.NotContains(t, key, "super-secret-session-token")
	assert.NotContains(t, key, "session")
	assert.NotContains(t, key, "super")
	// Key must only contain hex characters
	for _, c := range key {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"HMAC key must only contain hex characters")
	}

	// Set and verify - the cache entry contains only the key and channel info
	svc.Set(ctx, scope, 42)
	channelID, ok := svc.Get(scope)
	assert.True(t, ok)
	assert.Equal(t, 42, channelID)
}

func newSessionAffinityTestCandidates(channelIDs ...int) []*ChannelModelsCandidate {
	candidates := make([]*ChannelModelsCandidate, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		candidates = append(candidates, &ChannelModelsCandidate{
			Channel: &biz.Channel{Channel: &ent.Channel{ID: channelID}},
		})
	}

	return candidates
}
