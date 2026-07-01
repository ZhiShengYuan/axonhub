package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestModelMapper_MapModel(t *testing.T) {
	ctx := context.Background()
	mapper := NewModelMapper()

	tests := []struct {
		name          string
		apiKey        *ent.APIKey
		originalModel string
		expectedModel string
	}{
		{
			name:          "nil api key",
			apiKey:        nil,
			originalModel: "gpt-4",
			expectedModel: "gpt-4",
		},
		{
			name: "no profiles",
			apiKey: &ent.APIKey{
				Name:     "test-key",
				Profiles: nil,
			},
			originalModel: "gpt-4",
			expectedModel: "gpt-4",
		},
		{
			name: "no active profile",
			apiKey: &ent.APIKey{
				Name: "test-key",
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "",
					Profiles: []objects.APIKeyProfile{
						{
							Name: "profile1",
							ModelMappings: []objects.ModelMapping{
								{From: "gpt-4", To: "claude-3"},
							},
						},
					},
				},
			},
			originalModel: "gpt-4",
			expectedModel: "gpt-4",
		},
		{
			name: "active profile with exact match",
			apiKey: &ent.APIKey{
				Name: "test-key",
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "profile1",
					Profiles: []objects.APIKeyProfile{
						{
							Name: "profile1",
							ModelMappings: []objects.ModelMapping{
								{From: "gpt-4", To: "claude-3-opus"},
							},
						},
					},
				},
			},
			originalModel: "gpt-4",
			expectedModel: "claude-3-opus",
		},
		{
			name: "active profile with regexp match",
			apiKey: &ent.APIKey{
				Name: "test-key",
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "profile1",
					Profiles: []objects.APIKeyProfile{
						{
							Name: "profile1",
							ModelMappings: []objects.ModelMapping{
								{From: "gpt-.*", To: "claude-3-opus"},
							},
						},
					},
				},
			},
			originalModel: "gpt-4-turbo",
			expectedModel: "claude-3-opus",
		},
		{
			name: "active profile with regexp match 2",
			apiKey: &ent.APIKey{
				Name: "test-key",
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "profile1",
					Profiles: []objects.APIKeyProfile{
						{
							Name: "profile1",
							ModelMappings: []objects.ModelMapping{
								{From: "claude.*-haiku.*", To: "deepseek-chat"},
							},
						},
					},
				},
			},
			originalModel: "claude-haiku-4-5-20251001",
			expectedModel: "deepseek-chat",
		},
		{
			name: "active profile with no matching mapping",
			apiKey: &ent.APIKey{
				Name: "test-key",
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "profile1",
					Profiles: []objects.APIKeyProfile{
						{
							Name: "profile1",
							ModelMappings: []objects.ModelMapping{
								{From: "gpt-4", To: "claude-3-opus"},
							},
						},
					},
				},
			},
			originalModel: "gpt-3.5-turbo",
			expectedModel: "gpt-3.5-turbo",
		},
		{
			name: "active profile not found in profiles list",
			apiKey: &ent.APIKey{
				Name: "test-key",
				Profiles: &objects.APIKeyProfiles{
					ActiveProfile: "nonexistent",
					Profiles: []objects.APIKeyProfile{
						{
							Name: "profile1",
							ModelMappings: []objects.ModelMapping{
								{From: "gpt-4", To: "claude-3-opus"},
							},
						},
					},
				},
			},
			originalModel: "gpt-4",
			expectedModel: "gpt-4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapper.MapModel(ctx, tt.apiKey, tt.originalModel)
			assert.Equal(t, tt.expectedModel, result)
		})
	}
}

func TestModelMapper_MatchesMapping(t *testing.T) {
	mapper := NewModelMapper()

	tests := []struct {
		name     string
		pattern  string
		str      string
		expected bool
	}{
		{
			name:     "exact match",
			pattern:  "gpt-4",
			str:      "gpt-4",
			expected: true,
		},
		{
			name:     "no match",
			pattern:  "gpt-*",
			str:      "claude-3",
			expected: false,
		},
		{
			name:     "wildcard only",
			pattern:  "*",
			str:      "any-model",
			expected: true,
		},
		{
			name:     "regex special chars escaped",
			pattern:  "model.v1",
			str:      "model.v1",
			expected: true,
		},
		{
			name:     "regex special chars no match",
			pattern:  "model.v1",
			str:      "modelxv1",
			expected: true,
		},
		{
			name:     "invalid regex returns false",
			pattern:  "[invalid",
			str:      "[invalid",
			expected: false,
		},
		{
			name:     "invalid regex returns false for any string",
			pattern:  "[invalid",
			str:      "other",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapper.matchesMapping(tt.pattern, tt.str)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestModelMapper_ReplaceResponseModel_WithAlias(t *testing.T) {
	tests := []struct {
		name         string
		requestModel string
		alias        string
		upstream     string
		expected     string
	}{
		{
			name:         "alias_replaces_upstream_model_even_when_request_equals_upstream",
			requestModel: "gpt-4",
			alias:        "my-public-alias",
			upstream:     "gpt-4",
			expected:     "my-public-alias",
		},
		{
			name:         "alias_replaces_upstream_model_when_upstream_equals_request",
			requestModel: "gpt-4",
			alias:        "my-public-alias",
			upstream:     "gpt-4-turbo",
			expected:     "my-public-alias",
		},
		{
			name:         "alias_replaces_upstream_model_when_upstream_differs",
			requestModel: "my-public-alias",
			alias:        "my-public-alias",
			upstream:     "claude-3-opus-20240229",
			expected:     "my-public-alias",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := NewModelMapper()
			resp := &llm.Response{Model: tt.upstream}
			mapper.ReplaceResponseModel(resp, tt.requestModel, tt.alias)
			require.NotNil(t, resp)
			assert.Equal(t, tt.expected, resp.Model)
		})
	}
}

func TestModelMapper_ReplaceResponseModel_BlankAliasFallsBackToRequestModel(t *testing.T) {
	tests := []struct {
		name         string
		requestModel string
		alias        string
		upstream     string
	}{
		{
			name:         "empty_alias_uses_request_model",
			requestModel: "gpt-4",
			alias:        "",
			upstream:     "gpt-4-0613",
		},
		{
			name:         "whitespace_only_alias_treated_as_unset",
			requestModel: "gpt-4",
			alias:        "   \t\n",
			upstream:     "gpt-4-0613",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := NewModelMapper()
			resp := &llm.Response{Model: tt.upstream}
			mapper.ReplaceResponseModel(resp, tt.requestModel, tt.alias)
			assert.Equal(t, tt.requestModel, resp.Model)
		})
	}
}

func TestModelMapper_ReplaceResponseModel_EmptyResponseModelUntouched(t *testing.T) {
	tests := []struct {
		name     string
		alias    string
		upstream string
	}{
		{
			name:     "empty_upstream_no_alias",
			alias:    "",
			upstream: "",
		},
		{
			name:     "empty_upstream_with_alias_kept_empty",
			alias:    "my-public-alias",
			upstream: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := NewModelMapper()
			resp := &llm.Response{Model: tt.upstream}
			mapper.ReplaceResponseModel(resp, "gpt-4", tt.alias)
			assert.Equal(t, "", resp.Model)
		})
	}
}

func TestModelMapper_ReplaceResponseModel_NilResponseDoesNotPanic(t *testing.T) {
	mapper := NewModelMapper()
	assert.NotPanics(t, func() {
		mapper.ReplaceResponseModel(nil, "gpt-4", "alias")
	})
	assert.NotPanics(t, func() {
		mapper.ReplaceResponseModel(nil, "gpt-4", "")
	})
}

func TestModelMapper_ReplaceResponseModel_AliasWhenUpstreamMatchesAlias(t *testing.T) {
	mapper := NewModelMapper()
	resp := &llm.Response{Model: "my-public-alias"}
	mapper.ReplaceResponseModel(resp, "gpt-4", "my-public-alias")
	assert.Equal(t, "my-public-alias", resp.Model)
}
