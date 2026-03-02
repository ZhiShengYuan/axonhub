package permission

import (
	"testing"

	"github.com/looplj/axonhub/axon/permission/extractor"
	"github.com/looplj/axonhub/axon/permission/grant"
	"github.com/looplj/axonhub/axon/permission/policy"
	"github.com/stretchr/testify/assert"
)

func TestFromExtractorResources_Skill(t *testing.T) {
	in := []extractor.Resource{
		{Type: extractor.ResourceSkill, Skill: "code-review"},
	}

	out := fromExtractorResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, ResourceSkill, out[0].Type)
	assert.Equal(t, "code-review", out[0].Skill)
}

func TestFromExtractorResources_MixedTypes(t *testing.T) {
	in := []extractor.Resource{
		{Type: extractor.ResourcePath, Path: "/tmp/file.txt", WorkspaceRel: "file.txt"},
		{Type: extractor.ResourceSkill, Skill: "walkthrough"},
	}

	out := fromExtractorResources(in)

	assert.Len(t, out, 2)
	assert.Equal(t, ResourcePath, out[0].Type)
	assert.Equal(t, "/tmp/file.txt", out[0].Path)
	assert.Equal(t, ResourceSkill, out[1].Type)
	assert.Equal(t, "walkthrough", out[1].Skill)
}

func TestToPolicyResources_Skill(t *testing.T) {
	in := []Resource{
		{Type: ResourceSkill, Skill: "deploy"},
	}

	out := toPolicyResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, policy.ResourceSkill, out[0].Type)
	assert.Equal(t, "deploy", out[0].Skill)
}

func TestToGrantResources_Skill(t *testing.T) {
	in := []Resource{
		{Type: ResourceSkill, Skill: "commit"},
	}

	out := toGrantResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, grant.ResourceSkill, out[0].Type)
	assert.Equal(t, "commit", out[0].Skill)
}

func TestToGrantResources_MixedWithSkill(t *testing.T) {
	in := []Resource{
		{Type: ResourcePath, Path: "/tmp/file.txt", WorkspaceRel: "file.txt"},
		{Type: ResourceSkill, Skill: "code-review"},
		{Type: ResourceDomain, Domain: "example.com"},
	}

	out := toGrantResources(in)

	assert.Len(t, out, 3)
	assert.Equal(t, grant.ResourcePath, out[0].Type)
	assert.Equal(t, grant.ResourceSkill, out[1].Type)
	assert.Equal(t, "code-review", out[1].Skill)
	assert.Equal(t, grant.ResourceDomain, out[2].Type)
}
