package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestWithSessionAffinityID(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		headers         map[string]string
		existingSession string
		expectedSession string
		expectSet       bool
	}{
		{
			name:            "X-Session-ID header",
			headers:         map[string]string{"X-Session-ID": "session-abc-123"},
			expectedSession: "session-abc-123",
			expectSet:       true,
		},
		{
			name:            "X-Session-Affinity header",
			headers:         map[string]string{"X-Session-Affinity": "session-xyz-789"},
			expectedSession: "session-xyz-789",
			expectSet:       true,
		},
		{
			name:            "x-claude-code-session-id header",
			headers:         map[string]string{"x-claude-code-session-id": "claude-session-456"},
			expectedSession: "claude-session-456",
			expectSet:       true,
		},
		{
			name:            "session-id header",
			headers:         map[string]string{"session-id": "generic-session-001"},
			expectedSession: "generic-session-001",
			expectSet:       true,
		},
		{
			name:            "x-conversation-id header",
			headers:         map[string]string{"x-conversation-id": "conv-789"},
			expectedSession: "conv-789",
			expectSet:       true,
		},
		{
			name:            "x-thread-id header",
			headers:         map[string]string{"x-thread-id": "thread-321"},
			expectedSession: "thread-321",
			expectSet:       true,
		},
		{
			name:            "precedence: X-Session-ID wins over X-Session-Affinity",
			headers:         map[string]string{"X-Session-ID": "first", "X-Session-Affinity": "second"},
			expectedSession: "first",
			expectSet:       true,
		},
		{
			name:            "precedence: first non-empty wins over lower-priority",
			headers:         map[string]string{"X-Session-Affinity": "second-wins", "x-claude-code-session-id": "third"},
			expectedSession: "second-wins",
			expectSet:       true,
		},
		{
			name:            "blank/whitespace headers are skipped",
			headers:         map[string]string{"X-Session-ID": "   ", "X-Session-Affinity": "\t\n", "session-id": "valid"},
			expectedSession: "valid",
			expectSet:       true,
		},
		{
			name:      "no matching header leaves context unset",
			headers:   map[string]string{},
			expectSet: false,
		},
		{
			name:            "existing context session ID is not overwritten",
			existingSession: "existing-id",
			headers:         map[string]string{"X-Session-ID": "new-id"},
			expectedSession: "existing-id",
			expectSet:       true,
		},
		{
			name:            "header names are case-insensitive via Go net/http",
			headers:         map[string]string{"x-session-id": "lowercase-works"},
			expectedSession: "lowercase-works",
			expectSet:       true,
		},
		{
			name:      "non-listed header Session_id is not extracted",
			headers:   map[string]string{"Session_id": "codex-session"},
			expectSet: false,
		},
		{
			name:            "all headers present: highest priority wins",
			headers:         map[string]string{"X-Session-ID": "p1", "X-Session-Affinity": "p2", "x-claude-code-session-id": "p3", "session-id": "p4", "x-conversation-id": "p5", "x-thread-id": "p6"},
			expectedSession: "p1",
			expectSet:       true,
		},
		{
			name:            "lower-priority skipped when higher-priority is valid",
			headers:         map[string]string{"x-thread-id": "last", "x-conversation-id": "fifth"},
			expectedSession: "fifth",
			expectSet:       true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(WithSessionAffinityID())

			var capturedSession string
			var capturedOK bool

			router.GET("/test", func(c *gin.Context) {
				capturedSession, capturedOK = shared.GetSessionID(c.Request.Context())
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}

			if tc.existingSession != "" {
				ctx := shared.WithSessionID(req.Context(), tc.existingSession)
				req = req.WithContext(ctx)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if tc.expectSet {
				require.True(t, capturedOK, "expected session ID to be set in context")
				require.Equal(t, tc.expectedSession, capturedSession)
			} else {
				require.False(t, capturedOK, "expected session ID to NOT be set in context")
			}
		})
	}
}
