package config

import "time"

// ExporterConfig enumerates supported telemetry exporters.
type ExporterConfig struct {
	OTLP *OTLPConfig `yaml:"otlp" json:"otlp"`
}

// OTLPConfig defines both gRPC and HTTP export settings.
type OTLPConfig struct {
	Protocol    string            `yaml:"protocol"    json:"protocol"`
	Endpoint    string            `yaml:"endpoint"    json:"endpoint"`
	Insecure    bool              `yaml:"insecure"    json:"insecure"`
	Headers     map[string]string `yaml:"headers"     json:"headers"`
	Timeout     time.Duration     `yaml:"timeout"     json:"timeout"`
	Batch       BatchConfig       `yaml:"batch"       json:"batch"`
	Retry       RetryConfig       `yaml:"retry"       json:"retry"`
	TLS         TLSConfig         `yaml:"tls"         json:"tls"`
	Compression string            `yaml:"compression" json:"compression"`
}
