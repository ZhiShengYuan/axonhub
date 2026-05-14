package db

import (
	"testing"
)

func TestNewEntClientForSQLite(t *testing.T) {
	cfg := Config{
		Dialect: "sqlite3",
		DSN:     "file::memory:?cache=shared",
	}

	client := NewEntClientFor("test", cfg, false)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestNewConfigEntClient(t *testing.T) {
	cfg := ConfigDB{
		Config: Config{
			Dialect: "sqlite3",
			DSN:     "file::memory:?cache=shared",
		},
	}

	client := NewConfigEntClient(cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestNewLogEntClient(t *testing.T) {
	cfg := LogsDB{
		Config: Config{
			Dialect: "sqlite3",
			DSN:     "file::memory:?cache=shared",
		},
	}

	client := NewLogEntClient(cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestConfigDBLogsDBDistinctTypes(t *testing.T) {
	cfgDB := ConfigDB{
		Config: Config{
			Dialect: "sqlite3",
			DSN:     "file:config.db",
		},
	}

	logDB := LogsDB{
		Config: Config{
			Dialect: "sqlite3",
			DSN:     "file:log.db",
		},
	}

	if cfgDB.DSN == logDB.DSN {
		t.Error("ConfigDB and LogsDB should have different DSNs")
	}
	if cfgDB.Dialect != "sqlite3" {
		t.Errorf("expected ConfigDB.Dialect to be sqlite3, got %s", cfgDB.Dialect)
	}
	if logDB.Dialect != "sqlite3" {
		t.Errorf("expected LogsDB.Dialect to be sqlite3, got %s", logDB.Dialect)
	}
}