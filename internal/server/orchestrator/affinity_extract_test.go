package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/llm/httpclient"
)

const (
	testSessionUUID = "550e8400-e29b-41d4-a716-446655440000"
	testSessionID   = "_session_" + testSessionUUID
)

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// headersFromMap is a small helper that builds an http.Header from a flat
// map. It canonicalizes keys so tests can pass either "x-session-affinity"
// or "X-Session-Affinity" and still hit the case-insensitive lookup.
func headersFromMap(t *testing.T, m map[string]string) http.Header {
	t.Helper()

	h := http.Header{}
	for k, v := range m {
		h.Set(k, v)
	}

	return h
}

func bodyFromMap(t *testing.T, m map[string]any) []byte {
	t.Helper()

	b, err := json.Marshal(m)
	require.NoError(t, err)

	return b
}

func TestAffinityExtraction_PrimaryHeaderWins(t *testing.T) {
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": "primary-affinity-value",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "X-Session-Affinity", state.Source)
	assert.Equal(t, hashOf("primary-affinity-value"), state.Hash)
	assert.Equal(t, "unknown", state.ModelScope)
}

func TestAffinityExtraction_FallbackExactHeaderOrder(t *testing.T) {
	tests := []struct {
		name        string
		headerName  string
		headerValue string
	}{
		{name: "X-Claude-Code-Session-Id", headerName: "X-Claude-Code-Session-Id", headerValue: "claude-123"},
		{name: "Session_id", headerName: "Session_id", headerValue: "snake-123"},
		{name: "Session-Id", headerName: "Session-Id", headerValue: "dash-123"},
		{name: "X-Litellm-Session-Id", headerName: "X-Litellm-Session-Id", headerValue: "litellm-123"},
		{name: "X-Amp-Thread-Id", headerName: "X-Amp-Thread-Id", headerValue: "amp-123"},
		{name: "X-Session-Id", headerName: "X-Session-Id", headerValue: "session-123"},
		{name: "X-Openai-Session-Id", headerName: "X-Openai-Session-Id", headerValue: "openai-123"},
		{name: "X-Task-ID", headerName: "X-Task-ID", headerValue: "task-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &httpclient.Request{
				Headers: headersFromMap(t, map[string]string{
					tt.headerName: tt.headerValue,
				}),
			}

			state, err := ExtractAffinity(t.Context(), req)
			require.NoError(t, err)
			require.NotNil(t, state)
			assert.Equal(t, tt.headerName, state.Source)
			assert.Equal(t, hashOf(tt.headerValue), state.Hash)
		})
	}
}

func TestAffinityExtraction_PrimaryBeatsFallback(t *testing.T) {
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity":       "primary",
			"X-Session-Id":             "fallback",
			"X-Claude-Code-Session-Id": "fallback-2",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "X-Session-Affinity", state.Source)
	assert.Equal(t, hashOf("primary"), state.Hash)
}

func TestAffinityExtraction_DenylistIgnored(t *testing.T) {
	// None of these headers should ever produce an affinity state, even
	// when their values look like session identifiers.
	denylisted := []string{
		"X-Request-Id",
		"X-Correlation-Id",
		"Traceparent",
		"X-Trace-Id",
		"X-Litellm-Trace-Id",
		"X-Client-Request-Id",
		"Idempotency-Key",
		"Authorization",
		"Cookie",
		"User-Agent",
		"X-Opencode-Request",
		"X-Opencode-Project",
		"X-Opencode-Client",
		"X-Claude-Code-Agent-Id",
		"X-Claude-Code-Parent-Agent-Id",
		"Openai-Organization",
		"Openai-Project",
		"Openai-Beta",
		"Anthropic-Version",
		"Anthropic-Beta",
		"Mcp-Session-Id",
	}

	for _, name := range denylisted {
		t.Run(name, func(t *testing.T) {
			req := &httpclient.Request{
				Headers: headersFromMap(t, map[string]string{
					name: "some-value",
				}),
			}

			state, err := ExtractAffinity(t.Context(), req)
			require.NoError(t, err)
			assert.Nil(t, state, "denylisted header %q must not produce affinity", name)
		})
	}
}

func TestAffinityExtraction_RegexFallback(t *testing.T) {
	tests := []struct {
		name        string
		headerName  string
		headerValue string
	}{
		{name: "x-foo-session-id", headerName: "X-Foo-Session-Id", headerValue: "regex-1"},
		{name: "x-bar-thread-id", headerName: "X-Bar-Thread-Id", headerValue: "regex-2"},
		{name: "x-baz-conversation-id", headerName: "X-Baz-Conversation-Id", headerValue: "regex-3"},
		{name: "canonical-lowercase", headerName: http.CanonicalHeaderKey("x-custom-session-id"), headerValue: "regex-4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &httpclient.Request{
				Headers: headersFromMap(t, map[string]string{
					tt.headerName: tt.headerValue,
				}),
			}

			state, err := ExtractAffinity(t.Context(), req)
			require.NoError(t, err)
			require.NotNil(t, state)
			assert.Equal(t, tt.headerName, state.Source)
			assert.Equal(t, hashOf(tt.headerValue), state.Hash)
		})
	}
}

func TestAffinityExtraction_RegexOrderPrecedence(t *testing.T) {
	// session-id is the first regex pattern, so it should win over
	// thread-id and conversation-id when all three are present.
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Z-Session-Id":      "session-val",
			"X-Z-Thread-Id":       "thread-val",
			"X-Z-Conversation-Id": "conv-val",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "X-Z-Session-Id", state.Source)
}

func TestAffinityExtraction_BodyFallback(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		expected string
		source   string
	}{
		{
			name: "metadata.user_id with _session_<uuid>",
			body: map[string]any{
				"model": "claude-3-5-sonnet",
				"metadata": map[string]any{
					"user_id": testSessionID,
				},
			},
			expected: testSessionID,
			source:   "metadata.user_id",
		},
		{
			name: "metadata.session_id",
			body: map[string]any{
				"model": "claude-3-5-sonnet",
				"metadata": map[string]any{
					"session_id": "meta-session",
				},
			},
			expected: "meta-session",
			source:   "metadata.session_id",
		},
		{
			name: "prompt_cache_key",
			body: map[string]any{
				"model":            "claude-3-5-sonnet",
				"prompt_cache_key": "cache-key",
			},
			expected: "cache-key",
			source:   "prompt_cache_key",
		},
		{
			name: "conversation_id",
			body: map[string]any{
				"model":           "claude-3-5-sonnet",
				"conversation_id": "conv-1",
			},
			expected: "conv-1",
			source:   "conversation_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &httpclient.Request{
				Body: bodyFromMap(t, tt.body),
			}

			state, err := ExtractAffinity(t.Context(), req)
			require.NoError(t, err)
			require.NotNil(t, state)
			assert.Equal(t, tt.source, state.Source)
			assert.Equal(t, hashOf(tt.expected), state.Hash)
		})
	}
}

func TestAffinityExtraction_BodyFieldPrecedence(t *testing.T) {
	// When multiple body fields are present, the order is:
	// metadata.user_id (if matches pattern) > metadata.session_id >
	// prompt_cache_key > conversation_id.
	req := &httpclient.Request{
		Body: bodyFromMap(t, map[string]any{
			"metadata": map[string]any{
				"user_id":    testSessionID,
				"session_id": "should-not-be-used",
			},
			"prompt_cache_key": "should-not-be-used",
			"conversation_id":  "should-not-be-used",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "metadata.user_id", state.Source)
}

func TestAffinityExtraction_MetadataUserIDNonSessionIgnored(t *testing.T) {
	// user_id that does NOT match the _session_<uuid> pattern is
	// ignored — fall through to next field.
	req := &httpclient.Request{
		Body: bodyFromMap(t, map[string]any{
			"metadata": map[string]any{
				"user_id": "user-12345",
			},
			"prompt_cache_key": "cache-key",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "prompt_cache_key", state.Source)
}

func TestAffinityExtraction_NoSignal(t *testing.T) {
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Request-Id": "abc-123",
		}),
		Body: bodyFromMap(t, map[string]any{
			"model": "claude-3-5-sonnet",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	assert.Nil(t, state)
}

func TestAffinityExtraction_NilRequest(t *testing.T) {
	state, err := ExtractAffinity(t.Context(), nil)
	require.NoError(t, err)
	assert.Nil(t, state)
}

func TestAffinityExtraction_EmptyValueIgnored(t *testing.T) {
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": "",
		}),
		Body: bodyFromMap(t, map[string]any{
			"prompt_cache_key": "fallback",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "prompt_cache_key", state.Source)
}

func TestAffinityExtraction_WhitespaceValueIgnored(t *testing.T) {
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": "   ",
		}),
		Body: bodyFromMap(t, map[string]any{
			"prompt_cache_key": "fallback",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "prompt_cache_key", state.Source)
}

func TestAffinityExtraction_4096ByteCap(t *testing.T) {
	// 4097-byte value must be ignored; the next-lower-priority source wins.
	tooLong := strings.Repeat("a", 4097)

	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": tooLong,
		}),
		Body: bodyFromMap(t, map[string]any{
			"prompt_cache_key": "fallback",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "prompt_cache_key", state.Source)

	// 4096-byte value (exact cap) is accepted.
	exactCap := strings.Repeat("a", 4096)
	req = &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": exactCap,
		}),
	}

	state, err = ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "X-Session-Affinity", state.Source)
	assert.Equal(t, hashOf(exactCap), state.Hash)
}

func TestAffinityExtraction_ModelScope(t *testing.T) {
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": "session-1",
		}),
		Body: bodyFromMap(t, map[string]any{
			"model": "gpt-4o",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "gpt-4o", state.ModelScope)
}

func TestAffinityExtraction_ModelScopeUnknown(t *testing.T) {
	tests := []struct {
		name string
		body []byte
	}{
		{name: "empty body", body: nil},
		{name: "non-JSON body", body: []byte("not json")},
		{name: "JSON without model", body: bodyFromMap(t, map[string]any{"messages": []any{}})},
		{name: "JSON with empty model", body: bodyFromMap(t, map[string]any{"model": "  "})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &httpclient.Request{
				Headers: headersFromMap(t, map[string]string{
					"X-Session-Affinity": "session-1",
				}),
				Body: tt.body,
			}

			state, err := ExtractAffinity(t.Context(), req)
			require.NoError(t, err)
			require.NotNil(t, state)
			assert.Equal(t, "unknown", state.ModelScope)
		})
	}
}

func TestAffinityExtraction_RawValuesNotInState(t *testing.T) {
	raw := "super-secret-affinity-value"
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": raw,
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)

	// Hash must be SHA256 of the raw value, NOT the raw value itself.
	assert.NotEqual(t, raw, state.Hash)
	assert.Equal(t, hashOf(raw), state.Hash)
}

func TestAffinityExtraction_HeaderBeatsBody(t *testing.T) {
	// A header signal should always win over a body signal.
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": "from-header",
		}),
		Body: bodyFromMap(t, map[string]any{
			"prompt_cache_key": "from-body",
		}),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "X-Session-Affinity", state.Source)
	assert.Equal(t, hashOf("from-header"), state.Hash)
}

func TestAffinityExtraction_HeaderCaseInsensitive(t *testing.T) {
	// http.Header.Get is canonical-form-insensitive.
	req := &httpclient.Request{
		Headers: http.Header{
			http.CanonicalHeaderKey("x-session-affinity"): []string{"lower-value"},
		},
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, hashOf("lower-value"), state.Hash)
}

func TestAffinityContext_StoreAndRetrieve(t *testing.T) {
	state := &contexts.AffinityState{
		Hash:       "abc123",
		Source:     "X-Session-Affinity",
		ModelScope: "gpt-4o",
	}

	ctx := contexts.WithAffinityState(t.Context(), state)
	got, ok := contexts.GetAffinityState(ctx)
	require.True(t, ok)
	require.NotNil(t, got)
	assert.Equal(t, "abc123", got.Hash)
	assert.Equal(t, "X-Session-Affinity", got.Source)
	assert.Equal(t, "gpt-4o", got.ModelScope)
}

func TestAffinityContext_RetrieveMissing(t *testing.T) {
	got, ok := contexts.GetAffinityState(t.Context())
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestAffinityContext_NilStateIgnored(t *testing.T) {
	// WithAffinityState must be a no-op when state is nil; the
	// returned context should not store any affinity state.
	ctx := contexts.WithAffinityState(t.Context(), nil)

	got, ok := contexts.GetAffinityState(ctx)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestAffinityExtraction_BodyInvalidJSONSkipped(t *testing.T) {
	req := &httpclient.Request{
		Headers: headersFromMap(t, map[string]string{
			"X-Session-Affinity": "header-wins",
		}),
		Body: []byte("{not json"),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, "X-Session-Affinity", state.Source)
}

func TestAffinityExtraction_BodyInvalidJSONNoSignal(t *testing.T) {
	req := &httpclient.Request{
		Body: []byte("{not json"),
	}

	state, err := ExtractAffinity(t.Context(), req)
	require.NoError(t, err)
	assert.Nil(t, state)
}
