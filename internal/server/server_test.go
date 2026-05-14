package server

import (
	"testing"
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