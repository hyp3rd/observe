// Package logging provides logging adapters for various logging libraries.
package logging

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Adapter describes the logging contract used within the observe library.
type Adapter interface {
	Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue)
	Info(ctx context.Context, msg string, attrs ...attribute.KeyValue)
	Error(ctx context.Context, err error, msg string, attrs ...attribute.KeyValue)
}

// NoopAdapter discards all logs.
type NoopAdapter struct{}

// NewNoopAdapter returns a logger that drops every log event.
func NewNoopAdapter() Adapter {
	return NoopAdapter{}
}

// Info implements Adapter.
func (NoopAdapter) Info(context.Context, string, ...attribute.KeyValue) {}

// Error implements Adapter.
func (NoopAdapter) Error(context.Context, error, string, ...attribute.KeyValue) {}

// Debug implements Adapter.
func (NoopAdapter) Debug(context.Context, string, ...attribute.KeyValue) {}

// SlogAdapter writes logs using log/slog.
type SlogAdapter struct {
	logger *slog.Logger
}

// NewSlogAdapter creates a slog-based adapter. If logger is nil a default JSON logger is used.
func NewSlogAdapter(logger *slog.Logger) Adapter {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	return &SlogAdapter{logger: logger}
}

// Info implements Adapter.
func (s *SlogAdapter) Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	s.logger.LogAttrs(ctx, slog.LevelInfo, msg, toSlogAttrs(withTrace(ctx, attrs))...)
}

// Debug implements Adapter.
func (s *SlogAdapter) Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	s.logger.LogAttrs(ctx, slog.LevelDebug, msg, toSlogAttrs(withTrace(ctx, attrs))...)
}

// Error implements Adapter.
func (s *SlogAdapter) Error(ctx context.Context, err error, msg string, attrs ...attribute.KeyValue) {
	if err != nil {
		attrs = append(attrs, attribute.String("error", err.Error()))
	}

	s.logger.LogAttrs(ctx, slog.LevelError, msg, toSlogAttrs(withTrace(ctx, attrs))...)
}

// ZapAdapter writes logs via zap.Logger.
type ZapAdapter struct {
	logger *zap.Logger
}

// NewZapAdapter creates a zap adapter. The logger must not be nil.
func NewZapAdapter(logger *zap.Logger) Adapter {
	return &ZapAdapter{logger: logger}
}

// Info implements Adapter.
func (z *ZapAdapter) Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	z.logger.Info(msg, toZapFields(withTrace(ctx, attrs))...)
}

// Debug implements Adapter.
func (z *ZapAdapter) Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	z.logger.Debug(msg, toZapFields(withTrace(ctx, attrs))...)
}

// Error implements Adapter.
func (z *ZapAdapter) Error(ctx context.Context, err error, msg string, attrs ...attribute.KeyValue) {
	fields := toZapFields(withTrace(ctx, attrs))
	if err != nil {
		fields = append(fields, zap.Error(err))
	}

	z.logger.Error(msg, fields...)
}

// ZerologAdapter writes logs via zerolog.
type ZerologAdapter struct {
	logger zerolog.Logger
}

// NewZerologAdapter creates an adapter using zerolog.
func NewZerologAdapter(logger zerolog.Logger) Adapter {
	return &ZerologAdapter{logger: logger}
}

// Info implements Adapter.
func (z ZerologAdapter) Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	event := z.logger.Info()
	for _, attr := range withTrace(ctx, attrs) {
		event = event.Interface(string(attr.Key), attrValue(attr))
	}

	event.Msg(msg)
}

// Debug implements Adapter.
func (z ZerologAdapter) Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	event := z.logger.Debug()
	for _, attr := range withTrace(ctx, attrs) {
		event = event.Interface(string(attr.Key), attrValue(attr))
	}

	event.Msg(msg)
}

// Error implements Adapter.
func (z ZerologAdapter) Error(ctx context.Context, err error, msg string, attrs ...attribute.KeyValue) {
	event := z.logger.Error()
	for _, attr := range withTrace(ctx, attrs) {
		event = event.Interface(string(attr.Key), attrValue(attr))
	}

	if err != nil {
		event = event.Err(err)
	}

	event.Msg(msg)
}

// StdAdapter uses the standard library logger.
type StdAdapter struct {
	logger *log.Logger
}

// NewStdAdapter creates an adapter around log.Logger. If logger is nil log.Default is used.
func NewStdAdapter(logger *log.Logger) Adapter {
	if logger == nil {
		logger = log.Default()
	}

	return &StdAdapter{logger: logger}
}

// Info implements Adapter.
func (s *StdAdapter) Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	s.logger.Println(formatLine("INFO", msg, withTrace(ctx, attrs)))
}

// Debug implements Adapter.
func (s *StdAdapter) Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	s.logger.Println(formatLine("DEBUG", msg, withTrace(ctx, attrs)))
}

// Error implements Adapter.
func (s *StdAdapter) Error(ctx context.Context, err error, msg string, attrs ...attribute.KeyValue) {
	if err != nil {
		attrs = append(attrs, attribute.String("error", err.Error()))
	}

	s.logger.Println(formatLine("ERROR", msg, withTrace(ctx, attrs)))
}

func withTrace(ctx context.Context, attrs []attribute.KeyValue) []attribute.KeyValue {
	spanCtx := trace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() {
		return attrs
	}

	traceAttrs := []attribute.KeyValue{
		attribute.String("trace_id", spanCtx.TraceID().String()),
		attribute.String("span_id", spanCtx.SpanID().String()),
	}

	return append(traceAttrs, attrs...)
}

func attrValue(attr attribute.KeyValue) any {
	//nolint:exhaustive // attribute.INVALID falls through to AsInterface.
	switch attr.Value.Type() {
	case attribute.BOOL:
		return attr.Value.AsBool()
	case attribute.INT64:
		return attr.Value.AsInt64()
	case attribute.FLOAT64:
		return attr.Value.AsFloat64()
	case attribute.STRING:
		return attr.Value.AsString()
	case attribute.BOOLSLICE:
		return attr.Value.AsBoolSlice()
	case attribute.INT64SLICE:
		return attr.Value.AsInt64Slice()
	case attribute.FLOAT64SLICE:
		return attr.Value.AsFloat64Slice()
	case attribute.STRINGSLICE:
		return attr.Value.AsStringSlice()
	default:
		return attr.Value.AsInterface()
	}
}

func toSlogAttrs(attrs []attribute.KeyValue) []slog.Attr {
	out := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, slog.Any(string(attr.Key), attrValue(attr)))
	}

	return out
}

func toZapFields(attrs []attribute.KeyValue) []zap.Field {
	out := make([]zap.Field, 0, len(attrs))
	for _, attr := range attrs {
		out = append(out, zap.Any(string(attr.Key), attrValue(attr)))
	}

	return out
}

func formatLine(level, msg string, attrs []attribute.KeyValue) string {
	builder := strings.Builder{}
	builder.WriteString(level)
	builder.WriteString(" ")
	builder.WriteString(msg)

	for _, attr := range attrs {
		builder.WriteString(" ")
		builder.WriteString(string(attr.Key))
		builder.WriteString("=")
		builder.WriteString(fmt.Sprint(attrValue(attr)))
	}

	return builder.String()
}
