package orchestrator

import (
	"context"
	"net/http"
	"testing"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// TestApplyXForwardedForPassThrough tests the X-Forwarded-For pass-through middleware.
func TestApplyXForwardedForPassThrough(t *testing.T) {
	tests := []struct {
		name              string
		channelXFFSetting *bool // Channel-level override
		clientIP          string // RawRequest.ClientIP from inbound request
		wantXFFHeader     string // Expected X-Forwarded-For in outbound, empty if none
	}{
		// nil setting → default disabled (no XFF)
		{
			name:              "nil_setting_default_disabled",
			channelXFFSetting: nil,
			clientIP:          "203.0.113.9",
			wantXFFHeader:     "", // Default: XFF not present
		},
		// false setting → explicitly disabled (no XFF)
		{
			name:              "false_setting_explicitly_disabled",
			channelXFFSetting: lo.ToPtr(false),
			clientIP:          "203.0.113.9",
			wantXFFHeader:     "", // Explicitly disabled: XFF not present
		},
		// true with valid IP → XFF set to client IP
		{
			name:              "enabled_with_valid_ip",
			channelXFFSetting: lo.ToPtr(true),
			clientIP:          "203.0.113.9",
			wantXFFHeader:     "203.0.113.9",
		},
		// true but empty IP → no XFF (invalid)
		{
			name:              "enabled_but_empty_ip",
			channelXFFSetting: lo.ToPtr(true),
			clientIP:          "",
			wantXFFHeader:     "", // No IP to forward: middleware omits XFF
		},
		// true but whitespace IP → no XFF (invalid)
		{
			name:              "enabled_but_whitespace_ip",
			channelXFFSetting: lo.ToPtr(true),
			clientIP:          "   ",
			wantXFFHeader:     "", // Whitespace IP is invalid: middleware omits XFF
		},
		// true overwrites existing XFF
		{
			name:              "enabled_overwrites_existing_xff",
			channelXFFSetting: lo.ToPtr(true),
			clientIP:          "203.0.113.9",
			wantXFFHeader:     "203.0.113.9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use background context - no system service needed for per-channel XFF
			ctx := context.Background()

			// Create channel with pass-through setting
			channelSettings := &objects.ChannelSettings{}
			if tt.channelXFFSetting != nil {
				channelSettings.PassThroughXForwardedFor = tt.channelXFFSetting
			}

			channel := &biz.Channel{
				Channel: &ent.Channel{
					ID:       1,
					Name:     "test-channel",
					Settings: channelSettings,
				},
				Outbound: &mockTransformer{},
			}

			// Create llm request with client IP
			llmRequest := &llm.Request{
				Model: "gpt-4",
				RawRequest: &httpclient.Request{
					ClientIP: tt.clientIP,
				},
			}

			// Create outbound transformer
			outbound := &PersistentOutboundTransformer{
				wrapped: &mockTransformer{},
				state: &PersistenceState{
					CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
					LlmRequest:       llmRequest,
				},
			}

			// Create middleware - pass nil for systemService (no global fallback)
			middleware := applyXForwardedForPassThrough(outbound, nil)

			// Execute middleware
			rawRequest := &httpclient.Request{
				Headers: make(http.Header),
			}
			// Pre-set an existing XFF header if testing overwrite
			if tt.name == "enabled_overwrites_existing_xff" {
				rawRequest.Headers.Set("X-Forwarded-For", "10.0.0.1, 10.0.0.2")
			}
			processedRequest, err := middleware.OnOutboundRawRequest(ctx, rawRequest)

			require.NoError(t, err)
			require.NotNil(t, processedRequest)

			// Verify X-Forwarded-For header
			if tt.wantXFFHeader != "" {
				require.Equal(t, tt.wantXFFHeader, processedRequest.Headers.Get("X-Forwarded-For"))
			} else {
				// When no XFF expected, header should be empty
				require.Empty(t, processedRequest.Headers.Get("X-Forwarded-For"))
			}
		})
	}
}

// TestApplyXForwardedForPassThrough_NoChannel tests the middleware when no channel is selected.
func TestApplyXForwardedForPassThrough_NoChannel(t *testing.T) {
	// Use background context - no system service needed
	ctx := context.Background()

	// Create outbound without a channel
	outbound := &PersistentOutboundTransformer{
		wrapped: &mockTransformer{},
		state:   &PersistenceState{},
	}

	// Create middleware - pass nil for systemService (no global fallback)
	middleware := applyXForwardedForPassThrough(outbound, nil)

	// Execute middleware
	rawRequest := &httpclient.Request{
		Headers: make(http.Header),
	}
	processedRequest, err := middleware.OnOutboundRawRequest(ctx, rawRequest)
	require.NoError(t, err)
	require.NotNil(t, processedRequest)
	// No channel selected: no error, XFF should not be set
	require.Empty(t, processedRequest.Headers.Get("X-Forwarded-For"))
}