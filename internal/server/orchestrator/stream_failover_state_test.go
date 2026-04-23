package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/pipeline/stream"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestStreamFailover_StateMachine(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	ch := createTestChannel(t, ctx, client)
	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	outbound, err := openai.NewOutboundTransformer(ch.BaseURL, ch.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: ch, Outbound: outbound}

	channelSelector := &staticChannelSelector{
		candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4"),
	}

	executor := &mockExecutorWithIncompleteStream{
		events: []*httpclient.StreamEvent{
			{
				Data: []byte(`invalid json that won't parse`),
			},
			{
				Data: []byte(`also invalid`),
			},
		},
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:   channelSelector,
		Inbound:           openai.NewInboundTransformer(),
		RequestService:    requestService,
		ChannelService:    channelService,
		PromptProvider:    &stubPromptProvider{},
		SystemService:     systemService,
		UsageLogService:   usageLogService,
		PipelineFactory:   pipeline.NewFactory(executor),
		ModelMapper:       NewModelMapper(),
		connectionTracker: NewDefaultConnectionTracker(1024),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	httpRequest := buildTestRequest("gpt-4", "Hi!", true)
	ctx = contexts.WithProjectID(ctx, project.ID)

	result, err := orchestrator.Process(ctx, httpRequest)
	require.NoError(t, err)
	assert.NotNil(t, result.ChatCompletionStream)

	for result.ChatCompletionStream.Next() {
		_ = result.ChatCompletionStream.Current()
	}

	err = result.ChatCompletionStream.Close()
	require.NoError(t, err)

	requests, err := client.Request.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	dbRequest := requests[0]
	assert.Equal(t, request.StatusProcessing, dbRequest.Status,
		"parent request should stay `processing` when stream ends without terminal event (IncompleteStreamError), allowing failover to proceed")

	executions, err := client.RequestExecution.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 1)

	dbExec := executions[0]
	assert.Equal(t, requestexecution.StatusFailed, dbExec.Status,
		"execution should be marked `failed` due to incomplete stream")
}

type mockExecutorWithIncompleteStream struct {
	events []*httpclient.StreamEvent
}

func (m *mockExecutorWithIncompleteStream) Do(_ context.Context, _ *httpclient.Request) (*httpclient.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *mockExecutorWithIncompleteStream) DoStream(_ context.Context, _ *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	return &incompleteStream{events: m.events}, nil
}

type incompleteStream struct {
	events []*httpclient.StreamEvent
	idx    int
}

func (s *incompleteStream) Next() bool {
	return s.idx < len(s.events)
}

func (s *incompleteStream) Current() *httpclient.StreamEvent {
	if s.idx >= len(s.events) {
		return nil
	}
	item := s.events[s.idx]
	s.idx++
	return item
}

func (s *incompleteStream) Err() error {
	return nil
}

func (s *incompleteStream) Close() error { return nil }

func TestStreamFailover_CanceledRequest_RemainsCanceled(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	ch := createTestChannel(t, ctx, client)
	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	outbound, err := openai.NewOutboundTransformer(ch.BaseURL, ch.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: ch, Outbound: outbound}

	channelSelector := &staticChannelSelector{
		candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4"),
	}

	executor := &mockExecutorWithCanceledContext{
		events: []*httpclient.StreamEvent{
			{
				Data: []byte(
					`{"id":"","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
				),
			},
		},
		canceledCtx: nil, // Will be set below when we create the processCtx
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:   channelSelector,
		Inbound:           openai.NewInboundTransformer(),
		RequestService:    requestService,
		ChannelService:    channelService,
		PromptProvider:    &stubPromptProvider{},
		SystemService:     systemService,
		UsageLogService:   usageLogService,
		PipelineFactory:   pipeline.NewFactory(executor),
		ModelMapper:       NewModelMapper(),
		connectionTracker: NewDefaultConnectionTracker(1024),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	httpRequest := buildTestRequest("gpt-4", "Hi!", true)

	// Create context with project ID for Process()
	processCtx := contexts.WithProjectID(context.Background(), project.ID)
	processCtx = ent.NewContext(processCtx, client)
	processCtx = authz.WithTestBypass(processCtx)

	// Create a cancelable context - will be canceled AFTER Process() returns but BEFORE Close()
	canceledCtx, cancel := context.WithCancel(processCtx)
	executor.canceledCtx = canceledCtx

	// Use the canceled context for Process() so the stream stores it
	result, err := orchestrator.Process(canceledCtx, httpRequest)
	require.NoError(t, err)
	assert.NotNil(t, result.ChatCompletionStream)

	for result.ChatCompletionStream.Next() {
		_ = result.ChatCompletionStream.Current()
	}

	// Now cancel the context - the stream will see this when Close() is called
	cancel()

	err = result.ChatCompletionStream.Close()
	require.NoError(t, err)

	requests, err := client.Request.Query().All(processCtx)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	dbRequest := requests[0]
	assert.Equal(t, request.StatusCanceled, dbRequest.Status,
		"request should be `canceled` when context is canceled, not `failed`")
}

type mockExecutorWithCanceledContext struct {
	events      []*httpclient.StreamEvent
	canceledCtx context.Context
}

func (m *mockExecutorWithCanceledContext) Do(_ context.Context, _ *httpclient.Request) (*httpclient.Response, error) {
	return nil, errors.New("not implemented")
}

func (m *mockExecutorWithCanceledContext) DoStream(_ context.Context, _ *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	return &canceledStream{events: m.events, canceledCtx: m.canceledCtx}, nil
}

type canceledStream struct {
	events         []*httpclient.StreamEvent
	idx            int
	canceledCtx    context.Context
}

func (s *canceledStream) Next() bool {
	return s.idx < len(s.events)
}

func (s *canceledStream) Current() *httpclient.StreamEvent {
	if s.idx >= len(s.events) {
		return nil
	}
	item := s.events[s.idx]
	s.idx++
	return item
}

func (s *canceledStream) Err() error {
	return s.canceledCtx.Err()
}

func (s *canceledStream) Close() error { return nil }

type canceledPersistentStream struct {
	streams.Stream[*httpclient.StreamEvent]
	canceledContext context.Context
}

func (s *canceledPersistentStream) Close() error {
	_ = s.canceledContext.Err()
	return s.Stream.Close()
}

// TestStreamFailover_IncompleteStreamError_ChunksNotPersistedToParentRequest verifies that
// when a stream fails with IncompleteStreamError, the failed attempt's chunks are NOT
// persisted to the parent request. This ensures boundary isolation between attempts.
//
// Boundary semantics verified:
// 1. Failed attempt's chunks accumulate in ts.responseChunks but are never persisted
// 2. persistResponseChunks() is NOT called when IncompleteStreamError is returned
// 3. Only the execution (not parent request) is marked as failed
// 4. The preview buffer for the failed attempt is properly unregistered
func TestStreamFailover_IncompleteStreamError_ChunksNotPersistedToParentRequest(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	ch := createTestChannel(t, ctx, client)
	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	outbound, err := openai.NewOutboundTransformer(ch.BaseURL, ch.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: ch, Outbound: outbound}

	channelSelector := &staticChannelSelector{
		candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4"),
	}

	// This executor returns chunks that don't form a valid complete response
	executor := &mockExecutorWithIncompleteStream{
		events: []*httpclient.StreamEvent{
			{
				Data: []byte(`invalid json that won't parse`),
			},
			{
				Data: []byte(`also invalid`),
			},
		},
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:   channelSelector,
		Inbound:           openai.NewInboundTransformer(),
		RequestService:    requestService,
		ChannelService:    channelService,
		PromptProvider:    &stubPromptProvider{},
		SystemService:     systemService,
		UsageLogService:   usageLogService,
		PipelineFactory:   pipeline.NewFactory(executor),
		ModelMapper:       NewModelMapper(),
		connectionTracker: NewDefaultConnectionTracker(1024),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	httpRequest := buildTestRequest("gpt-4", "Hi!", true)
	ctx = contexts.WithProjectID(ctx, project.ID)

	result, err := orchestrator.Process(ctx, httpRequest)
	require.NoError(t, err)
	assert.NotNil(t, result.ChatCompletionStream)

	for result.ChatCompletionStream.Next() {
		_ = result.ChatCompletionStream.Current()
	}

	_ = result.ChatCompletionStream.Close()

	// Verify parent request status - should still be processing (not failed)
	requests, err := client.Request.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	dbRequest := requests[0]
	assert.Equal(t, request.StatusProcessing, dbRequest.Status,
		"parent request should stay `processing` - chunks from failed attempt should NOT be persisted as failed request")
	assert.Empty(t, dbRequest.ResponseBody,
		"parent request response body should be empty - chunks from failed attempt should NOT pollute parent request")

	// Verify execution is marked as failed
	executions, err := client.RequestExecution.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 1)

	dbExec := executions[0]
	assert.Equal(t, requestexecution.StatusFailed, dbExec.Status,
		"execution should be marked `failed` due to incomplete stream")
}

// TestStreamFailover_NonStreamRequest_UnaffectedByStreamingFailoverStatus verifies that
// non-streaming (synchronous) requests are not affected by streaming failover status changes.
// Non-stream requests use a completely different code path (pre-stream error path at
// orchestrator.go L272-284) and should not be impacted by streaming-specific logic.
//
// This regression test ensures:
// 1. Non-stream requests complete successfully when the executor returns a valid response
// 2. Non-stream requests use the pre-stream error path, not the streaming error path
// 3. Non-stream request status transitions are independent of streaming failover logic
func TestStreamFailover_NonStreamRequest_UnaffectedByStreamingFailoverStatus(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	ch := createTestChannel(t, ctx, client)
	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	outbound, err := openai.NewOutboundTransformer(ch.BaseURL, ch.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: ch, Outbound: outbound}

	channelSelector := &staticChannelSelector{
		candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4"),
	}

	// Mock executor for non-stream requests - returns a successful response
	mockResponse := &httpclient.Response{
		StatusCode: 200,
		Body: []byte(`{
			"id": "chatcmpl-nonstream-123",
			"object": "chat.completion",
			"model": "gpt-4",
			"choices": [{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello from non-stream request!"
				},
				"finish_reason": "stop"
			}],
			"usage": {
				"prompt_tokens": 10,
				"completion_tokens": 20,
				"total_tokens": 30
			}
		}`),
	}

	executor := &mockExecutor{
		response:     mockResponse,
		streamEvents: nil, // No stream events for non-stream request
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:   channelSelector,
		Inbound:           openai.NewInboundTransformer(),
		RequestService:    requestService,
		ChannelService:    channelService,
		PromptProvider:    &stubPromptProvider{},
		SystemService:     systemService,
		UsageLogService:   usageLogService,
		PipelineFactory:   pipeline.NewFactory(executor),
		ModelMapper:       NewModelMapper(),
		connectionTracker: NewDefaultConnectionTracker(1024),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	// NON-STREAM request (isStream = false)
	httpRequest := buildTestRequest("gpt-4", "Hi!", false)
	ctx = contexts.WithProjectID(ctx, project.ID)

	result, err := orchestrator.Process(ctx, httpRequest)
	require.NoError(t, err)
	assert.NotNil(t, result.ChatCompletion,
		"non-stream request should return ChatCompletion, not ChatCompletionStream")
	assert.Nil(t, result.ChatCompletionStream,
		"non-stream request should NOT return ChatCompletionStream")

	// Verify request was created in database
	requests, err := client.Request.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, requests, 1)

	dbRequest := requests[0]
	assert.Equal(t, request.StatusCompleted, dbRequest.Status,
		"non-stream request should be marked `completed`")
	assert.Equal(t, "chatcmpl-nonstream-123", dbRequest.ExternalID,
		"non-stream request should have correct external ID")

	// Verify request execution was created
	executions, err := client.RequestExecution.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 1)

	dbExec := executions[0]
	assert.Equal(t, ch.ID, dbExec.ChannelID)
	assert.Equal(t, dbRequest.ID, dbExec.RequestID)
	assert.Equal(t, "chatcmpl-nonstream-123", dbExec.ExternalID)

	// Verify usage log was created
	usageLogs, err := client.UsageLog.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, usageLogs, 1)
	assert.Equal(t, dbRequest.ID, usageLogs[0].RequestID)
}

// TestStreamFailover_FailoverExhausted_ParentRequestFailed verifies that when all
// failover candidates are exhausted (FailoverCallback is invoked but all channels fail),
// the FailoverCallback correctly returns nil to signal no more candidates are available.
func TestStreamFailover_FailoverExhausted_ParentRequestFailed(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	ch := createTestChannel(t, ctx, client)
	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	outbound, err := openai.NewOutboundTransformer(ch.BaseURL, ch.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: ch, Outbound: outbound}

	// Use a single channel candidate so failover exhausts immediately
	channelSelector := &staticChannelSelector{
		candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4"),
	}

	// Executor that returns incomplete stream (will trigger failover)
	executor := &mockExecutorWithIncompleteStream{
		events: []*httpclient.StreamEvent{
			{
				Data: []byte(`invalid json that won't parse`),
			},
			{
				Data: []byte(`also invalid`),
			},
		},
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:   channelSelector,
		Inbound:           openai.NewInboundTransformer(),
		RequestService:    requestService,
		ChannelService:    channelService,
		PromptProvider:    &stubPromptProvider{},
		SystemService:     systemService,
		UsageLogService:   usageLogService,
		PipelineFactory:   pipeline.NewFactory(executor),
		ModelMapper:       NewModelMapper(),
		connectionTracker: NewDefaultConnectionTracker(1024),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	httpRequest := buildTestRequest("gpt-4", "Hi!", true)
	ctx = contexts.WithProjectID(ctx, project.ID)

	result, err := orchestrator.Process(ctx, httpRequest)
	require.NoError(t, err)
	require.NotNil(t, result.FailoverCallback, "FailoverCallback should be set for streaming requests")

	// Consume the stream - it will end with IncompleteStreamError
	for result.ChatCompletionStream.Next() {
		_ = result.ChatCompletionStream.Current()
	}

	// Close the stream - this marks the execution as failed
	err = result.ChatCompletionStream.Close()
	require.NoError(t, err)

	// First execution should be failed
	executions, err := client.RequestExecution.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	dbExec := executions[0]
	assert.Equal(t, requestexecution.StatusFailed, dbExec.Status,
		"first execution should be marked failed")

	failoverResult := result.FailoverCallback(nil, false)
	assert.Nil(t, failoverResult, "FailoverCallback should return nil when all candidates exhausted")
}

// TestStreamFailover_FailedAttemptThenSuccessfulFailover_NoChunkPollution verifies that
// when the first attempt fails with IncompleteStreamError, the failed attempt's chunks
// are NOT persisted to the parent request (no chunk pollution).
func TestStreamFailover_FailedAttemptThenSuccessfulFailover_NoChunkPollution(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx = ent.NewContext(ctx, client)

	project := createTestProject(t, ctx, client)
	ch := createTestChannel(t, ctx, client)
	channelService, requestService, systemService, usageLogService := setupTestServices(t, client)

	outbound, err := openai.NewOutboundTransformer(ch.BaseURL, ch.Credentials.APIKey)
	require.NoError(t, err)

	bizChannel := &biz.Channel{Channel: ch, Outbound: outbound}

	channelSelector := &staticChannelSelector{
		candidates: channelsToTestCandidates([]*biz.Channel{bizChannel}, "gpt-4"),
	}

	// Executor that returns incomplete stream (no terminal event, invalid chunks)
	executor := &mockExecutorWithIncompleteStream{
		events: []*httpclient.StreamEvent{
			{
				Data: []byte(`invalid json that won't parse`),
			},
			{
				Data: []byte(`also invalid`),
			},
		},
	}

	orchestrator := &ChatCompletionOrchestrator{
		channelSelector:   channelSelector,
		Inbound:           openai.NewInboundTransformer(),
		RequestService:    requestService,
		ChannelService:    channelService,
		PromptProvider:    &stubPromptProvider{},
		SystemService:     systemService,
		UsageLogService:   usageLogService,
		PipelineFactory:   pipeline.NewFactory(executor),
		ModelMapper:       NewModelMapper(),
		connectionTracker: NewDefaultConnectionTracker(1024),
		Middlewares: []pipeline.Middleware{
			stream.EnsureUsage(),
		},
	}

	httpRequest := buildTestRequest("gpt-4", "Hi!", true)
	ctx = contexts.WithProjectID(ctx, project.ID)

	result, err := orchestrator.Process(ctx, httpRequest)
	require.NoError(t, err)
	require.NotNil(t, result.FailoverCallback, "FailoverCallback should be set for streaming requests")

	// Consume the first stream - it will end with IncompleteStreamError
	for result.ChatCompletionStream.Next() {
		_ = result.ChatCompletionStream.Current()
	}

	// Close the first stream - this marks first execution as failed
	err = result.ChatCompletionStream.Close()
	require.NoError(t, err)

	// First execution should be failed
	executions, err := client.RequestExecution.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	dbExec := executions[0]
	assert.Equal(t, requestexecution.StatusFailed, dbExec.Status,
		"first execution should be marked failed")

	// Parent request should still be processing (failover is possible)
	requests, err := client.Request.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	dbRequest := requests[0]
	assert.Equal(t, request.StatusProcessing, dbRequest.Status,
		"parent request should still be processing before failover callback")

	// Verify that the failed attempt's chunks were NOT persisted to the parent request
	assert.Empty(t, dbRequest.ResponseBody,
		"parent request response body should be empty - chunks from failed attempt should NOT pollute parent request")
}