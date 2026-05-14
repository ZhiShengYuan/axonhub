package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDBLoadsFromEnv(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yml")

	configContent := `
db:
  dialect: sqlite3
  dsn: "file:test.db"
log:
  level: info
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	defer os.Chdir(oldCwd)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg.DB.Dialect != "sqlite3" {
		t.Errorf("expected DB.Dialect to be sqlite3, got %s", cfg.DB.Dialect)
	}

	if cfg.DB.DSN != "file:test.db" {
		t.Errorf("expected DB.DSN to be file:test.db, got %s", cfg.DB.DSN)
	}
}