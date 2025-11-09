package kafka

import (
	"context"

	"github.com/segmentio/kafka-go"

	"github.com/hyp3rd/observe/pkg/instrumentation/messaging"
)

// Writer wraps a kafka.Writer with instrumentation.
type Writer struct {
	writer kafkaWriter
	helper *messaging.Helper
}

type kafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// NewWriter returns a Writer wrapper that instruments publish operations via the messaging helper.
func NewWriter(inner *kafka.Writer, helper *messaging.Helper) *Writer {
	return NewWriterWith(inner, helper)
}

// NewWriterWith returns a Writer wrapper that instruments publish operations via the messaging helper.
func NewWriterWith(inner kafkaWriter, helper *messaging.Helper) *Writer {
	return &Writer{
		writer: inner,
		helper: helper,
	}
}

// WriteMessages instruments the call and delegates to the underlying writer.
func (w *Writer) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	if len(msgs) == 0 || w.helper == nil {
		return w.writer.WriteMessages(ctx, msgs...)
	}

	info := messaging.PublishInfo{
		System:          "kafka",
		Destination:     msgs[0].Topic,
		DestinationKind: "topic",
		SizeBytes:       totalPayloadBytes(msgs),
	}

	return w.helper.InstrumentPublish(ctx, info, func(ctx context.Context) error {
		return w.writer.WriteMessages(ctx, msgs...)
	})
}

func totalPayloadBytes(msgs []kafka.Message) int64 {
	var total int64
	for _, msg := range msgs {
		total += int64(len(msg.Value))
	}

	return total
}
