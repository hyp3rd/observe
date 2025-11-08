// Package config defines the configuration structures for the application.
package config

import (
	"time"
)

// Config is the canonical configuration consumed by the observe runtime.
// It is intentionally verbose to capture all required knobs up front.
type Config struct {
	Service         ServiceConfig         `yaml:"service"         json:"service"`
	Exporters       ExporterConfig        `yaml:"exporters"       json:"exporters"`
	Sampling        SamplingConfig        `yaml:"sampling"        json:"sampling"`
	Instrumentation InstrumentationConfig `yaml:"instrumentation" json:"instrumentation"`
	Logging         LoggingConfig         `yaml:"logging"         json:"logging"`
	Diagnostics     DiagnosticsConfig     `yaml:"diagnostics"     json:"diagnostics"`
}

// ServiceConfig captures metadata propagated as OTEL resource attributes.
type ServiceConfig struct {
	Name        string            `yaml:"name"        json:"name"`
	Namespace   string            `yaml:"namespace"   json:"namespace"`
	Version     string            `yaml:"version"     json:"version"`
	Environment string            `yaml:"environment" json:"environment"`
	Attributes  map[string]string `yaml:"attributes"  json:"attributes"`
}

// BatchConfig defines batch processor settings.
type BatchConfig struct {
	Enabled        bool          `yaml:"enabled"          json:"enabled"`
	MaxExportBatch int           `yaml:"max_export_batch" json:"max_export_batch"`
	Timeout        time.Duration `yaml:"timeout"          json:"timeout"`
	MaxQueueSize   int           `yaml:"max_queue_size"   json:"max_queue_size"`
}

// RetryConfig specifies retry settings for exporters.
type RetryConfig struct {
	Enabled         bool          `yaml:"enabled"          json:"enabled"`
	MaxElapsedTime  time.Duration `yaml:"max_elapsed_time" json:"max_elapsed_time"`
	InitialInterval time.Duration `yaml:"initial_interval" json:"initial_interval"`
	MaxInterval     time.Duration `yaml:"max_interval"     json:"max_interval"`
}

// TLSConfig encapsulates TLS dial settings.
type TLSConfig struct {
	CAFile   string `yaml:"ca_file"   json:"ca_file"`
	CertFile string `yaml:"cert_file" json:"cert_file"`
	KeyFile  string `yaml:"key_file"  json:"key_file"`
	Insecure bool   `yaml:"insecure"  json:"insecure"`
}

// SamplingConfig defines tracing sampling strategies.
type SamplingConfig struct {
	Mode          string              `yaml:"mode"           json:"mode"`
	Argument      float64             `yaml:"argument"       json:"argument"`
	TenantLimiter TenantLimiterConfig `yaml:"tenant_limiter" json:"tenant_limiter"`
}

// TenantLimiterConfig throttles noisy tenants.
type TenantLimiterConfig struct {
	Enabled bool    `yaml:"enabled" json:"enabled"`
	Rate    float64 `yaml:"rate"    json:"rate"`
}

// InstrumentationConfig toggles modules.
type InstrumentationConfig struct {
	HTTP           HTTPInstrumentationConfig      `yaml:"http"            json:"http"`
	GRPC           GRPCInstrumentationConfig      `yaml:"grpc"            json:"grpc"`
	SQL            SQLInstrumentationConfig       `yaml:"sql"             json:"sql"`
	Messaging      MessagingInstrumentationConfig `yaml:"messaging"       json:"messaging"`
	RuntimeMetrics RuntimeMetricsConfig           `yaml:"runtime_metrics" json:"runtime_metrics"`
}

// MessagingInstrumentationConfig configures messaging instrumentation.
type MessagingInstrumentationConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// RuntimeMetricsConfig toggles runtime metrics collection.
type RuntimeMetricsConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// LoggingConfig controls structured log behavior.
type LoggingConfig struct {
	Level       string  `yaml:"level"        json:"level"`
	Format      string  `yaml:"format"       json:"format"`
	Adapter     string  `yaml:"adapter"      json:"adapter"`
	SampleRatio float64 `yaml:"sample_ratio" json:"sample_ratio"`
}

// DiagnosticsConfig toggles self-observation endpoints.
type DiagnosticsConfig struct {
	Enabled   bool   `yaml:"enabled"    json:"enabled"`
	HTTPAddr  string `yaml:"http_addr"  json:"http_addr"`
	AuthToken string `yaml:"auth_token" json:"auth_token"`
}
