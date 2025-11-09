package kafka_test

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/hyp3rd/observe/pkg/instrumentation/messaging"
	observekafka "github.com/hyp3rd/observe/pkg/instrumentation/messaging/kafka"
)

func TestWriterInstrumentsPublish(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	reader := metric.NewManualReader()
	mp := metric.NewMeterProvider(metric.WithReader(reader))

	helper, err := messaging.NewHelper(tp, mp)
	if err != nil {
		t.Fatalf("NewHelper returned error: %v", err)
	}

	stub := &stubKafkaWriter{}
	writer := observekafka.NewWriterWith(stub, helper)

	msg := kafka.Message{Topic: "orders", Value: []byte("data")}

	err = writer.WriteMessages(ctx, msg)
	if err != nil {
		t.Fatalf("WriteMessages returned error: %v", err)
	}

	if !stub.called {
		t.Fatal("expected underlying writer to be called")
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected span to be recorded, got %d", len(spans))
	}

	if spans[0].Name() != "orders" {
		t.Fatalf("unexpected span name %q", spans[0].Name())
	}
}

type stubKafkaWriter struct {
	called bool
}

func (s *stubKafkaWriter) WriteMessages(_ context.Context, _ ...kafka.Message) error {
	s.called = true

	return nil
}
