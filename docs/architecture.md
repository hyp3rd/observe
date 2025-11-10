# Architecture: Observe OTEL Instrumentation Library

## 1. Overview

The Observe library encapsulates OpenTelemetry setup for Hyp3rd Go services. A single bootstrap call wires metrics, traces, and logs using layered configuration and modular instrumentation packs. The design optimizes for:

- **Performance**: <5% CPU overhead, minimal allocations, lock-free fast paths.
- **Scalability**: Batching exporters, deterministic sampling, tenant-aware attributes.
- **Programmability**: Fluent builders plus opt-in modules; public interfaces for custom hooks.
- **Flexibility**: Configurable via env, YAML, or remote sources with hot reload.

## 2. Core Principles

1. **Convention over configuration** for baseline telemetry; overrides only when necessary.
1. **Separation of concerns** between core runtime, configuration, and instrumentation packs.
1. **Pluggability everywhere**: exporters, samplers, resource detectors, instrumentation hooks.
1. **Safe defaults** that fail closed—invalid config never leaves the app half-instrumented.
1. **Self-observing**: the library emits its own health metrics/logs.

## 3. Package Layout

| Package | Responsibility |
| --- | --- |
| `pkg/observe` | Public entry points (`Init`, `Shutdown`, builder APIs) plus top-level configuration structs. |
| `pkg/config` | Config schema, loaders (env, YAML, remote), validation, diffing, hot-reload watcher. |
| `pkg/runtime` | Core runtime managing OTEL SDKs, provider factories, resource detection, sampling, exporter lifecycle. |
| `pkg/exporters` | Built-in OTLP HTTP/gRPC exporters + interfaces and helpers for third-party adapters. |
| `pkg/instrumentation/http` | Middleware for `net/http`, `chi`, `gin`, `echo` (client + server). |
| `pkg/instrumentation/grpc` | Unary/stream interceptors, payload metrics, metadata enrichment. |
| `pkg/instrumentation/sql` | `database/sql` driver wrappers, query span helpers. |
| `pkg/instrumentation/mq` | NATS/Kafka/PubSub wrappers, consumer/producer spans and metrics. |
| `pkg/logging` | Structured log helpers, adapters for `slog`, `zap`, `zerolog`. |
| `pkg/diagnostics` | Self-telemetry metrics, `/observe/status` HTTP handler, last-error recorder. |
| `internal/testkit` | Shared test harness utilities, fake exporters, benchmark fixtures. |

## 4. Initialization & Data Flow

```text
config.Load() -> runtime.Builder -> resource.Detector -> sampler.Manager
 -> exporter.Registry -> sdk.ProviderSet -> instrumentation.Registry -> app hooks
```

1. `observe.Init(ctx, options...)` accepts optional overrides; otherwise it loads config via `pkg/config`.
1. The builder composes resource attributes (env detectors + custom attributes), constructs samplers and exporters, then wires OTEL meter, tracer, and logger providers.
1. Instrumentation packs register via a registry (map of module name → factory). Config toggles enable/disable modules before they attach middleware/interceptors.
1. Runtime exposes handles (`observe.Tracer()`, `observe.Meter()`, `observe.Logger()`) and cleanup via `observe.Shutdown(ctx)`.

## 5. Configuration Layering

### Sources

1. **Environment variables** (`OBSERVE_*`) – quick overrides, highest precedence.
1. **YAML file** (`observe.yaml`) – checked into repos; supports include/anchors.
1. **Remote provider** (optional) – HTTP/etcd/consul; polled or pushed for runtime updates.

### Merge Strategy

```text
Defaults <- YAML <- Remote <- Env <- Code overrides
```

- Validation occurs after each merge; invalid segments reject the change.
- Hot reload uses fsnotify/remote watcher → diff → apply via runtime mutation (sampler/exporter replacements done atomically with double buffering).

### Key Config Sections

- `exporters`: list with type, endpoint, credentials, batching, retry, TLS.
- `sampling`: mode, rate, tenant policy, tail-based settings.
- `instrumentation`: enable flags + module-specific options (e.g., HTTP route filters).
- `logging`: adapter selection, level, format, correlation toggle.
- `diagnostics`: enable flag, endpoint bind address, auth options.

## 6. Runtime Components

| Component | Description |
| --- | --- |
| `Runtime` | Holds active providers, config cache, instrumentation registry, diagnostics server. |
| `ProviderSet` | Wrapper around OTEL tracer/meter/logger providers plus resource. |
| `SamplerManager` | Builds samplers per config; supports tenant-aware hashing and tail-based hook to collector. |
| `ExporterRegistry` | Keeps exporter instances keyed by signal; handles backpressure, retries, shutdown. |
| `ResourceManager` | Detects environment (k8s, ECS, Lambda) and merges custom attributes, caches results. |
| `InstrumentationRegistry` | Discovers modules, ensures dependency ordering, exposes `Enable(name)`/`Disable(name)`. |
| `DiagnosticsServer` | Serves `/observe/status`, metrics on queue size/dropped spans, recent errors. |

## 7. Instrumentation Packs

### HTTP

- Middleware for net/http, Gin, Chi, Echo.
- Features: route templating, client IP masking, body size metrics, optional request/response body sampling.
- Exposes helpers (`observehttp.WrapHandler`, `observehttp.Client`) for direct usage.

### gRPC

- Unary/stream interceptors with configurable payload logging, metadata filters, and per-method stats.
- Integrates with `grpc-go` stats handlers for connection-level metrics.

### SQL

- Wraps `*sql.DB` with hooks that tag spans with db.statement (redacted), db.system, peer host.
- Emits connection pool metrics (idle/open, wait duration).

### Message Queues

- Producer/consumer wrappers for NATS, Kafka, Pub/Sub; consistent attributes (messaging.system, operation).
- Kafka adapters wrap `segmentio/kafka-go` writers/readers, automatically invoking the messaging helper for publish/consume operations.
- Optional batching spans for bulk publish/ack.

### Background/Worker

- Helper instrumentation wraps cron/job runners, emitting spans and counters with success/error tagging.
- Worker helpers are exposed through `Runtime.WorkerHelper()` when `instrumentation.worker.enabled` is set.
- Concrete adapters live alongside the helper (e.g., `pkg/instrumentation/worker/ticker` for cron/ticker integrations and `pkg/instrumentation/worker/kafka` for stream processing) to demonstrate production-ready usage.

Instrumentation packs share a base module that fetches tracer/meter handles lazily and registers health metrics.

## 8. Exporter & Sampler Strategy

- Default exporter: OTLP/gRPC with retry/backoff, TLS, compression.
- Additional exporters (OTLP/HTTP, Jaeger, Zipkin, Prometheus) registered via factory functions.
- Exporters implement a `Component` interface (`Start(context.Context) error`, `Shutdown(context.Context) error`).
- Samplers: always-on/off, parent-based, hash-based per tenant, plus hook for remote tail-based sampling (delegates to collector). Tail sampling stub ensures config compatibility even if collector offlines.

## 9. Logging Integration

- `pkg/logging` exposes `observe.Logger()` returning an adapter that enriches entries with `trace_id`, `span_id`, `tenant_id`.
- Built-in adapters target `slog`, `zap`, `zerolog`, and stdlib loggers and expose a consistent `Debug/Info/Error` surface.
- Config-driven sampling + level filters keep noisy services lightweight while still surfacing errors.

## 10. Diagnostics & Self Telemetry

- `/observe/status` returns exporter health (protocol, endpoint, last success/error timestamps, cumulative error counts) for both trace and metric exporters, sampler mode, queue limit, dropped spans, instrumentation toggles, and config reload count. Optional auth via token/header (`diagnostics.auth_token`).
- Config hot reload is debounced and deduplicated using config fingerprints to avoid thrashing exporters on repeated writes.
- `runtime_metrics` instrument records queue size, dropped spans, config reload counts, instrumentation enablement status.
- Panic/failure hooks emit structured events and escalate via logging adapters.

## 11. Extensibility Hooks

- **Attribute mutators**: `type AttributeMutator interface { Mutate(ctx context.Context, attrs []attribute.KeyValue) }`.
- **Span processors**: integrators can register additional OTEL span processors via config or code.
- **Metric views**: config-driven views to control histogram boundaries, temporality, aggregation.
- **Module SPI**: instrumentation packs implement `Module interface { Name() string; Enable(ctx context.Context, r *Runtime) error; Disable(ctx context.Context, r *Runtime) error }`.

## 12. Testing & Bench Strategy

- `make test` runs unit tests (config, runtime, instrumentation) with fake exporters.
- `make test-integration` spins up local OTLP collector container (disabled in CI unless integration tag set).
- `make bench-observe` benchmarks HTTP/gRPC middleware, sampler hot path, exporter queues.
- Golden files for config parsing; fuzz tests for config loader and HTTP header parsing.

## 13. Deliverables & Next Steps

1. Implement `pkg/config` schema + loader with env+yaml support and validation.
1. Build `pkg/runtime` with OTEL provider wiring, exporter registry, sampler manager.
1. Ship HTTP instrumentation pack + example service to validate bootstrap/perf targets.
1. Add diagnostics module and basic logging adapter.
1. Expand to remaining instrumentation packs per roadmap.
