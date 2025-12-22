package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/llm"
	"github.com/looplj/axonhub/internal/pkg/httpclient"
)

// choiceAggregator is a helper struct to aggregate data for each choice.
type choiceAggregator struct {
	index            int
	content          strings.Builder
	reasoningContent strings.Builder
	toolCalls        map[int]*llm.ToolCall // Map to track tool calls by their index within the choice
	finishReason     *string
	role             string
}

// TransformChunk transforms an OpenRouter streaming chunk into an OpenRouter Response.
func TransformChunk(ctx context.Context, chunk *httpclient.StreamEvent) (*Response, error) {
	var chatResp Response

	err := json.Unmarshal(chunk.Data, &chatResp)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal chat completion response: %w", err)
	}

	return &chatResp, nil
}

// AggregateStreamChunks aggregates OpenRouter streaming response chunks into a complete response.
// This implementation works directly with OpenRouter Response models for better handling of OpenRouter-specific fields.
func AggregateStreamChunks(ctx context.Context, chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	if len(chunks) == 0 {
		data, err := json.Marshal(&llm.Response{})
		return data, llm.ResponseMeta{}, err
	}

	var (
		usage             *llm.Usage
		systemFingerprint string
		// Map to track choices by their index
		choicesAggs = make(map[int]*choiceAggregator)
		lastChunk   *Response
	)

	for _, chunk := range chunks {
		// Skip [DONE] events
		if bytes.HasPrefix(chunk.Data, []byte("[DONE]")) {
			continue
		}

		chunkResp, err := TransformChunk(ctx, chunk)
		if err != nil {
			continue // Skip invalid chunks
		}

		// Process each choice in the chunk
		for _, choice := range chunkResp.Choices {
			choiceIndex := choice.Index

			// Initialize choice aggregator if it doesn't exist
			if _, ok := choicesAggs[choiceIndex]; !ok {
				choicesAggs[choiceIndex] = &choiceAggregator{
					index:     choiceIndex,
					toolCalls: make(map[int]*llm.ToolCall),
					role:      "assistant",
				}
			}

			choiceAgg := choicesAggs[choiceIndex]

			if choice.Delta != nil {
				// Handle role
				if choice.Delta.Role != "" {
					choiceAgg.role = choice.Delta.Role
				}

				// Handle content
				if choice.Delta.Content.Content != nil {
					choiceAgg.content.WriteString(*choice.Delta.Content.Content)
				}

				// Handle reasoning content - aggregate from reasoning_details or reasoning field
				llmDelta := choice.Delta.ToLLMMessage()
				if llmDelta.ReasoningContent != nil {
					choiceAgg.reasoningContent.WriteString(*llmDelta.ReasoningContent)
				}

				// Handle tool calls
				if len(choice.Delta.ToolCalls) > 0 {
					for _, deltaToolCall := range choice.Delta.ToolCalls {
						// Use the index from the OpenAI delta tool call
						toolCallIndex := deltaToolCall.Index

						// Initialize tool call if it doesn't exist
						if _, ok := choiceAgg.toolCalls[toolCallIndex]; !ok {
							choiceAgg.toolCalls[toolCallIndex] = &llm.ToolCall{
								Index: toolCallIndex,
								ID:    deltaToolCall.ID,
								Type:  deltaToolCall.Type,
								Function: llm.FunctionCall{
									Name:      deltaToolCall.Function.Name,
									Arguments: "",
								},
							}
						}

						// Aggregate function arguments
						if deltaToolCall.Function.Arguments != "" {
							choiceAgg.toolCalls[toolCallIndex].Function.Arguments += deltaToolCall.Function.Arguments
						}

						// Update function name if provided
						if deltaToolCall.Function.Name != "" {
							choiceAgg.toolCalls[toolCallIndex].Function.Name = deltaToolCall.Function.Name
						}

						// Update ID and type if provided
						if deltaToolCall.ID != "" {
							choiceAgg.toolCalls[toolCallIndex].ID = deltaToolCall.ID
						}

						if deltaToolCall.Type != "" {
							choiceAgg.toolCalls[toolCallIndex].Type = deltaToolCall.Type
						}
					}
				}
			}

			// Capture finish reason
			if choice.FinishReason != nil {
				choiceAgg.finishReason = choice.FinishReason
			}
		}

		// Extract usage information if present
		if chunkResp.Usage != nil {
			usage = chunkResp.Usage.ToLLMUsage()
		}

		// Keep the first non-empty system fingerprint
		if systemFingerprint == "" && chunkResp.SystemFingerprint != "" {
			systemFingerprint = chunkResp.SystemFingerprint
		}

		// Keep the last chunk for metadata
		lastChunk = chunkResp
	}

	// Create a complete ChatCompletionResponse based on the last chunk structure
	if lastChunk == nil {
		data, err := json.Marshal(&llm.Response{})
		return data, llm.ResponseMeta{}, err
	}

	choices := make([]llm.Choice, len(choicesAggs))

	for choiceIndex := range choices {
		choiceAgg := choicesAggs[choiceIndex]

		var finalToolCalls []llm.ToolCall
		if len(choiceAgg.toolCalls) > 0 {
			finalToolCalls = make([]llm.ToolCall, len(choiceAgg.toolCalls))
			for index := range finalToolCalls {
				finalToolCalls[index] = *choiceAgg.toolCalls[index]
			}
		}

		// Build the message
		message := &llm.Message{
			Role: choiceAgg.role,
		}

		// Set reasoning content if available
		if choiceAgg.reasoningContent.Len() > 0 {
			reasoningContent := choiceAgg.reasoningContent.String()
			message.ReasoningContent = &reasoningContent
		}

		// Set content if available
		if choiceAgg.content.Len() > 0 {
			content := choiceAgg.content.String()
			message.Content = llm.MessageContent{Content: &content}
		}

		// Set tool calls if available (can coexist with content)
		if len(finalToolCalls) > 0 {
			message.ToolCalls = finalToolCalls
		}

		// Determine finish reason
		finishReason := choiceAgg.finishReason
		if finishReason == nil {
			if len(finalToolCalls) > 0 {
				finishReason = lo.ToPtr("tool_calls")
			} else {
				finishReason = lo.ToPtr("stop")
			}
		}

		choices[choiceIndex] = llm.Choice{
			Index:        choiceIndex,
			Message:      message,
			FinishReason: finishReason,
		}
	}

	// Build the final response
	response := &llm.Response{
		ID:                lastChunk.ID,
		Model:             lastChunk.Model,
		Object:            "chat.completion", // Change from "chat.completion.chunk" to "chat.completion"
		Created:           lastChunk.Created,
		SystemFingerprint: systemFingerprint,
		Choices:           choices,
		Usage:             usage,
	}

	data, err := json.Marshal(response)
	if err != nil {
		return nil, llm.ResponseMeta{}, err
	}

	return data, llm.ResponseMeta{
		ID:    response.ID,
		Usage: usage,
	}, nil
}
