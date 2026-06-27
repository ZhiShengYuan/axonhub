package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	entrequest "github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestProviderAffinityStore_returns_provider_when_scope_and_session_match(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(4)
	candidate := candidateWithProvider(channel.TypeOpenai)

	// When
	store.Set("api_key:1:project:2", "session-123", ProviderKey(candidate))
	got, ok := store.Get("api_key:1:project:2", "session-123")

	// Then
	require.True(t, ok)
	require.Equal(t, string(channel.TypeOpenai), got)
}

func TestProviderAffinityStore_isolates_same_session_id_by_scope(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(4)

	// When
	store.Set("api_key:1:project:2", "session-123", string(channel.TypeOpenai))
	store.Set("api_key:2:project:2", "session-123", string(channel.TypeAnthropic))
	firstScopeProvider, firstScopeOK := store.Get("api_key:1:project:2", "session-123")
	secondScopeProvider, secondScopeOK := store.Get("api_key:2:project:2", "session-123")

	// Then
	require.True(t, firstScopeOK)
	require.True(t, secondScopeOK)
	require.Equal(t, string(channel.TypeOpenai), firstScopeProvider)
	require.Equal(t, string(channel.TypeAnthropic), secondScopeProvider)
}

func TestProviderAffinityStore_evicts_entries_in_insertion_order_when_bound_exceeded(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(2)

	// When
	store.Set("scope", "first", string(channel.TypeOpenai))
	store.Set("scope", "second", string(channel.TypeAnthropic))
	store.Set("scope", "third", string(channel.TypeGemini))

	// Then
	_, firstOK := store.Get("scope", "first")
	secondProvider, secondOK := store.Get("scope", "second")
	thirdProvider, thirdOK := store.Get("scope", "third")
	require.False(t, firstOK)
	require.True(t, secondOK)
	require.True(t, thirdOK)
	require.Equal(t, string(channel.TypeAnthropic), secondProvider)
	require.Equal(t, string(channel.TypeGemini), thirdProvider)
}

func TestProviderAffinityStore_SetGetDelete_removes_existing_entry(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(4)

	// When
	store.Set("scope", "session", string(channel.TypeOpenai))
	storedProvider, storedOK := store.Get("scope", "session")
	store.Delete("scope", "session")
	_, deletedOK := store.Get("scope", "session")

	// Then
	require.True(t, storedOK)
	require.Equal(t, string(channel.TypeOpenai), storedProvider)
	require.False(t, deletedOK)
}

func TestProviderAffinityStore_returns_not_ok_when_scope_or_session_empty(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(4)

	tests := []struct {
		name      string
		scope     string
		sessionID string
	}{
		{name: "empty scope", scope: "", sessionID: "session"},
		{name: "empty session", scope: "scope", sessionID: ""},
		{name: "both empty", scope: "", sessionID: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			store.Set(tt.scope, tt.sessionID, string(channel.TypeOpenai))
			_, ok := store.Get(tt.scope, tt.sessionID)

			// Then
			require.False(t, ok)
		})
	}
}

func TestProviderAffinityStore_is_safe_for_concurrent_access(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(128)
	var wg sync.WaitGroup

	// When
	for worker := range 16 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for item := range 64 {
				sessionID := fmt.Sprintf("session-%d-%d", worker, item)
				store.Set("scope", sessionID, string(channel.TypeOpenai))
				_, _ = store.Get("scope", sessionID)
			}
		}(worker)
	}
	wg.Wait()

	// Then
	store.Set("scope", "final", string(channel.TypeAnthropic))
	got, ok := store.Get("scope", "final")
	require.True(t, ok)
	require.Equal(t, string(channel.TypeAnthropic), got)
}

func TestProviderAffinityStore_WithScopedSessionID_reads_scope_and_session_from_context(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(4)
	ctx := shared.WithSessionScope(context.Background(), "api_key:1:project:2")
	ctx = shared.WithSessionID(ctx, "session-123")

	// When
	scope, sessionID, ok := store.WithScopedSessionID(ctx)

	// Then
	require.True(t, ok)
	require.Equal(t, "api_key:1:project:2", scope)
	require.Equal(t, "session-123", sessionID)
}

func TestProviderAffinityStore_WithScopedSessionID_returns_not_ok_when_context_is_incomplete(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(4)

	tests := []struct {
		name string
		ctx  context.Context
	}{
		{name: "missing scope", ctx: shared.WithSessionID(context.Background(), "session-123")},
		{name: "missing session", ctx: shared.WithSessionScope(context.Background(), "scope")},
		{name: "empty scope", ctx: shared.WithSessionID(shared.WithSessionScope(context.Background(), ""), "session-123")},
		{name: "empty session", ctx: shared.WithSessionID(shared.WithSessionScope(context.Background(), "scope"), "")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// When
			_, _, ok := store.WithScopedSessionID(tt.ctx)

			// Then
			require.False(t, ok)
		})
	}
}

func TestProviderAffinityOrdering_moves_preferred_provider_first_within_priority_group(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "api_key:1:project:2")
	ctx = shared.WithSessionID(ctx, "session-123")
	store := NewProviderAffinityStore(4)
	store.Set("api_key:1:project:2", "session-123", string(channel.TypeAnthropic))
	candidates := []*ChannelModelsCandidate{
		candidateWithProviderWeight(1, channel.TypeOpenai, 0, 100),
		candidateWithProviderWeight(2, channel.TypeAnthropic, 0, 10),
		candidateWithProviderWeight(3, channel.TypeGemini, 0, 1),
	}

	result := selectProviderAffinityOrder(t, ctx, store, retryPolicy(false, 0), candidates)

	require.Len(t, result, 1)
	require.Equal(t, channel.TypeAnthropic, result[0].Channel.Type)
}

func TestProviderAffinityOrdering_non_matching_session_leaves_order_unchanged(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "api_key:1:project:2")
	ctx = shared.WithSessionID(ctx, "session-123")
	store := NewProviderAffinityStore(4)
	store.Set("api_key:1:project:2", "different-session", string(channel.TypeAnthropic))
	candidates := []*ChannelModelsCandidate{
		candidateWithProviderWeight(1, channel.TypeOpenai, 0, 100),
		candidateWithProviderWeight(2, channel.TypeAnthropic, 0, 10),
		candidateWithProviderWeight(3, channel.TypeGemini, 0, 1),
	}

	result := selectProviderAffinityOrder(t, ctx, store, retryPolicy(false, 0), candidates)

	require.Len(t, result, 1)
	require.Equal(t, channel.TypeOpenai, result[0].Channel.Type)
}

func TestProviderAffinityOrdering_keeps_non_preferred_retry_candidate_when_truncating(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "api_key:1:project:2")
	ctx = shared.WithSessionID(ctx, "session-123")
	store := NewProviderAffinityStore(4)
	store.Set("api_key:1:project:2", "session-123", string(channel.TypeAnthropic))
	candidates := []*ChannelModelsCandidate{
		candidateWithProviderWeight(1, channel.TypeAnthropic, 0, 100),
		candidateWithProviderWeight(2, channel.TypeAnthropic, 0, 90),
		candidateWithProviderWeight(3, channel.TypeOpenai, 0, 10),
	}

	result := selectProviderAffinityOrder(t, ctx, store, retryPolicy(true, 1), candidates)

	require.Len(t, result, 2)
	require.Equal(t, channel.TypeAnthropic, result[0].Channel.Type)
	require.Equal(t, channel.TypeOpenai, result[1].Channel.Type)
}

func TestProviderAffinityOrdering_never_promotes_lower_priority_group(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "api_key:1:project:2")
	ctx = shared.WithSessionID(ctx, "session-123")
	store := NewProviderAffinityStore(4)
	store.Set("api_key:1:project:2", "session-123", string(channel.TypeAnthropic))
	candidates := []*ChannelModelsCandidate{
		candidateWithProviderWeight(1, channel.TypeOpenai, 0, 10),
		candidateWithProviderWeight(2, channel.TypeAnthropic, 1, 100),
	}

	result := selectProviderAffinityOrder(t, ctx, store, retryPolicy(true, 1), candidates)

	require.Len(t, result, 2)
	require.Equal(t, channel.TypeOpenai, result[0].Channel.Type)
	require.Equal(t, channel.TypeAnthropic, result[1].Channel.Type)
}

func TestProviderAffinityRebind_successful_non_stream_response_prefers_winning_provider(t *testing.T) {
	// Given
	ctx, requestService, reqExec := newProviderAffinityRequestExecution(t)
	ctx = scopedAffinityContext(ctx, "api_key:1:project:2", "session-123")
	store := NewProviderAffinityStore(4)
	state := &PersistenceState{
		RequestService:     requestService,
		RequestExec:        reqExec,
		ProviderAffinity:   store,
		CurrentCandidate:   candidateWithProvider(channel.TypeOpenai),
		RawProviderRequest: &httpclient.Request{},
	}
	middleware := &persistRequestExecutionMiddleware{
		outbound:    &PersistentOutboundTransformer{state: state},
		rawResponse: &httpclient.Response{StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"id":"resp-a"}`)},
	}

	// When
	_, err := middleware.OnOutboundLlmResponse(ctx, &llm.Response{ID: "resp-a"})

	// Then
	require.NoError(t, err)
	ordered := selectProviderAffinityOrder(t, ctx, store, retryPolicy(false, 0), []*ChannelModelsCandidate{
		candidateWithProviderWeight(1, channel.TypeAnthropic, 0, 100),
		candidateWithProviderWeight(2, channel.TypeOpenai, 0, 1),
	})
	require.Len(t, ordered, 1)
	require.Equal(t, channel.TypeOpenai, ordered[0].Channel.Type)
}

func TestProviderAffinityRebind_failed_non_stream_response_does_not_overwrite_existing_affinity(t *testing.T) {
	// Given
	ctx, requestService, _ := newProviderAffinityRequestExecution(t)
	ctx = scopedAffinityContext(ctx, "api_key:1:project:2", "session-123")
	store := NewProviderAffinityStore(4)
	store.Set("api_key:1:project:2", "session-123", string(channel.TypeOpenai))
	state := &PersistenceState{
		RequestService:   requestService,
		RequestExec:      &ent.RequestExecution{ID: -1},
		ProviderAffinity: store,
		CurrentCandidate: candidateWithProvider(channel.TypeAnthropic),
	}
	middleware := &persistRequestExecutionMiddleware{
		outbound:    &PersistentOutboundTransformer{state: state},
		rawResponse: &httpclient.Response{StatusCode: http.StatusOK, Headers: http.Header{"Content-Type": {"application/json"}}, Body: []byte(`{"id":"resp-b"}`)},
	}

	// When
	_, err := middleware.OnOutboundLlmResponse(ctx, &llm.Response{ID: "resp-b"})

	// Then
	require.NoError(t, err)
	storedProvider, ok := store.Get("api_key:1:project:2", "session-123")
	require.True(t, ok)
	require.Equal(t, string(channel.TypeOpenai), storedProvider)
}

func TestProviderAffinityRebind_fallback_stream_success_becomes_next_same_session_preference(t *testing.T) {
	// Given
	ctx := scopedAffinityContext(context.Background(), "api_key:1:project:2", "session-123")
	store := NewProviderAffinityStore(4)
	store.Set("api_key:1:project:2", "session-123", string(channel.TypeOpenai))
	state := &PersistenceState{
		ProviderAffinity: store,
		CurrentCandidate: candidateWithProvider(channel.TypeAnthropic),
	}
	stream := &sliceEventStream{events: []*httpclient.StreamEvent{{Type: "response.completed", Data: []byte(`{"type":"response.completed"}`)}}}
	persistentStream := NewOutboundPersistentStream(ctx, stream, nil, nil, nil, nil, &mockTransformer{}, nil, state)

	// When
	for persistentStream.Next() {
		_ = persistentStream.Current()
	}
	require.NoError(t, persistentStream.Close())

	// Then
	ordered := selectProviderAffinityOrder(t, ctx, store, retryPolicy(false, 0), []*ChannelModelsCandidate{
		candidateWithProviderWeight(1, channel.TypeOpenai, 0, 100),
		candidateWithProviderWeight(2, channel.TypeAnthropic, 0, 1),
	})
	require.Len(t, ordered, 1)
	require.Equal(t, channel.TypeAnthropic, ordered[0].Channel.Type)
}

func TestProviderAffinityRebind_aggregated_stream_success_records_provider(t *testing.T) {
	// Given
	ctx := scopedAffinityContext(context.Background(), "api_key:1:project:2", "session-123")
	store := NewProviderAffinityStore(4)
	state := &PersistenceState{
		ProviderAffinity: store,
		CurrentCandidate: candidateWithProvider(channel.TypeGemini),
	}
	stream := &sliceEventStream{events: []*httpclient.StreamEvent{{Type: "response.output_text.delta", Data: []byte(`{"delta":"hi"}`)}}}
	transformer := &mockTransformer{
		aggregatedResponse: []byte(`{"id":"resp-c","status":"completed"}`),
		aggregatedMeta: llm.ResponseMeta{
			ID: "resp-c",
			Usage: &llm.Usage{
				CompletionTokens: 1,
			},
		},
	}
	persistentStream := NewOutboundPersistentStream(ctx, stream, nil, nil, nil, nil, transformer, nil, state)

	// When
	for persistentStream.Next() {
		_ = persistentStream.Current()
	}
	require.NoError(t, persistentStream.Close())

	// Then
	storedProvider, ok := store.Get("api_key:1:project:2", "session-123")
	require.True(t, ok)
	require.Equal(t, string(channel.TypeGemini), storedProvider)
}

func TestProviderAffinityRebind_incomplete_stream_does_not_overwrite_existing_affinity(t *testing.T) {
	// Given
	ctx := scopedAffinityContext(context.Background(), "api_key:1:project:2", "session-123")
	store := NewProviderAffinityStore(4)
	store.Set("api_key:1:project:2", "session-123", string(channel.TypeOpenai))
	state := &PersistenceState{
		ProviderAffinity: store,
		CurrentCandidate: candidateWithProvider(channel.TypeAnthropic),
	}
	stream := &sliceEventStream{events: []*httpclient.StreamEvent{{Type: "response.in_progress", Data: []byte(`{"type":"response.in_progress"}`)}}}
	transformer := &mockTransformer{aggregatedResponse: []byte(`{"id":"resp-d","status":"in_progress"}`)}
	persistentStream := NewOutboundPersistentStream(ctx, stream, nil, nil, nil, nil, transformer, nil, state)

	// When
	for persistentStream.Next() {
		_ = persistentStream.Current()
	}
	require.NoError(t, persistentStream.Close())

	// Then
	storedProvider, ok := store.Get("api_key:1:project:2", "session-123")
	require.True(t, ok)
	require.Equal(t, string(channel.TypeOpenai), storedProvider)
}

func TestProviderAffinityRebind_different_scope_does_not_see_rebind(t *testing.T) {
	// Given
	store := NewProviderAffinityStore(4)
	successCtx := scopedAffinityContext(context.Background(), "api_key:1:project:2", "session-123")
	otherCtx := scopedAffinityContext(context.Background(), "api_key:2:project:2", "session-123")
	state := &PersistenceState{
		ProviderAffinity: store,
		CurrentCandidate: candidateWithProvider(channel.TypeAnthropic),
	}

	// When
	recordAffinityOnStreamSuccess(successCtx, state)

	// Then
	ordered := selectProviderAffinityOrder(t, otherCtx, store, retryPolicy(false, 0), []*ChannelModelsCandidate{
		candidateWithProviderWeight(1, channel.TypeOpenai, 0, 100),
		candidateWithProviderWeight(2, channel.TypeAnthropic, 0, 1),
	})
	require.Len(t, ordered, 1)
	require.Equal(t, channel.TypeOpenai, ordered[0].Channel.Type)
}

func candidateWithProvider(providerType channel.Type) *ChannelModelsCandidate {
	return candidateWithProviderWeight(0, providerType, 0, 0)
}

func scopedAffinityContext(ctx context.Context, scope string, sessionID string) context.Context {
	ctx = shared.WithSessionScope(ctx, scope)
	return shared.WithSessionID(ctx, sessionID)
}

func newProviderAffinityRequestExecution(t *testing.T) (context.Context, *biz.RequestService, *ent.RequestExecution) {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })

	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)
	ctx = ent.NewContext(ctx, client)
	project := createTestProject(t, ctx, client)
	ch := createTestChannel(t, ctx, client)
	_, requestService, _, _ := setupTestServices(t, client)

	req, err := client.Request.Create().
		SetProjectID(project.ID).
		SetChannelID(ch.ID).
		SetModelID("gpt-4").
		SetStatus(entrequest.StatusProcessing).
		SetRequestBody([]byte(`{"model":"gpt-4"}`)).
		Save(ctx)
	require.NoError(t, err)

	exec, err := client.RequestExecution.Create().
		SetRequestID(req.ID).
		SetProjectID(project.ID).
		SetChannelID(ch.ID).
		SetModelID("gpt-4").
		SetRequestBody([]byte(`{"model":"gpt-4"}`)).
		SetFormat(string(llm.APIFormatOpenAIChatCompletion)).
		SetStatus(requestexecution.StatusPending).
		Save(ctx)
	require.NoError(t, err)

	return ctx, requestService, exec
}

func candidateWithProviderWeight(id int, providerType channel.Type, priority int, weight int) *ChannelModelsCandidate {
	return &ChannelModelsCandidate{
		Channel: &biz.Channel{
			Channel: &ent.Channel{ID: id, Type: providerType, OrderingWeight: weight},
		},
		Priority: priority,
	}
}

func selectProviderAffinityOrder(
	t *testing.T,
	ctx context.Context,
	store *ProviderAffinityStore,
	policy *biz.RetryPolicy,
	candidates []*ChannelModelsCandidate,
) []*ChannelModelsCandidate {
	t.Helper()
	loadBalancer := NewLoadBalancer(&mockRetryPolicyProvider{policy: policy}, nil)
	selector := WithLoadBalancedSelector(
		staticCandidateSelector{candidates: candidates},
		loadBalancer,
		&mockRetryPolicyProvider{policy: policy},
		WithProviderAffinity(store),
	)

	result, err := selector.Select(ctx, &llm.Request{Model: "gpt-4"})
	require.NoError(t, err)
	return result
}

func retryPolicy(enabled bool, maxChannelRetries int) *biz.RetryPolicy {
	return &biz.RetryPolicy{Enabled: enabled, MaxChannelRetries: maxChannelRetries}
}

type staticCandidateSelector struct {
	candidates []*ChannelModelsCandidate
}

func (s staticCandidateSelector) Select(context.Context, *llm.Request) ([]*ChannelModelsCandidate, error) {
	return s.candidates, nil
}
