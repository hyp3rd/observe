package worker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/hyp3rd/observe/pkg/instrumentation/worker"
)

func TestHelperInstrumentSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	helper, err := worker.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("NewHelper returned error: %v", err)
	}

	info := worker.JobInfo{
		Name:  "process-order",
		Queue: "orders",
		Attributes: []attribute.KeyValue{
			attribute.String("worker.type", "cron"),
		},
	}

	err = helper.Instrument(ctx, info, func(_ context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Instrument returned error: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if spans[0].Name() != "orders:process-order" {
		t.Fatalf("unexpected span name %q", spans[0].Name())
	}

	var rm metricdata.ResourceMetrics

	err = reader.Collect(ctx, &rm)
	if err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	if !hasMetric(rm, "worker.job.count") {
		t.Fatal("expected worker.job.count metric")
	}

	if !hasMetric(rm, "worker.job.duration_ms") {
		t.Fatal("expected worker.job.duration_ms metric")
	}
}

func TestHelperInstrumentError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tp := sdktrace.NewTracerProvider()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	helper, err := worker.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("NewHelper returned error: %v", err)
	}

	runErr := ewrap.New("boom")

	err = helper.Instrument(ctx, worker.JobInfo{Name: "fail"}, func(context.Context) error {
		return runErr
	})
	if !errors.Is(err, runErr) {
		t.Fatalf("expected error %v, got %v", runErr, err)
	}
}

func hasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name == name {
				return true
			}
		}
	}

	return false
}
