# Instrumentation Packs Overview

Observe ships a set of instrumentation helpers under `pkg/instrumentation`. Each helper is opt-in via `instrumentation.*` configuration fields and surfaced through `pkg/runtime`.

## HTTP

- Package: `pkg/instrumentation/http`
- Enable with `instrumentation.http.enabled`
- Features:
      - `Middleware.Handler` wraps `net/http` handlers.
      - Records RED metrics + spans using semconv HTTP attributes.
      - Supports ignore lists via `instrumentation.http.ignored_routes`.

## gRPC

- Package: `pkg/instrumentation/grpc`
- Enable with `instrumentation.grpc.enabled`
- Features:
      - Unary client/server interceptors exposing `Runtime.GRPCUnary{Client,Server}Interceptor()`.
      - Adds metadata allowlist for attributes via `instrumentation.grpc.metadata_allowlist`.

## SQL

- Package: `pkg/instrumentation/sql`
- Enable with `instrumentation.sql.enabled`
- Features:
      - `Helper.Register/Open/RegisterDBStats` wrap drivers and register pool metrics.
      - Honors `instrumentation.sql.collect_queries` to redact statements.

## Messaging

- Package: `pkg/instrumentation/messaging`
- Enable with `instrumentation.messaging.enabled`
- Features:
      - `Helper` exposes `InstrumentPublish` and `InstrumentConsume`.
      - Kafka adapters live in `pkg/instrumentation/messaging/kafka`.

## Worker

- Package: `pkg/instrumentation/worker`
- Enable with `instrumentation.worker.enabled`
- Features:
      - `Helper.Instrument` wraps background jobs.
      - Emits spans + `worker.job.count`/`worker.job.duration_ms` metrics with success/error tagging.
      - Concrete adapter `pkg/instrumentation/worker/ticker` runs cron/ticker style jobs with graceful stop + error hooks.

## Diagnostics & Runtime Metrics

- Diagnostics snapshots (`/observe/status`) include:
      - Service metadata, instrumentation toggles, config reload count.
      - Trace exporter protocol/endpoint + last error, queue limit, dropped spans.
      - Metric exporter protocol/endpoint + last error (mirrors trace fields for parity).
- Runtime metrics (enable via `instrumentation.runtime_metrics.enabled`):
      - Go runtime metrics via `go.opentelemetry.io/contrib/instrumentation/runtime`.
      - Observe-specific gauges for instrumentation enablement and exporter queue size.
