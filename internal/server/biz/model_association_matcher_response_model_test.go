package biz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

// makeResponseModelTestChannels builds two channels whose request models match
// the "gpt-4" / "gpt-3.5-turbo" entries used in the test associations. Sharing
// the construction keeps each test case focused on the alias itself, not on
// channel plumbing.
func makeResponseModelTestChannels() []*Channel {
	return []*Channel{
		{
			Channel: &ent.Channel{
				ID:              1,
				Name:            "openai-primary",
				Type:            channel.TypeOpenai,
				SupportedModels: []string{"gpt-4", "gpt-3.5-turbo"},
			},
		},
		{
			Channel: &ent.Channel{
				ID:              2,
				Name:            "openai-backup",
				Type:            channel.TypeOpenai,
				SupportedModels: []string{"gpt-4", "gpt-3.5-turbo"},
			},
		},
	}
}

// TestMatchAssociations_ResponseModelPropagation exercises the core Todo 4
// requirement: the alias from the winning ModelAssociation must travel with
// each ChannelModelEntry produced by MatchAssociations. The test asserts the
// alias value explicitly (not just field presence) so a regression that drops
// the alias during fan-out is caught.
func TestMatchAssociations_ResponseModelPropagation(t *testing.T) {
	channels := makeResponseModelTestChannels()

	t.Run("single channel_model rule carries alias into each entry", func(t *testing.T) {
		associations := []*objects.ModelAssociation{
			{
				Type:     "channel_model",
				Priority: 1,
				ChannelModel: &objects.ChannelModelAssociation{
					ChannelID: 1,
					ModelID:   "gpt-4",
				},
				ResponseModel: "openai-frontier",
			},
		}

		// Normalize so the result matches what the save-path produces.
		normalizeModelAssociations(associations)

		result := MatchConnections(associations, channels)
		require.Len(t, result, 1)
		require.Equal(t, 1, result[0].Channel.ID)
		require.Len(t, result[0].Models, 1)
		require.Equal(t, "gpt-4", result[0].Models[0].RequestModel)
		require.Equal(t, "openai-frontier", result[0].Models[0].ResponseModel)
	})

	t.Run("regex rule carries alias into every matched entry", func(t *testing.T) {
		associations := []*objects.ModelAssociation{
			{
				Type:     "regex",
				Priority: 1,
				Regex: &objects.RegexAssociation{
					Pattern: "gpt-.*",
				},
				ResponseModel: "axonhub-gpt",
			},
		}

		normalizeModelAssociations(associations)

		result := MatchConnections(associations, channels)
		require.Len(t, result, 2)

		// Each channel's matched models must carry the same alias; the alias is
		// the rule-level alias, not per-entry.
		for _, conn := range result {
			require.NotEmpty(t, conn.Models)
			for _, m := range conn.Models {
				require.Equal(t, "axonhub-gpt", m.ResponseModel,
					"channel=%d requestModel=%s missing alias", conn.Channel.ID, m.RequestModel)
			}
		}
	})

	t.Run("blank ResponseModel yields empty alias on entry", func(t *testing.T) {
		associations := []*objects.ModelAssociation{
			{
				Type:     "channel_model",
				Priority: 1,
				ChannelModel: &objects.ChannelModelAssociation{
					ChannelID: 1,
					ModelID:   "gpt-4",
				},
				// ResponseModel left unset.
			},
		}

		normalizeModelAssociations(associations)

		result := MatchConnections(associations, channels)
		require.Len(t, result, 1)
		require.Len(t, result[0].Models, 1)
		require.Empty(t, result[0].Models[0].ResponseModel,
			"unset alias must travel as empty string, not be fabricated")
	})

	t.Run("whitespace-only ResponseModel normalizes to empty alias", func(t *testing.T) {
		associations := []*objects.ModelAssociation{
			{
				Type:     "channel_model",
				Priority: 1,
				ChannelModel: &objects.ChannelModelAssociation{
					ChannelID: 1,
					ModelID:   "gpt-4",
				},
				ResponseModel: "   \t  ",
			},
		}

		normalizeModelAssociations(associations)

		result := MatchConnections(associations, channels)
		require.Len(t, result, 1)
		require.Len(t, result[0].Models, 1)
		require.Empty(t, result[0].Models[0].ResponseModel,
			"whitespace-only alias must normalize to empty after matching")
	})
}

// TestMatchAssociations_ResponseModelTieBreak locks the equal-priority override
// behavior: a model-level rule's alias must beat an inherited developer rule's
// alias, because mergeInheritedModelAssociations already places model rules
// before developer rules on equal priority. The test funnels both rules through
// MatchConnections and asserts the propagated alias is the model rule's.
func TestMatchAssociations_ResponseModelTieBreak(t *testing.T) {
	channels := makeResponseModelTestChannels()

	t.Run("model-level alias wins on equal priority", func(t *testing.T) {
		// Model-level rule (will be inherited/merged first) — sets alias
		// "model-alias". Developer rule on equal priority would win ties if
		// not for the sourceRank ordering in mergeInheritedModelAssociations.
		associations := []*objects.ModelAssociation{
			{
				Type:     "channel_model",
				Priority: 1,
				ChannelModel: &objects.ChannelModelAssociation{
					ChannelID: 1,
					ModelID:   "gpt-4",
				},
				ResponseModel: "model-alias",
			},
			{
				Type:     "channel_model",
				Priority: 1,
				ChannelModel: &objects.ChannelModelAssociation{
					ChannelID: 1,
					ModelID:   "gpt-4",
				},
				ResponseModel: "developer-alias",
			},
		}

		normalizeModelAssociations(associations)

		// After dedup, only the first association's entry survives. The first
		// association is the model-level rule. The model-rule alias must win.
		result := MatchConnections(associations, channels)
		require.Len(t, result, 1)
		require.Len(t, result[0].Models, 1)
		require.Equal(t, "model-alias", result[0].Models[0].ResponseModel,
			"model-level alias must win on equal priority after dedup-first-winner")
	})

	t.Run("first surviving match wins alias when priority differs", func(t *testing.T) {
		// Lower priority value = higher priority; first rule wins.
		associations := []*objects.ModelAssociation{
			{
				Type:     "channel_model",
				Priority: 1,
				ChannelModel: &objects.ChannelModelAssociation{
					ChannelID: 1,
					ModelID:   "gpt-4",
				},
				ResponseModel: "first-alias",
			},
			{
				Type:     "channel_model",
				Priority: 5,
				ChannelModel: &objects.ChannelModelAssociation{
					ChannelID: 1,
					ModelID:   "gpt-4",
				},
				ResponseModel: "second-alias",
			},
		}

		normalizeModelAssociations(associations)

		result := MatchConnections(associations, channels)
		require.Len(t, result, 1)
		require.Len(t, result[0].Models, 1)
		require.Equal(t, "first-alias", result[0].Models[0].ResponseModel,
			"first surviving match must keep its alias (dedup-first-winner)")
	})
}

// TestMatchAssociations_ResponseModel_DedupFirstWinnerAcrossTypes covers the
// case where two different rule types resolve to the same (channel, model)
// combination and the first surviving match must keep its alias. The test
// also covers multiple-models-attached-to-one-channel (regex rule producing
// multiple entries on the same channel) — each entry must carry the same
// alias as the originating rule.
func TestMatchAssociations_ResponseModel_DedupFirstWinnerAcrossTypes(t *testing.T) {
	channels := makeResponseModelTestChannels()

	associations := []*objects.ModelAssociation{
		{
			Type:     "channel_regex",
			Priority: 1,
			ChannelRegex: &objects.ChannelRegexAssociation{
				ChannelID: 1,
				Pattern:   "gpt-.*",
			},
			ResponseModel: "channel-regex-alias",
		},
		{
			Type:     "channel_model",
			Priority: 2,
			ChannelModel: &objects.ChannelModelAssociation{
				ChannelID: 1,
				ModelID:   "gpt-4",
			},
			ResponseModel: "channel-model-alias",
		},
	}

	normalizeModelAssociations(associations)

	result := MatchConnections(associations, channels)
	require.Len(t, result, 1, "channel 1 only — channel 2 has no matching rule")
	require.Equal(t, 1, result[0].Channel.ID)
	require.Len(t, result[0].Models, 2, "channel_regex produces both gpt-4 and gpt-3.5-turbo")

	// All entries on the surviving match must carry the FIRST winning
	// association's alias — not be mixed with the second association's alias.
	for _, m := range result[0].Models {
		require.Equal(t, "channel-regex-alias", m.ResponseModel,
			"first surviving match's alias must travel to every entry on its connection, requestModel=%s", m.RequestModel)
	}
}

// TestMatchAssociations_ResponseModelThroughEffectiveMerge ensures that
// EffectiveModelAssociations -> MatchConnections pipeline keeps the alias.
// This is the realistic path the plan expects in production.
func TestMatchAssociations_ResponseModelThroughEffectiveMerge(t *testing.T) {
	channels := makeResponseModelTestChannels()

	modelAssoc := &objects.ModelAssociation{
		Type:     "channel_model",
		Priority: 1,
		ChannelModel: &objects.ChannelModelAssociation{
			ChannelID: 1,
			ModelID:   "gpt-4",
		},
		ResponseModel: "model-rule-alias",
	}
	developerAssoc := &objects.ModelAssociation{
		Type:     "channel_model",
		Priority: 1,
		ChannelModel: &objects.ChannelModelAssociation{
			ChannelID: 1,
			// ModelID is filled in by inheritDeveloperAssociationForModel.
		},
		ResponseModel: "developer-rule-alias",
	}
	normalizeModelAssociationResponseModel(modelAssoc)
	normalizeModelAssociationResponseModel(developerAssoc)

	systemSettings := &SystemModelSettings{
		DeveloperSettings: []*DeveloperModelSettings{
			{
				Developer:    "openai",
				Associations: []*objects.ModelAssociation{developerAssoc},
			},
		},
	}

	entModel := &ent.Model{
		ModelID:   "gpt-4",
		Developer: "openai",
		Settings: &objects.ModelSettings{
			Associations: []*objects.ModelAssociation{modelAssoc},
		},
	}

	// The realistic flow: EffectiveModelAssociations merges inherited
	// developer rules + model rules, then MatchConnections applies dedup and
	// produces candidate connections.
	associations := EffectiveModelAssociations(systemSettings, entModel)
	require.Len(t, associations, 2, "model rule + inherited developer rule")

	result := MatchConnections(associations, channels)
	require.Len(t, result, 1)
	require.Len(t, result[0].Models, 1)
	// Model rule (sourceRank 0) wins on equal priority, so its alias travels.
	require.Equal(t, "model-rule-alias", result[0].Models[0].ResponseModel,
		"model rule alias must win the equal-priority tie through EffectiveModelAssociations")
}
