// Package sql provides helpers for SQL instrumentation.
package sql

import (
	"database/sql"

	"github.com/XSAM/otelsql"
	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"github.com/hyp3rd/observe/pkg/config"
)

// ErrDriverNameCannotBeEmpty is returned when a required driver name is not provided.
var ErrDriverNameCannotBeEmpty = ewrap.New("driverName cannot be empty")

// Helper exposes convenience helpers around github.com/XSAM/otelsql so callers
// can instrument database/sql connections with consistent defaults.
type Helper struct {
	cfg config.SQLInstrumentationConfig
}

// NewHelper constructs a Helper using the provided configuration.
func NewHelper(cfg config.SQLInstrumentationConfig) *Helper {
	return &Helper{cfg: cfg}
}

// Register wraps the driver referenced by driverName and returns a new
// registered driver name that emits telemetry when used with sql.Open.
func (h *Helper) Register(driverName string, opts ...otelsql.Option) (string, error) {
	if driverName == "" {
		return "", ErrDriverNameCannotBeEmpty
	}

	driver, err := otelsql.Register(driverName, h.options(driverName, opts...)...)
	if err != nil {
		return "", ewrap.Wrap(err, "otelsql.Register failed")
	}

	return driver, nil
}

// Open behaves like sql.Open but automatically wires OTEL spans/metrics.
func (h *Helper) Open(driverName, dataSourceName string, opts ...otelsql.Option) (*sql.DB, error) {
	if driverName == "" {
		return nil, ErrDriverNameCannotBeEmpty
	}

	db, err := otelsql.Open(driverName, dataSourceName, h.options(driverName, opts...)...)
	if err != nil {
		return nil, ewrap.Wrap(err, "otelsql.Open failed")
	}

	return db, nil
}

// RegisterDBStats subscribes sql.DBStats metrics with the configured meter provider.
func (h *Helper) RegisterDBStats(db *sql.DB, opts ...otelsql.Option) error {
	if db == nil {
		return ewrap.New("db cannot be nil")
	}

	err := otelsql.RegisterDBStatsMetrics(db, h.options("", opts...)...)
	if err != nil {
		return ewrap.Wrap(err, "otelsql.RegisterDBStatsMetrics failed")
	}

	return nil
}

func (h *Helper) options(driverName string, userOpts ...otelsql.Option) []otelsql.Option {
	spanOpts := otelsql.SpanOptions{
		DisableQuery: !h.cfg.CollectQueries,
	}

	attrs := []attribute.KeyValue{}
	if driverName != "" {
		attrs = append(attrs, semconv.DBSystemKey.String(driverName))
	}

	final := []otelsql.Option{
		otelsql.WithSpanOptions(spanOpts),
	}
	if len(attrs) > 0 {
		final = append(final, otelsql.WithAttributes(attrs...))
	}

	final = append(final, userOpts...)

	return final
}
