package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNewWithEmptyTrustedProxies(t *testing.T) {
	cfg := Config{
		TrustedProxies: nil,
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("expected no error for empty TrustedProxies, got %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil Server")
	}
}

func TestNewWithTrustedProxies(t *testing.T) {
	cfg := Config{
		TrustedProxies: []string{"127.0.0.1", "::1"},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("expected no error for TrustedProxies, got %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil Server")
	}
}

func TestNewWithInvalidTrustedProxy(t *testing.T) {
	cfg := Config{
		TrustedProxies: []string{"not-an-ip"},
	}

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for invalid TrustedProxies, got nil")
	}
}

func TestNewWithCloudflarePlatform(t *testing.T) {
	cfg := Config{
		TrustedPlatform: gin.PlatformCloudflare,
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("expected no error for Cloudflare TrustedPlatform, got %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil Server")
	}
	if srv.TrustedPlatform != gin.PlatformCloudflare {
		t.Fatalf("expected TrustedPlatform %q, got %q", gin.PlatformCloudflare, srv.TrustedPlatform)
	}
}

func TestNewWithCustomTrustedPlatform(t *testing.T) {
	cfg := Config{
		TrustedPlatform: "X-Custom-IP",
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("expected no error for custom TrustedPlatform, got %v", err)
	}
	if srv == nil {
		t.Fatal("expected non-nil Server")
	}
	if srv.TrustedPlatform != "X-Custom-IP" {
		t.Fatalf("expected TrustedPlatform %q, got %q", "X-Custom-IP", srv.TrustedPlatform)
	}
}

func TestClientIPWithEmptyTrustedProxies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := Config{
		TrustedProxies: nil,
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	srv.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
}

func TestClientIPWithTrustedProxyAndXFF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := Config{
		TrustedProxies: []string{"192.168.1.1"},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	srv.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if w.Body.String() != "10.0.0.1" {
		t.Fatalf("expected client IP 10.0.0.1 from XFF, got %q", w.Body.String())
	}
}

func TestClientIPWithCloudflarePlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := Config{
		TrustedPlatform: gin.PlatformCloudflare,
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	srv.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.50")

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	if w.Body.String() != "203.0.113.50" {
		t.Fatalf("expected client IP 203.0.113.50 from CF-Connecting-IP, got %q", w.Body.String())
	}
}

// TestNewAppliesTrustedProxyConfig verifies that server.New() applies trusted proxy
// configuration to the gin engine. This test FAILS if engine.SetTrustedProxies()
// is removed from the server constructor - ClientIP() would return the direct remote
// address instead of parsing X-Forwarded-For.
func TestNewAppliesTrustedProxyConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := Config{
		TrustedProxies: []string{"192.168.1.100"},
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	srv.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		wantIP     string
	}{
		{
			name:       "parses XFF when trusted proxy configured",
			remoteAddr: "192.168.1.100:12345",
			xff:        "10.0.0.1, 192.168.1.100",
			wantIP:     "10.0.0.1",
		},
		{
			name:       "single XFF value",
			remoteAddr: "192.168.1.100:12345",
			xff:        "10.0.0.5",
			wantIP:     "10.0.0.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ip", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}

			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}

			if w.Body.String() != tt.wantIP {
				t.Errorf("expected client IP %q, got %q (iff %q, remote %q)",
					tt.wantIP, w.Body.String(), tt.xff, tt.remoteAddr)
			}
		})
	}
}

// TestNewAppliesTrustedPlatform verifies that server.New() applies trusted platform
// configuration to the gin engine. This test FAILS if TrustedPlatform assignment
// is removed from the server constructor - CF-Connecting-IP header would not be
// used for client IP determination.
func TestNewAppliesTrustedPlatform(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := Config{
		TrustedPlatform: gin.PlatformCloudflare,
	}

	srv, err := New(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Verify the TrustedPlatform field is set on the engine
	if srv.TrustedPlatform != gin.PlatformCloudflare {
		t.Fatalf("expected TrustedPlatform %q, got %q", gin.PlatformCloudflare, srv.TrustedPlatform)
	}

	srv.GET("/ip", func(c *gin.Context) {
		c.String(http.StatusOK, c.ClientIP())
	})

	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	req.Header.Set("CF-Connecting-IP", "203.0.113.50")

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	// If TrustedPlatform was not set, ClientIP() would return "192.168.1.1" (remote addr)
	// With TrustedPlatform set to Cloudflare, it returns the CF-Connecting-IP header value
	if w.Body.String() != "203.0.113.50" {
		t.Fatalf("expected client IP 203.0.113.50 from CF-Connecting-IP, got %q", w.Body.String())
	}
}