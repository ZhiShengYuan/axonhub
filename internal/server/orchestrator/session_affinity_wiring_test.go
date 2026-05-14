package orchestrator

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/pipeline/stream"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestChatCompletionOrchestrator_Process_WithSessionAffinity(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	sessionAffinitySvc := NewSessionAffinityService([]byte("test-secret-for-affinity"))

	project := createTestProject(t, ctx, client)

	chA := createTestChannelWithProvider(t, ctx, client, "channel-a", channel.TypeOpenai, "https://api.channel-a.com/v1")
	chB := createTestChannelWithProvider(t, ctx, client, "channel-b", channel.TypeOpenai, "https://api.channel-b.com/v1")

	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	bizChA := &biz.Channel{Channel: chA, Outbound: nil}
	bizChB := &biz.Channel{Channel: chB, Outbound: nil}

	outboundA, err := openai.NewOutboundTransformer(chA.BaseURL, chA.Credentials.APIKey)
	require.NoError(t, err)
	bizChA.Outbound = outboundA

	outboundB, err := openai.NewOutboundTransformer(chB.BaseURL, chB.Credentials.APIKey)
	require.NoError(t, err)
	bizChB.Outbound = outboundB

	candidates := []*ChannelModelsCandidate{
		{Channel: bizChB, Priority: 0, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4", Source: "direct"}}},
		{Channel: bizChA, Priority: 0, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4", Source: "direct"}}},
	}

	selector := &staticChannelSelector{candidates: candidates}

	mockResp := buildMockOpenAIResponse("chatcmpl-123", "gpt-4", "Hello! How can I help you?", 10, 20)
	var usedURL string
	mockExec := &urlTrackingExecutor{
		response: &httpclient.Response{
			StatusCode: 200,
			Body:       mockResp,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
		},
		urlTracker: &usedURL,
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:           selector,
		Inbound:                  openai.NewInboundTransformer(),
		RequestService:           requestService,
		ChannelService:           channelService,
		PromptProvider:           &stubPromptProvider{},
		SystemService:            systemService,
		UsageLogService:         usageLogService,
		PipelineFactory:          pipeline.NewFactory(mockExec),
		ModelMapper:              NewModelMapper(),
		connectionTracker:        NewDefaultConnectionTracker(1024),
		SessionAffinityService:   sessionAffinitySvc,
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	scope := BuildAffinityScope(0, 0, "gpt-4", "openai", "test-session-affinity")
	sessionAffinitySvc.Set(ctx, scope, chA.ID)

	affinityCtx := contexts.WithSessionAffinity(ctx, "test-session-affinity")
	affinityCtx = contexts.WithProjectID(affinityCtx, project.ID)

	httpRequest := buildTestRequest("gpt-4", "Hello!", false)

	result, err := orchestrator.Process(affinityCtx, httpRequest)

	require.NoError(t, err)
	require.NotNil(t, result.ChatCompletion)

	assert.True(t, strings.Contains(usedURL, "channel-a"), "should use preferred channel A due to affinity, got URL: %s", usedURL)
}

func TestChatCompletionOrchestrator_Process_WithoutSessionAffinity(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	sessionAffinitySvc := NewSessionAffinityService([]byte("test-secret-for-affinity"))

	project := createTestProject(t, ctx, client)

	chA := createTestChannelWithProvider(t, ctx, client, "channel-a", channel.TypeOpenai, "https://api.channel-a.com/v1")
	chB := createTestChannelWithProvider(t, ctx, client, "channel-b", channel.TypeOpenai, "https://api.channel-b.com/v1")

	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	bizChA := &biz.Channel{Channel: chA, Outbound: nil}
	bizChB := &biz.Channel{Channel: chB, Outbound: nil}

	outboundA, err := openai.NewOutboundTransformer(chA.BaseURL, chA.Credentials.APIKey)
	require.NoError(t, err)
	bizChA.Outbound = outboundA

	outboundB, err := openai.NewOutboundTransformer(chB.BaseURL, chB.Credentials.APIKey)
	require.NoError(t, err)
	bizChB.Outbound = outboundB

	candidates := []*ChannelModelsCandidate{
		{Channel: bizChB, Priority: 0, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4", Source: "direct"}}},
		{Channel: bizChA, Priority: 0, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4", Source: "direct"}}},
	}

	selector := &staticChannelSelector{candidates: candidates}

	mockResp := buildMockOpenAIResponse("chatcmpl-123", "gpt-4", "Hello! How can I help you?", 10, 20)
	var usedURL string
	mockExec := &urlTrackingExecutor{
		response: &httpclient.Response{
			StatusCode: 200,
			Body:       mockResp,
			Headers:    http.Header{"Content-Type": []string{"application/json"}},
		},
		urlTracker: &usedURL,
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:         selector,
		Inbound:                openai.NewInboundTransformer(),
		RequestService:         requestService,
		ChannelService:         channelService,
		PromptProvider:         &stubPromptProvider{},
		SystemService:          systemService,
		UsageLogService:       usageLogService,
		PipelineFactory:        pipeline.NewFactory(mockExec),
		ModelMapper:            NewModelMapper(),
		connectionTracker:      NewDefaultConnectionTracker(1024),
		SessionAffinityService: sessionAffinitySvc,
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	projectCtx := contexts.WithProjectID(ctx, project.ID)

	httpRequest := buildTestRequest("gpt-4", "Hello!", false)

	result, err := orchestrator.Process(projectCtx, httpRequest)

	require.NoError(t, err)
	require.NotNil(t, result.ChatCompletion)

	assert.True(t, strings.Contains(usedURL, "channel-b"), "should use channel B (first in list) when no affinity, got URL: %s", usedURL)
}

func TestChatCompletionOrchestrator_Constructor_WiresSessionAffinityService(t *testing.T) {
	channelService := &biz.ChannelService{}
	defaultSelector := &DefaultSelector{}
	requestService := &biz.RequestService{}
	httpClient := &httpclient.HttpClient{}
	inboundTransformer := openai.NewInboundTransformer()
	systemService := &biz.SystemService{}
	usageLogService := &biz.UsageLogService{}
	promptService := &biz.PromptService{}
	quotaService := &biz.QuotaService{}
	promptProtectionRuleService := &biz.PromptProtectionRuleService{}
	liveStreamRegistry := &biz.LiveStreamRegistry{}
	sessionAffinityService := NewSessionAffinityService([]byte("test-secret"))

	orchestrator := NewChatCompletionOrchestrator(
		channelService,
		defaultSelector,
		requestService,
		httpClient,
		inboundTransformer,
		systemService,
		usageLogService,
		promptService,
		quotaService,
		promptProtectionRuleService,
		liveStreamRegistry,
		sessionAffinityService,
	)

	assert.NotNil(t, orchestrator.SessionAffinityService)
	assert.Equal(t, sessionAffinityService, orchestrator.SessionAffinityService)
}

func createTestChannelWithProvider(t *testing.T, ctx context.Context, client *ent.Client, name string, provider channel.Type, baseURL string) *ent.Channel {
	t.Helper()

	ch, err := client.Channel.Create().
		SetName(name).
		SetType(provider).
		SetStatus(channel.StatusEnabled).
		SetBaseURL(baseURL).
		SetCredentials(objects.ChannelCredentials{APIKey: "test-api-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	return ch
}

type urlTrackingExecutor struct {
	response    *httpclient.Response
	urlTracker *string
}

func (e *urlTrackingExecutor) Do(ctx context.Context, request *httpclient.Request) (*httpclient.Response, error) {
	*e.urlTracker = request.URL
	return e.response, nil
}

func (e *urlTrackingExecutor) DoStream(ctx context.Context, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	*e.urlTracker = request.URL
	return nil, nil
}
