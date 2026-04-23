package db

import (
	"testing"
)

func TestNewEntClient_InvalidDialect(t *testing.T) {
	cfg := Config{
		Dialect: "unsupported",
		DSN:     "",
	}
	client, err := NewEntClient(cfg)
	if client != nil {
		t.Errorf("expected nil client for invalid dialect, got %v", client)
	}
	if err == nil {
		t.Error("expected error for invalid dialect, got nil")
	}
	if err != nil {
		expectedMsg := "invalid database dialect: unsupported"
		if err.Error() != expectedMsg {
			t.Errorf("expected error %q, got %q", expectedMsg, err.Error())
		}
	}
}
