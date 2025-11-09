// Package kafka provides worker adapters for consuming Kafka messages as jobs.
package kafka

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hyp3rd/ewrap"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel/attribute"

	"github.com/hyp3rd/observe/pkg/instrumentation/messaging"
	"github.com/hyp3rd/observe/pkg/instrumentation/worker"
)

// Handler processes a Kafka message.
type Handler func(context.Context, kafka.Message) error

type reader interface {
	Config() kafka.ReaderConfig
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
}

// Consumer wires worker and messaging helpers into a kafka.Reader loop.
type Consumer struct {
	reader    reader
	worker    *worker.Helper
	messaging *messaging.Helper
}

// NewConsumer wraps the provided kafka.Reader.
func NewConsumer(r *kafka.Reader, workerHelper *worker.Helper, messagingHelper *messaging.Helper) *Consumer {
	return NewConsumerWith(r, workerHelper, messagingHelper)
}

// NewConsumerWith accepts any reader implementing the subset of kafka.Reader used by the consumer.
func NewConsumerWith(r reader, workerHelper *worker.Helper, messagingHelper *messaging.Helper) *Consumer {
	return &Consumer{
		reader:    r,
		worker:    workerHelper,
		messaging: messagingHelper,
	}
}

// Run starts the consumption loop until the context is cancelled or the handler returns an error.
func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	err := c.validate(handler)
	if err != nil {
		return err
	}

	cfg := c.reader.Config()

	for {
		err := ctx.Err()
		if err != nil {
			return ewrap.Wrap(err, "context error")
		}

		msg, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ewrap.Wrap(err, "context error")
			}

			return ewrap.Wrap(err, "fetch kafka message")
		}

		err = c.processMessage(ctx, cfg, msg, handler)
		if err != nil {
			return err
		}

		err = c.commit(ctx, msg)
		if err != nil {
			return err
		}
	}
}

func (c *Consumer) validate(handler Handler) error {
	if handler == nil {
		return ewrap.New("handler is nil")
	}

	if c.reader == nil {
		return ewrap.New("kafka reader is nil")
	}

	return nil
}

func (c *Consumer) processMessage(
	ctx context.Context,
	cfg kafka.ReaderConfig,
	msg kafka.Message,
	handler Handler,
) error {
	consumeInfo := messaging.ConsumeInfo{
		System:          "kafka",
		Destination:     cfg.Topic,
		DestinationKind: "topic",
		Group:           cfg.GroupID,
	}

	jobInfo := worker.JobInfo{
		Name:       jobName(msg),
		Queue:      cfg.Topic,
		Attributes: jobAttributes(msg),
		Schedule:   nextSchedule(msg.Time),
	}

	exec := func(execCtx context.Context) error {
		if c.messaging == nil {
			return handler(execCtx, msg)
		}

		return c.messaging.InstrumentConsume(execCtx, consumeInfo, func(ctx context.Context) error {
			return handler(ctx, msg)
		})
	}

	if c.worker != nil {
		return c.worker.Instrument(ctx, jobInfo, exec)
	}

	return exec(ctx)
}

func (c *Consumer) commit(ctx context.Context, msg kafka.Message) error {
	err := c.reader.CommitMessages(ctx, msg)
	if err != nil {
		return ewrap.Wrap(err, "commit kafka message")
	}

	return nil
}

func jobName(msg kafka.Message) string {
	for _, h := range msg.Headers {
		if strings.EqualFold(h.Key, "job-name") {
			return string(h.Value)
		}
	}

	if msg.Topic != "" {
		return msg.Topic + "-job"
	}

	return "kafka-job"
}

func jobAttributes(msg kafka.Message) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.Int("kafka.partition", msg.Partition),
		attribute.Int64("kafka.offset", msg.Offset),
	}

	if len(msg.Key) > 0 {
		attrs = append(attrs, attribute.String("kafka.key", string(msg.Key)))
	}

	return attrs
}

func nextSchedule(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}

	return ts.UTC().Format(time.RFC3339)
}
