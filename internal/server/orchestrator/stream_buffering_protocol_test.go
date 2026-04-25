package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

// TestStreamBufferingProtocol_OpenAI_EventOrdering verifies that OpenAI streaming events
// maintain proper ordering through the buffer flush. OpenAI streaming expects:
// 1. role (assistant) chunk first
// 2. content delta chunks
// 3. finish_reason chunk last
func TestStreamBufferingProtocol_OpenAI_EventOrdering(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream:         streamTrue,
			FirstTokenTime: func() *time.Time { now := time.Now(); return &now }(),
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 5,
			TimerDuration:  100 * time.Millisecond,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	// Simulate OpenAI streaming chunks in proper order:
	// 1. role chunk (first)
	// 2. content delta chunks
	// 3. finish_reason chunk (last)
	openAIChunks := []*llm.Response{
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Role: "assistant",
					},
				},
			},
		},
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr("Hello"),
						},
					},
				},
			},
		},
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr(" world"),
						},
					},
				},
			},
		},
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index:        0,
					Delta:        &llm.Message{},
					FinishReason: strPtr("stop"),
				},
			},
		},
	}

	originalStream := &mockLlmStream{chunks: openAIChunks}
	wrapped, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Collect all chunks from wrapped stream
	received := make([]*llm.Response, 0)
	for wrapped.Next() {
		received = append(received, wrapped.Current())
	}
	require.NoError(t, wrapped.Err())

	// Verify we got all chunks
	require.Equal(t, len(openAIChunks), len(received), "Should receive all chunks")

	// Verify FIFO ordering is preserved - first chunk should be role chunk
	assert.Equal(t, "assistant", received[0].Choices[0].Delta.Role,
		"First chunk after buffer flush should be role chunk")

	// Verify content chunks order
	assert.Equal(t, "Hello", *received[1].Choices[0].Delta.Content.Content,
		"Second chunk should be first content delta")
	assert.Equal(t, " world", *received[2].Choices[0].Delta.Content.Content,
		"Third chunk should be second content delta")

	// Verify finish_reason is last
	assert.Equal(t, "stop", *received[3].Choices[0].FinishReason,
		"Last chunk should be finish_reason")
}

// TestStreamBufferingProtocol_OpenAI_ToolCallOrdering verifies that OpenAI tool call
// events maintain proper ordering through buffer flush.
func TestStreamBufferingProtocol_OpenAI_ToolCallOrdering(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream:         streamTrue,
			FirstTokenTime: func() *time.Time { now := time.Now(); return &now }(),
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 3,
			TimerDuration:  100 * time.Millisecond,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	// Simulate OpenAI streaming with tool calls in proper order:
	// 1. role chunk
	// 2. tool_call delta chunks
	// 3. finish_reason chunk
	openAIChunks := []*llm.Response{
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Role: "assistant",
					},
				},
			},
		},
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						ToolCalls: []llm.ToolCall{
							{
								Index: 0,
								ID:    "call_123",
								Type:  "function",
								Function: llm.FunctionCall{
									Name:      "get_weather",
									Arguments: "",
								},
							},
						},
					},
				},
			},
		},
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						ToolCalls: []llm.ToolCall{
							{
								Index: 0,
								Function: llm.FunctionCall{
									Arguments: `{"location`,
								},
							},
						},
					},
				},
			},
		},
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						ToolCalls: []llm.ToolCall{
							{
								Index: 0,
								Function: llm.FunctionCall{
									Arguments: `:"London"}`,
								},
							},
						},
					},
				},
			},
		},
		{
			ID:     "chatcmpl-123",
			Object: "chat.completion.chunk",
			Model:  "gpt-4o",
			Choices: []llm.Choice{
				{
					Index:        0,
					Delta:        &llm.Message{},
					FinishReason: strPtr("tool_calls"),
				},
			},
		},
	}

	originalStream := &mockLlmStream{chunks: openAIChunks}
	wrapped, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Collect all chunks
	received := make([]*llm.Response, 0)
	for wrapped.Next() {
		received = append(received, wrapped.Current())
	}
	require.NoError(t, wrapped.Err())

	require.Equal(t, len(openAIChunks), len(received), "Should receive all chunks")

	// Verify role is first
	assert.Equal(t, "assistant", received[0].Choices[0].Delta.Role,
		"First chunk should be role")

	// Verify tool call ordering is preserved
	assert.Equal(t, "get_weather", received[1].Choices[0].Delta.ToolCalls[0].Function.Name,
		"Tool call name should come before arguments")
	assert.Equal(t, "", received[1].Choices[0].Delta.ToolCalls[0].Function.Arguments,
		"First tool call chunk should have empty arguments")

	// Verify tool arguments are accumulated in order
	assert.Equal(t, `{"location`, received[2].Choices[0].Delta.ToolCalls[0].Function.Arguments,
		"Second tool call chunk should have partial arguments")
	assert.Equal(t, `:"London"}`, received[3].Choices[0].Delta.ToolCalls[0].Function.Arguments,
		"Third tool call chunk should have remaining arguments")

	// Verify finish_reason is last
	assert.Equal(t, "tool_calls", *received[4].Choices[0].FinishReason,
		"Last chunk should be finish_reason with tool_calls")
}

// TestStreamBufferingProtocol_Anthropic_EventOrdering verifies that Anthropic streaming
// events maintain proper ordering through buffer flush. Anthropic streaming expects:
// 1. message_start first
// 2. content_block_start
// 3. content_block_delta
// 4. content_block_stop
// 5. message_delta
// 6. message_stop last
func TestStreamBufferingProtocol_Anthropic_EventOrdering(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream:         streamTrue,
			FirstTokenTime: func() *time.Time { now := time.Now(); return &now }(),
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 5,
			TimerDuration:  100 * time.Millisecond,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	// Simulate Anthropic streaming chunks in proper order:
	// 1. message_start
	// 2. content_block_start
	// 3. content_block_delta (text)
	// 4. content_block_delta (text)
	// 5. content_block_stop
	// 6. message_delta
	// 7. message_stop
	anthropicChunks := []*llm.Response{
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Role: "assistant",
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr(""), // content_block_start equivalent
						},
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr("Hello"),
						},
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr(" world"),
						},
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index:        0,
					Delta:        &llm.Message{},
					FinishReason: strPtr("end_turn"),
				},
			},
		},
	}

	originalStream := &mockLlmStream{chunks: anthropicChunks}
	wrapped, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Collect all chunks
	received := make([]*llm.Response, 0)
	for wrapped.Next() {
		received = append(received, wrapped.Current())
	}
	require.NoError(t, wrapped.Err())

	require.Equal(t, len(anthropicChunks), len(received), "Should receive all chunks")

	// Verify message_start (role) is first
	assert.Equal(t, "assistant", received[0].Choices[0].Delta.Role,
		"First chunk should be message_start (role)")

	// Verify content deltas are in order
	assert.Equal(t, "", *received[1].Choices[0].Delta.Content.Content,
		"Second chunk should be content_block_start (empty content)")
	assert.Equal(t, "Hello", *received[2].Choices[0].Delta.Content.Content,
		"Third chunk should be first content_block_delta")
	assert.Equal(t, " world", *received[3].Choices[0].Delta.Content.Content,
		"Fourth chunk should be second content_block_delta")

	// Verify message_delta/finish is last
	assert.Equal(t, "end_turn", *received[4].Choices[0].FinishReason,
		"Last chunk should be message_delta with stop reason")
}

// TestStreamBufferingProtocol_Anthropic_ThinkingContentOrdering verifies that Anthropic
// thinking/reasoning content maintains proper ordering through buffer flush.
func TestStreamBufferingProtocol_Anthropic_ThinkingContentOrdering(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream:         streamTrue,
			FirstTokenTime: func() *time.Time { now := time.Now(); return &now }(),
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 5,
			TimerDuration:  100 * time.Millisecond,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	// Simulate Anthropic streaming with thinking content:
	// 1. role chunk
	// 2. thinking delta
	// 3. thinking delta
	// 4. text delta
	// 5. finish_reason
	chunks := []*llm.Response{
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Role: "assistant",
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						ReasoningContent: strPtr("Let me think"),
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						ReasoningContent: strPtr(" about this"),
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr("Here is my answer"),
						},
					},
				},
			},
		},
		{
			ID:     "msg_123",
			Object: "chat.completion.chunk",
			Model:  "claude-3-5-sonnet",
			Choices: []llm.Choice{
				{
					Index:        0,
					Delta:        &llm.Message{},
					FinishReason: strPtr("stop"),
				},
			},
		},
	}

	originalStream := &mockLlmStream{chunks: chunks}
	wrapped, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Collect all chunks
	received := make([]*llm.Response, 0)
	for wrapped.Next() {
		received = append(received, wrapped.Current())
	}
	require.NoError(t, wrapped.Err())

	require.Equal(t, len(chunks), len(received), "Should receive all chunks")

	// Verify role is first
	assert.Equal(t, "assistant", received[0].Choices[0].Delta.Role,
		"First chunk should be role")

	// Verify thinking content order is preserved
	assert.Equal(t, "Let me think", *received[1].Choices[0].Delta.ReasoningContent,
		"Second chunk should be first thinking delta")
	assert.Equal(t, " about this", *received[2].Choices[0].Delta.ReasoningContent,
		"Third chunk should be second thinking delta")

	// Verify text content comes after thinking
	assert.Equal(t, "Here is my answer", *received[3].Choices[0].Delta.Content.Content,
		"Fourth chunk should be text content")

	// Verify finish_reason is last
	assert.Equal(t, "stop", *received[4].Choices[0].FinishReason,
		"Last chunk should be finish_reason")
}

// TestStreamBufferingProtocol_FIFOInvariant verifies that the buffer maintains
// First-In-First-Out ordering - the first chunk buffered is the first chunk emitted after flush.
func TestStreamBufferingProtocol_FIFOInvariant(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream:         streamTrue,
			FirstTokenTime: func() *time.Time { now := time.Now(); return &now }(),
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 10, // High threshold so timer triggers flush
			TimerDuration:  50 * time.Millisecond,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	// Create chunks with unique IDs to track ordering
	chunks := make([]*llm.Response, 20)
	for i := 0; i < 20; i++ {
		chunks[i] = &llm.Response{
			ID:     "chunk-" + string(rune('A'+i)),
			Object: "chat.completion.chunk",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr(string(rune('A' + i))),
						},
					},
				},
			},
		}
	}

	originalStream := &mockLlmStream{chunks: chunks}
	wrapped, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Wait for timer to trigger flush
	time.Sleep(100 * time.Millisecond)

	// Collect all chunks
	received := make([]*llm.Response, 0)
	for wrapped.Next() {
		received = append(received, wrapped.Current())
	}
	require.NoError(t, wrapped.Err())

	require.Equal(t, len(chunks), len(received), "Should receive all chunks")

	// Verify FIFO ordering - received order should match input order
	for i := 0; i < len(chunks); i++ {
		assert.Equal(t, chunks[i].ID, received[i].ID,
			"FIFO invariant violated at index %d: expected %s, got %s",
			i, chunks[i].ID, received[i].ID)
	}
}

// TestStreamBufferingProtocol_ThresholdFlushOrder verifies that threshold-triggered
// flush preserves ordering.
func TestStreamBufferingProtocol_ThresholdFlushOrder(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream:         streamTrue,
			FirstTokenTime: func() *time.Time { now := time.Now(); return &now }(),
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 5, // Flush after 5 chunks
			TimerDuration:  10 * time.Second,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	// Create 15 chunks - first 5 should trigger threshold flush
	chunks := make([]*llm.Response, 15)
	for i := 0; i < 15; i++ {
		chunks[i] = &llm.Response{
			ID:     "chunk-" + string(rune('A'+i)),
			Object: "chat.completion.chunk",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr(string(rune('A' + i))),
						},
					},
				},
			},
		}
	}

	originalStream := &mockLlmStream{chunks: chunks}
	wrapped, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Read first 5 (should be buffered and flushed at threshold)
	first5 := make([]*llm.Response, 0)
	for i := 0; i < 5; i++ {
		require.True(t, wrapped.Next(), "Should be able to read chunk %d", i)
		first5 = append(first5, wrapped.Current())
	}

	// Read remaining
	remaining := make([]*llm.Response, 0)
	for wrapped.Next() {
		remaining = append(remaining, wrapped.Current())
	}
	require.NoError(t, wrapped.Err())

	// Verify total count
	all := append(first5, remaining...)
	require.Equal(t, 15, len(all), "Should receive all 15 chunks")

	// Verify first 5 are chunks A-E
	for i := 0; i < 5; i++ {
		assert.Equal(t, chunks[i].ID, first5[i].ID,
			"First 5 chunks should be in order A-E")
	}

	// Verify remaining are chunks F-O
	for i := 0; i < 10; i++ {
		assert.Equal(t, chunks[5+i].ID, remaining[i].ID,
			"Remaining chunks should be in order F-O")
	}
}

// TestStreamBufferingProtocol_NoReorderingAcrossFlush verifies that buffer flush
// does not cause any reordering of events.
func TestStreamBufferingProtocol_NoReorderingAcrossFlush(t *testing.T) {
	streamTrue := true
	state := &PersistenceState{
		Perf: &biz.PerformanceRecord{
			Stream:         streamTrue,
			FirstTokenTime: func() *time.Time { now := time.Now(); return &now }(),
		},
		StreamBufferingConfig: StreamBufferingConfig{
			Enabled:        true,
			ChunkThreshold: 3, // Flush after 3 chunks
			TimerDuration:  10 * time.Second,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := &streamBuffering{outbound: outbound}

	// Create mixed event types in specific order
	// 1. role (should be first)
	// 2. content delta
	// 3. content delta
	// 4. finish (should be last)
	chunks := []*llm.Response{
		{
			ID:     "chunk-role",
			Object: "chat.completion.chunk",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Role: "assistant",
					},
				},
			},
		},
		{
			ID:     "chunk-content-1",
			Object: "chat.completion.chunk",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr("First"),
						},
					},
				},
			},
		},
		{
			ID:     "chunk-content-2",
			Object: "chat.completion.chunk",
			Choices: []llm.Choice{
				{
					Index: 0,
					Delta: &llm.Message{
						Content: llm.MessageContent{
							Content: strPtr("Second"),
						},
					},
				},
			},
		},
		{
			ID:     "chunk-finish",
			Object: "chat.completion.chunk",
			Choices: []llm.Choice{
				{
					Index:        0,
					Delta:        &llm.Message{},
					FinishReason: strPtr("stop"),
				},
			},
		},
	}

	originalStream := &mockLlmStream{chunks: chunks}
	wrapped, err := middleware.OnOutboundLlmStream(context.Background(), originalStream)
	require.NoError(t, err)
	require.NotNil(t, wrapped)

	// Collect all chunks
	received := make([]*llm.Response, 0)
	for wrapped.Next() {
		received = append(received, wrapped.Current())
	}
	require.NoError(t, wrapped.Err())

	require.Equal(t, len(chunks), len(received), "Should receive all chunks")

	// Critical: Verify no reordering occurred
	// The role chunk must still be first
	assert.Equal(t, "chunk-role", received[0].ID,
		"Role chunk should still be first after buffer flush")

	// Content chunks should still be in the middle
	assert.Equal(t, "chunk-content-1", received[1].ID,
		"First content chunk should still be in position 1")
	assert.Equal(t, "chunk-content-2", received[2].ID,
		"Second content chunk should still be in position 2")

	// Finish chunk should still be last
	assert.Equal(t, "chunk-finish", received[3].ID,
		"Finish chunk should still be last after buffer flush")
}

// strPtr is a helper to create string pointers
func strPtr(s string) *string {
	return &s
}