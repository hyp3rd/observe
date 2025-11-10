package logging

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"log/slog"
	"math"
	"os"
	"strings"

	"github.com/hyp3rd/ewrap"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/hyp3rd/observe/pkg/config"
)

// FromConfig builds an Adapter from logging configuration.
func FromConfig(cfg config.LoggingConfig) Adapter {
	base := buildBaseAdapter(cfg)
	base = applyLevelFilter(base, cfg.Level)
	base = applySampling(base, cfg.SampleRatio)

	return base
}

func buildBaseAdapter(cfg config.LoggingConfig) Adapter {
	switch strings.ToLower(cfg.Adapter) {
	case "std":
		return NewStdAdapter(nil)
	case "zap":
		logger, err := newZapLogger(cfg)
		if err == nil {
			return NewZapAdapter(logger)
		}
	case "zerolog":
		return NewZerologAdapter(zerolog.New(os.Stdout).With().Timestamp().Logger())
	default:
		return newSlogFromConfig(cfg)
	}

	return newSlogFromConfig(cfg)
}

func newSlogFromConfig(cfg config.LoggingConfig) Adapter {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: slogLevel(cfg.Level),
	}
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return NewSlogAdapter(slog.New(handler))
}

func newZapLogger(cfg config.LoggingConfig) (*zap.Logger, error) {
	level := zap.NewAtomicLevelAt(zapLevel(cfg.Level))
	configZap := zap.NewProductionConfig()
	configZap.Level = level

	switch strings.ToLower(cfg.Format) {
	case "text":
		configZap.Encoding = "console"
	default:
		configZap.Encoding = "json"
	}

	zapLogger, err := configZap.Build()
	if err != nil {
		return nil, ewrap.Wrap(err, "build zap logger")
	}

	return zapLogger, nil
}

func applyLevelFilter(adapter Adapter, level string) Adapter {
	if adapter == nil {
		return NewNoopAdapter()
	}

	switch strings.ToLower(level) {
	case "error":
		return infoDisabledAdapter{inner: adapter}
	default:
		return adapter
	}
}

type infoDisabledAdapter struct {
	inner Adapter
}

func (infoDisabledAdapter) Info(_ context.Context, _ string, _ ...attribute.KeyValue) {
	// drop info level
}

func (infoDisabledAdapter) Debug(_ context.Context, _ string, _ ...attribute.KeyValue) {
	// drop debug level
}

func (a infoDisabledAdapter) Error(ctx context.Context, err error, msg string, attrs ...attribute.KeyValue) {
	a.inner.Error(ctx, err, msg, attrs...)
}

func applySampling(adapter Adapter, ratio float64) Adapter {
	if adapter == nil {
		return NewNoopAdapter()
	}

	if ratio <= 0 {
		return &samplingAdapter{inner: adapter, ratio: 0}
	}

	if ratio >= 1 {
		return adapter
	}

	return &samplingAdapter{
		inner: adapter,
		ratio: ratio,
	}
}

type samplingAdapter struct {
	inner Adapter
	ratio float64
}

func (s *samplingAdapter) Info(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if s.shouldLog() {
		s.inner.Info(ctx, msg, attrs...)
	}
}

func (s *samplingAdapter) Debug(ctx context.Context, msg string, attrs ...attribute.KeyValue) {
	if s.shouldLog() {
		s.inner.Debug(ctx, msg, attrs...)
	}
}

func (s *samplingAdapter) Error(ctx context.Context, err error, msg string, attrs ...attribute.KeyValue) {
	s.inner.Error(ctx, err, msg, attrs...)
}

func (s *samplingAdapter) shouldLog() bool {
	if s.ratio <= 0 {
		return false
	}

	return randomFloat64() <= s.ratio
}

func randomFloat64() float64 {
	var randomBytes [8]byte

	_, err := rand.Read(randomBytes[:])
	if err != nil {
		return 1
	}

	n := binary.BigEndian.Uint64(randomBytes[:])

	return float64(n) / float64(math.MaxUint64)
}

func slogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "error":
		return slog.LevelError
	case "warn", "warning":
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func zapLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "error":
		return zapcore.ErrorLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	default:
		return zapcore.InfoLevel
	}
}
