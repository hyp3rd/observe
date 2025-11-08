package config

import (
	"time"

	"github.com/hyp3rd/observe/internal/constants"
)

const (
	defaultMaxElapsedTime    = 32 * time.Minute
	defaultInterval          = 500 * time.Millisecond
	defaultMaxInterval       = 5 * time.Second
	tenantLimiterDefaultRate = 10
)

// DefaultConfig returns a Config populated with production-safe defaults.
func DefaultConfig() Config {
	return Config{
		Service: ServiceConfig{
			Name:        "observe-service",
			Namespace:   "default",
			Version:     "0.0.1",
			Environment: "development",
			Attributes:  map[string]string{},
		},
		Exporters: ExporterConfig{
			OTLP: &OTLPConfig{
				Protocol: "grpc",
				Endpoint: "localhost:4317",
				Timeout:  2 * constants.DefaultTimeout,
				Batch: BatchConfig{
					Enabled:        true,
					MaxExportBatch: 512,
					Timeout:        constants.DefaultTimeout,
					MaxQueueSize:   2048,
				},
				Retry: RetryConfig{
					Enabled:         true,
					MaxElapsedTime:  defaultMaxElapsedTime,
					InitialInterval: defaultInterval,
					MaxInterval:     defaultMaxInterval,
				},
				TLS: TLSConfig{
					Insecure: true,
				},
				Compression: "gzip",
			},
		},
		Sampling: SamplingConfig{
			Mode:     "parentbased_always_on",
			Argument: 1.0,
			TenantLimiter: TenantLimiterConfig{
				Enabled: false,
				Rate:    tenantLimiterDefaultRate,
			},
		},
		Instrumentation: InstrumentationConfig{
			HTTP: HTTPInstrumentationConfig{
				Enabled: true,
			},
			GRPC: GRPCInstrumentationConfig{
				Enabled: true,
			},
			SQL: SQLInstrumentationConfig{
				Enabled: false,
			},
			Messaging: MessagingInstrumentationConfig{
				Enabled: false,
			},
			RuntimeMetrics: RuntimeMetricsConfig{
				Enabled: true,
			},
		},
		Logging: LoggingConfig{
			Level:       "info",
			Format:      "json",
			Adapter:     "slog",
			SampleRatio: 1.0,
		},
		Diagnostics: DiagnosticsConfig{
			Enabled:  true,
			HTTPAddr: "127.0.0.1:14271",
		},
	}
}
