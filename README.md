# Observe

## Development Environment Quick Start

### Features

- **Quick Setup**: One command to initialize your project
- **Pre-configured Tooling**: golangci-lint, gci, gofumpt, staticcheck
- **Pre-commit Hooks**: Automated code quality checks before commits
- **Protocol Buffers Support**: Built-in buf configuration for gRPC/protobuf projects
- **Testing**: Pre-configured test suite with coverage
- **Documentation**: Code of Conduct and Contributing guidelines included
- **Best Practices**: Follows Go project layout standards

### Install Development Tools

```bash
# Install all required Go tools and pre-commit hooks
make prepare-toolchain
```

This will install:

- `gci` - Go import formatter
- `gofumpt` - Stricter gofmt
- `golangci-lint` - Comprehensive linter
- `staticcheck` - Advanced static analysis
- `pre-commit` - Git hook framework

### 4. Start Coding

Your project structure is ready:

```text
.
├── api/           # Public API definitions (protobuf, OpenAPI)
├── internal/      # Private application code
├── pkg/           # Public library code
├── .pre-commit/   # Pre-commit hook scripts
├── Makefile       # Common development tasks
└── go.mod         # Go module file
```

## Development Workflow

### Running Tests

```bash
# Run all tests with coverage
make test

# Run benchmarks
make bench
```

### Code Quality

```bash
# Run all linters and formatters
make lint

# Run go vet with shadow analysis
make vet

# Update dependencies
make update-deps
```

### Pre-commit Hooks

Pre-commit hooks run automatically on `git commit`. They check:

- Import formatting (gci)
- Code linting (golangci-lint)
- Unit tests
- Markdown formatting
- YAML validation
- Trailing whitespace
- Spell checking

To run hooks manually:

```bash
pre-commit run --all-files
```

### Protocol Buffers (Optional)

If you're building a gRPC service:

```bash
# Install protobuf tools
make prepare-proto-tools

# Update dependencies
make proto-update

# Lint proto files
make proto-lint

# Generate code from proto files
make proto-generate

# Format proto files
make proto-format

# Run all proto tasks
make proto
```

## Project Customization

### Configure Linters

Edit `.golangci.yaml` to customize linting rules for your project.

### Customize Spell Checker

Add project-specific words to `cspell.json` in the `words` array.

## Available Make Targets

```bash
make help               # Show all available targets
make prepare-toolchain  # Install development tools
make update-toolchain   # Update the development tools the their latest version
make test               # Run tests
make bench              # Run benchmarks
make lint               # Run all linters
make vet                # Run go vet
make update-deps        # Update dependencies
make proto              # Run all protobuf tasks (if using gRPC)
```

## Requirements

- Go 1.25.4 or later
- Git
- Python 3.x (for pre-commit)
- Docker (optional, for containerized builds)

## Project Layout

This template follows the [Standard Go Project Layout](https://github.com/golang-standards/project-layout):

- **`/api`** - OpenAPI/Swagger specs, JSON schema files, protocol definition files
- **`/internal`** - Private application and library code
- **`/pkg`** - Library code that's ok to use by external applications

## Instrumentation Library

Observe ships with a reusable OpenTelemetry bootstrap library under `pkg/observe`.

### Quick Start

```go
package main

import (
 "context"
 "log"

 "github.com/hyp3rd/observe/pkg/observe"
)

func main() {
 ctx := context.Background()
 client, err := observe.Init(ctx)
 if err != nil {
  log.Fatalf("init observe: %v", err)
 }
 defer client.Shutdown(ctx)

 tracer := client.Runtime().Tracer("demo")
 ctx, span := tracer.Start(ctx, "demo-work")
 defer span.End()

 // application logic ...
 _ = ctx
}
```

### Configuration Layering

Configuration sources merge in the following order:

1. Built-in defaults (`pkg/config/defaults.go`).
1. Project-level `observe.yaml` (optional).
1. Environment variables prefixed with `OBSERVE_` (use double underscores to separate sections, e.g. `OBSERVE_SERVICE__NAME`).
1. Runtime overrides supplied via `observe.WithConfig`.

Environment variables accept comma-separated lists for slice fields (for example, `OBSERVE_INSTRUMENTATION__HTTP__IGNORED_ROUTES=/healthz,/readyz`).

Example `observe.yaml`:

```yaml
service:
  name: payments-api
  environment: production
exporters:
  otlp:
    endpoint: otel-collector.monitoring.svc:4317
    protocol: grpc
sampling:
  mode: trace_id_ratio
  argument: 0.2
```

### HTTP/gRPC Helpers

`pkg/runtime` wires OTLP exporters plus middleware packs automatically. Retrieve helpers from the runtime:

```go
httpMiddleware := client.Runtime().HTTPMiddleware()
if httpMiddleware != nil {
    mux := http.NewServeMux()
    mux.Handle("/api", httpMiddleware.Handler(apiHandler))
}

grpcServer := grpc.NewServer(
    grpc.UnaryInterceptor(client.Runtime().GRPCUnaryServerInterceptor()),
)
```

The HTTP middleware emits RED metrics and spans following OTEL semantic conventions. The gRPC interceptors capture spans for both server and client sides with optional metadata allowlists.

### SQL Instrumentation

When `instrumentation.sql.enabled` is true, use the SQL helper to register or open instrumented drivers:

```go
sqlHelper := client.Runtime().SQLHelper()
driverName, err := sqlHelper.Register("postgres")
db, err := sqlHelper.Open(driverName, os.Getenv("PG_DSN"))
```

Set `instrumentation.sql.collect_queries` to `false` to redact `db.statement` attributes if needed.

### Diagnostics Endpoint

Enable `diagnostics.enabled` (default) to expose `/observe/status` on `diagnostics.http_addr`. The endpoint returns JSON snapshots containing service metadata, exporter configuration, and instrumentation toggles. Protect the endpoint by setting `diagnostics.auth_token`—requests must supply `Authorization: Bearer <token>`.

### Config Hot Reload & Logging

`observe.Init` watches the configured `observe.yaml` by default. Updates are applied live without restarts. Disable this behavior with `observe.WithConfigWatcher(false)`. Runtime events (reloads, watcher errors) can be routed to your preferred logger via the adapters under `pkg/logging`:

```go
adapter := logging.NewSlogAdapter(slog.NewJSONHandler(os.Stdout, nil))
client, err := observe.Init(ctx, observe.WithLogger(adapter))
```

Available adapters: `slog`, `zap`, `zerolog`, and `log.Logger`, each automatically enriched with trace/span identifiers.

## Troubleshooting

### Pre-commit hooks fail

```bash
# Reinstall pre-commit hooks
pre-commit uninstall
pre-commit install
pre-commit install-hooks
```

### Go module issues

```bash
# Reset and reinitialize module
rm go.mod go.sum
./setup-project.sh --module github.com/your_username/your_project
go mod tidy
```

### Linter installation fails

```bash
# Clean and reinstall tools
rm -rf $(go env GOPATH)/bin/{gci,gofumpt,golangci-lint,staticcheck}
make prepare-toolchain
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for details on how to contribute to this project.

## Code of Conduct

This project adheres to a Code of Conduct. See [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for details.

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Toolchain Support

- [Documentation](https://github.com/hyp3rd/starter/wiki)
- [Issue Tracker](https://github.com/hyp3rd/starter/issues)
- [Discussions](https://github.com/hyp3rd/starter/discussions)
