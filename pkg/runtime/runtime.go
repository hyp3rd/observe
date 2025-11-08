// Package runtime provides a way to manage the OpenTelemetry SDK with a clean API.
package runtime

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"

	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/diagnostics"
	observegrpc "github.com/hyp3rd/observe/pkg/instrumentation/grpc"
	observehttp "github.com/hyp3rd/observe/pkg/instrumentation/http"
)

// Runtime encapsulates the active telemetry providers and lifecycle hooks.
type Runtime struct {
	cfg config.Config

	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	exporters      *exporterBundle
	httpMiddleware *observehttp.Middleware
	grpcServerInt  grpc.UnaryServerInterceptor
	grpcClientInt  grpc.UnaryClientInterceptor
	diagServer     *diagnostics.Server
	startTime      time.Time
	lastReload     time.Time

	mu    sync.RWMutex
	state runtimeState
	once  sync.Once
}

type runtimeState struct {
	shutdown bool
}

// New creates a Runtime from the supplied Config.
func New(ctx context.Context, cfg config.Config) (*Runtime, error) {
	exporters, err := newExporterBundle(ctx, cfg.Exporters)
	if err != nil {
		return nil, ewrap.Wrap(err, "build exporters")
	}

	res, err := buildResource(ctx, cfg.Service)
	if err != nil {
		return nil, ewrap.Wrap(err, "build resource")
	}

	tp, err := buildTracerProvider(cfg, res, exporters.traceExporter)
	if err != nil {
		return nil, ewrap.Wrap(err, "build tracer provider")
	}

	mp := buildMeterProvider(res, exporters.metricReader)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	rt := &Runtime{
		cfg:            cfg,
		tracerProvider: tp,
		meterProvider:  mp,
		exporters:      exporters,
		startTime:      time.Now().UTC(),
	}
	rt.lastReload = rt.startTime

	if cfg.Instrumentation.HTTP.Enabled {
		mw, err := observehttp.NewMiddleware(tp, mp, cfg.Instrumentation.HTTP)
		if err != nil {
			return nil, ewrap.Wrap(err, "init http instrumentation")
		}

		rt.httpMiddleware = mw
	}

	if cfg.Instrumentation.GRPC.Enabled {
		interceptors := observegrpc.NewInterceptors(tp, cfg.Instrumentation.GRPC)
		rt.grpcServerInt = interceptors.UnaryServer()
		rt.grpcClientInt = interceptors.UnaryClient()
	}

	if cfg.Diagnostics.Enabled {
		server := diagnostics.NewServer(cfg.Diagnostics, rt)

		err := server.Start(ctx)
		if err != nil {
			return nil, ewrap.Wrap(err, "start diagnostics server")
		}

		rt.diagServer = server
	}

	return rt, nil
}

// Config returns a copy of the currently active configuration.
func (r *Runtime) Config() config.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.cfg
}

// Tracer returns an instrumented tracer for callers to use directly.
func (r *Runtime) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return r.tracerProvider.Tracer(name, opts...)
}

// Meter returns a configured meter for instrumentation libraries.
func (r *Runtime) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	return r.meterProvider.Meter(name, opts...)
}

// HTTPMiddleware exposes the HTTP middleware if enabled.
func (r *Runtime) HTTPMiddleware() *observehttp.Middleware {
	return r.httpMiddleware
}

// GRPCUnaryServerInterceptor exposes the unary server interceptor when enabled.
func (r *Runtime) GRPCUnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return r.grpcServerInt
}

// GRPCUnaryClientInterceptor exposes the unary client interceptor when enabled.
func (r *Runtime) GRPCUnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return r.grpcClientInt
}

// Shutdown releases resources and flushes telemetry.
//
//nolint:revive // cognitive-complexity: this is acceptable for a shutdown function. Breaking it up would reduce clarity.
func (r *Runtime) Shutdown(ctx context.Context) error {
	var shutdownErr error

	r.once.Do(func() {
		var errs []error

		if r.tracerProvider != nil {
			err := r.tracerProvider.Shutdown(ctx)
			if err != nil {
				errs = append(errs, err)
			}
		}

		if r.meterProvider != nil {
			err := r.meterProvider.Shutdown(ctx)
			if err != nil {
				errs = append(errs, err)
			}
		}

		if r.exporters != nil {
			err := r.exporters.shutdown(ctx)
			if err != nil {
				errs = append(errs, err)
			}
		}

		if r.diagServer != nil {
			err := r.diagServer.Shutdown(ctx)
			if err != nil {
				errs = append(errs, err)
			}
		}

		if len(errs) > 0 {
			shutdownErr = errors.Join(errs...)
		}

		r.mu.Lock()
		r.state.shutdown = true
		r.mu.Unlock()
	})

	if shutdownErr != nil {
		return ewrap.Wrap(shutdownErr, "shutdown runtime")
	}

	return nil
}

// IsShutdown indicates whether the runtime has been terminated.
func (r *Runtime) IsShutdown() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.state.shutdown
}

func buildTracerProvider(cfg config.Config, res *resource.Resource, traceExp sdktrace.SpanExporter) (*sdktrace.TracerProvider, error) {
	sampler, err := samplerFromConfig(cfg.Sampling)
	if err != nil {
		return nil, err
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
	}

	if traceExp != nil {
		if cfg.Exporters.OTLP != nil {
			opts = append(opts, exporterSpanProcessor(cfg.Exporters.OTLP.Batch, traceExp))
		} else {
			opts = append(opts, exporterSpanProcessor(config.BatchConfig{Enabled: true}, traceExp))
		}
	}

	tp := sdktrace.NewTracerProvider(opts...)

	return tp, nil
}

func buildMeterProvider(res *resource.Resource, reader *sdkmetric.PeriodicReader) *sdkmetric.MeterProvider {
	options := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}
	if reader != nil {
		options = append(options, sdkmetric.WithReader(reader))
	}

	return sdkmetric.NewMeterProvider(options...)
}

func exporterSpanProcessor(cfg config.BatchConfig, exporter sdktrace.SpanExporter) sdktrace.TracerProviderOption {
	if !cfg.Enabled {
		return sdktrace.WithSyncer(exporter)
	}

	var opts []sdktrace.BatchSpanProcessorOption
	if cfg.Timeout > 0 {
		opts = append(opts, sdktrace.WithBatchTimeout(cfg.Timeout))
	}

	if cfg.MaxExportBatch > 0 {
		opts = append(opts, sdktrace.WithMaxExportBatchSize(cfg.MaxExportBatch))
	}

	if cfg.MaxQueueSize > 0 {
		opts = append(opts, sdktrace.WithMaxQueueSize(cfg.MaxQueueSize))
	}

	return sdktrace.WithBatcher(exporter, opts...)
}

func buildResource(ctx context.Context, svc config.ServiceConfig) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceNameKey.String(svc.Name),
		semconv.ServiceVersionKey.String(svc.Version),
		semconv.DeploymentEnvironmentKey.String(svc.Environment),
	}
	if svc.Namespace != "" {
		attrs = append(attrs, semconv.ServiceNamespaceKey.String(svc.Namespace))
	}

	for k, v := range svc.Attributes {
		attrs = append(attrs, attribute.String(k, v))
	}

	envRes, err := resource.New(ctx,
		resource.WithContainer(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create environment resource")
	}

	attrRes := resource.NewWithAttributes(semconv.SchemaURL, attrs...)

	merged, err := resource.Merge(resource.Default(), envRes)
	if err != nil {
		return nil, ewrap.Wrap(err, "merge environment resource")
	}

	merged, err = resource.Merge(merged, attrRes)
	if err != nil {
		return nil, ewrap.Wrap(err, "merge attribute resource")
	}

	return merged, nil
}

func samplerFromConfig(cfg config.SamplingConfig) (sdktrace.Sampler, error) {
	switch cfg.Mode {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "trace_id_ratio":
		if cfg.Argument <= 0 || cfg.Argument > 1 {
			return nil, ewrap.Newf("sampling.argument must be within (0,1], got %f", cfg.Argument)
		}

		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.Argument)), nil
	default:
		return nil, ewrap.Newf("unsupported sampling mode %q", cfg.Mode)
	}
}

// Snapshot implements diagnostics.SnapshotProvider.
func (r *Runtime) Snapshot() diagnostics.Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return diagnostics.Snapshot{
		ServiceName:      r.cfg.Service.Name,
		ServiceVersion:   r.cfg.Service.Version,
		Environment:      r.cfg.Service.Environment,
		SamplingMode:     r.cfg.Sampling.Mode,
		ExporterEndpoint: endpointForSnapshot(r.cfg),
		StartTime:        r.startTime,
		LastReloadTime:   r.lastReload,
		Instrumentation: map[string]bool{
			"http":           r.httpMiddleware != nil,
			"grpc":           r.grpcServerInt != nil,
			"sql":            r.cfg.Instrumentation.SQL.Enabled,
			"messaging":      r.cfg.Instrumentation.Messaging.Enabled,
			"runtimeMetrics": r.cfg.Instrumentation.RuntimeMetrics.Enabled,
		},
	}
}

func endpointForSnapshot(cfg config.Config) string {
	if cfg.Exporters.OTLP == nil {
		return ""
	}

	return cfg.Exporters.OTLP.Endpoint
}
