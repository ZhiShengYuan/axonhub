package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupContract_ProcessExitWithStderr(t *testing.T) {
	// Get repo root (go test runs from package dir cmd/axonhub/, go up two levels)
	pkgDir, err := os.Getwd()
	if err != nil {
		t.Skipf("Skipping test: cannot determine working directory: %v", err)
		return
	}
	// filepath.Dir of /home/kexi/axonhub/cmd/axonhub is /home/kexi/axonhub/cmd, so need two Dirs
	repoRoot := filepath.Dir(filepath.Dir(pkgDir))

	// Build the binary first
	cmd := exec.Command("go", "build", "-o", "axonhub_test_binary", "./cmd/axonhub")
	cmd.Dir = repoRoot
	buildErr := cmd.Run()
	if buildErr != nil {
		t.Skipf("Skipping test: failed to build binary: %v", buildErr)
		return
	}
	defer os.Remove("axonhub_test_binary")

	// Run with invalid log level to trigger config error during startup
	// This causes conf.Load() to return an error which fx propagates
	// The error must be logged to stdio before exit
	execCmd := exec.Command("./axonhub_test_binary")
	execCmd.Env = append(os.Environ(), "AXONHUB_LOG_LEVEL=invalid")
	execCmd.Dir = repoRoot

	output, err := execCmd.CombinedOutput()
	exitCode := -1
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}
	}

	// Startup failure MUST exit non-zero
	if exitCode == 0 {
		t.Error("Expected non-zero exit code for startup failure, got 0")
	}

	// Startup failure MUST emit stdio-visible error
	outputStr := string(output)
	if outputStr == "" {
		t.Error("Expected stdio-visible error output for startup failure, got empty output")
	}

	// Error should contain indication of the failure (either from fx logger or our stderr handler)
	if !strings.Contains(outputStr, "error") && !strings.Contains(outputStr, "Error") && !strings.Contains(outputStr, "ERROR") {
		t.Errorf("Expected error message in output, got: %s", outputStr)
	}
}