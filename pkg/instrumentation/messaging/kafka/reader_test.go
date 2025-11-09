package kafka_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hyp3rd/ewrap"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"

	"github.com/hyp3rd/observe/pkg/instrumentation/messaging"
	observekafka "github.com/hyp3rd/observe/pkg/instrumentation/messaging/kafka"
)

func TestReaderFetchMessageInstrumentsConsume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tp := trace.NewTracerProvider()
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	helper, err := messaging.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("NewHelper returned error: %v", err)
	}

	stub := &stubKafkaReader{
		config: kafka.ReaderConfig{
			Topic:   "payments",
			GroupID: "group-1",
		},
		message: kafka.Message{Topic: "payments"},
	}
	instrumented := observekafka.NewReaderWith(stub, helper)

	msg, err := instrumented.FetchMessage(ctx)
	if err != nil {
		t.Fatalf("FetchMessage returned error: %v", err)
	}

	if msg.Topic != "payments" {
		t.Fatalf("unexpected topic: %s", msg.Topic)
	}
}

func TestReaderFetchMessagePropagatesErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	helper, err := messaging.NewHelper(trace.NewTracerProvider(), metric.NewMeterProvider())
	if err != nil {
		t.Fatalf("NewHelper returned error: %v", err)
	}

	expected := ewrap.New("fetch failed")
	stub := &stubKafkaReader{
		config:   kafka.ReaderConfig{Topic: "orders"},
		fetchErr: expected,
	}
	instrumented := observekafka.NewReaderWith(stub, helper)

	_, err = instrumented.FetchMessage(ctx)
	if !errors.Is(err, expected) {
		t.Fatalf("expected %v, got %v", expected, err)
	}
}

type stubKafkaReader struct {
	config   kafka.ReaderConfig
	message  kafka.Message
	fetchErr error
}

func (s *stubKafkaReader) Config() kafka.ReaderConfig {
	return s.config
}

func (s *stubKafkaReader) FetchMessage(_ context.Context) (kafka.Message, error) {
	if s.fetchErr != nil {
		return kafka.Message{}, s.fetchErr
	}

	return s.message, nil
}

func (*stubKafkaReader) CommitMessages(_ context.Context, _ ...kafka.Message) error {
	return nil
}
