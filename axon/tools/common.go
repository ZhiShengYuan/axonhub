package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/looplj/axonhub/axon/agent"
	"github.com/samber/lo"
)

func validatePath(path, workspace string, restrict bool) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}

	path = filepath.Clean(path)

	if restrict && !strings.HasPrefix(path, filepath.Clean(workspace)+string(filepath.Separator)) && path != filepath.Clean(workspace) {
		return "", fmt.Errorf("path %q is outside the workspace %q", path, workspace)
	}

	return path, nil
}

func TextResult(text string) agent.ToolResult {
	return agent.ToolResult{
		Content: agent.Content{Text: lo.ToPtr(text)},
	}
}

func ErrorResult(err error) agent.ToolResult {
	return agent.ToolResult{Error: err}
}
