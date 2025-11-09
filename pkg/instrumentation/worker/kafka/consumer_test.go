package kafka

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hyp3rd/ewrap"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/hyp3rd/observe/pkg/instrumentation/messaging"
	"github.com/hyp3rd/observe/pkg/instrumentation/worker"
)

const (
	offset  = 42
	msgTime = 1710000000
)

func TestConsumerRunSuccess(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reader := &stubReader{
		cfg: kafka.ReaderConfig{
			Topic:   "orders",
			GroupID: "billing",
		},
		messages: []kafka.Message{
			{
				Topic:     "orders",
				Partition: 1,
				Offset:    offset,
				Time:      time.Unix(msgTime, 0),
				Headers: []kafka.Header{
					{Key: "job-name", Value: []byte("charge-card")},
				},
				Value: []byte("payload"),
			},
		},
	}

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	readerMeter := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(readerMeter))

	mHelper, err := messaging.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("messaging helper: %v", err)
	}

	wHelper := newWorkerHelper(t)

	consumer := NewConsumerWith(reader, wHelper, mHelper)

	handlerCalls := 0

	err = consumer.Run(ctx, func(_ context.Context, _ kafka.Message) error {
		handlerCalls++

		cancel()

		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}

	if handlerCalls != 1 {
		t.Fatalf("expected handler called once, got %d", handlerCalls)
	}

	if reader.commitCount != 1 {
		t.Fatalf("expected 1 commit, got %d", reader.commitCount)
	}

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("expected spans to be recorded")
	}

	var rm metricdata.ResourceMetrics

	err = readerMeter.Collect(context.Background(), &rm)
	if err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	if !hasMetric(rm, "messaging.consume.count") {
		t.Fatal("expected messaging.consume.count metric")
	}
}

func TestConsumerRunHandlerError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	reader := &stubReader{
		cfg: kafka.ReaderConfig{
			Topic:   "orders",
			GroupID: "billing",
		},
		messages: []kafka.Message{
			{Topic: "orders"},
		},
	}

	wHelper := newWorkerHelper(t)
	consumer := NewConsumerWith(reader, wHelper, nil)

	handlerErr := ewrap.New("handler failed")

	err := consumer.Run(ctx, func(context.Context, kafka.Message) error {
		return handlerErr
	})
	if !errors.Is(err, handlerErr) {
		t.Fatalf("expected handler error, got %v", err)
	}

	if reader.commitCount != 0 {
		t.Fatalf("expected commit skipped on error, got %d", reader.commitCount)
	}
}

func newWorkerHelper(t *testing.T) *worker.Helper {
	t.Helper()

	tp := sdktrace.NewTracerProvider()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	helper, err := worker.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("worker helper: %v", err)
	}

	return helper
}

type stubReader struct {
	cfg         kafka.ReaderConfig
	messages    []kafka.Message
	commitCount int
}

func (s *stubReader) Config() kafka.ReaderConfig {
	return s.cfg
}

func (s *stubReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if len(s.messages) == 0 {
		return kafka.Message{}, context.Canceled
	}

	msg := s.messages[0]
	s.messages = s.messages[1:]

	select {
	case <-ctx.Done():
		return kafka.Message{}, ewrap.Wrap(ctx.Err(), "context done")
	default:
	}

	return msg, nil
}

func (s *stubReader) CommitMessages(context.Context, ...kafka.Message) error {
	s.commitCount++

	return nil
}

func hasMetric(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, met := range scope.Metrics {
			if met.Name == name {
				return true
			}
		}
	}

	return false
}
