package observe

import (
	"context"

	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/logging"
)

// Option mutates initialization settings.
type Option func(*options)

type options struct {
	overrideConfig *config.Config
	loaders        []config.Loader
	logger         logging.Adapter
	loggerOverride bool
	watchConfig    bool
}

func defaultOptions() options {
	return options{
		loaders: []config.Loader{
			config.FileLoader{},
			config.EnvLoader{},
		},
		logger:      nil,
		watchConfig: true,
	}
}

func (o options) loadConfig(ctx context.Context) (config.Config, error) {
	if o.overrideConfig != nil {
		return *o.overrideConfig, nil
	}

	return config.Load(ctx, o.loaders...)
}

// WithConfig provides a fully resolved configuration and bypasses loaders.
func WithConfig(cfg config.Config) Option {
	return func(opt *options) {
		opt.overrideConfig = &cfg
	}
}

// WithLoaders replaces the default loader chain.
func WithLoaders(loaders ...config.Loader) Option {
	return func(opt *options) {
		opt.loaders = append([]config.Loader{}, loaders...)
	}
}

// WithLogger specifies the logging adapter used for runtime events.
func WithLogger(adapter logging.Adapter) Option {
	return func(opt *options) {
		opt.logger = adapter
		opt.loggerOverride = true
	}
}

// WithConfigWatcher toggles file-based config hot reload. Enabled by default.
func WithConfigWatcher(enabled bool) Option {
	return func(opt *options) {
		opt.watchConfig = enabled
	}
}

func (o options) fileWatcherPath() string {
	for _, loader := range o.loaders {
		if fl, ok := loader.(config.FileLoader); ok {
			if fl.Path != "" {
				return fl.Path
			}

			return "observe.yaml"
		}
	}

	return ""
}
