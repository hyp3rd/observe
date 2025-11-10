// Package observe provides initialization and management of the observability runtime.
package observe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/attribute"

	"github.com/hyp3rd/observe/internal/constants"
	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/logging"
	"github.com/hyp3rd/observe/pkg/runtime"
)

// Client provides access to the active runtime and useful helpers.
type Client struct {
	mu           sync.RWMutex
	runtime      *runtime.Runtime
	opts         options
	logger       logging.Adapter
	metricsState *runtime.MetricsState
	watchCancel  context.CancelFunc
	configDigest string
}

// Init bootstraps the instrumentation runtime from configuration sources.
// Callers must invoke Shutdown when finished.
func Init(ctx context.Context, opts ...Option) (*Client, error) {
	settings := defaultOptions()
	for _, opt := range opts {
		opt(&settings)
	}

	cfg, err := settings.loadConfig(ctx)
	if err != nil {
		return nil, ewrap.Wrap(err, "load config")
	}

	logger := settings.logger
	if !settings.loggerOverride {
		logger = logging.FromConfig(cfg.Logging)
	}

	if logger == nil {
		logger = logging.NewNoopAdapter()
	}

	settings.logger = logger

	metricsState := runtime.NewMetricsState()

	rt, err := runtime.New(ctx, cfg)
	if err != nil {
		return nil, ewrap.Wrap(err, "init runtime")
	}

	err = rt.InitMetrics(metricsState)
	if err != nil {
		return nil, ewrap.Wrap(err, "init runtime metrics")
	}

	digest, err := configDigest(cfg)
	if err != nil {
		return nil, ewrap.Wrap(err, "hash config")
	}

	client := &Client{
		runtime:      rt,
		opts:         settings,
		logger:       logger,
		metricsState: metricsState,
		configDigest: digest,
	}

	err = client.startConfigWatcher(ctx)
	if err != nil {
		client.logger.Error(ctx, err, "config watcher disabled")
	}

	return client, nil
}

// Shutdown flushes telemetry, stops watchers, and releases resources.
func (c *Client) Shutdown(ctx context.Context) error {
	if c.watchCancel != nil {
		c.watchCancel()
	}

	return c.Runtime().Shutdown(ctx)
}

// Runtime exposes the underlying runtime for advanced integrations.
func (c *Client) Runtime() *runtime.Runtime {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.runtime
}

// Config returns the active configuration snapshot.
func (c *Client) Config() config.Config {
	return c.Runtime().Config()
}

func (c *Client) startConfigWatcher(ctx context.Context) error {
	if !c.opts.watchConfig {
		return nil
	}

	path := c.opts.fileWatcherPath()
	if path == "" {
		return nil
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return ewrap.Wrap(err, "resolve config path")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return ewrap.Wrap(err, "create config watcher")
	}

	dir := filepath.Dir(abs)

	err = watcher.Add(dir)
	if err != nil {
		closeErr := watcher.Close()
		if closeErr != nil {
			c.logger.Error(ctx, closeErr, "close config watcher after add failure")
		}

		return ewrap.Wrap(err, "watch config directory")
	}

	ctx, cancel := context.WithCancel(ctx)

	c.watchCancel = cancel
	go c.watchLoop(ctx, watcher, abs)

	return nil
}

// watchLoop monitors configuration changes and triggers runtime reloads.
//
//nolint:revive,cyclop // cognitive-complexity: Breaking this up would reduce clarity.
func (c *Client) watchLoop(ctx context.Context, watcher *fsnotify.Watcher, target string) {
	defer func() {
		closeErr := watcher.Close()
		if closeErr != nil {
			c.logger.Error(ctx, closeErr, "close config watcher after add failure")
		}
	}()

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	defer timer.Stop()

	pending := false

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if event.Name != target {
				continue
			}

			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}

			if c.opts.reloadDebounce <= 0 {
				c.logger.Info(ctx, "configuration change detected", attribute.String("path", target))
				c.reloadRuntime(ctx)

				continue
			}

			pending = true

			resetTimer(timer, c.opts.reloadDebounce)
		case <-timer.C:
			if !pending {
				continue
			}

			pending = false

			c.logger.Info(ctx, "configuration change detected", attribute.String("path", target))
			c.reloadRuntime(ctx)
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}

			c.logger.Error(ctx, err, "config watcher error")
		}
	}
}

func (c *Client) reloadRuntime(ctx context.Context) {
	cfg, err := c.opts.loadConfig(ctx)
	if err != nil {
		c.logger.Error(ctx, err, "reload config failed")

		return
	}

	digest, err := configDigest(cfg)
	if err != nil {
		c.logger.Error(ctx, err, "hash config failed")

		return
	}

	if digest == c.configDigest {
		c.logger.Debug(ctx, "configuration unchanged, skipping reload")

		return
	}

	if !c.opts.loggerOverride {
		if logger := logging.FromConfig(cfg.Logging); logger != nil {
			c.logger = logger
			c.opts.logger = logger
		}
	}

	rt, err := runtime.New(ctx, cfg)
	if err != nil {
		c.logger.Error(ctx, err, "runtime rebuild failed")

		return
	}

	err = rt.InitMetrics(c.metricsState)
	if err != nil {
		c.logger.Error(ctx, err, "runtime metrics init failed")

		return
	}

	c.swapRuntime(ctx, rt)
	c.metricsState.IncrementConfigReloads()
	c.configDigest = digest
	c.logger.Info(ctx, "runtime reloaded")
}

func (c *Client) swapRuntime(ctx context.Context, newRuntime *runtime.Runtime) {
	c.mu.Lock()
	old := c.runtime
	c.runtime = newRuntime
	c.mu.Unlock()

	if old != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, constants.DefaultShutdownTimeout)
		defer cancel()

		err := old.Shutdown(shutdownCtx)
		if err != nil {
			c.logger.Error(shutdownCtx, err, "shutdown previous runtime")
		}
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	timer.Reset(duration)
}

func configDigest(cfg config.Config) (string, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", ewrap.Wrap(err, "marshal config for digest")
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:]), nil
}
