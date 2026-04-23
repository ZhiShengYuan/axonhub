//go:build !binary

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func buildBinary(t *testing.T) string {
	t.Helper()

	pkgDir, err := os.Getwd()
	if err != nil {
		t.Skipf("cannot determine working directory: %v", err)
		return ""
	}
	repoRoot := filepath.Dir(filepath.Dir(pkgDir))

	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "axonhub")

	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = repoRoot

	output := &bytes.Buffer{}
	cmd.Stderr = output

	if err := cmd.Run(); err != nil {
		t.Skipf("failed to build axonhub binary: %v\nOutput: %s", err, output.String())
		return ""
	}

	return binaryPath
}

func runBinary(t *testing.T, binaryPath string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = os.Environ()

	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf

	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run axonhub binary: %v", err)
			return "", "", -1
		}
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode
}