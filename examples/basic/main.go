// Package main demonstrates a basic example of using the observe library.
package main

import (
	"context"
	"log"
	"time"

	"github.com/hyp3rd/observe/internal/constants"
	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/observe"
)

const timerDuration = 10 * time.Millisecond

func main() {
	ctx := context.Background()

	client, err := observe.Init(ctx,
		observe.WithLoaders(
			config.FileLoader{Path: "examples/observe.yaml"},
			config.EnvLoader{},
		),
	)
	if err != nil {
		log.Fatalf("init observe: %v", err)
	}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), constants.DefaultTimeout)
		defer cancel()

		err := client.Shutdown(shutdownCtx)
		if err != nil {
			log.Printf("observe shutdown error: %v", err)
		}
	}()

	tracer := client.Runtime().Tracer("examples/basic")

	ctx, span := tracer.Start(ctx, "demo-span")
	defer span.End()

	timer := time.NewTimer(timerDuration)
	<-timer.C

	_ = ctx
}
