package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

// HedgeProtocol represents the streaming protocol for hedge routing.
type HedgeProtocol int

const (
	// HedgeProtocolNone indicates non-streaming or unsupported protocol.
	HedgeProtocolNone HedgeProtocol = iota
	// HedgeProtocolOpenAI indicates OpenAI-compatible streaming.
	HedgeProtocolOpenAI
	// HedgeProtocolAnthropic indicates Anthropic-compatible streaming.
	HedgeProtocolAnthropic
)

func (p HedgeProtocol) String() string {
	switch p {
	case HedgeProtocolNone:
		return "HedgeProtocolNone"
	case HedgeProtocolOpenAI:
		return "HedgeProtocolOpenAI"
	case HedgeProtocolAnthropic:
		return "HedgeProtocolAnthropic"
	default:
		return "Unknown"
	}
}

// isStreamingRequest checks if the httpclient.Request is a streaming request.
// It parses the body to look for the "stream" field.
func isStreamingRequest(request *httpclient.Request) bool {
	if request == nil || len(request.Body) == 0 {
		return false
	}

	// Try to parse as a generic map to check for stream field
	var body map[string]any
	if err := json.Unmarshal(request.Body, &body); err != nil {
		return false
	}

	// Check if stream field is present and true
	if streamVal, ok := body["stream"]; ok {
		if b, ok := streamVal.(bool); ok {
			return b
		}
	}

	return false
}

// GetHedgeProtocol returns the hedge protocol for the given request.
// Returns HedgeProtocolOpenAI for OpenAI-compatible streaming requests.
// Returns HedgeProtocolAnthropic for Anthropic-compatible streaming requests.
// Returns HedgeProtocolNone for non-streaming or unsupported protocols.
func GetHedgeProtocol(request *httpclient.Request) HedgeProtocol {
	if request == nil {
		return HedgeProtocolNone
	}

	// Check if request is streaming
	if !isStreamingRequest(request) {
		return HedgeProtocolNone
	}

	// Detect protocol based on APIFormat field
	// The APIFormat is set by the inbound transformer during request parsing
	switch llm.APIFormat(request.APIFormat) {
	case llm.APIFormatOpenAIChatCompletion:
		return HedgeProtocolOpenAI
	case llm.APIFormatAnthropicMessage:
		return HedgeProtocolAnthropic
	default:
		// Also check for other OpenAI-compatible formats
		apiFormat := request.APIFormat
		if apiFormat == "openai/chat_completions" ||
			apiFormat == "openai/responses" ||
			apiFormat == "vercel/ai-sdk" {
			return HedgeProtocolOpenAI
		}
		if apiFormat == "anthropic/messages" {
			return HedgeProtocolAnthropic
		}
		return HedgeProtocolNone
	}
}

// GetHedgeProtocolFromLlmRequest returns the hedge protocol for the given LLM request.
// This version works with the internal LLM request model.
func GetHedgeProtocolFromLlmRequest(llmRequest *llm.Request) HedgeProtocol {
	if llmRequest == nil {
		return HedgeProtocolNone
	}

	// Check if request is streaming
	if llmRequest.Stream == nil || !*llmRequest.Stream {
		return HedgeProtocolNone
	}

	// Detect protocol based on APIFormat field
	switch llmRequest.APIFormat {
	case llm.APIFormatOpenAIChatCompletion:
		return HedgeProtocolOpenAI
	case llm.APIFormatAnthropicMessage:
		return HedgeProtocolAnthropic
	default:
		return HedgeProtocolNone
	}
}

// IsHedgeEligible returns true only when ALL conditions are met:
// - Request is streaming (request.Stream == true)
// - Hedge feature is enabled (hedgePolicy.Enabled == true)
// - At least 2 distinct candidates exist (candidateSet != nil && Primary != nil && Secondary != nil)
// - Endpoint is OpenAI-compatible or Anthropic-compatible streaming
//
// Returns false for non-streaming, disabled, insufficient candidates, or unsupported endpoints.
func IsHedgeEligible(ctx context.Context, request *httpclient.Request, hedgePolicy *biz.HedgePolicy, candidateSet *HedgeCandidateSet) bool {
	// Check 1: Request must be streaming
	if !isStreamingRequest(request) {
		return false
	}

	// Check 2: Hedge feature must be enabled
	if hedgePolicy == nil || !hedgePolicy.Enabled {
		return false
	}

	// Check 3: At least 2 distinct candidates must exist
	if candidateSet == nil {
		return false
	}
	if candidateSet.Primary == nil || candidateSet.Secondary == nil {
		return false
	}

	// Check 4: Endpoint must be OpenAI or Anthropic compatible streaming
	protocol := GetHedgeProtocol(request)
	return protocol == HedgeProtocolOpenAI || protocol == HedgeProtocolAnthropic
}