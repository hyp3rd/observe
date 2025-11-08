// Package http provides HTTP instrumentation middleware.
package http

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/hyp3rd/observe/pkg/config"
)

// Middleware instruments HTTP handlers with tracing and RED metrics.
type Middleware struct {
	tracer        trace.Tracer
	requests      metric.Int64Counter
	duration      metric.Float64Histogram
	cfg           config.HTTPInstrumentationConfig
	ignoredRoutes map[string]struct{}
}

// NewMiddleware creates a new middleware using the provided tracer and meter.
func NewMiddleware(tp trace.TracerProvider, mp metric.MeterProvider, cfg config.HTTPInstrumentationConfig) (*Middleware, error) {
	tracer := tp.Tracer("observe/http")
	meter := mp.Meter("observe/http")

	reqCounter, err := meter.Int64Counter(
		"http.server.requests",
		metric.WithDescription("Number of HTTP server requests received"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create request counter")
	}

	latencyHist, err := meter.Float64Histogram(
		"http.server.duration.ms",
		metric.WithDescription("Latency of HTTP server requests"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, ewrap.Wrap(err, "create latency histogram")
	}

	return &Middleware{
		tracer:        tracer,
		requests:      reqCounter,
		duration:      latencyHist,
		cfg:           cfg,
		ignoredRoutes: toSet(cfg.IgnoredRoutes),
	}, nil
}

// Handler wraps the supplied handler with tracing and metrics.
func (m *Middleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := routeFromRequest(r)
		if m.shouldIgnore(route) {
			next.ServeHTTP(w, r)

			return
		}

		attrs := []attribute.KeyValue{
			semconv.HTTPMethodKey.String(r.Method),
			semconv.HTTPRouteKey.String(route),
		}

		ctx, span := m.tracer.Start(
			r.Context(),
			spanName(r.Method, route),
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		start := time.Now()
		rr := &responseRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rr, r.WithContext(ctx))

		duration := time.Since(start)
		statusAttr := semconv.HTTPStatusCodeKey.Int(rr.status)

		attrs = append(attrs, statusAttr)
		if host := clientIP(r); host != "" {
			attrs = append(attrs, semconv.ClientAddressKey.String(host))
		}

		if rr.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(rr.status))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		span.SetAttributes(attrs...)

		m.requests.Add(ctx, 1, metric.WithAttributes(attrs...))
		m.duration.Record(ctx, float64(duration.Milliseconds()), metric.WithAttributes(attrs...))
	})
}

func (m *Middleware) shouldIgnore(route string) bool {
	_, ok := m.ignoredRoutes[route]

	return ok
}

func toSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return map[string]struct{}{}
	}

	set := make(map[string]struct{}, len(values))
	for _, val := range values {
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}

		set[val] = struct{}{}
	}

	return set
}

func spanName(method, route string) string {
	if route == "" {
		route = "/"
	}

	return method + " " + route
}

func routeFromRequest(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}

	path := r.URL.Path
	if path == "" {
		return "/"
	}

	return path
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}

	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return host
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

// WriteHeader records the status code and delegates to the underlying ResponseWriter.
func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Write delegates to the underlying ResponseWriter.
func (r *responseRecorder) Write(b []byte) (int, error) {
	bytes, err := r.ResponseWriter.Write(b)
	if err != nil {
		return bytes, ewrap.Wrap(err, "write response")
	}

	return bytes, nil
}
