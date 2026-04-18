package metrics

// Config specifies the configuration for metrics.
type Config struct {
	// Enabled specifies whether metrics are enabled.
	// Default is false.
	Enabled bool `conf:"enabled" yaml:"enabled" json:"enabled"`

	Exporter ExporterConfig `conf:"exporter" yaml:"exporter" json:"exporter"`

	// BasicAuth specifies basic auth credentials for the /metrics endpoint.
	BasicAuth BasicAuthConfig `conf:"basic_auth" yaml:"basic_auth" json:"basic_auth"`
}

type ExporterConfig struct {
	Type     string `conf:"type" validate:"oneof=stdout otlpgrpc otlphttp prometheus" yaml:"type" json:"type"`
	Endpoint string `conf:"endpoint" yaml:"endpoint" json:"endpoint"`
	Insecure bool   `conf:"insecure" yaml:"insecure" json:"insecure"`
}

type BasicAuthConfig struct {
	Username string `conf:"username" yaml:"username" json:"username"`
	Password string `conf:"password" yaml:"password" json:"password"`
}
