package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
)

const attributeCountWithTrace = 3

func TestWithTraceAddsSpanContext(t *testing.T) {
	t.Parallel()

	ctx, span := trace.NewTracerProvider().Tracer("test").Start(context.Background(), "span")
	defer span.End()

	attrs := withTrace(ctx, []attribute.KeyValue{attribute.String("foo", "bar")})
	if len(attrs) < attributeCountWithTrace {
		t.Fatalf("expected trace attributes plus payload, got %d", len(attrs))
	}

	if attrs[0].Key != "trace_id" {
		t.Fatalf("expected trace_id first, got %s", attrs[0].Key)
	}

	if attrs[1].Key != "span_id" {
		t.Fatalf("expected span_id second, got %s", attrs[1].Key)
	}
}

func TestWithTraceNoSpan(t *testing.T) {
	t.Parallel()

	attrs := withTrace(context.Background(), []attribute.KeyValue{attribute.String("foo", "bar")})
	if len(attrs) != 1 {
		t.Fatalf("expected only original attrs, got %d", len(attrs))
	}
}

func TestSlogAdapterWritesTraceAttributes(t *testing.T) {
	t.Parallel()

	ctx, span := trace.NewTracerProvider().Tracer("test").Start(context.Background(), "span")
	defer span.End()

	var buf bytes.Buffer

	adapter := NewSlogAdapter(slogLogger(&buf))

	adapter.Info(ctx, "hello", attribute.String("foo", "bar"))

	var entry map[string]any

	err := json.Unmarshal(buf.Bytes(), &entry)
	if err != nil {
		t.Fatalf("unmarshal slog output: %v", err)
	}

	if entry["trace_id"] == nil {
		t.Fatalf("expected trace_id attribute, got %v", entry)
	}
}

func slogLogger(buf *bytes.Buffer) *slog.Logger {
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	return slog.New(handler)
}
