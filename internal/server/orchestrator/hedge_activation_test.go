package orchestrator

import (
	"context"
	"testing"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestIsProbingRequest_Deterministic(t *testing.T) {
	sampler := NewDeterministicHedgeSampler()

	tests := []struct {
		name      string
		requestID string
	}{
		{"empty string", ""},
		{"simple id", "req-123"},
		{"uuid style", "550e8400-e29b-41d4-a716-446655440000"},
		{"complex id", "proj_abc/user_xyz/req_12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 100; i++ {
				result1 := sampler.ShouldProbe(tt.requestID, 5.0)
				result2 := sampler.ShouldProbe(tt.requestID, 5.0)
				if result1 != result2 {
					t.Errorf("ShouldProbe is not deterministic: first call returned %v, second call returned %v", result1, result2)
				}
			}
		})
	}
}

func TestIsProbingRequest_PercentageBoundaries(t *testing.T) {
	sampler := NewDeterministicHedgeSampler()

	t.Run("percentage 0 never probes", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			requestID := "test-request-" + string(rune(i))
			if sampler.ShouldProbe(requestID, 0) {
				t.Errorf("percentage=0 should never probe, but probed for requestID=%s", requestID)
			}
		}
	})

	t.Run("percentage 100 always probes", func(t *testing.T) {
		testIDs := []string{"", "a", "req-1", "req-2", "req-3"}
		for _, requestID := range testIDs {
			if !sampler.ShouldProbe(requestID, 100) {
				t.Errorf("percentage=100 should always probe, but didn't for requestID=%s", requestID)
			}
		}
	})
}

func TestIsProbingRequest_Distribution(t *testing.T) {
	sampler := NewDeterministicHedgeSampler()

	t.Run("5 percent sampling approximates 5 percent", func(t *testing.T) {
		const iterations = 10000
		const expectedPercent = 5.0
		const tolerance = 2.0

		probeCount := 0
		for i := 0; i < iterations; i++ {
			requestID := "dist-test-" + string(rune(i))
			if sampler.ShouldProbe(requestID, expectedPercent) {
				probeCount++
			}
		}

		actualPercent := float64(probeCount) / float64(iterations) * 100

		if actualPercent < expectedPercent-tolerance || actualPercent > expectedPercent+tolerance {
			t.Errorf("Expected ~%.2f%%, got %.2f%% (%d/%d)",
				expectedPercent, actualPercent, probeCount, iterations)
		}
	})
}

type mockHedgeSampler struct {
	probes map[string]bool
}

func (m *mockHedgeSampler) ShouldProbe(requestID string, percentage float64) bool {
	if m.probes == nil {
		m.probes = make(map[string]bool)
	}
	val, ok := m.probes[requestID]
	if !ok {
		return false
	}
	return val
}

func TestShouldActivateHedge_AllConditionsMet(t *testing.T) {
	sampler := &mockHedgeSampler{probes: map[string]bool{"test-req": true}}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, isProbing := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true when all conditions are met")
	}
	if !isProbing {
		t.Error("Expected isProbing=true when sampler returns true")
	}
}

func TestShouldActivateHedge_ConditionNotStreaming(t *testing.T) {
	sampler := &mockHedgeSampler{}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body: []byte(`{"stream":false}`),
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if shouldHedge {
		t.Error("Expected shouldHedge=false when request is not streaming")
	}
}

func TestShouldActivateHedge_ConditionNotEnabled(t *testing.T) {
	sampler := &mockHedgeSampler{}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            false,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if shouldHedge {
		t.Error("Expected shouldHedge=false when hedge is not enabled")
	}
}

func TestShouldActivateHedge_ConditionNilPolicy(t *testing.T) {
	sampler := &mockHedgeSampler{}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, nil, candidateSet, sampler)

	if shouldHedge {
		t.Error("Expected shouldHedge=false when hedge policy is nil")
	}
}

func TestShouldActivateHedge_ConditionInsufficientCandidates(t *testing.T) {
	sampler := &mockHedgeSampler{}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, nil, sampler)

	if shouldHedge {
		t.Error("Expected shouldHedge=false when candidate set is nil")
	}
}

func TestShouldActivateHedge_ConditionOnlyPrimaryCandidate(t *testing.T) {
	sampler := &mockHedgeSampler{}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if shouldHedge {
		t.Error("Expected shouldHedge=false when only primary candidate exists")
	}
}

func TestShouldActivateHedge_ConditionOnlySecondaryCandidate(t *testing.T) {
	sampler := &mockHedgeSampler{}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if shouldHedge {
		t.Error("Expected shouldHedge=false when only secondary candidate exists")
	}
}

func TestShouldActivateHedge_UnsupportedProtocol(t *testing.T) {
	sampler := &mockHedgeSampler{}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "unsupported/format",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if shouldHedge {
		t.Error("Expected shouldHedge=false for unsupported protocol")
	}
}

func TestShouldActivateHedge_ProbingModeFalse(t *testing.T) {
	sampler := &mockHedgeSampler{probes: map[string]bool{"test-req": false}}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, isProbing := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true when all conditions are met")
	}
	if isProbing {
		t.Error("Expected isProbing=false when sampler returns false")
	}
}

func TestShouldActivateHedge_ProbingPercentageZero(t *testing.T) {
	sampler := &mockHedgeSampler{probes: map[string]bool{"test-req": true}}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, isProbing := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true when all conditions are met")
	}
	if isProbing {
		t.Error("Expected isProbing=false when probing percentage is 0")
	}
}

func TestShouldActivateHedge_OpenAIProtocol(t *testing.T) {
	sampler := &mockHedgeSampler{probes: map[string]bool{"test-req": true}}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true for OpenAI protocol")
	}
}

func TestShouldActivateHedge_AnthropicProtocol(t *testing.T) {
	sampler := &mockHedgeSampler{probes: map[string]bool{"test-req": true}}

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "anthropic/messages",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, _ := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true for Anthropic protocol")
	}
}

func TestShouldActivateHedge_DefaultSampler(t *testing.T) {
	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 100.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, isProbing := ShouldActivateHedge(context.Background(), request, hedgePolicy, candidateSet)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true when all conditions are met")
	}
	if !isProbing {
		t.Error("Expected isProbing=true when using default sampler with percentage=100")
	}
}

func TestShouldActivateHedge_CustomSampler(t *testing.T) {
	customProbed := false
	sampler := &mockHedgeSampler{
		probes: map[string]bool{
			"test-req": true,
		},
	}
	_ = customProbed

	request := &httpclient.Request{
		RequestID: "test-req",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 5.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, isProbing := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true when all conditions are met")
	}
	if !isProbing {
		t.Error("Expected isProbing=true when custom sampler returns true")
	}
}

func TestIsProbingRequest_Function(t *testing.T) {
	if !IsProbingRequest("test-id-1", 100.0) {
		t.Error("Expected true for percentage=100")
	}

	if IsProbingRequest("test-id-2", 0.0) {
		t.Error("Expected false for percentage=0")
	}
}

func TestShouldActivateHedge_EmptyRequestID(t *testing.T) {
	sampler := &mockHedgeSampler{probes: map[string]bool{"": true}}

	request := &httpclient.Request{
		RequestID: "",
		Body:      []byte(`{"stream":true}`),
		APIFormat: "openai/chat_completions",
	}
	hedgePolicy := &biz.HedgePolicy{
		Enabled:            true,
		ProbingPercentage: 100.0,
	}
	candidateSet := &HedgeCandidateSet{
		Primary:   &ChannelModelsCandidate{},
		Secondary: &ChannelModelsCandidate{},
	}

	shouldHedge, isProbing := ShouldActivateHedgeWithSampler(context.Background(), request, hedgePolicy, candidateSet, sampler)

	if !shouldHedge {
		t.Error("Expected shouldHedge=true when all conditions are met")
	}
	if !isProbing {
		t.Error("Expected isProbing=true when sampler returns true for empty request ID")
	}
}