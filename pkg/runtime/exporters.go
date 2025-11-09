package runtime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"

	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/diagnostics"
)

// ErrTLSNotEnabled is returned when TLS configuration is incomplete.
var ErrTLSNotEnabled = ewrap.New("tls is not enabled").WithContext(
	&ewrap.ErrorContext{
		Severity: ewrap.SeverityError,
		Type:     ewrap.ErrorTypeConfiguration,
	},
)

type exporterBundle struct {
	traceExporter  sdktrace.SpanExporter
	metricExporter sdkmetric.Exporter
	metricReader   *sdkmetric.PeriodicReader
	traceStats     *traceExporterStats
}

type traceExporterStats struct {
	queueLimit int64
	dropped    atomic.Int64
	protocol   string
	endpoint   string
	lastError  atomic.Pointer[exporterError]
}

type exporterError struct {
	message string
	time    time.Time
}

func newTraceExporterStats(cfg *config.OTLPConfig) *traceExporterStats {
	limit := int64(cfg.Batch.MaxQueueSize)
	if limit <= 0 {
		limit = 2048
	}

	protocol := cfg.Protocol
	if protocol == "" {
		protocol = "grpc"
	}

	return &traceExporterStats{
		queueLimit: limit,
		protocol:   strings.ToLower(protocol),
		endpoint:   cfg.Endpoint,
	}
}

func (s *traceExporterStats) recordDrop(n int64) {
	if s == nil || n <= 0 {
		return
	}

	s.dropped.Add(n)
}

func (s *traceExporterStats) recordError(err error) {
	if s == nil || err == nil {
		return
	}

	s.lastError.Store(&exporterError{
		message: err.Error(),
		time:    time.Now().UTC(),
	})
}

func (s *traceExporterStats) statusSnapshot() diagnostics.ExporterStatus {
	status := diagnostics.ExporterStatus{
		Protocol: strings.ToLower(s.protocol),
		Endpoint: s.endpoint,
	}
	if last := s.lastError.Load(); last != nil {
		status.LastError = last.message
		status.LastErrorTime = last.time
	}

	return status
}

func newExporterBundle(ctx context.Context, cfg config.ExporterConfig) (*exporterBundle, error) {
	if cfg.OTLP == nil {
		return nil, ewrap.New("otlp exporter config is required")
	}

	if cfg.OTLP.Endpoint == "" {
		return nil, ewrap.New("otlp exporter endpoint is required")
	}

	traceExp, err := newOTLPTraceExporter(ctx, cfg.OTLP)
	if err != nil {
		return nil, err
	}

	traceStats := newTraceExporterStats(cfg.OTLP)
	traceExp = &spanExporterWithStats{
		inner: traceExp,
		stats: traceStats,
	}

	metricExp, err := newOTLPMetricExporter(ctx, cfg.OTLP)
	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(
		metricExp,
		sdkmetric.WithInterval(time.Minute),
	)

	return &exporterBundle{
		traceExporter:  traceExp,
		metricExporter: metricExp,
		metricReader:   reader,
		traceStats:     traceStats,
	}, nil
}

func (b *exporterBundle) shutdown(ctx context.Context) error {
	var errs []error

	if b.metricReader != nil {
		err := b.metricReader.Shutdown(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if b.metricExporter != nil {
		err := b.metricExporter.Shutdown(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if b.traceExporter != nil {
		err := b.traceExporter.Shutdown(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func newOTLPTraceExporter(ctx context.Context, cfg *config.OTLPConfig) (sdktrace.SpanExporter, error) {
	switch strings.ToLower(cfg.Protocol) {
	case "http", "https":
		opts, err := otlpHTTPOptions(cfg)
		if err != nil {
			return nil, err
		}

		exp, err := otlptracehttp.New(ctx, opts...)
		if err != nil {
			return nil, ewrap.Wrap(err, "create otlp http trace exporter")
		}

		return exp, nil
	default:
		opts, err := otlpGRPCOptions(cfg)
		if err != nil {
			return nil, err
		}

		exp, err := otlptracegrpc.New(ctx, opts...)
		if err != nil {
			return nil, ewrap.Wrap(err, "create otlp grpc trace exporter")
		}

		return exp, nil
	}
}

func newOTLPMetricExporter(ctx context.Context, cfg *config.OTLPConfig) (sdkmetric.Exporter, error) {
	switch strings.ToLower(cfg.Protocol) {
	case "http", "https":
		opts, err := otlpMetricHTTPOptions(cfg)
		if err != nil {
			return nil, err
		}

		exp, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			return nil, ewrap.Wrap(err, "create otlp http metric exporter")
		}

		return exp, nil
	default:
		opts, err := otlpMetricGRPCOptions(cfg)
		if err != nil {
			return nil, err
		}

		exp, err := otlpmetricgrpc.New(ctx, opts...)
		if err != nil {
			return nil, ewrap.Wrap(err, "create otlp grpc metric exporter")
		}

		return exp, nil
	}
}

func otlpGRPCOptions(cfg *config.OTLPConfig) ([]otlptracegrpc.Option, error) {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	} else {
		tlsCfg, err := tlsConfigFrom(cfg.TLS)
		if err != nil && !ErrTLSNotEnabled.Is(err) {
			return nil, err
		}

		if tlsCfg != nil {
			opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(tlsCfg)))
		}
	}

	if cfg.Timeout > 0 {
		opts = append(opts, otlptracegrpc.WithTimeout(cfg.Timeout))
	}

	if cfg.Compression != "" {
		opts = append(opts, otlptracegrpc.WithCompressor(cfg.Compression))
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
	}

	if cfg.Retry.Enabled {
		opts = append(opts, otlptracegrpc.WithRetry(otlptracegrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	}

	return opts, nil
}

func otlpHTTPOptions(cfg *config.OTLPConfig) ([]otlptracehttp.Option, error) {
	return buildHTTPOptions(cfg, httpOptionFactory[otlptracehttp.Option]{
		withEndpoint: otlptracehttp.WithEndpoint,
		withInsecure: otlptracehttp.WithInsecure,
		withTLS:      otlptracehttp.WithTLSClientConfig,
		withTimeout:  otlptracehttp.WithTimeout,
		withHeaders:  otlptracehttp.WithHeaders,
		withCompression: func(value string) (otlptracehttp.Option, bool) {
			return otlptracehttp.WithCompression(traceHTTPCompression(value)), true
		},
		withRetry: func(retryCfg config.RetryConfig) otlptracehttp.Option {
			return otlptracehttp.WithRetry(otlptracehttp.RetryConfig{
				Enabled:         true,
				InitialInterval: retryCfg.InitialInterval,
				MaxInterval:     retryCfg.MaxInterval,
				MaxElapsedTime:  retryCfg.MaxElapsedTime,
			})
		},
	})
}

func otlpMetricGRPCOptions(cfg *config.OTLPConfig) ([]otlpmetricgrpc.Option, error) {
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	} else {
		tlsCfg, err := tlsConfigFrom(cfg.TLS)
		if err != nil && !ErrTLSNotEnabled.Is(err) {
			return nil, err
		}

		if tlsCfg != nil {
			opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(tlsCfg)))
		}
	}

	if cfg.Timeout > 0 {
		opts = append(opts, otlpmetricgrpc.WithTimeout(cfg.Timeout))
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(cfg.Headers))
	}

	if cmp := strings.ToLower(cfg.Compression); cmp != "" {
		opts = append(opts, otlpmetricgrpc.WithCompressor(cmp))
	}

	if cfg.Retry.Enabled {
		opts = append(opts, otlpmetricgrpc.WithRetry(otlpmetricgrpc.RetryConfig{
			Enabled:         true,
			InitialInterval: cfg.Retry.InitialInterval,
			MaxInterval:     cfg.Retry.MaxInterval,
			MaxElapsedTime:  cfg.Retry.MaxElapsedTime,
		}))
	}

	return opts, nil
}

func otlpMetricHTTPOptions(cfg *config.OTLPConfig) ([]otlpmetrichttp.Option, error) {
	return buildHTTPOptions(cfg, httpOptionFactory[otlpmetrichttp.Option]{
		withEndpoint: otlpmetrichttp.WithEndpoint,
		withInsecure: otlpmetrichttp.WithInsecure,
		withTLS:      otlpmetrichttp.WithTLSClientConfig,
		withTimeout:  otlpmetrichttp.WithTimeout,
		withHeaders:  otlpmetrichttp.WithHeaders,
		withCompression: func(value string) (otlpmetrichttp.Option, bool) {
			return otlpmetrichttp.WithCompression(metricHTTPCompression(value)), true
		},
		withRetry: func(retryCfg config.RetryConfig) otlpmetrichttp.Option {
			return otlpmetrichttp.WithRetry(otlpmetrichttp.RetryConfig{
				Enabled:         true,
				InitialInterval: retryCfg.InitialInterval,
				MaxInterval:     retryCfg.MaxInterval,
				MaxElapsedTime:  retryCfg.MaxElapsedTime,
			})
		},
	})
}

type httpOptionFactory[T any] struct {
	withEndpoint    func(string) T
	withInsecure    func() T
	withTLS         func(*tls.Config) T
	withTimeout     func(time.Duration) T
	withHeaders     func(map[string]string) T
	withCompression func(string) (T, bool)
	withRetry       func(config.RetryConfig) T
}

func buildHTTPOptions[T any](cfg *config.OTLPConfig, factory httpOptionFactory[T]) ([]T, error) {
	opts := []T{factory.withEndpoint(cfg.Endpoint)}
	if cfg.Insecure {
		opts = append(opts, factory.withInsecure())
	} else {
		tlsCfg, err := tlsConfigFrom(cfg.TLS)
		if err != nil && !ErrTLSNotEnabled.Is(err) {
			return nil, err
		}

		if tlsCfg != nil {
			opts = append(opts, factory.withTLS(tlsCfg))
		}
	}

	if cfg.Timeout > 0 {
		opts = append(opts, factory.withTimeout(cfg.Timeout))
	}

	if len(cfg.Headers) > 0 {
		opts = append(opts, factory.withHeaders(cfg.Headers))
	}

	if factory.withCompression != nil && cfg.Compression != "" {
		opt, ok := factory.withCompression(strings.ToLower(cfg.Compression))
		if ok {
			opts = append(opts, opt)
		}
	}

	if factory.withRetry != nil && cfg.Retry.Enabled {
		opts = append(opts, factory.withRetry(cfg.Retry))
	}

	return opts, nil
}

func traceHTTPCompression(value string) otlptracehttp.Compression {
	if value == "gzip" {
		return otlptracehttp.GzipCompression
	}

	return otlptracehttp.NoCompression
}

func metricHTTPCompression(value string) otlpmetrichttp.Compression {
	if value == "gzip" {
		return otlpmetrichttp.GzipCompression
	}

	return otlpmetrichttp.NoCompression
}

type spanExporterWithStats struct {
	inner sdktrace.SpanExporter
	stats *traceExporterStats
}

func (s *spanExporterWithStats) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if s == nil || s.inner == nil {
		return nil
	}

	err := s.inner.ExportSpans(ctx, spans)
	if err != nil {
		if s.stats != nil {
			s.stats.recordDrop(int64(len(spans)))
			s.stats.recordError(err)
		}

		return ewrap.Wrap(err, "export spans")
	}

	return nil
}

func (s *spanExporterWithStats) Shutdown(ctx context.Context) error {
	if s == nil || s.inner == nil {
		return nil
	}

	err := s.inner.Shutdown(ctx)
	if err != nil {
		return ewrap.Wrap(err, "shutdown span exporter")
	}

	return nil
}

// tlsConfigFrom builds a tls.Config from the provided TLSConfig.
func tlsConfigFrom(cfg config.TLSConfig) (*tls.Config, error) {
	if cfg.CAFile == "" && cfg.CertFile == "" && cfg.KeyFile == "" && !cfg.Insecure {
		return nil, ErrTLSNotEnabled
	}

	tlsCfg := &tls.Config{
		//nolint:gosec // allow insecure skip verify via config.
		InsecureSkipVerify: cfg.Insecure,
	}

	if cfg.CAFile != "" {
		data, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, ewrap.Wrapf(err, "read ca file %s", cfg.CAFile)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, ewrap.Newf("failed to parse ca file %s", cfg.CAFile)
		}

		tlsCfg.RootCAs = pool
	}

	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, ewrap.New("tls cert_file and key_file must both be set")
		}

		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, ewrap.Wrap(err, "load tls client certificate")
		}

		tlsCfg.Certificates = []tls.Certificate{cert}
	}

	return tlsCfg, nil
}
