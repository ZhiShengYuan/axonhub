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
		name          string
		headerValue   string
		expectedOK    bool
		expectedValue string
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

func TestWithSessionAffinityLongHeaderRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	longValue := make([]byte, MaxAffinityHeaderLength+1)
	for i := range longValue {
		longValue[i] = 'a'
	}
	longValueStr := string(longValue)

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
	req.Header.Set(SessionAffinityHeader, longValueStr)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"ok":false`)
}

func TestWithSessionAffinityBoundary256(t *testing.T) {
	gin.SetMode(gin.TestMode)

	exactValue := make([]byte, MaxAffinityHeaderLength)
	for i := range exactValue {
		exactValue[i] = 'a'
	}
	exactValueStr := string(exactValue)

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
	req.Header.Set(SessionAffinityHeader, exactValueStr)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"ok":true`)
}

func TestWithSessionAffinityRawValuesNotForwarded(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(WithSessionAffinity())
	router.GET("/test", func(c *gin.Context) {
		affinity, ok := contexts.GetSessionAffinity(c.Request.Context())
		if !ok {
			c.JSON(http.StatusOK, gin.H{"ok": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"ok":      true,
			"has_raw": len(affinity) > 0,
			"len":     len(affinity),
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(SessionAffinityHeader, "session-secret-abc")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.Contains(t, body, `"ok":true`)
	require.NotContains(t, body, "session-secret-abc")
}