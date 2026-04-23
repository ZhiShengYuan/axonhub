package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

// TestIncompleteStreamError_CrossChannelFailover tests that IncompleteStreamError
// blocks SAME-CHANNEL retry but allows CROSS-CHANNEL failover.
//
// Boundary behaviors being tested:
// 1. Same-channel retry is BLOCKED for IncompleteStreamError (CanRetry returns false)
// 2. Cross-channel failover is ALLOWED when HasMoreChannels() returns true
// 3. When all channels fail with IncompleteStreamError, Process() returns the error
func TestIncompleteStreamError_CrossChannelFailover(t *testing.T) {
	ctx := context.Background()
	inbound := &mockInbound{}

	t.Run("IncompleteStreamError_blocks_same_channel_retry_but_allows_cross_channel_failover", func(t *testing.T) {
		// This test verifies the boundary:
		// - IncompleteStreamError.CanRetry() = false (same-channel blocked)
		// - BUT Retryable.HasMoreChannels() = true, so cross-channel failover should work

		execCalls := 0
		executor := &mockExecutor{
			do: func(ctx context.Context, req *httpclient.Request) (*httpclient.Response, error) {
				execCalls++
				// Fail with IncompleteStreamError on first call
				if execCalls == 1 {
					return nil, &llm.IncompleteStreamError{ChunksReceived: 5}
				}
				// Succeed on second call (after failover)
				return &httpclient.Response{Body: []byte(`{}`)}, nil
			},
		}

		canRetryCalls := 0
		prepareForRetryCalls := 0
		hasMoreChannelsCalls := 0
		nextChannelCalls := 0

		outbound := &mockOutbound{
			// KEY: CanRetry returns FALSE for IncompleteStreamError (same-channel blocked)
			canRetry: func(err error) bool {
				canRetryCalls++
				if llm.IsIncompleteStreamError(err) {
					return false // BLOCK same-channel retry
				}
				return true
			},
			prepareForRetry: func(ctx context.Context) error {
				prepareForRetryCalls++
				return nil
			},
			// KEY: HasMoreChannels returns TRUE (cross-channel failover allowed)
			hasMoreChannels: func() bool {
				hasMoreChannelsCalls++
				return true
			},
			nextChannel: func(ctx context.Context) error {
				nextChannelCalls++
				return nil
			},
		}

		p := &pipeline{
			Executor:              executor,
			Inbound:               inbound,
			Outbound:              outbound,
			maxSameChannelRetries: 3,
			maxChannelRetries:     3,
		}

		res, err := p.Process(ctx, &httpclient.Request{})
		require.NoError(t, err, "Process should succeed after cross-channel failover")
		require.NotNil(t, res)

		// Verify execution behavior
		require.Equal(t, 2, execCalls, "Should execute twice: first fails, second succeeds after failover")

		// KEY ASSERTIONS: IncompleteStreamError should NOT trigger same-channel retry
		require.Equal(t, 1, canRetryCalls, "CanRetry should be called once for the IncompleteStreamError")
		require.Equal(t, 0, prepareForRetryCalls,
			"PrepareForRetry should NOT be called because CanRetry returned false for IncompleteStreamError")

		// KEY ASSERTIONS: Cross-channel failover should be triggered
		require.Equal(t, 1, hasMoreChannelsCalls, "HasMoreChannels should be called after CanRetry returns false")
		require.Equal(t, 1, nextChannelCalls, "NextChannel should be called to failover")
	})

	t.Run("IncompleteStreamError_when_all_channels_exhausted_returns_error", func(t *testing.T) {
		// This test verifies: when all candidates fail with IncompleteStreamError,
		// Process() should return the error (not succeed)

		execCalls := 0
		executor := &mockExecutor{
			do: func(ctx context.Context, req *httpclient.Request) (*httpclient.Response, error) {
				execCalls++
				// Always fail with IncompleteStreamError
				return nil, &llm.IncompleteStreamError{ChunksReceived: 3}
			},
		}

		outbound := &mockOutbound{
			// Same-channel retry is BLOCKED for IncompleteStreamError
			canRetry: func(err error) bool {
				if llm.IsIncompleteStreamError(err) {
					return false
				}
				return true
			},
			// But cross-channel failover IS allowed (HasMoreChannels returns true initially)
			hasMoreChannels: func() bool {
				return true
			},
			nextChannel: func(ctx context.Context) error {
				return nil
			},
		}

		p := &pipeline{
			Executor:              executor,
			Inbound:               inbound,
			Outbound:              outbound,
			maxSameChannelRetries: 3,
			maxChannelRetries:     3,
		}

		res, err := p.Process(ctx, &httpclient.Request{})

		// KEY: When all channels exhausted, Process should return IncompleteStreamError
		require.Error(t, err, "Process should return error when all channels fail")
		require.Nil(t, res, "Result should be nil when all channels fail")
		require.True(t, llm.IsIncompleteStreamError(err),
			"Error should be IncompleteStreamError when all channels fail with incomplete stream")

		// Verify all failover attempts were made
		require.Equal(t, 4, execCalls, "Should try initial + 3 channel switches = 4 executions")
	})

	t.Run("IncompleteStreamError_exhausts_channel_retries_and_stops", func(t *testing.T) {
		// This test verifies the boundary when channel switches are exhausted
		// (HasMoreChannels returns false after initial attempts)

		execCalls := 0
		executor := &mockExecutor{
			do: func(ctx context.Context, req *httpclient.Request) (*httpclient.Response, error) {
				execCalls++
				return nil, &llm.IncompleteStreamError{ChunksReceived: 2}
			},
		}

		hasMoreChannelsCalls := 0
		outbound := &mockOutbound{
			canRetry: func(err error) bool {
				return false // Always block same-channel retry
			},
			// Only allow ONE channel switch, then no more
			hasMoreChannels: func() bool {
				hasMoreChannelsCalls++
				return hasMoreChannelsCalls <= 1 // Only 1 channel switch allowed
			},
			nextChannel: func(ctx context.Context) error {
				return nil
			},
		}

		p := &pipeline{
			Executor:              executor,
			Inbound:               inbound,
			Outbound:              outbound,
			maxSameChannelRetries: 2,
			maxChannelRetries:     2,
		}

		res, err := p.Process(ctx, &httpclient.Request{})

		require.Error(t, err, "Process should return error when channel switches exhausted")
		require.Nil(t, res)

		// Initial execution + 1 failover = 2 executions
		require.Equal(t, 2, execCalls)
	})
}

// TestIncompleteStreamError_NotMixedWithOtherErrors verifies that IncompleteStreamError
// retry semantics are distinct from regular retryable errors
func TestIncompleteStreamError_NotMixedWithOtherErrors(t *testing.T) {
	ctx := context.Background()
	inbound := &mockInbound{}

	t.Run("regular_error_can_retry_same_channel_IncompleteStreamError_cannot", func(t *testing.T) {
		// Verify that regular errors allow same-channel retry,
		// but IncompleteStreamError blocks it

		execCalls := 0
		executor := &mockExecutor{
			do: func(ctx context.Context, req *httpclient.Request) (*httpclient.Response, error) {
				execCalls++
				if execCalls <= 2 {
					return nil, errors.New("temporary server error") // Retryable
				}
				return &httpclient.Response{Body: []byte(`{}`)}, nil
			},
		}

		canRetryCalls := []error{}
		outbound := &mockOutbound{
			canRetry: func(err error) bool {
				canRetryCalls = append(canRetryCalls, err)
				if llm.IsIncompleteStreamError(err) {
					return false // BLOCK for IncompleteStreamError
				}
				return true // Allow for other errors
			},
			prepareForRetry: func(ctx context.Context) error {
				return nil
			},
			hasMoreChannels: func() bool {
				return false
			},
		}

		p := &pipeline{
			Executor:              executor,
			Inbound:               inbound,
			Outbound:              outbound,
			maxSameChannelRetries: 3,
			maxChannelRetries:     0,
		}

		res, err := p.Process(ctx, &httpclient.Request{})
		require.NoError(t, err)
		require.NotNil(t, res)

		// First error should be a regular error (retryable on same channel)
		require.Len(t, canRetryCalls, 2, "CanRetry called for 2 failures")
		require.False(t, llm.IsIncompleteStreamError(canRetryCalls[0]),
			"First error should not be IncompleteStreamError")
		require.False(t, llm.IsIncompleteStreamError(canRetryCalls[1]),
			"Second error should not be IncompleteStreamError")

		// Same-channel retry should have been used
		require.Equal(t, 3, execCalls, "Should execute: fail, retry, succeed")
	})
}

