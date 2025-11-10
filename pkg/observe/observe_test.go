package observe

import (
	"testing"

	"github.com/hyp3rd/observe/pkg/config"
)

const configDigestErrorMsg = "configDigest returned error: %v"

func TestConfigDigestStable(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Service: config.ServiceConfig{
			Name:        "svc",
			Environment: "prod",
		},
	}

	first, err := configDigest(cfg)
	if err != nil {
		t.Fatalf(configDigestErrorMsg, err)
	}

	second, err := configDigest(cfg)
	if err != nil {
		t.Fatalf(configDigestErrorMsg, err)
	}

	if first != second {
		t.Fatalf("expected stable digest, got %s vs %s", first, second)
	}
}

func TestConfigDigestDiffers(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}

	initialDigest, err := configDigest(cfg)
	if err != nil {
		t.Fatalf(configDigestErrorMsg, err)
	}

	cfg.Service.Name = "svc"

	updatedDigest, err := configDigest(cfg)
	if err != nil {
		t.Fatalf(configDigestErrorMsg, err)
	}

	if initialDigest == updatedDigest {
		t.Fatal("expected different digests when config changes")
	}
}
