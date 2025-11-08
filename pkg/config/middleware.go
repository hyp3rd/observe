package config

// HTTPInstrumentationConfig configures HTTP middleware.
type HTTPInstrumentationConfig struct {
	Enabled       bool     `yaml:"enabled"        json:"enabled"`
	IgnoredRoutes []string `yaml:"ignored_routes" json:"ignored_routes"`
}

// GRPCInstrumentationConfig configures gRPC interceptors.
type GRPCInstrumentationConfig struct {
	Enabled           bool     `yaml:"enabled"            json:"enabled"`
	MetadataAllowlist []string `yaml:"metadata_allowlist" json:"metadata_allowlist"`
}

// SQLInstrumentationConfig configures SQL instrumentation.
type SQLInstrumentationConfig struct {
	Enabled        bool `yaml:"enabled"         json:"enabled"`
	CollectQueries bool `yaml:"collect_queries" json:"collect_queries"`
}
