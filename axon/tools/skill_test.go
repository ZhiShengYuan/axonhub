package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkillTool(t *testing.T) {
	// Create temp directory with test skill
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "demo-skill")
	require.NoError(t, os.MkdirAll(skillDir, 0755))

	skillContent := `---
name: demo-skill
description: Demo skill for testing
---

# Demo Skill

Follow these instructions.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0644))

	tool := NewSkillTool(tmpDir)

	t.Run("Definition", func(t *testing.T) {
		def := tool.Definition()
		assert.Equal(t, "Skill", def.Name)
		assert.NotEmpty(t, def.Description)
	})

	t.Run("Execute success", func(t *testing.T) {
		args, _ := json.Marshal(skillInput{Skill: "demo-skill"})
		result := tool.Execute(context.Background(), args)

		assert.Nil(t, result.Error)
		require.NotNil(t, result.Content.Text)
		assert.Contains(t, *result.Content.Text, `<skill name="demo-skill">`)
		assert.Contains(t, *result.Content.Text, "Follow these instructions")
		assert.Contains(t, *result.Content.Text, "</skill>")
	})

	t.Run("Execute with args", func(t *testing.T) {
		args, _ := json.Marshal(skillInput{Skill: "demo-skill", Args: "-m 'test message'"})
		result := tool.Execute(context.Background(), args)

		assert.Nil(t, result.Error)
		require.NotNil(t, result.Content.Text)
		assert.Contains(t, *result.Content.Text, "Arguments: -m 'test message'")
	})

	t.Run("Execute with qualified name", func(t *testing.T) {
		args, _ := json.Marshal(skillInput{Skill: "namespace:demo-skill"})
		result := tool.Execute(context.Background(), args)

		assert.Nil(t, result.Error)
		require.NotNil(t, result.Content.Text)
		assert.Contains(t, *result.Content.Text, `<skill name="demo-skill">`)
	})

	t.Run("Execute not found", func(t *testing.T) {
		args, _ := json.Marshal(skillInput{Skill: "nonexistent"})
		result := tool.Execute(context.Background(), args)

		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "not found")
	})

	t.Run("Execute empty skill name", func(t *testing.T) {
		args, _ := json.Marshal(skillInput{Skill: ""})
		result := tool.Execute(context.Background(), args)

		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "required")
	})
}

func TestSkillToolListSkills(t *testing.T) {
	// Create temp directory with multiple skills
	tmpDir := t.TempDir()

	// Create first skill
	skill1Dir := filepath.Join(tmpDir, "skill-one")
	require.NoError(t, os.MkdirAll(skill1Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skill1Dir, "SKILL.md"), []byte(`---
name: skill-one
description: First skill
---
# Skill One
`), 0644))

	// Create second skill
	skill2Dir := filepath.Join(tmpDir, "skill-two")
	require.NoError(t, os.MkdirAll(skill2Dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(skill2Dir, "SKILL.md"), []byte(`---
name: skill-two
description: Second skill
---
# Skill Two
`), 0644))

	tool := NewSkillTool(tmpDir)

	skills, err := tool.ListSkills()
	require.NoError(t, err)
	assert.Len(t, skills, 2)
}
