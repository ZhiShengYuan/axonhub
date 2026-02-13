package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/looplj/axonhub/axon/agent"
	"github.com/looplj/skills"
)

//go:embed skill.md
var skillDescription string

//go:embed skill_result.md
var skillResultTemplateStr string

var skillResultTemplate = template.Must(template.New("skill_result").Parse(skillResultTemplateStr))

// SkillTool executes skills within the conversation.
type SkillTool struct {
	// dirs are the directories to search for skills.
	// First directory has highest priority (workspace), then global.
	dirs []string
}

// NewSkillTool creates a new skill execution tool.
// dirs should be provided in priority order: workspace dir first, then global dir.
func NewSkillTool(dirs ...string) *SkillTool {
	return &SkillTool{dirs: dirs}
}

type skillInput struct {
	Skill string `json:"skill"`
	Args  string `json:"args,omitempty"`
}

func (t *SkillTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "Skill",
		Description: skillDescription,
		Parameters: jsonschema.Schema{
			Schema: "https://json-schema.org/draft/2020-12/schema",
			Type:   "object",
			Properties: map[string]*jsonschema.Schema{
				"skill": {
					Type:        "string",
					Description: "The skill name to invoke (e.g., \"pdf\", \"commit\", \"ms-office-suite:pdf\")",
				},
				"args": {
					Type:        "string",
					Description: "Optional arguments to pass to the skill",
				},
			},
			Required: []string{"skill"},
		},
	}
}

func (t *SkillTool) Execute(_ context.Context, arguments json.RawMessage) agent.ToolResult {
	var input skillInput
	if err := json.Unmarshal(arguments, &input); err != nil {
		return ErrorResult(fmt.Errorf("invalid arguments: %w", err))
	}

	if input.Skill == "" {
		return ErrorResult(fmt.Errorf("skill name is required"))
	}

	// Support fully qualified names like "ms-office-suite:pdf"
	parts := strings.SplitN(input.Skill, ":", 2)
	skillName := parts[len(parts)-1]

	// Use skills.Get to find the skill
	result, err := skills.Get(skills.GetOptions{
		Skill: skillName,
		Dirs:  t.dirs,
	})
	if err != nil {
		return ErrorResult(fmt.Errorf("skill %q not found: %w", input.Skill, err))
	}

	// Build the result with skill content using template
	data := struct {
		Name    string
		Dir     string
		Content string
	}{
		Name:    result.Skill.Name,
		Dir:     result.Skill.Dir,
		Content: result.Skill.Content,
	}

	var sb strings.Builder
	if err := skillResultTemplate.Execute(&sb, data); err != nil {
		return ErrorResult(fmt.Errorf("failed to execute skill result template: %w", err))
	}

	if input.Args != "" {
		fmt.Fprintf(&sb, "\nArguments: %s", input.Args)
	}

	return TextResult(sb.String())
}

// ListSkills returns all available skills from the configured directories.
func (t *SkillTool) ListSkills() ([]skills.ListedSkill, error) {
	return skills.List(skills.ListOptions{
		Dirs: t.dirs,
	})
}
