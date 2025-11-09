package runtime

import (
	"context"

	"github.com/hyp3rd/ewrap"
	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/hyp3rd/observe/internal/constants"
)

type runtimeMetricsController struct {
	state        *MetricsState
	registration metric.Registration
}

func (c *runtimeMetricsController) start(rt *Runtime, provider *sdkmetric.MeterProvider) error {
	err := runtimemetrics.Start(
		runtimemetrics.WithMeterProvider(provider),
	)
	if err != nil {
		return ewrap.Wrap(err, "start runtime metrics", ewrap.WithRetry(constants.DefaultRetryAttempts, constants.DefaultRetryDelay))
	}

	instruments, err := newRuntimeInstruments(provider)
	if err != nil {
		return err
	}

	reg, err := instruments.registerCallback(rt, c.state)
	if err != nil {
		return err
	}

	c.registration = reg

	return nil
}

func (c *runtimeMetricsController) shutdown() error {
	if c == nil {
		return nil
	}

	if c.registration != nil {
		err := c.registration.Unregister()
		if err != nil {
			return ewrap.Wrap(
				err,
				"unregister runtime metrics",
				ewrap.WithRetry(constants.DefaultRetryAttempts, constants.DefaultRetryDelay),
			)
		}
	}

	return nil
}

func boolToInt(v bool) int64 {
	if v {
		return 1
	}

	return 0
}

type runtimeInstruments struct {
	meter                metric.Meter
	configReloads        metric.Int64ObservableCounter
	instrumentationGauge metric.Int64ObservableGauge
	queueGauge           metric.Int64ObservableGauge
	droppedCounter       metric.Int64ObservableCounter
}

func newRuntimeInstruments(provider *sdkmetric.MeterProvider) (*runtimeInstruments, error) {
	meter := provider.Meter("observe/runtime")

	configReloads, err := meter.Int64ObservableCounter(
		"observe.runtime.config.reloads",
		metric.WithDescription("Cumulative number of configuration reloads applied by the runtime"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create config reloads counter")
	}

	instrumentationGauge, err := meter.Int64ObservableGauge(
		"observe.runtime.instrumentation.enabled",
		metric.WithDescription("Status (0=disabled,1=enabled) for built-in instrumentation modules"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create instrumentation enabled gauge")
	}

	queueGauge, err := meter.Int64ObservableGauge(
		"observe.runtime.trace.queue.limit",
		metric.WithDescription("Configured size of the trace batch processor queue"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create trace queue limit gauge")
	}

	droppedCounter, err := meter.Int64ObservableCounter(
		"observe.runtime.trace.dropped_spans",
		metric.WithDescription("Cumulative number of spans dropped due to exporter failures"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create dropped spans counter")
	}

	return &runtimeInstruments{
		meter:                meter,
		configReloads:        configReloads,
		instrumentationGauge: instrumentationGauge,
		queueGauge:           queueGauge,
		droppedCounter:       droppedCounter,
	}, nil
}

func (ri *runtimeInstruments) registerCallback(rt *Runtime, state *MetricsState) (metric.Registration, error) {
	reg, err := ri.meter.RegisterCallback(
		func(_ context.Context, observer metric.Observer) error {
			if state != nil {
				observer.ObserveInt64(ri.configReloads, state.ConfigReloads())
			}

			ri.observeModule(observer, rt.httpMiddleware != nil, "http")
			ri.observeModule(observer, rt.grpcServerInt != nil, "grpc")
			ri.observeModule(observer, rt.sqlHelper != nil, "sql")

			ri.observeTracerStats(observer, rt.exporters)

			return nil
		},
		ri.configReloads,
		ri.instrumentationGauge,
		ri.queueGauge,
		ri.droppedCounter,
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "register runtime metrics callback",
			ewrap.WithRetry(constants.DefaultRetryAttempts, constants.DefaultRetryDelay))
	}

	return reg, nil
}

func (ri *runtimeInstruments) observeModule(observer metric.Observer, enabled bool, name string) {
	observer.ObserveInt64(
		ri.instrumentationGauge,
		boolToInt(enabled),
		metric.WithAttributes(attribute.String("module", name)),
	)
}

func (ri *runtimeInstruments) observeTracerStats(observer metric.Observer, bundle *exporterBundle) {
	if bundle == nil || bundle.traceStats == nil {
		return
	}

	stats := bundle.traceStats
	observer.ObserveInt64(
		ri.queueGauge,
		stats.queueLimit,
		metric.WithAttributes(attribute.String("signal", "traces")),
	)
	observer.ObserveInt64(
		ri.droppedCounter,
		stats.dropped.Load(),
		metric.WithAttributes(attribute.String("signal", "traces")),
	)
}
