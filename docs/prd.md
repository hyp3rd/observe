# PRD: Observe OTEL Instrumentation Library

## 1. Background & Problem Statement

- Go services at Hyp3rd currently assemble observability piecemeal, which leads to inconsistent telemetry, drift from OpenTelemetry (OTEL) semantics, and elevated cost to onboard new workloads.
- Platform engineering wants a single library that encapsulates best practices for metrics, traces, and logs so product teams can opt-in with minimal effort while still retaining flexibility for advanced scenarios.
- The solution must assume high-throughput, multi-tenant services that need low overhead instrumentation and predictable configuration in containerized and serverless environments.

## 2. Objectives

### Primary Goals

1. Deliver a Go module that exposes an opinionated yet extensible OTEL setup (metrics, traces, logs) with <5% additional CPU and <10% additional p99 latency overhead when sampling is enabled.
1. Provide programmable building blocks (wrappers, interceptors, middleware) that cover the 80% case for HTTP/gRPC servers, background workers, and event pipelines while allowing custom extensions.
1. Offer central configuration (env, file, code) so that operators can adjust exporters, sampling, resource attributes, and feature toggles without redeploying binaries.
1. Ship first-party documentation, examples, and lintable guidelines so adopting teams can instrument new services in <30 minutes.

### Non-Goals

- Replace vendor backends (e.g., OTLP receiver, Jaeger, Prometheus); the library will integrate with but not host observability infrastructure.
- Provide language-agnostic SDKs; the scope is Go 1.25.4+ only.
- Bundle domain-specific dashboards or alerts; those remain on the platform observability roadmap.

## 3. Target Users & Use Cases

- **Service owners** who need turnkey telemetry with sensible defaults for HTTP/gRPC APIs, streaming workers, and cron jobs.
- **Platform engineers** who need instrumentation hooks to inject tenant/resource metadata, trace propagation policies, and org-wide semantic conventions.
- **SREs** who need consistent telemetry for incident triage, regression detection, and SLO enforcement.

Core scenarios:

1. A new microservice scaffolds instrumentation via one import, automatically emitting HTTP server traces, standard metrics, and structured logs.
1. A batch worker emits custom span events and business KPIs while respecting central sampling policies.
1. A multi-tenant API enriches telemetry with tenant/resource attributes and route metadata for cardinality control.

## 4. Success Metrics

- **Adoption**: ≥80% of newly created Go services use the library within two quarters; ≥50% of existing services migrate within four quarters.
- **Performance**: Library adds ≤5% mean CPU and ≤10% p99 latency overhead at 5k req/s per instance when sampling ratio ≤20%.
- **Consistency**: 100% of exported telemetry complies with OTEL semantic conventions checks (lint) before release.
- **Operability**: P0 incidents related to missing/incorrect telemetry drop by 50% within six months of rollout.
- **Programmability**: ≥70% of surveyed teams report being able to extend instrumentation without forking the library.

## 5. Functional Requirements

1. **Bootstrap API**: Single entry point to initialize OTEL providers (meter, tracer, logger) with layered config (env vars, YAML, code overrides). Must support hot-reload for config file changes.
1. **Exporter Support**: Built-in OTLP/HTTP and OTLP/gRPC exporters with pluggable interface to add Zipkin, Jaeger, Prometheus, and custom exporters.
1. **Auto Instrumentation Adapters**:
         - HTTP(S) server/client middleware for `net/http`, `fasthttp`, `fiber v2` `chi`, `gin`, `echo`.
         - gRPC interceptors (client/server) with payload size/latency metrics.
         - SQL/database driver instrumentation using `database/sql` hooks.
         - Message queue/event instrumentation (NATS, Kafka, Pub/Sub) via wrappers.
         - Worker adapters for cron/ticker loops and Kafka consumers so background workloads reuse common helpers.
1. **Context Propagation**: W3C TraceContext and Baggage by default; allow fallback to B3 for legacy consumers.
1. **Resource Detection**: Detect environment (k8s, ECS, Lambda, bare metal) and merge with custom resource attributes supplied by integrators.
1. **Sampling Policies**: Support always-on, always-off, parent-based, and tail-based sampling (pluggable). Provide deterministic per-tenant rate limiting to prevent noisy neighbors.
1. **Metrics Package**: Common instruments (counters, histograms, gauges) plus helper functions for request/response latency, error counts, queue depth, goroutine/memory stats.
1. **Logging Integration**: Structured logging helpers that correlate logs with trace/span IDs. Provide adapters for `zap`, `zerolog`, and Go stdlib log/slog.
1. **Diagnostics**: Self-telemetry (health metrics) and debug endpoints (e.g., `/observe/status`) gated behind config for field troubleshooting. Exporter health must include protocol, endpoint, queue stats, last success/error timestamps, and cumulative error counters for each signal.
1. **Extensibility**: Public interfaces for instrumentation hooks, attribute mutators, and exporters; documentation covering how to implement each.

## 6. Non-Functional Requirements

### Performance

- Keep allocation overhead below 3% compared to baseline instrumentation-free benchmark.
- Avoid global locks in hot paths; rely on lock-free or sharded structures.
- Provide benchmarking harness (`make bench-observe`) integrated with CI.

### Scalability & Reliability

- Support batching/backpressure for exporters with configurable queue sizes.
- Ensure graceful degradation when backend unreachable (drop policy, retries with jitter, health logging).
- Enable horizontal scaling by using context-aware tenant tagging and sampling controls that work in stateless environments.

### Security & Compliance

- Redact or hash PII/PHI attributes through configurable processors.
- Support FIPS-eligible crypto dependencies and document threat model for trace propagation.

### Operability & Flexibility

- Config validation with actionable errors.
- Feature flags to enable/disable instrumentation modules at runtime.
- Clear versioning strategy (semver) with CHANGELOG and migration guides.

## 7. Architecture Overview

### Component Model

1. **Core Runtime**: Manages OTEL SDK instances, resource detectors, sampling, and exporter lifecycle.
1. **Instrumentation Packs**: Optional modules per surface (HTTP, gRPC, DB, MQ, cron) that register middleware and metrics.
1. **Exporter Adapters**: Interfaces plus default OTLP implementations; additional adapters live under `pkg/exporters`.
1. **Configuration Layer**: Sources (env, file, remote) merge into a canonical struct; supports live reload via fsnotify.
1. **Extension Points**: Hook interfaces for attribute mutation, span processors, metric pipelines, and log sinks.
1. **Diagnostics Module**: Exposes runtime health, dependency stats, and last-error info.

### Data Flow

`Application code -> Instrumentation middleware -> OTEL SDK processors -> Batch exporter -> OTLP backend`.

### Dependencies

- Go 1.25.4+, `go.opentelemetry.io/otel` latest stable, `golang.org/x` libs for context, sync, and net.
- Optional: `github.com/prometheus/client_golang` for Prometheus exporter integration, `github.com/grpc-ecosystem/go-grpc-middleware` for interceptors.

## 8. Programmability & Flexibility Strategies

- **Configurable Builders**: Fluent builder API for advanced scenarios; default helper `observe.MustInit()` for simple cases.
- **Module Registry**: Instrumentation packs register themselves via init hooks, but can be toggled via config to minimize bloat.
- **Policy Engines**: Attribute processors expressed as WASM or embedded Lua are out of scope; instead support Go interfaces plus templated rules loaded from YAML.
- **Generics & Context Helpers**: Provide typed helpers (where Go allows) to minimize boilerplate while keeping zero allocations in steady state.

## 9. Observability Surfaces

- **Traces**: Server/client spans with status codes, events, and links; ensure span names follow OTEL HTTP/gRPC conventions.
- **Metrics**: Export RED (rate, errors, duration) metrics, runtime stats, and instrumentation health metrics (dropped spans, queue size).
- **Logs**: Provide context-enriched structured logs, JSON by default, log sampling and log-to-trace correlation toggles.

## 10. Testing & Quality Strategy

- **Unit Tests**: Cover config parsing, exporter lifecycle, middleware instrumentation, log correlation utilities.
- **Integration Tests**: Spin up local OTLP collector (container) via `make test-integration` to validate end-to-end telemetry.
- **Performance Tests**: Benchmark under load (wrk/vegeta + `make bench`) and gate releases on CPU/latency thresholds.
- **Compatibility Tests**: Matrix across Go versions (1.25.4, 1.26.x), key frameworks (gin, chi, grpc-go), and exporters.
- **Static Analysis**: golangci-lint, go vet, gofumpt, staticcheck enforced via pre-commit/CI.

## 11. Rollout & Milestones

1. **M0 – Foundations (2 weeks)**: Define config schema, bootstrap OTEL SDK init, add CI scaffolding, publish docs site skeleton.
1. **M1 – HTTP/gRPC Beta (4 weeks)**: Ship HTTP/gRPC instrumentation packs, OTLP exporter, sampling controls, runtime metrics. Tag v0.1.0.
1. **M2 – Storage & MQ Support (4 weeks)**: Add SQL and message queue wrappers, logging adapters, diagnostics endpoint. Tag v0.2.0.
1. **M3 – Hardening (3 weeks)**: Performance tuning, fuzz tests, doc polish, migration guides. Release v1.0.0.
1. **M4 – Adoption Drive (ongoing)**: Partner with top-5 services, gather feedback, iterate on extension APIs.

## 12. Documentation & Developer Experience

- README updates with quickstart, `examples/` directory per use case, GoDoc comments on all public APIs.
- Decision records (`docs/adr/`) for major architectural choices.
- Templates for issue/PR to capture instrumentation gaps and perf regressions.

## 13. Open Questions & Risks

- **Tail-based Sampling Backend**: Should we depend on an external collector capability or embed an in-process tail sampler? Need cost/complexity trade study.
- **Remote Configuration**: Do we standardize on existing config service (if any) or ship a simple HTTP polling client?
- **Binary Size**: Instrumentation packs may inflate binary size; need guidance on build tags or modular imports.
- **Policy Conflicts**: How do platform-level attribute mutators interact with team-specific hooks? Need a deterministic merge strategy.
- **Security Review**: Trace context headers can leak across trust boundaries; require formal security sign-off before GA.

---

## Next Steps

- Validate requirements with platform/SRE stakeholders.
- Prototype the bootstrap API and HTTP middleware to de-risk performance targets.
- Align with observability infrastructure owners on exporter defaults and SLAs.
