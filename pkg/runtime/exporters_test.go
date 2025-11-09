package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMetricExporterWithStatsRecordsErrors(t *testing.T) {
	t.Parallel()

	exportErr := ewrap.New("export boom")
	inner := &stubMetricExporter{
		exportErr:   exportErr,
		forceErr:    ewrap.New("flush boom"),
		shutdownErr: ewrap.New("shutdown boom"),
	}

	stats := &metricExporterStats{
		protocol: "grpc",
		endpoint: "collector:4317",
	}

	wrapper := &metricExporterWithStats{
		inner: inner,
		stats: stats,
	}

	err := wrapper.Export(context.Background(), &metricdata.ResourceMetrics{})
	if err == nil || !strings.Contains(err.Error(), "export metrics") {
		t.Fatalf("expected wrapped export error, got %v", err)
	}

	last := stats.lastError.Load()
	if last == nil || last.message != exportErr.Error() {
		t.Fatal("expected stats to capture export error")
	}

	if wrapper.ForceFlush(context.Background()) == nil {
		t.Fatal("expected force flush error")
	}

	if wrapper.Shutdown(context.Background()) == nil {
		t.Fatal("expected shutdown error")
	}
}

type stubMetricExporter struct {
	exportErr   error
	forceErr    error
	shutdownErr error
}

func (*stubMetricExporter) Temporality(metric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}

func (*stubMetricExporter) Aggregation(metric.InstrumentKind) metric.Aggregation {
	return metric.AggregationDefault{}
}

func (s *stubMetricExporter) Export(context.Context, *metricdata.ResourceMetrics) error {
	return s.exportErr
}

func (s *stubMetricExporter) ForceFlush(context.Context) error {
	return s.forceErr
}

func (s *stubMetricExporter) Shutdown(context.Context) error {
	return s.shutdownErr
}
