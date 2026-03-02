package permission

import (
	"testing"

	"github.com/looplj/axonhub/axon/permission/extractor"
	"github.com/looplj/axonhub/axon/permission/grant"
	"github.com/looplj/axonhub/axon/permission/policy"
	"github.com/stretchr/testify/assert"
)

func TestFromExtractorResources_Dir(t *testing.T) {
	in := []extractor.Resource{
		{
			Type:             extractor.ResourceDir,
			Path:             "/workspace/src",
			WorkspaceRel:     "src",
			OutsideWorkspace: false,
		},
	}

	out := fromExtractorResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, ResourceDir, out[0].Type)
	assert.Equal(t, "/workspace/src", out[0].Path)
	assert.Equal(t, "src", out[0].WorkspaceRel)
	assert.False(t, out[0].OutsideWorkspace)
}

func TestFromExtractorResources_DirOutsideWorkspace(t *testing.T) {
	in := []extractor.Resource{
		{
			Type:             extractor.ResourceDir,
			Path:             "/tmp/data",
			OutsideWorkspace: true,
		},
	}

	out := fromExtractorResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, ResourceDir, out[0].Type)
	assert.True(t, out[0].OutsideWorkspace)
}

func TestFromExtractorResources_DirAndCommand(t *testing.T) {
	in := []extractor.Resource{
		{Type: extractor.ResourceCommand, Command: "go test", Cwd: "/workspace"},
		{Type: extractor.ResourceDir, Path: "/workspace", WorkspaceRel: "."},
	}

	out := fromExtractorResources(in)

	assert.Len(t, out, 2)
	assert.Equal(t, ResourceCommand, out[0].Type)
	assert.Equal(t, ResourceDir, out[1].Type)
}

func TestToPolicyResources_Dir(t *testing.T) {
	in := []Resource{
		{
			Type:             ResourceDir,
			Path:             "/workspace/src",
			WorkspaceRel:     "src",
			OutsideWorkspace: false,
		},
	}

	out := toPolicyResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, policy.ResourceDir, out[0].Type)
	assert.Equal(t, "/workspace/src", out[0].Path)
	assert.Equal(t, "src", out[0].WorkspaceRel)
	assert.False(t, out[0].OutsideWorkspace)
}

func TestToGrantResources_Dir(t *testing.T) {
	in := []Resource{
		{
			Type:             ResourceDir,
			Path:             "/workspace/src",
			WorkspaceRel:     "src",
			OutsideWorkspace: false,
		},
	}

	out := toGrantResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, grant.ResourceDir, out[0].Type)
	assert.Equal(t, "/workspace/src", out[0].Path)
	assert.Equal(t, "src", out[0].WorkspaceRel)
	assert.False(t, out[0].OutsideWorkspace)
}

func TestFromExtractorResources_AllTypes(t *testing.T) {
	in := []extractor.Resource{
		{Type: extractor.ResourceDir, Path: "/workspace/src", WorkspaceRel: "src"},
		{Type: extractor.ResourcePath, Path: "/workspace/file.txt", WorkspaceRel: "file.txt"},
		{Type: extractor.ResourceCommand, Command: "ls"},
		{Type: extractor.ResourceURL, URL: "https://example.com", Domain: "example.com", Scheme: "https"},
		{Type: extractor.ResourceDomain, Domain: "example.com"},
		{Type: extractor.ResourceSkill, Skill: "deploy"},
	}

	out := fromExtractorResources(in)

	assert.Len(t, out, 6)
	assert.Equal(t, ResourceDir, out[0].Type)
	assert.Equal(t, ResourcePath, out[1].Type)
	assert.Equal(t, ResourceCommand, out[2].Type)
	assert.Equal(t, ResourceURL, out[3].Type)
	assert.Equal(t, ResourceDomain, out[4].Type)
	assert.Equal(t, ResourceSkill, out[5].Type)
}

func TestToGrantResources_SkipsUnknownTypes(t *testing.T) {
	in := []Resource{
		{Type: ResourceDir, Path: "/workspace/src", WorkspaceRel: "src"},
		{Type: ResourceURL, URL: "https://example.com"},
	}

	out := toGrantResources(in)

	assert.Len(t, out, 1)
	assert.Equal(t, grant.ResourceDir, out[0].Type)
}

func TestToGrantResources_MixedWithDir(t *testing.T) {
	in := []Resource{
		{Type: ResourceDir, Path: "/workspace/src", WorkspaceRel: "src"},
		{Type: ResourceCommand, Command: "go test"},
		{Type: ResourceDomain, Domain: "example.com"},
		{Type: ResourceSkill, Skill: "deploy"},
	}

	out := toGrantResources(in)

	assert.Len(t, out, 4)
	assert.Equal(t, grant.ResourceDir, out[0].Type)
	assert.Equal(t, grant.ResourceCommand, out[1].Type)
	assert.Equal(t, grant.ResourceDomain, out[2].Type)
	assert.Equal(t, grant.ResourceSkill, out[3].Type)
}
