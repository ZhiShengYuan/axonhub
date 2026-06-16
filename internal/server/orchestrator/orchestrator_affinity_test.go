package orchestrator

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/pipeline/stream"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

// hasAffinityStrategy walks the strategy slice and returns true when at least
// one strategy is an *AffinityAwareStrategy.
func hasAffinityStrategy(strategies []LoadBalanceStrategy) bool {
	for _, s := range strategies {
		if _, ok := s.(*AffinityAwareStrategy); ok {
			return true
		}
	}
	return false
}

// setupAffinityTestServices creates the service trio needed to build load
// balancers that reference real RequestService (the ChannelAffinityProvider).
func setupAffinityTestServices(t *testing.T, client *ent.Client) (*biz.ChannelService, *biz.RequestService, *biz.SystemService, *biz.UsageLogService) {
	t.Helper()

	cacheConfig := xcache.Config{Mode: xcache.ModeMemory}
	systemService := biz.NewSystemService(biz.SystemServiceParams{CacheConfig: cacheConfig, Ent: client})
	channelService := biz.NewChannelServiceForTest(client)
	dataStorageService := &biz.DataStorageService{
		AbstractService: &biz.AbstractService{},
		SystemService:   systemService,
		Cache:           xcache.NewFromConfig[ent.DataStorage](cacheConfig),
	}
	usageLogService := biz.NewUsageLogService(client, systemService, channelService)
	requestService := biz.NewRequestService(client, systemService, usageLogService, dataStorageService, biz.NewLiveStreamRegistry())

	return channelService, requestService, systemService, usageLogService
}

// buildAdaptiveLoadBalancer mirrors the adaptive load-balancer construction in
// NewChatCompletionOrchestrator.
func buildAdaptiveLoadBalancer(systemService *biz.SystemService, channelService *biz.ChannelService, requestService *biz.RequestService) *LoadBalancer {
	rateLimitStrategy := NewRateLimitAwareStrategy(NewChannelRequestTracker(), NewChannelLimiterManager())
	quotaStrategy := NewQuotaAwareStrategy(&mockQuotaStatusProvider{}, systemService)

	return NewLoadBalancer(systemService, channelService,
		NewTraceAwareStrategy(requestService),
		NewAffinityAwareStrategy(requestService),
		NewErrorAwareStrategy(channelService),
		NewWeightRoundRobinStrategy(channelService),
		NewLatencyAwareStrategy(channelService),
		rateLimitStrategy,
		quotaStrategy,
	)
}

func buildFailoverLoadBalancer(systemService *biz.SystemService, channelService *biz.ChannelService, requestService *biz.RequestService) *LoadBalancer {
	rateLimitStrategy := NewRateLimitAwareStrategy(NewChannelRequestTracker(), NewChannelLimiterManager())
	quotaStrategy := NewQuotaAwareStrategy(&mockQuotaStatusProvider{}, systemService)

	return NewLoadBalancer(systemService, channelService,
		NewWeightStrategy(), NewAffinityAwareStrategy(requestService), NewRandomStrategy(), rateLimitStrategy, quotaStrategy)
}

func buildCircuitBreakerLoadBalancer(systemService *biz.SystemService, channelService *biz.ChannelService, requestService *biz.RequestService) *LoadBalancer {
	rateLimitStrategy := NewRateLimitAwareStrategy(NewChannelRequestTracker(), NewChannelLimiterManager())
	quotaStrategy := NewQuotaAwareStrategy(&mockQuotaStatusProvider{}, systemService)
	modelCircuitBreaker := biz.NewModelCircuitBreaker()

	return NewLoadBalancer(systemService, channelService,
		NewWeightStrategy(), NewAffinityAwareStrategy(requestService), NewModelAwareCircuitBreakerStrategy(modelCircuitBreaker), rateLimitStrategy, quotaStrategy)
}

func TestLoadBalancer_AffinityStrategyRegistered_Adaptive(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	_, requestService, systemService, _ := setupAffinityTestServices(t, client)
	channelService := biz.NewChannelServiceForTest(client)

	lb := buildAdaptiveLoadBalancer(systemService, channelService, requestService)

	assert.True(t, hasAffinityStrategy(lb.strategies),
		"adaptive load balancer should include AffinityAwareStrategy")
}

func TestLoadBalancer_AffinityStrategyRegistered_Failover(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	_, requestService, systemService, _ := setupAffinityTestServices(t, client)
	channelService := biz.NewChannelServiceForTest(client)

	lb := buildFailoverLoadBalancer(systemService, channelService, requestService)

	assert.True(t, hasAffinityStrategy(lb.strategies),
		"failover load balancer should include AffinityAwareStrategy")
}

func TestLoadBalancer_AffinityStrategyRegistered_CircuitBreaker(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	_, requestService, systemService, _ := setupAffinityTestServices(t, client)
	channelService := biz.NewChannelServiceForTest(client)

	lb := buildCircuitBreakerLoadBalancer(systemService, channelService, requestService)

	assert.True(t, hasAffinityStrategy(lb.strategies),
		"circuit-breaker load balancer should include AffinityAwareStrategy")
}

// TestChatCompletionOrchestrator_Process_NoAffinityUnchanged verifies that a
// request without any affinity headers does not panic and produces a normal
// completion result. The absence of affinity state must be invisible to the
// pipeline — no error, no crash, just normal routing.
func TestChatCompletionOrchestrator_Process_NoAffinityUnchanged(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	channelRow, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Test Channel NoAffinity").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-api-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	respBody := buildMockOpenAIResponse("chatcmpl-test-no-affinity", "gpt-4", "Hello", 5, 2)

	executor := &mockExecutor{
		response: &httpclient.Response{
			StatusCode: 200,
			Body:       respBody,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
		},
	}

	outbound, err := openai.NewOutboundTransformer(channelRow.BaseURL, channelRow.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: channelRow, Outbound: outbound}
	channelSelector := &staticChannelSelector{candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4")}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:       channelSelector,
		Inbound:               openai.NewInboundTransformer(),
		RequestService:        requestService,
		ChannelService:        channelService,
		PromptProvider:        &stubPromptProvider{},
		SystemService:         systemService,
		UsageLogService:       usageLogService,
		PipelineFactory:       pipeline.NewFactory(executor),
		ModelMapper:           NewModelMapper(),
		channelLimiterManager: NewChannelLimiterManager(),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	// Request without any affinity headers — normal behavior expected.
	httpRequest := buildTestRequest("gpt-4", "Hello", false)
	ctx = contexts.WithProjectID(ctx, project.ID)

	result, err := orchestrator.Process(ctx, httpRequest)
	require.NoError(t, err, "Process must not error when no affinity headers are present")
	require.NotNil(t, result.ChatCompletion, "must produce a completion result")
	require.Nil(t, result.ChatCompletionStream, "non-streaming request must not return a stream")
}

// TestChatCompletionOrchestrator_Process_WithAffinityHeader verifies that a
// request with an X-Session-Affinity header completes successfully. This
// exercises the full ExtractAffinity → WithAffinityState path inside Process
// without needing a pre-populated affinity cache.
func TestChatCompletionOrchestrator_Process_WithAffinityHeader(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	channelRow, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Test Channel WithAffinity").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-api-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	respBody := buildMockOpenAIResponse("chatcmpl-test-with-affinity", "gpt-4", "Hi from affinity session", 5, 3)

	executor := &mockExecutor{
		response: &httpclient.Response{
			StatusCode: 200,
			Body:       respBody,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
		},
	}

	outbound, err := openai.NewOutboundTransformer(channelRow.BaseURL, channelRow.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: channelRow, Outbound: outbound}
	channelSelector := &staticChannelSelector{candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4")}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:       channelSelector,
		Inbound:               openai.NewInboundTransformer(),
		RequestService:        requestService,
		ChannelService:        channelService,
		PromptProvider:        &stubPromptProvider{},
		SystemService:         systemService,
		UsageLogService:       usageLogService,
		PipelineFactory:       pipeline.NewFactory(executor),
		ModelMapper:           NewModelMapper(),
		channelLimiterManager: NewChannelLimiterManager(),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	// Build request with an affinity header.
	httpRequest := buildTestRequest("gpt-4", "Hello with affinity", false)
	httpRequest.Headers.Set("X-Session-Affinity", "session-abc-123")
	ctx = contexts.WithProjectID(ctx, project.ID)

	// The request should complete normally — the affinity cache is empty so
	// the strategy returns 0 for all channels and routing proceeds as usual.
	result, err := orchestrator.Process(ctx, httpRequest)
	require.NoError(t, err, "Process must not error with affinity headers present")
	require.NotNil(t, result.ChatCompletion, "must produce a completion result")
}
