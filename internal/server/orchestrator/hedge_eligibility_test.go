package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestGetHedgeProtocol_OpenAIStreaming(t *testing.T) {
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true, "model": "gpt-4"}`),
	}

	protocol := GetHedgeProtocol(req)
	assert.Equal(t, HedgeProtocolOpenAI, protocol)
}

func TestGetHedgeProtocol_AnthropicStreaming(t *testing.T) {
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatAnthropicMessage),
		Body:      []byte(`{"stream": true, "model": "claude-3"}`),
	}

	protocol := GetHedgeProtocol(req)
	assert.Equal(t, HedgeProtocolAnthropic, protocol)
}

func TestGetHedgeProtocol_NonStreaming(t *testing.T) {
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": false, "model": "gpt-4"}`),
	}

	protocol := GetHedgeProtocol(req)
	assert.Equal(t, HedgeProtocolNone, protocol)
}

func TestGetHedgeProtocol_NilRequest(t *testing.T) {
	protocol := GetHedgeProtocol(nil)
	assert.Equal(t, HedgeProtocolNone, protocol)
}

func TestGetHedgeProtocol_EmptyBody(t *testing.T) {
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte{},
	}

	protocol := GetHedgeProtocol(req)
	assert.Equal(t, HedgeProtocolNone, protocol)
}

func TestGetHedgeProtocol_UnsupportedFormat(t *testing.T) {
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatGeminiContents),
		Body:      []byte(`{"stream": true, "model": "gemini-pro"}`),
	}

	protocol := GetHedgeProtocol(req)
	assert.Equal(t, HedgeProtocolNone, protocol)
}

func TestGetHedgeProtocol_OpenAIResponsesAPI(t *testing.T) {
	req := &httpclient.Request{
		APIFormat: "openai/responses",
		Body:      []byte(`{"stream": true}`),
	}

	protocol := GetHedgeProtocol(req)
	assert.Equal(t, HedgeProtocolOpenAI, protocol)
}

func TestIsHedgeEligible_Eligible(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true, "model": "gpt-4"}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, candidateSet)
	assert.True(t, eligible)
}

func TestIsHedgeEligible_AnthropicEligible(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatAnthropicMessage),
		Body:      []byte(`{"stream": true}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, candidateSet)
	assert.True(t, eligible)
}

func TestIsHedgeEligible_NonStreaming(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": false, "model": "gpt-4"}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, candidateSet)
	assert.False(t, eligible)
}

func TestIsHedgeEligible_HedgeDisabled(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: false}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true, "model": "gpt-4"}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, candidateSet)
	assert.False(t, eligible)
}

func TestIsHedgeEligible_NilHedgePolicy(t *testing.T) {
	ctx := context.Background()
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true}`),
	}

	eligible := IsHedgeEligible(ctx, req, nil, candidateSet)
	assert.False(t, eligible)
}

func TestIsHedgeEligible_NilCandidateSet(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, nil)
	assert.False(t, eligible)
}

func TestIsHedgeEligible_NilPrimary(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   nil,
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, candidateSet)
	assert.False(t, eligible)
}

func TestIsHedgeEligible_NilSecondary(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: nil,
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, candidateSet)
	assert.False(t, eligible)
}

func TestIsHedgeEligible_UnsupportedEndpoint(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatGeminiContents),
		Body:      []byte(`{"stream": true}`),
	}

	eligible := IsHedgeEligible(ctx, req, hedgePolicy, candidateSet)
	assert.False(t, eligible)
}

func TestIsHedgeEligible_NilRequest(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	eligible := IsHedgeEligible(ctx, nil, hedgePolicy, candidateSet)
	assert.False(t, eligible)
}

func TestHedgeProtocol_String(t *testing.T) {
	tests := []struct {
		protocol HedgeProtocol
		expected string
	}{
		{HedgeProtocolNone, "HedgeProtocolNone"},
		{HedgeProtocolOpenAI, "HedgeProtocolOpenAI"},
		{HedgeProtocolAnthropic, "HedgeProtocolAnthropic"},
		{HedgeProtocol(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.protocol.String())
		})
	}
}

func TestIsHedgeEligible_OpenAIWithAllConditionsMet(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatOpenAIChatCompletion),
		Body:      []byte(`{"stream": true, "model": "gpt-4o"}`),
	}

	require.True(t, IsHedgeEligible(ctx, req, hedgePolicy, candidateSet))
}

func TestIsHedgeEligible_AnthropicWithAllConditionsMet(t *testing.T) {
	ctx := context.Background()
	hedgePolicy := &biz.HedgePolicy{Enabled: true}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}
	req := &httpclient.Request{
		APIFormat: string(llm.APIFormatAnthropicMessage),
		Body:      []byte(`{"stream": true, "model": "claude-3-5-sonnet"}`),
	}

	require.True(t, IsHedgeEligible(ctx, req, hedgePolicy, candidateSet))
}