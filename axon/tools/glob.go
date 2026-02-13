package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/looplj/axonhub/axon/agent"
	"github.com/looplj/axonhub/axon/pkg/glob"
)

//go:embed glob.md
var globDescription string

type GlobTool struct {
	workspace string
	restrict  bool
}

func NewGlobTool(workspace string, restrict bool) *GlobTool {
	return &GlobTool{
		workspace: workspace,
		restrict:  restrict,
	}
}

type globInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

func (t *GlobTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "Glob",
		Description: globDescription,
		Parameters: jsonschema.Schema{
			Schema: "https://json-schema.org/draft/2020-12/schema",
			Type:   "object",
			Properties: map[string]*jsonschema.Schema{
				"pattern": {
					Type:        "string",
					Description: "The glob pattern to match files against",
				},
				"path": {
					Type:        "string",
					Description: "The directory to search in. Defaults to workspace root.",
				},
			},
			Required: []string{"pattern"},
		},
	}
}

func (t *GlobTool) Execute(ctx context.Context, arguments json.RawMessage) agent.ToolResult {
	var input globInput
	if err := json.Unmarshal(arguments, &input); err != nil {
		return ErrorResult(fmt.Errorf("invalid arguments: %w", err))
	}

	if input.Pattern == "" {
		return ErrorResult(fmt.Errorf("pattern is required"))
	}

	searchPath := t.workspace
	if input.Path != "" {
		resolved, err := validatePath(input.Path, t.workspace, t.restrict)
		if err != nil {
			return ErrorResult(err)
		}
		searchPath = resolved
	}

	globber := glob.NewGlobber(t.workspace)
	result, err := globber.Glob(ctx, glob.Options{
		Pattern: input.Pattern,
		Path:    searchPath,
	})
	if err != nil {
		return ErrorResult(err)
	}

	if len(result.Matches) == 0 {
		return TextResult("No files found matching pattern.")
	}

	var sb strings.Builder
	for _, path := range result.Matches {
		sb.WriteString(path)
		sb.WriteString("\n")
	}
	if result.Truncated {
		fmt.Fprintf(&sb, "... (showing first %d results)\n", glob.MaxResults)
	}

	return TextResult(sb.String())
}
