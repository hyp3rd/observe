# Developer Guide

This document captures the day‑to‑day details required to extend or debug the Observe runtime. It complements the high level overview in `docs/architecture.md`.

## Runtime Layout

| Package | Notes |
| --- | --- |
| `pkg/observe` | Entry point (`Init`, `Shutdown`), file watcher, config debounce/fingerprinting, runtime swapping. |
| `pkg/runtime` | OTEL provider wiring, exporter lifecycle, diagnostics snapshots, metrics state. |
| `pkg/logging` | Adapter abstraction + config driven level/sampling controls. |
| `pkg/instrumentation/*` | Helper packs (HTTP/gRPC/SQL/messaging/worker/Kafka adapters). |
| `pkg/config` | Schema, loaders (file/env), validation, defaults. |
| `pkg/diagnostics` | `/observe/status` handler, exporter health surface. |

## Logging & Correlation

- The `logging.Adapter` interface now exposes `Debug`, `Info`, and `Error`. Every adapter automatically injects `trace_id`/`span_id` when a span context is present.
- `logging.FromConfig` composes:
      - An adapter (`slog`, `zap`, `zerolog`, or stdlib `log.Logger`).
      - Level filters (currently `error` drops debug+info).
      - Probabilistic sampling via `logging.LoggingConfig.SampleRatio` to curb noisy info/debug logs without losing errors.
- When the watcher reloads configuration and no explicit logger override was provided, `FromConfig` is called again so runtime logging honours new settings.
- Tests live in `pkg/logging/logging_test.go` to ensure trace correlation remains intact; add new adapters here for regression coverage.

## Config Reload Flow

1. `observe.Init` resolves file/env loaders and starts an `fsnotify` watcher when `WithConfigWatcher(true)` (default) is set.
1. File events are debounced (`WithReloadDebounce`, default `250ms`). Burst writes reset the timer and only trigger a reload once.
1. Each config snapshot is SHA-256 hashed (`configDigest`). If the digest hasn’t changed since the last reload the runtime logs a debug message and exits early, preventing exporter thrash.
1. On successful reload:
        - A new runtime is constructed and metrics are initialized before swapping.
        - The previous runtime is shut down with `constants.DefaultShutdownTimeout`.
        - `MetricsState` increments the reload counter, which surfaces via diagnostics and runtime metrics.
1. Errors at any step are logged via the adapter so operators can spot misconfigurations quickly.

## Instrumentation Highlights

- Workers: `pkg/instrumentation/worker` exposes helpers; ticker and Kafka adapters (`worker/ticker`, `worker/kafka`) show how to wrap concrete schedulers/consumers.
- Messaging: `pkg/instrumentation/messaging` plus Kafka wrappers share helper structs (`PublishInfo`, `ConsumeInfo`) for semantic alignment.
- Diagnostics: `/observe/status` returns exporter protocol, endpoint, last success/error timestamps, and cumulative error counts for both traces and metrics. Ensure new exporters update `traceExporterStats`/`metricExporterStats`.

## Developer Workflow

1. **Coding**: follow existing patterns (no globals, errors wrapped with `github.com/hyp3rd/ewrap`).
1. **Lint**: `golangci-lint` (see `.golangci.yaml`) enforces `revive`, `staticcheck`, `dupl`, etc. Keep functions under complexity limits; extract helpers where necessary.
1. **Tests**: run `go test ./...`. When working inside the sandbox use `GOCACHE=$(pwd)/.cache/go-build go test ./...` to avoid permission issues, then remove `.cache/`.
1. **Documentation**: update `README.md`, `docs/architecture.md`, and `docs/instrumentation.md` when behaviour changes. This developer guide should be extended with operational details.

## Troubleshooting Tips

- **Watcher noise**: if local tooling rapidly writes config files, increase the debounce interval via `observe.WithReloadDebounce`.
- **Skipped reloads**: enable debug logging (set `logging.level: debug`) to see “configuration unchanged, skipping reload” messages; verify config digests differ.
- **Logging tests**: when adding adapters ensure they’re covered in `pkg/logging/logging_test.go` to confirm trace propagation and JSON formats.
- **Diagnostics**: use `/observe/status` locally (set `diagnostics.http_addr: "127.0.0.1:4319"`) to verify exporter stats, reload counts, and instrumentation toggles after code changes.
