package config

import "github.com/hyp3rd/ewrap"

// Validate asserts that the config meets baseline expectations.
func Validate(cfg Config) error {
	if cfg.Service.Name == "" {
		return invalidConfigError("service.name is required")
	}

	if cfg.Exporters.OTLP == nil {
		return invalidConfigError("exporters.otlp section is required")
	}

	if cfg.Exporters.OTLP.Endpoint == "" {
		return invalidConfigError("exporters.otlp.endpoint is required")
	}

	mode := cfg.Sampling.Mode
	switch mode {
	case "always_on", "always_off", "parentbased_always_on", "parentbased_always_off", "trace_id_ratio":
	default:
		return invalidConfigError("unsupported sampling.mode %q", mode)
	}

	return nil
}

func invalidConfigError(format string, args ...any) error {
	return ewrap.Newf("invalid configuration: "+format, args...)
}
