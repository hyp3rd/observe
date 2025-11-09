package runtime

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/diagnostics"
)

const (
	collectorEndpoint = "collector:4318"
	droppedSpans      = 7
)

func TestSamplerFromConfigModes(t *testing.T) {
	t.Parallel()

	traceID := trace.TraceID{}

	tests := []struct {
		name         string
		cfg          config.SamplingConfig
		wantDecision sdktrace.SamplingDecision
	}{
		{
			name:         "always_on",
			cfg:          config.SamplingConfig{Mode: "always_on"},
			wantDecision: sdktrace.RecordAndSample,
		},
		{
			name:         "always_off",
			cfg:          config.SamplingConfig{Mode: "always_off"},
			wantDecision: sdktrace.Drop,
		},
		{
			name:         "parentbased_always_on",
			cfg:          config.SamplingConfig{Mode: "parentbased_always_on"},
			wantDecision: sdktrace.RecordAndSample,
		},
		{
			name:         "parentbased_always_off",
			cfg:          config.SamplingConfig{Mode: "parentbased_always_off"},
			wantDecision: sdktrace.Drop,
		},
		{
			name:         "trace_id_ratio",
			cfg:          config.SamplingConfig{Mode: "trace_id_ratio", Argument: 0.25}, //nolint:revive
			wantDecision: sdktrace.RecordAndSample,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sampler, err := samplerFromConfig(tc.cfg)
			if err != nil {
				t.Fatalf("samplerFromConfig returned error: %v", err)
			}

			params := sdktrace.SamplingParameters{
				ParentContext: context.Background(),
				TraceID:       traceID,
				Name:          "test-span",
				Kind:          trace.SpanKindInternal,
			}

			decision := sampler.ShouldSample(params).Decision
			if decision != tc.wantDecision {
				t.Fatalf("expected decision %v, got %v", tc.wantDecision, decision)
			}
		})
	}
}

func TestSamplerFromConfigInvalidArgument(t *testing.T) {
	t.Parallel()

	tests := []config.SamplingConfig{
		{Mode: "trace_id_ratio", Argument: 0},
		{Mode: "trace_id_ratio", Argument: 1.5}, //nolint:revive
	}

	for _, cfg := range tests {
		_, err := samplerFromConfig(cfg)
		if err == nil {
			t.Fatalf("expected error for cfg %+v, got nil", cfg)
		}
	}
}

func TestSamplerFromConfigUnsupportedMode(t *testing.T) {
	t.Parallel()

	_, err := samplerFromConfig(config.SamplingConfig{Mode: "unknown_mode"})
	if err == nil {
		t.Fatal("expected error for unsupported mode, got nil")
	}
}

func TestEndpointForSnapshot(t *testing.T) {
	t.Parallel()

	var cfg config.Config
	if got := endpointForSnapshot(cfg); got != "" {
		t.Fatalf("expected empty endpoint, got %q", got)
	}

	cfg.Exporters.OTLP = &config.OTLPConfig{Endpoint: collectorEndpoint}
	if got := endpointForSnapshot(cfg); got != collectorEndpoint {
		t.Fatalf("expected endpoint collector:4318, got %q", got)
	}
}

func TestRuntimeConfigReturnsCopy(t *testing.T) {
	t.Parallel()

	initial := config.Config{
		Service: config.ServiceConfig{Name: "service-A"},
	}

	rt := &Runtime{cfg: initial}

	cfgCopy := rt.Config()
	cfgCopy.Service.Name = "mutated"

	if rt.cfg.Service.Name != "service-A" {
		t.Fatal("runtime config should not be mutated by caller")
	}
}

func TestRuntimeShutdownSetsState(t *testing.T) {
	t.Parallel()

	rt := &Runtime{}

	ctx := context.Background()

	err := rt.Shutdown(ctx)
	if err != nil {
		t.Fatalf("unexpected error on shutdown: %v", err)
	}

	if !rt.IsShutdown() {
		t.Fatal("expected runtime to be marked as shutdown")
	}

	err = rt.Shutdown(ctx)
	if err != nil {
		t.Fatalf("second shutdown should not error, got %v", err)
	}
}

func TestSnapshotBasic(t *testing.T) {
	t.Parallel()

	start := time.Unix(1700000000, 0).UTC()
	reload := start.Add(5 * time.Minute)

	rt := &Runtime{
		cfg: config.Config{
			Service: config.ServiceConfig{
				Name:        "svc",
				Version:     "1.0.0",
				Environment: "prod",
			},
			Sampling: config.SamplingConfig{
				Mode: "always_on",
			},
		},
		startTime:  start,
		lastReload: reload,
	}

	snap := rt.Snapshot()

	assertSnapshotMetadata(t, snap, start, reload)
	assertInstrumentationDisabled(t, snap)
	assertSnapshotDefaults(t, snap)
}

func TestSnapshotExporterStatus(t *testing.T) {
	t.Parallel()

	traceStats := &traceExporterStats{
		queueLimit: 512,
		protocol:   "grpc",
		endpoint:   "collector:4317",
	}
	traceStats.dropped.Store(droppedSpans)

	metricStats := &metricExporterStats{
		protocol: "http",
		endpoint: collectorEndpoint,
	}

	rt := &Runtime{
		cfg: config.Config{
			Service: config.ServiceConfig{
				Name: "svc",
			},
		},
		exporters: &exporterBundle{
			traceStats:  traceStats,
			metricStats: metricStats,
		},
	}

	snap := rt.Snapshot()

	if snap.TraceQueueLimit != 512 {
		t.Fatalf("expected trace queue limit 512, got %d", snap.TraceQueueLimit)
	}

	if snap.TraceDroppedSpans != droppedSpans {
		t.Fatalf("expected dropped spans %d, got %d", droppedSpans, snap.TraceDroppedSpans)
	}

	if snap.TraceExporter.Endpoint != "collector:4317" {
		t.Fatalf("expected trace exporter endpoint collector:4317, got %s", snap.TraceExporter.Endpoint)
	}

	if snap.MetricExporter.Endpoint != collectorEndpoint {
		t.Fatalf("expected metric exporter endpoint collector:4318, got %s", snap.MetricExporter.Endpoint)
	}

	if snap.MetricExporter.Protocol != "http" {
		t.Fatalf("expected metric protocol http, got %s", snap.MetricExporter.Protocol)
	}
}

func assertSnapshotMetadata(t *testing.T, snap diagnostics.Snapshot, start, reload time.Time) {
	t.Helper()

	if snap.ServiceName != "svc" {
		t.Fatalf("expected service name svc, got %s", snap.ServiceName)
	}

	if snap.ServiceVersion != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %s", snap.ServiceVersion)
	}

	if snap.Environment != "prod" {
		t.Fatalf("expected environment prod, got %s", snap.Environment)
	}

	if snap.SamplingMode != "always_on" {
		t.Fatalf("expected sampling mode always_on, got %s", snap.SamplingMode)
	}

	if !snap.StartTime.Equal(start) {
		t.Fatalf("expected start time %v, got %v", start, snap.StartTime)
	}

	if !snap.LastReloadTime.Equal(reload) {
		t.Fatalf("expected last reload %v, got %v", reload, snap.LastReloadTime)
	}
}

func assertInstrumentationDisabled(t *testing.T, snap diagnostics.Snapshot) {
	t.Helper()

	keys := []string{"http", "grpc", "sql", "messaging", "worker", "runtimeMetrics"}
	for _, key := range keys {
		val, ok := snap.Instrumentation[key]
		if !ok {
			t.Fatalf("expected instrumentation key %s to be present", key)
		}

		if val {
			t.Fatalf("expected instrumentation %s to be disabled", key)
		}
	}
}

func assertSnapshotDefaults(t *testing.T, snap diagnostics.Snapshot) {
	t.Helper()

	if snap.TraceQueueLimit != 0 {
		t.Fatalf("expected trace queue limit 0, got %d", snap.TraceQueueLimit)
	}

	if snap.TraceDroppedSpans != 0 {
		t.Fatalf("expected dropped spans 0, got %d", snap.TraceDroppedSpans)
	}

	if snap.ConfigReloadCount != 0 {
		t.Fatalf("expected reload count 0, got %d", snap.ConfigReloadCount)
	}

	if snap.ExporterEndpoint != "" {
		t.Fatalf("expected empty exporter endpoint, got %s", snap.ExporterEndpoint)
	}

	if snap.MetricExporter.Endpoint != "" {
		t.Fatalf("expected empty metric exporter endpoint, got %s", snap.MetricExporter.Endpoint)
	}
}
