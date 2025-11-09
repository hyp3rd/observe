// Package worker provides OpenTelemetry instrumentation helpers for background workers.
package worker

import (
	"context"
	"time"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

// JobInfo contains metadata describing a worker job execution.
type JobInfo struct {
	Name       string
	Queue      string
	Schedule   string
	Attributes []attribute.KeyValue
}

// Helper provides instrumentation helpers for background workers.
type Helper struct {
	tracer     trace.Tracer
	jobCounter metric.Int64Counter
	jobLatency metric.Float64Histogram
}

// NewHelper constructs a worker Helper.
func NewHelper(tp trace.TracerProvider, mp metric.MeterProvider) (*Helper, error) {
	if tp == nil {
		return nil, ewrap.New("tracer provider is nil")
	}

	if mp == nil {
		mp = noop.NewMeterProvider()
	}

	tracer := tp.Tracer("observe/worker")
	meter := mp.Meter("observe/worker")

	counter, err := meter.Int64Counter(
		"worker.job.count",
		metric.WithDescription("Number of jobs executed by worker helpers"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create worker job counter")
	}

	latency, err := meter.Float64Histogram(
		"worker.job.duration_ms",
		metric.WithDescription("Latency of worker job executions"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create worker job latency histogram")
	}

	return &Helper{
		tracer:     tracer,
		jobCounter: counter,
		jobLatency: latency,
	}, nil
}

// Instrument executes fn while recording tracing and metrics for the job.
func (h *Helper) Instrument(ctx context.Context, info JobInfo, fn func(context.Context) error) error {
	if h == nil {
		return fn(ctx)
	}

	if info.Name == "" {
		info.Name = "worker-job"
	}

	ctx, span := h.tracer.Start(ctx, spanName(info))
	start := time.Now()

	attrs := jobAttributes(info)
	span.SetAttributes(attrs...)

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.End()

	duration := float64(time.Since(start)) / float64(time.Millisecond)
	h.jobLatency.Record(ctx, duration, metric.WithAttributes(attrs...))

	statusAttr := attribute.String("worker.result", resultTag(err))

	countAttrs := append([]attribute.KeyValue{}, attrs...)
	countAttrs = append(countAttrs, statusAttr)
	h.jobCounter.Add(ctx, 1, metric.WithAttributes(countAttrs...))

	return err
}

func spanName(info JobInfo) string {
	if info.Queue != "" {
		return info.Queue + ":" + info.Name
	}

	return info.Name
}

func jobAttributes(info JobInfo) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("worker.name", info.Name),
	}
	if info.Queue != "" {
		attrs = append(attrs, attribute.String("worker.queue", info.Queue))
	}

	if info.Schedule != "" {
		attrs = append(attrs, attribute.String("worker.schedule", info.Schedule))
	}

	return append(attrs, info.Attributes...)
}

func resultTag(err error) string {
	if err != nil {
		return "error"
	}

	return "success"
}
