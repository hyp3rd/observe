// Package main demonstrates configuring Observe with custom logging adapters.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"

	"github.com/hyp3rd/observe/internal/constants"
	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/logging"
	"github.com/hyp3rd/observe/pkg/observe"
)

const timerDuration = 15 * time.Millisecond

func main() {
	ctx := context.Background()

	adapter := logging.NewSlogAdapter(slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}),
	))

	client, err := observe.Init(ctx,
		observe.WithLoaders(
			config.FileLoader{Path: "examples/observe.yaml"},
			config.EnvLoader{},
		),
		observe.WithLogger(adapter),
	)
	if err != nil {
		log.Fatalf("init observe: %v", err)
	}
	defer shutdown(ctx, client)

	tracer := client.Runtime().Tracer("examples/logging")

	reqCtx, span := tracer.Start(ctx, "handle-request")
	defer span.End()

	adapter.Debug(reqCtx, "request received",
		attribute.String("route", "/healthz"),
		attribute.String("method", "GET"),
	)

	_, processSpan := tracer.Start(reqCtx, "process")

	timer := time.NewTimer(timerDuration)
	defer timer.Stop()

	<-timer.C

	processSpan.End()

	adapter.Info(reqCtx, "request completed", attribute.String("status", "ok"))
}

func shutdown(ctx context.Context, client *observe.Client) {
	shutdownCtx, cancel := context.WithTimeout(ctx, constants.DefaultShutdownTimeout)
	defer cancel()

	err := client.Shutdown(shutdownCtx)
	if err != nil {
		log.Printf("observe shutdown error: %v", err)
	}
}
