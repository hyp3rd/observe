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
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"

	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/diagnostics"
	observegrpc "github.com/hyp3rd/observe/pkg/instrumentation/grpc"
	observehttp "github.com/hyp3rd/observe/pkg/instrumentation/http"
	observemsg "github.com/hyp3rd/observe/pkg/instrumentation/messaging"
	observesql "github.com/hyp3rd/observe/pkg/instrumentation/sql"
	observeworker "github.com/hyp3rd/observe/pkg/instrumentation/worker"
)

// Runtime encapsulates the active telemetry providers and lifecycle hooks.
type Runtime struct {
	cfg config.Config

	tracerProvider  *sdktrace.TracerProvider
	meterProvider   *sdkmetric.MeterProvider
	exporters       *exporterBundle
	httpMiddleware  *observehttp.Middleware
	grpcServerInt   grpc.UnaryServerInterceptor
	grpcClientInt   grpc.UnaryClientInterceptor
	messagingHelper *observemsg.Helper
	metrics         *runtimeMetricsController
	sqlHelper       *observesql.Helper
	workerHelper    *observeworker.Helper
	diagServer      *diagnostics.Server
	startTime       time.Time
	lastReload      time.Time

	mu    sync.RWMutex
	state runtimeState
	once  sync.Once

	metricsState *MetricsState
}

type runtimeState struct {
	shutdown bool
}

// New creates a Runtime from the supplied Config.
//
//nolint:revive // cognitive-complexity: acceptable for a constructor function.
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

	if cfg.Instrumentation.SQL.Enabled {
		rt.sqlHelper = observesql.NewHelper(cfg.Instrumentation.SQL)
	}

	if cfg.Instrumentation.Messaging.Enabled {
		mHelper, err := observemsg.NewHelper(tp, mp)
		if err != nil {
			return nil, ewrap.Wrap(err, "init messaging instrumentation")
		}

		rt.messagingHelper = mHelper
	}

	if cfg.Instrumentation.Worker.Enabled {
		wHelper, err := observeworker.NewHelper(tp, mp)
		if err != nil {
			return nil, ewrap.Wrap(err, "init worker instrumentation")
		}

		rt.workerHelper = wHelper
	}

	if cfg.Diagnostics.Enabled {
		err := rt.startDiagnosticsServer(ctx, cfg.Diagnostics)
		if err != nil {
			return nil, ewrap.Wrap(err, "start diagnostics server")
		}
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

// SQLHelper exposes the SQL instrumentation helper when enabled.
func (r *Runtime) SQLHelper() *observesql.Helper {
	return r.sqlHelper
}

// MessagingHelper exposes the messaging instrumentation helper when enabled.
func (r *Runtime) MessagingHelper() *observemsg.Helper {
	return r.messagingHelper
}

// WorkerHelper exposes the worker instrumentation helper when enabled.
func (r *Runtime) WorkerHelper() *observeworker.Helper {
	return r.workerHelper
}

// InitMetrics wires runtime-level metrics if enabled in configuration.
func (r *Runtime) InitMetrics(state *MetricsState) error {
	if !r.cfg.Instrumentation.RuntimeMetrics.Enabled {
		return nil
	}

	r.metricsState = state

	controller := &runtimeMetricsController{
		state: state,
	}

	err := controller.start(r, r.meterProvider)
	if err != nil {
		return err
	}

	r.metrics = controller

	return nil
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

		if r.metrics != nil {
			err := r.metrics.shutdown()
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
		semconv.DeploymentEnvironmentNameKey.String(svc.Environment),
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

	queueLimit := int64(0)
	droppedSpans := int64(0)

	if r.exporters != nil && r.exporters.traceStats != nil {
		queueLimit = r.exporters.traceStats.queueLimit
		droppedSpans = r.exporters.traceStats.dropped.Load()
	}

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
			"sql":            r.sqlHelper != nil,
			"messaging":      r.messagingHelper != nil,
			"worker":         r.workerHelper != nil,
			"runtimeMetrics": r.metrics != nil,
		},
		ConfigReloadCount: reloadCount(r.metricsState),
		TraceQueueLimit:   queueLimit,
		TraceDroppedSpans: droppedSpans,
		TraceExporter:     exporterStatus(r.exporters),
		MetricExporter:    metricExporterStatus(r.exporters),
	}
}

func endpointForSnapshot(cfg config.Config) string {
	if cfg.Exporters.OTLP == nil {
		return ""
	}

	return cfg.Exporters.OTLP.Endpoint
}

func reloadCount(state *MetricsState) int64 {
	if state == nil {
		return 0
	}

	return state.ConfigReloads()
}

func exporterStatus(bundle *exporterBundle) diagnostics.ExporterStatus {
	if bundle == nil || bundle.traceStats == nil {
		return diagnostics.ExporterStatus{}
	}

	return bundle.traceStats.statusSnapshot()
}

func metricExporterStatus(bundle *exporterBundle) diagnostics.ExporterStatus {
	if bundle == nil || bundle.metricStats == nil {
		return diagnostics.ExporterStatus{}
	}

	return bundle.metricStats.statusSnapshot()
}

func (r *Runtime) startDiagnosticsServer(ctx context.Context, cfg config.DiagnosticsConfig) error {
	server := diagnostics.NewServer(cfg, r)

	err := server.Start(ctx)
	if err != nil {
		return ewrap.Wrap(err, "start diagnostics server")
	}

	r.diagServer = server

	return nil
}
