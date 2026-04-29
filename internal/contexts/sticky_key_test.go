package contexts

import (
	"context"
	"testing"
)

func TestWithStickyKey(t *testing.T) {
	ctx := t.Context()
	stickyKey := "sticky-value-123"

	// Test storing sticky key
	newCtx := WithStickyKey(ctx, stickyKey)
	if newCtx == ctx {
		t.Error("WithStickyKey should return a new context")
	}

	// Test retrieving sticky key
	retrievedStickyKey, ok := GetStickyKey(newCtx)
	if !ok {
		t.Error("GetStickyKey should return true for existing sticky key")
	}

	if retrievedStickyKey != stickyKey {
		t.Errorf("expected sticky key %s, got %s", stickyKey, retrievedStickyKey)
	}
}

func TestGetStickyKey(t *testing.T) {
	ctx := t.Context()

	// Test retrieving sticky key from empty context
	stickyKey, ok := GetStickyKey(ctx)
	if ok {
		t.Error("GetStickyKey should return false for empty context")
	}

	if stickyKey != "" {
		t.Error("GetStickyKey should return empty string for empty context")
	}

	// Test retrieving sticky key from context with other values
	ctxWithOtherValue := context.WithValue(ctx, "other_key", "other_value")

	stickyKey, ok = GetStickyKey(ctxWithOtherValue)
	if ok {
		t.Error("GetStickyKey should return false for context without sticky key")
	}

	if stickyKey != "" {
		t.Error("GetStickyKey should return empty string for context without sticky key")
	}
}

func TestStickyKeyOverwrite(t *testing.T) {
	ctx := t.Context()

	// Test overwriting sticky key
	ctx = WithStickyKey(ctx, "first-value")
	ctx = WithStickyKey(ctx, "second-value")

	stickyKey, ok := GetStickyKey(ctx)
	if !ok {
		t.Error("GetStickyKey should return true after overwrite")
	}

	if stickyKey != "second-value" {
		t.Errorf("expected sticky key 'second-value', got '%s'", stickyKey)
	}
}

func TestStickyKeyIsolation(t *testing.T) {
	ctx := t.Context()

	// Create a context with sticky key
	ctx1 := WithStickyKey(ctx, "value-1")

	// Create another context with different sticky key
	ctx2 := WithStickyKey(ctx, "value-2")

	// Test that the two contexts are isolated from each other
	stickyKey1, ok1 := GetStickyKey(ctx1)
	stickyKey2, ok2 := GetStickyKey(ctx2)

	if !ok1 || !ok2 {
		t.Error("Both contexts should have sticky keys")
	}

	if stickyKey1 == stickyKey2 {
		t.Error("Sticky keys should be different")
	}

	if stickyKey1 != "value-1" {
		t.Errorf("expected 'value-1', got '%s'", stickyKey1)
	}

	if stickyKey2 != "value-2" {
		t.Errorf("expected 'value-2', got '%s'", stickyKey2)
	}
}

func TestStickyKeyMultipleValues(t *testing.T) {
	ctx := t.Context()

	// Test storing sticky key alongside other context values
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithRequestID(ctx, "req-456")
	ctx = WithStickyKey(ctx, "my-sticky-key")
	ctx = WithOperationName(ctx, "test.operation")

	// Test retrieving all values
	traceID, ok := GetTraceID(ctx)
	if !ok || traceID != "trace-123" {
		t.Error("Trace ID should be stored and retrievable")
	}

	requestID, ok := GetRequestID(ctx)
	if !ok || requestID != "req-456" {
		t.Error("Request ID should be stored and retrievable")
	}

	stickyKey, ok := GetStickyKey(ctx)
	if !ok || stickyKey != "my-sticky-key" {
		t.Error("Sticky key should be stored and retrievable")
	}

	operationName, ok := GetOperationName(ctx)
	if !ok || operationName != "test.operation" {
		t.Error("Operation name should be stored and retrievable")
	}
}