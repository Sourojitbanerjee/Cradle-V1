package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cradle-dev/cradle/pkg/telemetry"
)

const WorkerVersion = "0.1.0-alpha"

func main() {
	logger := telemetry.NewLogger(os.Stdout, slog.LevelInfo)
	slog.SetDefault(logger)

	logger.Info("Starting Cradle Background Discovery Worker",
		slog.String("version", WorkerVersion),
		slog.Int("pid", os.Getpid()),
	)

	// Context for coordinating graceful shutdown across worker goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		s := <-sig
		logger.Info("Received termination signal; draining discovery tasks...", slog.String("signal", s.String()))
		cancel()
	}()

	logger.Info("Worker engine initialized; listening for cloud discovery tasks")

	// Main worker event loop
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Worker stopped cleanly; all tasks drained")
			return
		case <-ticker.C:
			logger.Debug("Worker heartbeat check", slog.Time("timestamp", time.Now().UTC()))
		}
	}
}
