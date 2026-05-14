package db

import "time"

// ConfigDB marks a db.Config as the main/config database config.
// Used by fx to distinguish the config DB from the log DB in DI.
// Embeds db.Config so field accesses (DSN, Dialect, etc.) are promoted.
type ConfigDB struct {
	Config `conf:"db"`
}

// LogsDB marks a db.Config as the log/audit database config.
// Used by fx to distinguish the log DB from the config DB in DI.
// Embeds db.Config so field accesses (DSN, Dialect, etc.) are promoted.
type LogsDB struct {
	Config `conf:"db_logs"`
}

// Config holds database connection settings.
type Config struct {
	Dialect         string        `conf:"dialect" yaml:"dialect" json:"dialect"`
	DSN             string        `conf:"dsn" yaml:"dsn" json:"dsn"`
	Debug           bool          `conf:"debug" yaml:"debug" json:"debug"`
	MaxOpenConns    int           `conf:"max_open_conns" yaml:"max_open_conns" json:"max_open_conns"`
	MaxIdleConns    int           `conf:"max_idle_conns" yaml:"max_idle_conns" json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `conf:"conn_max_lifetime" yaml:"conn_max_lifetime" json:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `conf:"conn_max_idle_time" yaml:"conn_max_idle_time" json:"conn_max_idle_time"`
}

// Unwrap returns the underlying db.Config from a ConfigDB wrapper.
func (c ConfigDB) Unwrap() Config { return c.Config }

// Unwrap returns the underlying db.Config from a LogsDB wrapper.
func (c LogsDB) Unwrap() Config { return c.Config }
