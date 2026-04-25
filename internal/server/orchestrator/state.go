package orchestrator

import (
	"fmt"
	"sync"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

// HedgePhase tracks the lifecycle phase of hedge execution.
// Uses mutex-protected transitions since hedge state is accessed concurrently.
type HedgePhase int

const (
	// HedgeDisabled means hedging is not active for this request.
	HedgeDisabled HedgePhase = iota
	// HedgePrimaryOnly means primary candidate is running, secondary not yet launched.
	HedgePrimaryOnly
	// HedgeSecondaryLaunched means secondary candidate has been launched in parallel.
	HedgeSecondaryLaunched
	// HedgeObservationActive means both candidates are running, observing for winner.
	HedgeObservationActive
	// HedgeWinnerReleased means winner has been selected and released to client.
	HedgeWinnerReleased
	// HedgeLoserShadowing means loser is still running in shadow mode.
	HedgeLoserShadowing
	// HedgeShadowCompleted means shadow candidate finished successfully.
	HedgeShadowCompleted
	// HedgeShadowDeadlineExceeded means shadow candidate exceeded deadline.
	HedgeShadowDeadlineExceeded
	// HedgeFallbackResumed means fallback to remaining candidates is happening.
	HedgeFallbackResumed
)

func (p HedgePhase) String() string {
	switch p {
	case HedgeDisabled:
		return "HedgeDisabled"
	case HedgePrimaryOnly:
		return "HedgePrimaryOnly"
	case HedgeSecondaryLaunched:
		return "HedgeSecondaryLaunched"
	case HedgeObservationActive:
		return "HedgeObservationActive"
	case HedgeWinnerReleased:
		return "HedgeWinnerReleased"
	case HedgeLoserShadowing:
		return "HedgeLoserShadowing"
	case HedgeShadowCompleted:
		return "HedgeShadowCompleted"
	case HedgeShadowDeadlineExceeded:
		return "HedgeShadowDeadlineExceeded"
	case HedgeFallbackResumed:
		return "HedgeFallbackResumed"
	default:
		return "Unknown"
	}
}

// HedgeState tracks the state of hedge execution for a request.
type HedgeState struct {
	// Phase is the current lifecycle phase of hedge execution.
	Phase HedgePhase

	// PrimaryCandidateIndex is the index of the primary candidate in ChannelModelsCandidates.
	PrimaryCandidateIndex int
	// SecondaryCandidateIndex is the index of the secondary candidate.
	SecondaryCandidateIndex int

	// HedgeStartTime is when the hedge race started.
	HedgeStartTime time.Time
	// ObservationWindowStart is when the observation window opened.
	ObservationWindowStart time.Time
	// ObservationWindowEnd is when the observation window closes.
	ObservationWindowEnd time.Time

	// WinnerIndex indicates which candidate won (0=primary, 1=secondary, -1=undecided).
	WinnerIndex int
	// LoserIndex indicates which candidate lost.
	LoserIndex int

	// ShadowDeadline is the hard deadline for shadow completion.
	ShadowDeadline time.Time
	// ShadowCompletionReason describes why shadow completed: "completed", "deadline_exceeded",
	// "upstream_error", "server_shutdown", "client_disconnected".
	ShadowCompletionReason string

	// FallbackAllowed indicates whether fallback to remaining candidates is still possible.
	FallbackAllowed bool

	// hedgeMu protects hedge state transitions.
	hedgeMu sync.Mutex
}

// TransitionToSecondaryLaunched transitions from HedgePrimaryOnly to HedgeSecondaryLaunched.
// Returns error if current phase is not HedgePrimaryOnly.
func (h *HedgeState) TransitionToSecondaryLaunched() error {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()

	if h.Phase != HedgePrimaryOnly {
		return fmt.Errorf("invalid transition: cannot transition to SecondaryLaunched from %s", h.Phase)
	}
	h.Phase = HedgeSecondaryLaunched
	return nil
}

// TransitionToObservationActive transitions from HedgeSecondaryLaunched to HedgeObservationActive.
// Returns error if current phase is not HedgeSecondaryLaunched.
func (h *HedgeState) TransitionToObservationActive() error {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()

	if h.Phase != HedgeSecondaryLaunched {
		return fmt.Errorf("invalid transition: cannot transition to ObservationActive from %s", h.Phase)
	}
	h.Phase = HedgeObservationActive
	return nil
}

// TransitionToWinnerReleased transitions from HedgeObservationActive to HedgeWinnerReleased.
// The winnerIndex must be 0 (primary) or 1 (secondary).
// Returns error if current phase is not HedgeObservationActive or winnerIndex is invalid.
func (h *HedgeState) TransitionToWinnerReleased(winnerIndex int) error {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()

	if h.Phase != HedgeObservationActive {
		return fmt.Errorf("invalid transition: cannot transition to WinnerReleased from %s", h.Phase)
	}
	if winnerIndex != 0 && winnerIndex != 1 {
		return fmt.Errorf("invalid winner index: must be 0 or 1, got %d", winnerIndex)
	}
	h.Phase = HedgeWinnerReleased
	h.WinnerIndex = winnerIndex
	// Loser is the other candidate
	if winnerIndex == 0 {
		h.LoserIndex = 1
	} else {
		h.LoserIndex = 0
	}
	return nil
}

// TransitionToLoserShadowing transitions from HedgeWinnerReleased to HedgeLoserShadowing.
// Returns error if current phase is not HedgeWinnerReleased.
func (h *HedgeState) TransitionToLoserShadowing() error {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()

	if h.Phase != HedgeWinnerReleased {
		return fmt.Errorf("invalid transition: cannot transition to LoserShadowing from %s", h.Phase)
	}
	h.Phase = HedgeLoserShadowing
	return nil
}

// TransitionToShadowCompleted transitions from HedgeLoserShadowing to HedgeShadowCompleted.
// The reason must be one of: "completed", "deadline_exceeded", "upstream_error",
// "server_shutdown", "client_disconnected".
// Returns error if current phase is not HedgeLoserShadowing or reason is invalid.
func (h *HedgeState) TransitionToShadowCompleted(reason string) error {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()

	if h.Phase != HedgeLoserShadowing {
		return fmt.Errorf("invalid transition: cannot transition to ShadowCompleted from %s", h.Phase)
	}
	validReasons := map[string]bool{
		"completed":         true,
		"deadline_exceeded": true,
		"upstream_error":    true,
		"server_shutdown":   true,
		"client_disconnected": true,
	}
	if !validReasons[reason] {
		return fmt.Errorf("invalid shadow completion reason: %s", reason)
	}
	h.Phase = HedgeShadowCompleted
	h.ShadowCompletionReason = reason
	return nil
}

// TransitionToShadowDeadlineExceeded transitions from HedgeLoserShadowing to HedgeShadowDeadlineExceeded.
// Returns error if current phase is not HedgeLoserShadowing.
func (h *HedgeState) TransitionToShadowDeadlineExceeded() error {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()

	if h.Phase != HedgeLoserShadowing {
		return fmt.Errorf("invalid transition: cannot transition to ShadowDeadlineExceeded from %s", h.Phase)
	}
	h.Phase = HedgeShadowDeadlineExceeded
	h.ShadowCompletionReason = "deadline_exceeded"
	return nil
}

// TransitionToFallbackResumed transitions from any terminal state to HedgeFallbackResumed.
// This allows fallback to resume when hedge has concluded.
// Returns error if transition is not allowed from current phase.
func (h *HedgeState) TransitionToFallbackResumed() error {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()

	// Allow transition from terminal hedge states
	switch h.Phase {
	case HedgeShadowCompleted, HedgeShadowDeadlineExceeded:
		// OK
	case HedgeWinnerReleased:
		// Winner released but fallback still needed
		// OK
	default:
		return fmt.Errorf("invalid transition: cannot transition to FallbackResumed from %s", h.Phase)
	}
	h.Phase = HedgeFallbackResumed
	h.FallbackAllowed = true
	return nil
}

// IsHedgeActive returns true if the hedge phase is greater than HedgeDisabled.
func (h *HedgeState) IsHedgeActive() bool {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()
	return h.Phase > HedgeDisabled
}

// IsObservationActive returns true if the current phase is HedgeObservationActive.
func (h *HedgeState) IsObservationActive() bool {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()
	return h.Phase == HedgeObservationActive
}

// IsShadowActive returns true if the current phase is HedgeLoserShadowing.
func (h *HedgeState) IsShadowActive() bool {
	h.hedgeMu.Lock()
	defer h.hedgeMu.Unlock()
	return h.Phase == HedgeLoserShadowing
}

// StreamReleaseState tracks whether client-visible data has been released.
// Uses mutex-protected transitions since stream state is accessed concurrently.
type StreamReleaseState int32

const (
	// ReleaseNone means no buffering started yet, no client data sent.
	ReleaseNone StreamReleaseState = iota
	// ReleaseBuffering means chunks are being buffered, no client data sent yet.
	ReleaseBuffering
	// ReleaseEmitted means first flush has happened, client has seen data.
	// Once emitted, retry is forbidden for streaming requests.
	ReleaseEmitted
	// ReleaseForbidden means release happened, retry is now impossible.
	ReleaseForbidden
)

func (s StreamReleaseState) String() string {
	switch s {
	case ReleaseNone:
		return "ReleaseNone"
	case ReleaseBuffering:
		return "ReleaseBuffering"
	case ReleaseEmitted:
		return "ReleaseEmitted"
	case ReleaseForbidden:
		return "ReleaseForbidden"
	default:
		return "Unknown"
	}
}

// StreamBufferingConfig holds buffering configuration for streaming requests.
type StreamBufferingConfig struct {
	// Enabled indicates whether streaming buffering is active.
	Enabled bool
	// ChunkThreshold is the number of chunks to buffer before first release.
	// Default is 16.
	ChunkThreshold int
	// TimerDuration is the maximum time to buffer after TTFT before forced release.
	// Default is 3 seconds.
	TimerDuration time.Duration
}

// DefaultStreamBufferingConfig returns the default streaming buffering configuration.
// Returns Enabled: true, ChunkThreshold: 16, TimerDuration: 3 seconds.
func DefaultStreamBufferingConfig() StreamBufferingConfig {
	return StreamBufferingConfig{
		Enabled:        true,
		ChunkThreshold: 16,
		TimerDuration:  3 * time.Second,
	}
}

// DisabledStreamBufferingConfig returns a disabled buffering configuration.
func DisabledStreamBufferingConfig() StreamBufferingConfig {
	return StreamBufferingConfig{
		Enabled: false,
	}
}

// PersistenceState holds shared state with channel management and retry capabilities.
// TODO: move the dependencies out of the state to make it a real state.
type PersistenceState struct {
	APIKey *ent.APIKey

	RequestService      *biz.RequestService
	UsageLogService     *biz.UsageLogService
	ChannelService      *biz.ChannelService
	PromptProvider      PromptProvider
	PromptProtecter     PromptProtecter
	RetryPolicyProvider RetryPolicyProvider
	HedgePolicy        *biz.HedgePolicy
	HedgeState        *HedgeState
	CandidateSelector   CandidateSelector
	LoadBalancer        *LoadBalancer

	// Request state
	ModelMapper *ModelMapper
	// Proxy config, will be used to override channel's default proxy config.
	Proxy *httpclient.ProxyConfig

	// OriginalModel is the model after API key profile mapping, used for channel selection
	OriginalModel string
	RawRequest    *httpclient.Request
	LlmRequest    *llm.Request

	// RequestedModelRaw is the original model string from the user's request.
	// This is captured at OnInboundLlmRequest before any mapping/transformation,
	// ensuring metrics are labeled by the model the user requested, not the
	// actual model that was ultimately used.
	RequestedModelRaw string

	// Persistence state
	Request     *ent.Request
	RequestExec *ent.RequestExecution

	// ChannelModelsCandidates is the primary state for channel selection
	ChannelModelsCandidates []*ChannelModelsCandidate
	// Candidate state - current candidate index of ChannelModelsCandidates
	CurrentCandidateIndex int
	// CurrentCandidate is the currently selected candidate of ChannelModelsCandidates
	CurrentCandidate *ChannelModelsCandidate
	// CurrentModelIndex is the current model index in CurrentCandidate.Models
	CurrentModelIndex int

	// HedgeCandidates is the top-2 candidates for hedge dispatch, nil if hedge not applicable
	HedgeCandidates *HedgeCandidateSet

	// HedgeProtocol is the streaming protocol for hedge-eligible requests.
	// Set by GetHedgeProtocol during hedge eligibility check.
	HedgeProtocol HedgeProtocol

	// Perf is the performance record for the current request.
	Perf *biz.PerformanceRecord

	// LivePreview controls whether live stream preview is enabled for this connection.
	// When true, active streams register their chunk slices to biz.DefaultStreamPreviewRegistry.
	LivePreview bool

	// StoreChunks controls whether response chunks are persisted at stream close.
	StoreChunks bool

	// StreamCompleted tracks whether the stream has response successfully completed.
	// This is used to distinguish between a stream that was canceled mid-way
	// versus a stream that completed successfully but the client disconnected
	// immediately after receiving the last chunk.
	StreamCompleted bool

	// StreamReleaseState tracks whether client-visible data has been released.
	// This is used by retry logic to determine if fallback is allowed.
	StreamReleaseState StreamReleaseState

	// StreamBufferingConfig holds the buffering configuration for streaming requests.
	StreamBufferingConfig StreamBufferingConfig

	// streamReleaseMu protects StreamReleaseState transitions.
	streamReleaseMu sync.Mutex
}

// MarkStreamReleased atomically transitions from ReleaseBuffering to ReleaseEmitted.
// Idempotent: if already ReleaseEmitted or ReleaseForbidden, this is a no-op.
func (s *PersistenceState) MarkStreamReleased() {
	s.streamReleaseMu.Lock()
	defer s.streamReleaseMu.Unlock()

	if s.StreamReleaseState == ReleaseBuffering {
		s.StreamReleaseState = ReleaseEmitted
	}
}

// MarkReleaseForbidden transitions to ReleaseForbidden state.
// This is called when any client data is sent, making retry impossible.
func (s *PersistenceState) MarkReleaseForbidden() {
	s.streamReleaseMu.Lock()
	defer s.streamReleaseMu.Unlock()

	s.StreamReleaseState = ReleaseForbidden
}

// MarkStreamBuffering transitions from ReleaseNone to ReleaseBuffering state.
// This marks the start of streaming buffering - enables the CanRetryStream() gate in retry logic.
func (s *PersistenceState) MarkStreamBuffering() {
	s.streamReleaseMu.Lock()
	defer s.streamReleaseMu.Unlock()

	if s.StreamReleaseState == ReleaseNone {
		s.StreamReleaseState = ReleaseBuffering
	}
}

// IsStreamReleased returns true if state is ReleaseEmitted or ReleaseForbidden.
func (s *PersistenceState) IsStreamReleased() bool {
	s.streamReleaseMu.Lock()
	defer s.streamReleaseMu.Unlock()
	state := s.StreamReleaseState
	return state == ReleaseEmitted || state == ReleaseForbidden
}

// GetStreamReleaseState returns the current stream release state for logging purposes.
// This method holds the mutex during the read to ensure consistent state visibility.
func (s *PersistenceState) GetStreamReleaseState() StreamReleaseState {
	s.streamReleaseMu.Lock()
	defer s.streamReleaseMu.Unlock()
	return s.StreamReleaseState
}

// CanRetryStream returns true ONLY if StreamReleaseState is ReleaseNone or ReleaseBuffering.
// Returns false if any client-visible data has been sent.
func (s *PersistenceState) CanRetryStream() bool {
	s.streamReleaseMu.Lock()
	defer s.streamReleaseMu.Unlock()
	state := s.StreamReleaseState
	return state == ReleaseNone || state == ReleaseBuffering
}

// CloneForSecondary creates a copy of the state for secondary candidate execution.
// It properly handles mutex fields by zero-initializing them (they are unlocked).
// Pointer/reference fields are shared between primary and secondary (services, load balancer,
// candidates, etc.) which is correct - the secondary needs its own release tracking state
// and hedge coordination is managed by the primary state.
func (s *PersistenceState) CloneForSecondary(secondaryCandidate *ChannelModelsCandidate) *PersistenceState {
	clone := &PersistenceState{
		APIKey:                    s.APIKey,
		RequestService:            s.RequestService,
		UsageLogService:            s.UsageLogService,
		ChannelService:            s.ChannelService,
		PromptProvider:            s.PromptProvider,
		PromptProtecter:            s.PromptProtecter,
		RetryPolicyProvider:       s.RetryPolicyProvider,
		HedgePolicy:               s.HedgePolicy,
		HedgeState:                nil, // Secondary doesn't track hedge state
		CandidateSelector:         s.CandidateSelector,
		LoadBalancer:              s.LoadBalancer,
		ModelMapper:               s.ModelMapper,
		Proxy:                     s.Proxy,
		OriginalModel:             s.OriginalModel,
		RawRequest:                s.RawRequest,
		LlmRequest:                s.LlmRequest,
		RequestedModelRaw:         s.RequestedModelRaw,
		Request:                   s.Request,
		RequestExec:               nil,
		ChannelModelsCandidates:   s.ChannelModelsCandidates,
		CurrentCandidateIndex:     1,
		CurrentCandidate:          secondaryCandidate,
		CurrentModelIndex:         0,
		HedgeCandidates:           s.HedgeCandidates,
		HedgeProtocol:             s.HedgeProtocol,
		Perf:                      nil, // Secondary doesn't need its own performance record
		LivePreview:               s.LivePreview,
		StoreChunks:               s.StoreChunks,
		StreamCompleted:           false,
		StreamReleaseState:        ReleaseNone, // Secondary starts fresh
		StreamBufferingConfig:     s.StreamBufferingConfig,
		// streamReleaseMu is intentionally omitted - zero value is valid/unlocked
	}
	return clone
}
