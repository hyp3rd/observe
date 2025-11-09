// Package messaging provides helpers for messaging instrumentation.
package messaging

import (
	"context"
	"time"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// AttrDestinationKind is the attribute key for messaging destination kind.
	AttrDestinationKind = attribute.Key("messaging.destination.kind")
	// AttrConsumerGroup is the attribute key for messaging consumer group.
	AttrConsumerGroup = attribute.Key("messaging.consumer.group")
)

const (
	defaultPublishOperation = "publish"
	defaultConsumeOperation = "process"
)

// PublishInfo captures metadata for producer spans/metrics.
type PublishInfo struct {
	System          string
	Destination     string
	DestinationKind string
	Key             string
	Attributes      []attribute.KeyValue
	SizeBytes       int64
	Operation       string
}

// ConsumeInfo captures metadata for consumer spans/metrics.
type ConsumeInfo struct {
	System          string
	Destination     string
	DestinationKind string
	Group           string
	Attributes      []attribute.KeyValue
	Operation       string
}

// Helper provides helpers for messaging instrumentation.
type Helper struct {
	tracer         trace.Tracer
	publishLatency metric.Float64Histogram
	publishCount   metric.Int64Counter
	consumeLatency metric.Float64Histogram
	consumeCount   metric.Int64Counter
}

// NewHelper initializes messaging instrumentation helpers.
func NewHelper(tp trace.TracerProvider, mp metric.MeterProvider) (*Helper, error) {
	if tp == nil {
		return nil, ewrap.New("tracer provider is nil")
	}

	if mp == nil {
		mp = noop.NewMeterProvider()
	}

	tr := tp.Tracer("observe/messaging")
	meter := mp.Meter("observe/messaging")

	pubLatency, err := meter.Float64Histogram(
		"messaging.publish.latency_ms",
		metric.WithDescription("Latency of publishing messages"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create publish latency histogram")
	}

	pubCount, err := meter.Int64Counter(
		"messaging.publish.count",
		metric.WithDescription("Number of published messages"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create publish counter")
	}

	conLatency, err := meter.Float64Histogram(
		"messaging.consume.latency_ms",
		metric.WithDescription("Latency of processing consumed messages"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create consume latency histogram")
	}

	conCount, err := meter.Int64Counter(
		"messaging.consume.count",
		metric.WithDescription("Number of consumed messages"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create consume counter")
	}

	return &Helper{
		tracer:         tr,
		publishLatency: pubLatency,
		publishCount:   pubCount,
		consumeLatency: conLatency,
		consumeCount:   conCount,
	}, nil
}

// InstrumentPublish wraps a publish function with tracing and metrics.
func (h *Helper) InstrumentPublish(ctx context.Context, info PublishInfo, fn func(context.Context) error) error {
	if h == nil {
		return fn(ctx)
	}

	return h.instrument(
		ctx,
		trace.SpanKindProducer,
		info.Operation,
		info.Destination,
		publishAttributes(info),
		fn,
		h.publishLatency,
		h.publishCount,
	)
}

// InstrumentConsume wraps a consumer handler with tracing and metrics.
func (h *Helper) InstrumentConsume(ctx context.Context, info ConsumeInfo, fn func(context.Context) error) error {
	if h == nil {
		return fn(ctx)
	}

	return h.instrument(
		ctx,
		trace.SpanKindConsumer,
		info.Operation,
		info.Destination,
		consumeAttributes(info),
		fn,
		h.consumeLatency,
		h.consumeCount,
	)
}

func publishAttributes(info PublishInfo) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKey.String(info.System),
	}
	if info.Destination != "" {
		attrs = append(attrs, semconv.MessagingDestinationNameKey.String(info.Destination))
	}

	if info.DestinationKind != "" {
		attrs = append(attrs, AttrDestinationKind.String(info.DestinationKind))
	}

	if info.Operation != "" {
		attrs = append(attrs, semconv.MessagingOperationNameKey.String(info.Operation))
	} else {
		attrs = append(attrs, semconv.MessagingOperationNameKey.String(defaultPublishOperation))
	}

	if info.Key != "" {
		attrs = append(attrs, semconv.MessagingClientIDKey.String(info.Key))
	}

	if info.SizeBytes > 0 {
		attrs = append(attrs, semconv.MessagingMessageBodySizeKey.Int(int(info.SizeBytes)))
	}

	attrs = append(attrs, info.Attributes...)

	return attrs
}

func (h *Helper) instrument(
	ctx context.Context,
	kind trace.SpanKind,
	operation string,
	destination string,
	attrs []attribute.KeyValue,
	fn func(context.Context) error,
	hist metric.Float64Histogram,
	counter metric.Int64Counter,
) error {
	if h == nil {
		return fn(ctx)
	}

	ctx, span := h.tracer.Start(ctx, spanName(operation, destination), trace.WithSpanKind(kind))
	start := time.Now()

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
	hist.Record(ctx, duration, metric.WithAttributes(attrs...))
	counter.Add(ctx, 1, metric.WithAttributes(attrs...))

	return err
}

func consumeAttributes(info ConsumeInfo) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKey.String(info.System),
	}
	if info.Destination != "" {
		attrs = append(attrs, semconv.MessagingDestinationNameKey.String(info.Destination))
	}

	if info.DestinationKind != "" {
		attrs = append(attrs, AttrDestinationKind.String(info.DestinationKind))
	}

	if info.Operation != "" {
		attrs = append(attrs, semconv.MessagingOperationNameKey.String(info.Operation))
	} else {
		attrs = append(attrs, semconv.MessagingOperationNameKey.String(defaultConsumeOperation))
	}

	if info.Group != "" {
		attrs = append(attrs, AttrConsumerGroup.String(info.Group))
	}

	attrs = append(attrs, info.Attributes...)

	return attrs
}

func spanName(operation, destination string) string {
	switch {
	case operation != "" && destination != "":
		return operation + " " + destination
	case destination != "":
		return destination
	default:
		return "messaging"
	}
}
