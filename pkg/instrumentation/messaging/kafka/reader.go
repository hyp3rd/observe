// Package kafka provides instrumentation for Kafka consumers and producers.
package kafka

import (
	"context"

	"github.com/segmentio/kafka-go"

	"github.com/hyp3rd/observe/pkg/instrumentation/messaging"
)

// Reader wraps a kafka.Reader with instrumentation.
type Reader struct {
	reader kafkaReader
	helper *messaging.Helper
}

type kafkaReader interface {
	Config() kafka.ReaderConfig
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

// NewReader instruments the provided kafka.Reader.
func NewReader(inner *kafka.Reader, helper *messaging.Helper) *Reader {
	return NewReaderWith(inner, helper)
}

// NewReaderWith instruments the provided kafka.Reader.
func NewReaderWith(inner kafkaReader, helper *messaging.Helper) *Reader {
	return &Reader{
		reader: inner,
		helper: helper,
	}
}

// FetchMessage instruments the fetch operation and returns the fetched message.
func (r *Reader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	if r.helper == nil {
		return r.reader.FetchMessage(ctx)
	}

	var (
		msg kafka.Message
		err error
	)

	cfg := r.reader.Config()
	info := messaging.ConsumeInfo{
		System:          "kafka",
		Destination:     cfg.Topic,
		DestinationKind: "topic",
		Group:           cfg.GroupID,
	}

	wrappedErr := r.helper.InstrumentConsume(ctx, info, func(ctx context.Context) error {
		msg, err = r.reader.FetchMessage(ctx)

		return err
	})
	if wrappedErr != nil {
		return kafka.Message{}, wrappedErr
	}

	return msg, nil
}

// CommitMessages delegates to the underlying reader.
func (r *Reader) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	return r.reader.CommitMessages(ctx, msgs...)
}
