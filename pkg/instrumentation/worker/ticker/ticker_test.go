package ticker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hyp3rd/ewrap"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/hyp3rd/observe/pkg/instrumentation/worker"
)

func TestNewAdapterValidation(t *testing.T) {
	t.Parallel()

	helper := newTestHelper(t)

	_, err := NewAdapter(nil, Config{Interval: time.Second})
	if err == nil {
		t.Fatal("expected error when helper is nil")
	}

	_, err = NewAdapter(helper, Config{Interval: 0})
	if err == nil {
		t.Fatal("expected error when interval is zero")
	}
}

func TestAdapterRunsJobs(t *testing.T) {
	t.Parallel()

	helper := newTestHelper(t)

	cfg := Config{
		Interval: time.Second,
		Job: worker.JobInfo{
			Name: "cache-refresh",
		},
	}

	fake := newFakeTicker()

	handlerCh := make(chan error, 1)
	jobErr := ewrap.New("job failed")
	cfg.ErrorHandler = func(err error) {
		select {
		case handlerCh <- err:
		default:
		}
	}

	adapter, err := NewAdapter(helper, cfg)
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	adapter.newTicker = func(time.Duration) ticker {
		return fake
	}

	ctx := t.Context()

	jobCalled := make(chan struct{})

	var once sync.Once

	err = adapter.Start(ctx, func(context.Context) error {
		once.Do(func() {
			close(jobCalled)
		})

		return jobErr
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	fake.tick()

	select {
	case <-jobCalled:
	case <-time.After(time.Second):
		t.Fatal("job was not executed")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	err = adapter.Stop(stopCtx)
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	select {
	case err = <-handlerCh:
		if !errors.Is(err, jobErr) {
			t.Fatalf("expected handler error %v, got %v", jobErr, err)
		}
	default:
		t.Fatal("expected error handler to be invoked")
	}
}

func TestAdapterStartErrors(t *testing.T) {
	t.Parallel()

	helper := newTestHelper(t)

	adapter, err := NewAdapter(helper, Config{Interval: time.Second})
	if err != nil {
		t.Fatalf("NewAdapter returned error: %v", err)
	}

	ctx := t.Context()

	err = adapter.Start(ctx, nil)
	if err == nil {
		t.Fatal("expected error when job func is nil")
	}

	err = adapter.Start(ctx, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("expected first start to succeed, got %v", err)
	}

	err = adapter.Start(ctx, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("expected error when starting twice")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()

	err = adapter.Stop(stopCtx)
	if err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
}

func newTestHelper(t *testing.T) *worker.Helper {
	t.Helper()

	tp := sdktrace.NewTracerProvider()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	helper, err := worker.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("NewHelper returned error: %v", err)
	}

	return helper
}

type fakeTicker struct {
	ch chan time.Time
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{
		ch: make(chan time.Time, 1),
	}
}

func (t *fakeTicker) C() <-chan time.Time {
	return t.ch
}

func (*fakeTicker) Stop() {}

func (t *fakeTicker) tick() {
	t.ch <- time.Now()
}
