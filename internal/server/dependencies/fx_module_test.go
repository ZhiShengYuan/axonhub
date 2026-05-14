package dependencies

import (
	"testing"

	"github.com/looplj/axonhub/internal/server/db"
)

func configEntWire(cfg db.ConfigDB) string {
	return "config:" + cfg.DSN
}

func logEntWire(cfg db.LogsDB) string {
	return "log:" + cfg.DSN
}

func TestConfigDBLogsDBDistinct(t *testing.T) {
	var _ func(db.ConfigDB) string = configEntWire
	var _ func(db.LogsDB) string = logEntWire

	cfgDB := db.Config{DSN: "config-dsn"}
	cfgLog := db.Config{DSN: "log-dsn"}

	r1 := configEntWire(db.ConfigDB{Config: cfgDB})
	r2 := logEntWire(db.LogsDB{Config: cfgLog})

	if r1 != "config:config-dsn" {
		t.Errorf("configEntWire: expected 'config:config-dsn', got %q", r1)
	}
	if r2 != "log:log-dsn" {
		t.Errorf("logEntWire: expected 'log:log-dsn', got %q", r2)
	}
}

func TestNewConfigEntClient(t *testing.T) {
	cfg := db.ConfigDB{
		Config: db.Config{
			Dialect: "sqlite3",
			DSN:     "file::memory:?cache=shared",
		},
	}

	client := db.NewConfigEntClient(cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}

func TestNewLogEntClient(t *testing.T) {
	cfg := db.LogsDB{
		Config: db.Config{
			Dialect: "sqlite3",
			DSN:     "file::memory:?cache=shared",
		},
	}

	client := db.NewLogEntClient(cfg)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	defer client.Close()
}