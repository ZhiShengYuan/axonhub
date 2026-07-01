package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

// TestDefaultSelector_CarriesResponseModelAliasOnCandidate locks the
// orchestrator-level behavior promised by Todo 4: when a model rule defines a
// responseModel alias, the candidate produced by the selector carries that
// alias on every entry, and the alias survives all decorator / aggregation
// passes. The test asserts the alias value explicitly, not just field
// presence, to catch a regression where the field is dropped during fan-out.
func TestDefaultSelector_CarriesResponseModelAliasOnCandidate(t *testing.T) {
	ctx, client := setupTest(t)

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Alias Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	channelService := newTestChannelServiceForChannels(client)
	modelService := newTestModelService(client)
	systemService := newTestSystemService(client)
	selector := NewDefaultSelector(channelService, modelService, systemService)

	_, err = client.Model.Create().
		SetModelID("my-gpt-4").
		SetName("My GPT-4").
		SetDeveloper("openai").
		SetIcon("openai").
		SetGroup("gpt-4").
		SetModelCard(&objects.ModelCard{}).
		SetStatus("enabled").
		SetSettings(&objects.ModelSettings{
			Associations: []*objects.ModelAssociation{
				{
					Type:     "channel_model",
					Priority: 1,
					ChannelModel: &objects.ChannelModelAssociation{
						ChannelID: ch.ID,
						ModelID:   "gpt-4",
					},
					ResponseModel: "axonhub-gpt-4",
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	candidates, err := selector.Select(ctx, &llm.Request{Model: "my-gpt-4"})
	require.NoError(t, err)
	require.Len(t, candidates, 1, "single channel with a single channel_model rule")
	require.Len(t, candidates[0].Models, 1)
	require.Equal(t, "gpt-4", candidates[0].Models[0].ActualModel)
	require.Equal(t, "axonhub-gpt-4", candidates[0].Models[0].ResponseModel,
		"candidate for matched rule must carry the rule's ResponseModel alias")
}

// TestDefaultSelector_CandidateBlankResponseModelYieldsEmptyAlias locks the
// unset alias behavior: when the matched rule has no ResponseModel, the
// candidate's entry carries an empty alias (not a fabricated value). The
// masking layer in Todo 5 treats this as "fall back to request model".
func TestDefaultSelector_CandidateBlankResponseModelYieldsEmptyAlias(t *testing.T) {
	ctx, client := setupTest(t)

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("No-Alias Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	channelService := newTestChannelServiceForChannels(client)
	modelService := newTestModelService(client)
	systemService := newTestSystemService(client)
	selector := NewDefaultSelector(channelService, modelService, systemService)

	_, err = client.Model.Create().
		SetModelID("no-alias-model").
		SetName("No Alias").
		SetDeveloper("openai").
		SetIcon("openai").
		SetGroup("test").
		SetModelCard(&objects.ModelCard{}).
		SetStatus("enabled").
		SetSettings(&objects.ModelSettings{
			Associations: []*objects.ModelAssociation{
				{
					Type:     "channel_model",
					Priority: 1,
					ChannelModel: &objects.ChannelModelAssociation{
						ChannelID: ch.ID,
						ModelID:   "gpt-4",
					},
					// ResponseModel intentionally left blank.
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	candidates, err := selector.Select(ctx, &llm.Request{Model: "no-alias-model"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Len(t, candidates[0].Models, 1)
	require.Empty(t, candidates[0].Models[0].ResponseModel,
		"unset alias must travel as empty string, not be fabricated")
}

// TestDefaultSelector_CandidateResponseModelTieBreak_ModelRuleBeatsDeveloperRule
// locks the equal-priority precedence: a model-level rule's alias must beat
// an inherited developer rule's alias. This corresponds to the "model-rule
// sourceRank 0 wins ties" guarantee from mergeInheritedModelAssociations.
func TestDefaultSelector_CandidateResponseModelTieBreak_ModelRuleBeatsDeveloperRule(t *testing.T) {
	ctx, client := setupTest(t)

	ch, err := client.Channel.Create().
		SetType(channel.TypeAnthropic).
		SetName("Anthropic Tie-Break").
		SetBaseURL("https://api.anthropic.com").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key-anthropic"}).
		SetSupportedModels([]string{"claude-opus-4-6"}).
		SetDefaultTestModel("claude-opus-4-6").
		SetTags([]string{"anthropic"}).
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	channelService := newTestChannelServiceForChannels(client)
	modelService := newTestModelService(client)
	systemService := newTestSystemService(client)

	// Developer setting declares a channel_tags_model rule with one alias.
	// The model setting declares a channel_model rule with a different alias
	// at the same priority. Per mergeInheritedModelAssociations the model
	// rule is sourceRank 0 (developer is 1), so on equal priority the model
	// rule's alias must travel.
	err = systemService.SetModelSettings(ctx, biz.SystemModelSettings{
		DeveloperSettings: []*biz.DeveloperModelSettings{
			{
				Developer: "anthropic",
				Associations: []*objects.ModelAssociation{
					{
						Type:     "channel_tags_model",
						Priority: 1,
						ChannelTagsModel: &objects.ChannelTagsModelAssociation{
							ChannelTags: []string{"anthropic"},
						},
						ResponseModel: "developer-rule-alias",
					},
				},
			},
		},
	})
	require.NoError(t, err)

	_, err = client.Model.Create().
		SetModelID("claude-opus-4-6").
		SetName("Claude Opus 4.6").
		SetDeveloper("anthropic").
		SetIcon("anthropic").
		SetGroup("claude").
		SetModelCard(&objects.ModelCard{}).
		SetStatus("enabled").
		SetSettings(&objects.ModelSettings{
			Associations: []*objects.ModelAssociation{
				{
					Type:     "channel_model",
					Priority: 1,
					ChannelModel: &objects.ChannelModelAssociation{
						ChannelID: ch.ID,
						ModelID:   "claude-opus-4-6",
					},
					ResponseModel: "model-rule-alias",
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	selector := NewDefaultSelector(channelService, modelService, systemService)
	candidates, err := selector.Select(ctx, &llm.Request{Model: "claude-opus-4-6"})
	require.NoError(t, err)
	require.Len(t, candidates, 1, "single channel")
	require.Len(t, candidates[0].Models, 1)
	require.Equal(t, "model-rule-alias", candidates[0].Models[0].ResponseModel,
		"model-level alias must win the equal-priority tie over inherited developer alias")
}

// TestDefaultSelector_CandidateResponseModel_DedupFirstWinner locks the
// dedup-first behavior at the candidate level: when two rules resolve to the
// same (channel, model), the first surviving match keeps its alias. The test
// uses the same channel/actual-model combination for two rules; after dedup
// the candidate has one entry carrying the first rule's alias.
func TestDefaultSelector_CandidateResponseModel_DedupFirstWinner(t *testing.T) {
	ctx, client := setupTest(t)

	ch, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Dedup Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-4", "gpt-3.5-turbo"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	channelService := newTestChannelServiceForChannels(client)
	modelService := newTestModelService(client)
	systemService := newTestSystemService(client)
	selector := NewDefaultSelector(channelService, modelService, systemService)

	_, err = client.Model.Create().
		SetModelID("dedup-alias-model").
		SetName("Dedup Alias").
		SetDeveloper("openai").
		SetIcon("openai").
		SetGroup("test").
		SetModelCard(&objects.ModelCard{}).
		SetStatus("enabled").
		SetSettings(&objects.ModelSettings{
			Associations: []*objects.ModelAssociation{
				{
					Type:     "channel_regex",
					Priority: 1,
					ChannelRegex: &objects.ChannelRegexAssociation{
						ChannelID: ch.ID,
						Pattern:   "gpt-.*",
					},
					ResponseModel: "first-alias",
				},
				{
					Type:     "channel_model",
					Priority: 2,
					ChannelModel: &objects.ChannelModelAssociation{
						ChannelID: ch.ID,
						ModelID:   "gpt-4",
					},
					ResponseModel: "second-alias",
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	candidates, err := selector.Select(ctx, &llm.Request{Model: "dedup-alias-model"})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Len(t, candidates[0].Models, 2, "regex fan-out: gpt-4 + gpt-3.5-turbo both survive the first rule")

	// The first rule (channel_regex, priority 1) survives for both models;
	// its alias must travel to every entry on the candidate.
	for _, m := range candidates[0].Models {
		require.Equal(t, "first-alias", m.ResponseModel,
			"first surviving match's alias must travel to every entry on the candidate (requestModel=%s)", m.RequestModel)
	}
}
