package config_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/hyp3rd/observe/pkg/config"
)

func TestLoadLayers(t *testing.T) {
	t.Setenv("OBSERVE_SERVICE__NAME", "env-service")
	t.Setenv("OBSERVE_INSTRUMENTATION__HTTP__ENABLED", "false")
	t.Setenv("OBSERVE_INSTRUMENTATION__HTTP__IGNORED_ROUTES", "/healthz,/readyz")

	fs := fstest.MapFS{
		"observe.yaml": {
			Data: []byte(`
service:
  name: file-service
  environment: staging
exporters:
  otlp:
    endpoint: collector:4317
`),
		},
	}

	cfg, err := config.Load(context.Background(),
		config.FileLoader{FS: fs},
		config.EnvLoader{},
	)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.Service.Name != "env-service" {
		t.Fatalf("expected env override for service.name, got %q", cfg.Service.Name)
	}

	if cfg.Service.Environment != "staging" {
		t.Fatalf("expected service.environment from file, got %q", cfg.Service.Environment)
	}

	if cfg.Exporters.OTLP.Endpoint != "collector:4317" {
		t.Fatalf("expected exporter endpoint from file, got %q", cfg.Exporters.OTLP.Endpoint)
	}

	if cfg.Instrumentation.HTTP.Enabled {
		t.Fatal("expected http instrumentation disabled by env override")
	}

	if got := cfg.Instrumentation.HTTP.IgnoredRoutes; len(got) != 2 || got[0] != "/healthz" || got[1] != "/readyz" {
		t.Fatalf("unexpected ignored routes: %#v", got)
	}
}
