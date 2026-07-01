package biz

import (
	"context"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/model"
	"github.com/looplj/axonhub/internal/objects"
)

// normalizeModelAssociationResponseModel is a tiny pure helper for tests; the real
// implementation lives next to other association normalization in model_settings_inheritance.go.
func normalizeModelAssociationResponseModelForTest(assoc *objects.ModelAssociation) {
	if assoc == nil {
		return
	}

	assoc.ResponseModel = strings.TrimSpace(assoc.ResponseModel)
}

func TestNormalizeModelAssociationResponseModel_TrimsAndCollapsesBlank(t *testing.T) {
	t.Run("trims surrounding whitespace", func(t *testing.T) {
		assoc := &objects.ModelAssociation{
			Type:          "model",
			ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4"},
			ResponseModel: "  gpt-4-alias  ",
		}

		normalizeModelAssociationResponseModelForTest(assoc)

		require.Equal(t, "gpt-4-alias", assoc.ResponseModel)
	})

	t.Run("collapses whitespace-only to empty", func(t *testing.T) {
		cases := []string{"", "   ", "\t", "\n", " \t \n "}
		for _, in := range cases {
			assoc := &objects.ModelAssociation{
				Type:          "model",
				ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4"},
				ResponseModel: in,
			}

			normalizeModelAssociationResponseModelForTest(assoc)

			require.Equal(t, "", assoc.ResponseModel, "input %q should normalize to empty", in)
		}
	})
}

func TestNormalizeSystemModelSettings_TrimsResponseModelForDeveloperAssociations(t *testing.T) {
	settings := &SystemModelSettings{
		DeveloperSettings: []*DeveloperModelSettings{
			{
				Developer: "openai",
				Associations: []*objects.ModelAssociation{
					{
						Type:          "channel_model",
						ChannelModel:  &objects.ChannelModelAssociation{ChannelID: 10},
						ResponseModel: "   ",
					},
					{
						Type:          "channel_model",
						ChannelModel:  &objects.ChannelModelAssociation{ChannelID: 20},
						ResponseModel: "  openai-public-alias  ",
					},
				},
			},
		},
	}

	normalizeSystemModelSettings(settings)

	require.Empty(t, settings.DeveloperSettings[0].Associations[0].ResponseModel)
	require.Equal(t, "openai-public-alias", settings.DeveloperSettings[0].Associations[1].ResponseModel)
}

func TestEffectiveModelAssociations_PreservesResponseModelFromInheritedDeveloperRule(t *testing.T) {
	// Simulate the system developer-settings save path: normalizeSystemModelSettings
	// trims the alias before persistence, then EffectiveModelAssociations reads it back.
	settings := &SystemModelSettings{
		DeveloperSettings: []*DeveloperModelSettings{
			{
				Developer: "openai",
				Associations: []*objects.ModelAssociation{
					{
						Type:          "channel_model",
						Priority:      1,
						ChannelModel:  &objects.ChannelModelAssociation{ChannelID: 10},
						ResponseModel: "  public-alias  ",
					},
				},
			},
		},
	}
	normalizeSystemModelSettings(settings)

	result := EffectiveModelAssociations(settings, &ent.Model{
		Developer: "openai",
		ModelID:   "gpt-4o",
	})

	require.Len(t, result, 1)
	require.Equal(t, "public-alias", result[0].ResponseModel,
		"inherited developer association must carry the alias")
	require.Equal(t, "gpt-4o", result[0].ChannelModel.ModelID,
		"inherited developer association must still inject the concrete modelID")
	require.Empty(t, settings.DeveloperSettings[0].Associations[0].ChannelModel.ModelID,
		"the source developer association must remain untouched (modelID is filled in clones)")
}

func TestEffectiveModelAssociations_PreservesResponseModelOnModelRule(t *testing.T) {
	// Simulate the model-level save path: validateModelSettings normalizes the
	// alias before persistence, then EffectiveModelAssociations reads it back.
	modelSettings := &objects.ModelSettings{
		Associations: []*objects.ModelAssociation{
			{
				Type:          "model",
				Priority:      0,
				ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4o"},
				ResponseModel: " my-alias  ",
			},
		},
	}
	normalizeModelSettings(modelSettings)

	result := EffectiveModelAssociations(&SystemModelSettings{}, &ent.Model{
		Developer: "openai",
		ModelID:   "gpt-4o",
		Settings:  modelSettings,
	})

	require.Len(t, result, 1)
	require.Equal(t, "my-alias", result[0].ResponseModel)
}

func TestEffectiveModelAssociations_PriorityOrderAndModelRuleWinsOnTie(t *testing.T) {
	modelAssociation := &objects.ModelAssociation{
		Type:          "model",
		Priority:      1,
		ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4o"},
		ResponseModel: "model-alias",
	}
	developerAssociationSamePriority := &objects.ModelAssociation{
		Type:          "channel_model",
		Priority:      1,
		ChannelModel:  &objects.ChannelModelAssociation{ChannelID: 10},
		ResponseModel: "developer-alias",
	}
	developerAssociationHigherPriority := &objects.ModelAssociation{
		Type:     "channel_tags_model",
		Priority: 0,
		ChannelTagsModel: &objects.ChannelTagsModelAssociation{
			ChannelTags: []string{"anthropic"},
		},
		ResponseModel: "developer-higher-priority-alias",
	}

	result := EffectiveModelAssociations(&SystemModelSettings{
		DeveloperSettings: []*DeveloperModelSettings{
			{
				Developer: "openai",
				Associations: []*objects.ModelAssociation{
					developerAssociationSamePriority,
					developerAssociationHigherPriority,
				},
			},
		},
	}, &ent.Model{
		Developer: "openai",
		ModelID:   "gpt-4o",
		Settings: &objects.ModelSettings{
			Associations: []*objects.ModelAssociation{modelAssociation},
		},
	})

	require.Len(t, result, 3, "expect 1 model rule + 2 inherited developer rules")
	// priority 0 (developer higher priority) goes first
	require.Equal(t, 0, result[0].Priority)
	require.Equal(t, "channel_tags_model", result[0].Type)
	require.Equal(t, "developer-higher-priority-alias", result[0].ResponseModel)
	require.Equal(t, "gpt-4o", result[0].ChannelTagsModel.ModelID)
	// priority 1: model rule (sourceRank 0) wins on tie
	require.Equal(t, 1, result[1].Priority)
	require.Equal(t, "model", result[1].Type)
	require.Equal(t, "model-alias", result[1].ResponseModel)
	// priority 1: developer rule (sourceRank 1) follows
	require.Equal(t, 1, result[2].Priority)
	require.Equal(t, "channel_model", result[2].Type)
	require.Equal(t, "developer-alias", result[2].ResponseModel)
	require.Equal(t, "gpt-4o", result[2].ChannelModel.ModelID)
}

func TestNormalizeModelAssociationResponseModel_IdempotentOnSecondPass(t *testing.T) {
	assoc := &objects.ModelAssociation{
		Type:          "model",
		ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4"},
		ResponseModel: "  alias-1  ",
	}

	normalizeModelAssociationResponseModelForTest(assoc)
	require.Equal(t, "alias-1", assoc.ResponseModel)

	// Re-normalizing an already-trimmed value must not change it.
	normalizeModelAssociationResponseModelForTest(assoc)
	require.Equal(t, "alias-1", assoc.ResponseModel)
}

func TestModelService_CreateModel_PersistsTrimmedResponseModelAlias(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:ent-response-alias-create?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := &ModelService{
		AbstractService: &AbstractService{db: client},
	}

	trimmedInput := ent.CreateModelInput{
		Developer: "openai",
		ModelID:   "gpt-4o",
		Name:      "gpt-4o",
		Type:      lo.ToPtr(model.TypeChat),
		ModelCard: &objects.ModelCard{},
		Settings: &objects.ModelSettings{
			Associations: []*objects.ModelAssociation{
				{
					Type:          "model",
					ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4o"},
					ResponseModel: "  trimmed-alias  ",
				},
			},
		},
	}

	created, err := svc.CreateModel(ctx, trimmedInput)
	require.NoError(t, err)
	require.NotNil(t, created.Settings)
	require.Len(t, created.Settings.Associations, 1)
	require.Equal(t, "trimmed-alias", created.Settings.Associations[0].ResponseModel,
		"CreateModel must persist the trimmed alias")

	whitespaceInput := ent.CreateModelInput{
		Developer: "openai",
		ModelID:   "gpt-4-turbo",
		Name:      "gpt-4-turbo",
		Type:      lo.ToPtr(model.TypeChat),
		ModelCard: &objects.ModelCard{},
		Settings: &objects.ModelSettings{
			Associations: []*objects.ModelAssociation{
				{
					Type:          "model",
					ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4-turbo"},
					ResponseModel: "   ",
				},
			},
		},
	}

	createdBlank, err := svc.CreateModel(ctx, whitespaceInput)
	require.NoError(t, err)
	require.NotNil(t, createdBlank.Settings)
	require.Empty(t, createdBlank.Settings.Associations[0].ResponseModel,
		"whitespace-only alias must collapse to empty on save")
}

func TestModelService_UpdateModel_PersistsTrimmedResponseModelAlias(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:ent-response-alias-update?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := &ModelService{
		AbstractService: &AbstractService{db: client},
	}

	created, err := svc.CreateModel(ctx, ent.CreateModelInput{
		Developer: "openai",
		ModelID:   "gpt-4o",
		Name:      "gpt-4o",
		Type:      lo.ToPtr(model.TypeChat),
		ModelCard: &objects.ModelCard{},
		Settings:  &objects.ModelSettings{},
	})
	require.NoError(t, err)

	newSettings := &objects.ModelSettings{
		Associations: []*objects.ModelAssociation{
			{
				Type:          "model",
				ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4o"},
				ResponseModel: "\t updated-alias \n",
			},
			{
				Type:          "model",
				ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4o-other"},
				ResponseModel: "   ",
			},
		},
	}

	updated, err := svc.UpdateModel(ctx, created.ID, &ent.UpdateModelInput{
		Settings: newSettings,
	})
	require.NoError(t, err)
	require.NotNil(t, updated.Settings)
	require.Len(t, updated.Settings.Associations, 2)
	require.Equal(t, "updated-alias", updated.Settings.Associations[0].ResponseModel,
		"UpdateModel must persist the trimmed alias")
	require.Empty(t, updated.Settings.Associations[1].ResponseModel,
		"whitespace-only alias on update must collapse to empty")
}

func TestModelService_BulkCreateModels_PersistsTrimmedResponseModelAlias(t *testing.T) {
	client := enttest.Open(t, dialect.SQLite, "file:ent-response-alias-bulk?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := &ModelService{
		AbstractService: &AbstractService{db: client},
	}

	created, err := svc.BulkCreateModels(ctx, []*ent.CreateModelInput{
		{
			Developer: "openai",
			ModelID:   "gpt-4o",
			Name:      "gpt-4o",
			Type:      lo.ToPtr(model.TypeChat),
			ModelCard: &objects.ModelCard{},
			Settings: &objects.ModelSettings{
				Associations: []*objects.ModelAssociation{
					{
						Type:          "model",
						ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4o"},
						ResponseModel: "  bulk-alias  ",
					},
				},
			},
		},
		{
			Developer: "openai",
			ModelID:   "gpt-4-turbo",
			Name:      "gpt-4-turbo",
			Type:      lo.ToPtr(model.TypeChat),
			ModelCard: &objects.ModelCard{},
			Settings: &objects.ModelSettings{
				Associations: []*objects.ModelAssociation{
					{
						Type:          "model",
						ModelID:       &objects.ModelIDAssociation{ModelID: "gpt-4-turbo"},
						ResponseModel: "   ",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, created, 2)

	aliases := []string{
		created[0].Settings.Associations[0].ResponseModel,
		created[1].Settings.Associations[0].ResponseModel,
	}
	require.Equal(t, "bulk-alias", aliases[0],
		"BulkCreateModels must persist the trimmed alias")
	require.Empty(t, aliases[1],
		"BulkCreateModels must collapse whitespace-only alias to empty")
}
