// Package main shows how to wire the worker helper with the ticker adapter.
package main

import (
	"context"
	"log"
	"time"

	"github.com/hyp3rd/observe/internal/constants"
	"github.com/hyp3rd/observe/pkg/config"
	"github.com/hyp3rd/observe/pkg/instrumentation/worker"
	workerticker "github.com/hyp3rd/observe/pkg/instrumentation/worker/ticker"
	"github.com/hyp3rd/observe/pkg/observe"
)

const (
	timerDuration = 5 * time.Second
	jobDuration   = 50 * time.Millisecond
	interval      = 500 * time.Millisecond
)

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
	defer shutdown(ctx, client)

	helper := client.Runtime().WorkerHelper()
	if helper == nil {
		log.Println("worker helper is disabled; ensure instrumentation.worker.enabled=true")

		return
	}

	adapter, err := workerticker.NewAdapter(helper, workerticker.Config{
		Interval: interval,
		Job: worker.JobInfo{
			Name:  "cache-refresh",
			Queue: "cron",
		},
		ErrorHandler: func(err error) {
			log.Printf("worker job failed: %v", err)
		},
	})
	if err != nil {
		log.Printf("create ticker adapter: %v\n", err)

		return
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		err = adapter.Start(runCtx, func(jobCtx context.Context) error {
			_, span := client.Runtime().Tracer("examples/worker").Start(jobCtx, "refresh-cache")

			timer := time.NewTimer(jobDuration)
			defer timer.Stop()

			<-timer.C
			span.End()

			return nil
		})
		if err != nil {
			log.Printf("start adapter: %v", err)
		}
	}()

	timer := time.NewTimer(timerDuration)
	defer timer.Stop()

	<-timer.C

	stopCtx, stopCancel := context.WithTimeout(ctx, constants.DefaultTimeout)
	defer stopCancel()

	err = adapter.Stop(stopCtx)
	if err != nil {
		log.Printf("stop adapter: %v", err)
	}
}

func shutdown(ctx context.Context, client *observe.Client) {
	shutdownCtx, cancel := context.WithTimeout(ctx, constants.DefaultShutdownTimeout)
	defer cancel()

	err := client.Shutdown(shutdownCtx)
	if err != nil {
		log.Printf("observe shutdown error: %v", err)
	}
}
