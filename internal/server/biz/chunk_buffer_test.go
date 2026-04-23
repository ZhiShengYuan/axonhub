package biz

import (
	"testing"
	"time"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/stretchr/testify/assert"
)

func TestChunkBuffer_Append(t *testing.T) {
	buffer := NewChunkBuffer()

	// Append chunks
	chunk1 := &httpclient.StreamEvent{Type: "test", Data: []byte("data1")}
	chunk2 := &httpclient.StreamEvent{Type: "test", Data: []byte("data2")}

	assert.True(t, buffer.Append(chunk1))
	assert.True(t, buffer.Append(chunk2))
	assert.Equal(t, 2, buffer.Len())

	// Nil chunk should be ignored
	assert.False(t, buffer.Append(nil))
	assert.Equal(t, 2, buffer.Len())
}

func TestChunkBuffer_Slice(t *testing.T) {
	buffer := NewChunkBuffer()

	chunk1 := &httpclient.StreamEvent{Type: "test", Data: []byte("data1")}
	chunk2 := &httpclient.StreamEvent{Type: "test", Data: []byte("data2")}

	buffer.Append(chunk1)
	buffer.Append(chunk2)

	slice := buffer.Slice()
	assert.Len(t, slice, 2)
	assert.Equal(t, chunk1, slice[0])
	assert.Equal(t, chunk2, slice[1])

	// Verify it's a copy
	slice[0] = nil
	assert.NotNil(t, buffer.Slice()[0])
}

func TestChunkBuffer_Close(t *testing.T) {
	buffer := NewChunkBuffer()

	assert.False(t, buffer.IsClosed())

	chunk := &httpclient.StreamEvent{Type: "test", Data: []byte("data")}
	assert.True(t, buffer.Append(chunk))

	buffer.Close()
	assert.True(t, buffer.IsClosed())

	// Appends should fail after close
	assert.False(t, buffer.Append(&httpclient.StreamEvent{Type: "test2", Data: []byte("data2")}))
	assert.Equal(t, 1, buffer.Len())
}

func TestChunkBuffer_ConcurrentAccess(t *testing.T) {
	buffer := NewChunkBuffer()

	// Simulate concurrent appends
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(n int) {
			chunk := &httpclient.StreamEvent{
				Type: "test",
				Data: []byte{byte(n)},
			}
			buffer.Append(chunk)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 100; i++ {
		<-done
	}

	assert.Equal(t, 100, buffer.Len())
}

func TestChunkBuffer_SnapshotLen(t *testing.T) {
	buffer := NewChunkBuffer()

	assert.Equal(t, 0, buffer.SnapshotLen())

	chunk := &httpclient.StreamEvent{Type: "test", Data: []byte("data")}
	buffer.Append(chunk)

	assert.Equal(t, 1, buffer.SnapshotLen())
}

func TestChunkBuffer_ChunksPointer(t *testing.T) {
	buffer := NewChunkBuffer()

	chunk := &httpclient.StreamEvent{Type: "test", Data: []byte("data")}
	buffer.Append(chunk)

	ptr := buffer.ChunksPointer()
	assert.NotNil(t, ptr)
	assert.Len(t, *ptr, 1)
}

// Regression tests for Task 3: Preview buffer independence from downstream commit state
// These tests verify that ChunkBuffer behavior is completely decoupled from
// any retry-gate or downstream commit concepts introduced in Task 2.

// TestChunkBuffer_Subscribe_Notifications_Independent_Of_Commit proves that
// subscriber notifications fire on chunk append/close operations,
// NOT on any downstream commit or forwarding state.
func TestChunkBuffer_Subscribe_Notifications_Independent_Of_Commit(t *testing.T) {
	buffer := NewChunkBuffer()

	// Subscribe to buffer changes
	notifyCh, unsubscribe := buffer.Subscribe()
	defer unsubscribe()

	// Append a chunk - subscriber should receive notification
	chunk1 := &httpclient.StreamEvent{Type: "test", Data: []byte("data1")}
	buffer.Append(chunk1)
	select {
	case <-notifyCh:
		// Expected: notification received
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected notification on append, but got none")
	}

	// Append another chunk - another notification
	chunk2 := &httpclient.StreamEvent{Type: "test", Data: []byte("data2")}
	buffer.Append(chunk2)
	select {
	case <-notifyCh:
		// Expected: notification received
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected notification on second append, but got none")
	}

	// Close the buffer - final notification
	buffer.Close()
	select {
	case <-notifyCh:
		// Expected: notification received on close
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected notification on close, but got none")
	}

	// Verify buffer state
	assert.Equal(t, 2, buffer.Len())
	assert.True(t, buffer.IsClosed())
}

// TestChunkBuffer_MaxCapacity verifies capacity limits are preserved.
func TestChunkBuffer_MaxCapacity(t *testing.T) {
	buffer := NewChunkBuffer()

	// Fill buffer to capacity (50,000 chunks)
	for i := 0; i < maxChunkCapacity; i++ {
		chunk := &httpclient.StreamEvent{Type: "test", Data: []byte("data")}
		assert.True(t, buffer.Append(chunk), "Append should succeed for chunk %d", i)
	}

	// Verify capacity reached
	assert.Equal(t, maxChunkCapacity, buffer.Len())

	// Additional appends should be rejected
	rejected := &httpclient.StreamEvent{Type: "test", Data: []byte("overflow")}
	assert.False(t, buffer.Append(rejected), "Append should be rejected at capacity")
	assert.Equal(t, maxChunkCapacity, buffer.Len())
}

// TestChunkBuffer_Subscribe_Unsubscribe verifies clean subscription lifecycle.
func TestChunkBuffer_Subscribe_Unsubscribe(t *testing.T) {
	buffer := NewChunkBuffer()

	notifyCh1, unsub1 := buffer.Subscribe()
	notifyCh2, unsub2 := buffer.Subscribe()

	// Both subscribers should receive notification
	chunk := &httpclient.StreamEvent{Type: "test", Data: []byte("data")}
	buffer.Append(chunk)

	select {
	case <-notifyCh1:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected notification on subscriber 1")
	}

	select {
	case <-notifyCh2:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected notification on subscriber 2")
	}

	// Unsubscribe one
	unsub1()

	// Append again - only remaining subscriber should get notification
	buffer.Append(chunk)

	select {
	case <-notifyCh1:
		t.Fatal("Unsubscribed channel should not receive notifications")
	case <-time.After(50 * time.Millisecond):
		// Expected: no notification on unsubscribed channel
	}

	select {
	case <-notifyCh2:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected notification on active subscriber")
	}

	unsub2()

	// Final append - no crashes even with no subscribers
	buffer.Append(chunk)
	assert.Equal(t, 3, buffer.Len())
}

// TestChunkBuffer_Slice_ReturnsCopy proves Slice() returns independent copy.
func TestChunkBuffer_Slice_ReturnsCopy(t *testing.T) {
	buffer := NewChunkBuffer()

	chunk1 := &httpclient.StreamEvent{Type: "test", Data: []byte("data1")}
	buffer.Append(chunk1)

	slice1 := buffer.Slice()
	slice1[0] = nil // Modify the copy

	slice2 := buffer.Slice()
	assert.NotNil(t, slice2[0], "Original buffer should be unaffected by copy modification")
}

// TestChunkBuffer_ClosePreventsFurtherAppends proves close is final.
func TestChunkBuffer_ClosePreventsFurtherAppends(t *testing.T) {
	buffer := NewChunkBuffer()

	chunk1 := &httpclient.StreamEvent{Type: "test", Data: []byte("data1")}
	assert.True(t, buffer.Append(chunk1))

	buffer.Close()

	// All subsequent appends rejected
	assert.False(t, buffer.Append(&httpclient.StreamEvent{Type: "test2", Data: []byte("data2")}))
	assert.False(t, buffer.Append(nil))
	assert.Equal(t, 1, buffer.Len())
	assert.True(t, buffer.IsClosed())
}

// TestStreamPreviewRegistry_RegisterBuffer_And_GetBuffer tests the preview registry pattern.
// This verifies the decoupling: preview registry simply holds buffer references,
// it has no awareness of retry-gate or downstream commit state.
func TestStreamPreviewRegistry_RegisterBuffer_And_GetBuffer(t *testing.T) {
	registry := NewStreamPreviewRegistry()

	buffer := NewChunkBuffer()
	key := "request:123"

	// Register the buffer
	registry.RegisterBuffer(key, buffer)

	// Retrieve the buffer
	retrieved := registry.GetBuffer(key)
	assert.Equal(t, buffer, retrieved)

	// Append to buffer - registry still holds same reference
	chunk := &httpclient.StreamEvent{Type: "test", Data: []byte("data")}
	buffer.Append(chunk)
	assert.Equal(t, 1, retrieved.Len())

	// Unregister
	registry.Unregister(key)
	assert.Nil(t, registry.GetBuffer(key))
}

// TestStreamPreviewRegistry_BufferIndependence proves buffer operations
// don't depend on registry state.
func TestStreamPreviewRegistry_BufferIndependence(t *testing.T) {
	registry := NewStreamPreviewRegistry()

	buffer := NewChunkBuffer()
	key := "request:456"

	// Register buffer
	registry.RegisterBuffer(key, buffer)

	// Verify empty buffer via GetBuffer
	assert.NotNil(t, registry.GetBuffer(key), "GetBuffer should return buffer even when empty")
	assert.Equal(t, 0, registry.GetBuffer(key).Len())

	// Append some chunks
	chunk1 := &httpclient.StreamEvent{Type: "test", Data: []byte("data1")}
	chunk2 := &httpclient.StreamEvent{Type: "test", Data: []byte("data2")}
	buffer.Append(chunk1)
	buffer.Append(chunk2)

	// Buffer state reflects appends via direct access
	assert.Equal(t, 2, buffer.Len())

	// Registry has no concept of "commit" - buffer is always accessible
	// until explicitly unregistered or closed
	assert.False(t, buffer.IsClosed())

	// Closing buffer doesn't unregister it automatically
	buffer.Close()
	assert.True(t, buffer.IsClosed())
	assert.NotNil(t, registry.GetBuffer(key), "Close doesn't unregister from registry")

	// Unregister explicitly
	registry.Unregister(key)
	assert.Nil(t, registry.GetBuffer(key))
}
