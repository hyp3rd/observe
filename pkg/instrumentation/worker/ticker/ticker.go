// Package ticker provides a concrete worker adapter that runs jobs on a fixed interval.
package ticker

import (
	"context"
	"sync"
	"time"

	"github.com/hyp3rd/ewrap"

	"github.com/hyp3rd/observe/pkg/instrumentation/worker"
)

// Config configures a Ticker adapter.
type Config struct {
	Interval     time.Duration
	Job          worker.JobInfo
	ErrorHandler func(error)
}

// Adapter wraps a worker.Helper and executes jobs on a ticker.
type Adapter struct {
	helper    *worker.Helper
	cfg       Config
	newTicker func(time.Duration) ticker

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
}

// JobFunc is executed every interval tick.
type JobFunc func(context.Context) error

type ticker interface {
	C() <-chan time.Time
	Stop()
}

// NewAdapter constructs a ticker-based worker adapter.
func NewAdapter(helper *worker.Helper, cfg Config) (*Adapter, error) {
	if helper == nil {
		return nil, ewrap.New("worker helper is required")
	}

	if cfg.Interval <= 0 {
		return nil, ewrap.New("interval must be greater than zero")
	}

	job := cfg.Job
	if job.Name == "" {
		job.Name = "worker-job"
	}

	if job.Schedule == "" {
		job.Schedule = cfg.Interval.String()
	}

	cfg.Job = job

	return &Adapter{
		helper:    helper,
		cfg:       cfg,
		newTicker: defaultTickerFactory,
	}, nil
}

// Start begins executing fn every configured interval until the context is canceled or Stop is invoked.
func (a *Adapter) Start(ctx context.Context, fn JobFunc) error {
	if fn == nil {
		return ewrap.New("job function is required")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.running {
		return ewrap.New("ticker adapter already running")
	}

	jobCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.running = true

	a.wg.Add(1)

	go a.run(jobCtx, fn)

	return nil
}

// Stop stops the adapter and waits for in-flight executions to finish.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	cancel := a.cancel
	running := a.running
	a.mu.Unlock()

	if !running {
		return nil
	}

	if cancel != nil {
		cancel()
	}

	done := make(chan struct{})

	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ewrap.Wrap(ctx.Err(), "stop ticker adapter")
	case <-done:
		return nil
	}
}

func (a *Adapter) run(ctx context.Context, fn JobFunc) {
	defer a.wg.Done()
	defer a.markStopped()

	ticker := a.newTicker(a.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			err := a.helper.Instrument(ctx, a.cfg.Job, fn)
			if err != nil && a.cfg.ErrorHandler != nil {
				a.cfg.ErrorHandler(err)
			}
		}
	}
}

func (a *Adapter) markStopped() {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.running = false
	a.cancel = nil
}

func defaultTickerFactory(interval time.Duration) ticker {
	return &stdTicker{inner: time.NewTicker(interval)}
}

type stdTicker struct {
	inner *time.Ticker
}

func (t *stdTicker) C() <-chan time.Time {
	return t.inner.C
}

func (t *stdTicker) Stop() {
	t.inner.Stop()
}
