package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/pkg/xcontext"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/pipeline/cc"
	"github.com/looplj/axonhub/llm/pipeline/stream"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
)

func NewChatCompletionOrchestrator(
	channelService *biz.ChannelService,
	defaultSelector *DefaultSelector,
	requestService *biz.RequestService,
	httpClient *httpclient.HttpClient,
	inbound transformer.Inbound,
	systemService *biz.SystemService,
	usageLogService *biz.UsageLogService,
	promptService *biz.PromptService,
	quotaService *biz.QuotaService,
	promptProtectionRuleService *biz.PromptProtectionRuleService,
) *ChatCompletionOrchestrator {
	connectionTracker := NewDefaultConnectionTracker(256)
	rateLimitTracker := NewChannelRequestTracker()

	// Initialize model circuit breaker
	modelCircuitBreaker := biz.NewModelCircuitBreaker()

	rateLimitStrategy := NewRateLimitAwareStrategy(rateLimitTracker, connectionTracker)

	adaptiveLoadBalancer := NewLoadBalancer(systemService, channelService,
		NewStickyRoutingStrategy(channelService),
		NewErrorAwareStrategy(channelService),
		NewWeightRoundRobinStrategy(channelService),
		NewLatencyAwareStrategy(channelService),
		rateLimitStrategy,
	)

	failoverLoadBalancer := NewLoadBalancer(systemService, channelService,
		NewWeightStrategy(), NewRandomStrategy(), rateLimitStrategy)

	circuitBreakerLoadBalancer := NewLoadBalancer(systemService, channelService,
		NewWeightStrategy(), NewModelAwareCircuitBreakerStrategy(modelCircuitBreaker), rateLimitStrategy)

	return &ChatCompletionOrchestrator{
		Inbound:         inbound,
		RequestService:  requestService,
		ChannelService:  channelService,
		SystemService:   systemService,
		UsageLogService: usageLogService,
		QuotaService:    quotaService,
		PromptProvider:  promptService,
		PromptProtecter: promptProtectionRuleService,
		Middlewares: []pipeline.Middleware{
			cc.StripBillingHeaderCCH(),
			stream.EnsureUsage(),
		},
		PipelineFactory:            pipeline.NewFactory(httpClient),
		ModelMapper:                NewModelMapper(),
		channelSelector:            defaultSelector,
		connectionTracker:          connectionTracker,
		rateLimitTracker:           rateLimitTracker,
		adaptiveLoadBalancer:       adaptiveLoadBalancer,
		failoverLoadBalancer:       failoverLoadBalancer,
		circuitBreakerLoadBalancer: circuitBreakerLoadBalancer,
		modelCircuitBreaker:        modelCircuitBreaker,
		proxy:                      nil,
	}
}

type ChatCompletionOrchestrator struct {
	Inbound         transformer.Inbound
	RequestService  *biz.RequestService
	ChannelService  *biz.ChannelService
	SystemService   *biz.SystemService
	UsageLogService *biz.UsageLogService
	QuotaService    *biz.QuotaService
	PromptProvider  PromptProvider
	PromptProtecter PromptProtecter
	Middlewares     []pipeline.Middleware
	PipelineFactory *pipeline.Factory
	ModelMapper     *ModelMapper

	// The runtime fields.

	// The default channel selector.
	channelSelector CandidateSelector
	// The load balancer for channel load balancing.
	adaptiveLoadBalancer       *LoadBalancer
	failoverLoadBalancer       *LoadBalancer
	circuitBreakerLoadBalancer *LoadBalancer
	// The connection tracker used for request lifetime tracking and rate-limit concurrency fallback.
	connectionTracker ConnectionTracker
	// The rate limit tracker for rate limit aware load balancing.
	rateLimitTracker *ChannelRequestTracker
	// The model circuit breaker for circuit-breaker load balancing.
	modelCircuitBreaker *biz.ModelCircuitBreaker

	// proxy is the proxy configuration for testing
	// If set, it will override the channel's default proxy configuration
	proxy *httpclient.ProxyConfig
}

func (processor *ChatCompletionOrchestrator) WithChannelSelector(selector CandidateSelector) *ChatCompletionOrchestrator {
	c := *processor
	c.channelSelector = selector

	return &c
}

func (processor *ChatCompletionOrchestrator) WithAllowedChannels(allowedChannelIDs []int) *ChatCompletionOrchestrator {
	c := *processor
	c.channelSelector = WithSelectedChannelsSelector(processor.channelSelector, allowedChannelIDs)

	return &c
}

func (processor *ChatCompletionOrchestrator) WithProxy(proxy *httpclient.ProxyConfig) *ChatCompletionOrchestrator {
	c := *processor
	c.proxy = proxy

	return &c
}

type ChatCompletionResult struct {
	ChatCompletion       *httpclient.Response
	ChatCompletionStream streams.Stream[*httpclient.StreamEvent]

	// ReleaseMarkCallback is called when the first SSE event is written to the client.
	// This marks the point at which client-visible data has been released.
	ReleaseMarkCallback func()

	// HedgeProtocol indicates the streaming protocol if hedge is active.
	// This is set by the orchestrator during hedge-eligible request processing.
	HedgeProtocol HedgeProtocol
}

func (processor *ChatCompletionOrchestrator) Process(ctx context.Context, request *httpclient.Request) (ChatCompletionResult, error) {
	// The context is system bypassed to allow the orchestrator to access the system settings.
	ctx = authz.WithSystemBypass(ctx, "process-chat-completion")

	apiKey, _ := contexts.GetAPIKey(ctx)

	// Get retry policy from system settings
	retryPolicy := processor.SystemService.RetryPolicyOrDefault(ctx)
	storagePolicy := processor.SystemService.StoragePolicyOrDefault(ctx)

	strategy := deriveLoadBalancerStrategy(retryPolicy, apiKey)
	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "chat request received",
			log.String("request_body", string(request.Body)),
			log.Any("request_headers", request.Headers),
			log.Any("retry_policy", retryPolicy),
			log.String("system_load_balance_strategy", retryPolicy.LoadBalancerStrategy),
			log.String("load_balance_strategy", strategy),
		)
	}

	loadBalancer := processor.adaptiveLoadBalancer

	switch strategy {
	case biz.LoadBalancerStrategyAdaptive:
		loadBalancer = processor.adaptiveLoadBalancer
	case biz.LoadBalancerStrategyFailover:
		loadBalancer = processor.failoverLoadBalancer
	case biz.LoadBalancerStrategyCircuitBreaker:
		loadBalancer = processor.circuitBreakerLoadBalancer
	default:
		// Default to adaptive load balancer
	}

	state := &PersistenceState{
		APIKey:                apiKey,
		RequestService:        processor.RequestService,
		UsageLogService:       processor.UsageLogService,
		ChannelService:        processor.ChannelService,
		PromptProvider:        processor.PromptProvider,
		PromptProtecter:       processor.PromptProtecter,
		RetryPolicyProvider:   processor.SystemService,
		HedgePolicy:           processor.SystemService.HedgePolicyOrDefault(ctx),
		CandidateSelector:     processor.channelSelector,
		LoadBalancer:          loadBalancer,
		ModelMapper:          processor.ModelMapper,
		Proxy:                 processor.proxy,
		LivePreview:           storagePolicy.LivePreview,
		StoreChunks:          storagePolicy.StoreChunks,
		CurrentCandidateIndex: 0,
		StreamBufferingConfig: DefaultStreamBufferingConfig(),
	}

	var pipelineOpts []pipeline.Option

	// Only apply retry if policy is enabled
	if retryPolicy.Enabled {
		pipelineOpts = append(pipelineOpts, pipeline.WithRetry(
			retryPolicy.MaxChannelRetries,
			retryPolicy.MaxSingleChannelRetries,
			time.Duration(retryPolicy.RetryDelayMs)*time.Millisecond,
		))
	}

	var middlewares []pipeline.Middleware

	// Add global middlewares
	middlewares = append(middlewares, processor.Middlewares...)

	inbound, outbound := NewPersistentTransformers(state, processor.Inbound)

	// Add inbound middlewares (executed after inbound.TransformRequest)
	middlewares = append(middlewares,
		enforceQuota(inbound, processor.QuotaService),
		checkApiKeyModelAccess(inbound),
		applyModelMapping(inbound),
		selectCandidates(inbound),
		injectPrompts(inbound),
		protectPrompts(inbound),
		persistRequest(inbound),
	)

	// Add outbound middlewares (executed after outbound.TransformRequest)
	middlewares = append(middlewares,
		applyOverrideRequestBody(outbound),
		// applyUserAgentPassThrough runs before header overrides to set the initial
		// User-Agent value (either from client pass-through or default "axonhub/1.0").
		// This allows override headers to modify the User-Agent if configured.
		applyUserAgentPassThrough(outbound, processor.SystemService),
		applyOverrideRequestHeaders(outbound),

		// Unified performance tracking middleware.
		withPerformanceRecording(outbound),

		// Stream buffering middleware - wraps LLM stream with buffering after TTFT is recorded.
		// Must be registered AFTER withPerformanceRecording so TTFT is recorded before buffering timer starts.
		withStreamBuffering(outbound),

		withModelCircuitBreaker(outbound, processor.modelCircuitBreaker, strategy),

		// The request execution middleware must be the final middleware
		// to ensure that the request execution is created with the correct request bodys.
		persistRequestExecution(outbound),

		// Rate limit tracking middleware for load balancing.
		withRateLimitTracking(outbound, processor.rateLimitTracker),
		// Connection tracking middleware for load balancing.
		withConnectionTracking(outbound, processor.connectionTracker),
	)

	pipelineOpts = append(pipelineOpts, pipeline.WithMiddlewares(middlewares...))

	pipe := processor.PipelineFactory.Pipeline(
		inbound,
		outbound,
		pipelineOpts...,
	)

	result, err := pipe.Process(ctx, request)
	if err != nil {
		persistCtx, cancel := xcontext.DetachWithTimeout(ctx, time.Second*10)
		defer cancel()

		// Update the last request execution status based on error if it exists
		// This ensures that when retry fails completely, the last execution is properly marked
		if requestExec := outbound.GetRequestExecution(); requestExec != nil {
			if updateErr := processor.RequestService.UpdateRequestExecutionStatusFromError(
				persistCtx,
				requestExec.ID,
				err,
			); updateErr != nil {
				log.Warn(persistCtx, "Failed to update request execution status from error", log.Cause(updateErr))
			}
		}

		// Update the main request status based on error
		if request := outbound.GetRequest(); request != nil {
			if updateErr := processor.RequestService.UpdateRequestStatusFromError(
				persistCtx,
				request.ID,
				err,
			); updateErr != nil {
				log.Warn(persistCtx, "Failed to update request status from error", log.Cause(updateErr))
			}
		}

		return ChatCompletionResult{}, err
	}

	// Return result based on stream type
	if result.Stream {
		// Check if hedge is eligible for this streaming request
		// Hedge eligibility is determined by hedge policy, candidate availability, and protocol
		if state.HedgeCandidates != nil &&
			state.HedgeCandidates.Primary != nil &&
			state.HedgeCandidates.Secondary != nil &&
			state.HedgeProtocol != HedgeProtocolNone {

			// Hedge is eligible - run the hedge race
			shouldHedge, isProbing := ShouldActivateHedge(
				ctx,
				request,
				state.HedgePolicy,
				state.HedgeCandidates,
			)

			if shouldHedge {
				winningStream, hedgeErr := processor.runHedgeRace(
					ctx,
					state,
					result.EventStream,
					state.HedgeCandidates.Primary,
					state.HedgeCandidates.Secondary,
					state.LlmRequest,
					state.HedgePolicy,
					isProbing,
				)

				if hedgeErr == nil && winningStream != nil {
					return ChatCompletionResult{
						ChatCompletion:       nil,
						ChatCompletionStream: winningStream,
						ReleaseMarkCallback:  state.MarkStreamReleased,
						HedgeProtocol:        state.HedgeProtocol,
					}, nil
				}

				// Hedge race failed, fall back to normal stream
				if hedgeErr != nil {
					log.Warn(ctx, "hedge race failed, falling back to normal stream", log.Cause(hedgeErr))
				}
			}
		}

		return ChatCompletionResult{
			ChatCompletion:       nil,
			ChatCompletionStream: result.EventStream,
			ReleaseMarkCallback:  state.MarkStreamReleased,
			HedgeProtocol:        state.HedgeProtocol,
		}, nil
	}

	return ChatCompletionResult{
		ChatCompletion:       result.Response,
		ChatCompletionStream: nil,
		HedgeProtocol:        state.HedgeProtocol,
	}, nil
}

// runHedgeRace executes the hedge race for streaming requests.
// It runs primary and secondary candidates concurrently and returns the winning stream.
// The loser stream is consumed by the shadow consumer in background.
func (processor *ChatCompletionOrchestrator) runHedgeRace(
	ctx context.Context,
	state *PersistenceState,
	primaryStream streams.Stream[*httpclient.StreamEvent],
	primaryCandidate *ChannelModelsCandidate,
	secondaryCandidate *ChannelModelsCandidate,
	llmRequest *llm.Request,
	hedgePolicy *biz.HedgePolicy,
	isProbing bool,
) (streams.Stream[*httpclient.StreamEvent], error) {
	// Generate hedge pair ID for linking winner/loser in persistence
	hedgePairID := fmt.Sprintf("hedge-%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)

	// Initialize hedge state
	state.HedgeState = &HedgeState{
		Phase:                   HedgePrimaryOnly,
		PrimaryCandidateIndex:    0,
		SecondaryCandidateIndex:  1,
		HedgeStartTime:          time.Now(),
	}

	// Build hedge coordinator config from policy
	coordinatorConfig := HedgeCoordinatorConfig{
		HedgeTriggerDelay: time.Duration(hedgePolicy.HedgeTriggerSeconds) * time.Second,
		ObservationWindow: time.Duration(hedgePolicy.ObservationWindowSeconds) * time.Second,
		IsProbingMode:     isProbing,
		HedgePairID:       hedgePairID,
		TimeNow:           time.Now,
	}

	// Build shadow consumer config from policy
	shadowConfig := ShadowConsumerConfig{
		ShadowDeadline:           time.Duration(hedgePolicy.ShadowHardDeadlineMinutes) * time.Minute,
		FullTextRetentionEnabled: hedgePolicy.FullShadowTextEnabled,
		HedgePairID:             hedgePairID,
		TimeNow:                 time.Now,
	}

	// Create observability recorder
	obsRecorder := NewHedgeObservabilityRecorder(hedgePairID)
	obsRecorder.RecordPrimaryLaunched(ctx, primaryCandidate.Channel.ID, primaryCandidate.Models[0].ActualModel)
	obsRecorder.RecordSecondaryLaunched(ctx, secondaryCandidate.Channel.ID, secondaryCandidate.Models[0].ActualModel, coordinatorConfig.IsProbingMode)

	// Create shadow consumer
	shadowConsumer := NewShadowConsumer(shadowConfig)

	// Execute secondary candidate to get secondary stream for the race
	// This must be done BEFORE calling StartRace so we have both streams ready
	secondaryState := state.CloneForSecondary(secondaryCandidate)

	secondaryStream, execErr := processor.executeSecondaryCandidate(ctx, secondaryState, secondaryCandidate, llmRequest)
	if execErr != nil {
		log.Debug(ctx, "secondary candidate execution failed for hedge race", log.Cause(execErr))
		// Fall back to primary stream on secondary execution failure
		LogHedgeObservationEnded(ctx, hedgePairID, 0, 0, -1)
		obsRecorder.RecordObservationEnded(ctx, 0, 0, -1)
		RecordHedgeRaceCompletion(primaryCandidate.Channel.ID, 0, HedgeOutcomeFallback)
		state.HedgeState = nil
		return primaryStream, nil
	}

	// Instantiate hedge coordinator and start the race
	coordinator := NewHedgeCoordinator(coordinatorConfig)

	// Record observation started
	LogHedgeObservationStarted(ctx, hedgePairID, "primary")
	obsRecorder.RecordObservationStarted(ctx, "primary")

	// Secondary has been launched via executeSecondaryCandidate before StartRace
	// Transition to SecondaryLaunched then ObservationActive
	state.HedgeState.TransitionToSecondaryLaunched()
	state.HedgeState.TransitionToObservationActive()

	// Start the race - this blocks until observation window ends and winner is selected
	raceResult, raceErr := coordinator.StartRace(ctx, primaryStream, secondaryStream)

	// Handle race errors gracefully by falling back to primary stream
	if raceErr != nil {
		log.Warn(ctx, "hedge race failed, falling back to primary stream", log.Cause(raceErr))
		LogHedgeObservationEnded(ctx, hedgePairID, 0, 0, -1)
		obsRecorder.RecordObservationEnded(ctx, 0, 0, -1)
		RecordHedgeRaceCompletion(primaryCandidate.Channel.ID, 0, HedgeOutcomeFallback)
		state.HedgeState = nil
		return primaryStream, nil
	}

	// Handle case where both streams failed during race
	if raceResult.BothFailed {
		log.Debug(ctx, "both streams failed during hedge race observation window")
		LogHedgeObservationEnded(ctx, hedgePairID, 0, 0, -1)
		obsRecorder.RecordObservationEnded(ctx, 0, 0, -1)
		RecordHedgeRaceCompletion(primaryCandidate.Channel.ID, secondaryCandidate.Channel.ID, HedgeOutcomeBothFailed)
		state.HedgeState = nil
		return primaryStream, nil
	}

	// Determine winner stream based on race result
	// raceResult.WinnerStream already wraps buffer + live stream properly
	winnerStream := raceResult.WinnerStream
	var primaryTPS, secondaryTPS float64

	// Get buffers from coordinator to compute TPS metrics
	primaryBuffer := coordinator.GetPrimaryBuffer()
	secondaryBuffer := coordinator.GetSecondaryBuffer()

	// Compute real TPS values using the observation window
	metricsResult := ComputeHedgeMetrics(primaryBuffer, secondaryBuffer, coordinatorConfig.ObservationWindow)
	primaryTPS = metricsResult.PrimaryTPS
	secondaryTPS = metricsResult.SecondaryTPS

	// Determine loser stream for shadow consumption - use the actual live stream, not buffer-only
	var loserStream streams.Stream[*httpclient.StreamEvent]
	if raceResult.LoserIndex == 0 {
		loserStream = raceResult.PrimaryStream
	} else {
		loserStream = raceResult.SecondaryStream
	}

	// Log observation ended with real TPS values
	LogHedgeObservationEnded(ctx, hedgePairID, primaryTPS, secondaryTPS, raceResult.WinnerIndex)
	obsRecorder.RecordObservationEnded(ctx, primaryTPS, secondaryTPS, raceResult.WinnerIndex)

	// Log winner chosen
	var winnerChannelID, loserChannelID int
	var winnerTPS, loserTPS float64
	if raceResult.WinnerIndex == 0 {
		winnerChannelID = primaryCandidate.Channel.ID
		loserChannelID = secondaryCandidate.Channel.ID
		winnerTPS = primaryTPS
		loserTPS = secondaryTPS
	} else {
		winnerChannelID = secondaryCandidate.Channel.ID
		loserChannelID = primaryCandidate.Channel.ID
		winnerTPS = secondaryTPS
		loserTPS = primaryTPS
	}
	LogHedgeWinnerChosen(ctx, hedgePairID, winnerChannelID, loserChannelID, winnerTPS, loserTPS)
	obsRecorder.RecordWinnerChosen(ctx, winnerChannelID, loserChannelID, winnerTPS, loserTPS)
	RecordHedgeRaceCompletion(winnerChannelID, loserChannelID, HedgeOutcomeWinnerReleased)

	// Record hedge metrics with real TPS values
	RecordHedgeMetrics(ctx, metricsResult, primaryCandidate, secondaryCandidate, nil, nil)

	// Transition to winner released
	state.HedgeState.TransitionToWinnerReleased(raceResult.WinnerIndex)

	// Start shadow consumption for loser in background goroutine
	// Use context.WithoutCancel so shadow continues even if client disconnects
	shadowCtx := context.WithoutCancel(ctx)
	go func() {
		shadowResult, shadowErr := shadowConsumer.StartShadow(shadowCtx, loserStream, hedgePairID)
		if shadowErr != nil {
			log.Debug(shadowCtx, "shadow consumer error", log.Cause(shadowErr))
		}
		if shadowResult != nil {
			LogHedgeShadowCompleted(shadowCtx, hedgePairID, string(shadowResult.CompletionReason), shadowResult.TotalTokensConsumed, shadowResult.Duration)
			obsRecorder.RecordShadowCompleted(shadowCtx, shadowResult.CompletionReason, shadowResult.TotalTokensConsumed, shadowResult.Duration)
		}
	}()

	// Log loser shadowed
	LogHedgeLoserShadowed(ctx, hedgePairID, loserChannelID)
	obsRecorder.RecordLoserShadowed(ctx, loserChannelID)

	// Transition to loser shadowing
	state.HedgeState.TransitionToLoserShadowing()

	// Return winner stream to client
	return winnerStream, nil
}

// executeSecondaryCandidate executes a single candidate for hedge shadow.
func (processor *ChatCompletionOrchestrator) executeSecondaryCandidate(
	ctx context.Context,
	state *PersistenceState,
	candidate *ChannelModelsCandidate,
	llmRequest *llm.Request,
) (streams.Stream[*httpclient.StreamEvent], error) {
	// Create transformers for this execution
	inbound, outbound := NewPersistentTransformers(state, processor.Inbound)

	// Transform request for this specific candidate
	httpReq, err := outbound.TransformRequest(ctx, llmRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to transform request for secondary candidate: %w", err)
	}

	// Get executor and customize it for this channel
	executor := outbound.CustomizeExecutor(processor.PipelineFactory.Executor)

	// Execute the stream
	rawStream, err := executor.DoStream(ctx, httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute secondary candidate: %w", err)
	}

	// Transform the stream through outbound transformer
	llmStream, err := outbound.TransformStream(ctx, rawStream)
	if err != nil {
		return nil, fmt.Errorf("failed to transform secondary stream: %w", err)
	}

	// Transform through inbound transformer
	inboundStream, err := inbound.TransformStream(ctx, llmStream)
	if err != nil {
		return nil, fmt.Errorf("failed to transform secondary inbound stream: %w", err)
	}

	return inboundStream, nil
}
