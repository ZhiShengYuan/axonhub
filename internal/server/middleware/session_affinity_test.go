package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
)

func TestWithSessionAffinity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		headerValue    string
		expectedOK     bool
		expectedValue  string
	}{
		{
			name:          "valid affinity value",
			headerValue:   "session-abc123",
			expectedOK:    true,
			expectedValue: "session-abc123",
		},
		{
			name:          "valid affinity with whitespace",
			headerValue:   "  session-xyz789  ",
			expectedOK:    true,
			expectedValue: "session-xyz789",
		},
		{
			name:          "empty header",
			headerValue:   "",
			expectedOK:    false,
			expectedValue: "",
		},
		{
			name:          "whitespace only header",
			headerValue:   "   ",
			expectedOK:    false,
			expectedValue: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(WithSessionAffinity())
			router.GET("/test", func(c *gin.Context) {
				affinity, ok := contexts.GetSessionAffinity(c.Request.Context())
				if !ok {
					c.JSON(http.StatusOK, gin.H{"ok": false})
					return
				}
				c.JSON(http.StatusOK, gin.H{"ok": true, "affinity": affinity})
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.headerValue != "" {
				req.Header.Set(SessionAffinityHeader, tt.headerValue)
			}

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)

			require.Equal(t, http.StatusOK, recorder.Code)

			body := recorder.Body.String()
			if tt.expectedOK {
				require.Contains(t, body, `"ok":true`)
				require.Contains(t, body, `"affinity":`+`"`+tt.expectedValue+`"`)
			} else {
				require.Contains(t, body, `"ok":false`)
			}
		})
	}
}