package messaging_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/hyp3rd/observe/pkg/instrumentation/messaging"
)

func TestInstrumentPublishRecordsSpanAndMetrics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	helper, err := messaging.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("NewHelper returned error: %v", err)
	}

	info := messaging.PublishInfo{
		System:          "kafka",
		Destination:     "orders",
		DestinationKind: "topic",
	}

	err = helper.InstrumentPublish(ctx, info, func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("InstrumentPublish returned error: %v", err)
	}

	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if got, want := span.Name(), "orders"; got != want {
		t.Fatalf("unexpected span name: got %q want %q", got, want)
	}

	assertHasAttr(t, span.Attributes(), semconv.MessagingSystemKey.String("kafka"))
	assertHasAttr(t, span.Attributes(), messaging.AttrDestinationKind.String("topic"))

	rm := collectMetrics(ctx, t, reader)
	if !hasMetric(rm, "messaging.publish.count") {
		t.Fatal("expected messaging.publish.count metric")
	}

	if !hasMetric(rm, "messaging.publish.latency_ms") {
		t.Fatal("expected messaging.publish.latency_ms metric")
	}
}

func TestInstrumentConsumeNilHelper(t *testing.T) {
	t.Parallel()

	var helper *messaging.Helper

	calls := 0

	err := helper.InstrumentConsume(context.Background(), messaging.ConsumeInfo{}, func(_ context.Context) error {
		calls++

		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected function to be invoked once, got %d", calls)
	}
}

func assertHasAttr(t *testing.T, attrs []attribute.KeyValue, target attribute.KeyValue) {
	t.Helper()

	for _, attr := range attrs {
		if attr.Key == target.Key {
			return
		}
	}

	t.Fatalf("attribute %s not found", target.Key)
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

func collectMetrics(ctx context.Context, t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics

	err := reader.Collect(ctx, &rm)
	if err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	return rm
}
