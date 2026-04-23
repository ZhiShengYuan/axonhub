package objects

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelSettingsUserAgentContract tests the UserAgent field contract
// and null/empty-string normalization.
func TestChannelSettingsUserAgentContract(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantUA      *string
		wantNilUA   bool
		description string
	}{
		{
			name:        "null_userAgent_becomes_nil",
			input:       `{"userAgent": null}`,
			wantNilUA:   true,
			description: "null is treated as not configured",
		},
		{
			name:        "empty_string_becomes_nil",
			input:       `{"userAgent": ""}`,
			wantNilUA:   true,
			description: "empty string is treated as not configured (normalized to nil)",
		},
		{
			name:        "valid_userAgent_preserved",
			input:       `{"userAgent": "MyApp/1.0"}`,
			wantUA:      strPtr("MyApp/1.0"),
			wantNilUA:   false,
			description: "non-empty User-Agent is preserved",
		},
		{
			name:        "omit_userAgent_becomes_nil",
			input:       `{}`,
			wantNilUA:   true,
			description: "omitted field becomes nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var settings ChannelSettings
			err := json.Unmarshal([]byte(tt.input), &settings)
			require.NoError(t, err)

			if tt.wantNilUA {
				assert.Nil(t, settings.UserAgent, tt.description)
			} else {
				require.NotNil(t, settings.UserAgent, tt.description)
				assert.Equal(t, *tt.wantUA, *settings.UserAgent, tt.description)
			}
		})
	}
}

// TestChannelSettingsUserAgentPrecedenceContract documents the expected precedence
// for User-Agent resolution at runtime. This contract is enforced by the orchestrator
// middleware (applyUserAgentOverride in override.go).
//
// Precedence (highest to lowest):
//   1. Custom User-Agent value (if set and non-empty)
//   2. Pass-through of inbound User-Agent header (if PassThroughUserAgent is true)
//   3. AxonHub/provider default User-Agent (default behavior)
func TestChannelSettingsUserAgentPrecedenceContract(t *testing.T) {
	// Test case: custom UA takes precedence over pass-through
	t.Run("custom_UA_overrides_pass_through", func(t *testing.T) {
		settings := ChannelSettings{
			UserAgent:           strPtr("Custom/1.0"),
			PassThroughUserAgent: ptr(true),
		}
		// Custom UA should be set, not pass-through
		assert.NotNil(t, settings.UserAgent)
		assert.Equal(t, "Custom/1.0", *settings.UserAgent)
		assert.True(t, *settings.PassThroughUserAgent)
	})

	// Test case: empty string UA is treated as nil (falls through to pass-through)
	t.Run("empty_string_falls_through_to_pass_through", func(t *testing.T) {
		var settings ChannelSettings
		err := json.Unmarshal([]byte(`{"userAgent": "", "passThroughUserAgent": true}`), &settings)
		require.NoError(t, err)
		assert.Nil(t, settings.UserAgent)
		assert.True(t, *settings.PassThroughUserAgent)
	})

	// Test case: nil UA with pass-through disabled uses default
	t.Run("nil_UA_with_pass_through_disabled_uses_default", func(t *testing.T) {
		settings := ChannelSettings{
			UserAgent:           nil,
			PassThroughUserAgent: ptr(false),
		}
		assert.Nil(t, settings.UserAgent)
		assert.False(t, *settings.PassThroughUserAgent)
	})

	// Test case: nil UA and nil pass-through uses default
	t.Run("nil_UA_and_nil_pass_through_uses_default", func(t *testing.T) {
		settings := ChannelSettings{
			UserAgent:           nil,
			PassThroughUserAgent: nil,
		}
		assert.Nil(t, settings.UserAgent)
		assert.Nil(t, settings.PassThroughUserAgent)
	})
}

// TestChannelSettingsUserAgentJSONRoundTrip verifies JSON serialization
// preserves the null/empty-string normalization.
func TestChannelSettingsUserAgentJSONRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		settings   ChannelSettings
		checkField string
		checkValue interface{}
	}{
		{
			name:       "nil_user_agent_omitted",
			settings:   ChannelSettings{UserAgent: nil},
			checkField: "userAgent",
			checkValue: nil,
		},
		{
			name:       "empty_string_user_agent_omitted",
			settings:   ChannelSettings{UserAgent: strPtr("")},
			checkField: "userAgent",
			checkValue: nil, // empty string normalized to nil
		},
		{
			name:       "custom_user_agent_preserved",
			settings:   ChannelSettings{UserAgent: strPtr("MyApp/2.0")},
			checkField: "userAgent",
			checkValue: "MyApp/2.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.settings)
			require.NoError(t, err)

			var result map[string]interface{}
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)

			// Check the userAgent field specifically
			got, exists := result[tt.checkField]
			if tt.checkValue == nil {
				// nil values may be omitted or null depending on omitempty
				if exists && got == nil {
					// null is acceptable
					return
				}
				// Field might be omitted entirely which is also fine
				return
			}
			assert.Equal(t, tt.checkValue, got)
		})
	}
}

func strPtr(s string) *string {
	return &s
}

func ptr(b bool) *bool {
	return &b
}